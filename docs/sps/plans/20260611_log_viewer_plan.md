# Log Viewer Unification & E2E Test Fix

This plan addresses the E2E test failures and unifies the log viewing experience across the application. 

## User Review Required

> [!IMPORTANT]
> - 我们将创建一个共享的 `LogViewer` 组件，并在任何需要查看日志的地方（如 Live Console、任务历史记录、执行日志全局页面）统一使用它。
> - 当用户点击查看历史日志时，将打开与实时日志一致的终端风格查看器（具备全屏、下载和删除功能），而不是简单的文本框。
> - Live Console 将有清晰独特的 "已停止 (STOPPED)" 视觉状态（例如颜色变化或横幅），以便用户可以轻松判断实时执行已结束。
> - 请确认此计划，确认后我们将进入编码阶段。

## 提议的变更

---

### `web/src/components/LogViewer.vue`
#### [NEW] [LogViewer.vue](file:///d:/claudeprj/codex/web/src/components/LogViewer.vue)
- 一个全新的组件，提取自 `TaskList.vue` 中的终端 UI 逻辑。
- 支持通过 props 传入 `mode` (`'live'` 或 `'history'`)、`logs` (内容)、`status`、`duration` 等参数。
- 封装全屏切换逻辑、搜索/过滤和自动滚动逻辑。

---

### `web/src/views/TaskList.vue`
#### [MODIFY] [TaskList.vue](file:///d:/claudeprj/codex/web/src/views/TaskList.vue)
- 将内联的 "Live Console" HTML 替换为 `<LogViewer>` 组件。
- 在 "执行历史 (Execution History)" 标签页中，当用户点击某条日志时，在抽屉或全屏模态框中打开 `<LogViewer>` 以展示完全相同的 UX。
- 确保当任务结束或退出时，Live Console 能够清晰地传入并展示 `STOPPED` 状态。

---

### `web/src/views/ExecutionLogs.vue`
#### [MODIFY] [ExecutionLogs.vue](file:///d:/claudeprj/codex/web/src/views/ExecutionLogs.vue)
- 将日志详细信息使用的简单文本块替换为 `<LogViewer>` 组件。
- 赋予全局日志页面相同的全屏和搜索能力。

---

### `web/src/views/GroupList.vue`
#### [MODIFY] [GroupList.vue](file:///d:/claudeprj/codex/web/src/views/GroupList.vue)
- 在展开任务组历史日志时，使用 `<LogViewer>` 组件。

---

### E2E Test Fixes
#### [MODIFY] `web/tests/e2e/specs/*.spec.ts`
- 修复因上一轮 UI 更新而破坏的 Playwright 测试选择器。

## Verification Plan

### Automated Tests
- 执行 `npm run test:e2e` 以在本地运行 Playwright 测试，确保 CI 恢复正常通过。

### Manual Verification
- 测试查看运行中的实时任务，等待其停止，以查看新的 "STOPPED" 清晰视觉状态。
- 测试从各个页面（任务列表、全局执行日志）查看历史日志，以验证全屏和日志体验是否在所有地方都一致。
