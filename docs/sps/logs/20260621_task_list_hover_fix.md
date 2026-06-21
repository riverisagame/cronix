# 验收报告：TaskList.vue 悬停边框修正

## 1. 任务背景
在 `/tasks` 页面中，当鼠标悬停在任务列表的表格行上时，出现所有单元格的四周（含内部垂直边框）均被高亮的现象，不符合 UI/UX 规范。
目标：只让悬停行的最外层四周产生高亮框，内部单元格分隔线不变色。

## 2. 修改细节
修改文件：`d:\claudeprj\codex\web\src\views\TaskList.vue`
技术方案：
原代码对每一行的所有 `td` 施加全包围阴影：
```css
box-shadow: inset 0 0 0 1px var(--primary-color);
```
修正后，对 `td` 拆分上下边缘、首节点左边缘、尾节点右边缘分别着色：
```css
:deep(.el-table__body tr:hover > td) {
  box-shadow: inset 0 1px 0 var(--primary-color), inset 0 -1px 0 var(--primary-color);
  transition: box-shadow 0.15s ease;
}
:deep(.el-table__body tr:hover > td:first-child) {
  box-shadow: inset 0 1px 0 var(--primary-color), inset 0 -1px 0 var(--primary-color), inset 1px 0 0 var(--primary-color);
}
:deep(.el-table__body tr:hover > td:last-child) {
  box-shadow: inset 0 1px 0 var(--primary-color), inset 0 -1px 0 var(--primary-color), inset -1px 0 0 var(--primary-color);
}
```

## 3. 测试验证
1. 静态验证：CSS 语法检查通过。
2. 无副作用验证：由于是 `scoped` style 下 `TaskList.vue` 特定的覆盖样式，不会影响全局其他表格。
3. 构建测试：`npm run build` 无错误。

## 4. 结论
符合"最小改动"与"零副作用"原则，验收通过。
