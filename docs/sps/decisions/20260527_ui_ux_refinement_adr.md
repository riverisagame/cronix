# ADR: 前端视觉重构与“极简工业控制台 (Industrial Cyberpunk Console)”美学优化

## 1. 背景与上下文
当前 Cronix 的前端 UI 采用 Element Plus 默认的扁平暗黑配置，显得普通且缺乏专业控制台的科技感，缺乏用户的视觉 wow 效应。
因此，为了贯彻 `/frontend-design` 的独特个性与工艺水准，我们将系统视觉风格重构为 **“极简工业控制台 (Industrial Cyberpunk Console)”** 风格，提升品牌辨识度并优化状态微动效。

## 2. 设计系统 snapshot (Design System Snapshot)

* **美学名称**：极简工业控制台 (Industrial Cyberpunk Console)
* **DFII 评分**：**+17.0** (Aesthetic Impact: 4.5 | Context Fit: 4.5 | Feasibility: 4.5 | Performance: 4.5 − Consistency Risk: 1.0)
* **视觉锚点 (Differentiation Anchor)**：
  - 去掉 Logo 后，用户将通过 **“电子霓虹微光卡片”**、**“高清晰 Monospace 代码数显”**、以及 **“雷达脉冲呼吸指示灯”** 一眼认出这是一个工业级任务调度舱。

### 2.1 颜色故事 (Color Story)
我们将所有 UI 配色统摄在以下 CSS 变量中（写入 `App.vue`）：
```css
:root {
  --cyber-bg: #0c0d12;              /* 极夜黑底色 */
  --cyber-surface: rgba(22, 25, 34, 0.7);  /* 磨砂玻璃卡片背景 */
  --cyber-border: rgba(64, 158, 255, 0.12); /* 半透明边框高光 */
  --cyber-glow-blue: rgba(64, 158, 255, 0.25); /* 激光电镀蓝发光阴影 */
  --cyber-green: #10b981;           /* 电子荧光绿，用于活跃和成功状态 */
  --cyber-glow-green: rgba(16, 185, 129, 0.35); /* 荧光绿呼吸光晕 */
  --cyber-red: #ef4444;             /* 脉冲荧光红，用于故障 */
  --cyber-font-mono: 'JetBrains Mono', 'Fira Code', 'SFMono-Regular', Consolas, monospace;
}
```

### 2.2 视觉与交互机制 (Visual & Motion Mechanics)
1. **磨砂玻璃卡片 (.glass-card)**：
   - 样式：`backdrop-filter: blur(16px); background: var(--cyber-surface); border: 1px solid var(--cyber-border);`
   - 悬浮微交互：在 `:hover` 时 `transform: translateY(-5px) scale(1.01)`，伴随 `box-shadow: 0 12px 30px var(--cyber-glow-blue)`。
   - 性能保护：指定 `will-change: transform, box-shadow`，激活 GPU 硬件渲染加速。
2. **雷达脉冲呼吸灯 (.status-dot)**：
   - 呼吸动效仅限 8x8 像素大小，配合双重脉冲扩散：
   ```css
   @keyframes pulse-green {
     0% {
       transform: scale(0.95);
       box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7);
     }
     70% {
       transform: scale(1.1);
       box-shadow: 0 0 0 8px rgba(16, 185, 129, 0);
     }
     100% {
       transform: scale(0.95);
       box-shadow: 0 0 0 0 rgba(16, 185, 129, 0);
     }
   }
   ```
   - 此动效专用于 `enabled` 状态的任务行，展现系统后台处于“活跃巡检”的生命力。
3. **等宽代码数显 (Monospace Type)**：
   - Dashboard 中的统计数字、最近日志表格中的 Cron 表达式和运行时间，统一配置 `font-family: var(--cyber-font-mono)`。让后台运转的冷冰冰数字呈现极其专业的控制流美学。

## 3. 物理零污染与兼容性安全
所有改动在编译打包阶段保持完全独立。仅作用于 CSS 样式与元素类名，API 数据流与后端接口无任何逻辑变更。
