package scheduler

import (
	"cronix/internal/model"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGormLogRepository_CreateAndSave 测试 GORM 实现的基本创建和保存
func TestGormLogRepository_CreateAndSave(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	task := model.Task{Name: "repo-test-task", TaskType: "shell", Command: "echo hi", Enabled: true}
	db.Create(&task)

	// 创建执行日志
	now := time.Now()
	execLog := &model.ExecutionLog{
		TaskID:      &task.ID,
		TaskName:    task.Name,
		Status:      model.StateRunning,
		TriggerType: "cron",
		StartTime:   now,
	}
	err := repo.CreateExecutionLog(execLog)
	require.NoError(t, err)
	assert.NotZero(t, execLog.ID, "创建后应分配 ID")

	// 更新执行日志
	endTime := time.Now()
	execLog.Status = model.StateSuccess
	execLog.EndTime = &endTime
	err = repo.SaveExecutionLog(execLog)
	require.NoError(t, err)

	// 验证更新生效
	var reloaded model.ExecutionLog
	db.First(&reloaded, execLog.ID)
	assert.Equal(t, model.StateSuccess, reloaded.Status)
	assert.NotNil(t, reloaded.EndTime)
}

// TestGormLogRepository_CountRunningLogs 测试统计运行中日志
func TestGormLogRepository_CountRunningLogs(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	task := model.Task{Name: "count-test", TaskType: "shell", Command: "echo", Enabled: true}
	db.Create(&task)

	// 初始应该为 0
	count, err := repo.CountRunningLogs(task.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	// 插入一条 running 日志
	now := time.Now()
	db.Create(&model.ExecutionLog{
		TaskID: &task.ID, TaskName: task.Name,
		Status: model.StateRunning, TriggerType: "cron", StartTime: now,
	})

	count, err = repo.CountRunningLogs(task.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// 已完成的不应计入
	endTime := time.Now()
	db.Create(&model.ExecutionLog{
		TaskID: &task.ID, TaskName: task.Name,
		Status: model.StateSuccess, TriggerType: "cron", StartTime: now, EndTime: &endTime,
	})
	count, err = repo.CountRunningLogs(task.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "已完成日志不应计入 running 统计")
}

// TestGormLogRepository_CleanupOrphanedLogs 测试孤儿日志清理
func TestGormLogRepository_CleanupOrphanedLogs(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	task := model.Task{Name: "orphan-test", TaskType: "shell", Command: "echo", Enabled: true}
	db.Create(&task)

	// 插入孤儿 running 日志
	now := time.Now()
	db.Create(&model.ExecutionLog{
		TaskID: &task.ID, TaskName: task.Name,
		Status: model.StateRunning, TriggerType: "daemon", StartTime: now,
	})

	// 清理
	err := repo.CleanupOrphanedLogs(time.Now())
	require.NoError(t, err)

	// 验证被置为 failed
	var count int64
	db.Model(&model.ExecutionLog{}).Where("status = ?", model.StateRunning).Count(&count)
	assert.Equal(t, int64(0), count, "孤儿日志应被清理")

	var log model.ExecutionLog
	db.First(&log)
	assert.Equal(t, model.StateFailed, log.Status)
	assert.Equal(t, "System restarted or crashed", log.ErrorMsg)
	assert.NotNil(t, log.EndTime)
}

// TestGormLogRepository_GetLatestTaskLog 测试获取最新日志
func TestGormLogRepository_GetLatestTaskLog(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	task := model.Task{Name: "latest-test", TaskType: "shell", Command: "echo", Enabled: true}
	db.Create(&task)

	now := time.Now()
	// 插入两条日志
	db.Create(&model.ExecutionLog{
		TaskID: &task.ID, TaskName: task.Name,
		Status: model.StateSuccess, TriggerType: "cron", StartTime: now,
	})
	db.Create(&model.ExecutionLog{
		TaskID: &task.ID, TaskName: task.Name,
		Status: model.StateFailed, TriggerType: "cron", StartTime: now,
	})

	latest, err := repo.GetLatestTaskLog(task.ID)
	require.NoError(t, err)
	require.NotNil(t, latest, "应返回非 nil 的日志对象")
	assert.Equal(t, model.StateFailed, latest.Status, "应返回最新（ID 最大）的日志")
}

// TestGormLogRepository_DeleteExcessTaskLogs 测试单任务超额日志清理
func TestGormLogRepository_DeleteExcessTaskLogs(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	task := model.Task{Name: "excess-test", TaskType: "shell", Command: "echo", Enabled: true}
	db.Create(&task)

	now := time.Now()
	// 插入 5 条日志
	for i := 0; i < 5; i++ {
		db.Create(&model.ExecutionLog{
			TaskID: &task.ID, TaskName: task.Name,
			Status: model.StateSuccess, TriggerType: "cron", StartTime: now,
		})
	}

	// 保留 3 条，应删除 2 条
	err := repo.DeleteExcessTaskLogs(task.ID, 3)
	require.NoError(t, err)

	var count int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&count)
	assert.Equal(t, int64(3), count, "应保留 3 条日志")
}

// TestGormLogRepository_GroupLogCRUD 测试组日志的创建和保存
func TestGormLogRepository_GroupLogCRUD(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	now := time.Now()
	glog := &model.GroupExecutionLog{
		GroupID:     1,
		GroupName:   "test-group",
		Mode:        "parallel",
		TriggerType: "cron",
		MemberCount: 3,
		Status:      model.StateRunning,
		StartTime:   now,
	}
	err := repo.CreateGroupLog(glog)
	require.NoError(t, err)
	assert.NotZero(t, glog.ID)

	endTime := time.Now()
	glog.Status = model.StateSuccess
	glog.EndTime = &endTime
	err = repo.SaveGroupLog(glog)
	require.NoError(t, err)

	var reloaded model.GroupExecutionLog
	db.First(&reloaded, glog.ID)
	assert.Equal(t, model.StateSuccess, reloaded.Status)
}
