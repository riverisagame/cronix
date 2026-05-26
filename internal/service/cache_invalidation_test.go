package service

import (
	"path/filepath"
	"testing"

	"cronix/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupExecTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.AutoMigrate(&model.Task{}, &model.ExecutionLog{})
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return db
}

// TestStatsCacheInvalidation 测试仪表盘缓存的即时失效逻辑
func TestStatsCacheInvalidation(t *testing.T) {
	db := setupExecTestDB(t)
	svc := NewExecutionService(db)

	// 1. 初始查询，此时应生成缓存
	stats1, err := svc.GetDashboardStats()
	if err != nil {
		t.Fatalf("GetDashboardStats failed: %v", err)
	}

	// 写入一个新任务以更改真实数据，但由于 60s 缓存，GetDashboardStats 应该继续返回旧数据
	task := model.Task{Name: "dummy-task", TaskType: "shell", Enabled: true}
	db.Create(&task)

	stats2, _ := svc.GetDashboardStats()
	if stats2["total_tasks"].(int64) != stats1["total_tasks"].(int64) {
		// 校验缓存是否存在
		t.Logf("Stats total_tasks changed without invalidation: %d -> %d (cache not populated?)", stats1["total_tasks"], stats2["total_tasks"])
	}

	// 2. 主动失效缓存 [RED]
	// 期望：失效后再次 GetDashboardStats 能获取到最新数据（total_tasks 增加为 1）
	svc.InvalidateStatsCache()

	stats3, err := svc.GetDashboardStats()
	if err != nil {
		t.Fatalf("GetDashboardStats failed after invalidation: %v", err)
	}

	if stats3["total_tasks"].(int64) != 1 {
		t.Errorf("expected total_tasks to be 1 after cache invalidation, got %v", stats3["total_tasks"])
	}
}
