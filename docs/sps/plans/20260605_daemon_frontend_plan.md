# 常驻进程守护前端 UI 实现计划

基于后端的常驻进程守护（Daemon Supervisor）功能已就绪，本计划详述如何在前端 Web 应用中实现其操作界面。

## User Review Required

> [!IMPORTANT]  
> 核心设计决策：对于常驻任务的实时状态（RUNNING / STOPPED / FATAL），我们计划在 `TaskList.vue` 列表页每隔 **3秒** 轮询一次 `/api/daemon/states` 接口，以保持状态徽章和运行时间的实时更新。请确认此轮询频率是否满足性能与体验的平衡？

## Proposed Changes

---

### API Layer
扩展 `src/api/index.ts`，加入与常驻任务相关的新接口调用。

#### [MODIFY] [web/src/api/index.ts](file:///d:/claudeprj/codex/web/src/api/index.ts)
- 在 `taskAPI` 中添加：
  - `startDaemon(id: number)`: 发送 `POST /tasks/:id/daemon/start`
  - `stopDaemon(id: number)`: 发送 `POST /tasks/:id/daemon/stop`
  - `getDaemonStatus(id: number)`: 发送 `GET /tasks/:id/daemon/status`
- 增加新的 `daemonAPI` (或在 `taskAPI` 增加 `getAllDaemonStates()`)，调用 `GET /daemon/states` 获取全量状态。

---

### Task Form (TaskEdit)
在任务编辑页支持常驻模式（`run_mode`）及相关参数的配置。

#### [MODIFY] [web/src/views/TaskEdit.vue](file:///d:/claudeprj/codex/web/src/views/TaskEdit.vue)
- **表单数据模型**：在 `form` 响应式对象中补充默认值：`run_mode: 'cron'`, `restart_policy: 'always'`, `max_restart_attempts: 10`。
- **运行模式选择**：在表单 Basic 区域增加 "Run Mode" 单选框（Cron 定时任务 / Daemon 常驻任务）。
- **字段条件渲染**：
  - 若 `run_mode === 'cron'`，显示原有的 `Cron Expression` 和 `Depends On` 字段。
  - 若 `run_mode === 'daemon'`，隐藏 Cron 表达式，新增并显示 `Restart Policy`（always/on-failure/never）和 `Max Restart Attempts`。
- **发送保存**：确保新字段在表单保存时被正确解构和传递到 API。

---

### Task Management List (TaskList)
在任务列表直观区分定时任务与常驻任务，并提供常驻任务的手动启停和实时状态。

#### [MODIFY] [web/src/views/TaskList.vue](file:///d:/claudeprj/codex/web/src/views/TaskList.vue)
- **状态聚合与轮询**：
  - 添加响应式变量 `daemonStates` 存储从后端抓取的常驻状态快照。
  - 在组件挂载时，启动 `setInterval` 每 3 秒拉取一次 `daemonAPI.getAllStates()`，并在组件卸载 (`onUnmounted`) 时清除定时器。
- **表格列更新**：
  - **Cron / Mode 列**：如果 `row.run_mode === 'daemon'`，不显示 Cron，而是渲染一个状态徽章（如绿色的 `RUNNING`，红色的 `FATAL`）及运行时长 (`uptime`)。
  - **Actions 列**：如果 `row.run_mode === 'daemon'`，将原来的单次“执行”按钮（蓝色闪电/播放），替换为两个专属按钮：
    - **Start Daemon** (绿色播放图标)：点击调用 `startDaemon(row.id)`
    - **Stop Daemon** (红色停止图标)：点击调用 `stopDaemon(row.id)`
    - 如果状态已经是 `RUNNING`，则禁用 Start 按钮；如果是 `STOPPED`，禁用 Stop 按钮。
- **拓扑图适配**：在 SVG 拓扑图中略过或特异化显示常驻任务（主要关注其列表态展示）。

## Verification Plan

### Manual Verification
1. 在前端新建一个常驻任务，选择 `always` 策略。保存后回到列表。
2. 列表界面应该自动显示其为常驻任务状态（默认 STOPPED）。
3. 点击 "Start Daemon" 按钮，状态变为 `RUNNING` 且 uptime 读秒开始增加。
4. 修改常驻任务的配置并保存，确认后端触发热重载，前端状态能在短时间内从 RUNNING -> 闪烁 -> 再次 RUNNING，Uptime 重置。
5. 测试故意写错命令（如 `not_exist_cmd`），观察 UI 从 RUNNING -> BACKOFF -> FATAL 的全过程可视化。
