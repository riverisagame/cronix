# 架构深度评审与对冲报告 (ADR: Cronix Scheduler Engine)

## 1. 评审背景与攻击面设定
在修复了因进程强杀导致的常驻任务死锁问题后，作为主控架构师，我受命对 `cronix` 的调度核心（`Engine`、`Executor`、`Daemon Monitor`、`Database`）进行深度架构扫描。

**核心攻击方法论**：实现、自我攻击、性能对冲。
**关注焦点**：可靠性 (Resilience)、性能与吞吐 (Performance)、可维护性与扩展性 (Maintainability)。

---

## 2. 三路攻击与性能对冲

### 第一路：可靠性与防崩溃分析 (Resilience: 极端异常的自我恢复)
*攻击手段：模拟进程假死、网络脑裂、长任务失联*

* **现状防御**：刚才我们通过在 `NewExecutor` 启动时清理 `end_time IS NULL` 的悬挂日志（Orphaned Logs），成功化解了崩溃重启后的死锁危机。
* **致命弱点 (Risk: High)**：目前的自愈机制是**“启动时被动补偿”**。如果一个任务预期执行 2 小时，但底层 `shell` 进程在第 10 分钟卡死（如等待 I/O 或管道阻塞），`cronix` 进程本身并未崩溃。此时任务在数据库中永远是 `running` 状态，并且消耗了 `ants.Pool` 中的一个协程位。由于系统没重启，孤儿清理逻辑无法触发，该任务被永久静默挂起。
* **架构对冲方案**：
  - 必须引入 **活跃度租约机制 (Heartbeat/Lease)**。对运行超过指定阈值的任务，如果未收到执行器的心跳，内部的 Watchdog 协程应将其强行判定为超时并标记为 `failed`，同时释放本地 `ants` 协程池资源，避免线程泄露。

### 第二路：性能对冲与吞吐量上限 (Performance: 10万级任务并发压测)
*攻击手段：瞬时触发十万个定时任务，观察系统的雪崩点*

* **现状防御**：`handleTrigger` 中巧妙使用了“轻量级 `go func()` 配合 `ants.Pool.Submit`”，实现了一个优雅的非阻塞背压（Backpressure）漏斗，保证了内存不会因为协程爆炸而 OOM。
* **致命弱点 (Risk: Critical)**：**存储层的单点锁喉瓶颈**。通过审查 `internal/database/database.go`，发现系统使用 SQLite 并强制设置了 `sqlDB.SetMaxOpenConns(1)`。尽管开启了 WAL 模式，但这仅缓解了读写互斥，**所有的高并发任务状态变更（插入执行日志、更新任务状态）全部挤在这一条串行管道中**。当并发执行 1000 个轻量级短任务时，SQLite 单连接排队写盘将导致严重的写入延迟，进而反向阻塞 `ants.Pool` 的工作协程（协程在等待 DB 返回），最终拖垮整个调度引擎。
* **架构对冲方案**：
  - **短期**：在写入执行日志时引入 **异步批量提交 (Batch Write/Write-Behind Log)** 机制。任务状态先在内存或本地 Channel 更新，然后定时（如每 200ms）批量一次性写入 SQLite。
  - **长期**：将 `database` 抽象为 `Storage Interface`，解耦底层实现，允许高并发场景下零成本切换至 MySQL / PostgreSQL，利用真实连接池分流压力。

### 第三路：架构演进与单点瓶颈 (Maintainability & Distributed Readiness)
*攻击手段：多副本高可用部署测试*

* **现状防御**：目前通过 `engine.go` 将内存态的 `cron` 定时器与数据库状态同步，实现了一个轻巧的单机调度器。
* **致命弱点 (Risk: High)**：完全缺乏分布式容灾基因。如果运维在两台机器上启动 `cronix` 指向同一个共享网络盘的 SQLite（或未来切到 MySQL），由于内存中维护了相同的 `cron.EntryID`，**两台机器会在同一秒向通道里塞入相同的 `taskID`，导致任务双重触发（脑裂，Split-Brain）**。
* **架构对冲方案**：
  - **引入悲观抢占锁或 Leader Election**。在执行任务前，基于数据库表实现一个极轻量的 CAS (Compare-And-Swap) 抢占操作（如 `UPDATE execution_logs SET status='running' WHERE id=? AND status='pending'`）。或者，引入 Leader/Follower 角色，只有 Leader 加载定时器并派发任务，Follower 仅提供工作节点（Worker）的执行力。

---

## 3. 架构师最终决断 (Architect's Verdict)

当前的 `cronix` 是一个**“极其优秀的单机版调度引擎”**。它的并发隔离机制和本地内存防击穿设计非常干净。但是，它对 SQLite 过于依赖，且由于采用了同步写入日志的设计，其实际并发上限受制于单块磁盘的 IOPS。

### 优先改造建议路线图 (Roadmap)：
1. **P0 (近期止血)**：在 `model.Task` 中增加 `Timeout` 字段，并在 `executeTask` 引入基于 `context.WithTimeout` 的强制阻断，防备 `shell` 进程僵死导致的线程泄漏。
2. **P1 (性能升维)**：对 `execution_logs` 的写入进行异步化缓冲改造，解放 SQLite 的单连接串行阻塞。
3. **P2 (架构重构)**：抽象 `Repository` 接口，剥离对 GORM SQLite 细节的硬编码依赖，为集群模式铺路。
