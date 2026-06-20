# 纳米级计划：前端视觉与体验升级 (UI/UX Pro Max)

## 需求对齐
根据 `/ui-ux-pro-max` 的规则及用户确认的选项 B，我们将在不破坏现有逻辑、不引入冗余依赖的前提下，以极小改动（CSS 与组件属性层面）实现显著的视觉和体验提升。

## 核心优化点
1. **毛玻璃质感 (Glassmorphism)**: 针对 DAG 画布节点和侧边抽屉，引入 `backdrop-filter` 营造高级感空间层级。
2. **状态高对比度 (Status Prominence)**: 调整执行日志的时间线标签颜色，增加饱和度并启用 `effect="dark"` 以形成更强的视觉焦点。
3. **微交互反馈 (Micro-interactions)**: 为所有可交互卡片与按钮补充 `cursor-pointer` 及 `transition`，消除操作生硬感。

## 详细执行计划 (IR)

### 阶段一：[CSS 注入] 样式升级
- **文件**: `web/src/views/TaskList.vue`
- **改动范围** (约 15 行):
  - 定位 `.glass-node-rect`，将其 `fill` 替换为 `rgba(255,255,255,0.7)`，并补充 `backdrop-filter: blur(8px)` (由于 SVG 对 filter 的支持限制，若无效则回退至 SVG原生 `<feGaussianBlur>`)。
  - 为 `el-card` (Timeline 内部) 注入悬停浮起的 CSS 过渡动画：`transition: transform 0.2s, box-shadow 0.2s`。

### 阶段二：[属性调优] 组件参数增强
- **文件**: `web/src/views/TaskList.vue`
- **改动范围** (约 10 行):
  - 修改 `el-tag`（任务类型及状态），增加 `effect="dark"` 或 `effect="plain"` 产生立体对比。
  - 为 Timeline 的 `el-card` 加上 `style="cursor: pointer"` 提升可点击感（即便目前只是阅读）。

## 出口准则
- 无需新增 npm 依赖。
- 修改后运行 Vite 开发服务器或打包验证页面无任何破坏。
- 代码行数变动控制在 30 行以内。
