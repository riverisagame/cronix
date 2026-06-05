---
name: views
description: "Skill for the Views area of cronix. 25 symbols across 7 files."
---

# Views

25 symbols | 7 files | Cohesion: 98%

## When to Use

- Working with code in `web/`
- Understanding how load, runGroup, deleteGroup work
- Modifying views-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `web/src/api/index.ts` | clearGroup, list, setMembers, login, update (+1) |
| `web/src/views/GroupEdit.vue` | parseCronField, cronNext, pad, loadAllTasks, saveMembers |
| `web/src/views/GroupList.vue` | load, runGroup, deleteGroup, clearGroupLogs |
| `web/src/views/TaskList.vue` | load, runTask, deleteTask, toggleTask |
| `web/src/views/TaskEdit.vue` | parseCronField, cronNext, pad, save |
| `web/src/views/Login.vue` | handleLogin |
| `web/src/views/Settings.vue` | save |

## Entry Points

Start here when exploring this area:

- **`load`** (Function) — `web/src/views/GroupList.vue:88`
- **`runGroup`** (Function) — `web/src/views/GroupList.vue:96`
- **`deleteGroup`** (Function) — `web/src/views/GroupList.vue:103`
- **`clearGroupLogs`** (Function) — `web/src/views/GroupList.vue:122`
- **`load`** (Function) — `web/src/views/TaskList.vue:345`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `load` | Function | `web/src/views/GroupList.vue` | 88 |
| `runGroup` | Function | `web/src/views/GroupList.vue` | 96 |
| `deleteGroup` | Function | `web/src/views/GroupList.vue` | 103 |
| `clearGroupLogs` | Function | `web/src/views/GroupList.vue` | 122 |
| `load` | Function | `web/src/views/TaskList.vue` | 345 |
| `runTask` | Function | `web/src/views/TaskList.vue` | 371 |
| `deleteTask` | Function | `web/src/views/TaskList.vue` | 383 |
| `toggleTask` | Function | `web/src/views/TaskList.vue` | 394 |
| `parseCronField` | Function | `web/src/views/GroupEdit.vue` | 197 |
| `cronNext` | Function | `web/src/views/GroupEdit.vue` | 206 |
| `pad` | Function | `web/src/views/GroupEdit.vue` | 234 |
| `loadAllTasks` | Function | `web/src/views/GroupEdit.vue` | 277 |
| `saveMembers` | Function | `web/src/views/GroupEdit.vue` | 308 |
| `parseCronField` | Function | `web/src/views/TaskEdit.vue` | 372 |
| `cronNext` | Function | `web/src/views/TaskEdit.vue` | 394 |
| `pad` | Function | `web/src/views/TaskEdit.vue` | 435 |
| `handleLogin` | Function | `web/src/views/Login.vue` | 137 |
| `save` | Function | `web/src/views/Settings.vue` | 223 |
| `save` | Function | `web/src/views/TaskEdit.vue` | 483 |
| `clearGroup` | Method | `web/src/api/index.ts` | 113 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `ClearGroupLogs → Delete` | cross_community | 3 |
| `ClearGroupLogs → List` | intra_community | 3 |
| `RunGroup → List` | intra_community | 3 |
| `DeleteGroup → List` | intra_community | 3 |

## Connected Areas

| Area | Connections |
|------|-------------|
| Api | 1 calls |

## How to Explore

1. `gitnexus_context({name: "load"})` — see callers and callees
2. `gitnexus_query({query: "views"})` — find related execution flows
3. Read key files listed above for implementation details
