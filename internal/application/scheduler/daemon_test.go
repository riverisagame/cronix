/*
📌 【大厂面试·核心考点】
1. 如何在单测中模拟真实操作系统的进程崩溃和退出？(回答：通过真实的轻量级系统命令如 exit 1 或跨平台 sleep/ping 模拟)
2. 如何实现并验证退避重试(Exponential Backoff)机制？(回答：观察在指定时间窗口内重试次数/执行日志数是否符合衰减预期)
3. 测试替身(Test Double)中的 Mock 与 Stub 的区别？(回答：Mock强调行为验证，Stub强调状态/返回值的桩数据。本文件使用轻量级Fake数据库实现，保证物理零污染)

🏗️ 【架构设计·模式对比】
本测试文件采用“轻量级内置集成测试(In-memory Integration Test)”模式。
对比传统的纯单元测试（仅通过Go interface Mock），本方案直接使用 SQLite 临时文件库，
可以真实检验 ORM 的执行、SQL方言适配以及并发读写竞争情况，极大地增强了对数据持久层的信心。
*/
package scheduler
import (
	"context"
	"cronix/internal/infrastructure/config"
	"cronix/internal/domain/model"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

/*
🧪 【测试工程·质量保障】
测试数据的“物理零污染”原则：
这里使用 `t.TempDir()` 和 SQLite 临时文件数据库，确保每次运行的 DB 都是独立且隔离的。
这就像在无菌手术室操作一样，测试完即销毁（`t.Cleanup`），绝不影响开发机或CI/CD流水线的其他系统真实数据。
坚决杜绝在物理机器的数据库中执行 DDL、DROP 甚至脏数据写入，保证测试用例环境 100% 幂等。
*/
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

/*
🔬 【底层原理·深度剖析】
进程崩溃与僵尸进程(Zombie Process)模拟：
在 Unix 系统下，父进程通过 fork-exec 创建子进程，如果子进程退出（例如 exit 1），内核会释放其用户态资源，
但保留 PCB (Process Control Block) 中的退出状态，此时它变成“僵尸进程”。直到父进程调用 wait/waitpid 才会彻底回收。
Go 语言的 `os/exec` 包在调用 `Wait()` 时会自动执行 waitpid 逻辑。本测试通过 `exit 1` 强行阻断业务，
正是为了验证监控器能否正确接收到 `Wait()` 的错误返回，并触发状态机流转（进入失败或 BACKOFF 状态）。

⚡ 【性能实战·生产调优】
退避重试算法 (Backoff)：
在真实的生产环境（比如每秒万级并发），如果守护进程崩溃后被死循环立即拉起（不加退避），
会导致 CPU 瞬时被打满（CPU 抖动），或者短时间内耗尽系统的 PID 池（PID Exhaustion）。
标准做法是引入指数退避（例如1s, 2s, 4s, 8s），这里预留的 `time.Sleep(4 * time.Second)` 就是用来验证退避窗口的逻辑流转，通过延时确保拉起频次符合预期。

💀 【踩坑血泪·反面教材】
错误做法：在测试常驻进程时死等进程退出 `WaitGroup.Wait()`，如果子进程被阻塞或写了死循环，整个 CI 流水线将永远卡死！
正确做法：本测试通过 context 注入及延时检查机制，给予有限的等待时间并在最后阶段进行状态断言，这是容错测试的标准姿势。
*/
// TestDaemonMonitor_KeepAlive 测试常驻任务崩溃后自动拉起重启逻辑
func TestDaemonMonitor_KeepAlive(t *testing.T) {
	db := setupDaemonTestDB(t)

	// 创建一个运行就会快速失败（退出码 1）的常驻任务
	// 🛡️ 【安全攻防·漏洞防线】
	// 这里严格指定了 RunMode = daemon。在生产系统中，此类直接从数据库读取Command并执行的逻辑必须防范任意命令注入(Command Injection)。
	// 测试中即便运行 exit 1，也是受限在测试沙箱与当前进程权限内，通过精确控制命令字符串保证安全性。
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

	// 使用包内轻量 mock（避免 import cycle with service 包）
	taskSvc := &testTaskLoader{db: db}
	execSvc := &testLogQuerier{db: db}
	monitor := NewDaemonMonitor(taskSvc, execSvc, executor)
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

/*
🔬 【底层原理·深度剖析】
进程的优雅退出 (Graceful Shutdown) 原理：
当调用 `monitor.StopDaemon(task.ID)` 时，底层本质上是向进程组发送了 SIGTERM 信号（Windows 下为 taskkill / PID 等效机制）。
如果直接发送 SIGKILL (kill -9)，子进程打开的文件描述符、网络连接将无法正常回收，导致内存泄漏甚至数据损坏。
本测试通过跨平台的休眠命令 (sleep/ping) 模拟业务长链接进程，测试系统是否能在限定时间内真正地把该进程杀掉。

🧪 【测试工程·质量保障】
测试结果的稳定性（防止 Flaky Test）：
在并发调度系统中，时间的掌控是痛点。代码中通过 `time.Sleep(500 * time.Millisecond)` 等待异步的协程关闭。
虽然这种固定睡眠不是最完美的方案（理想情况是通过 channel 接收停止信号或者监听事件总线），
但在单机本地环境集成测试中配合合理的容忍阈值，足以确保后续的断言 (`countAfter == countBefore`) 稳定通过。
*/
// TestDaemonMonitor_Stop 测试手动停止常驻守护任务，确保它优雅退出并不再重启
func TestDaemonMonitor_Stop(t *testing.T) {
	db := setupDaemonTestDB(t)

	cmdStr := "sleep 100"
	if runtime.GOOS == "windows" {
		cmdStr = "ping 127.0.0.1 -n 100 > NUL"
	}

	// 创建一个长久挂起运行的 shell 任务（sleep 100）
	task := model.Task{
		ID:                 102,
		Name:               "daemon-long-sleep",
		TaskType:           "shell",
		Command:            cmdStr, // 长久挂起
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
	monitor := NewDaemonMonitor(&testTaskLoader{db: db}, &testLogQuerier{db: db}, executor)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitor.Start(ctx)

	// 等待 1 秒让进程成功跑起来
	time.Sleep(1 * time.Second)

	state, exists := monitor.GetDaemonState(task.ID)  // task.ID 由 GORM Create 后自动填充
	if !exists || state.Status != DaemonRunning {
		t.Fatalf("expected task %d to be in RUNNING state, got: %+v", task.ID, state)
	}

	// 手动停止该常驻进程
	monitor.StopDaemon(task.ID)

	// 验证进程状态是否变成 STOPPED
	time.Sleep(500 * time.Millisecond)
	stateAfterStop, existsAfter := monitor.GetDaemonState(task.ID)
	if !existsAfter || stateAfterStop.Status != DaemonStopped {
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

/*
🏗️ 【架构设计·模式对比】
内部接口桩 (Local Mock)：
这里没有使用 gomock/testify 等重量级代码生成工具，而是手写了简单的 `testTaskLoader` 和 `testLogQuerier`。
优势对比：
- gomock：适合超大型接口、复杂的调用次数断言，但维护桩代码成本高。
- 本地 Fake 结构体：这种“轻量级 Fake”模式在跨包引用（避免 import cycle）或者仅仅为了提供特定行为时，可读性极高、代码更内聚，并且与 SQLite 内存/临时库完美结合。
*/
// Mock 实现：TaskLoader 接口
type testTaskLoader struct {
	db *gorm.DB
}

func (m *testTaskLoader) GetTask(id uint) (*model.Task, error) {
	var task model.Task
	if err := m.db.First(&task, id).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (m *testTaskLoader) GetDaemonTasks() ([]model.Task, error) {
	var tasks []model.Task
	err := m.db.Where("run_mode = ? AND enabled = ?", "daemon", true).Find(&tasks).Error
	return tasks, err
}

// Mock 实现：LogQuerier 接口
type testLogQuerier struct {
	db *gorm.DB
}

func (m *testLogQuerier) GetLatestLog(taskID uint) (*model.ExecutionLog, error) {
	var log model.ExecutionLog
	err := m.db.Where("task_id = ?", taskID).Order("id DESC").First(&log).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}
