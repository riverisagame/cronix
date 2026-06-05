---
name: notify
description: "Skill for the Notify area of cronix. 4 symbols across 1 files."
---

# Notify

4 symbols | 1 files | Cohesion: 100%

## When to Use

- Working with code in `internal/`
- Understanding how Start work
- Modifying notify-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `internal/notify/notifier.go` | Start, send, sendWebhook, sendEmail |

## Entry Points

Start here when exploring this area:

- **`Start`** (Method) — `internal/notify/notifier.go:55`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `Start` | Method | `internal/notify/notifier.go` | 55 |
| `send` | Method | `internal/notify/notifier.go` | 67 |
| `sendWebhook` | Method | `internal/notify/notifier.go` | 78 |
| `sendEmail` | Method | `internal/notify/notifier.go` | 111 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `Start → SendWebhook` | intra_community | 3 |
| `Start → SendEmail` | intra_community | 3 |

## How to Explore

1. `gitnexus_context({name: "Start"})` — see callers and callees
2. `gitnexus_query({query: "notify"})` — find related execution flows
3. Read key files listed above for implementation details
