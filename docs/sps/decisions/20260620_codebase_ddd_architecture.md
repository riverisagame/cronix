# 代码架构 DDD 改造 (SCAN与选型分析)

## 1. 现状：贫血模型与三层架构
目前项目 `internal/` 的包按技术职责划分：
- `model/`: 所有的数据结构（Task, ExecutionLog, Group）。这导致业务实体缺乏行为，退化为单纯的 DTO（贫血模型）。
- `service/`: 大量的过程式逻辑代码集中在此处，且强耦合了数据库事务、缓存操作与外部调用。
- `handler/`: API 端点，直接依赖 `service/`。
- `scheduler/` 和 `executor/`: 核心调度逻辑，与上述包互相调用，存在循环依赖或模块边界模糊的风险。

## 2. 目标结构 (DDD Hexagonal/Onion Architecture)
为了实现“彻底的 DDD 与清晰的文档结构相匹配”，我们提议按以下限界上下文（Bounded Contexts）或标准分层架构重组 `internal/`：

### 领域分层建议（方案选型）

**A. 严格按限界上下文划分 (包即领域)**
- `internal/task/`: 包含 Task 的 Model, Repository, Service (UseCase)。
- `internal/execution/`: 包含运行态逻辑、Executor、Log。
- `internal/scheduler/`: 调度内核。
- *优点*：高度内聚，微服务化准备好。*缺点*：重构跨度极大，解决循环依赖极度痛苦。

**B. 按标准 DDD 分层划分 (推荐)**
- `internal/domain/`: (领域层) 核心实体（`model/` 的进化版）和 Repository 接口。绝对不依赖其他层。
- `internal/application/`: (应用层) 用例、工作流编排（由原 `service/`, `executor/`, `scheduler/` 构成），依赖 `domain/`。
- `internal/infrastructure/`: (基础设施层) `database/` 实现、`cache/` 实现。
- `internal/interfaces/`: (接口层/适配器层) `handler/`, `router/`, `middleware/`。

## 3. 爆炸半径评估 (Blast Radius)
- 目前 `model` 包被所有层深度依赖（2200+ 符号引用）。将 `model` 迁移到 `domain` 会触发全项目数百个文件的 `import` 路径替换。
- `service` 移动到 `application` 同样会导致大量的重构和包名修改。

## 4. 行动建议
按照 GitNexus 审计要求，在正式生成 `[IR] 纳米级计划` 之前，我们需要达成以下共识：
1. **采用哪种方案？**（强烈推荐方案 B：按 DDD 分层，保持当前组件粒度但理清依赖层级）。
2. 我们需要分阶段进行重构。第一阶段仅进行**包路径移动与 Import 修正**，不触碰业务代码细节。
