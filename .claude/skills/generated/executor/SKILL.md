---
name: executor
description: "Skill for the Executor area of cronix. 5 symbols across 4 files."
---

# Executor

5 symbols | 4 files | Cohesion: 80%

## When to Use

- Working with code in `internal/`
- Understanding how ExecuteCleanup, ExecuteHealthCheck, ExecuteShell work
- Modifying executor-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `internal/scheduler/executor.go` | runTaskByType, truncate |
| `internal/executor/cleanup.go` | ExecuteCleanup |
| `internal/executor/healthcheck.go` | ExecuteHealthCheck |
| `internal/executor/shell_windows.go` | ExecuteShell |

## Entry Points

Start here when exploring this area:

- **`ExecuteCleanup`** (Function) — `internal/executor/cleanup.go:34`
- **`ExecuteHealthCheck`** (Function) — `internal/executor/healthcheck.go:26`
- **`ExecuteShell`** (Function) — `internal/executor/shell_windows.go:29`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `ExecuteCleanup` | Function | `internal/executor/cleanup.go` | 34 |
| `ExecuteHealthCheck` | Function | `internal/executor/healthcheck.go` | 26 |
| `ExecuteShell` | Function | `internal/executor/shell_windows.go` | 29 |
| `truncate` | Function | `internal/scheduler/executor.go` | 406 |
| `runTaskByType` | Method | `internal/scheduler/executor.go` | 329 |

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
| Circuit | 1 calls |

## How to Explore

1. `gitnexus_context({name: "ExecuteCleanup"})` — see callers and callees
2. `gitnexus_query({query: "executor"})` — find related execution flows
3. Read key files listed above for implementation details
