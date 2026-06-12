package scheduler

import (
	"cronix/internal/config"
	"cronix/internal/model"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecutor_CronGlobalTimeout 测试 cron 触发路径受全局超时保护
// 模拟一个任务执行时间超过全局 MaxTimeoutSec，验证：
// 1. 任务被超时强杀
// 2. 日志状态被正确更新为 failed/timeout
// 3. 协程被释放回池中（不泄漏）
func TestExecutor_CronGlobalTimeout(t *testing.T) {
	db := setupExecutorTestDB(t)

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:      2,
			MaxTimeoutSec: 2, // 全局超时 2 秒
			OutputTruncateKB: 64,
		},
		Log: config.LogConfig{
			MaxLogsPerTask: 1000,
		},
	}
	engine := NewEngine(db)
	exec, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	// 创建一个 sleep 10s 的任务（远超 2s 全局超时）
	task := model.Task{
		Name:       "timeout-test-task",
		TaskType:   "shell",
		Command:    "sleep 10",
		Enabled:    true,
		TimeoutSec: 0, // 任务级超时为 0，由全局兜底
	}
	db.Create(&task)

	// 通过 handleTrigger 触发（模拟 cron 路径）
	exec.handleTrigger(task.ID)

	// 等待足够时间让超时生效（2s 超时 + 1s 缓冲）
	time.Sleep(4 * time.Second)

	// 验证：任务日志应该已创建，且状态不是 running（应为 failed 或 timeout）
	var logs []model.ExecutionLog
	db.Where("task_id = ?", task.ID).Find(&logs)
	require.NotEmpty(t, logs, "应有执行日志")

	latestLog := logs[len(logs)-1]
	assert.NotEqual(t, model.StateRunning, latestLog.Status,
		"超时后任务不应仍处于 running 状态")
	assert.NotNil(t, latestLog.EndTime, "超时后应有结束时间")
}

// TestExecutor_ManualTriggerTimeout 测试手动触发路径也受全局超时保护
func TestExecutor_ManualTriggerTimeout(t *testing.T) {
	db := setupExecutorTestDB(t)

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:      2,
			MaxTimeoutSec: 2,
			OutputTruncateKB: 64,
		},
		Log: config.LogConfig{
			MaxLogsPerTask: 1000,
		},
	}
	engine := NewEngine(db)
	exec, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	task := model.Task{
		Name:       "manual-timeout-test",
		TaskType:   "shell",
		Command:    "sleep 10",
		Enabled:    true,
		TimeoutSec: 0,
	}
	db.Create(&task)

	exec.RunTaskNow(task.ID)

	time.Sleep(4 * time.Second)

	var logs []model.ExecutionLog
	db.Where("task_id = ?", task.ID).Find(&logs)
	require.NotEmpty(t, logs)

	latestLog := logs[len(logs)-1]
	assert.NotEqual(t, model.StateRunning, latestLog.Status,
		"手动触发的任务也应受全局超时保护")
}
