package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cronix/internal/model"
)

// TestNotifier_Concurrency 验证通知发送器在高并发下不会阻塞调用方（RED阶段预期失败）
// 当前单线程模型下，如果Webhook慢，超过256个缓冲就会阻塞
func TestNotifier_Concurrency(t *testing.T) {
	// 1. Mock 一个龟速的 Webhook 服务器（每次请求耗时 20ms）
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

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
		select {
		case n.NotifyChan() <- event:
			successCount++
		case <-time.After(50 * time.Millisecond):
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
