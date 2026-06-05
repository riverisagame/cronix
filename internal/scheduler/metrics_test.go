package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMetricsRegistry_RecordAndSnapshot(t *testing.T) {
	registry := NewMetricsRegistry()
	registry.Start()
	defer registry.Stop()

	// Record some executions
	registry.RecordExecution(100, true)
	registry.RecordExecution(200, true)
	registry.RecordExecution(500, false)

	// Wait for async processing
	time.Sleep(50 * time.Millisecond)

	snapshot := registry.GetSnapshot()

	assert.NotEmpty(t, snapshot.MinuteLabels, "Expected minute labels to be populated")
	if len(snapshot.MinuteSuccess) > 0 {
		assert.Equal(t, int64(2), snapshot.MinuteSuccess[len(snapshot.MinuteSuccess)-1], "Expected 2 successes in the current minute")
		assert.Equal(t, int64(1), snapshot.MinuteFailed[len(snapshot.MinuteFailed)-1], "Expected 1 failure in the current minute")
	} else {
		t.Error("Expected at least one minute bucket")
	}
}

func TestMetricsRegistry_P95P99(t *testing.T) {
	registry := NewMetricsRegistry()
	registry.Start()
	defer registry.Stop()

	// Push 100 durations: 1 to 100
	for i := int64(1); i <= 100; i++ {
		registry.RecordExecution(i, true)
	}

	time.Sleep(50 * time.Millisecond)

	snapshot := registry.GetSnapshot()
	if len(snapshot.MinuteP95) > 0 {
		p95 := snapshot.MinuteP95[len(snapshot.MinuteP95)-1]
		p99 := snapshot.MinuteP99[len(snapshot.MinuteP99)-1]

		// 95th percentile of 1..100 should be >= 95
		assert.GreaterOrEqual(t, p95, int64(95))
		assert.GreaterOrEqual(t, p99, int64(99))
	} else {
		t.Error("Expected at least one minute bucket")
	}
}
