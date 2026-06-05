---
name: config
description: "Skill for the Config area of cronix. 10 symbols across 6 files."
---

# Config

10 symbols | 6 files | Cohesion: 81%

## When to Use

- Working with code in `internal/`
- Understanding how GenerateJWTSecret, Load, TestLoadConfig work
- Modifying config-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `internal/config/config.go` | GenerateJWTSecret, Load, Validate, GetJWTSecret, SaveConfig |
| `internal/config/config_test.go` | TestLoadConfig |
| `internal/handler/auth.go` | Login |
| `internal/middleware/auth.go` | AuthMiddleware |
| `internal/router/router.go` | SetupRouter |
| `internal/handler/log.go` | UpdateSettings |

## Entry Points

Start here when exploring this area:

- **`GenerateJWTSecret`** (Function) — `internal/config/config.go:286`
- **`Load`** (Function) — `internal/config/config.go:313`
- **`TestLoadConfig`** (Function) — `internal/config/config_test.go:25`
- **`GetJWTSecret`** (Function) — `internal/config/config.go:466`
- **`AuthMiddleware`** (Function) — `internal/middleware/auth.go:20`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `GenerateJWTSecret` | Function | `internal/config/config.go` | 286 |
| `Load` | Function | `internal/config/config.go` | 313 |
| `TestLoadConfig` | Function | `internal/config/config_test.go` | 25 |
| `GetJWTSecret` | Function | `internal/config/config.go` | 466 |
| `AuthMiddleware` | Function | `internal/middleware/auth.go` | 20 |
| `SetupRouter` | Function | `internal/router/router.go` | 29 |
| `SaveConfig` | Function | `internal/config/config.go` | 494 |
| `Validate` | Method | `internal/config/config.go` | 438 |
| `Login` | Method | `internal/handler/auth.go` | 52 |
| `UpdateSettings` | Method | `internal/handler/log.go` | 80 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `RunServe → Validate` | cross_community | 3 |
| `RunServe → GenerateJWTSecret` | cross_community | 3 |
| `RunLogs → Validate` | cross_community | 3 |
| `RunLogs → GenerateJWTSecret` | cross_community | 3 |
| `SetupRouter → GetJWTSecret` | intra_community | 3 |

## How to Explore

1. `gitnexus_context({name: "GenerateJWTSecret"})` — see callers and callees
2. `gitnexus_query({query: "config"})` — find related execution flows
3. Read key files listed above for implementation details
