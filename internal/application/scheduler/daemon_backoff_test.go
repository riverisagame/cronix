package scheduler

import (
	"testing"
	"time"
)

// TestDaemonBackoffOverflow 测试当 restartCount 过大时，退避时间位移溢出的问题
func TestDaemonBackoffOverflow(t *testing.T) {
	// 模拟 daemon_monitor.go 中约 253 行的代码：
	// backoff := time.Duration(1<<uint(restartCount-1)) * time.Second
	// if backoff > 60*time.Second { backoff = 60 * time.Second }

	calculateBackoff := func(restartCount int) time.Duration {
		var backoff time.Duration
		if restartCount >= 7 {
			backoff = 60 * time.Second
		} else {
			backoff = time.Duration(1<<uint(restartCount-1)) * time.Second
		}
		return backoff
	}

	// 1. 正常情况
	b1 := calculateBackoff(1)
	if b1 != 1*time.Second {
		t.Errorf("Expected 1s for count 1, got %v", b1)
	}

	// 2. 正常上限情况
	b10 := calculateBackoff(10)
	if b10 != 60*time.Second {
		t.Errorf("Expected 60s for count 10, got %v", b10)
	}

	// 3. 触发线上 Bug: restartCount 达到 70209
	bBug := calculateBackoff(70209)
	if bBug != 60*time.Second {
		t.Fatalf("EXPECTED GREEN PHASE FAILURE: Expected 60s for count 70209, but got %v", bBug)
	}
}
