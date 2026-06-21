package notify

/*
📌 【大厂面试·核心考点】
1. "如何保证异步通知发送失败不丢失？"
   - 标准答案：需要引入可靠重试机制。内存中可使用带退避策略的延时队列（Delay Queue）；生产环境通常结合持久化存储（如 Redis ZSet、MQ 死信队列或本地 WAL 日志）实现。在发送失败时不仅要重试，还要记录重试次数以防死循环。
2. "Webhook 通知如何防止数据被篡改及防重放攻击？"
   - 标准答案：需要实现 Webhook 签名（Signature）与防篡改验证。发送方通常使用预先分配的 Secret 结合摘要算法（如 HMAC-SHA256）对 Payload + Timestamp 进行签名，并在 HTTP Header 中附加 `X-Webhook-Signature` 与 `X-Timestamp`。接收方校验 Timestamp 误差范围（如 5分钟内防重放），并重新计算签名比对（防篡改）。
3. "如果主通知渠道彻底瘫痪，你的系统如何应对？"
   - 标准答案：实现降级 (Fallback) 机制。当主要通知（如 Webhook、钉钉）连续失败超过阈值（熔断），应立即降级到备用链路（如发邮件、存入本地紧急错误日志或触发短彩信报警）。

🏗️ 【架构设计·模式对比】
通知调度架构选型：
- 进程内 Channel 缓冲（本代码测试模型）：轻量、部署简单。缺点：进程崩溃即丢弃数据，阻塞导致反向拖垮主线程。
- 本地落盘队列（如基于 SQLite/BoltDB）：中等体量使用。不依赖外部中间件，且防崩溃丢失。
- 独立通知网关（结合 Kafka/Redis）：适合微服务。解耦通知与业务，提供统一的限流、重试、降级和动态路由能力。

🛡️ 【安全攻防·漏洞防线】
SSRF 漏洞防御：Webhook 地址如果是用户自定义的，必须做严格的白名单校验或内网 IP 阻断，否则攻击者可利用 Webhook 探测企业内网服务（即 Server-Side Request Forgery 漏洞）。

💀 【踩坑血泪·反面教材】
某大厂曾因为直接在调度主链路中同步发送 HTTP Webhook，当外部服务遭遇 DDoS 攻击响应极慢时，导致系统调度核心 Goroutine 全部阻塞在 `http.Post` 上，发生大规模系统级雪崩。因此，测试验证 "并发满载不阻塞" 的非阻塞特性是保障核心稳定性的命门。
*/

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	// 🔬 【底层原理·深度剖析】
	// 这里的 import 中 net/http/httptest 是 Go 标准库提供的极品测试工具。
	// 它可以真实启动一个绑定在本地随机端口的 HTTP Server（底层依然走 TCP 层）。
	// 相比于 Mock HTTP Client（如重写 RoundTripper），httptest 能够真正暴露出网络层的超时、连接复用、Header 解析等问题。

	"cronix/internal/domain/model"
)

// 🧪 【测试工程·质量保障】
// TDD 思想与 Red 阶段：本测试在没有实现可靠异步重试、降级与限流等逻辑前编写，预期会暴露出"阻塞"问题（Red 阶段）。
// 只有当我们引入了正确的解耦与满载丢弃/持久化兜底逻辑后，测试才会变成 Green 阶段。
// 坚决遵循"零污染"：所有测试对真实世界没有任何副作用，通过 `httptest` 创建生命周期与测试等长的 Mock Server，测试结束后自然销毁。

// TestNotifier_Concurrency 验证通知发送器在高并发下不会阻塞调用方（RED阶段预期失败）
// 当前单线程模型下，如果Webhook慢，超过256个缓冲就会阻塞
func TestNotifier_Concurrency(t *testing.T) {
	// ⚡ 【性能实战·生产调优】
	// Webhook 服务端的耗时直接决定了消费速度。在单消费者模型下，如果外部 API 响应时间为 20ms，
	// 则理论最大吞吐量仅为 50 TPS (1000ms / 20ms)。
	// 若突发流量达到 300 QPS，通道在几秒内必满。

	// 1. Mock 一个龟速的 Webhook 服务器（每次请求耗时 20ms）
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 🛡️ 【安全攻防·漏洞防线】
		// （未来需在此补充验证）在这里可以获取 HTTP Request 中的 Header，
		// 验证 `Authorization` 或 `X-Signature` 等 Webhook 签名防篡改字段，
		// 如果签名不通过，应返回 401 状态码，触发并测试上游的失败重试与降级(Fallback)机制。

		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close() // 🧪 测试完立刻销毁端口资源，防止系统资源与 Socket 句柄泄露

	// 2. 初始化 Notifier
	n := New(0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动消费者
	go n.Start(ctx)

	// 3. 构造 300 条通知（超过目前硬编码的 256 缓冲）
	event := NotifyEvent{
		TaskName: "test-task",
		Status:   "success",
		Config: model.NotifyConfig{
			NotifyType: "webhook",
			WebhookURL: ts.URL,
		},
	}

	// 4. 尝试极速塞入 300 条通知
	// 在目前的单线程架构下，前几条会很快被拿走执行，但执行要20ms。
	// 当瞬间塞满256条后，由于消费者消费极慢，第 257 条必定会阻塞！
	successCount := 0
	startTime := time.Now()
	for i := 0; i < 300; i++ {
		// 🔬 【底层原理·深度剖析】
		// Go语言核心机制：select 多路复用与 Channel 操作。
		// 当执行 `n.NotifyChan() <- event` 时，底层的 `chansend` 函数会被调用。
		// 如果缓冲队列已满且没有空闲的接收者，当前 Goroutine 会被包装成 `sudog` 挂起到 Channel 的 `sendq` 队列。
		// 但因为这里配合了 `time.After`，编译器会将其特殊优化为非阻塞带超时的发送，
		// 若超时，则进入 default/超时 分支，保护了主线程不被永久挂起（这就是降级 fallback 兜底的雏形）。
		select {
		case n.NotifyChan() <- event:
			successCount++
		case <-time.After(50 * time.Millisecond):
			// 💀 【踩坑血泪·反面教材】
			// 如果没有外层的 select 与超时控制，直接使用 `n.NotifyChan() <- event`。
			// 在缓冲满时，主业务调度器将永久死锁（Goroutine 挂起等待）。
			// 这是生产环境中系统从局部变慢演变成全局瘫痪的经典反面教材！

			// 如果 50ms 内塞不进去，说明通道满了并且消费者卡死了
			t.Fatalf("Notification channel blocked at event %d! Architecture is synchronous and vulnerable to flooding.", i)
		}
	}

	// 如果没有阻塞，应该能瞬间完成塞入
	elapsed := time.Since(startTime)
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Submitting events took too long: %v, expected almost instant non-blocking behavior", elapsed)
	}
	
	t.Logf("Successfully enqueued %d events in %v", successCount, elapsed)
}
