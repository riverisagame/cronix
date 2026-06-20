# Architecture Decision Record: DDD Feasibility Evaluation

## Context
用户提出评估当前系统（Cronix）是否适合引入领域驱动设计 (DDD)。当前系统采用典型的三层架构（Model, Service, Handler）配合独立的后台调度引擎（Scheduler, Executor）。

## Current State Analysis
1. **模型层 (internal/model)**: 当前是典型的贫血模型 (Anemic Domain Model)，以 `Task` 为例，内部全为 GORM 标签和数据字段，没有任何业务行为。
2. **业务逻辑 (internal/service & internal/scheduler)**: 包含核心规则（如并发限制、DAG 拓扑排序、Daemon 拉起策略、重试机制）。目前这些逻辑高度耦合在 Service 和 Scheduler 的实现方法中。
3. **领域复杂度**: 作为一个调度系统，其复杂度更多偏向于**技术复杂度**（协程管理、进程控制、资源配额），而非纯粹的**业务复杂度**（如电商的订单流转、财务的账单核算）。

## Options
### Option 1: 保持现状（三层架构 + 独立执行器引擎）
- **优点**: 简单直接，开发速度快，代码认知负担低，完美契合目前的 CRUD 需求。
- **缺点**: 随着核心引擎（DAG、Daemon、依赖流转）变复杂，代码会变得臃肿，测试难以与基础设施剥离。

### Option 2: 全面转向 DDD (Hexagonal/Clean Architecture)
- **优点**: 领域逻辑极度纯净，与 GORM 和底层设施解耦。单元测试非常容易写。
- **缺点**: 引入巨大的样板代码（Boilerplate）。需要维护 DO（Domain Object）、PO（Persistent Object）、DTO 的互相转换。对于偏技术驱动的调度器来说，属于严重过度设计（Over-engineering）。

### Option 3: 战术性局部吸收 DDD (Lite-DDD)
- 将核心复杂逻辑（如 DAG 状态机、Task 生命周期、Daemon 重启状态转移）提炼到单纯的 Go 结构体行为中（充血模型），脱离 DB。但外围 CRUD 依然保持三层架构。

## Decision
建议采用 **Option 1** 或未来渐进式向 **Option 3** 演进，强烈反对 **Option 2**（全面转向 DDD）。
遵循 Architecture Skill 的核心原则：`Simplicity is the ultimate sophistication. Start simple. Add complexity ONLY when proven necessary.`
