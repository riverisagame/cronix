// ============================================================
// internal/scheduler/daemon_monitor.go - 常驻进程守护控制器
//
// 【纳米级源码说明书 - 架构篇】
// 类似于 Supervisor（Linux下的进程管家），它负责管理 RunMode == "daemon"（常驻模式）的任务。
//
// 知识点补充（给小白的背景）：
// 什么是“常驻任务”？
// 就像你手机里的微信后台，它必须一直挂着（常驻），不能跑一遍就死掉。如果微信闪退了（异常退出），系统得赶紧把它重新拉起来。
// 本文件里的 DaemonMonitor 就是干这个活的。
//
// 它有几个核心功能：
//   1. 系统刚启动时，自动扫描数据库，把所有标了“常驻”的任务都拉起来运行。
//   2. 如果任务崩溃了，它会自动重启（Keep-Alive）。它还能“退避延迟”（失败了等1秒、下次等2秒、再下次4秒...防止疯狂重启把系统搞死）。
//   3. 连续失败太多次后“熔断”（FATAL），不再盲目重启。
//   4. 支持人为点“启动”或“停止”。
//   5. 提供给前端接口查状态。
//
// ============================================================
package scheduler // package 就像是给这块代码分个类，放进名为 scheduler (调度器) 的抽屉里。

import (
	"context" // 【大厂考点】context (上下文)：就像老板给员工的工牌，老板想开除员工时（取消任务），通过这个工牌能立马通知他停手。
	"sync"    // 【大厂考点】sync (同步)：Go语言超高并发下，必须保证大家不去抢同一个数据。sync 提供了 Mutex (锁)。
	"time"    // 用来处理时间、定时器、延迟等。

	"cronix/internal/domain/model" // 引入我们自己写的领域模型（相当于借用别的抽屉里的工具）

	"github.com/rs/zerolog/log"    // 这是一个第三方的、性能极高的日志打印工具。
)

// ============================================================
// 第一部分：常量与状态定义
// ============================================================

// const 关键字用来定义“常量”，就是定死了永远不会变的值。
const (
    DaemonStopped  = "STOPPED"   // 停止状态
    DaemonStarting = "STARTING"  // 正在启动中
    DaemonRunning  = "RUNNING"   // 正在跑
    DaemonBackoff  = "BACKOFF"   // 刚跑失败了，正在“退避”等待下次重启
    DaemonFatal    = "FATAL"     // 彻底没救了（失败次数超限），被熔断
)

// DaemonState 描述任务对外的运行状态。
// type 关键字用于自定义一个新的类型，比如把结构体命名为 DaemonState。
// struct（结构体）就像是一张表格，里面有各种各样的字段组合在一起。
type DaemonState struct {
	// Status 记录上面定义的那个英文状态词。
	// 后面的 `json:"status"` 叫“结构体标签(Tag)”。
	// 它的作用是：当我们把这个结构体发给网页前端时（变成JSON格式），不要叫 "Status"（首字母大写），请把它重命名为小写的 "status"。
	Status string `json:"status"`
	// RestartCount 连续重启失败了几次（只要成功跑完一次，这个数就会被清零）。
	RestartCount int `json:"restart_count"`
	// MaxRestartAttempts 配置文件里允许它最多重试几次。
	MaxRestartAttempts int `json:"max_restart_attempts"`
	// LastError 如果上一次跑挂了，原因写在这里。omitempty 表示如果这个字段是空的，发给前端时干脆隐藏掉它。
	LastError string `json:"last_error,omitempty"`
	// LastStartTime 最后一次启动的具体时间。这里用了指针（*time.Time），因为指针可以是 nil（空），代表“还从来没启动过”。
	LastStartTime *time.Time `json:"last_start_time,omitempty"`
	// Uptime 已经连续运行了多久（格式化成字符串，比如 "2h15m"）。
	Uptime string `json:"uptime,omitempty"`
}

// daemonTaskState 是专门给内部管理用的结构。它把暴露给外面的信息，和内部的控制句柄绑在了一起。
type daemonTaskState struct {
	// 【Go语法特性】匿名嵌套：把上面的 DaemonState 直接塞进来，相当于继承了它所有的字段。
	DaemonState
	
	// cancel 是一个函数。结合 context 使用，只要调用这个函数，这头常驻的“猛兽”就会被立刻杀死。
	cancel context.CancelFunc
	
	// parentCtx 记住是谁把这个任务拉起来的。如果源头（系统总控）退出了，它也要跟着退出。
	parentCtx context.Context
}

// ============================================================
// 第二部分：接口（Interface）定义
// ============================================================

// interface 也是大厂极其喜欢问的。它叫“接口”。
// 接口就像是一个插座标准（比如两孔插座）。不管你是苹果充电器还是小米充电器，只要你有两个插头，就能插进这个插座。
// 这里定义了 TaskLoader 接口：只要谁能提供 GetTask 和 GetDaemonTasks 方法，谁就能当这里的 TaskLoader，实现解耦。
type TaskLoader interface {
	GetTask(id uint) (*model.Task, error)
	GetDaemonTasks() ([]model.Task, error)
}

// LogQuerier 同理，定义了一个查最新日志的“插座标准”。
type LogQuerier interface {
	GetLatestLog(taskID uint) (*model.ExecutionLog, error)
}

// ============================================================
// 第三部分：核心监控器实现
// ============================================================

// DaemonMonitor 是这个模块的终极老大。它把所有的东西组装起来。
type DaemonMonitor struct {
	taskSvc  TaskLoader   // 找数据库拿任务配置的工具
	execSvc  LogQuerier   // 查历史日志的工具
	executor *Executor    // 真正去干活的机器（执行器）
	
	// 【大厂高频考点】读写锁（RWMutex）
	// 因为可能有很多人同时看状态，同时也可能在更新状态。
	// 读写锁的好处是：可以允许一百个人同时“读”，但只要有一个人要“写”，所有人（包括其他读的人）都必须等他写完。
	mu       sync.RWMutex 
	
	// states 是一张花名册（字典 map）。
	// map[A]B 意思是：通过 A 类型（这里是任务的数字ID），能查到 B 类型（这个任务的内部控制状态）。
	states   map[uint]*daemonTaskState
}

// NewDaemonMonitor 就像是一个“工厂函数”，专门用来生产一个新的 DaemonMonitor 对象。
// 注意方法名前面的 New，这是 Go 语言约定俗成的规矩，表示它是构造函数。
func NewDaemonMonitor(taskSvc TaskLoader, execSvc LogQuerier, executor *Executor) *DaemonMonitor {
	// 返回一个指针（用 & 符号取地址）。指针好比交出这套房子的钥匙，而不是把整个房子搬给对方。这样非常节省系统内存。
	return &DaemonMonitor{
		taskSvc:  taskSvc,
		execSvc:  execSvc,
		executor: executor,
		// map 在 Go 里必须用 make 关键字来初始化，不然它是空的（nil），强行往里塞东西会直接导致程序崩溃（panic）！
		states:   make(map[uint]*daemonTaskState),
	}
}

// Start 方法名开头是大写，说明它是“公开的”（Public），别的包也能调它。
// 这里 (m *DaemonMonitor) 叫“方法接收者”。意思是这个方法是绑定在 DaemonMonitor 身上的，就像人类有“走路”方法一样。
func (m *DaemonMonitor) Start(ctx context.Context) {
	// 在 Go 里，先声明变量，再接收错误是常用的标准写法。
	var tasks []model.Task
	var err error
	
	// 去查所有配置了“常驻”的任务
	tasks, err = m.taskSvc.GetDaemonTasks()
	if err != nil { // 【经典 Go 语言错误处理】如果 err 不等于 nil（空），说明出错了。
		log.Error().Err(err).Msg("daemon monitor: 扫描常驻任务失败")
		return // 直接退出
	}

	log.Info().Int("count", len(tasks)).Msg("daemon monitor: 已扫描到常驻守护任务")

	// for 循环：range 是 Go 特有的遍历方式，会把任务列表(tasks)里的元素一个一个拿出来处理。
	for _, task := range tasks {
		// 调内部方法启动它
		m.startDaemonInternal(ctx, task.ID)
	}
}

// StartDaemon 供外面的人（比如用户在网页上点“启动”按钮）调用
func (m *DaemonMonitor) StartDaemon(taskID uint) {
       // 【锁的实战应用】RLock (Read Lock)：上只读锁。
       // 为什么只读？因为我们只是想看看字典里有没有它，并不打算改字典的内容。
       m.mu.RLock()
       var parentCtx context.Context
       // 从花名册里查。如果查到了，而且它有爹（parentCtx），就用它爹的；否则就用系统的兜底上下文 context.Background()。
       if st, exists := m.states[taskID]; exists && st.parentCtx != nil {
               parentCtx = st.parentCtx
       } else {
               parentCtx = context.Background()
       }
       m.mu.RUnlock() // 【务必牢记】读完之后赶紧解锁，不然别人没法改数据了！

       m.startDaemonInternal(parentCtx, taskID)
}

// startDaemonInternal 核心中的核心！开始派发守护协程。小写开头代表“私有（Private）”。
func (m *DaemonMonitor) startDaemonInternal(parentCtx context.Context, taskID uint) {
	// 先从数据库把任务的具体信息查出来（比如它允许重试几次）
	taskPtr, err := m.taskSvc.GetTask(taskID)
	if err != nil {
		log.Error().Err(err).Uint("task_id", taskID).Msg("daemon monitor: 加载任务失败")
		return
	}
	task := *taskPtr // 星号(*) 意思是把指针指向的内容“解引用”抠出来。

	// 【大厂面试考点：Context 取消树】
	// 基于父 Context，生出一个子 Context（ctx）和一把刀（cancel）。
	// 只要动了这把刀（调 cancel() 函数），所有关联着这个 ctx 的动作都会被立即打断。
	ctx, cancel := context.WithCancel(parentCtx)

       now := time.Now() // 获取系统当前时间
       
       // 【锁的实战应用】Lock：上写锁（独占锁）。
       // 此时我们要往花名册里写数据，如果不独占，大家同时写，系统内存就被写花了。
       m.mu.Lock()
       if old, exists := m.states[taskID]; exists {
       	       // 防止竞态（TOCTOU: Check-Time-To-Use）。如果任务已经在跑了，赶紧解锁退出。
               if old.Status == DaemonRunning || old.Status == DaemonStarting || old.Status == DaemonBackoff {
                       m.mu.Unlock()
                       log.Warn().Uint("task_id", taskID).Str("status", old.Status).Msg("daemon monitor: 任务已在运行中，跳过重复启动")
                       cancel() // 【细节】因为任务已经在跑了，刚刚白造了一把刀(cancel)，为了不浪费内存，赶紧用掉它释放掉。
                       return
               }
               // 如果本来字典里有它，但它停了（STOPPED/FATAL），那我们就把老旧的协程彻底清理掉，防止丧尸进程（Goroutine泄漏）。
               if old.cancel != nil {
                       old.cancel()
               }
       }
       
       // 把新状态登写入花名册
       m.states[taskID] = &daemonTaskState{
               DaemonState: DaemonState{
                       Status:             DaemonStarting,
                       RestartCount:       0,
                       MaxRestartAttempts: task.MaxRestartAttempts,
                       LastStartTime:      &now,
               },
               cancel:    cancel,
               parentCtx: parentCtx,
       }
       m.mu.Unlock() // 写完立刻解锁！

	// 【大厂核心中的核心考点：Goroutine】
	// 前面写一个 "go" 关键字，Go语言就会为你创建一个轻量级线程（协程）去后台独立运行这个任务。
	// 它不会堵住当前的代码往下走，它相当于被丢到另一个次元去干活了。
	go m.runDaemonLoop(ctx, taskID, &task)
}

// runDaemonLoop 守护死循环：也就是在后台次元里不停干活的“苦力”。
func (m *DaemonMonitor) runDaemonLoop(ctx context.Context, taskID uint, task *model.Task) {
	// 【大厂必考：defer 和 recover】
	// defer 就是“临终遗言”。不管这个函数是怎么退出的（正常退出还是报错崩溃），最后一定会执行 defer 里的东西。
	// recover 是“抢救仪”。如果因为除数为0或者空指针导致程序崩溃（panic），recover 能把它救回来，防止整个后台死机。
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Uint("task_id", taskID).Msg("daemon monitor: 守护协程 panic 恢复")
		}
	}()

	// 处理一些默认配置...
	maxAttempts := task.MaxRestartAttempts
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	restartPolicy := task.RestartPolicy
	if restartPolicy == "" {
		restartPolicy = "always"
	}

	restartDelaySec := task.RestartDelaySec
	useFixedDelay := restartDelaySec > 0
	scheduledRestartSec := task.ScheduledRestartSec
	restartCount := 0

	// 死循环开始（守护进程不达目的不罢休）
	for {
		// 【大厂必考：select 机制】
		// select 就像是同时等几个信箱来信。哪个信箱有信，就执行哪一段代码。
		select {
		// `<-ctx.Done()`：一旦有人在前台按了停止按钮（调用了上面的 cancel 刀），这封信就会被收到。
		case <-ctx.Done():
			m.setStatus(taskID, DaemonStopped, "")
			log.Info().Uint("task_id", taskID).Msg("daemon monitor: 守护协程收到停止信号，退出")
			return // 彻底结束这个苦力的生命
		default:
			// 如果没收到停止信号，就继续往下走。
		}

		// 改花名册，把它标记为“正在跑 (RUNNING)”
		now := time.Now()
		m.mu.Lock()
		if st, ok := m.states[taskID]; ok {
			st.Status = DaemonRunning
			st.LastStartTime = &now
			_ = m.execSvc 
		}
		m.mu.Unlock()

		log.Info().Uint("task_id", taskID).Str("task", task.Name).Int("restart_count", restartCount).Msg("daemon monitor: 拉起常驻任务")

		// 让干活机器（executor）去干活。这是阻塞操作，也就是会卡在这里，直到干活结束或被强杀。
		wasScheduled := false
		if scheduledRestartSec > 0 { // 如果配置了定时重启
			execCtx, execCancel := context.WithCancel(ctx)
			// time.AfterFunc 相当于定了个闹钟，闹钟响了就把任务干掉
			timer := time.AfterFunc(time.Duration(scheduledRestartSec)*time.Second, func() {
				execCancel() // 闹钟到点了，痛下杀手
				log.Info().Uint("task_id", taskID).Int("interval_sec", scheduledRestartSec).
					Msg("daemon monitor: 定时重启触发")
			})
			m.executor.ExecuteTaskWithContext(execCtx, taskID)
			timer.Stop() // 如果没到点任务就自己干完了，赶紧把闹钟撤了。
			wasScheduled = execCtx.Err() != nil && ctx.Err() == nil
			execCancel() 
		} else {
			m.executor.ExecuteTaskWithContext(ctx, taskID)
		}

		// 干完活回来，再看一眼是不是有人按了停止按钮？如果有，就别重启了，下班吧。
		select {
		case <-ctx.Done():
			m.setStatus(taskID, DaemonStopped, "")
			log.Info().Uint("task_id", taskID).Msg("daemon monitor: 任务执行期间收到停止信号，退出守护")
			return
		default:
		}

		// 检查一下刚干的活是成功还是失败了
		var latestLog model.ExecutionLog
		latestLogPtr, logErr := m.execSvc.GetLatestLog(taskID)
		if logErr == nil {
			latestLog = *latestLogPtr
		}
		exitSuccess := (logErr == nil && latestLog.Status == "success") || wasScheduled

		// 分析策略：要不要重启它？
		shouldRestart := wasScheduled
		if !shouldRestart {
			switch restartPolicy {
			case "always": // 死了就拉起来
				shouldRestart = true
			case "on-failure": // 只有失败了才拉起来
				shouldRestart = !exitSuccess
			}
		}

		// 判官说：不需要重启了！
		if !shouldRestart {
			m.setStatus(taskID, DaemonStopped, "")
			log.Info().Uint("task_id", taskID).Str("policy", restartPolicy).Msg("daemon monitor: 重启策略判定不需要重启，守护退出")
			return
		}

		// 如果它是成功退出的
		if exitSuccess {
			restartCount = 0 // 失败次数清零，真棒！
			delay := 1 * time.Second
			if useFixedDelay {
				delay = time.Duration(restartDelaySec) * time.Second
			}
			
			// 休息一会（退避）。同时还要保持警戒，如果在休息时有人按了停止按钮，也得能马上退出。
			select {
			case <-ctx.Done():
				m.setStatus(taskID, DaemonStopped, "")
				log.Info().Uint("task_id", taskID).Msg("daemon monitor: 成功退出后退避期间收到停止信号，退出守护")
				return
			case <-time.After(delay): // 这个就是定时休息，时间到了这个信箱才会收到信。
			}
		} else {
			// 如果它是失败退出的
			restartCount++ // 记一笔失败
			lastErr := ""
			if latestLog.ErrorMsg != "" {
				lastErr = latestLog.ErrorMsg
			}
			m.setStatus(taskID, DaemonBackoff, lastErr) // 状态标为退避

			// 如果连续失败太多次了，超过了配置的容忍极限，就熔断它（FATAL）。不再拉起，防止系统资源耗尽。
			if restartPolicy != "always" && restartCount >= maxAttempts {
				m.mu.Lock()
				if st, ok := m.states[taskID]; ok {
					st.Status = DaemonFatal
					st.RestartCount = restartCount
					st.LastError = lastErr
				}
				m.mu.Unlock()
				log.Error().Uint("task_id", taskID).Int("attempts", restartCount).Int("max", maxAttempts).
					Msg("daemon monitor: 连续重启失败超过阈值，进入 FATAL 熔断")
				return
			}

			// 【算法考点：指数退避算法 (Exponential Backoff)】
			// 每次失败后，等待的时间呈指数级增长：1s -> 2s -> 4s -> 8s...
			// 这样可以极大地减轻下游服务死机时的压力（雪崩效应的解决之道）。
			var backoff time.Duration
			if useFixedDelay {
				backoff = time.Duration(restartDelaySec) * time.Second
			} else {
				if restartCount >= 7 { // 2的6次方是64，最多等1分钟左右
					backoff = 60 * time.Second
				} else {
					// 移位运算符 `<<`。 1 << x 就是 2 的 x 次方。
					backoff = time.Duration(1<<uint(restartCount-1)) * time.Second
				}
			}

			// 更新花名册上的重试次数
			m.mu.Lock()
			if st, ok := m.states[taskID]; ok {
				st.RestartCount = restartCount
			}
			m.mu.Unlock()

			log.Warn().Uint("task_id", taskID).Str("error", lastErr).Dur("backoff", backoff).Int("attempt", restartCount).
				Msg("daemon monitor: 执行失败，进入退避等待")

			// 在等待时依然保持接受中止信号的姿势
			select {
			case <-ctx.Done():
				m.setStatus(taskID, DaemonStopped, "")
				log.Info().Uint("task_id", taskID).Msg("daemon monitor: 退避期间收到停止信号，退出守护")
				return
			case <-time.After(backoff):
			}
		}
	}
}

// StopDaemon 用户手动点击了“停止”按钮。
func (m *DaemonMonitor) StopDaemon(taskID uint) {
	m.mu.Lock()
	st, exists := m.states[taskID]
	if !exists {
		m.mu.Unlock()
		log.Warn().Uint("task_id", taskID).Msg("daemon monitor: 停止失败，任务不存在")
		return
	}

	// 只要调用 cancel，上面那张上下文关系网就会瞬间全部崩溃，后台的协程也会乖乖退出。
	if st.cancel != nil {
		st.cancel()
	}
	st.Status = DaemonStopped
	m.mu.Unlock()

	log.Info().Uint("task_id", taskID).Msg("daemon monitor: 已发送停止信号")
}

// GetDaemonState 给 Web 接口提供数据查询用。
func (m *DaemonMonitor) GetDaemonState(taskID uint) (DaemonState, bool) {
	m.mu.RLock() // 读锁，光看不改
	defer m.mu.RUnlock()

	st, exists := m.states[taskID]
	if !exists {
		return DaemonState{}, false // 多返回值是 Go 语言特色。
	}

	result := st.DaemonState
	// 顺便算一下它跑到目前为止已经活了多久了。
	if st.Status == DaemonRunning && st.LastStartTime != nil {
		// time.Since() 计算从那个时间点到现在过去了多久。
		result.Uptime = time.Since(*st.LastStartTime).Truncate(time.Second).String()
	}
	return result, true
}

// GetAllDaemonStates 列出花名册上的所有人。
func (m *DaemonMonitor) GetAllDaemonStates() map[uint]DaemonState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[uint]DaemonState, len(m.states))
	for id, st := range m.states {
		ds := st.DaemonState
		if st.Status == DaemonRunning && st.LastStartTime != nil {
			ds.Uptime = time.Since(*st.LastStartTime).Truncate(time.Second).String()
		}
		result[id] = ds
	}
	return result
}

// setStatus 一个内部小工具箱里的方法：懒人专用的状态修改器，帮你加锁写状态。
func (m *DaemonMonitor) setStatus(taskID uint, status, lastError string) {
	m.mu.Lock()
	defer m.mu.Unlock() // 用 defer 保证哪怕中途写一半报错崩溃了，锁也能被乖乖解开。
	if st, ok := m.states[taskID]; ok {
		st.Status = status
		if lastError != "" {
			st.LastError = lastError
		}
	}
}

// ReloadDaemon 网页上修改了任务配置时，触发热更新！
func (m *DaemonMonitor) ReloadDaemon(task model.Task) {
	m.mu.RLock()
	st, exists := m.states[task.ID]
	m.mu.RUnlock()

	// 1. 如果用户把任务禁用（Enabled=false）了，或者把它从常驻模式改成了定时模式（cron）
	if !task.Enabled || task.RunMode != "daemon" {
		if exists { // 如果之前在花名册里
			log.Info().Uint("task_id", task.ID).Msg("daemon monitor: 检测到任务禁用或模式切换，正在停止常驻任务")
			m.StopDaemon(task.ID) // 杀！
		}
		return
	}

	// 2. 如果任务依旧是 daemon，且它本来就在花名册里活跃着（没死透）
	if exists && (st.Status == DaemonRunning || st.Status == DaemonStarting || st.Status == DaemonBackoff || st.Status == DaemonFatal) {
		log.Info().Uint("task_id", task.ID).Msg("daemon monitor: 检测到配置更新，正在热重载守护任务")
		m.StopDaemon(task.ID) // 先把它干掉
		
		// 新开一个协程，等个 200 毫秒（给子进程一点被杀的喘息时间），然后再复活它！
		go func(id uint) {
			time.Sleep(200 * time.Millisecond)
			m.StartDaemon(id)
		}(task.ID)
	}
}
