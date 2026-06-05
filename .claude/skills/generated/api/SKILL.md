---
name: api
description: "Skill for the Api area of cronix. 12 symbols across 2 files."
---

# Api

12 symbols | 2 files | Cohesion: 95%

## When to Use

- Working with code in `web/`
- Understanding how load, clearAllLogs, showDetail work
- Modifying api-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `web/src/api/index.ts` | delete, clearAll, deleteLog, clearTask, list (+4) |
| `web/src/views/ExecutionLogs.vue` | load, clearAllLogs, showDetail |

## Entry Points

Start here when exploring this area:

- **`load`** (Function) — `web/src/views/ExecutionLogs.vue:153`
- **`clearAllLogs`** (Function) — `web/src/views/ExecutionLogs.vue:169`
- **`showDetail`** (Function) — `web/src/views/ExecutionLogs.vue:180`
- **`delete`** (Method) — `web/src/api/index.ts:73`
- **`clearAll`** (Method) — `web/src/api/index.ts:109`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `load` | Function | `web/src/views/ExecutionLogs.vue` | 153 |
| `clearAllLogs` | Function | `web/src/views/ExecutionLogs.vue` | 169 |
| `showDetail` | Function | `web/src/views/ExecutionLogs.vue` | 180 |
| `delete` | Method | `web/src/api/index.ts` | 73 |
| `clearAll` | Method | `web/src/api/index.ts` | 109 |
| `deleteLog` | Method | `web/src/api/index.ts` | 110 |
| `clearTask` | Method | `web/src/api/index.ts` | 112 |
| `list` | Method | `web/src/api/index.ts` | 44 |
| `get` | Method | `web/src/api/index.ts` | 58 |
| `getDeps` | Method | `web/src/api/index.ts` | 93 |
| `getLog` | Method | `web/src/api/index.ts` | 111 |
| `stats` | Method | `web/src/api/index.ts` | 125 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `ClearAllLogs → Delete` | intra_community | 3 |
| `ClearGroupLogs → Delete` | cross_community | 3 |
| `ShowDetail → Get` | intra_community | 3 |

## How to Explore

1. `gitnexus_context({name: "load"})` — see callers and callees
2. `gitnexus_query({query: "api"})` — find related execution flows
3. Read key files listed above for implementation details
