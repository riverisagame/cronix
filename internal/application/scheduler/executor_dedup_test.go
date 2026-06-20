package scheduler

import (
	"cronix/internal/infrastructure/config"
	"cronix/internal/domain/model"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecutor_DedupRunningLog 测试 executeTask 的防重逻辑：
// 如果同一任务已存在未完成的执行记录，不应创建第二条 RUNNING 日志
func TestExecutor_DedupRunningLog(t *testing.T) {
	db := setupExecutorTestDB(t)

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize: 1,
		},
	}
	engine := NewEngine(db)
	exec, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	// 创建一个测试任务
	task := model.Task{
		Name:     "dedup-test-task",
		TaskType: "shell",
		Command:  "echo hello",
		Enabled:  true,
	}
	db.Create(&task)

	// 预插入一条 RUNNING 状态的执行记录（模拟已有未完成执行）
	now := time.Now()
	existingLog := model.ExecutionLog{
		TaskID:      &task.ID,
		TaskName:    task.Name,
		Status:      model.StateRunning,
		TriggerType: "cron",
		StartTime:   now,
		// end_time 为空，表示未结束
	}
	db.Create(&existingLog)

	// 记录当前该任务的执行日志数量
	var countBefore int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&countBefore)
	assert.Equal(t, int64(1), countBefore, "预插入后应有 1 条日志")

	// 触发 executeTask（应该被防重逻辑拦截）
	exec.executeTask(task.ID)

	// 等待异步 goroutine 完成（若有的话，executeTask 是同步的）
	time.Sleep(200 * time.Millisecond)

	// 验证：执行日志数量应该仍然是 1
	var countAfter int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&countAfter)
	assert.Equal(t, int64(1), countAfter, "防重逻辑应阻止创建第二条 RUNNING 日志")

	// 验证：existingLog 的 end_time 仍然为空（未被篡改）
	var reloaded model.ExecutionLog
	db.First(&reloaded, existingLog.ID)
	assert.Nil(t, reloaded.EndTime, "原有的 RUNNING 记录的 end_time 不应被修改")
}

// TestExecutor_DedupAfterCompletion 测试：当之前的执行完成后，新调用应该能正常执行
func TestExecutor_DedupAfterCompletion(t *testing.T) {
	db := setupExecutorTestDB(t)

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize: 1,
		},
	}
	engine := NewEngine(db)
	exec, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	task := model.Task{
		Name:     "dedup-after-complete-task",
		TaskType: "shell",
		Command:  "echo test",
		Enabled:  true,
	}
	db.Create(&task)

	// 插入一条已完成（有 end_time）的执行记录 — 这不影响新触发
	now := time.Now()
	completedLog := model.ExecutionLog{
		TaskID:      &task.ID,
		TaskName:    task.Name,
		Status:      model.StateSuccess,
		TriggerType: "cron",
		StartTime:   now,
		EndTime:     &now, // 已完成
	}
	db.Create(&completedLog)

	// 触发 executeTask — 应该正常创建新 RUNNING 日志
	exec.executeTask(task.ID)
	time.Sleep(200 * time.Millisecond)

	// 验证：应该有 2 条日志（1 条已完成 + 1 条新的 running/成功/失败）
	var totalCount int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&totalCount)
	assert.GreaterOrEqual(t, totalCount, int64(2),
		"已完成的任务不影响新触发，应有新的执行记录")
}
