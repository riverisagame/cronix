# 纳米级计划制定：日志功能增强 (UI/UX)

## 变更文件
`web/src/views/TaskList.vue`

## 详细改动步骤

### 1. 全屏功能 (Live Console)
- **模板变更**：在 `terminal-header` 中添加全屏图标按钮（ `<el-button>` ），通过图标 `el-icon-full-screen` 标识。
- **逻辑变更**：
  - 添加 `terminalRef` 引用指向包含 `<pre>` 标签的外层容器。
  - 实现 `toggleFullscreen()` 函数：检测当前是否全屏，调用 `element.requestFullscreen()` 或 `document.exitFullscreen()`。
- **样式变更**：处理全屏后的高度问题（全屏时终端占满屏幕而不是固定高度），利用 CSS `:fullscreen` 伪类调整高度为 `100vh`。

### 2. 下载导出与删除 (Execution History)
- **模板变更**：在 `<el-timeline-item>` 的卡片右上角或动作区域加入两个小按钮：
  - `Download`: `el-button` 配备 `el-icon-download`。
  - `Delete`: `el-button` 配备 `el-icon-delete`，外层包裹 `el-popconfirm` 以防误触。
- **逻辑变更**：
  - `downloadLog(log)`：构造 Blob 对象 `new Blob([log.output], { type: 'text/plain' })`，生成 `URL.createObjectURL`，创建隐藏的 `<a>` 标签触发下载，文件名为 `task_xx_timestamp.log`。
  - `deleteLogRecord(logId)`：调用 `logAPI.deleteLog(logId)`，成功后执行 `ElMessage.success`，并在 `historyLogs.value` 中 `findIndex` 后使用 `splice` 剔除该元素实现局部刷新。

### 3. 执行时间计算
- **逻辑变更 (历史执行时长)**：
  - 增加一个辅助函数 `calculateDuration(start, end)`。
  - 使用 `dayjs(end).diff(dayjs(start), 'millisecond')`。
  - 格式化输出为 `Xm Ys` 或 `Xms`，在卡片的标题旁展示（如 `耗时：2s 300ms`）。
- **逻辑变更 (实时运行耗时)**：
  - 新增 `liveDuration` 的 ref 变量。
  - 在启动 `startLiveStream` 时，通过获取到任务点击时的本地时间（由于暂无准确 `start_time`，可以暂存 `liveStartTime.value = Date.now()`）。
  - 在每次定时器触发时（甚至可单独搞个 1 秒级的计时器），计算 `Date.now() - liveStartTime.value` 并更新到 UI。
  - 考虑到任务如果是别人触发的，此时可能需要先拉取一下 `getLogs` 获取真实的 `start_time`；为了稳妥并降低 API 开销，最简单的方法是在 Live 控制台的 `terminal-header` 右侧展示一个每秒自增的计时器 "持续监控中: Xs"。

## 审查与验证指标
- **边界条件测试**：全屏切换按 `ESC` 键的退出状态兼容；日志内容为空时仍能下载 0KB 文件；正在轮询中的任务点击删除旧日志无干扰。
- **物理零侵入**：后端 Golang 代码 0 改动。
