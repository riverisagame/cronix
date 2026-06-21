// 📌 【大厂面试·核心考点】
// 面试官：如何测试高并发系统中的线程池是否真正起效？如何检测潜在的Data Race？
// 标准答案：
// 1. 使用 atomic 包维护当前并发数和历史最大并发数（无锁并发统计）。
// 2. 使用 httptest 提供可控延迟的Mock服务，模拟慢速IO操作，以强制占用Worker资源，促使线程池饱和。
// 3. 通过同时（for loop 并发或者批量下发）触发大量任务，测试当投递量大于 PoolSize 时的系统表现。
// 4. 验证 maxConcurrentExecutions 是否严格小于等于 PoolSize，若大于则说明存在“协程逃逸”或线程池隔离失效的架构漏洞。
//
// 🧪 【测试工程·质量保障】
// 本文件主要覆盖调度执行器（Executor）的高并发压测与边界场景测试。
// 遵循“物理零污染”原则，所有测试数据仅在测试专用的DB内存/隔离库中产生，绝不向外部真实业务服务发请求，使用本地 mock server (httptest) 处理模拟任务。
package scheduler

import (
	"cronix/internal/infrastructure/config"
	"cronix/internal/domain/model"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestExecutor_ConcurrencyBypass_Red 验证普通定时任务触发时，
// ants.Pool 是否能有效限制并发（修复前会被架空，并发度无限大）。
//
// 🔬 【底层原理·深度剖析】
// 任务调度系统的防线在于“协程池”。在 Go 中，轻易的 `go func()` 会导致协程数量爆炸（Goroutine Leak），
// 最终引发 OOM 或引发下游被 DDoS。引入协程池（如 ants）是为了限流。
// 这个测试正是用于验证“架构防线”是否被击穿（Bypass），即新进来的任务是否无视了协程池的最大容量。
func TestExecutor_ConcurrencyBypass_Red(t *testing.T) {
	db := setupExecutorTestDB(t)

	// 🛡️ 【安全攻防·漏洞防线】
	// 必须使用原子操作（atomic）来记录并发量。如果在并发测试用普通的 var int32 累加，会导致 Data Race。
	// 配合 go test -race 能够检测出此类隐蔽的竞态条件。
	var concurrentExecutions int32
	var maxConcurrentExecutions int32
	var wg sync.WaitGroup

	// 🧪 【测试工程·质量保障】
	// 使用 httptest.NewServer 本地启动一个轻量级 HTTP 服务。
	// 优点：1. 完全隔离，无外部真实网络依赖；2. 随意控制延迟和返回码，方便构造“慢IO”堵塞场景以打满协程池。
	// Setup a slow HTTP server to track concurrency
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ⚡ 【性能实战·生产调优】
		// 模拟请求进入，当前并发数无锁安全 +1。使用 defer 保证退出时安全 -1。
		current := atomic.AddInt32(&concurrentExecutions, 1)
		defer atomic.AddInt32(&concurrentExecutions, -1)

		// 📌 【大厂面试·核心考点】
		// 面试官：除了 sync.Mutex，如何实现无锁更新最大值？
		// 标准答案：CAS (Compare-And-Swap) 自旋（SpinLock）。
		// 获取当前最大值，如果发现 current 更大，则尝试 CAS 替换。若期间别的协程改了最大值，CAS 会失败，通过 for 循环重试即可。
		for {
			max := atomic.LoadInt32(&maxConcurrentExecutions)
			if current <= max {
				break
			}
			if atomic.CompareAndSwapInt32(&maxConcurrentExecutions, max, current) {
				break
			}
		}

		// 💀 【踩坑血泪·反面教材】
		// 如果这里不主动 Sleep，由于本地调度极快，任务瞬间执行完释放了 Worker，协程池就永远达不到饱和。
		// 必须刻意制造延迟（300ms），让前 2 个任务牢牢霸占 Pool 里的 2 个 Worker，后续任务才会面临 Rejected / Blocking 策略。
		time.Sleep(300 * time.Millisecond) // Simulate slow execution
		w.WriteHeader(http.StatusOK)
		wg.Done()
	}))
	defer ts.Close()

	// 🏗️ 【架构设计·模式对比】
	// 这里刻意将 PoolSize 设为极其有限的值 (2)，充当高并发测试的“探针”。
	// 生产环境下架构选型指南：
	// - 如果是 CPU 密集型任务，PoolSize 建议设置为 GOMAXPROCS (通常等于核数)。
	// - 如果是 IO 密集型任务（如本项目的 HTTP 请求分发调度），PoolSize 建议设置为 100~1000（视机器内存与下游承受力而定）。
	// Configure pool size to exactly 2
	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:         2, 
			OutputTruncateKB: 64,
		},
	}

	engine := NewEngine(db)
	executor, err := NewExecutor(db, cfg, engine)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	// ⚡ 【性能实战·生产调优】
	// 批量下发测试数据。10 个任务足以撑爆我们容量只有 2 的微型池。
	// Create 10 slow tasks
	numTasks := 10
	for i := 1; i <= numTasks; i++ {
		task := model.Task{
			ID:         uint(i),
			Name:       fmt.Sprintf("task-slow-%d", i),
			TaskType:   "http",
			HTTPMethod: "GET",
			HTTPURL:    ts.URL,
			Enabled:    true,
			TimeoutSec: 10,
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	// 💀 【踩坑血泪·反面教材】
	// 此处模拟大批量任务被 Cron Timer 同时触发（Burst流量）。
	// 之前系统出现架构漏洞的原因：在 trigger 逻辑内，没有将任务通过规范接口 submit 到 pool 中，
	// 或者遇到了 pool 满时，错误地 fallback 到了隐式的 `go exec()`，导致这 10 个任务瞬间无视容量上限全量跑起。
	// 正确的 Worker 拒绝策略 (Reject Policy) 应该是：阻塞等待、直接丢弃、或投递到重试消息队列，绝不能退化为无界并发。
	// Trigger all 10 tasks at virtually the same time
	wg.Add(numTasks)
	for i := 1; i <= numTasks; i++ {
		executor.handleTrigger(uint(i))
	}

	// Wait for all to finish
	wg.Wait()
	time.Sleep(300 * time.Millisecond) // Let async logs finish

	maxConcurrent := atomic.LoadInt32(&maxConcurrentExecutions)
	t.Logf("Max concurrent executions observed: %d", maxConcurrent)

	// 🧪 【测试工程·质量保障】
	// 测试边界断言：这是整场测试的核心灵魂。一旦 maxConcurrent 超过了物理配置的 PoolSize 容量，
	// 直接证明系统的限流功能已实质性失效，Worker池被架空（Bypassed）。
	// 这种属于极为严重的架构级别 Bug，必须以 FATAL / ERROR 级别阻断发布流程。
	// Since pool size is 2, the absolute maximum concurrent executions should be 2.
	// If it exceeds 2, it proves the ants pool is being bypassed.
	if maxConcurrent > 2 {
		t.Errorf("FATAL ARCHITECTURE BUG: PoolSize is 2, but observed %d concurrent executions! Thread pool is being bypassed.", maxConcurrent)
	}
}
