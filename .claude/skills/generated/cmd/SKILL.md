---
name: cmd
description: "Skill for the Cmd area of cronix. 4 symbols across 3 files."
---

# Cmd

4 symbols | 3 files | Cohesion: 100%

## When to Use

- Working with code in `cmd/`
- Understanding how Execute, SetWebDist work
- Modifying cmd-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `cmd/root.go` | Execute, SetWebDist |
| `main.go` | main |
| `embed.go` | init |

## Entry Points

Start here when exploring this area:

- **`Execute`** (Function) — `cmd/root.go:69`
- **`SetWebDist`** (Function) — `cmd/root.go:178`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `Execute` | Function | `cmd/root.go` | 69 |
| `SetWebDist` | Function | `cmd/root.go` | 178 |
| `main` | Function | `main.go` | 37 |
| `init` | Function | `embed.go` | 39 |

## How to Explore

1. `gitnexus_context({name: "Execute"})` — see callers and callees
2. `gitnexus_query({query: "cmd"})` — find related execution flows
3. Read key files listed above for implementation details
