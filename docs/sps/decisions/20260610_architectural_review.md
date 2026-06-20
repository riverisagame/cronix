# Architectural Review of Cronix System (2026-06-10)

## 1. 架构现状分析 (Context & Current State)
Cronix 目前是一个基于 `robfig/cron/v3` 和 `ants` 线程池构建的单节点分布式定时任务系统，结合了 GORM 进行 SQLite/MySQL 的存储。其核心分为 `Engine` (触发器) 和 `Executor` (执行器)。

在之前的几轮迭代中，我们修复了 `DaemonMonitor` 击穿死循环、`setpgid` 权限逃逸以及错误日志丢失等严重的基础架构级 Bug。

然而，通过本次全景扫码和深度架构审查，发现了**几个深水区的架构设计缺陷**。

## 2. 核心架构缺陷 (Architectural Violations & Risks)

### [CRITICAL] 1. 线程池全局旁路漏洞 (Thread Pool Bypass & Unbounded Goroutines)
- **现象**: 在 `executor.go` 的 `handleTrigger` 方法中，定时器触发普通任务时，直接使用了原生的 `go func() { e.executeTask(taskID) }()`。
- **危害**: 系统初始化时花费资源配置并创建的 `ants.Pool`（线程池，防止系统资源耗尽）**被完全架空**（仅在 `RunGroup` 的 parallel 模式下才被使用）。如果同一秒钟有 1 万个 cron 任务触发，系统会瞬间裸启 1 万个 Goroutine，导致 CPU 瞬时打满、内存暴涨，并直接压垮数据库连接池（Thundering Herd 效应）。

### [HIGH] 2. 幽灵的 DAG 依赖图 (Dead Code & Fake Feature)
- **现象**: `dag.go` 中完整实现了 Kahn 算法、拓扑排序和环检测，并且 `executor.go` 中还有个 `buildDAG` 方法。
- **危害**: 经过交叉引用检索，`buildDAG` 没有任何地方被调用。系统宣称支持的 "任务依赖关系" (Task Dependency) 实际上是一个幻觉，底层的执行器完全是各自为战。

### [HIGH] 3. 同步数据库级联阻塞 (Synchronous Log Pruning Contention)
- **现象**: 每次任务结束时，`executeTask` 会同步调用 `limitTaskLogs`，内部执行 `Count -> Pluck -> Delete` 三连击来清理超额日志。
- **危害**: 这种设计让本该轻量结束的执行线程，陷入到笨重的数据库事务中。如果有上百个任务同时结束，这会导致 `execution_logs` 表产生极高的锁竞争，阻塞其他正在尝试写入开始日志的任务。

### [MEDIUM] 4. 通知队列静默丢包 (Silent Alert Dropping)
- **现象**: `notifyTaskResult` 使用了非阻塞通道写入 `select { case e.Notifier.NotifyChan() <- event: ... default: ... }`。
- **危害**: 如果通知发送通道达到上限（例如邮件发送端网络卡顿），当瞬间有大批任务完成需要报警时，多余的严重告警会直接被默默抛弃，运维人员对此将一无所知。

## 3. 架构优化方向 (Recommended Refactoring)

1. **强行收口线程池 (Enforce Thread Pool Isolation)**
   - 将 `handleTrigger` 中的 `go func()` 替换为 `e.pool.Submit(func() { ... })`。
   - 所有异步后台任务强制通过 `ants.Pool` 提交，从而赋予系统真正的限流抗压能力。

2. **异步化日志治理 (Asynchronous Audit Pruning)**
   - 剥离 `limitTaskLogs` 的同步调用。
   - 引入一个专门的后台垃圾回收器 (Garbage Collector) 协程或利用现在的 `cleanupTicker` 来集中低峰期异步清理所有多余记录，完全释放执行器性能。

3. **落实 DAG 编排引擎 (Implement Actual DAG Execution)**
   - 重构依赖任务调度：如果任务A设定依赖任务B，那么 A 不应该被独立的 cron 注册，而应当只注册根节点（B）。当B成功时，再通过事件驱动触发 A。

4. **通知系统反压与持久化 (Notification Backpressure)**
   - 将通知机制改由数据库或 Redis 等带持久化属性的队列缓冲，或者增大 channel size 并结合带超时阻塞的回退机制（Backoff），杜绝报警凭空消失。

## 4. 结论 (Conclusion)
Cronix 目前可以运转良好，主要归功于业务量不大，但面对高并发场景的脆弱性极高。建议立即对 **"线程池旁路"** 这个致命级问题开刀进行微创手术。
