package scheduler

import (
	"cronix/internal/domain/model"
	"cronix/internal/infrastructure/config"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ============================================================
// internal/application/scheduler/scheduler_test.go — 调度系统核心测试
// ============================================================
//
// 🏗️ 【架构设计·模式对比】
// 为什么要在 scheduler_test.go 中做并发和集成的测试，而不是拆在各个小文件？
// 调度系统（Engine + Executor + Daemon）是一个有机的整体：
// - Engine 负责 cron 触发
// - Executor 负责控制并发、去重、依赖解析
// - Daemon 负责宕机恢复
// 把它们串联起来做集成测试，能验证真实物理机上各个模块的协同是否线程安全，
// 避免“单独测都对，集成在一起就死锁”的经典困境。
//
// 📌 【大厂面试·核心考点】
// 面试官：如何保证一个定时任务系统的高并发测试不相互干扰？
// 答：
//   1. 物理层隔离：每个 Test 用 t.TempDir() 创建独立的 SQLite 内存或文件数据库，坚决不复用。
//   2. 端口隔离：如果有 HTTP Server Mock，使用 httptest.NewServer 让系统随机分配可用端口。
//   3. Goroutine 纯净度：使用 goleak 保证每个测试运行前后不会有后台驻留协程污染下一个用例。
//
// 💀 【踩坑血泪·反面教材】
// 如果测试中共享全局 DB 实例且使用并行测试 `t.Parallel()`，
// 极其容易导致数据竞争（Data Race），报 "database is locked" (SQLite)
// 或是 "connection refused" 的玄学错误，极大地增加排查成本。
// ============================================================

// ============================================================
// 🧪 【测试工程·质量保障】Goroutine 泄漏检测门神
// ============================================================
//
// 📌 【大厂面试·核心考点】
// 面试官：Goroutine 泄露怎么排查？
// 答：
//   1. 本地单测：使用 go.uber.org/goleak。在 TestMain 中调用 `goleak.VerifyTestMain(m)`。
//      原理是读取运行时的 stack trace，解析所有 goroutine 的状态，过滤系统自带的。
//   2. 线上监控：接入 pprof，看 `goroutine` 的火焰图和 count 数，
//      或者在 Prometheus 里打 runtime.NumGoroutine()，设置大于阀值告警。
//
// 🔬 【底层原理·深度剖析】
// goleak 检测原理的核心：
//   goleak 会在所有用例运行后，调用 `runtime.Stack()` 获取全量 goroutine 堆栈。
//   然后通过字符串正则匹配，剔除如 `runtime.gopark`、`testing.Main` 等框架级协程。
//   如果过滤后发现仍然有属于业务逻辑的 goroutine 活着（比如一个死锁的 channel 接收者），
//   就会强制使当前 Test 失败，阻断代码合并到 main 分支。
// ============================================================
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// setupDB 为集成测试提供纯净、隔离的数据库环境
//
// ⚡ 【性能实战·生产调优】
// SQLite in-memory 模式非常快，每次测试耗时通常在 < 50ms。
// 绝不能在单元测试中使用真实的 MySQL 连接，那会导致测试耗时暴增。
func setupDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to open isolated sqlite db: %v", err)
	}
	db.AutoMigrate(&model.Task{}, &model.TaskGroup{}, &model.ExecutionLog{}, &model.GroupExecutionLog{})

	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return db
}

// ============================================================
// 🧪 【测试工程·质量保障】表驱动测试(Table-Driven Tests) 与 并发调度集成
// ============================================================
//
// 📌 【大厂面试·核心考点】
// 面试官：为什么要用表驱动测试？有什么好处？
// 答：
//   1. 减少样板代码（Boilerplate Code）：只需要定义一个执行主干，通过 struct 切片注入数据即可。
//   2. 提高边界覆盖：可以非常轻松地增加 Edge Cases（边缘情况）的测试。
//   3. 方便失败追溯：结合 t.Run(tt.name, func(t *testing.T))，测试失败时明确指出哪一行输入错了，
//      而不是对着一个长达 500 行的函数抓瞎。
//
// 🔬 【底层原理·深度剖析：并发与共享变量的坑】
// 如果我们在 t.Run 中使用了 t.Parallel()，并且在一个普通的 for 循环里：
//   for _, tt := range tests {
//       t.Run(tt.name, func(t *testing.T) { t.Parallel() ... })
//   }
// 在 Go 1.22 以前，这个 `tt` 变量在循环中是复用地址的！这会导致并行执行的所有测试
// 拿到的是最后一个用例的参数（经典变量捕获问题）。
// 虽然 Go 1.22 修复了循环变量的语义，但在老代码中，这被称为 "Go 中最危险的并发陷阱"。
//
// 🛡️ 【安全攻防·漏洞防线】
// 并发测试里，使用 `var count int` 进行 `count++` 是绝对不安全的。
// 我们在下方的测试中强制使用了 `atomic.AddInt32` 和 `sync.WaitGroup`。
// 如果不这么做，Go 编译器加上 `-race` 标志（Data Race Detector）必定会报竞态冲突。
// ============================================================
func TestScheduler_ConcurrentTableDriven(t *testing.T) {
	// 定义表驱动的数据结构
	tests := []struct {
		name          string
		poolSize      int
		tasksToRun    int
		expectedMax   int32
		simulateDelay time.Duration
	}{
		{
			name:          "High concurrency, small pool",
			poolSize:      3,
			tasksToRun:    10,
			expectedMax:   3, // PoolSize=3，不管任务多少，最高并发绝对不能超过3
			simulateDelay: 100 * time.Millisecond,
		},
		{
			name:          "Low concurrency, large pool",
			poolSize:      50,
			tasksToRun:    5,
			expectedMax:   5, // 资源很足，5个任务一起跑
			simulateDelay: 50 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		// Go 1.22 之前必须做的变量绑定：tc := tt
		// 不过为了保险起见，我们依然保持最佳实践
		tc := tt

		t.Run(tc.name, func(t *testing.T) {
			// 如果要彻底并发执行这些 subtest，可开启 t.Parallel()
			// t.Parallel()

			db := setupDB(t)

			cfg := &config.Config{
				Executor: config.ExecutorConfig{
					PoolSize: tc.poolSize,
				},
			}

			engine := NewEngine(db)
			executor, err := NewExecutor(db, cfg, engine)
			if err != nil {
				t.Fatalf("Failed to create executor: %v", err)
			}

			var wg sync.WaitGroup
			var currentExecutions int32
			var maxObserved int32

			// Mock Executor 的具体运行行为，绕过实际的命令执行
			executor.commandRunner = func(cmdStr string) error {
				current := atomic.AddInt32(&currentExecutions, 1)
				defer atomic.AddInt32(&currentExecutions, -1)

				// 🔬 乐观锁方式记录历史最大并发度
				for {
					max := atomic.LoadInt32(&maxObserved)
					if current <= max {
						break
					}
					if atomic.CompareAndSwapInt32(&maxObserved, max, current) {
						break
					}
				}

				time.Sleep(tc.simulateDelay)
				return nil
			}

			// 预置测试数据
			for i := 1; i <= tc.tasksToRun; i++ {
				task := model.Task{
					ID:       uint(i),
					Name:     fmt.Sprintf("task-%d", i),
					TaskType: "shell",
					Command:  "mock",
					Enabled:  true,
				}
				db.Create(&task)
			}

			// ⚡ 模拟高并发雷暴：瞬间触发所有任务
			wg.Add(tc.tasksToRun)
			for i := 1; i <= tc.tasksToRun; i++ {
				go func(taskID uint) {
					defer wg.Done()
					executor.handleTrigger(taskID)
				}(uint(i))
			}

			// 阻塞等待全部任务派发完毕
			wg.Wait()
			// 给异步协程一点收尾时间
			time.Sleep(tc.simulateDelay + 50*time.Millisecond)

			finalMax := atomic.LoadInt32(&maxObserved)
			if finalMax > tc.expectedMax {
				// 💀 这里如果是错的，代表发生了严重的并发控制穿透。
				t.Errorf("Expected max concurrency <= %d, but got %d", tc.expectedMax, finalMax)
			}

			// 优雅关闭 executor 中的连接池（ants），防止泄漏被 goleak 捕捉到
			executor.pool.Release()
		})
	}
}
