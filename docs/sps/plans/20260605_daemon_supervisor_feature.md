# 类 Supervisor 常驻进程守护功能纳米级执行计划

本计划旨在实现 Cronix 的常驻守护进程管理功能（Task-05），包括 Task 模型字段扩展、守护引擎 DaemonMonitor 实现、`shell_unix.go` 的 `ExecuteShell` 的 context 传入 Bug 修复、手动启停 API 路由对接，以及 TDD 绝对测试驱动的单元测试编写。

## 1. 变更文件与受影响范围

| 文件路径 | 变更类型 | 影响范围 | 预计代码行数 |
| --- | --- | --- | --- |
| `internal/model/task.go` | [MODIFY] | 扩展 Task 结构体，增加 `RunMode`、`RestartPolicy`、`MaxRestartAttempts` 三个字段 | ~10 行 |
| `internal/scheduler/engine.go` | [MODIFY] | 定时触发时自动忽略 `RunMode == "daemon"` 的任务，防范其定时执行 | ~15 行 |
| `internal/executor/shell_unix.go` | [MODIFY] | 修复 `ExecuteShell` 内部 `context.WithTimeout` 错误地使用 `context.Background()` 而不是传入的 `ctx` 的 Bug | ~5 行 |
| `internal/scheduler/daemon_monitor.go` | [NEW] | 实现常驻进程守护管理器 DaemonMonitor：维护运行状态、支持 Keep-Alive 自动拉起、指数退避重启、手动启停控制及状态查询 | ~180 行 |
| `internal/scheduler/daemon_test.go` | [NEW] | 编写 TDD 单元测试：测试自动重启、崩溃退避、多次失败 FATAL 熔断以及手动 Stop 的强杀效果 | ~120 行 |
| `internal/handler/task.go` | [MODIFY] | TaskHandler 结构体增加 DaemonMon，添加常驻守护任务的手动启动、停止和状态查询接口 | ~60 行 |
| `internal/router/router.go` | [MODIFY] | 注册常驻守护任务的手动 Start、Stop、Status API 路由端点 | ~10 行 |
| `cmd/root.go` | [MODIFY] | 在服务 serve 启动时初始化 DaemonMonitor，挂载至 TaskHandler 并运行 | ~20 行 |

---

## 2. 纳米级执行步骤

每个子任务代码变动严格控制在 10-25 行以内。

### 阶段一：[RED 阶段] 运行单元测试 (TDD)
* **[T1.1]** 在 WSL 环境下运行当前测试：`wsl go test -v ./internal/scheduler/... -run TestDaemonMonitor`，观察到编译失败（红灯），报告 `Task` 没有 `RunMode` 字段。

### 阶段二：[GREEN 阶段] 最小化实现

#### 第一步：Task 模型字段扩展与 Shell Unix 上下文修复
* **[S2.1]** 更改 `internal/model/task.go`：
  - 在 `Task` 结构体上增加 `RunMode` (默认为 `"cron"`)、`RestartPolicy` (默认为 `"always"`)、`MaxRestartAttempts` (默认为 `10`) 字段的声明及 JSON/GORM 标签。
* **[S2.2]** 更改 `internal/executor/shell_unix.go`：
  - 将 `ExecuteShell` 内部的 `context.WithTimeout(context.Background(), ...)` 修改为 `context.WithTimeout(ctx, ...)`，使其正确支持外部传入的取消信号。

#### 第二步：调度引擎定时忽略常驻任务
* **[S2.3]** 更改 `internal/scheduler/engine.go`：
  - 在 `ReloadAll()` 中过滤 `task.RunMode == "daemon"` 的任务，跳过在 `cron.AddFunc` 的注册。
  - 在 `UpdateTaskSchedule()` 中，如果 `task.RunMode == "daemon"`，直接返回 `nil`。

#### 第三步：常驻守护引擎实现 (DaemonMonitor)
* **[S2.4]** 创建 `internal/scheduler/daemon_monitor.go`：
  - 定义 `DaemonState` 结构体，包含运行状态（`Status`）、PID、重启次数（`RestartCount`）、最后错误（`LastError`）和最后启动时间。
  - 定义 `DaemonMonitor` 结构体，内部包含全局数据库引用 `*gorm.DB`，`*Executor`，以及带 `sync.RWMutex` 保护的 `states map[uint]*daemonTaskState`（存放每个任务的取消句柄 `cancel context.CancelFunc`，当前状态等）。
* **[S2.5]** 在 `DaemonMonitor` 中实现 `Start(ctx context.Context)`：
  - 启动时扫描数据库中已启用的常驻任务：`db.Where("enabled = ? AND run_mode = ?", true, "daemon")`，对每个任务并发调用 `StartDaemon`。
* **[S2.6]** 在 `DaemonMonitor` 中实现 `StartDaemon(taskID uint)`：
  - 启动常驻运行协程，受 `context.WithCancel` 控制。在循环中，同步调用 `executor.executeTaskWithContext(ctx, taskID)`。
  - 如果执行退出，根据 `RestartPolicy` 判定是否重启。若需要重启且连续失败次数未超限，进入指数退避（延迟翻倍，最大 60s），并在退避期间响应 `ctx.Done()` 以支持即时终止。
  - 若超出 `MaxRestartAttempts`，将内存状态标记为 `FATAL`。
* **[S2.7]** 在 `DaemonMonitor` 中实现 `StopDaemon(taskID uint)`：
  - 手动停止时，撤销其 context，将内存状态置为 `STOPPED`。

#### 第四步：HTTP API 端点与路由注册
* **[S2.8]** 更改 `internal/handler/task.go`：
  - 在 `TaskHandler` 结构体中添加 `DaemonMon *scheduler.DaemonMonitor` 字段。
  - 编写 `StartDaemon(c *gin.Context)`、`StopDaemon(c *gin.Context)` 和 `GetDaemonStatus(c *gin.Context)` HTTP 处理器。
* **[S2.9]** 更改 `internal/router/router.go`：
  - 注册 `/api/tasks/:id/daemon/start` (POST)、`/api/tasks/:id/daemon/stop` (POST)、`/api/tasks/:id/daemon/status` (GET) 三个新路由端点。

#### 第五步：在启动入口中挂载引擎
* **[S2.10]** 更改 `cmd/root.go`：
  - 在启动 `runServe` 构造好 `executor` 和 `taskHandler` 之后，实例化 `NewDaemonMonitor`，将其注入到 `taskHandler` 中，并运行 `go monitor.Start(ctx)`。

## 3. 验证与归档 (Verify & Finish)
* 再次在 WSL 环境下运行测试，确认 `daemon_test.go` 全绿（GREEN）。
* 编写验收报告归档到 `docs/sps/logs/`，更新 `MASTER_LOG.md` 状态为 `FINISH`。

