# [IR] CEX 风格落地页优化计划 (Login.vue)

## 1. 业务与需求对齐
目标：将原有的 `Login.vue` 升级重构为一个“专业中心化交易所（CEX）风格”的落地页，包含暗色模式、虚构的市场交易对、交易界面预览插图、安全特性说明，以及一个完整的注册/登录交互区。
核心：保留原有的 `authAPI.login` 业务逻辑，视觉效果大幅提升。

## 2. 纳米级执行步骤

### 第一步：更新全局样式与字体 (App.vue)
- **文件路径**: `web/src/App.vue`
- **改动逻辑**: 
  - 引入 Google Fonts: `Orbitron` 和 `Exo 2`。
  - 在 `:root` 中加入 CEX 主题变量：
    - `--cex-bg-dark`: `#0F172A`
    - `--cex-primary-gold`: `#F59E0B`
    - `--cex-accent-purple`: `#8B5CF6`
  - 修改 `body` 默认背景为深蓝灰暗色。

### 第二步：重构落地页组件结构 (Login.vue)
- **文件路径**: `web/src/views/Login.vue`
- **改动逻辑**:
  - **Hero Section**: 页面左侧。大字号 `Orbitron` 标题 "Trade the Future, Task the Present"。
  - **Market Pairs**: 增加一个跑马灯或卡片组，显示 BTC/USDT、ETH/USDT 等模拟行情数据。
  - **Security & Trust**: 列出 "Bank-Grade Security", "99.9% Uptime", "End-to-End Encryption" 等特性，辅以 Lucide 或 Heroicons 图标。
  - **Registration/Login Form**: 页面右侧悬浮，应用 `backdrop-filter: blur(12px)` 的 Glassmorphism (毛玻璃) 卡片。集成现有的 `username` / `password` 输入框与提交按钮。
  - **交互**: 悬停状态平滑过渡 (150ms-300ms)。

### 第三步：验证与测试
- 确保表单依旧正常提交，`handleLogin` 能够触发 `router.push('/')`。
- 响应式检查：左侧内容在小屏幕隐藏，右侧登录表单居中。

## 3. 出口
以上为纳米级重构计划，等待批准执行。
