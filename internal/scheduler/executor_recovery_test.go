package scheduler

import (
	"cronix/internal/config"
	"cronix/internal/model"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecutor_RecoverOrphanedLogs 测试启动时清理孤儿执行日志的功能
func TestExecutor_RecoverOrphanedLogs(t *testing.T) {
	db := setupExecutorTestDB(t)

	// 插入一个任务
	task := model.Task{
		Name:     "recovery-test-task",
		TaskType: "shell",
		Command:  "echo hello",
		Enabled:  true,
	}
	db.Create(&task)

	// 插入一条由于崩溃残留的孤儿 RUNNING 执行记录
	now := time.Now()
	orphanedLog := model.ExecutionLog{
		TaskID:      &task.ID,
		TaskName:    task.Name,
		Status:      model.StateRunning,
		TriggerType: "daemon",
		StartTime:   now,
		// end_time 为空，表示未结束，模拟崩溃残留
	}
	db.Create(&orphanedLog)

	// 验证插入成功
	var countBefore int64
	db.Model(&model.ExecutionLog{}).Where("status = ?", model.StateRunning).Count(&countBefore)
	assert.Equal(t, int64(1), countBefore, "预插入后应有 1 条 RUNNING 日志")

	// 模拟重启，创建新 Executor
	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize: 1,
		},
	}
	engine := NewEngine(db)
	_, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	// 此时，NewExecutor 应该自动把 orphanedLog 置为 Failed
	var countAfter int64
	db.Model(&model.ExecutionLog{}).Where("status = ?", model.StateRunning).Count(&countAfter)
	assert.Equal(t, int64(0), countAfter, "启动时应该清理所有 RUNNING 状态的孤儿日志")

	var reloaded model.ExecutionLog
	db.First(&reloaded, orphanedLog.ID)
	assert.Equal(t, model.StateFailed, reloaded.Status, "孤儿日志应该被置为 failed 状态")
	assert.NotNil(t, reloaded.EndTime, "孤儿日志应该补上结束时间")
	assert.Equal(t, "System restarted or crashed", reloaded.ErrorMsg, "应该包含由于崩溃导致的失败原因提示")
}
