package scheduler

import (
	"cronix/internal/infrastructure/config"
	"cronix/internal/domain/model"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestExecutor_ConcurrencyBypass_Red 验证普通定时任务触发时，
// ants.Pool 是否能有效限制并发（修复前会被架空，并发度无限大）。
func TestExecutor_ConcurrencyBypass_Red(t *testing.T) {
	db := setupExecutorTestDB(t)

	var concurrentExecutions int32
	var maxConcurrentExecutions int32
	var wg sync.WaitGroup

	// Setup a slow HTTP server to track concurrency
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&concurrentExecutions, 1)
		defer atomic.AddInt32(&concurrentExecutions, -1)

		for {
			max := atomic.LoadInt32(&maxConcurrentExecutions)
			if current <= max {
				break
			}
			if atomic.CompareAndSwapInt32(&maxConcurrentExecutions, max, current) {
				break
			}
		}

		time.Sleep(300 * time.Millisecond) // Simulate slow execution
		w.WriteHeader(http.StatusOK)
		wg.Done()
	}))
	defer ts.Close()

	// Configure pool size to exactly 2
	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:         2, 
			OutputTruncateKB: 64,
		},
	}

	engine := NewEngine(db)
	executor, err := NewExecutor(db, cfg, engine)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	// Create 10 slow tasks
	numTasks := 10
	for i := 1; i <= numTasks; i++ {
		task := model.Task{
			ID:         uint(i),
			Name:       fmt.Sprintf("task-slow-%d", i),
			TaskType:   "http",
			HTTPMethod: "GET",
			HTTPURL:    ts.URL,
			Enabled:    true,
			TimeoutSec: 10,
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	// Trigger all 10 tasks at virtually the same time
	wg.Add(numTasks)
	for i := 1; i <= numTasks; i++ {
		executor.handleTrigger(uint(i))
	}

	// Wait for all to finish
	wg.Wait()
	time.Sleep(300 * time.Millisecond) // Let async logs finish

	maxConcurrent := atomic.LoadInt32(&maxConcurrentExecutions)
	t.Logf("Max concurrent executions observed: %d", maxConcurrent)

	// Since pool size is 2, the absolute maximum concurrent executions should be 2.
	// If it exceeds 2, it proves the ants pool is being bypassed.
	if maxConcurrent > 2 {
		t.Errorf("FATAL ARCHITECTURE BUG: PoolSize is 2, but observed %d concurrent executions! Thread pool is being bypassed.", maxConcurrent)
	}
}
