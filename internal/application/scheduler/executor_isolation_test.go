package scheduler

import (
	"cronix/internal/infrastructure/config"
	"cronix/internal/domain/model"
	"testing"
	"time"
)

// TestExecutor_ExecutionIsolation 测试任务触发的隔离性，
// 确保触发某个任务时不会导致其他无关任务被附带执行（避免曾经的第0层全量执行Bug）。
func TestExecutor_ExecutionIsolation(t *testing.T) {
	db := setupExecutorTestDB(t)

	// 创建两个独立的测试任务 A 和 B
	taskA := model.Task{
		ID:         1,
		Name:       "task-a",
		TaskType:   "shell",
		Command:    "echo 'A'",
		Enabled:    true,
		TimeoutSec: 10,
	}
	taskB := model.Task{
		ID:         2,
		Name:       "task-b",
		TaskType:   "shell",
		Command:    "echo 'B'",
		Enabled:    true,
		TimeoutSec: 10,
	}

	if err := db.Create(&taskA).Error; err != nil {
		t.Fatalf("create task A: %v", err)
	}
	if err := db.Create(&taskB).Error; err != nil {
		t.Fatalf("create task B: %v", err)
	}

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:         4,
			OutputTruncateKB: 64,
		},
	}

	engine := NewEngine(db)
	executor, err := NewExecutor(db, cfg, engine)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	// 模拟触发 Task A (比如手动触发或定时器触发)
	// 在修复前，这会导致 Task B 也被执行。修复后，只有 Task A 被执行。
	executor.RunTaskNow(taskA.ID)
	executor.handleTrigger(taskA.ID)

	// 等待异步执行完成
	time.Sleep(1 * time.Second)

	var countA, countB int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", taskA.ID).Count(&countA)
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", taskB.ID).Count(&countB)

	// 触发了两次 Task A（一次 RunTaskNow，一次 handleTrigger），所以 countA 应该 >= 2
	// 有时候 handleTrigger 是完全异步的，所以给一点等待时间，这里主要验证不为0即可
	if countA == 0 {
		t.Errorf("expected task A to be executed, but got 0 logs")
	}

	// 核心验证：Task B 不应有任何执行记录
	if countB != 0 {
		t.Errorf("FATAL BUG: Task B was unexpectedly executed %d times when only Task A was triggered", countB)
	}
}
