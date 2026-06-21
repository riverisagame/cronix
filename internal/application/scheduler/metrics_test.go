// 📌 【大厂面试·核心考点】
// 面试官问：在Go中如何测试并发指标的正确性？如果发生Data Race怎么排查？
// 标准答案：在测试时必须加上 `-race` 标志运行 `go test -race` 来检测数据竞争。
// 在设计指标收集器时，常用的并发保护机制包括：
// 1. 互斥锁（sync.Mutex / RWMutex）保护共享数据结构；
// 2. 使用 channel 将并发写入转化为单协程顺序处理（类似此处的 actor 模型，消除锁竞争）；
// 3. 使用 sync/atomic 包进行原子操作（适用于简单的无状态计数器，性能最高）。
// 测试 Prometheus 指标断言通常使用 `testutil.ToFloat64` 或模拟拉取 `/metrics` 接口解析数据，精确验证记录的准确性。
//
// 🧪 【测试工程·质量保障】
// 测试策略：指标测试需要全面覆盖“异步收集并发压测”、“断连重试”和“时序聚合精准度”。
// Mock原理：虽然这里测试了自定义结构的快照聚合，但在更深入的 Prometheus 生态体系中，
// 强烈建议利用 `prometheus/client_golang/prometheus/testutil` 包提供的内置断言方法
// （如 `testutil.GatherAndCompare` 或 `testutil.CollectAndCount`）实现规范的 Prometheus 注册表状态断言。
// 物理零污染原则：测试严禁污染真实组件；指标系统本身也必须对业务逻辑绝对透明，不抛异常，哪怕缓冲满也不应阻塞主业务。
package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 🔬 【底层原理·深度剖析】
// 这里的 MetricsRegistry 使用了异步处理机制（Start() 启动后台独立 goroutine 处理生命周期），
// 因此在业务方调用 RecordExecution 后，数据是通过 channel 非阻塞投递或加锁放入队列缓冲的。
// 这在底层原理上是一种典型的并发安全设计（CSP并发模型 / 环形缓冲无锁化编程的思想）。
// 在没有显式同步机制的情况下，如果多协程并发写入常规切片或 map，在Go运行时的并发读写检测机制下，必将触发 Data Race，严重时造成内存损坏及进程崩溃（panic）。
func TestMetricsRegistry_RecordAndSnapshot(t *testing.T) {
	registry := NewMetricsRegistry()
	registry.Start()
	defer registry.Stop()

	// Record some executions
	registry.RecordExecution(100, true)
	registry.RecordExecution(200, true)
	registry.RecordExecution(500, false)

	// 💀 【踩坑血泪·反面教材】
	// 真实生产事故案例：在异步系统的单测中，直接依赖硬编码的 `time.Sleep` 是一种典型的 Flaky Test（脆弱测试）反面教材。
	// 血泪教训：如果运行在负载极高的 CI/CD 并发容器中，CPU时间片分配延迟，50ms 内后台消费协程可能尚未将数据落盘/落表，从而导致测试偶发性（Flaky）失败，增加大量排查成本。
	// 如何避免：正确的做法应当使用 `sync.WaitGroup` 等待特定消费完成，或者在获取快照前提供一个具有重试性质的 `Eventually` 轮询断言，
	// 比如利用 testify 的能力：`assert.Eventually(t, func() bool { return len(registry.GetSnapshot().MinuteSuccess) > 0 }, 1*time.Second, 10*time.Millisecond)`。
	// Wait for async processing
	time.Sleep(50 * time.Millisecond)

	snapshot := registry.GetSnapshot()

	assert.NotEmpty(t, snapshot.MinuteLabels, "Expected minute labels to be populated")
	if len(snapshot.MinuteSuccess) > 0 {
		assert.Equal(t, int64(2), snapshot.MinuteSuccess[len(snapshot.MinuteSuccess)-1], "Expected 2 successes in the current minute")
		assert.Equal(t, int64(1), snapshot.MinuteFailed[len(snapshot.MinuteFailed)-1], "Expected 1 failure in the current minute")
	} else {
		t.Error("Expected at least one minute bucket")
	}
}

// ⚡ 【性能实战·生产调优】
// 性能数据与时间/空间复杂度：计算 P95/P99 等分位数（Percentile）时，传统的暴力计算方法是保存时间窗口内所有执行耗时然后全量排序（时间复杂度 O(N log N)）。
// 但在高并发调度器中（例如每秒万级 QPS），全量保留样本将引发内存飙升（O(N) 空间复杂度）以及极其严重的垃圾回收（GC）停顿（STW抖动）。
// 优化手段：在真实的超大规模生产调优中，我们通常采用近似流式算法，如 T-Digest、HDR Histogram 或基于固定位宽的分桶聚合计数。
//
// 🛡️ 【安全攻防·漏洞防线】
// 内存耗尽攻击（OOM）风险点：如果调度系统处理来源于外部输入的任务，且未对任务类型维度（Label）限制数量，
// 恶意构造的海量不同标签的流量将导致指标存储器内部分配无数的新时序记录区，最终导致应用进程 OOM 被操作系统 OOM Killer 强杀。
// 防御策略：使用固定容量的哈希表并实行 LRU 淘汰淘汰策略，同时对标签组合维度（Label Cardinality）实行严格白名单审查，彻底拦截时间序列爆炸攻击（Cardinality Explosion）。
func TestMetricsRegistry_P95P99(t *testing.T) {
	registry := NewMetricsRegistry()
	registry.Start()
	defer registry.Stop()

	// Push 100 durations: 1 to 100
	for i := int64(1); i <= 100; i++ {
		registry.RecordExecution(i, true)
	}

	time.Sleep(50 * time.Millisecond)

	snapshot := registry.GetSnapshot()
	if len(snapshot.MinuteP95) > 0 {
		p95 := snapshot.MinuteP95[len(snapshot.MinuteP95)-1]
		p99 := snapshot.MinuteP99[len(snapshot.MinuteP99)-1]

		// 🏗️ 【架构设计·模式对比】
		// 内存计算推送模型 vs Prometheus 纯粹拉取与服务端聚合模型：
		// 此处测试直接验证了在应用内存计算出 P95/P99 快照的逻辑。这类似 Dropwizard/StatsD 等传统监控的常见设计（富客户端模式）。
		// 相比之下，云原生更推崇 Prometheus 生态：我们不再在客户端直接计算分位数，而是暴露 Counter 和 Histogram（特定耗时的分界桶 buckets）。
		// 选型理由（为何推荐后者）：
		// 1. 在本地使用 Summary 类型计算分位数由于要维持滑动窗口，性能开销昂贵；
		// 2. 最致命的是，应用端直接计算出来的 P95 或 P99 是无法跨越不同实例节点进行再聚合的（各节点的P95求平均毫无数学意义）。
		// 替代方案：更现代的架构设计是使用 `Prometheus Histogram`，通过轻量级递增将计算压力卸载，
		// 随后配合强悍的 PromQL 语法 `histogram_quantile(0.99, sum(rate(scheduler_task_duration_seconds_bucket[5m])) by (le))` 在服务端动态进行集群维度的准实时近似计算。
		// 95th percentile of 1..100 should be >= 95
		assert.GreaterOrEqual(t, p95, int64(95))
		assert.GreaterOrEqual(t, p99, int64(99))
	} else {
		t.Error("Expected at least one minute bucket")
	}
}
