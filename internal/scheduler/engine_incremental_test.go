package scheduler

import (
	"path/filepath"
	"testing"

	"cronix/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupEngineTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.AutoMigrate(&model.Task{}, &model.TaskGroup{})
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return db
}

// TestIncrementalTaskScheduling 测试增量添加、修改与删除任务调度的行为
func TestIncrementalTaskScheduling(t *testing.T) {
	db := setupEngineTestDB(t)
	engine := NewEngine(db)

	// 创建一个启用的定时任务
	task := model.Task{
		ID:         1,
		Name:       "incremental-task-1",
		CronExpr:   "*/5 * * * * *", // 每5秒
		TaskType:   "shell",
		Command:    "echo 1",
		Enabled:    true,
		TimeoutSec: 10,
	}

	// 1. 测试增量添加调度 [RED]
	// 期望：UpdateTaskSchedule 成功且 entryMap 中记录了该任务
	err := engine.UpdateTaskSchedule(task)
	if err != nil {
		t.Fatalf("UpdateTaskSchedule failed: %v", err)
	}

	engine.mu.Lock()
	_, exists := engine.entryMap[task.ID]
	engine.mu.Unlock()
	if !exists {
		t.Error("expected task entry to exist in entryMap after UpdateTaskSchedule")
	}

	// 2. 测试增量更新调度为禁用 [RED]
	// 期望：更新为禁用后，entryMap 中的调度条目被安全移除
	task.Enabled = false
	err = engine.UpdateTaskSchedule(task)
	if err != nil {
		t.Fatalf("UpdateTaskSchedule (disable) failed: %v", err)
	}

	engine.mu.Lock()
	_, exists = engine.entryMap[task.ID]
	engine.mu.Unlock()
	if exists {
		t.Error("expected task entry to be removed from entryMap when disabled")
	}

	// 3. 测试增量删除调度 [RED]
	// 期望：RemoveTaskSchedule 后定时器无该条目
	task.Enabled = true
	_ = engine.UpdateTaskSchedule(task)
	engine.RemoveTaskSchedule(task.ID)

	engine.mu.Lock()
	_, exists = engine.entryMap[task.ID]
	engine.mu.Unlock()
	if exists {
		t.Error("expected task entry to be removed after RemoveTaskSchedule")
	}
}

// TestIncrementalGroupScheduling 测试增量组任务调度的行为
func TestIncrementalGroupScheduling(t *testing.T) {
	db := setupEngineTestDB(t)
	engine := NewEngine(db)

	var triggeredGroupID uint
	engine.SetGroupTrigger(func(gid uint) {
		triggeredGroupID = gid
	})

	group := model.TaskGroup{
		ID:       10,
		Name:     "incremental-group-1",
		CronExpr: "*/10 * * * * *",
		Enabled:  true,
		Mode:     "parallel",
	}

	// 1. 测试增量添加组调度 [RED]
	err := engine.UpdateGroupSchedule(group)
	if err != nil {
		t.Fatalf("UpdateGroupSchedule failed: %v", err)
	}

	engine.mu.Lock()
	_, exists := engine.groupEntryMap[group.ID]
	engine.mu.Unlock()
	if !exists {
		t.Error("expected group entry to exist in groupEntryMap")
	}

	// 2. 测试增量移除组调度 [RED]
	engine.RemoveGroupSchedule(group.ID)
	engine.mu.Lock()
	_, exists = engine.groupEntryMap[group.ID]
	engine.mu.Unlock()
	if exists {
		t.Error("expected group entry to be removed from groupEntryMap")
	}

	_ = triggeredGroupID
}
