// ============================================================
// internal/application/scheduler/engine.go - 定时任务调度引擎
//
// 【纳米级源码说明书 - 架构篇】
// 这是整个系统的“心脏起搏器”。它的唯一职责是“看表”——到了规定时间，就拉响警报。
// 
// ============================================================
// 💡 【大厂面试·底层原理扩展：海量定时任务调度 (Timing Wheel vs Min Heap)】
// 
// 场景重现：
// 面试官问：如果系统里有 100 万个定时任务，Cron 引擎底层是怎么做到每一秒都能精准触发的？难道是一个个遍历（O(N)）吗？
//
// 底层剖析与大厂对冲方案：
// 1. 最小堆（Min Heap）：`robfig/cron` 底层实际上是用了一个排序数组（相当于最小堆）。
//    它会把所有任务按“下一次执行时间”从小到大排序。每过一秒钟，它只需要看数组的第一个元素（堆顶），
//    如果堆顶的时间没到，那后面的绝对都没到，直接睡眠；如果堆顶的时间到了，就把它拉出来执行，
//    然后重新计算它的下一次时间并插入堆中（O(logN)）。这是中小规模的最优解。
// 2. 时间轮（Timing Wheel）：如果是 Kafka 或千万级延迟任务（如订单 30 分钟未支付自动取消），大厂会用时间轮。
//    想象一个带 60 个槽位的钟表表盘，每一格代表一秒。指针每秒走一格（O(1)），走到哪一格，
//    就把挂在这一格上的链表（任务队列）全部扔进线程池执行。如果是 30 分钟后的任务，就放在层级时间轮（多级齿轮）里。
//    时间复杂度从 O(logN) 降到了极致的 O(1)。
// 3. 阻塞反噬防御：由于 Cron 触发非常快，如果闹钟响了，你直接在当前协程里去查库或者发网络请求，
//    哪怕卡住 1 秒，后面的闹钟全都会跟着迟到（这就是面试常考的“定时器漂移”）。
//    Cronix 的解法：大爷（Engine）只负责按门铃（将 taskID 扔进异步的 `triggerCh` 通道），
//    绝对不亲自去车间干活！这样门卫大爷永远不会被堵住，保证了纳秒级的触发精度。
//
// ============================================================
// 
// 核心工作流：
//   1. 看表（cron） -> 2. 到点按铃（channel） -> 3. 车间主任收到信号开始排兵布阵（DAG） -> 4. 工人干活（ants线程池）
//
// ============================================================
package scheduler

import (
    "context"     // 【大厂考点】上下文：用来传递取消信号、超时控制
    "strings"     // 字符串处理：检查cron字段数量
    "sync"        // 【大厂考点】并发控制：Mutex互斥锁。保证多个人同时改排班表时，不会乱套。

    "cronix/internal/domain/model" // 数据模型：任务结构体

    "github.com/robfig/cron/v3" // robfig/cron：Go语言最火的定时任务库，底层原理类似“时间轮 (Timing Wheel)”
    "github.com/rs/zerolog/log"  // zerolog：超高性能日志库
    "gorm.io/gorm"              // GORM：操作数据库的利器
)

// Engine 是定时任务调度引擎（门卫大爷）
// struct 就是把大爷需要的各种工具打包放在他身上。
type Engine struct {
    // 【考点】底层定时器。支持精确到秒的6字段Cron表达式（秒 分 时 日 月 周）。
    cron           *cron.Cron             
    
    // 数据库连接，大爷用来查排班表的。
    db             *gorm.DB               
    
    // 【大厂高频考点】channel (通道)。这根电线带了“缓冲(buffer)”。
    // 门铃响得太快，车间主任来不及处理怎么办？缓冲通道可以先把铃声存起来（最多存1024个），慢慢处理。
    //
    // 📌 图解缓冲通道（Buffered Channel）：
    // 就像一个传送带，最多可以放 1024 个包裹（任务ID）。
    //
    // [ Engine 门卫 ] ---> (包裹1) (包裹2) (包裹3) ... (空) ---> [ Executor 主任 ]
    //
    // 面试官问：底层是怎么实现的？
    // 答：底层是一个【环形数组 (Ring Buffer)】加上一把锁。指针 head 指向头，tail 指向尾，写满了就阻塞等待。
    triggerCh      chan uint              
    
    // 【考点】互斥锁（排他锁）。大爷在改排班表时，得把门锁上，不然别人也来改，表就被撕破了。
    mu             sync.Mutex             
    
    // 映射表（花名册）：记录 任务ID -> Cron条目ID。
    // 如果任务A不干了，大爷得知道任务A对应排班表上的哪一行（EntryID），把它划掉。
    entryMap       map[uint]cron.EntryID  
    groupEntryMap  map[uint]cron.EntryID  // 组ID的映射表
    
    // 这是一个“回调函数”（Callback）。遇到组任务到点，大爷就通过这个函数通知外面。
    groupTrigger   func(uint)             
}

// NewEngine 是门卫大爷的“入职办理”函数。工厂函数，返回一个大爷的指针。
func NewEngine(db *gorm.DB) *Engine {
    // 【Go语法注意】用 & 取地址，返回指针。这样传递大爷的时候，传递的是他的工位号，而不是把他克隆一份。
    return &Engine{
        cron:          cron.New(cron.WithSeconds()), // 开启秒级精度支持！
        db:            db,                            
        triggerCh:     make(chan uint, 1024),         // 核心电线！容量为 1024。
        entryMap:      make(map[uint]cron.EntryID),   // map 必须用 make 初始化！千万别忘了！
        groupEntryMap: make(map[uint]cron.EntryID),   
    }
}

// TriggerChan 暴露出这根电线给外面（车间主任）接上。
// 【大厂考点：单向通道】 `<-chan` 意思是这个通道只能“往外读（出水）”，不能“往里写（进水）”。
// 这样就从语法级别防止了车间主任乱按门铃，只有门卫大爷能按。这叫“权限最小化原则”。
func (e *Engine) TriggerChan() <-chan uint {
    return e.triggerCh
}

// SetGroupTrigger 设置回调函数。
func (e *Engine) SetGroupTrigger(fn func(uint)) {
    e.groupTrigger = fn
}

// Start 大爷开始上班！
func (e *Engine) Start() {
	e.cron.Start() // 钟表开始走动
	GlobalMetricsRegistry.Start() // 打点监控系统也跟着启动
}

// Stop 大爷下班！
// 【考点】优雅关机（Graceful Shutdown）：大爷不是拔腿就跑，而是把手头的活交接完。
func (e *Engine) Stop() context.Context {
	GlobalMetricsRegistry.Stop()
	// Stop() 返回一个上下文，外面可以通过它知道啥时候真正的旧任务彻底跑完了。
	return e.cron.Stop() 
}

// ReloadAll 全量重载：大爷把旧排班表全撕了，重新去数据库抄一份最新的。
func (e *Engine) ReloadAll() error {
    // 1. 先查数据库（无锁操作！）
    // 为什么不加锁？因为查数据库很慢（可能要几十毫秒）。如果加着锁去查，整个系统就卡住了（锁竞争阻塞）。
    var tasks []model.Task                                      
    if err := e.db.Where("enabled = ?", true).Find(&tasks).Error; err != nil { 
        return err                                              
    }

    // 2. 查完数据拿在手里了，再加锁（快进快出）！
    e.mu.Lock()                                                 
    defer e.mu.Unlock() // 【大厂考点：defer】函数执行完一定会解锁，就算中间发生 panic 也会解锁，绝对不会死锁。

    // 3. 撕掉旧排班表
    for taskID, entryID := range e.entryMap {                   
        e.cron.Remove(entryID)                                  
        delete(e.entryMap, taskID)                              
    }

    // 4. 抄写新排班表
    var skipped int                                             
    for _, task := range tasks {                                
        taskID := task.ID                                       
        // 常驻任务（Daemon）不归看表大爷管，归另一位“贴身保镖（DaemonMonitor）”管，所以跳过。
        if task.RunMode == "daemon" {
            continue
        }
        // 如果没有写时间（只允许手动触发的），大爷也不管。
        if task.CronExpr == "" {
            continue
        }
        // 被编入某个“班组”的任务，由班长（Group）统一管，大爷也不单独管它了。
        if task.GroupID != nil {
            continue
        }
        
        expr := task.CronExpr                                   
        // 【兼容性设计】如果用户输的是5位的标准linux cron（没包含秒），自动在前面补个 "0 "，表示第0秒执行。
        if len(strings.Fields(expr)) == 5 {
            expr = "0 " + expr
        }
        
        // 【核心触发逻辑】
        // AddFunc 是注册闹钟。到了 expr 规定的时间，就会执行后面这个匿名函数 (func() {...})。
        entryID, err := e.cron.AddFunc(expr, func() {           
            // 【大厂考点：闭包 (Closure)】
            // 这里的 taskID 就是上面的局部变量。闹钟响的时候，它能准确记得自己代表的那个任务ID。
            e.triggerCh <- taskID // 叮咚！把 ID 丢进电线（通道）。
        })
        if err != nil {
            // 如果某个人乱写 cron，报错了，大爷只记个小本本（警告），不影响其他人。这叫“容错性”。
            log.Warn().Err(err).Str("task", task.Name).Str("cron", task.CronExpr).Msg("跳过无效cron表达式的任务")
            skipped++
            continue
        }
        e.entryMap[taskID] = entryID // 记录在案！
    }

    if skipped > 0 {
        log.Warn().Int("skipped", skipped).Int("loaded", len(tasks)-skipped).Msg("部分任务因无效cron被跳过")
    } else {
        log.Info().Int("loaded", len(tasks)).Msg("所有任务加载成功")
    }

    // 5. 处理任务组（Group）的排班，逻辑同上。
    for _, eid := range e.groupEntryMap {
        e.cron.Remove(eid)
    }
    e.groupEntryMap = make(map[uint]cron.EntryID)
    var groups []model.TaskGroup
    if err := e.db.Where("enabled = ? AND cron_expr != ''", true).Find(&groups).Error; err == nil {
        for _, g := range groups {
            gid := g.ID
            expr := g.CronExpr
            if len(strings.Fields(expr)) == 5 {
                expr = "0 " + expr
            }
            if e.groupTrigger != nil {
                entryID, err := e.cron.AddFunc(expr, func() {
                    e.groupTrigger(gid) // 触发班长回调！
                })
                if err != nil {
                    log.Warn().Err(err).Str("group", g.Name).Str("cron", g.CronExpr).Msg("跳过无效cron的组")
                } else {
                    e.groupEntryMap[gid] = entryID
                }
            }
        }
        log.Info().Int("groups", len(e.groupEntryMap)).Msg("任务组定时加载完成")
    }

    return nil                                                   
}

// RemoveTaskSchedule 增量更新：大爷只划掉排班表上的某一个人，不用全撕。
// 【大厂考点：增量同步】如果为了改一个任务，就触发 ReloadAll 全量查询，性能会极其低下。增量更香！
func (e *Engine) RemoveTaskSchedule(taskID uint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if entryID, ok := e.entryMap[taskID]; ok { // 去查大爷的花名册
		e.cron.Remove(entryID)                 // 找到了就删
		delete(e.entryMap, taskID)
		log.Info().Uint("task_id", taskID).Msg("增量移除任务定时器")
	}
}

// UpdateTaskSchedule 增量注册/更新。
func (e *Engine) UpdateTaskSchedule(task model.Task) error {
	// 先把旧的删了。这是更新的常规操作（先删后加 = 修改）
	e.RemoveTaskSchedule(task.ID)

	// 常驻任务不管
	if task.RunMode == "daemon" {
		return nil
	}

	// 没开启、没cron、或者有爹（组长）的，不管
	if !task.Enabled || task.CronExpr == "" || task.GroupID != nil {
		return nil
	}

	e.mu.Lock() // 要修改大爷的花名册了，上锁！
	defer e.mu.Unlock()

	expr := task.CronExpr
	if len(strings.Fields(expr)) == 5 {
		expr = "0 " + expr
	}

	taskID := task.ID
	entryID, err := e.cron.AddFunc(expr, func() {
		e.triggerCh <- taskID // 叮咚！
	})
	if err != nil {
		log.Warn().Err(err).Str("task", task.Name).Str("cron", task.CronExpr).Msg("增量注册任务定时器失败")
		return err
	}

	e.entryMap[taskID] = entryID // 更新花名册
	log.Info().Str("task", task.Name).Uint("task_id", taskID).Str("cron", expr).Msg("增量注册任务定时器成功")
	return nil
}

// RemoveGroupSchedule 组的增量删除，同理。
func (e *Engine) RemoveGroupSchedule(groupID uint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if entryID, ok := e.groupEntryMap[groupID]; ok {
		e.cron.Remove(entryID)
		delete(e.groupEntryMap, groupID)
		log.Info().Uint("group_id", groupID).Msg("增量移除任务组定时器")
	}
}

// UpdateGroupSchedule 组的增量更新，同理。
func (e *Engine) UpdateGroupSchedule(group model.TaskGroup) error {
	e.RemoveGroupSchedule(group.ID)

	if !group.Enabled || group.CronExpr == "" {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	expr := group.CronExpr
	if len(strings.Fields(expr)) == 5 {
		expr = "0 " + expr
	}

	if e.groupTrigger != nil {
		groupID := group.ID
		entryID, err := e.cron.AddFunc(expr, func() {
			e.groupTrigger(groupID) // 喊组长！
		})
		if err != nil {
			log.Warn().Err(err).Str("group", group.Name).Str("cron", group.CronExpr).Msg("增量注册任务组定时器失败")
			return err
		}
		e.groupEntryMap[groupID] = entryID
		log.Info().Str("group", group.Name).Uint("group_id", groupID).Str("cron", expr).Msg("增量注册任务组定时器成功")
	}
	return nil
}
