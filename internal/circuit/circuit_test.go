// ============================================================
// internal/scheduler/circuit_test.go - circuit breaker tests
// ============================================================
package circuit

import (
    "testing"
    "time"
)

func TestCircuitBreakerClosed(t *testing.T) {
    cb := NewCircuitBreaker(3, 60)
    if !cb.Allow() {
        t.Error("should allow in closed state")
    }
    if cb.State() != CircuitClosed {
        t.Error("initial state should be closed")
    }
    t.Log("circuit breaker starts closed")
}

func TestCircuitBreakerOpen(t *testing.T) {
    cb := NewCircuitBreaker(2, 60)
    cb.RecordFailure()
    cb.RecordFailure()
    if cb.Allow() {
        t.Error("should not allow after threshold reached")
    }
    if cb.State() != CircuitOpen {
        t.Errorf("expected open, got %d", cb.State())
    }
    t.Log("circuit breaker opens after threshold failures")
}

func TestCircuitBreakerHalfOpen(t *testing.T) {
    // short cooldown for testing
    cb := NewCircuitBreaker(1, 1)
    cb.RecordFailure()
    // wait for cooldown to expire
    time.Sleep(1100 * time.Millisecond)
    // should allow probe after cooldown
    if !cb.Allow() {
        t.Error("should allow probe after cooldown")
    }
    cb.RecordSuccess()
    if cb.State() != CircuitClosed {
        t.Error("should return to closed after success")
    }
    t.Log("circuit breaker: open -> half-open -> closed on success")
}

func TestCircuitBreakerStayOpen(t *testing.T) {
    cb := NewCircuitBreaker(1, 9999)
    cb.RecordFailure()
    if cb.Allow() {
        t.Error("should not allow with long cooldown")
    }
    t.Log("circuit breaker stays open during cooldown")
}
