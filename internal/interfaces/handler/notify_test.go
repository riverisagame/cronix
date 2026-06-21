/*
📌 【大厂面试·核心考点】
面试官：设计一个高可用、高安全的 Webhook 回调通知系统，需要考虑哪些核心问题？
标准答案：
1. 安全性设计（防伪造与防重放）：采用 HMAC-SHA256 签名机制防止中间人篡改。在 Request Header 中带上 Timestamp（时间戳）和 Nonce（一次性随机数），防止重放攻击（Replay Attack）。请求接收端必须验证签名，且时间戳的误差范围通常限制在 5 分钟内。
2. 可靠性设计（防丢失）：Webhook 推送必须与业务主逻辑解耦。采用异步重试机制（通常结合指数退避算法），基于本地消息表（Outbox Pattern）或消息队列（如 Kafka/RabbitMQ）实现最终一致性，避免下游服务不可用导致上游主业务阻塞。
3. SSRF 防御（防内网探测）：必须校验 Webhook 目标 URL，禁止访问 127.0.0.1、局域网 IP 段（192.168.x.x, 10.x.x.x）以及特殊协议如 file://、gopher:// 等，防止服务器沦为攻击内网的肉鸡。

🛡️ 【安全攻防·漏洞防线】 HTTP Webhook 回调安全认证测试
生活比喻：就像古代送密信，不能光看目的地，还得核对火漆印（签名），看日期是不是旧信（防重放），不能随便把绝密信件交给来路不明的人。
- 防重放攻击 (Anti-Replay)：恶意攻击者如果截获了通知请求，原样再次发送，会导致下游系统产生重复发货或重复扣款等严重脏数据。防御策略是引入 `X-Timestamp` 和 `X-Nonce`，服务端通过 Redis 缓存并校验 Nonce 是否已消费。
- 签名校验 (Signature Verification)：将 Body 体内容、Timestamp、Nonce 组合后，使用预分配的 Secret Key 进行 HMAC-SHA256 哈希，放入请求头 `X-Signature` 中。接收方以相同算法计算并比对，任何对 Body 哪怕一个字节的篡改都会导致校验失败。

⚡ 【性能实战·生产调优】 高并发下的吞吐压测
对于 Webhook 的发送端（即当前系统），在十万级并发场景下直接发起 `http.Post` 会瞬间耗尽系统的 TCP 端口资源（TIME_WAIT 飙升）并打满文件句柄（fd）。
- 客户端优化：必须复用全局的 `http.Transport`，合理调大 `MaxIdleConns` 和 `MaxIdleConnsPerHost`（例如设为 1000），并设置严格的超时限制（如 Timeout = 3s）。
- 吞吐压测：在写压测用例（Benchmark）时，可利用 `httptest.NewServer` 启动一个伪造的下游，使用 `b.RunParallel` 来模拟高并发推送，观察内存逃逸和 Goroutine 数量的爆炸情况，并在生产环境引入有界协程池（如 `ants`）来进行背压（Backpressure）限流。
*/
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cronix/internal/domain/model"
	"cronix/internal/application/service"

	// 🔬 【底层原理·深度剖析】
	// * net/http/httptest：Go 标准库提供的 HTTP 零开销测试神器。它能直接在进程内存中构建 ResponseRecorder 和 Request，使得整个 HTTP 链路测试无需真正监听端口（Listen & Serve）。避免了端口占用冲突、TCP 握手开销以及操作系统的网络栈层转换，极大提升了测试并发度和执行速度。
	// * github.com/gin-gonic/gin：Go 语言主流的高性能 HTTP 框架。底层由于采用了基于基数树（Radix Tree / HttpRouter）的路由实现，其路由匹配过程不会使用影响性能的正则回溯，而是逐字符比对，具有极高的性能表现（时间复杂度 O(K)，K 为 URL 长度）。
	// * github.com/glebarez/sqlite：纯 Go 实现的 SQLite 驱动。与传统的基于 CGO 编译的 mattn/go-sqlite3 不同，它消除了跨平台交叉编译（Cross-Compile）的痛点，特别适合在容器化 CI/CD 流水线中进行零配置依赖的快速单元测试。
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

/*
🧪 【测试工程·质量保障】 物理零污染与 DDL 绝对禁绝
生活比喻：在沙盘上推演战争（内存测试），绝不能用真炸弹去炸毁真实的房屋（生产数据库）。
1. 内存数据库隔离：此测试采用了 `file::memory:?cache=shared` 的 SQLite 内存数据库方案。所有表的创建（AutoMigrate）、数据的增删改查全都在当前进程的一块内存区中发生，测试执行完毕后随着进程退出立刻烟消云散，实现了真正的“物理零污染”。
2. 杜绝高危操作：优秀的测试原则是：哪怕在集成测试连接真实独立数据库时，物理源码层中也绝对严禁出现任何 `DROP`、`TRUNCATE` 等毁灭性语句！对于数据状态重置，最佳实践是开启嵌套事务（Transaction），在单个测试用例运行结束后触发无条件的 `Rollback`，从而让测试后的表结构与数据 100% 毫发无损。
3. TDD（测试驱动开发）RED阶段思维：当前测试体现了先写规范/断言，后补实现的风格。即使目标路由或处理器尚未实现，先将其行为和预期状态用代码固化下来，防止后续功能重构引发回归故障。
*/
// TestNotifyAPI 验证独立的 NotifyConfig GET/PUT API （RED阶段预期失败）
func TestNotifyAPI(t *testing.T) {
	// 1. 初始化内存数据库和依赖
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&model.Task{}, &model.NotifyConfig{})

	taskSvc := &service.TaskService{DB: db}
	handler := &TaskHandler{TaskSvc: taskSvc}

	// 创建路由
	// 🏗️ 【架构设计·模式对比】
	// 将 Mode 设置为 TestMode 能有效屏蔽掉多余的中间件日志输出（如 Debug 级别彩色列印），避免污染测试执行时控制台的结果输出，保证了测试环境下的高效与安静。
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 预期这两个新路由还不存在或抛错
	router.GET("/api/tasks/:id/notify", handler.GetTaskNotify)
	router.PUT("/api/tasks/:id/notify", handler.UpdateTaskNotify)

	// 先造一个任务
	task := model.Task{Name: "test-notify-task"}
	db.Create(&task)

	// 测试 GET：新任务应该返回 200，但 NotifyConfig 是空的（或者默认值）
	t.Run("GetNotifyConfig_NotExists", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/tasks/1/notify", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Expected 200 even if no config exists (should return empty config)")
	})

	// 测试 PUT：更新/创建通知配置
	t.Run("UpdateNotifyConfig", func(t *testing.T) {
		// 💀 【踩坑血泪·反面教材】
		// 在配置 WebhookURL 的更新接口时，很多后端新手往往直接将用户传入的 URL 存库。
		// 真实事故：某互联网大厂曾由于未对 Webhook URL 进行白名单或内网过滤，黑客构造了恶意负载：`http://169.254.169.254/latest/meta-data/`。
		// 当系统被动触发 Webhook 回调时，这行配置成功使得业务服务器向云厂商内网服务发起了请求，最终被截获 AWS EC2 实例的 IAM 临时高权限访问凭证。
		// 防御之道：虽然本测试尚未覆盖，但在生产系统的实际 Service 层实现中，必须解析 URL、提取 IP 并强校验是否归属于内网私有地址网段，做到 SSRF 彻底防御。
		cfg := model.NotifyConfig{
			WebhookURL: "https://example.com/webhook",
			OnFailure:  true,
			OnSuccess:  false,
		}
		body, _ := json.Marshal(cfg)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tasks/1/notify", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Expected 200 OK after updating notify config")

		// 验证数据库确实写入了
		var saved model.NotifyConfig
		err := db.Where("task_id = ?", 1).First(&saved).Error
		assert.NoError(t, err)
		assert.Equal(t, "https://example.com/webhook", saved.WebhookURL)
	})
}
