# Architectural Review: Daemon & Supervisor Implementation
**Date**: 2026-06-05
**Reviewer**: Architect Agent (Active Skill: architect-review)
**Target**: Task-05 常驻守护进程功能 (DaemonMonitor)

## 1. 架构上下文与目标
本次变更引入了常驻任务（Daemon）管理能力，使 Cronix 从单一的 Cron 调度演进为“Cron + Daemon”双擎架构。目标是支持长生命周期进程的 Keep-Alive 守护、优雅终止以及异常熔断。

## 2. 架构设计亮点 (Strengths)
1. **职责分离 (Separation of Concerns)**:
   - 将 `DaemonMonitor` 与现有的定时任务 `Engine` 分离。定时任务走 `cron` 定时器，常驻任务走独立的守护协程，两者边界清晰，符合单一职责原则（SRP）。
2. **全链路 Context 取消机制 (Context Propagation)**:
   - 彻底重构了执行器的上下文传导。从 HTTP API 触发 `StopDaemon` -> 取消 `ctx` -> `runTaskByTypeCtx` -> `ExecuteShell` -> `exec.CommandContext`，实现了基于信号流的进程树强杀。这是分布式系统和现代进程管理的最佳实践。
3. **弹性与自愈 (Resilience & Self-Healing)**:
   - 引入了**指数退避 (Exponential Backoff)** 算法（1s -> 2s -> 4s ... -> 60s），有效防止了由于进程秒崩导致的 CPU 空转（Tight Loop）。
   - 设置了 `MaxRestartAttempts` 熔断机制，连续失败进入 `FATAL` 状态，实现了类似于断路器（Circuit Breaker）的保护。

## 3. 架构风险与技术债务评估 (Risks & Tech Debt)
在“自我攻击”和“性能对冲”审计下，当前架构仍存在以下潜在缺陷：

1. **配置热更新同步断层 (Configuration Sync Gap)** [风险级别：中高]
   - **现象**：目前 `DaemonMonitor` 在 `Start()` 时从数据库加载全量配置。如果用户在界面上修改了某个正在运行的 daemon 任务的 `RestartPolicy` 或 `Command`，现有的调度引擎重载机制 (`Engine.ReloadAll`) 仅过滤了 daemon 任务，但并**没有**通知 `DaemonMonitor` 热更新配置或重启对应的守护协程。
   - **影响**：必须先手动 Stop 再 Start 才能应用新配置。
2. **单点与内存状态易失性 (State Volatility)** [风险级别：中]
   - **现象**：`DaemonMonitor` 的状态（如 `RestartCount`、`Uptime`）全部存储在内存（`m.states`）中。
   - **影响**：Cronix 服务重启后，所有 FATAL 或 BACKOFF 的计数器将被重置，所有启用的 daemon 会被视为全新启动。对于单机版影响较小，若未来向高可用/集群演进，需考虑状态的持久化或分布式存储。
3. **僵尸进程遗留风险 (Zombie Process Risk)** [风险级别：低]
   - **现象**：虽然通过 `Context` 取消能停掉主进程，但在复杂的 Bash 脚本中，子进程如果未被分配到同一个 Process Group 或未正确捕获信号，仍可能成为孤儿进程。
   - **对冲建议**：目前已通过 `syscall.Setpgid` 和 `-kill` 负 PID 杀进程组（在 Task-04 中实现），风险已降至最低。

## 4. 架构改进建议 (Recommendations)
1. **短期 (Next Actions)**: 
   - 建议在任务更新 API（如更新任务配置的逻辑）中，若检测到是 daemon 任务，自动通过 channel 或方法调用通知 `DaemonMonitor` 平滑重启该任务（停旧起新），以修复热更新断层。
2. **中长期 (Evolution)**:
   - 引入事件总线（Event Bus）解耦 `Engine`、`DaemonMonitor` 与 `TaskService` 的通信，使得配置变更事件能被各类消费者独立监听和响应，进一步走向事件驱动架构（EDA）。

## 5. 结论
当前实现符合“零侵入”重构原则，安全且对原有 Cron 引擎无污染。满足阶段性交付标准，但在后续迭代中需关注配置热更新的协同。
