/*
📌 【架构设计·模式对比】
Executor Quota（执行器配额与限流）是分布式任务调度系统（如 XXL-JOB、ElasticJob 等）中不可或缺的保护机制。
在任务密集型系统中，日志表的数据量会呈指数级增长。如果不限制单个任务或全局的日志记录数，
会导致数据库磁盘被打爆，并且全表扫描或分页查询性能急剧下降。
本文件专注于测试单任务级别的日志限额控制机制。

🧪 【测试工程·质量保障】
遵循物理零污染原则，采用内存 SQLite (基于临时文件) 进行测试，使得测试与环境隔离。
在执行完后，自动清理 DB，并在测试结束时确保资源得到释放。
使用“插桩-触发-校验”的标准三段式（Arrange-Act-Assert）单测模式进行断言。

📌 【大厂面试·核心考点】
面试官：你设计的调度系统如何避免海量调度日志拖垮数据库？
标准答案：
1. 采用多级日志清理策略：
   - 细粒度（单任务级）：配置每个任务最大保留日志数（如：最近 10 条）。
   - 粗粒度（全局级）：定期清理过期数据（如：只保留最近 7 天的日志）。
2. 异步化执行清理：日志的清理和插入应尽量解耦，避免业务核心路径（Task Execute）被慢 SQL 阻塞。
3. 如果数据规模极大，将日志存储由 RDBMS 迁移至 ElasticSearch / ClickHouse 或采用冷热分离。
*/
package scheduler

import (
	"cronix/internal/infrastructure/config"
	"cronix/internal/domain/model"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupExecutorTestDB 初始化 Executor 模块所需的隔离测试数据库。
//
// 🔬 【底层原理·深度剖析】
// GORM 在处理内存 SQLite 或是临时 SQLite 文件时，可以做到高度隔离。
// 使用 `t.TempDir()` 可以由 go test 框架接管临时目录的生命周期，
// 在测试结束后会自动递归删除（包含由于 DB 持久化产生的临时文件），
// 极大地简化了 TearDown (清理操作) 的负担，避免由于宕机等意外遗留垃圾文件。
//
// ⚡ 【性能实战·生产调优】
// 测试中将 gorm logger 的日志级别设置为 Warn 级别，可以大幅减少
// SQL 打印造成的 I/O 和 Console 渲染开销，在拥有成百上千个测试的 CI/CD 流水线中，
// 降低测试时长。
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
	db.AutoMigrate(&model.Task{}, &model.ExecutionLog{}, &model.GroupExecutionLog{}, &model.NotifyConfig{}, &model.TaskGroup{}, &model.TaskDep{})
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return db
}

// TestExecutor_TaskLevelQuota 测试单任务级别的数据库日志限额逻辑
//
// 💀 【踩坑血泪·反面教材】
// 曾有生产事故：某研发配置了一个每秒执行 1 次的心跳检查任务，
// 一天产生 86400 条执行日志。由于调度框架没有针对单任务的记录数限制，
// 一周后日志表高达上亿行，导致后台打开任务执行记录页时发生 OOM 与数据库 CPU 100%。
// 此单测就是为了预防和验证上述场景的拦截能力。
//
// 🧪 【测试工程·质量保障】
// 测试设计思路：
// 1. (Arrange) 准备环境，插入超过配额阈值的旧日志数据（模拟历史积压）。
// 2. (Act) 启动执行器并执行一次新任务，触发清理或配额控制。
// 3. (Assert) 校验日志总数是否被限制在预期的阈值内。
func TestExecutor_TaskLevelQuota(t *testing.T) {
	db := setupExecutorTestDB(t)

	// 创建测试任务
	// 📌 【架构设计·模式对比】
	// 这里通过代码层级的模型构建替代了真实的外部请求，
	// 直接走底层的数据初始化，绕过 API 层，保持测试聚焦在配额和日志清理领域逻辑。
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
	// ⚡ 【性能实战·生产调优】
	// 在测试中批量插入（如果使用 DB.CreateInBatches）性能会远高于循环单条插入，
	// 但在样本数据极小（15条）时，循环写入可接受。如果是大规模压测，
	// 切记使用 `db.CreateInBatches(&logs, 100)` 以减轻 DB 压力并加速测试。
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
	// 🔬 【底层原理·深度剖析】
	// LogConfig.MaxLogsPerTask 参数的设计至关重要。
	// 底层通常会在触发一次 Task 运行完毕后，开启一个 Goroutine，异步查询该 Task 的日志总数，
	// 当 count > max_logs 时，采用如 `DELETE FROM execution_logs WHERE task_id = ? AND id NOT IN (SELECT id FROM execution_logs WHERE task_id = ? ORDER BY id DESC LIMIT ?)` 这种类似的 SQL 将最旧的数据清理掉。
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
	// 🛡️ 【安全攻防·漏洞防线】
	// executor.executeTask 内部需要对潜在的 Panic 进行 recover。
	// 在测试中显式调用该函数，也相当于顺带验证了单次运行调用的安全性（不崩溃）。
	// 期望：运行完任务，插入第 16 条日志后，触发限额清理（限制最多 10 条）。
	// 结果：该任务的日志数应该被截断并限制为最多 10 条。
	executor.executeTask(task.ID)

	// 因为 limitTaskLogs 现在是完全异步的（脱离了 executeTask 的主路径），
	// 这里给后台 goroutine 留一点时间完成数据库清理操作。
	//
	// ⚡ 【性能实战·生产调优】
	// 为什么需要 time.Sleep？
	// 异步清理（Async Cleanup）模式能显著降低 executeTask 主干流程的时延（Latency）。
	// 如果是同步清理，每次执行后都会增加几毫秒到几十毫秒的数据库耗时。
	// 但这在测试中引入了数据一致性竞态，所以我们采用 sleep 作为简易的等待。
	// 在更高要求的工程实践中，可以采用 sync.WaitGroup 或是 Channel 进行准确的信号同步。
	time.Sleep(200 * time.Millisecond)

	var count int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&count)

	// 如果 count 大于 10，说明单任务限额清理逻辑还未实现，测试失败（符合 RED 预期）
	//
	// 📌 【大厂面试·核心考点】
	// 面试官：如果单测中经常出现时序问题（Flaky tests），你会怎么解决？
	// 标准答案：禁止使用固定时间（time.Sleep），应当使用轮询机制（Poller）+ 超时等待。
	// 比如：使用类似 `require.Eventually(t, condition, 1*time.Second, 10*time.Millisecond)`，
	// 每 10 毫秒检查一次 condition，最大等待时间 1 秒。
	// 当条件满足立即退出，既保证测试稳定又提升测试速度。
	if count > 10 {
		t.Errorf("expected task logs count to be limited to 10, but got: %d", count)
	}
}
