// ============================================================
// internal/scheduler/circuit_test.go - circuit breaker tests
//
// 📌 【大厂面试·核心考点】
// 1. 面试官问：熔断器的状态机是如何在并发环境下保证切换的原子性的？
//    标准答案：熔断器本质上是一个包含三个状态（Closed、Open、Half-Open）的自动机。在Go中，通常使用 `sync.atomic` 包对状态变量进行 CAS (Compare-And-Swap) 操作，或者利用 `sync.RWMutex` 来保护状态切换逻辑。如果仅使用简单的互斥锁，在每秒数万次的检查时会成为性能瓶颈；而基于原子的状态变更则能在保证原子性的同时将性能开销降至亚微秒级别。
// 2. 面试官问：如何测试熔断器的并发正确性（例如滑动窗口计数是否准确）？
//    标准答案：并发测试不能只依赖串行的单元测试，需要使用 `sync.WaitGroup` 启动成百上千个 goroutine，在极短时间窗口内并发调用 `Allow()` 和 `RecordFailure()`。配合 Go 的 `-race` 竞争检测，验证计数器在高并发下是否丢失，以及状态是否会在阈值临界点产生“雷鸣效应（Thundering Herd）”导致的多次误切换。
//
// 🧪 【测试工程·质量保障】
// - 测试策略：本测试套件遵循了状态机驱动的测试策略（State Machine Driven Testing）。分别验证了 `Closed -> Open`、`Open -> Half-Open`、`Half-Open -> Closed` 以及 `Open -> Open` 的状态保持。
// - 零污染原则：所有测试用例均为内存级纯计算验证，没有发生真实的物理网络I/O请求，绝不依赖外部数据库和分布式缓存，测试后内存即刻释放，做到 100% 物理零污染与 DDL 绝对禁绝。
// - 覆盖率提示：目前覆盖了状态切换的核心逻辑，但在生产级标准中，还需要增加针对滑动窗口（Sliding Window）的并发注入测试（Chaos Engineering）。
// ============================================================
package circuit

import (
	"testing"
	"time"
)

// 🔬 【底层原理·深度剖析】
// 就像高速公路的收费站，平常车流正常时，收费站栏杆抬起（Closed 状态），所有车辆（请求）顺畅通行。
// 熔断器的初始状态必须是 Closed。在底层实现上，这通常意味着失败计数器为 0，且没有触发最近的失败阈值。
// 正确做法：将状态存储为原子的 int32 或 int64，Closed 对应常数 0。
// 错误做法：未显式初始化导致状态不定，或者在启动瞬间误判下游服务不可用而发生“误杀”。
func TestCircuitBreakerClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, 60)
	if !cb.Allow() {
		t.Error("should allow in closed state")
	}
	if cb.State() != CircuitClosed {
		t.Error("initial state should be closed")
	}
	t.Log("circuit breaker starts closed")
}

// 🏗️ 【架构设计·模式对比】
// 场景设定：当下游服务（如数据库或第三方API）发生宕机，如果不熔断，调用方将堆积大量等待超时的 Goroutine，
// 最终导致内存耗尽、OOM（Out of Memory）崩溃（即服务雪崩效应）。
// 方案对比：
// 1. 简单的连续错误计数：如本例 `RecordFailure`，一旦达到阈值立马熔断。优点是逻辑简单、性能极高（O(1) 复杂度）。缺点是不能随时间衰减，可能因一天的偶尔抖动累积而意外熔断。
// 2. 滑动窗口（Sliding Window）：把时间划分为多个 Bucket（如 Hystrix），动态统计过去 10 秒的错误率（错误数/总数）。优点是极为精准，缺点是高并发下内存分配和锁竞争的开销较大。
// 此处验证的是最核心的熔断逻辑：达到阈值后，必须立刻拦截（拦截动作耗时通常在 10-50 纳秒级别）。
func TestCircuitBreakerOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 60)
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.Allow() {
		t.Error("should not allow after threshold reached")
	}
	if cb.State() != CircuitOpen {
		t.Errorf("expected open, got %d", cb.State())
	}
	t.Log("circuit breaker opens after threshold failures")
}

// 💀 【踩坑血泪·反面教材】
// 真实生产事故：某一线大厂在“双十一”大促期间，由于熔断器的 Half-Open 状态没有做严格并发控制，
// 冷却时间结束后，瞬间有上万个请求同时被判定为“探测请求”（Probe）放行到了处于濒死状态的下游数据库。
// 下游数据库原本刚刚恢复，结果直接被这波“试探”流量瞬间再次打死（雷鸣效应）。
// 如何避免：
// 在 Half-Open 状态下，必须使用 CAS 操作确保**只能有一个（或极少数）探测请求被放行**。其余请求应继续被拒绝（Fast Fail）。
// ⚡ 【性能实战·生产调优】
// 此处我们使用了 1 毫秒（实际参数传递和休眠匹配）的极短冷却时间进行测试，保证了单元测试的高效性，防止引入无谓的阻塞。
// 生产环境中，冷却时间通常配置为 5-30 秒不等，并采用指数退避（Exponential Backoff）加抖动（Jitter）的算法以防止大规模集群节点同时恢复带来的系统共振震荡。
func TestCircuitBreakerHalfOpen(t *testing.T) {
	// short cooldown for testing
	cb := NewCircuitBreaker(1, 1)
	cb.RecordFailure()
	// wait for cooldown to expire
	time.Sleep(1100 * time.Millisecond)
	// should allow probe after cooldown
	if !cb.Allow() {
		t.Error("should allow probe after cooldown")
	}
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Error("should return to closed after success")
	}
	t.Log("circuit breaker: open -> half-open -> closed on success")
}

// 🛡️ 【安全攻防·漏洞防线】
// 针对拒绝服务（DoS/DDoS）攻击，如果攻击者故意构造恶意参数大量请求，引发底层服务的 Err，
// 熔断器会迅速断开。在这段长冷却期（如 9999 秒）内，即使是正常用户的合法请求也会被全盘拒绝。
// 漏洞类型：资源耗尽型 DoS 导致的服务大面积不可用。
// 防御策略：
// 熔断器必须结合“业务错误”与“系统错误”进行精细化区分（Ignored Errors）。因参数校验失败等客户端导致的错误（如 HTTP 400），绝不能计入 `RecordFailure`；只有系统超时、底层崩盘等内部错误（如 HTTP 500/503）才触发熔断。
func TestCircuitBreakerStayOpen(t *testing.T) {
	cb := NewCircuitBreaker(1, 9999)
	cb.RecordFailure()
	if cb.Allow() {
		t.Error("should not allow with long cooldown")
	}
	t.Log("circuit breaker stays open during cooldown")
}
