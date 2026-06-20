# [IR] 纳米级执行计划: 执行器抗压与异步化改造

## 核心目标
修复 `ants.Pool` 线程池被旁路架空的致命高危问题，并解决 DB 写锁竞争级联阻塞导致性能暴跌的架构隐患。

## 详细改动拆解

### Step 1: 收口线程池与入队反压隔离 (代码行数：~15行)
- **目标文件**: `internal/scheduler/executor.go`
- **目标函数**: `handleTrigger(taskID uint)`
- **修改逻辑**:
  原本的逻辑是直接用 `go func() { e.executeTask(taskID) }()`，导致无限制并发。
  **改为**：
  ```go
  func (e *Executor) handleTrigger(taskID uint) {
      // 外层轻量级 goroutine 仅用于排队，确保不阻塞主调度循环 (Run 内部的 select)
      go func() {
          // 提交到 ants 线程池执行，如果超出 PoolSize 会在这里排队等待
          err := e.pool.Submit(func() {
              defer func() {
                  if r := recover(); r != nil {
                      log.Error().Interface("panic", r).Uint("task_id", taskID).Msg("cron task panic")
                  }
              }()
              e.executeTask(taskID)
          })
          if err != nil {
              log.Error().Err(err).Uint("task_id", taskID).Msg("线程池提交失败 (pool exhausted)")
          }
      }()
  }
  ```
- **架构考量**: 这样既利用了 `ants` 做真正的并发数压制，又避免了阻塞 `executor.Run()` 的 `e.engine.TriggerChan()` 接收，实现完美的背压缓冲。

### Step 2: 任务级日志裁切异步化 (代码行数：~5行)
- **目标文件**: `internal/scheduler/executor.go`
- **目标函数**: `executeTask(taskID uint)`
- **修改逻辑**:
  找到代码：
  ```go
  // 执行单任务数据库日志限额清理
  if execLog.TaskID != nil && e.cfg.Log.MaxLogsPerTask > 0 {
      e.limitTaskLogs(*execLog.TaskID, e.cfg.Log.MaxLogsPerTask)
  }
  ```
  **改为**：
  ```go
  // 异步执行单任务数据库日志限额清理，绝不阻塞当前 Worker 归还给线程池
  if execLog.TaskID != nil && e.cfg.Log.MaxLogsPerTask > 0 {
      taskIDToClean := *execLog.TaskID // 捕获变量副本以策安全
      maxLogs := e.cfg.Log.MaxLogsPerTask
      go e.limitTaskLogs(taskIDToClean, maxLogs)
  }
  ```
- **架构考量**: 当前 `executeTask` 已经被线程池强行限流（最高不超过 CPU*4 或配置上限），所以这批 `go` 的并发量物理上已被封顶，绝不会暴增。此时让 DB 的高开销 Delete 动作脱离核心调度执行流，能提升一倍以上的任务吞吐量。

### Step 3: 更新 MASTER_LOG 索引
- **目标文件**: `docs/sps/MASTER_LOG.md`
- **修改逻辑**: 标记本次 `ARCH-01` 计划生效，并持久化追溯链路。

## 测试验证对齐 (Test-Driven Alignment)
- **测试用例复用**: 利用已有的 `internal/scheduler/executor_quota_test.go` 或 `executor_isolation_test.go`。
- **物理验证点**: 
  - 触发 100 个任务，配置 `ants.Pool` 为 10。
  - 通过注入的监控点，强制核实最大并发执行数为 10（以前会是 100 裸奔）。
  - `limitTaskLogs` 不会导致主执行线程的总耗时延长。
