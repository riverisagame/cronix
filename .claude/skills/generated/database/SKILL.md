---
name: database
description: "Skill for the Database area of cronix. 5 symbols across 3 files."
---

# Database

5 symbols | 3 files | Cohesion: 80%

## When to Use

- Working with code in `internal/`
- Understanding how Init, Close, TestInit work
- Modifying database-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `internal/database/database.go` | Init, Close |
| `internal/database/database_test.go` | TestInit, TestTaskCRUD |
| `cmd/logs.go` | runLogs |

## Entry Points

Start here when exploring this area:

- **`Init`** (Function) — `internal/database/database.go:77`
- **`Close`** (Function) — `internal/database/database.go:201`
- **`TestInit`** (Function) — `internal/database/database_test.go:12`
- **`TestTaskCRUD`** (Function) — `internal/database/database_test.go:43`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `Init` | Function | `internal/database/database.go` | 77 |
| `Close` | Function | `internal/database/database.go` | 201 |
| `TestInit` | Function | `internal/database/database_test.go` | 12 |
| `TestTaskCRUD` | Function | `internal/database/database_test.go` | 43 |
| `runLogs` | Function | `cmd/logs.go` | 91 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `RunServe → Config` | cross_community | 3 |
| `RunServe → Task` | cross_community | 3 |
| `RunServe → TaskDep` | cross_community | 3 |
| `RunServe → TaskGroup` | cross_community | 3 |
| `RunLogs → Validate` | cross_community | 3 |
| `RunLogs → GenerateJWTSecret` | cross_community | 3 |
| `RunLogs → Config` | intra_community | 3 |
| `RunLogs → Task` | intra_community | 3 |
| `RunLogs → TaskDep` | intra_community | 3 |
| `RunLogs → TaskGroup` | intra_community | 3 |

## Connected Areas

| Area | Connections |
|------|-------------|
| Config | 1 calls |

## How to Explore

1. `gitnexus_context({name: "Init"})` — see callers and callees
2. `gitnexus_query({query: "database"})` — find related execution flows
3. Read key files listed above for implementation details
