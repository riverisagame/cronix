# 纳米级计划：代码目录 DDD 化重构 (第一阶段)

## 目标
将原有的 `internal/` 扁平化架构改造为标准的 DDD 洋葱架构分层（仅作目录迁移与包名引用替换，绝对不修改任何函数的业务逻辑）。

## 影响范围 (Blast Radius)
- **风险等级**：CRITICAL（涉及超过 100 个文件的依赖引用替换）
- **变更方式**：自动化文本替换，纯物理路径调整，不涉及任何运行时内存模型或数据库结构的变动。

## 原子化执行步骤 (Atomic Steps)

### Step 1: 创建 DDD 物理分层目录
**动作**：
```bash
mkdir -p internal/domain
mkdir -p internal/application
mkdir -p internal/infrastructure
mkdir -p internal/interfaces
```

### Step 2: 迁移 Package 到对应领域 (git mv)
**动作**：
- **领域层**：`internal/model` -> `internal/domain/model`
- **应用层**：
  - `internal/service` -> `internal/application/service`
  - `internal/executor` -> `internal/application/executor`
  - `internal/scheduler` -> `internal/application/scheduler`
- **基础设施层**：
  - `internal/database` -> `internal/infrastructure/database`
  - `internal/cache` -> `internal/infrastructure/cache`
  - `internal/notify` -> `internal/infrastructure/notify`
  - `internal/circuit` -> `internal/infrastructure/circuit`
  - `internal/config` -> `internal/infrastructure/config`
- **接口适配层**：
  - `internal/handler` -> `internal/interfaces/handler`
  - `internal/router` -> `internal/interfaces/router`
  - `internal/middleware` -> `internal/interfaces/middleware`

### Step 3: 全局 Import 路径批量替换
**动作**：遍历全项目所有 `.go` 文件，执行以下字符串精确替换（使用正则或 PowerShell 脚本）：
- `"cronix/internal/model"` -> `"cronix/internal/domain/model"`
- `"cronix/internal/service"` -> `"cronix/internal/application/service"`
- `"cronix/internal/executor"` -> `"cronix/internal/application/executor"`
- `"cronix/internal/scheduler"` -> `"cronix/internal/application/scheduler"`
- `"cronix/internal/database"` -> `"cronix/internal/infrastructure/database"`
- `"cronix/internal/cache"` -> `"cronix/internal/infrastructure/cache"`
- `"cronix/internal/notify"` -> `"cronix/internal/infrastructure/notify"`
- `"cronix/internal/circuit"` -> `"cronix/internal/infrastructure/circuit"`
- `"cronix/internal/config"` -> `"cronix/internal/infrastructure/config"`
- `"cronix/internal/handler"` -> `"cronix/internal/interfaces/handler"`
- `"cronix/internal/router"` -> `"cronix/internal/interfaces/router"`
- `"cronix/internal/middleware"` -> `"cronix/internal/interfaces/middleware"`

### Step 4: 清理与重新编译
**动作**：
- 执行 `go mod tidy`
- 执行 `go fmt ./...`
- 执行 `go build`，确认编译无误。
- 执行所有的现存测试用例，确保无任何破坏。

## 验收标准
- `internal/` 目录下只有 `domain`, `application`, `infrastructure`, `interfaces` 4个子目录。
- 所有应用构建成功，测试用例 `PASS`。
