package scheduler

import (
	"cronix/internal/config"
	"cronix/internal/model"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupExecutorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_executor.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.AutoMigrate(&model.Task{}, &model.ExecutionLog{}, &model.GroupExecutionLog{}, &model.NotifyConfig{})
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return db
}

// TestExecutor_TaskLevelQuota 测试单任务级别的数据库日志限额逻辑
func TestExecutor_TaskLevelQuota(t *testing.T) {
	db := setupExecutorTestDB(t)

	// 创建测试任务
	task := model.Task{
		ID:         1,
		Name:       "quota-test-task",
		TaskType:   "shell",
		Command:    "echo 'hello'",
		Enabled:    true,
		TimeoutSec: 10,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	// 插入 15 条历史日志记录（模拟以前积累的数据）
	for i := 1; i <= 15; i++ {
		logItem := model.ExecutionLog{
			TaskID:      &task.ID,
			TaskName:    task.Name,
			CronExpr:    task.CronExpr,
			Status:      "success",
			TriggerType: "cron",
			StartTime:   time.Now().Add(-time.Duration(20-i) * time.Minute),
			ErrorMsg:    "dummy error " + strconv.Itoa(i),
		}
		if err := db.Create(&logItem).Error; err != nil {
			t.Fatalf("create log item %d failed: %v", i, err)
		}
	}

	// 初始化 Executor 配置，设置单任务日志最大保留数限制为 10 条
	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:         4,
			OutputTruncateKB: 64,
		},
		Log: config.LogConfig{
			MaxRecords:     100000,
			MaxLogsPerTask: 10,
		},
	}
	// 在我们的真实配置和实现中，我们将在 LogConfig 增加 MaxLogsPerTask。
	// 为了使当前阶段的测试在编译上能够识别（如果直接无法通过编译算作 RED 阶段的第一步），
	// 我们将在配置装配完毕后，通过执行器逻辑检查。

	engine := NewEngine(db)
	executor, err := NewExecutor(db, cfg, engine)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	// 运行一次任务
	// 期望：运行完任务，插入第 16 条日志后，触发限额清理（限制最多 10 条）。
	// 结果：该任务的日志数应该被截断并限制为最多 10 条。
	executor.executeTask(task.ID)

	// 因为 limitTaskLogs 现在是完全异步的（脱离了 executeTask 的主路径），
	// 这里给后台 goroutine 留一点时间完成数据库清理操作。
	time.Sleep(200 * time.Millisecond)

	var count int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&count)

	// 如果 count 大于 10，说明单任务限额清理逻辑还未实现，测试失败（符合 RED 预期）
	if count > 10 {
		t.Errorf("expected task logs count to be limited to 10, but got: %d", count)
	}
}
