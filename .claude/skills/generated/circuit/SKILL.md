---
name: circuit
description: "Skill for the Circuit area of cronix. 12 symbols across 3 files."
---

# Circuit

12 symbols | 3 files | Cohesion: 87%

## When to Use

- Working with code in `internal/`
- Understanding how NewCircuitBreaker, TestCircuitBreakerClosed, TestCircuitBreakerOpen work
- Modifying circuit-related functionality

## Key Files

| File | Symbols |
|------|---------|
| `internal/circuit/circuit.go` | NewCircuitBreaker, Allow, RecordSuccess, RecordFailure |
| `internal/circuit/circuit_test.go` | TestCircuitBreakerClosed, TestCircuitBreakerOpen, TestCircuitBreakerHalfOpen, TestCircuitBreakerStayOpen |
| `internal/executor/http_exec.go` | getCircuitBreaker, ExecuteHTTP, applyHTTPAuth, getOAuthToken |

## Entry Points

Start here when exploring this area:

- **`NewCircuitBreaker`** (Function) — `internal/circuit/circuit.go:49`
- **`TestCircuitBreakerClosed`** (Function) — `internal/circuit/circuit_test.go:10`
- **`TestCircuitBreakerOpen`** (Function) — `internal/circuit/circuit_test.go:21`
- **`TestCircuitBreakerHalfOpen`** (Function) — `internal/circuit/circuit_test.go:34`
- **`TestCircuitBreakerStayOpen`** (Function) — `internal/circuit/circuit_test.go:51`

## Key Symbols

| Symbol | Type | File | Line |
|--------|------|------|------|
| `NewCircuitBreaker` | Function | `internal/circuit/circuit.go` | 49 |
| `TestCircuitBreakerClosed` | Function | `internal/circuit/circuit_test.go` | 10 |
| `TestCircuitBreakerOpen` | Function | `internal/circuit/circuit_test.go` | 21 |
| `TestCircuitBreakerHalfOpen` | Function | `internal/circuit/circuit_test.go` | 34 |
| `TestCircuitBreakerStayOpen` | Function | `internal/circuit/circuit_test.go` | 51 |
| `ExecuteHTTP` | Function | `internal/executor/http_exec.go` | 92 |
| `Allow` | Method | `internal/circuit/circuit.go` | 59 |
| `RecordSuccess` | Method | `internal/circuit/circuit.go` | 81 |
| `RecordFailure` | Method | `internal/circuit/circuit.go` | 90 |
| `getCircuitBreaker` | Function | `internal/executor/http_exec.go` | 69 |
| `applyHTTPAuth` | Function | `internal/executor/http_exec.go` | 169 |
| `getOAuthToken` | Function | `internal/executor/http_exec.go` | 216 |

## Execution Flows

| Flow | Type | Steps |
|------|------|-------|
| `RunGroup → Allow` | cross_community | 6 |
| `RunGroup → RecordFailure` | cross_community | 6 |
| `RunTask → Allow` | cross_community | 6 |
| `RunTask → RecordFailure` | cross_community | 6 |
| `NewExecutor → Allow` | cross_community | 6 |
| `NewExecutor → RecordFailure` | cross_community | 6 |
| `Run → Allow` | cross_community | 6 |
| `Run → RecordFailure` | cross_community | 6 |
| `ApplyHTTPAuth → OauthToken` | intra_community | 3 |
| `GetCircuitBreaker → CircuitBreaker` | intra_community | 3 |

## How to Explore

1. `gitnexus_context({name: "NewCircuitBreaker"})` — see callers and callees
2. `gitnexus_query({query: "circuit"})` — find related execution flows
3. Read key files listed above for implementation details
