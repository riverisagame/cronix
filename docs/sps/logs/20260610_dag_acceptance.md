# 验收报告：DAG 调度引擎激活 (2026-06-10)

## 1. 验收目标
激活 `internal/scheduler/dag.go` 中存在的 Kahn 拓扑排序算法，将其接入到 `TaskGroup`（任务组）的执行循环中，允许任务在组内严格按照 DAG 依赖拓扑分层、并发执行。并且要绝对保证单体任务的手动触发（RunOnce）不受连动影响。

## 2. 验证过程与结果

### 2.1 单元测试通过情况
执行了 `go test -v ./internal/...` 全量测试。新增的 DAG 专用测试以及原有的调度器测试全部通过：
- **`TestExecutor_DAGGroupExecution_Red` (PASS)**：验证了 DAG 构建、分层并发、以及发生节点 Error 时的下游任务熔断。
- **`TestDAGNoCycle` / `TestDAGCycleDetection` (PASS)**：原有的底层 DAG 算法健壮性无损。
- **`TestExecutor_ExecutionIsolation` (PASS)**：验证了单体任务触发（RunTaskNow/RunOnce）绝对隔离，并未因为加入 DAG 而导致意外连动。

### 2.2 影响范围检查 (Blast Radius)
- `internal/scheduler/executor.go` 中新增了 `case "dag":` 分支，对原有的 `parallel` 和 `sequential` 逻辑零侵入。
- 对全局 `executor` 线程池的利用继续维持限制，所有 DAG 层的并发都在 `wg.Add` 结合 `e.pool.Submit` 的严格限制下安全运行。

## 3. 验收结论
**[BUILD_SUCCESS]**
“幽灵 DAG”已成功转正。Cronix 现在支持在 `TaskGroup` 级别进行可靠的工作流拓扑调度，完美符合“高内聚低耦合”与“绝对物理无损”的极简设计要求。
