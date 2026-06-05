---
name: handler
description: "Skill for the Handler area of cronix. 38 symbols across 7 files."
---

# Handler

38 symbols | 7 files | Cohesion: 95%

## When to Use

- Working with code in `internal/`
- Understanding how CreateTask, UpdateTask, DeleteTask work
- Modifying handler-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `internal/handler/task.go` | validateTask, CreateTask, UpdateTask, DeleteTask, UpdateTaskDeps (+4) |
| `internal/service/execution_service.go` | GetAllLogs, GetDashboardStats, ClearAllLogs, ClearTaskLogs, DeleteLog (+4) |
| `internal/handler/log.go` | GetAllLogs, GetDashboardStats, ClearAllLogs, ClearTaskLogs, DeleteLog (+3) |
| `internal/service/task_service.go` | CreateTask, UpdateTask, DeleteTask, UpdateTaskDeps, ListTasks (+2) |
| `internal/handler/group.go` | ListGroups, GetGroupLogs |
| `internal/service/group_service.go` | ListGroups, GetGroupLogs |
| `internal/scheduler/engine.go` | ReloadAll |

## Entry Points

Start here when exploring this area:

- **`CreateTask`** (Method) — `internal/handler/task.go:108`
- **`UpdateTask`** (Method) — `internal/handler/task.go:153`
- **`DeleteTask`** (Method) — `internal/handler/task.go:201`
- **`UpdateTaskDeps`** (Method) — `internal/handler/task.go:252`
- **`ReloadAll`** (Method) — `internal/scheduler/engine.go:71`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `CreateTask` | Method | `internal/handler/task.go` | 108 |
| `UpdateTask` | Method | `internal/handler/task.go` | 153 |
| `DeleteTask` | Method | `internal/handler/task.go` | 201 |
| `UpdateTaskDeps` | Method | `internal/handler/task.go` | 252 |
| `ReloadAll` | Method | `internal/scheduler/engine.go` | 71 |
| `CreateTask` | Method | `internal/service/task_service.go` | 86 |
| `UpdateTask` | Method | `internal/service/task_service.go` | 105 |
| `DeleteTask` | Method | `internal/service/task_service.go` | 122 |
| `UpdateTaskDeps` | Method | `internal/service/task_service.go` | 155 |
| `ListGroups` | Method | `internal/handler/group.go` | 19 |
| `ListGroups` | Method | `internal/service/group_service.go` | 16 |
| `GetGroupLogs` | Method | `internal/handler/group.go` | 117 |
| `GetGroupLogs` | Method | `internal/service/group_service.go` | 111 |
| `GetAllLogs` | Method | `internal/handler/log.go` | 27 |
| `GetAllLogs` | Method | `internal/service/execution_service.go` | 58 |
| `GetDashboardStats` | Method | `internal/handler/log.go` | 53 |
| `GetDashboardStats` | Method | `internal/service/execution_service.go` | 128 |
| `ClearAllLogs` | Method | `internal/handler/log.go` | 116 |
| `ClearAllLogs` | Method | `internal/service/execution_service.go` | 218 |
| `ClearTaskLogs` | Method | `internal/handler/log.go` | 130 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `CreateTask → GroupTrigger` | intra_community | 4 |
| `CreateGroup → GroupTrigger` | cross_community | 4 |
| `UpdateGroup → GroupTrigger` | cross_community | 4 |
| `DeleteGroup → GroupTrigger` | cross_community | 4 |
| `UpdateTask → GroupTrigger` | intra_community | 4 |
| `DeleteTask → GroupTrigger` | intra_community | 4 |
| `UpdateTaskDeps → GroupTrigger` | intra_community | 4 |
| `ExportLogs → ExecutionLog` | intra_community | 3 |
| `CreateTask → Task` | intra_community | 3 |
| `GetGroupLogs → GroupExecutionLog` | intra_community | 3 |

## How to Explore

1. `gitnexus_context({name: "CreateTask"})` — see callers and callees
2. `gitnexus_query({query: "handler"})` — find related execution flows
3. Read key files listed above for implementation details
