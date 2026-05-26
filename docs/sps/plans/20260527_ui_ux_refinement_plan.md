# 极简工业控制台视觉重构纳米级执行计划 - 2026-05-27

本计划旨在通过磨砂玻璃高光边框、等宽代码数显、雷达呼吸指示灯等，对 Cronix 前端视觉界面进行重构，将其打造成富有精致科技感的“极简工业控制台”。

## 1. 变更文件与受影响范围

| 文件路径 | 变更类型 | 影响范围 |
| --- | --- | --- |
| `web/src/App.vue` | [MODIFY] | 注入 CSS 变量与全局样式：`.glass-card`, `.status-dot`（雷达呼吸效果） |
| `web/src/views/Dashboard.vue` | [MODIFY] | 4个统计卡片、进度卡片、执行记录卡片应用磨砂玻璃，数字部分配置等宽字体 |
| `web/src/views/TaskList.vue` | [MODIFY] | 任务管理卡片玻璃化，列表任务名旁嵌入呼吸指示圆点 |
| `web/src/views/GroupList.vue` | [MODIFY] | 任务组卡片玻璃化 |
| `web/src/views/ExecutionLogs.vue` | [MODIFY] | 执行日志卡片玻璃化 |
| `web/src/views/Settings.vue` | [MODIFY] | 设置卡片玻璃化 |
| `web/src/views/Login.vue` | [MODIFY] | 登录卡片玻璃化，增加底部激光高光背景 |

---

## 2. 纳米级执行步骤

代码修改限制在 10-20 行之内，逐步平滑更替。

### 第一步：注入全局视觉系统 (Design Token)
* **[S1.1]** 更改 `web/src/App.vue`：在底部的 `<style>` 中注入 `:root` CSS 变量（极夜黑背景、磨砂卡片背景、激光蓝高光和等宽代码字体）。
* **[S1.2]** 更改 `web/src/App.vue`：在底部的 `<style>` 中，全局重定义 `body` 背景色为 `var(--cyber-bg)`，并注入 `.glass-card`、`.glass-card:hover`、`.status-dot` 等样式。
* **[S1.3]** 更改 `web/src/App.vue`：定义 `@keyframes pulse-green` 实现电子绿雷达脉冲扩散。

### 第二步：Dashboard 卡片玻璃化与代码数显
* **[S2.1]** 更改 `web/src/views/Dashboard.vue`：在行布局 `el-row` 内，将所有 `el-card` 追加 `class="glass-card"`，并把数字显示区域改为等宽字体 `font-family: var(--cyber-font-mono)`。
* **[S2.2]** 更改 `web/src/views/Dashboard.vue`：优化成功率环和最近记录卡片，应用 `glass-card`，并将表格中时间与输出列应用等宽字体风格。

### 第三步：任务列表及呼吸灯微动效
* **[S3.1]** 更改 `web/src/views/TaskList.vue`：将顶层卡片挂载 `class="glass-card"`。
* **[S3.2]** 更改 `web/src/views/TaskList.vue`：在 Task Name 列插槽中，插入状态呼吸点：`<span class="status-dot" :class="row.enabled ? 'active' : 'inactive'"></span>`。
* **[S3.3]** 更改 `web/src/views/TaskList.vue`：对 Cron 列、ID 列、Type 标签及动作按钮应用细化的样式重塑。

### 第四步：其余后台页面统一磨砂玻璃化
* **[S4.1]** 更改 `web/src/views/GroupList.vue`：卡片应用 `glass-card`。
* **[S4.2]** 更改 `web/src/views/ExecutionLogs.vue`：卡片应用 `glass-card`。
* **[S4.3]** 更改 `web/src/views/Settings.vue`：卡片应用 `glass-card`。

### 第五步：登录页面 wow 级第一印象重构
* **[S5.1]** 更改 `web/src/views/Login.vue`：修改登录页面的背景，使其呈现极夜渐变微光，并将登录框卡片改为极致磨砂半透明 `glass-card`。

---

## 3. 验证方案

1. **构建校验**：运行 `npm run build` 打包前端静态资源，必须 100% SUCCESS 且无任何 Lint / TS 报错。
2. **集成与压力测试验证**：重新编译 Go 二进制并拷入沙箱，在 WSL 中执行 `run_tests.sh`（执行 test-suite, prod-test, stress-test），确保 API 数据及鉴权拦截流程完好无损。
