# 纳米级执行计划 (Nano-level Execution Plan)
Date: 2026-06-11
Task ID: Task-15
Module: Backend (Model), Frontend (UI/UX)
Status: [IR] Planned

## 1. 目标 (Goals)
根据架构和体验专家的评估建议，在**绝对不影响现有核心调度逻辑**的前提下，落实以下两项改进：
1. **[Backend]** 为 `Task` 模型引入状态机防线（State Machine），防止非法状态流转。
2. **[Frontend]** 为任务列表引入空状态引导（Empty States），提升用户体验。

## 2. 影响范围分析 (Blast Radius)
- **风险等级**：极低 (Low)
- **现有功能影响**：零侵入。后端的流转拦截目前仅作为前置校验，失败直接返回错误。前端仅在数据为空时展示，不影响现有表格渲染。

## 3. 详细执行步骤 (Execution Steps)

### Step 1: [Backend - RED] 编写状态机单元测试
- **File**: `internal/model/task_test.go` (新建或修改)
- **Action**: 编写 `TestTaskStateTransition`，验证 `CanTransitionTo` 和 `TransitionTo` 的行为。
  - 必须测试：`pending -> running` (通过)
  - 必须测试：`running -> success` (通过)
  - 必须测试：`success -> running` (失败，终态不可变)
  - 必须测试：`failed -> pending` (通过，重试)

### Step 2: [Backend - GREEN] 实现状态机逻辑
- **File**: `internal/model/task.go`
- **Action**:
  - 新增常量定义合法状态：`StatePending="pending"`, `StateRunning="running"`, 等。
  - 新增方法 `func (t *Task) CanTransitionTo(target string) bool`。
  - 新增方法 `func (t *Task) TransitionTo(target string) error`。

### Step 3: [Frontend - RED/GREEN] 任务列表空状态引导
- **File**: `web/src/views/TaskList.vue`
- **Action**:
  - 定位到 `<el-table :data="tasks">`。
  - 增加 `<template #empty>` 插槽。
  - 在插槽内使用 `<el-empty description="No tasks found. Get started by creating your first task!">`。
  - 在 `el-empty` 内部增加一个 `<el-button type="primary" @click="router.push('/tasks/new')">Create Task</el-button>`。

## 4. 验证计划 (Verification Plan)
- 运行 `go test ./internal/model/... -v`，确保全部状态机流转测试通过。
- 打开 Dashboard，清空所有任务（或模拟后端返回空数据），验证空状态及跳转按钮的可用性。
