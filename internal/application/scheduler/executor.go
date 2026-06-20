// ============================================================
// internal/application/scheduler/executor.go - 任务执行协调器
//
// 【纳米级源码说明书 - 架构篇】
// 这是工厂里的“车间主任”。门卫大爷（Engine）只负责按铃，按铃之后所有的苦活累活，都归车间主任管。
// 
// ============================================================
// 💡 【大厂面试·底层原理扩展：协程池并发底盘与 N+1 并发控制】
// 
// 场景重现 1：
// 面试官问：如果双十一大促，1 秒内涌入 100 万个定时任务，你的系统会 OOM（内存溢出）挂掉吗？
//
// 底层剖析与大厂对冲方案（ants 协程池）：
// 1. 无限并发的灾难：Go 语言虽然号称“协程很轻量”（每个约 2KB），但 100 万个协程同时跑依然会瞬间吃掉 2GB 内存，
//    更恐怖的是，这 100 万个协程如果都在读数据库（创建 100 万个 DB 连接），数据库瞬间就会被击穿！
// 2. 协程池的削峰填谷：这里引入了全网性能天花板的 `ants.Pool`。我们限制整个厂子最多只有 N 个打工人（比如 1000 个）。
//    这 1000 个人复用 1000 个栈内存空间。当第 1001 个任务来时，它必须在门外“阻塞排队等待”，绝对不允许它抢占系统资源。
// 3. Worker 清理机制：大促过后，系统闲下来了，这 1000 个协程会一直占着内存吗？
//    不会！ants 底层有定期的清除线程（Purge Goroutine），会自动回收长时间没活干的 Worker，让内存回到初始状态。
//
// 场景重现 2：
// 面试官问：什么是 DAG（有向无环图）调度？如果有一个环怎么办？
//
// 底层剖析与大厂对冲方案：
// 1. 经典场景：必须先【下载数据A】，才能【分析数据B】。这就是依赖。如果 B 依赖 A，A 依赖 B，就是死锁（死循环）。
//    我们的 `buildDAG` 会在算法层面检测环的存在（深度优先搜索 DFS），如果是环，直接拦截报错。
// 2. 并发拓扑排序：
// 
// 📌 图解 DAG 并发执行分层：
// [任务A] ---> [任务B] ---+
//                         |---> [任务D]
// [任务C] ----------------+
// 
// 第一层：[任务A], [任务C]  -> (入度为0，同时塞进 ants 并发执行)
// 第二层：[任务B]           -> (必须等A跑完，WG 阻塞)
// 第三层：[任务D]           -> (必须等B和C都跑完)
// 
// 核心：在同一层内（如 A 和 C），我们用 `sync.WaitGroup` 包装放入协程池，实现“极速并发”！
// ============================================================
package scheduler

import (
	"context"      // 上下文：控制协程生死
	"fmt"          // 格式化输出
	"runtime"      // 【大厂考点】获取电脑真实 CPU 核心数
	"sync"         // 【大厂考点】并发控制：WaitGroup (等待组) 非常经典
	"sync/atomic"  // 【大厂考点】原子操作：不需要加锁的、极速的数字加减法
	"time"         

	"cronix/internal/infrastructure/config"   
	"cronix/internal/application/executor"  // 具体去执行 Shell/HTTP 的干将
	"cronix/internal/domain/model"     
	"cronix/internal/infrastructure/notify"    // 发微信/邮件/钉钉

	"github.com/panjf2000/ants/v2"   // ants：全网超牛的 Goroutine 池（线程池）
	"github.com/rs/zerolog/log"      
	"gorm.io/gorm"                   
)

// StatsCacheInvalidator 缓存清理小助手。
// 面试问：为什么定义接口？
// 答：为了解耦。主任只管叫它清理，不关心它是用什么技术（Redis/内存）清理的。
type StatsCacheInvalidator interface {
	InvalidateStatsCache()
}

// Executor 执行器（车间主任）结构体。
type Executor struct {
	db               *gorm.DB               
	logRepo          LogRepository          // 日志仓储：操作数据库的封装接口
	
	// 【大厂考点】异步刷盘神器：把日志凑够一批再写硬盘，速度快100倍。
	// 📌 为什么不直接写硬盘？
	// 硬盘很慢（毫秒级），内存很快（纳秒级）。如果每个任务干完都卡在那里写硬盘，整个系统就变乌龟了。
	// 异步写就是把账本塞进一个“信箱（Channel）”，让旁边专门负责抄写的书童（后台协程）慢慢写。
	asyncWriter      *AsyncLogWriter        
	
	pool             *ants.Pool             // 协程池
	cfg              *config.Config         
	engine           *Engine                // 门卫大爷：用来接收大爷按的门铃
	CacheInvalidator StatsCacheInvalidator 
	Notifier         *notify.Notifier       
	logWriteCounter  uint64                 // 原子计数器：记满一定次数，就打扫一次历史垃圾。
}

// NewExecutor 办理车间主任入职
func NewExecutor(db *gorm.DB, cfg *config.Config, engine *Engine) (*Executor, error) {
    // 【协程池大小计算】
    poolSize := cfg.Executor.PoolSize                          
    if poolSize <= 0 {                                         
        // 如果老板没规定多少工人，默认用 CPU 核心数 * 4。
        // 面试常问：为什么要 *4？ 
        // 答：因为任务多半是网络请求或者跑脚本，会等待（IO密集型）。在等待时CPU是闲着的，可以多派几个人顶上。
        poolSize = runtime.NumCPU() * 4                        
    }
    
    // 初始化 ants 协程池
    pool, err := ants.NewPool(poolSize)                         
    if err != nil {
        return nil, fmt.Errorf("create ants pool: %w", err)    // %w 是 Go1.13 加入的，叫“错误包裹”。
    }
    
    // 实例化日志库和异步写入器
    logRepo := NewGormLogRepository(db)
    asyncWriter := NewAsyncLogWriter(logRepo, 200) // 一次攒 200 条才写盘！
    asyncWriter.Start() // 启动异步记账员

    exec := &Executor{db: db, logRepo: logRepo, asyncWriter: asyncWriter, pool: pool, cfg: cfg, engine: engine}

    // 主任上任第一件事：清理之前的烂摊子（孤儿日志）
    // 孤儿日志就是：程序上次异常断电，导致某些任务一直显示“正在运行（Running）”。现在得把它们强行标为失败。
    now := time.Now()
    if err := logRepo.CleanupOrphanedLogs(now); err != nil {
        log.Error().Err(err).Msg("启动时清理孤儿执行日志失败")
    }
    
    // 组执行的孤儿日志同理处理
    db.Model(&model.GroupExecutionLog{}).
        Where("status = ? AND end_time IS NULL", model.StateRunning).
        Updates(map[string]interface{}{
            "status":    model.StateFailed,
            "error_msg": "System restarted or crashed",
            "end_time":  now,
        })

    // 主任给大爷（engine）塞了一张纸条：“如果有组任务（Group）到点了，按这个规则办”。
    engine.SetGroupTrigger(func(groupID uint) {
        var g model.TaskGroup
        if err := db.First(&g, groupID).Error; err != nil {
            return
        }
        var members []model.Task
        // 把组成员全部找出来，按照 sort_order（排序号）排好队
        db.Where("group_id = ?", groupID).Order("sort_order ASC, id ASC").Find(&members)
        if len(members) == 0 {
            return
        }
        // 触发组运行大招
        exec.RunGroup(&g, members, "cron")
    })
    return exec, nil
}

// Run 车间主任的日常巡逻（主循环）。
// 它是阻塞的，只有系统要关闭（ctx.Done）时才会退出。
func (e *Executor) Run(ctx context.Context) {
    // 给自己定个每个小时响一次的闹钟，用来打扫历史垃圾日志。
    cleanupTicker := time.NewTicker(1 * time.Hour)              
    defer cleanupTicker.Stop()                                  

    for {                                                       // 无限循环
        // 【大厂必考：Select 多路复用】
        // Select 就像主任坐在办公室，盯着三个电话机，哪个响了就接哪个。
        select {                                                
        case <-ctx.Done():                                      // 电话1：老板说公司倒闭了（服务停止），收拾包袱走人！
            return                                              
        case taskID := <-e.engine.TriggerChan():                // 电话2：大爷按了门铃！任务 taskID 该干活了！
            e.handleTrigger(taskID)                             // 赶紧派人去干活！
        case <-cleanupTicker.C:                                 // 电话3：每小时闹钟响了！
            e.cleanupOldLogs()                                  // 拿扫把去打扫过期日志！
        }
    }
}

// cleanupOldLogs 扫地大妈：清理过期日志。
func (e *Executor) cleanupOldLogs() {
    retentionDays := e.cfg.Log.RetentionDays                    // 保留多少天
    maxRecords := e.cfg.Log.MaxRecords                          // 最多留多少条

    // 策略一：删太老的（时间过滤）
    if retentionDays > 0 {
        // time.Now().Add(-X) 表示现在时间倒退 X。
        cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour) 
        result := e.db.Where("created_at < ?", cutoff).Delete(&model.ExecutionLog{}) 
        if result.Error == nil && result.RowsAffected > 0 {                                          
            log.Info().Int64("deleted", result.RowsAffected).Int("retention_days", retentionDays).Msg("log cleanup (retention)")
        }
    }

    // 策略二：如果还是太多，按条数硬删
    if maxRecords > 0 {
        e.deleteOldestBatch(&model.ExecutionLog{}, "execution_logs", maxRecords)
    }

    // 对 Group 日志也执行同样的两种扫地策略...
    if retentionDays > 0 {
        cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
        e.db.Where("created_at < ?", cutoff).Delete(&model.GroupExecutionLog{})
    }
    if maxRecords > 0 {
        e.deleteOldestBatch(&model.GroupExecutionLog{}, "group_execution_logs", maxRecords)
    }
}

// deleteOldestBatch 【大厂面试考点：慢SQL防范】
// 为什么不直接 DELETE FROM xxx LIMIT 50000 删掉多出来的记录？
// 答：因为一次性删几万条数据，会导致数据库表被死锁（长事务），甚至把数据库卡死。
// 所以这里用了“切香肠”战术（Batch），一次只删 1000 条，删完歇一歇再删，绝不给数据库太大压力。
func (e *Executor) deleteOldestBatch(model interface{}, tableName string, maxRecords int) {
    var count int64
    e.db.Model(model).Count(&count) // 先数数有多少条
    if count <= int64(maxRecords) {
        return // 没超标，收工
    }
    excess := count - int64(maxRecords) // 超标了多少条
    batchSize := int64(1000)            // 每次切 1000 条
    
    for excess > 0 {
        n := batchSize
        if excess < n {
            n = excess // 最后一次切剩下那一丁点
        }
        // 子查询找到最旧的 n 个 ID，然后删掉。
        result := e.db.Where("id IN (?)",
            e.db.Model(model).Select("id").Order("id ASC").Limit(int(n)),
        ).Delete(model)
        
        if result.Error != nil || result.RowsAffected == 0 {
            break // 删不动了或者报错了，跑路
        }
        excess -= result.RowsAffected // 剩余超标数减小
    }
}

// RunTaskNow 网页前端用户点了一下“立即运行”按钮，就会调用这里。
func (e *Executor) RunTaskNow(taskID uint) {
    var task model.Task
    if err := e.db.First(&task, taskID).Error; err != nil {     
        log.Error().Err(err).Uint("task_id", taskID).Msg("manual trigger: task not found")
        return
    }
    log.Info().Str("task", task.Name).Uint("id", task.ID).Msg("manual trigger") 

    // 【考点】开一个新的协程去后台跑，让前端立马收到 "启动成功" 提示，不阻塞 HTTP 响应。
    go func() {
        defer func() {
            if r := recover(); r != nil {                       
                log.Error().Interface("panic", r).Uint("task_id", taskID).Msg("manual task panic")
            }
        }()
        e.executeTask(taskID)
    }()
}

// handleTrigger 接收到了大爷的触发信号。
func (e *Executor) handleTrigger(taskID uint) {
	// 【架构之美：二级缓冲】
	// 第一级：大爷的 channel。
	// 第二级：马上开个轻量协程去处理。
	// 为什么要开协程去处理？如果 e.pool.Submit 被塞满了卡住了，不开协程的话，主任就会一直傻站在这里，
	// 导致大爷的其他门铃他也听不到了（主循环被阻塞）。开了协程相当于找了个临时工去排队等干活。
	go func() {
		// e.pool.Submit：把活丢给工人池（ants线程池）。
		err := e.pool.Submit(func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Uint("task_id", taskID).Msg("cron task panic")
				}
			}()
			e.executeTask(taskID)
		})
		if err != nil { // 如果池子都爆了（说明系统彻底扛不住了）
			log.Error().Err(err).Uint("task_id", taskID).Msg("Failed to submit task to pool (pool exhausted)")
		}
	}()
}

// buildDAG 构建【有向无环图 DAG】。
// 如果 A 必须在 B 前面跑，B 在 C 前面跑，连起来就是 A->B->C 的图。
// 【算法考点】用图论解决任务依赖问题。
func (e *Executor) buildDAG(tasks []model.Task) *DAG {
    dag := NewDAG()                                             
    taskMap := make(map[uint]model.Task)                        
    for _, t := range tasks {
        taskMap[t.ID] = t
        dag.AddNode(t.ID)                                       
    }
    var deps []model.TaskDep                                    
    e.db.Find(&deps)
    for _, dep := range deps {                                  
        if _, ok := taskMap[dep.TaskID]; !ok {                 
            continue
        }
        if _, ok := taskMap[dep.DependsOnID]; !ok {            
            continue
        }
        // 画箭头：前置任务 -> 后置任务
        dag.AddEdge(dep.DependsOnID, dep.TaskID)               
    }
    return dag
}

// executeTask 包装函数。给独立定时任务用的，它写日志是异步的。
func (e *Executor) executeTask(taskID uint) {
    e.executeTaskInternal(taskID, false)
}

// executeTaskSync 同步写日志版。用于组（Group）任务，因为组任务下一步要立刻去数据库查上一步的结果！
func (e *Executor) executeTaskSync(taskID uint) {
    e.executeTaskInternal(taskID, true)
}

// executeTaskInternal 这是车间里最复杂的一台机器，它负责完成一个任务的【全生命周期】。
func (e *Executor) executeTaskInternal(taskID uint, syncSave bool) {
    var task model.Task
    if err := e.db.First(&task, taskID).Error; err != nil {
        return
    }
    
    // 1. 【防重击穿】检查这个任务是不是已经在跑了。如果在跑，直接放弃，防止产生雪崩。
    runningCount, _ := e.logRepo.CountRunningLogs(taskID)
    if runningCount > 0 {
        log.Warn().Uint("task_id", taskID).Str("task", task.Name).Msg("任务已有正在执行的记录，跳过本次触发")
        return
    }

    // 2. 登机牌：马上在数据库里抢注一条 "Running" 的执行日志。
    now := time.Now()
    execLog := model.ExecutionLog{
        TaskID:      &task.ID,
        TaskName:    task.Name,
        CronExpr:    task.CronExpr,
        Status:      model.StateRunning,
        TriggerType: "cron",
        StartTime:   now,
    }
    e.logRepo.CreateExecutionLog(&execLog)

    // 3. 容错大考：【重试机制】！
    maxRetries := task.RetryCount
    if maxRetries < 0 { maxRetries = 0 }

    // 【风控】为了防止用户设置了一个永远执行不完的脚本（死循环），系统加了兜底“强制超时”。
    timeout := task.TimeoutSec
    if maxTO := e.cfg.Executor.MaxTimeoutSec; maxTO > 0 && timeout > maxTO {
        timeout = maxTO // 不能超出了公司规定的最高时长
    }
    if timeout <= 0 && e.cfg.Executor.MaxTimeoutSec > 0 {
        timeout = e.cfg.Executor.MaxTimeoutSec // 用户没填，用公司兜底
    }
    
    // 一个精巧的 for 循环实现了自动重试：从第 0 次（首发）干到 maxRetries（重试）次。
    for attempt := 0; attempt <= maxRetries; attempt++ {
        if attempt > 0 { // 如果是重试，先睡一会儿压压惊。
            execLog.RetryAttempt = attempt
            time.Sleep(time.Duration(task.RetryIntervalSec) * time.Second)
        }
        
        execLog.Status = model.StateRunning
        execLog.ErrorMsg = ""
        e.runTaskByType(&task, &execLog, timeout) // 真人肉搏：去干活
        
        // 成功了？那就立马跳出循环，不用再重试了！
        if execLog.Status == model.StateSuccess {
            break
        }
    }

    // 4. 交工与算账：活干完了，把记账本保存起来。
    if syncSave {
        e.logRepo.SaveExecutionLog(&execLog)   // 同步记账（立刻写库）
    } else {
        e.asyncWriter.Enqueue(&execLog)        // 异步记账（扔进纸篓让后台慢慢写）
    }
    
    // 控制某个任务下面不能积攒太多日志。
    if execLog.TaskID != nil && e.cfg.Log.MaxLogsPerTask > 0 {
        taskIDToClean := *execLog.TaskID
        maxLogs := e.cfg.Log.MaxLogsPerTask
        go func() {
            _ = e.logRepo.DeleteExcessTaskLogs(taskIDToClean, maxLogs)
        }()
    }
    
    // 【大厂考点：无锁计数器 atomic】
    // 📌 底层原理：
    // 普通的加减法，在多线程下会“撞车”，必须加锁，导致排队很慢。
    // CPU 的硬件级别提供了一种叫 CAS（Compare-And-Swap）的指令，一次就能原子化完成加减。
    // 这叫无锁编程。每干完一个活，全局计数器+1。到了500次，就派大妈去打扫一次全场垃圾。
    atomic.AddUint64(&e.logWriteCounter, 1)
    if atomic.LoadUint64(&e.logWriteCounter)%500 == 0 {
        go e.cleanupOldLogs()
    }

    // 5. 汇报成果：发短信/邮件给对应的负责人。
    e.notifyTaskResult(&task, &execLog)
}

// ExecuteTaskWithContext 专为常驻程序（Daemon）提供的版本。
// 区别：它带了上下文（Context），一旦上面把 Context 停了，这个活会被强制一刀斩断。
func (e *Executor) ExecuteTaskWithContext(ctx context.Context, taskID uint) {
    var task model.Task
    if err := e.db.First(&task, taskID).Error; err != nil {
        return
    }
    // 防重
    runningCount, _ := e.logRepo.CountRunningLogs(taskID)
    if runningCount > 0 {
        return
    }

    now := time.Now()
    execLog := model.ExecutionLog{
        TaskID:      &task.ID,
        TaskName:    task.Name,
        CronExpr:    task.CronExpr,
        Status:      model.StateRunning,
        TriggerType: "daemon",
        StartTime:   now,
    }
    e.logRepo.CreateExecutionLog(&execLog)

    // 执行任务。常驻任务不用重试（由保镖自己负责复活），也不用超时配置（一直跑到天荒地老，除非 Context 被杀掉）。
    e.runTaskByTypeCtx(ctx, &task, &execLog, 0)
    e.logRepo.SaveExecutionLog(&execLog)
    e.notifyTaskResult(&task, &execLog)
}

// runTaskByType 是一个普通外壳。
func (e *Executor) runTaskByType(task *model.Task, execLog *model.ExecutionLog, timeoutSec int) {
    e.runTaskByTypeCtx(context.Background(), task, execLog, timeoutSec)
}

// runTaskByTypeCtx：真正根据工种发派工具。
// 包含了 Shell、HTTP、Cleanup、HealthCheck 各种花样。
func (e *Executor) runTaskByTypeCtx(ctx context.Context, task *model.Task, execLog *model.ExecutionLog, timeoutSec int) {
	truncateKB := e.cfg.Executor.OutputTruncateKB                // 防打爆硬盘：最大输出长度限制

    switch task.TaskType {                                       // Go语言神级 Switch：不需要加 break 就会自动断开
    case "shell":
        // 去调用 Shell 脚本的底层能力
        result := executor.ExecuteShell(ctx, task.Command, task.WorkDir, timeoutSec, task.RunAs, task.ID)
        if result.Error != nil {
            execLog.Status = model.StateFailed
            execLog.ErrorMsg = result.Error.Error()
        } else {
            execLog.Status = model.StateSuccess
        }
        execLog.ExitCode = &result.ExitCode                      // Linux 标准的返回码，0就是好，非0就是出错了。
        execLog.Output = truncate(result.Output, truncateKB)     // 截断日志

    case "http":
        // 去调用 HTTP 接口的底层能力
        result := executor.ExecuteHTTP(ctx, task.HTTPMethod, task.HTTPURL,
            task.HTTPHeaders, task.HTTPBody, task.HTTPAuthType, task.HTTPAuthConfig,
            timeoutSec, e.cfg.CircuitBreaker.FailureThreshold,
            e.cfg.CircuitBreaker.CooldownSeconds)
        if result.Error != nil {
            execLog.Status = model.StateFailed
            execLog.ErrorMsg = result.Error.Error()
        } else if result.StatusCode >= 400 {                     // 【Web常识】状态码如 404/500 等都意味着出毛病了。
            execLog.Status = model.StateFailed
            execLog.ErrorMsg = fmt.Sprintf("HTTP %d", result.StatusCode)
        } else {
            execLog.Status = model.StateSuccess
        }
        code := result.StatusCode
        execLog.ExitCode = &code
        execLog.Output = truncate(result.Body, truncateKB)

    case "cleanup":
        result := executor.ExecuteCleanup(ctx, task.Command)     
        if result.Error != nil {
            execLog.Status = model.StateFailed
            execLog.ErrorMsg = result.Error.Error()
        } else {
            execLog.Status = model.StateSuccess
            execLog.Output = fmt.Sprintf("Deleted %d files", result.DeletedCount) 
        }
        code := 0
        execLog.ExitCode = &code

    case "healthcheck":
        result := executor.ExecuteHealthCheck(ctx, task.HTTPURL, timeoutSec)
        if result.Error != nil {
            execLog.Status = model.StateFailed
            execLog.ErrorMsg = result.Error.Error()
        } else {
            execLog.Status = model.StateSuccess
        }
        code := result.StatusCode
        execLog.ExitCode = &code
        execLog.Output = fmt.Sprintf("Status: %d", result.StatusCode) 

	default:
		// 其他奇怪的不认识的种类，当成错误。
		execLog.Status = model.StateFailed
		execLog.ErrorMsg = fmt.Sprintf("unknown task type: %s", task.TaskType)
	}

	now := time.Now()
	execLog.EndTime = &now                                       

	// 数据大屏用到的：记录耗时
	duration := now.Sub(execLog.StartTime).Milliseconds()
	GlobalMetricsRegistry.RecordExecution(duration, execLog.Status == model.StateSuccess)

    if e.CacheInvalidator != nil {
        e.CacheInvalidator.InvalidateStatsCache() 
    }
}

// truncate 日志截断。比如脚本执行输出了10G乱码，不截断整个系统内存就爆了。
func truncate(s string, maxKB int) string {
    maxBytes := maxKB * 1024                                     
    if len(s) > maxBytes {                                       
        return s[:maxBytes] + "\n... (truncated)"                // 贴心加上一段小字
    }
    return s
}

// notifyTaskResult 智能通知中心。
func (e *Executor) notifyTaskResult(task *model.Task, execLog *model.ExecutionLog) {
    var notifies []model.NotifyConfig                            
    e.db.Where("task_id = ?", task.ID).Find(&notifies) // 去数据库找找谁关心这个任务
    
    for _, n := range notifies {                                 
        // 判断有没有触及到那个人的神经。比如配置了成功通知，它确实成功了；配置了失败通知，它确实失败了。
        shouldNotify := (execLog.Status == model.StateSuccess && n.OnSuccess) ||
            (execLog.Status == model.StateFailed && n.OnFailure)
        if !shouldNotify {
            continue
        }
        if e.Notifier != nil {
            event := notify.NotifyEvent{
                TaskName: task.Name,
                Status:   execLog.Status,
                Config:   n,
            }
            
            // 【大厂神操作：非阻塞投递 channel】
            // default 分支的存在，保证了当发通知的小弟实在干不过来（队列满）的时候，
            // 主线业务不会傻傻等他，而是直接丢弃这条通知，然后打印个警告走人。
            // 
            // 📌 图解 Lossy Queue (有损丢弃队列)
            // 正常队伍：
            //   [写邮件工人] <---(排队)--- [任务A] [任务B] [任务C] ... (队伍满) [新来的任务D只能干等，把整个车间堵死]
            // 有损队列：
            //   [写邮件工人] <---(排队)--- [任务A] [任务B] ... (满) [新任务D直接被扔进垃圾桶并打印警告，绝不堵死车间！]
            // 
            select {
            case e.Notifier.NotifyChan() <- event:
                log.Debug().Str("task", task.Name).Str("type", n.NotifyType).Msg("通知事件已入队")
            default:
                log.Warn().Str("task", task.Name).Str("type", n.NotifyType).Msg("通知队列已满，本条通知被丢弃")
            }
        }
    }
}

// Shutdown 安全下班程序。
func (e *Executor) Shutdown() {
    if e.asyncWriter != nil {
        e.asyncWriter.Stop()  // 先让记账员把手头的账写完。
    }
    e.pool.Release()          // 再把所有工人遣散。
}

// RunGroup 运行“组任务”。这可是高级玩法。
// 组有三种形态：
//   1. parallel（并发型）：所有人一起上。
//   2. sequential（串行型）：A干完B干，B干完C干。一旦中间谁倒下，后面的人就不用干了。
//   3. dag（图网络型）：A和B同时干，他们都干完了C才能干，非常复杂。
func (e *Executor) RunGroup(g *model.TaskGroup, members []model.Task, triggerType string) {
    now := time.Now()
    glog := model.GroupExecutionLog{
        GroupID:     g.ID,
        GroupName:   g.Name,
        Mode:        g.Mode,
        TriggerType: triggerType,
        MemberCount: len(members),
        Status:      model.StateRunning,
        StartTime:   now,
    }
    e.logRepo.CreateGroupLog(&glog)

    var success, failed int
    var errMsg string

    switch g.Mode {
    case "parallel":
        // 【大厂并发考点：sync.WaitGroup 并发等待组】
        // 就像点名：出去几个人干活（Add(1)），回来几个人报到（Done()），等所有人都报到了（Wait()），才宣布收工。
        // 
        // 📌 图解 WaitGroup
        // 主任说：出去3个人干活！ -> WG 内部计数器 = 3
        // 工人A回来：完成！ -> WG 内部计数器 = 2
        // 工人B回来：完成！ -> WG 内部计数器 = 1
        // 工人C回来：完成！ -> WG 内部计数器 = 0 -> 闸门打开，主任继续往下走！
        var mu sync.Mutex
        var wg sync.WaitGroup
        for _, t := range members {
            wg.Add(1)
            task := t // 【非常重要的语法坑】Go for-loop 中的闭包陷阱！必须重新赋值。
            
            if task.RunMode == "daemon" {
                wg.Done()
                continue  // 常驻任务不跟着组掺和。
            }
            
            e.pool.Submit(func() {
                defer wg.Done() // 回来报到。
                defer func() {
                    if r := recover(); r != nil {
                        log.Error().Interface("panic", r).Str("task", task.Name).Msg("group task panic")
                    }
                }()
                e.executeTaskSync(task.ID) // 同步执行
                lastLog, err := e.logRepo.GetLatestTaskLog(task.ID)
                
                // 记下战果。注意，多个人同时回来报战果，得加锁（mu.Lock()）。
                mu.Lock()
                if err == nil && lastLog != nil && lastLog.Status == model.StateSuccess { success++ } else { failed++ }
                mu.Unlock()
            })
        }
        wg.Wait() // 主任在这里抽根烟，等所有小弟回来报到。

    case "sequential":
        // 串行执行，就是最简单的 for 循环，没花招。
        for _, t := range members {
            if t.RunMode == "daemon" {
                continue  
            }
            e.executeTaskSync(t.ID)
            lastLog, err := e.logRepo.GetLatestTaskLog(t.ID)
            if err == nil && lastLog != nil && lastLog.Status == model.StateSuccess { 
                success++ 
            } else { 
                failed++; 
                if lastLog != nil { errMsg = lastLog.ErrorMsg } 
            }
            
            // 一旦有人失败了，直接 break 拔刀斩断，后面的人不用排队了。这叫“失败短路（Fail-Fast）”。
            if lastLog != nil && lastLog.Status == model.StateFailed {
                break
            }
        }

    case "dag":
        // 高阶玩法：有向无环图。
        daemonTaskIDs := make(map[uint]bool)
        for _, t := range members {
            if t.RunMode == "daemon" {
                daemonTaskIDs[t.ID] = true
            }
        }
        // 画依赖图。
        dag := e.buildDAG(members)
        // 拓扑排序就是教你：在这么多依赖里，先穿内裤还是先穿外裤？
		layers := dag.TopologicalSort()
		layerFailed := false

        // 按层级一层一层往下剥着执行。
		for _, layer := range layers {
            // 如果上一层有人搞砸了，这层就不用干了，收工回家！
			if layerFailed {
				break
			}
			
			var mu sync.Mutex
			var wg sync.WaitGroup
			for _, taskID := range layer {
				wg.Add(1)
				tid := taskID
                if daemonTaskIDs[tid] {
                        wg.Done()
                        continue
                }
                
                // 同一层里的活互不依赖，可以并发丢给池子。
				e.pool.Submit(func() {
					defer wg.Done()
					defer func() {
						if r := recover(); r != nil {
							log.Error().Interface("panic", r).Uint("task_id", tid).Msg("group (dag) task panic")
						}
					}()
					
					e.executeTaskSync(tid)
					lastLog, logErr := e.logRepo.GetLatestTaskLog(tid)
					
					mu.Lock()
					if logErr == nil && lastLog != nil && lastLog.Status == model.StateSuccess {
						success++
					} else {
						failed++
						if lastLog != nil {
							errMsg = lastLog.ErrorMsg
						}
						layerFailed = true // 标记这一层翻车了！
					}
					mu.Unlock()
				})
			}
			wg.Wait() // 等这层的人都回来，再看下要不要执行下一层。
		}
	}

    // 总结战绩，记录到数据库！
    endTime := time.Now()
    glog.EndTime = &endTime
    glog.SuccessCount = success
    glog.FailedCount = failed
    glog.ErrorMsg = errMsg
    
    // 定性：部分成功叫 "partial"，全败叫 "failed"，全胜叫 "success"。
    if failed > 0 && success > 0 {
        glog.Status = "partial"
    } else if failed == len(members) && len(members) > 0 {
        glog.Status = "failed"
	} else {
		glog.Status = "success"
	}
	e.logRepo.SaveGroupLog(&glog)
	
	if e.CacheInvalidator != nil {
		e.CacheInvalidator.InvalidateStatsCache() 
	}
}
