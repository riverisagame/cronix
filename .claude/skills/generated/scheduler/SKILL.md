---
name: scheduler
description: "Skill for the Scheduler area of cronix. 32 symbols across 9 files."
---

# Scheduler

32 symbols | 9 files | Cohesion: 78%

## When to Use

- Working with code in `internal/`
- Understanding how NewExecutor, NewEngine, NewExecutionService work
- Modifying scheduler-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `internal/scheduler/executor.go` | NewExecutor, RunGroup, RunTaskNow, handleTrigger, buildDAG (+6) |
| `internal/scheduler/engine.go` | SetGroupTrigger, NewEngine, Start, Stop, TriggerChan |
| `internal/scheduler/dag_test.go` | TestDAGNoCycle, TestDAGCycleDetection, TestDAGSelfDependency, TestDAGLinearChain, TestDAGDiamond |
| `internal/scheduler/dag.go` | TopologicalSort, NewDAG, AddEdge, hasCycle |
| `internal/handler/group.go` | GetGroup, RunGroup |
| `internal/service/group_service.go` | GetGroup, GetGroupMembers |
| `internal/handler/task.go` | RunTask |
| `cmd/root.go` | runServe |
| `internal/service/execution_service.go` | NewExecutionService |

## Entry Points

Start here when exploring this area:

- **`NewExecutor`** (Function) — `internal/scheduler/executor.go:36`
- **`NewEngine`** (Function) — `internal/scheduler/engine.go:36`
- **`NewExecutionService`** (Function) — `internal/service/execution_service.go:26`
- **`NewDAG`** (Function) — `internal/scheduler/dag.go:20`
- **`TestDAGNoCycle`** (Function) — `internal/scheduler/dag_test.go:15`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `NewExecutor` | Function | `internal/scheduler/executor.go` | 36 |
| `NewEngine` | Function | `internal/scheduler/engine.go` | 36 |
| `NewExecutionService` | Function | `internal/service/execution_service.go` | 26 |
| `NewDAG` | Function | `internal/scheduler/dag.go` | 20 |
| `TestDAGNoCycle` | Function | `internal/scheduler/dag_test.go` | 15 |
| `TestDAGCycleDetection` | Function | `internal/scheduler/dag_test.go` | 60 |
| `TestDAGSelfDependency` | Function | `internal/scheduler/dag_test.go` | 86 |
| `TestDAGLinearChain` | Function | `internal/scheduler/dag_test.go` | 104 |
| `TestDAGDiamond` | Function | `internal/scheduler/dag_test.go` | 142 |
| `GetGroup` | Method | `internal/handler/group.go` | 44 |
| `RunGroup` | Method | `internal/handler/group.go` | 101 |
| `SetGroupTrigger` | Method | `internal/scheduler/engine.go` | 53 |
| `RunGroup` | Method | `internal/scheduler/executor.go` | 435 |
| `GetGroup` | Method | `internal/service/group_service.go` | 24 |
| `GetGroupMembers` | Method | `internal/service/group_service.go` | 81 |
| `RunTask` | Method | `internal/handler/task.go` | 212 |
| `TopologicalSort` | Method | `internal/scheduler/dag.go` | 118 |
| `RunTaskNow` | Method | `internal/scheduler/executor.go` | 150 |
| `Start` | Method | `internal/scheduler/engine.go` | 59 |
| `Stop` | Method | `internal/scheduler/engine.go` | 65 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `RunGroup → ShellResult` | cross_community | 6 |
| `RunGroup → Allow` | cross_community | 6 |
| `RunGroup → RecordFailure` | cross_community | 6 |
| `RunGroup → CleanupResult` | cross_community | 6 |
| `RunGroup → HealthCheckResult` | cross_community | 6 |
| `RunTask → ShellResult` | cross_community | 6 |
| `RunTask → Allow` | cross_community | 6 |
| `RunTask → RecordFailure` | cross_community | 6 |
| `RunTask → CleanupResult` | cross_community | 6 |
| `NewExecutor → ShellResult` | cross_community | 6 |

## Connected Areas

| Area | Connections |
|------|-------------|
| Config | 3 calls |
| Database | 2 calls |
| Handler | 1 calls |
| Executor | 1 calls |

## How to Explore

1. `gitnexus_context({name: "NewExecutor"})` — see callers and callees
2. `gitnexus_query({query: "scheduler"})` — find related execution flows
3. Read key files listed above for implementation details
