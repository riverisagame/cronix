# [Task-18] 流式日志与进程手动干预 (Log Streaming & Kill Switch)

## 风险评估与对体现有功能的“零侵入”设计
针对您提出的“风险大不大”以及“绝对无损”要求：
- **无损保障 (Zero-Impact Design)**：如果每秒把日志打进 SQLite，会引发锁表灾难。因此我们采取**双轨制日志结构**：
  1. 脚本运行时，底层只把 stdout/stderr 通过管道 (`io.MultiWriter`) 写入到系统临时文件区（例如 `./logs/exec_{id}.log`），并保留一份指针。
  2. 此期间，SQLite 的 I/O 为 `0`。
  3. 前端界面通过长轮询或特定 `/api/executions/:id/logstream` 接口增量读取该文本文件。
  4. **进程结束后**，再将整个文件读取并一次性回写到 SQLite 的 `execution_logs`，然后删除物理临时文件。
- 这个方案与当前“阻塞执行完成后一次性写入”在数据库层面的影响**完全等价**，彻底杜绝了锁表和主程性能倒退的风险。

## 架构拆解 (IR)

### 阶段一：[执行器底层改造] 日志双轨与取消句柄 (约30行)
- **目标**: `executor/shell_unix.go` 及 `windows.go`
  - 将 `cmd.Stdout = &bytes.Buffer` 改造为写入临时文件 `os.Create(fmt.Sprintf("logs/%d.log", executionID))`。
  - 在全局/局部维持一个 `var RunningTaskCancels = sync.Map{}`，注入 `context.CancelFunc`。
- **出口**: 底层执行器具备了中途 `cancel()` 的能力，且文件落地。

### 阶段二：[路由层] 暴露干预接口 (约15行)
- **目标**: 新增路由组。
  - `GET /api/executions/:id/stream` - 返回当前的增量日志行（利用 `Seek`）。
  - `POST /api/executions/:id/kill` - 从 `sync.Map` 找到 cancel，调用之。

### 阶段三：[UI/UX Pro Max] Github Actions 级体验 (约40行)
- **目标**: `web/src/views/ExecutionLogs.vue` (或内嵌的抽屉日志区)
  - 风格参考：全黑底色，`font-mono`，带绿色/黄色语法高亮的“终端面板”。
  - **头部状态栏**: “🟢 Running” (带动画 pulsing)，右侧固定一个红色的 `[✖ Cancel Run]` 按钮。
  - **自动滚动**: 监听日志内容变化，始终平滑滚动至最底部。
  - **交互纪律**: 必须含有 `cursor-pointer` 和禁用态防误触 (`loading-buttons` rule)。

## 需要您确认的 Open Questions
1. 增量日志的读取，您接受前端每隔 1-2 秒发起一次 `GET` 请求（长轮询），还是期望引入 `WebSocket`？（推荐 HTTP 长轮询，改动极小且性能对当前场景完全足够，不会引入 WS 断线重连的心智负担）
2. 临时日志文件暂存目录，您是否同意默认创建在应用根目录的 `./data/logs/` 下？

> [!IMPORTANT]
> 此为核心模块修改，修改过程必须严格执行 TDD 的 Red-Green-Refactor 流程。请确认上述两个问题。
