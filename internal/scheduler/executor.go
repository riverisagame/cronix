// ============================================================
// internal/scheduler/executor.go - 任务执行协调器
// 负责接收触发信号、构建依赖图、在线程池中执行任务、记录日志
// ============================================================
package scheduler

import (
	"context"      // 上下文：控制goroutine的生命周期
	"fmt"          // 格式化输出：拼接错误信息
	"runtime"      // 运行时信息：获取CPU核心数
	"sync"         // 并发控制：WaitGroup等待一组任务完成
	"sync/atomic"  // 原子操作：用于日志写入计数器
	"time"         // 时间处理：定时器、时间计算

	"cronix/internal/config"   // 配置模块
	"cronix/internal/executor"  // 执行模块：实际执行shell、HTTP、清理等任务
	"cronix/internal/model"     // 数据模型
	"cronix/internal/notify"    // 通知模块：Webhook/Email通知发送

	"github.com/panjf2000/ants/v2"   // ants：高性能的goroutine池（线程池）
	"github.com/rs/zerolog/log"      // zerolog：结构化日志库
	"gorm.io/gorm"                   // GORM：数据库操作
)

// StatsCacheInvalidator defines the interface to invalidate dashboard statistics cache.
// This interface avoids circular dependency between scheduler and service packages.
// @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
type StatsCacheInvalidator interface {
	InvalidateStatsCache()
}

// Executor 是任务执行器，负责真正运行任务
// 它从调度引擎接收触发信号，然后在线程池中执行
type Executor struct {
	db               *gorm.DB               // 数据库连接：查询任务、保存执行日志
	pool             *ants.Pool             // ants线程池：控制同时运行的任务数量，防止资源耗尽
	cfg              *config.Config         // 系统配置：线程池大小、输出截断大小等
	engine           *Engine                // 调度引擎：从这里接收"该执行任务了"的信号
	CacheInvalidator StatsCacheInvalidator // 缓存失效接口
	Notifier         *notify.Notifier       // 通知发送器：Webhook/Email通知（可选，为 nil 时不发通知）
	// @Ref: architect_review_20260609.md P1-3 | @Date: 2026-06-09
	logWriteCounter  uint64                 // 原子计数器：控制全局日志主动清理的时机
}

// NewExecutor 创建任务执行器
// 参数 db: 数据库连接
// 参数 cfg: 系统配置
// 参数 engine: 调度引擎
// 返回值：初始化好的Executor，可能返回错误（如果创建线程池失败）
func NewExecutor(db *gorm.DB, cfg *config.Config, engine *Engine) (*Executor, error) {
    poolSize := cfg.Executor.PoolSize                          // 从配置中读取线程池大小
    if poolSize <= 0 {                                         // 如果没配置或配错了（<=0）
        poolSize = runtime.NumCPU() * 4                        // 自动计算：CPU核心数 × 4
    }
    pool, err := ants.NewPool(poolSize)                         // 创建ants线程池
    if err != nil {
        return nil, fmt.Errorf("create ants pool: %w", err)    // %w 把原始错误包装起来，方便上层查看
    }
    exec := &Executor{db: db, pool: pool, cfg: cfg, engine: engine}
    // 注册组定时回调：cron到点后加载组成员并执行
    engine.SetGroupTrigger(func(groupID uint) {
        var g model.TaskGroup
        if err := db.First(&g, groupID).Error; err != nil {
            log.Error().Err(err).Uint("group_id", groupID).Msg("group trigger: group not found")
            return
        }
        var members []model.Task
        db.Where("group_id = ?", groupID).Order("sort_order ASC, id ASC").Find(&members)
        if len(members) == 0 {
            log.Warn().Str("group", g.Name).Msg("group trigger: group has no members")
            return
        }
        exec.RunGroup(&g, members, "cron")
    })
    return exec, nil
}

// Run 是执行器的主循环，会一直运行直到收到取消信号
// 参数 ctx：上下文，用于优雅关闭
func (e *Executor) Run(ctx context.Context) {
    // 创建一个每小时触发一次的定时器，用于定期清理旧日志
    cleanupTicker := time.NewTicker(1 * time.Hour)              // NewTicker 返回一个每隔1小时发信号的通道
    defer cleanupTicker.Stop()                                  // 函数结束时停止定时器

    for {                                                       // 无限循环，持续监听各种事件
        select {                                                // select：同时等待多个通道
        case <-ctx.Done():                                      // 如果上下文被取消（程序要关闭了）
            return                                              // 退出循环
        case taskID := <-e.engine.TriggerChan():                // 收到调度引擎发来的任务触发信号
            e.handleTrigger(taskID)                             // 处理这个任务触发
        case <-cleanupTicker.C:                                 // 每小时定时器到点了
            e.cleanupOldLogs()                                  // 清理过期的日志记录
        }
    }
}

// cleanupOldLogs 清理过期的执行日志（按保留天数和最多记录数两种策略）
func (e *Executor) cleanupOldLogs() {
    retentionDays := e.cfg.Log.RetentionDays                    // 配置的日志保留天数
    maxRecords := e.cfg.Log.MaxRecords                          // 配置的日志最多保留条数

    // 策略一：按天数清理——删除创建时间超过retentionDays天的日志
    if retentionDays > 0 {
        cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour) // 计算截止时间：现在减去N天
        result := e.db.Where("created_at < ?", cutoff).Delete(&model.ExecutionLog{}) // 删除早于截止时间的记录
        if result.Error != nil {
            log.Warn().Err(result.Error).Msg("log cleanup (retention) failed")      // 清理失败，记录警告
        } else if result.RowsAffected > 0 {                                          // 如果有记录被删除
            log.Info().Int64("deleted", result.RowsAffected).Int("retention_days", retentionDays).Msg("log cleanup (retention)")
        }
    }

    // 策略二：按条数清理——如果日志总数超过maxRecords，删除最旧的那些
    if maxRecords > 0 {
        e.deleteOldestBatch(&model.ExecutionLog{}, "execution_logs", maxRecords)
    }

    // Group execution log cleanup
    if retentionDays > 0 {
        cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
        result := e.db.Where("created_at < ?", cutoff).Delete(&model.GroupExecutionLog{})
        if result.Error != nil {
            log.Warn().Err(result.Error).Msg("group log cleanup (retention) failed")
        } else if result.RowsAffected > 0 {
            log.Info().Int64("deleted", result.RowsAffected).Int("retention_days", retentionDays).Msg("group log cleanup (retention)")
        }
    }
    if maxRecords > 0 {
        e.deleteOldestBatch(&model.GroupExecutionLog{}, "group_execution_logs", maxRecords)
    }
}

// deleteOldestBatch deletes excess records in batches of 1000 to avoid
// long transactions and large subquery expansion on big tables.
func (e *Executor) deleteOldestBatch(model interface{}, tableName string, maxRecords int) {
    var count int64
    e.db.Model(model).Count(&count)
    if count <= int64(maxRecords) {
        return
    }
    excess := count - int64(maxRecords)
    batchSize := int64(1000)
    for excess > 0 {
        n := batchSize
        if excess < n {
            n = excess
        }
        result := e.db.Where("id IN (?)",
            e.db.Model(model).Select("id").Order("id ASC").Limit(int(n)),
        ).Delete(model)
        if result.Error != nil {
            log.Warn().Err(result.Error).Str("table", tableName).Msg("batch cleanup failed")
            break
        }
        if result.RowsAffected == 0 {
            break
        }
        excess -= result.RowsAffected
    }
}

// RunTaskNow 手动触发一个任务立即执行
// 参数 taskID：要手动运行的任务ID
func (e *Executor) RunTaskNow(taskID uint) {
    var task model.Task
    if err := e.db.First(&task, taskID).Error; err != nil {     // 查询任务是否存在
        log.Error().Err(err).Uint("task_id", taskID).Msg("manual trigger: task not found")
        return
    }
    log.Info().Str("task", task.Name).Uint("id", task.ID).Msg("manual trigger") // 记录手动触发日志

    // 在后台线程中执行，不阻塞HTTP响应
    go func() {
        defer func() {
            if r := recover(); r != nil {                       // recover() 捕获panic
                log.Error().Interface("panic", r).Uint("task_id", taskID).Msg("manual task panic")
            }
        }()
        
        // 仅执行当前被触发的任务，不牵连其他任务
        e.executeTask(taskID)
    }()
}

// handleTrigger 处理定时器触发的任务
func (e *Executor) handleTrigger(taskID uint) {
	// 外层轻量级协程排队，避免阻塞 Run 内部 select
	go func() {
		// 提交给 ants.Pool 执行，利用线程池封顶并发防击穿
		err := e.pool.Submit(func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Uint("task_id", taskID).Msg("cron task panic")
				}
			}()
			e.executeTask(taskID)
		})
		if err != nil {
			log.Error().Err(err).Uint("task_id", taskID).Msg("Failed to submit task to pool (pool exhausted)")
		}
	}()
}

// buildDAG 根据数据库中的任务和依赖关系构建依赖图
// 参数 tasks：所有启用的任务列表
// 返回值：构建好的DAG有向无环图
func (e *Executor) buildDAG(tasks []model.Task) *DAG {
    dag := NewDAG()                                             // 创建空的有向无环图
    taskMap := make(map[uint]model.Task)                        // 建立ID到任务的映射，方便快速查找
    for _, t := range tasks {
        taskMap[t.ID] = t
        dag.AddNode(t.ID)                                       // 把每个任务添加为图中的一个节点
    }
    var deps []model.TaskDep                                    // 查询所有依赖关系
    e.db.Find(&deps)
    for _, dep := range deps {                                  // 遍历每条依赖关系
        if _, ok := taskMap[dep.TaskID]; !ok {                 // 依赖方不在任务列表中，跳过
            continue
        }
        if _, ok := taskMap[dep.DependsOnID]; !ok {            // 被依赖方不在任务列表中，跳过
            continue
        }
        dag.AddEdge(dep.DependsOnID, dep.TaskID)               // 添加边：被依赖方 → 依赖方（先执行DependsOn，再执行Task）
    }
    return dag
}

// executeTask 执行单个任务（包含重试逻辑和结果通知）
// 参数 taskID：要执行的任务ID
func (e *Executor) executeTask(taskID uint) {
    // 第一步：从数据库查询任务详情
    var task model.Task
    if err := e.db.First(&task, taskID).Error; err != nil {
        log.Error().Err(err).Uint("task_id", taskID).Msg("fetch task failed")
        return
    }

    // 第二步：创建一条执行日志记录
    now := time.Now()
    execLog := model.ExecutionLog{                               // 初始化日志结构体
        TaskID:      &task.ID,                                   // 任务ID（指针类型，可以为空）
        TaskName:    task.Name,                                  // 任务名
        CronExpr:    task.CronExpr,                              // Cron表达式
        Status:      "running",                                  // 初始状态：运行中
        TriggerType: "cron",                                     // 触发类型：定时触发
        StartTime:   now,                                        // 开始时间
    }
    e.db.Create(&execLog)                                        // 插入数据库
    log.Info().Str("task", task.Name).Uint("id", task.ID).Msg("executing task")

    // 第三步：执行任务，支持重试
    maxRetries := task.RetryCount                                // 配置的重试次数
    if maxRetries < 0 {                                          // 负数没有意义，视为0
        maxRetries = 0
    }

    // 超时上限：取 min(任务设置, 全局上限)
    timeout := task.TimeoutSec
    if maxTO := e.cfg.Executor.MaxTimeoutSec; maxTO > 0 && timeout > maxTO {
        timeout = maxTO
        log.Warn().Str("task", task.Name).Int("requested", task.TimeoutSec).Int("capped", maxTO).Msg("任务超时超过全局上限，已限制")
    }
    for attempt := 0; attempt <= maxRetries; attempt++ {         // 从第0次到第maxRetries次（最多maxRetries+1次尝试）
        if attempt > 0 {                                         // 如果这不是第一次尝试
            log.Info().Str("task", task.Name).Int("attempt", attempt).Int("max", maxRetries).Msg("retrying")
            execLog.RetryAttempt = attempt                        // 记录重试次数
            time.Sleep(time.Duration(task.RetryIntervalSec) * time.Second) // 等待重试间隔
        }
        execLog.Status = "running"                                // 重置状态
        execLog.ErrorMsg = ""                                     // 清空错误信息
        e.runTaskByType(&task, &execLog, timeout)                     // 根据任务类型执行
        if execLog.Status == "success" {                          // 执行成功，不再重试
            break
        }
    }

    // 第四步：发送通知（如果需要的话）
    e.notifyTaskResult(&task, &execLog)
}

// ExecuteTaskWithContext 带上下文的任务执行（供 DaemonMonitor 调用）
// 当 ctx 被取消时，底层 ExecuteShell 会收到取消信号并强杀进程组
// @Ref: docs/sps/plans/20260605_daemon_supervisor_feature.md | @Date: 2026-06-05
func (e *Executor) ExecuteTaskWithContext(ctx context.Context, taskID uint) {
    // 第一步：从数据库查询任务详情
    var task model.Task
    if err := e.db.First(&task, taskID).Error; err != nil {
        log.Error().Err(err).Uint("task_id", taskID).Msg("fetch task failed")
        return
    }

    // 第二步：创建一条执行日志记录
    now := time.Now()
    execLog := model.ExecutionLog{
        TaskID:      &task.ID,
        TaskName:    task.Name,
        CronExpr:    task.CronExpr,
        Status:      "running",
        TriggerType: "daemon",
        StartTime:   now,
    }
    e.db.Create(&execLog)
    log.Info().Str("task", task.Name).Uint("id", task.ID).Msg("daemon executing task")

    // 第三步：执行任务（常驻任务不重试，由 DaemonMonitor 统一管理重启策略）
    // Daemon 任务不使用 TimeoutSec — 生命周期由 DaemonMonitor ctx 取消管理
    // 否则 DB 默认值 300s 会导致 daemon 每 5 分钟被超时强杀
    e.runTaskByTypeCtx(ctx, &task, &execLog, 0)


    // 第四步：发送通知
    e.notifyTaskResult(&task, &execLog)
}

// runTaskByType 根据任务的类型（shell/http/cleanup/healthcheck）执行实际操作
// 参数 task：任务对象
// 参数 execLog：执行日志对象（会被修改）
func (e *Executor) runTaskByType(task *model.Task, execLog *model.ExecutionLog, timeoutSec int) {
    e.runTaskByTypeCtx(context.Background(), task, execLog, timeoutSec)
}

// runTaskByTypeCtx 带上下文版本的任务执行分发，可以通过 ctx 取消来即时强杀子进程
// @Ref: docs/sps/plans/20260605_daemon_supervisor_feature.md | @Date: 2026-06-05
func (e *Executor) runTaskByTypeCtx(ctx context.Context, task *model.Task, execLog *model.ExecutionLog, timeoutSec int) {
	truncateKB := e.cfg.Executor.OutputTruncateKB                // 输出截断大小（KB）

    switch task.TaskType {                                       // 根据不同任务类型，走不同分支
    case "shell":
        // Shell任务：在操作系统命令行中执行一条命令
        result := executor.ExecuteShell(ctx, task.Command, task.WorkDir, timeoutSec, task.RunAs, task.ID)
        if result.Error != nil {
            execLog.Status = "failed"
            execLog.ErrorMsg = result.Error.Error()
        } else {
            execLog.Status = "success"
        }
        execLog.ExitCode = &result.ExitCode                      // 记录命令的退出码（0=成功）
        execLog.Output = truncate(result.Output, truncateKB)     // 截断过长的输出

    case "http":
        // HTTP任务：发送一个HTTP请求到指定URL
        result := executor.ExecuteHTTP(ctx, task.HTTPMethod, task.HTTPURL,
            task.HTTPHeaders, task.HTTPBody, task.HTTPAuthType, task.HTTPAuthConfig,
            timeoutSec, e.cfg.CircuitBreaker.FailureThreshold,
            e.cfg.CircuitBreaker.CooldownSeconds)
        if result.Error != nil {
            execLog.Status = "failed"
            execLog.ErrorMsg = result.Error.Error()
        } else if result.StatusCode >= 400 {                     // HTTP状态码>=400表示请求失败
            execLog.Status = "failed"
            execLog.ErrorMsg = fmt.Sprintf("HTTP %d", result.StatusCode)
        } else {
            execLog.Status = "success"
        }
        code := result.StatusCode
        execLog.ExitCode = &code
        execLog.Output = truncate(result.Body, truncateKB)

    case "cleanup":
        // 清理任务：删除指定目录下符合条件的旧文件
        result := executor.ExecuteCleanup(ctx, task.Command)     // task.Command存放的是JSON配置
        if result.Error != nil {
            execLog.Status = "failed"
            execLog.ErrorMsg = result.Error.Error()
        } else {
            execLog.Status = "success"
            execLog.Output = fmt.Sprintf("Deleted %d files", result.DeletedCount) // 输出删除了多少个文件
        }
        code := 0
        execLog.ExitCode = &code

    case "healthcheck":
        // 健康检查任务：访问一个URL，检查服务是否正常
        result := executor.ExecuteHealthCheck(ctx, task.HTTPURL, timeoutSec)
        if result.Error != nil {
            execLog.Status = "failed"
            execLog.ErrorMsg = result.Error.Error()
        } else {
            execLog.Status = "success"
        }
        code := result.StatusCode
        execLog.ExitCode = &code
        execLog.Output = fmt.Sprintf("Status: %d", result.StatusCode) // 输出HTTP状态码

	default:
		// 未知的任务类型，标记为失败
		execLog.Status = "failed"
		execLog.ErrorMsg = fmt.Sprintf("unknown task type: %s", task.TaskType)
	}

	now := time.Now()
	execLog.EndTime = &now                                       // 记录结束时间

	// @Ref: docs/sps/plans/20260605_metrics_plan.md | @Date: 2026-06-05
	duration := now.Sub(execLog.StartTime).Milliseconds()
	GlobalMetricsRegistry.RecordExecution(duration, execLog.Status == "success")

	e.db.Save(execLog)                                           // 把执行结果保存到数据库
    
    // 异步执行单任务数据库日志限额清理，绝不阻塞当前 Worker 归还给线程池
    if execLog.TaskID != nil && e.cfg.Log.MaxLogsPerTask > 0 {
        taskIDToClean := *execLog.TaskID
        maxLogs := e.cfg.Log.MaxLogsPerTask
        go e.limitTaskLogs(taskIDToClean, maxLogs)
    }

    // 原子计数器递增，每 500 次写入异步触发一次全局日志清理
    atomic.AddUint64(&e.logWriteCounter, 1)
    if atomic.LoadUint64(&e.logWriteCounter)%500 == 0 {
        go e.cleanupOldLogs()
    }

    if e.CacheInvalidator != nil {
        e.CacheInvalidator.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
    }
}

// truncate 截断字符串，防止输出太长
// 参数 s：原始字符串
// 参数 maxKB：最大保留多少KB
// 返回值：截断后的字符串
func truncate(s string, maxKB int) string {
    maxBytes := maxKB * 1024                                     // 把KB转成字节数
    if len(s) > maxBytes {                                       // 如果字符串长度超过限制
        return s[:maxBytes] + "\n... (truncated)"                // 截断并加提示
    }
    return s
}

// notifyTaskResult 根据任务执行结果发送通知
// @Ref: architect_review_20260609.md P1-3 | @Date: 2026-06-09
func (e *Executor) notifyTaskResult(task *model.Task, execLog *model.ExecutionLog) {
    var notifies []model.NotifyConfig                            // 查询这个任务的所有通知配置
    e.db.Where("task_id = ?", task.ID).Find(&notifies)
    for _, n := range notifies {                                 // 遍历每条通知配置
        // 判断是否需要通知：成功且配置了成功通知，或者失败且配置了失败通知
        shouldNotify := (execLog.Status == "success" && n.OnSuccess) ||
            (execLog.Status == "failed" && n.OnFailure)
        if !shouldNotify {
            continue
        }
        if e.Notifier != nil {
            // 真正发送通知事件（非阵塞）
            // 用 select+default 防止 channel 满时阻塞执行链
            event := notify.NotifyEvent{
                TaskName: task.Name,
                Status:   execLog.Status,
                Config:   n,
            }
            select {
            case e.Notifier.NotifyChan() <- event:
                log.Debug().Str("task", task.Name).Str("type", n.NotifyType).Msg("通知事件已入队")
            default:
                log.Warn().Str("task", task.Name).Str("type", n.NotifyType).Msg("通知队列已满，本条通知被丢弃")
            }
        } else {
            log.Info().Str("task", task.Name).Str("type", n.NotifyType).Msg("通知器未配置，跳过通知")
        }
    }
}

// Shutdown 关闭执行器，释放线程池资源
func (e *Executor) Shutdown() {
    e.pool.Release()                                             // 释放线程池
}

// RunGroup executes all tasks in a group according to the group's mode.
func (e *Executor) RunGroup(g *model.TaskGroup, members []model.Task, triggerType string) {
    now := time.Now()
    glog := model.GroupExecutionLog{
        GroupID:     g.ID,
        GroupName:   g.Name,
        Mode:        g.Mode,
        TriggerType: triggerType,
        MemberCount: len(members),
        Status:      "running",
        StartTime:   now,
    }
    e.db.Create(&glog)
    log.Info().Str("group", g.Name).Str("mode", g.Mode).Int("members", len(members)).Msg("running group")

    var success, failed int
    var errMsg string

    switch g.Mode {
    case "parallel":
        var mu sync.Mutex
        var wg sync.WaitGroup
        for _, t := range members {
            wg.Add(1)
            task := t
            e.pool.Submit(func() {
                defer wg.Done()
                defer func() {
                    if r := recover(); r != nil {
                        log.Error().Interface("panic", r).Str("task", task.Name).Msg("group task panic")
                    }
                }()
                e.executeTask(task.ID)
                var lastLog model.ExecutionLog
                e.db.Where("task_id = ?", task.ID).Order("id DESC").First(&lastLog)
                mu.Lock()
                if lastLog.Status == "success" { success++ } else { failed++ }
                mu.Unlock()
            })
        }
        wg.Wait()
        log.Info().Str("group", g.Name).Msg("group (parallel) completed")

    case "sequential":
        for _, t := range members {
            e.executeTask(t.ID)
            var lastLog model.ExecutionLog
            e.db.Where("task_id = ?", t.ID).Order("id DESC").First(&lastLog)
            if lastLog.Status == "success" { success++ } else { failed++; errMsg = lastLog.ErrorMsg }
            if lastLog.Status == "failed" {
                log.Warn().Str("group", g.Name).Str("task", t.Name).Msg("group (sequential) stopped due to failure")
                break
            }
        }
        log.Info().Str("group", g.Name).Msg("group (sequential) completed")
        	case "dag":
		dag := e.buildDAG(members)
		layers := dag.TopologicalSort()
		layerFailed := false

		for i, layer := range layers {
			if layerFailed {
				log.Warn().Str("group", g.Name).Int("layer", i).Msg("group (dag) stopped due to previous layer failure")
				break
			}
			
			var mu sync.Mutex
			var wg sync.WaitGroup
			for _, taskID := range layer {
				wg.Add(1)
				tid := taskID
				e.pool.Submit(func() {
					defer wg.Done()
					defer func() {
						if r := recover(); r != nil {
							log.Error().Interface("panic", r).Uint("task_id", tid).Msg("group (dag) task panic")
						}
					}()
					
					e.executeTask(tid)
					var lastLog model.ExecutionLog
					e.db.Where("task_id = ?", tid).Order("id DESC").First(&lastLog)
					
					mu.Lock()
					if lastLog.Status == "success" {
						success++
					} else {
						failed++
						errMsg = lastLog.ErrorMsg
						layerFailed = true
					}
					mu.Unlock()
				})
			}
			wg.Wait()
		}
		log.Info().Str("group", g.Name).Msg("group (dag) completed")
	}

    // Update group execution log
    endTime := time.Now()
    glog.EndTime = &endTime
    glog.SuccessCount = success
    glog.FailedCount = failed
    glog.ErrorMsg = errMsg
    if failed > 0 && success > 0 {
        glog.Status = "partial"
    } else if failed == len(members) && len(members) > 0 {
        glog.Status = "failed"
	} else {
		glog.Status = "success"
	}
	e.db.Save(&glog)
	if e.CacheInvalidator != nil {
		e.CacheInvalidator.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
}

// limitTaskLogs 清理单个任务的超额日志记录，防止数据库爆满和“劣币驱逐良币”
func (e *Executor) limitTaskLogs(taskID uint, maxLogs int) {
	var count int64
	e.db.Model(&model.ExecutionLog{}).Where("task_id = ?", taskID).Count(&count)
	if count <= int64(maxLogs) {
		return
	}
	excess := count - int64(maxLogs)

	// 为了兼容 SQLite / MySQL 等不同数据库，不能直接在 DELETE 中使用 LIMIT。
	// 我们先查询出最旧的 excess 个 id，然后再批量删除它们。
	var ids []uint
	err := e.db.Model(&model.ExecutionLog{}).
		Select("id").
		Where("task_id = ?", taskID).
		Order("id ASC").
		Limit(int(excess)).
		Pluck("id", &ids).Error
	if err != nil {
		log.Warn().Err(err).Uint("task_id", taskID).Msg("failed to query oldest log ids for limitTaskLogs")
		return
	}

	if len(ids) > 0 {
		result := e.db.Where("id IN (?)", ids).Delete(&model.ExecutionLog{})
		if result.Error != nil {
			log.Warn().Err(result.Error).Uint("task_id", taskID).Msg("failed to prune oldest log ids in limitTaskLogs")
		} else {
			log.Debug().Int64("deleted", result.RowsAffected).Uint("task_id", taskID).Msg("limitTaskLogs pruned excess logs")
		}
	}
}
