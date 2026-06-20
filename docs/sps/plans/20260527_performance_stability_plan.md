# 性能与稳定优化纳米级执行计划 - 2026-05-27

本计划旨在实现调度器的增量热重载，并解决仪表盘数据的缓存一致性延迟。本优化按最小侵入、测试驱动（TDD）及物理零污染原则开展。

## 架构设计与并发安全原则 (Architecture & Concurrency Safety)

为确保高并发下响应延迟控制在 150ms 以内并保障调度精度，本项目实施以下架构硬约束：
1. **无锁式/锁外 I/O 查询**：调度器 `Engine` 的全局排他锁 `e.mu` 仅保护 `e.entryMap`、`e.groupEntryMap` 内存映射以及底层 `robfig/cron` 的指针操作。**严禁**在锁内执行任何 GORM 数据库查询、磁盘 I/O 或外部网络调用。
2. **轻量双检读写锁（Double-checked Locking）**：仪表盘统计 `statsCache` 采用读写锁 `sync.RWMutex` 进行并发隔离，当高并发读请求触发且缓存失效时，仅一个协程持有写锁进入数据库查询，其余协程在读锁中安全等待并直接命中刚生成的缓存。
3. **调度事务边界与一致性**：当创建或更新任务调度表达式（如 `UpdateTaskSchedule`）时，若 `AddFunc` 失败，必须将错误返回并回滚数据库事务，确保 DB 状态与内存定时器绝对一致，防范垃圾 entry。

## 1. 变更文件与受影响范围

| 文件路径 | 变更类型 | 影响范围 | 预计代码行数 |
| --- | --- | --- | --- |
| `internal/service/execution_service.go` | [MODIFY] | 缓存失效机制，提供清除缓存 API | ~15 行 |
| `internal/scheduler/engine.go` | [MODIFY] | 增量调度 API (`UpdateTaskSchedule`, `RemoveTaskSchedule` 等) | ~60 行 |
| `internal/scheduler/executor.go` | [MODIFY] | 增加 `ExecSvc` 引用并在任务/组执行完毕时失效缓存 | ~15 行 |
| `internal/service/task_service.go` | [MODIFY] | 引入 `ExecSvc` 并在 CRUD 时调用缓存失效与增量调度 | ~25 行 |
| `internal/service/group_service.go` | [MODIFY] | 引入 `ExecSvc`，在组 CRUD 时调用缓存失效与增量调度；修复 SetGroupMembers 状态未刷新 Bug | ~35 行 |
| `cmd/root.go` | [MODIFY] | 组装/注入 `ExecSvc` 到 `taskSvc`, `groupSvc` 及 `executor` | ~10 行 |

---

## 2. 纳米级执行步骤

每个子任务代码变动限制在 10-20 行以内。

### 阶段一：[RED 阶段] 编写失败的单元测试 (TDD)
* **[T1.1]** 编写 `internal/scheduler/engine_incremental_test.go`：测试 `UpdateTaskSchedule` 和 `RemoveTaskSchedule` 的基本逻辑，模拟不通过 `ReloadAll` 的增量调度刷新。
* **[T1.2]** 编写 `internal/service/cache_invalidation_test.go`：测试 `InvalidateStatsCache` 方法在修改任务、以及在执行器跑完任务后是否能立即清空 Dashboard 缓存。

### 阶段二：[GREEN 阶段] 最小化实现

#### 第一步：缓存机制失效支持
* **[S2.1]** 更改 `internal/service/execution_service.go`：在 `statsCache` 结构上新增 `Invalidate()` 读写锁方法。
* **[S2.2]** 更改 `internal/service/execution_service.go`：在 `ExecutionService` 上暴露 `InvalidateStatsCache()` API。
* **[S2.3]** 更改 `internal/service/execution_service.go`：在 `ClearAllLogs` 等内部写入/删除日志的方法结尾，调用 `InvalidateStatsCache()`。

#### 第二步：调度引擎增量接口
* **[S2.4]** 更改 `internal/scheduler/engine.go`：新增 `RemoveTaskSchedule(taskID uint)` 接口。
* **[S2.5]** 更改 `internal/scheduler/engine.go`：新增 `UpdateTaskSchedule(task model.Task) error` 接口。
* **[S2.6]** 更改 `internal/scheduler/engine.go`：新增 `RemoveGroupSchedule(groupID uint)` 接口。
* **[S2.7]** 更改 `internal/scheduler/engine.go`：新增 `UpdateGroupSchedule(group model.TaskGroup) error` 接口。

#### 第三步：服务与执行器接线与调用
* **[S2.8]** 更改 `internal/scheduler/executor.go`：在 `Executor` 结构体上添加 `ExecSvc *service.ExecutionService` 字段。
* **[S2.9]** 更改 `internal/scheduler/executor.go`：在 `executeTask` 的 `db.Save(execLog)` 后，若 `ExecSvc != nil` 则调用 `ExecSvc.InvalidateStatsCache()`。
* **[S2.10]** 更改 `internal/scheduler/executor.go`：在 `RunGroup` 结尾的 `db.Save(&glog)` 后，若 `ExecSvc != nil` 则调用 `ExecSvc.InvalidateStatsCache()`。
* **[S2.11]** 更改 `internal/service/task_service.go`：给 `TaskService` 添加 `ExecSvc *ExecutionService` 字段。
* **[S2.12]** 更改 `internal/service/task_service.go`：在 `CreateTask` 中，将 `ReloadAll()` 改为 `UpdateTaskSchedule()` 和 `InvalidateStatsCache()`。
* **[S2.13]** 更改 `internal/service/task_service.go`：在 `UpdateTask` 中，查询更新后的任务，改用 `UpdateTaskSchedule()` 并失效缓存。
* **[S2.14]** 更改 `internal/service/task_service.go`：在 `DeleteTask` 中，改用 `RemoveTaskSchedule()` 并失效缓存。
* **[S2.15]** 更改 `internal/service/group_service.go`：给 `GroupService` 添加 `ExecSvc *ExecutionService` 字段。
* **[S2.16]** 更改 `internal/service/group_service.go`：修改 `CreateGroup`，改用 `UpdateGroupSchedule` 并失效缓存。
* **[S2.17]** 更改 `internal/service/group_service.go`：修改 `UpdateGroup`，获取最新组数据，改用 `UpdateGroupSchedule` 并失效缓存。
* **[S2.18]** 更改 `internal/service/group_service.go`：修改 `DeleteGroup`，改用 `RemoveGroupSchedule` 并失效缓存。
* **[S2.19]** 更改 `internal/service/group_service.go`：修改 `SetGroupMembers`，在事务提交后，查出受影响的任务并逐个调用 `UpdateTaskSchedule` 增量热更新（修复原先未刷新 Bug）。
* **[S2.20]** 更改 `cmd/root.go`：在 `runServe` 的服务初始化位置，将 `execSvc` 注入到 `exec.ExecSvc`, `taskSvc.ExecSvc` 以及 `groupSvc` 中。

---

## 3. 验证方案

1. **编译验证**：在 WSL Debian 下执行 `go build -o cronix .`，确保编译无误。
2. **测试套件验证**：运行现有的 `test-suite.sh`、`prod-test.sh` 及 `stress-test.sh`，确保高负载、高并发下响应速度正常，无漏触发，且缓存失效运行正确。
   * *注意*：需将 `test-suite.sh` 中的 SQL 注入查询参数由未编码字面量修改为标准 URL 编码，防范 Curl 进程抛出错误码 3 导致 `set -e` 终止。
3. **并发性能对比 (Benchmark)**：
   * 在大量修改任务时，对比原先全载重模式与增量更新模式的 CPU 和锁延迟。
