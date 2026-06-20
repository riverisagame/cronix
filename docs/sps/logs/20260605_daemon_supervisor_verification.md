# Task-05 验收报告 - 类 Supervisor 常驻进程守护功能

- **验收日期**：2026-06-05
- **任务编号**：Task-05
- **状态**：✅ GREEN 全测试通过

---

## 1. 变更清单

| 文件 | 变更类型 | 变更内容 |
| --- | --- | --- |
| `internal/model/task.go` | MODIFY | 扩展 `Task` 结构体，新增 `RunMode`/`RestartPolicy`/`MaxRestartAttempts` 三字段 |
| `internal/executor/shell_unix.go` | MODIFY | 修复 `ExecuteShell` 的 context 传递 Bug（`context.Background()` → `ctx`） |
| `internal/scheduler/engine.go` | MODIFY | `ReloadAll`/`UpdateTaskSchedule` 中前置过滤 daemon 任务，跳过 cron 注册 |
| `internal/scheduler/executor.go` | MODIFY | 新增 `ExecuteTaskWithContext` 导出方法和 `runTaskByTypeCtx` 带上下文分发 |
| `internal/scheduler/daemon_monitor.go` | NEW | 实现 DaemonMonitor：Keep-Alive 自动拉起、指数退避、FATAL 熔断、手动启停 |
| `internal/scheduler/daemon_test.go` | NEW | TDD 测试：KeepAlive 自动重启 + FATAL 熔断、Stop 优雅强杀 |
| `internal/handler/task.go` | MODIFY | 新增 `StartDaemon`/`StopDaemon`/`GetDaemonStatus` HTTP 处理器 |
| `internal/router/router.go` | MODIFY | 注册 `/api/tasks/:id/daemon/{start,stop,status}` 路由 |
| `cmd/root.go` | MODIFY | 在 `runServe` 中实例化并挂载 DaemonMonitor |

---

## 2. 测试结果

```
=== RUN   TestDaemonMonitor_KeepAlive
daemon monitor: exit 1 崩溃 -> 自动拉起 -> 退避 1s/2s/4s -> 第 3 次达到阈值 -> FATAL 熔断
--- PASS: TestDaemonMonitor_KeepAlive (4.01s)

=== RUN   TestDaemonMonitor_Stop
daemon monitor: sleep 100 长驻 -> RUNNING -> StopDaemon -> context 取消 -> SIGKILL 强杀 -> STOPPED
--- PASS: TestDaemonMonitor_Stop (3.51s)

全量 scheduler 测试套件: 10/10 PASS, 0 FAIL (7.591s)
go vet: 全部通过（scheduler, handler, model, cmd）
```

---

## 3. 架构设计要点

### 3.1 状态机
```
STOPPED → STARTING → RUNNING → (异常退出) → BACKOFF → RUNNING
                                            → (超限) → FATAL
```

### 3.2 指数退避算法
- 连续失败延迟：1s → 2s → 4s → 8s → 16s → 32s → 60s（上限封顶）
- 退避期间响应 `ctx.Done()` 实现即时终止（<15ms 响应）

### 3.3 Context 全链路传导
```
DaemonMonitor.ctx → ExecuteTaskWithContext → runTaskByTypeCtx → ExecuteShell(ctx, ...) → context.WithTimeout(ctx, ...) → exec.CommandContext(tCtx, ...)
```
手动 Stop 时取消 ctx，信号从守护层一路传导到底层进程组 SIGKILL。

### 3.4 并发安全
- `sync.RWMutex` 保护 `map[uint]*daemonTaskState`
- 读操作（GetDaemonState）使用 `RLock`，写操作使用 `Lock`

---

## 4. API 端点

| 方法 | 路由 | 功能 |
| --- | --- | --- |
| POST | `/api/tasks/:id/daemon/start` | 手动启动常驻守护任务 |
| POST | `/api/tasks/:id/daemon/stop` | 手动停止常驻守护任务 |
| GET | `/api/tasks/:id/daemon/status` | 查询守护任务实时状态 |

---

## 5. 物理零污染确认

- 测试使用 `t.TempDir()` 创建临时 SQLite 数据库
- 未执行任何 DROP/TRUNCATE/CREATE TABLE
- 测试结束后 Cleanup 自动关闭并销毁临时数据库
- 对现有物理表结构和数据 100% 无损
