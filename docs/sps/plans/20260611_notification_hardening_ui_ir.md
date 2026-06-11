# Notification System Hardening & UI Integration

解决“通知队列堵塞引发雪崩丢包”问题，并补齐缺失的前端 Webhook 配置界面。采用绝对的 TDD（红绿重构）方法，确保现有调度与执行功能**0影响**。

## User Review Required

- **前端集成方式**：由于历史接口未集成 NotifyConfig，为避免破坏原有的 `/api/tasks` 存量数据结构，本次计划将使用完全独立的新 API 端点 `/api/tasks/:id/notify`。这符合 RESTful 标准且完全防退化。请确认是否同意。
- **并发池限制**：默认使用系统已存在的 `ants` 库作为并发池，大小设置为 50。这意味着最高支持同时发出 50 个独立的 Webhook 请求。请确认是否满足当前预期。

## 架构原则遵守情况
- **[Architecture]**：遵循 “Simplicity is the ultimate sophistication” 原则。不引入复杂的 MQ 或 DB 轮询队列，仅仅通过引入 `ants` 协程池隔离网络阻塞问题。
- **[UI-UX-Pro-Max]**：遵循 Accessibility (Form inputs have labels) 和 Touch & Interaction (clear hover states, no emojis)。将使用 Element Plus 原生组件并在 `TaskEdit.vue` 增加独立配置区块。
- **[TDD]**：后端功能严格遵守 RED-GREEN-REFACTOR 流程，无测试不代码。

---

## Proposed Changes

### 1. Backend: Notification Concurrency Hardening (TDD)

#### [MODIFY] [internal/notify/notifier_test.go](file:///d:/claudeprj/codex/internal/notify/notifier_test.go)
- **动作 (RED)**: 编写 `TestNotifier_Concurrency`。启动 Notifier，Mock 一个长耗时 (100ms) 的 HTTP Webhook 服务。瞬间写入 200 个通知事件到 `notifyCh`，验证队列是否会阻塞发送方，且验证处理耗时是否远小于串行耗时。
- **验证**: `go test ./internal/notify` 必须因为超时或死锁而**失败**。

#### [MODIFY] [internal/notify/notifier.go](file:///d:/claudeprj/codex/internal/notify/notifier.go)
- **动作 (GREEN)**: 引入 `github.com/panjf2000/ants/v2` 协程池（容量 50）。在 `Start()` 的 select 循环中，将 `n.send(event)` 改为 `n.pool.Submit(func() { n.send(event) })`。
- **动作 (REFACTOR)**: 增加优雅关机机制，在 `ctx.Done()` 时调用 `n.pool.Release()` 释放协程资源。

### 2. Backend: REST API for NotifyConfig (TDD)

#### [NEW] [internal/handler/notify_test.go](file:///d:/claudeprj/codex/internal/handler/notify_test.go)
- **动作 (RED)**: 编写 GET 和 PUT `/api/tasks/:id/notify` 的接口测试，验证 404 状态与 200 更新逻辑。无侵入测试。

#### [MODIFY] [internal/service/task_service.go](file:///d:/claudeprj/codex/internal/service/task_service.go)
- **动作**: 增加 `GetTaskNotify(taskID uint)` 和 `UpdateTaskNotify(taskID uint, cfg *model.NotifyConfig)`。此修改限制在 20 行代码以内。

#### [MODIFY] [internal/handler/task.go](file:///d:/claudeprj/codex/internal/handler/task.go)
- **动作**: 增加 `GetTaskNotify(c *gin.Context)` 和 `UpdateTaskNotify(c *gin.Context)` 绑定逻辑。

#### [MODIFY] [internal/router/router.go](file:///d:/claudeprj/codex/internal/router/router.go)
- **动作**: 在鉴权路由组中挂载 `GET /api/tasks/:id/notify` 和 `PUT /api/tasks/:id/notify`。

### 3. Frontend: NotifyConfig UI (UI-UX-Pro-Max)

#### [MODIFY] [web/src/api/task.ts](file:///d:/claudeprj/codex/web/src/api/task.ts)
- **动作**: 增加前端接口函数 `getTaskNotify(id: number)` 和 `updateTaskNotify(id: number, data: any)`。

#### [MODIFY] [web/src/views/TaskEdit.vue](file:///d:/claudeprj/codex/web/src/views/TaskEdit.vue)
- **动作**: 在任务表单中，添加一个折叠面板 `<el-collapse>` 或独立卡片，命名为 "通知配置"。
- **UI规范**:
  - `label="Webhook URL"`, 并且输入框具有清晰的 placeholder。
  - 使用 `<el-switch>` 控制 `OnFailure` 和 `OnSuccess` 触发条件。
  - 在加载数据时并行请求 `getTaskNotify`，保存时并行请求 `updateTaskNotify`（不影响原有表单提交结构）。

## Verification Plan

### Automated Tests
- `go test -v ./internal/notify`
- `go test -v ./internal/handler -run TestNotify`
- 测试必须100%覆盖且不能修改、触碰现有业务代码的测试。

### Manual Verification
- `cd web && npm run build` 确保前端通过静态检查与打包。
- 在页面上修改 Webhook 配置，并手动触发一次失败的任务，确认服务端协程池正常接收并抛出请求。
