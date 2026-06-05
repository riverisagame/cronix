---
name: service
description: "Skill for the Service area of cronix. 13 symbols across 3 files."
---

# Service

13 symbols | 3 files | Cohesion: 68%

## When to Use

- Working with code in `internal/`
- Understanding how TestGroupCRUD, TestGroupValidation, TestGroupMembers work
- Modifying service-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `internal/service/group_service_test.go` | setupGroupTestDB, TestGroupCRUD, TestGroupValidation, seedTasks, TestGroupMembers |
| `internal/handler/group.go` | CreateGroup, UpdateGroup, DeleteGroup, SetMembers |
| `internal/service/group_service.go` | CreateGroup, UpdateGroup, DeleteGroup, SetGroupMembers |

## Entry Points

Start here when exploring this area:

- **`TestGroupCRUD`** (Function) — `internal/service/group_service_test.go:47`
- **`TestGroupValidation`** (Function) — `internal/service/group_service_test.go:127`
- **`TestGroupMembers`** (Function) — `internal/service/group_service_test.go:88`
- **`CreateGroup`** (Method) — `internal/handler/group.go:31`
- **`UpdateGroup`** (Method) — `internal/handler/group.go:58`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `TestGroupCRUD` | Function | `internal/service/group_service_test.go` | 47 |
| `TestGroupValidation` | Function | `internal/service/group_service_test.go` | 127 |
| `TestGroupMembers` | Function | `internal/service/group_service_test.go` | 88 |
| `CreateGroup` | Method | `internal/handler/group.go` | 31 |
| `UpdateGroup` | Method | `internal/handler/group.go` | 58 |
| `CreateGroup` | Method | `internal/service/group_service.go` | 32 |
| `UpdateGroup` | Method | `internal/service/group_service.go` | 46 |
| `DeleteGroup` | Method | `internal/handler/group.go` | 72 |
| `SetMembers` | Method | `internal/handler/group.go` | 85 |
| `DeleteGroup` | Method | `internal/service/group_service.go` | 59 |
| `SetGroupMembers` | Method | `internal/service/group_service.go` | 91 |
| `setupGroupTestDB` | Function | `internal/service/group_service_test.go` | 14 |
| `seedTasks` | Function | `internal/service/group_service_test.go` | 34 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `CreateGroup → GroupTrigger` | cross_community | 4 |
| `UpdateGroup → GroupTrigger` | cross_community | 4 |
| `DeleteGroup → GroupTrigger` | cross_community | 4 |
| `UpdateGroup → TaskGroup` | intra_community | 3 |
| `DeleteGroup → Task` | intra_community | 3 |
| `DeleteGroup → GroupExecutionLog` | intra_community | 3 |
| `DeleteGroup → TaskGroup` | intra_community | 3 |
| `SetMembers → Task` | intra_community | 3 |

## Connected Areas

| Area | Connections |
|------|-------------|
| Scheduler | 5 calls |
| Handler | 3 calls |

## How to Explore

1. `gitnexus_context({name: "TestGroupCRUD"})` — see callers and callees
2. `gitnexus_query({query: "service"})` — find related execution flows
3. Read key files listed above for implementation details
