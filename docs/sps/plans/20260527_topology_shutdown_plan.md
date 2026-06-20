# 依赖拓扑图与优雅退出优化纳米级执行计划 - 2026-05-27

本计划旨在通过前端原生 SVG 渲染 DAG 拓扑依赖关系，并对后端的优雅退出链条进行多维重构，确保系统的视觉掌控力与并发退出的数据安全。

## 1. 变更文件与受影响范围

| 文件路径 | 变更类型 | 影响范围 | 预计代码行数 |
| --- | --- | --- | --- |
| `internal/model/task.go` | [MODIFY] | Task 结构体，增加非持久化依赖 ID 数组字段 | ~5 行 |
| `internal/service/task_service.go` | [MODIFY] | `ListTasks` 填充每个任务的前置依赖 ID | ~15 行 |
| `web/src/views/TaskList.vue` | [MODIFY] | 提供 Topology / Table 视图切换，用原生 SVG 动态渲染分层拓扑图并处理交互 | ~120 行 |
| `cmd/root.go` | [MODIFY] | 重构退出处理机制，依次优雅关闭 HTTP、Cron 定时引擎、Ants 线程池和 SQLite 物理连接 | ~30 行 |

---

## 2. 纳米级执行步骤

每个子任务改动量限制在最小范围，保障系统稳定。

### 第一步：后端依赖关联字段与批量预载
* **[S1.1]** 更改 `internal/model/task.go`：在 `Task` 结构体底部追加 `DependsOnIDs []uint \`gorm:"-" json:"depends_on_ids,omitempty"\``。
* **[S1.2]** 更改 `internal/service/task_service.go`：在 `ListTasks` 尾部，如果 `tasks` 数组不为空，则通过 `s.DB.Where("task_id IN ?", taskIDs).Find(&deps)` 一次性获取所有相关的依赖，在内存中进行 Group 并填充给各个任务的 `DependsOnIDs`。

### 第二步：前端 SVG 分层拓扑网络
* **[S2.1]** 更改 `web/src/views/TaskList.vue`：在页面顶部操作栏增加一个“Topology View”切换按钮。
* **[S2.2]** 更改 `web/src/views/TaskList.vue`：使用 `<svg>` 代替 `el-table` 当处于拓扑视图时。
* **[S2.3]** 更改 `web/src/views/TaskList.vue`：在 `<script setup>` 中编写一个拓扑分层布局算法。它将任务节点分为不同的层级（基于依赖深度），并动态计算各节点在 SVG 内的 $X$ 和 $Y$ 坐标。
* **[S2.4]** 更改 `web/src/views/TaskList.vue`：用 `<rect>` 或 `<g>` 渲染磨砂玻璃节点，用带有荧光蓝/绿的 `<path d="..."></path>` 和箭头标记绘制依赖连接线，并在悬浮时高亮显示。

### 第三步：多维优雅退出机制 (Graceful Shutdown)
* **[S3.1]** 更改 `cmd/root.go`：将 `r.Run(addr)` 替换为手动实例化的 `srv := &http.Server{ Addr: addr, Handler: r }`，并放入协程异步监听。
* **[S3.2]** 更改 `cmd/root.go`：在收到 SIGINT/SIGTERM 中断信号时，主协程顺序阻塞地执行：关闭 `srv`、取消 context、停止调度引擎 `engine`、释放 ants 线程池，最后获取底层的 `sql.DB` 物理实例并调用 `Close()` 动作。

---

## 3. 验证方案

1. **编译打包验证**：在 WSL 中执行 `npm run build` 和 `go build -buildvcs=false`，确保无任何编译与构建缺陷。
2. **优雅退出验证**：在启动服务后，触发一次手动任务运行（这会将任务送入 Ants 线程池），然后发送 `SIGINT` (kill -2) 信号给进程。
   - **验证点**：检查控制台输出的日志，确认是否先打印了 `正在优雅关闭服务器...`，并且确认是否在执行日志中看到任务正常完成后进程才彻底退出（而不是硬生生切断）。
3. **集成回归测试**：在 WSL 下运行 `run_tests.sh` 确保 100% 绿灯。
