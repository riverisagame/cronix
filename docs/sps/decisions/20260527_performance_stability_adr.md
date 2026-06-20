# ADR: 调度引擎增量热更新与仪表盘缓存失效优化

## 1. 背景与上下文

当前 Cronix 定时任务调度器存在以下技术债：
1. **全量重载（ReloadAll）缺陷**：在新建、修改、删除任务或任务组时，系统会全量移除 `robfig/cron` 底层所有的已注册任务（Entry），并重新查询数据库全量注册。若在移除与重新注册的毫秒级间隙中刚好有任务触发，将造成**漏触发**；当任务规模增大时，此操作有显著的 CPU 锁争用。
2. **仪表盘统计（Stats）时效滞后**：`/api/dashboard/stats` 接口使用了 60 秒的固定 TTL 缓存。虽然这减轻了 SQLite 的并发读取压力，但当任务手动执行完毕或状态更新时，仪表盘无法实时体现最新状态。

## 2. 选型对比

| 维度 | 方案 A：全量 Reload + 固定 TTL 缓存（现状） | 方案 B：增量调度热更新 + 缓存失效（所选方案） |
| --- | --- | --- |
| **稳定性** | 存在重载期间漏触发风险 | 完全消除重载间隙，热更新仅移除/更新受影响的单一 Entry |
| **性能** | 任务规模多时，锁持有时间与任务数呈线性正相关 ($O(N)$) | 单任务更新开销为常量级 ($O(1)$) |
| **数据一致性** | 仪表盘统计最多存在 60 秒延迟 | 执行任务或变更配置时，主动失效缓存，实现实时刷新 |

## 3. 架构设计与变更范围

### 3.1. 调度层增量更新
在 `internal/scheduler/engine.go` 中，新增以下方法以支持单个任务/任务组的增量增删改：
* `UpdateTaskSchedule(task model.Task) error`：当任务被创建/更新时调用，根据状态增量注册或移除。
* `RemoveTaskSchedule(taskID uint)`：当任务被删除时调用，安全移除 Entry。
* `UpdateGroupSchedule(group model.TaskGroup) error`：支持组的增量更新。
* `RemoveGroupSchedule(groupID uint)`

在 `internal/service/task_service.go` 与 `internal/service/group_service.go` 中，替换原有的 `Engine.ReloadAll()` 逻辑，改用上面提供的增量 API。

### 3.2. 缓存失效机制
在 `internal/service/execution_service.go` 中：
* 新增 `InvalidateStatsCache()`，在创建/删除/更新任务，以及任务执行完毕写入执行日志时，主动清除缓存，以在下一次请求时刷新仪表盘，彻底解决 60 秒延迟。

## 4. 并发安全性审计 (Self-Attack & Performance)
* **读写锁保护**：`Engine` 的增量操作必须持有全局锁 `e.mu`，但锁的作用范围严格限制在修改 `e.entryMap` 和 `cron.Remove/AddFunc` 的微秒级原子操作内，不再包含耗时的数据库 `SELECT`。
* **物理零污染**：本期优化在 WSL Debian 上部署，所有测试覆盖将通过已有的 Mock 测试环境及内存操作完成，绝不影响真实的持久化数据。
