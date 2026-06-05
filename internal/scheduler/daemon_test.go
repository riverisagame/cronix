package scheduler

import (
	"context"
	"cronix/internal/config"
	"cronix/internal/model"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupDaemonTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_daemon.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// 在测试中迁移所需的数据表
	db.AutoMigrate(&model.Task{}, &model.ExecutionLog{}, &model.GroupExecutionLog{}, &model.NotifyConfig{})
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return db
}

// TestDaemonMonitor_KeepAlive 测试常驻任务崩溃后自动拉起重启逻辑
func TestDaemonMonitor_KeepAlive(t *testing.T) {
	db := setupDaemonTestDB(t)

	// 创建一个运行就会快速失败（退出码 1）的常驻任务
	task := model.Task{
		ID:                 101,
		Name:               "daemon-failure-task",
		TaskType:           "shell",
		Command:            "exit 1", // 模拟进程异常退出
		Enabled:            true,
		TimeoutSec:         10,
		RunMode:            "daemon", // 常驻守护模式（在 RED 阶段此字段尚未在 Task 中定义，编译必失败）
		RestartPolicy:      "always", // 总是重启
		MaxRestartAttempts: 3,        // 最大连续重试次数
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:         4,
			OutputTruncateKB: 64,
		},
		Log: config.LogConfig{
			MaxRecords: 100000,
		},
	}

	engine := NewEngine(db)
	executor, err := NewExecutor(db, cfg, engine)
	if err != nil {
		t.Fatalf("new executor failed: %v", err)
	}

	// 初始化 DaemonMonitor。在 RED 阶段，NewDaemonMonitor 函数及 DaemonMonitor 结构体尚未实现，编译报错。
	monitor := NewDaemonMonitor(db, executor)
	
	// 启动守护，它会在后台加载 task 并启动守护协程
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitor.Start(ctx)

	// 等待一段时间（由于退避重试，第 1 次失败后重试需等待 1-2 秒）
	// 我们给它充足的 4 秒时间，期望它至少触发并写入了多次执行日志记录（因为每次拉起运行都会产生新的 execution_log 记录）
	time.Sleep(4 * time.Second)

	// 查询执行日志数，如果自动拉起工作正常，该 task_id 应有不止 1 条执行记录
	var logCount int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&logCount)

	if logCount <= 1 {
		t.Errorf("expected daemon task to be restarted multiple times, but got log count: %d", logCount)
	}

	// 检查当前内存状态是否进入了 BACKOFF 或 FATAL
	state, exists := monitor.GetDaemonState(task.ID)
	if !exists {
		t.Errorf("expected daemon state to exist for task %d", task.ID)
	} else {
		t.Logf("task %d current state: %s, restart attempts: %d", task.ID, state.Status, state.RestartCount)
	}
}

// TestDaemonMonitor_Stop 测试手动停止常驻守护任务，确保它优雅退出并不再重启
func TestDaemonMonitor_Stop(t *testing.T) {
	db := setupDaemonTestDB(t)

	// 创建一个长久挂起运行的 shell 任务（sleep 100）
	task := model.Task{
		ID:                 102,
		Name:               "daemon-long-sleep",
		TaskType:           "shell",
		Command:            "sleep 100", // 长久挂起
		Enabled:            true,
		TimeoutSec:         300,
		RunMode:            "daemon",
		RestartPolicy:      "always",
		MaxRestartAttempts: 5,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:         4,
			OutputTruncateKB: 64,
		},
		Log: config.LogConfig{
			MaxRecords: 100000,
		},
	}

	engine := NewEngine(db)
	executor, _ := NewExecutor(db, cfg, engine)
	monitor := NewDaemonMonitor(db, executor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitor.Start(ctx)

	// 等待 1 秒让进程成功跑起来
	time.Sleep(1 * time.Second)

	state, exists := monitor.GetDaemonState(task.ID)
	if !exists || state.Status != "RUNNING" {
		t.Fatalf("expected task %d to be in RUNNING state, got: %+v", task.ID, state)
	}

	// 手动停止该常驻进程
	monitor.StopDaemon(task.ID)

	// 验证进程状态是否变成 STOPPED
	time.Sleep(500 * time.Millisecond)
	stateAfterStop, existsAfter := monitor.GetDaemonState(task.ID)
	if !existsAfter || stateAfterStop.Status != "STOPPED" {
		t.Errorf("expected task %d state to be STOPPED after stop, but got: %+v", task.ID, stateAfterStop)
	}

	// 验证其日志条数在停止后不会再增长（保证不再自动重启）
	var countBefore int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&countBefore)

	time.Sleep(2 * time.Second)

	var countAfter int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&countAfter)

	if countAfter > countBefore {
		t.Errorf("expected log count to stay at %d after stop, but grew to %d", countBefore, countAfter)
	}
}
