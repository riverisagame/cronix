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
    "time"         // 时间处理：定时器、时间计算

    "cronix/internal/config"   // 配置模块
    "cronix/internal/executor"  // 执行模块：实际执行shell、HTTP、清理等任务
    "cronix/internal/model"     // 数据模型

    "github.com/panjf2000/ants/v2"   // ants：高性能的goroutine池（线程池）
    "github.com/rs/zerolog/log"      // zerolog：结构化日志库
    "gorm.io/gorm"                   // GORM：数据库操作
)

// Executor 是任务执行器，负责真正运行任务
// 它从调度引擎接收触发信号，然后在线程池中执行
type Executor struct {
    db     *gorm.DB       // 数据库连接：查询任务、保存执行日志
    pool   *ants.Pool     // ants线程池：控制同时运行的任务数量，防止资源耗尽
    cfg    *config.Config // 系统配置：线程池大小、输出截断大小等
    engine *Engine        // 调度引擎：从这里接收"该执行任务了"的信号
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
    return &Executor{db: db, pool: pool, cfg: cfg, engine: engine}, nil
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
        var count int64
        e.db.Model(&model.ExecutionLog{}).Count(&count)         // 统计当前日志总条数
        if count > int64(maxRecords) {                          // 如果超过了最大限制
            excess := count - int64(maxRecords)                 // 计算超出多少条
            // 删除ID最小的那部分（也就是最旧的记录）
            result := e.db.Where("id IN (?)",
                e.db.Model(&model.ExecutionLog{}).Select("id").Order("id ASC").Limit(int(excess)), // 子查询：找出最旧的excess条记录的ID
            ).Delete(&model.ExecutionLog{})
            if result.Error != nil {
                log.Warn().Err(result.Error).Msg("log cleanup (max_records) failed")
            } else if result.RowsAffected > 0 {
                log.Info().Int64("deleted", result.RowsAffected).Int("max_records", maxRecords).Int64("was", count).Msg("log cleanup (max_records)")
            }
        }
    }
}

// RunTaskNow 手动触发一个任务立即执行（包含依赖解析）
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
        // defer + recover 是Go的异常保护机制：如果发生了panic（程序崩溃），这里能兜底
        defer func() {
            if r := recover(); r != nil {                       // recover() 捕获panic
                log.Error().Interface("panic", r).Uint("task_id", taskID).Msg("manual task panic")
            }
        }()

        // 第一步：查询所有启用的任务，构建依赖图（DAG）
        var tasks []model.Task
        if err := e.db.Where("enabled = ?", true).Find(&tasks).Error; err != nil { // 查询失败
            e.executeTask(taskID)                               // 退化为单独执行这个任务
            return
        }
        dag := e.buildDAG(tasks)                                // 构建依赖图
        layers := dag.TopologicalSort()                          // 拓扑排序：按依赖关系分层

        // 第二步：找到触发任务在哪个层级
        targetLayer := -1
        for i, layer := range layers {                          // 遍历每一层
            for _, nid := range layer {                         // 遍历这一层里的所有节点
                if nid == taskID {                              // 找到了！
                    targetLayer = i
                    break
                }
            }
            if targetLayer >= 0 {
                break
            }
        }

        // 如果任务不在图中（没有依赖关系），直接执行
        if targetLayer < 0 {
            e.executeTask(taskID)
            return
        }

        // 第三步：从第0层开始，逐层执行到目标层
        // 同一层里的任务可以同时执行（因为它们之间没有依赖关系）
        for i := 0; i <= targetLayer; i++ {
            var wg sync.WaitGroup                                // WaitGroup：等待一组任务完成
            for _, nodeID := range layers[i] {                  // 遍历这一层的所有节点
                wg.Add(1)                                       // 计数器+1，表示有一个任务要执行
                nID := nodeID                                   // 复制变量，避免闭包引用循环变量的问题
                e.pool.Submit(func() {                          // 把任务提交到线程池
                    defer wg.Done()                             // 任务完成后，计数器-1
                    defer func() {                              // 异常保护
                        if r := recover(); r != nil {
                            log.Error().Interface("panic", r).Uint("task_id", nID).Msg("task panic")
                        }
                    }()
                    e.executeTask(nID)                          // 真正执行任务
                })
            }
            wg.Wait()                                           // 等待这一层所有任务执行完，再进入下一层
        }
    }()
}

// handleTrigger 处理定时器触发的任务（走完整DAG依赖解析）
func (e *Executor) handleTrigger(taskID uint) {
    // 查询所有启用的任务
    var tasks []model.Task
    if err := e.db.Where("enabled = ?", true).Find(&tasks).Error; err != nil {
        log.Error().Err(err).Msg("fetch tasks for DAG")
        return
    }
    dag := e.buildDAG(tasks)                                    // 构建依赖图
    layers := dag.TopologicalSort()                              // 按依赖分层排序

    // 逐层执行
    for _, layer := range layers {
        var wg sync.WaitGroup
        for _, nodeID := range layer {
            wg.Add(1)
            nID := nodeID
            e.pool.Submit(func() {
                defer wg.Done()
                defer func() {
                    if r := recover(); r != nil {
                        log.Error().Interface("panic", r).Uint("task_id", nID).Msg("task panic recovered")
                    }
                }()
                e.executeTask(nID)
            })
        }
        wg.Wait()
    }
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
    for attempt := 0; attempt <= maxRetries; attempt++ {         // 从第0次到第maxRetries次（最多maxRetries+1次尝试）
        if attempt > 0 {                                         // 如果这不是第一次尝试
            log.Info().Str("task", task.Name).Int("attempt", attempt).Int("max", maxRetries).Msg("retrying")
            execLog.RetryAttempt = attempt                        // 记录重试次数
            time.Sleep(time.Duration(task.RetryIntervalSec) * time.Second) // 等待重试间隔
        }
        execLog.Status = "running"                                // 重置状态
        execLog.ErrorMsg = ""                                     // 清空错误信息
        e.runTaskByType(&task, &execLog)                          // 根据任务类型执行
        if execLog.Status == "success" {                          // 执行成功，不再重试
            break
        }
    }

    // 第四步：发送通知（如果需要的话）
    e.notifyTaskResult(&task, &execLog)
}

// runTaskByType 根据任务的类型（shell/http/cleanup/healthcheck）执行实际操作
// 参数 task：任务对象
// 参数 execLog：执行日志对象（会被修改）
func (e *Executor) runTaskByType(task *model.Task, execLog *model.ExecutionLog) {
    ctx := context.Background()                                  // 创建一个空的上下文
    now := time.Now()
    execLog.EndTime = &now                                       // 记录结束时间
    truncateKB := e.cfg.Executor.OutputTruncateKB                // 输出截断大小（KB）

    switch task.TaskType {                                       // 根据不同任务类型，走不同分支
    case "shell":
        // Shell任务：在操作系统命令行中执行一条命令
        result := executor.ExecuteShell(ctx, task.Command, task.WorkDir, task.TimeoutSec)
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
            task.TimeoutSec, e.cfg.CircuitBreaker.FailureThreshold,
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
        result := executor.ExecuteHealthCheck(ctx, task.HTTPURL, task.TimeoutSec)
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

    e.db.Save(execLog)                                           // 把执行结果保存到数据库
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
func (e *Executor) notifyTaskResult(task *model.Task, execLog *model.ExecutionLog) {
    var notifies []model.NotifyConfig                            // 查询这个任务的所有通知配置
    e.db.Where("task_id = ?", task.ID).Find(&notifies)
    for _, n := range notifies {                                 // 遍历每条通知配置
        // 判断是否需要通知：成功且配置了成功通知，或者失败且配置了失败通知
        shouldNotify := (execLog.Status == "success" && n.OnSuccess) ||
            (execLog.Status == "failed" && n.OnFailure)
        if shouldNotify {
            log.Info().Str("task", task.Name).Str("type", n.NotifyType).Msg("would notify")
            // 注意：当前版本只在日志中记录"会通知"，实际通知功能见notify模块
        }
    }
}

// Shutdown 关闭执行器，释放线程池资源
func (e *Executor) Shutdown() {
    e.pool.Release()                                             // 释放线程池
}
