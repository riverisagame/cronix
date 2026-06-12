# 架构加固三路改造计划 (P2 → P0 → P1)

## 背景
基于 2026-06-12 架构评审发现的三个致命弱点，按依赖关系排序执行：
- P2（接口抽象）是 P0/P1 的基座
- P0（超时阻断）依赖 P2 的接口
- P1（异步写入）依赖 P2 的接口

---

## Phase 1: P2 — LogRepository 接口抽象

### 目标
将 `executor.go` 中所有对 `e.db` 的执行日志相关操作，提取为 `LogRepository` 接口，GORM 实现作为默认后端。

### Step 1.1: 定义接口 [NEW]
- **文件**: `internal/scheduler/log_repository.go`
- **接口名**: `LogRepository`
- **方法签名**:
```go
type LogRepository interface {
    CreateExecutionLog(log *model.ExecutionLog) error
    SaveExecutionLog(log *model.ExecutionLog) error
    CountRunningLogs(taskID uint) (int64, error)
    GetLatestTaskLog(taskID uint) (*model.ExecutionLog, error)
    CleanupOrphanedLogs(now time.Time) error
    DeleteLogsBefore(cutoff time.Time) (int64, error)
    DeleteExcessLogs(maxRecords int) error
    DeleteExcessTaskLogs(taskID uint, maxLogs int) error
    // 组执行日志
    CreateGroupLog(log *model.GroupExecutionLog) error
    SaveGroupLog(log *model.GroupExecutionLog) error
    DeleteGroupLogsBefore(cutoff time.Time) (int64, error)
    DeleteExcessGroupLogs(maxRecords int) error
}
```

### Step 1.2: GORM 实现 [NEW]
- **文件**: `internal/scheduler/log_repository_gorm.go`
- **类型名**: `GormLogRepository`
- **字段**: `db *gorm.DB`
- 每个方法直接搬运 executor.go 中对应的 DB 操作代码，无逻辑变更

### Step 1.3: 修改 Executor 结构体 [MODIFY]
- **文件**: `internal/scheduler/executor.go`
- **变更**:
  - `Executor` 结构体新增字段 `logRepo LogRepository`
  - `NewExecutor` 函数签名不变，内部自动创建 `GormLogRepository{db: db}`
  - 逐个替换 `e.db.Create(&execLog)` → `e.logRepo.CreateExecutionLog(&execLog)` 等（约 15 处）
  - 保留 `e.db` 字段用于非日志类查询（如 `e.db.First(&task, taskID)`）

### Step 1.4: 测试 [NEW]
- **文件**: `internal/scheduler/log_repository_test.go`
- **测试**: `TestGormLogRepository_CRUD` — 验证 GORM 实现的基本 CRUD
- **回归**: 运行全量 `go test ./internal/scheduler/...` 确保无退化

> 改动量约 10-15 行/子步骤，总计约 120 行新增 + 60 行替换

---

## Phase 2: P0 — Cron 路径超时阻断 + Daemon Watchdog

### 目标
确保所有 cron/manual 触发路径受全局超时保护；为 daemon 模式增加 Watchdog 审计。

### Step 2.1: cron 触发路径增加 context.WithTimeout [MODIFY]
- **文件**: `internal/scheduler/executor.go`
- **函数**: `handleTrigger`
- **变更**: 在 `e.pool.Submit(func(){...})` 内部，包裹 `context.WithTimeout(context.Background(), maxTimeout)`
  - `maxTimeout` 取 `e.cfg.Executor.MaxTimeoutSec`（默认 3600s）
  - 超时后记录 WARN 日志

### Step 2.2: RunTaskNow 手动触发路径同步加固 [MODIFY]
- **文件**: `internal/scheduler/executor.go`
- **函数**: `RunTaskNow`
- **变更**: 同 Step 2.1，用 `context.WithTimeout` 包裹 `e.executeTask(taskID)` 调用

### Step 2.3: executeTask 改为接受 context [MODIFY]
- **文件**: `internal/scheduler/executor.go`
- **函数**: `executeTask(taskID uint)` → `executeTask(ctx context.Context, taskID uint)`
- **变更**: 
  - 将外部 context 传递到 `runTaskByTypeCtx`
  - 在重试循环中检查 `ctx.Err()`，超时则提前退出
  - 所有调用点（`handleTrigger`, `RunTaskNow`, `RunGroup`）同步修改

### Step 2.4: Daemon Watchdog 审计 [MODIFY]
- **文件**: `internal/scheduler/daemon_monitor.go`
- **函数**: `runDaemonLoop` 内部
- **变更**: 在退避等待循环中，新增对 `execution_logs` 的主动健康审计：
  - 如果一个 daemon 任务的最新 `running` 日志已经超过 `2 * MaxRestartAttempts * backoff` 时间没有更新，主动将其标记为 `failed` 并重置状态
  - 通过 `logRepo.CountRunningLogs` 接口实现（依赖 P2）

### Step 2.5: 测试
- **文件**: `internal/scheduler/executor_timeout_test.go` [NEW]
- **测试**: `TestExecutor_CronTimeout` — 验证超长任务被全局超时强杀
- **回归**: 全量测试

> 改动量约 8-12 行/子步骤，总计约 80 行修改

---

## Phase 3: P1 — Save 异步批量写入

### 目标
将独立任务（cron/manual）的最终 `SaveExecutionLog` 异步化，通过内存 Channel 缓冲 + 定时批量刷盘，解放 SQLite 单连接串行阻塞。

### Step 3.1: AsyncLogWriter 组件 [NEW]
- **文件**: `internal/scheduler/async_log_writer.go`
- **类型名**: `AsyncLogWriter`
- **字段**:
  ```go
  type AsyncLogWriter struct {
      logRepo   LogRepository       // 底层存储（依赖 P2 接口）
      saveCh    chan *model.ExecutionLog  // 缓冲通道
      done      chan struct{}        // 关闭信号
      flushInterval time.Duration   // 刷盘间隔（默认 200ms）
      batchSize int                 // 单次批量上限（默认 50）
  }
  ```
- **方法**:
  - `NewAsyncLogWriter(repo LogRepository, bufSize int) *AsyncLogWriter`
  - `Start()` — 启动后台 flusher goroutine
  - `Enqueue(log *model.ExecutionLog)` — 非阻塞入队（channel 满时降级同步写）
  - `Flush()` — 强制刷盘（Shutdown 时调用）
  - `Stop()` — 排空 + 关闭

### Step 3.2: flusher 内部逻辑 [NEW]
- **文件**: `internal/scheduler/async_log_writer.go`
- **逻辑**:
  ```
  for {
      select {
      case log := <-saveCh:
          batch = append(batch, log)
          if len(batch) >= batchSize { flushBatch() }
      case <-ticker.C:
          if len(batch) > 0 { flushBatch() }
      case <-done:
          drainAndFlush(); return
      }
  }
  ```
- `flushBatch` 内部逐条调用 `logRepo.SaveExecutionLog`（因为每条 log 的 ID 不同，是 UPDATE 操作）

### Step 3.3: 集成到 Executor [MODIFY]
- **文件**: `internal/scheduler/executor.go`
- **变更**:
  - `Executor` 新增字段 `asyncWriter *AsyncLogWriter`
  - `NewExecutor` 中初始化并 `Start()`
  - `runTaskByTypeCtx` 尾部的 `e.logRepo.SaveExecutionLog(execLog)`
    → 改为 `e.asyncWriter.Enqueue(execLog)`（仅独立任务路径）
  - `RunGroup` 内部路径保持同步 `e.logRepo.SaveExecutionLog`（因为 RunGroup 内部有即时查询依赖）
  - `Shutdown` 中先 `e.asyncWriter.Stop()` 再 `e.pool.Release()`

### Step 3.4: 测试
- **文件**: `internal/scheduler/async_log_writer_test.go` [NEW]
- **测试**:
  - `TestAsyncLogWriter_BatchFlush` — 验证批量刷盘
  - `TestAsyncLogWriter_GracefulShutdown` — 验证关闭时排空
  - `TestAsyncLogWriter_FullChannelFallback` — 验证通道满时降级同步写
- **回归**: 全量测试

> 改动量约 10-15 行/子步骤，总计约 100 行新增 + 20 行修改

---

## 验证计划

### 自动化测试
```bash
go test ./internal/scheduler/... -v -count=1
```

### WSL 集成测试
复用 `repro.sh` 脚本验证：
1. 孤儿日志清理仍然生效（P2 接口未破坏原有逻辑）
2. 超时任务被正确强杀（P0）
3. 高并发短任务场景下日志无丢失（P1）

---

## 风险矩阵

| 风险 | 缓解措施 |
|------|---------|
| P2 接口替换遗漏导致编译失败 | 逐个替换 + `go build` 编译检查 |
| P0 context 传递导致 daemon 误杀 | daemon 路径的 `timeoutSec=0` 保持不变 |
| P1 异步写入导致日志丢失 | Shutdown 时强制 Flush + channel 满时降级同步 |
| P1 与 RunGroup 的即时查询冲突 | RunGroup 保持同步写入 |
