# [SCAN] 接口隔离与依赖倒置重构计划 (Nano-scale TDD)

## 1. 背景与目标 (Background & Goals)
当前系统存在物理包级别的强引用：`internal/service` 层内的业务结构体强耦合了 `internal/scheduler` 的具体执行引擎指针 (`*scheduler.Engine`, `*scheduler.DaemonMonitor`)。这不仅导致测试困难（必须启动真实引擎），还埋下了循环依赖的隐患。

目标：在**严格物理零污染、不影响现有任何功能**的前提下，对 `TaskService` 和 `GroupService` 实施纳米级接口隔离（Interface Segregation）。

## 2. 纳米级执行步骤 (Nano-scale Steps)

### 第一阶段：Service 层回调隔离 (Service -> Scheduler)

#### 步骤 1.1: [RED] 定义 Mock 与测试用例
- **目标文件**: `internal/service/task_service_test.go`
- **动作**: 编写一个新的测试 `TestTaskService_InterfaceMock`。在测试内部实现局部的 `MockTaskReloader` 和 `MockDaemonReloader`。
- **断言**: 注入 Mock 引擎到 `TaskService`，触发一次任务更新，验证 Mock 对象的方法是否被正确调用。
- **预期**: 由于 `TaskService` 目前只接受具体的 `*scheduler.Engine`，传入 Mock 对象将会在编译期报错。这就是预期的 `[RED 阶段]` 失败。

#### 步骤 1.2: [GREEN] 定义 Service 侧所需接口
- **目标文件**: `internal/service/interfaces.go` (新建)
- **动作**: 定义出业务层所需的极简行为抽象：
  ```go
  package service
  import "cronix/internal/model"

  type TaskReloader interface {
      UpdateTaskSchedule(task model.Task) error
      RemoveTaskSchedule(id uint)
  }
  type GroupReloader interface {
      UpdateGroupSchedule(group model.TaskGroup) error
      RemoveGroupSchedule(id uint)
  }
  type DaemonReloader interface {
      ReloadDaemon(task model.Task)
  }
  type StatsInvalidator interface {
      InvalidateStatsCache()
  }
  ```

#### 步骤 1.3: [GREEN] 重构 Service 结构体
- **目标文件**: `internal/service/task_service.go`, `internal/service/group_service.go`, `internal/service/execution_service.go`
- **动作**: 将原来强引用的指针替换为接口：
  - `TaskService.Engine *scheduler.Engine` -> `TaskService.Engine TaskReloader`
  - `TaskService.DaemonMon *scheduler.DaemonMonitor` -> `TaskService.DaemonMon DaemonReloader`
  - `GroupService.Engine *scheduler.Engine` -> `GroupService.Engine GroupReloader`
  - `TaskService.ExecSvc *ExecutionService` -> `TaskService.ExecSvc StatsInvalidator`
- **动作**: 运行 `go test ./...`。由于 Go 是隐式接口实现，`cmd/root.go` 注入真实的 `scheduler.Engine` 仍然合法，现有逻辑**完全不被破坏**。

#### 步骤 1.4: [FINISH] 运行验收
- 执行全量 `go test ./...`，确保没有任何回归错误。
