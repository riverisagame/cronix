/*
📌 【大厂面试·核心考点】
面试官常问：在Go中如何测试长连接（SSE/WebSocket）的并发推送和连接断开？如何防止长连接导致的Goroutine和内存泄漏？
标准答案：
1. 测试连接断开：在测试中可以使用 `httptest.ResponseRecorder` 并主动关闭其底层的 `CloseNotify` channel，或者使用 `context.WithCancel` 取消请求上下文，模拟客户端主动断开连接。服务端必须监听 `req.Context().Done()` 以安全退出推送循环，否则会导致 Goroutine 泄漏。
2. 并发推送测试：使用 `sync.WaitGroup` 配合多个 Goroutine 同时向多个连接发送数据，验证并发安全性（尤其是底层如果共享了类似于 Broadcast 广播器或 map 结构时，需要检查竞态条件，常使用 `go test -race` 发现）。
3. 内存泄漏测试：长连接特别容易产生内存泄漏。在测试结束时，通过调用 `runtime.NumGoroutine()` 判断是否有残留的 Goroutine；或者使用 `pprof` 对比连接前后的内存快照。另外可以借助第三方库诸如 `goleak.VerifyNone` 来进行 Goroutine 泄漏的自动化扫描。

🔬 【底层原理·深度剖析】
就像水管系统一样，长连接（SSE/WebSocket）相当于建立了一根不会自动关断的常开水管。
- SSE底层原理：基于 HTTP/1.1，服务端通过设置 `Content-Type: text/event-stream` 以及 `Connection: keep-alive`，告诉客户端不要关闭连接，然后持续向客户端 `Write` 数据流并 `Flush`。
- 测试复杂性：由于长连接请求不会像短连接那样 "请求-响应-结束" 同步返回，因此单元测试中必须使用非阻塞读取，或者引入超时控制 (`time.After`) 来防止测试永久挂起。

⚡ 【性能实战·生产调优】
- 生产环境中，SSE 广播器通常使用 `sync.Map` 或带读写锁的 `map` 维护上千个连接，时间复杂度通常在并发写入时表现为 O(1)。但在频繁建立/断开连接时，锁竞争会导致性能下降。
- 调优手段：对海量并发推送，可引入 RingBuffer 或无锁队列；使用连接池控制并发上限；开启 TCP Keep-Alive 探活，及时清理僵尸连接（半开连接）。

💀 【踩坑血泪·反面教材】
某一线大厂线上事故：某业务使用 SSE 推送股票行情，服务端未监听客户端断开事件（忽略了 `ctx.Done()`），客户端每刷新一次页面就新建一个 Goroutine 执行死循环推送。当天导致 OOM，节点崩溃。
- 正确做法：必须在 `select` 中同时监听业务 `channel` 和 `req.Context().Done()`。

🧪 【测试工程·质量保障】
零侵入测试策略（TDD-Red阶段）：在红灯阶段，我们首先构造期望的路由与请求体，由于实现尚未完成，接口必然返回 404 或特定错误（模拟物理零污染，不改动业务逻辑，纯通过 mock/httptest 校验接口契约）。后续在 Green 阶段，应引入 `testify/require` 对长连接的数据流做结构化反序列化测试。
*/
package handler

import (
	// 📌 【底层原理·深度剖析】 net/http：Go内置网络库，处理协议层封装。对于SSE长连接，它提供的 `http.Flusher` 接口是核心，用于将缓冲区内的数据强制推送到网络链路中。
	"net/http"
	// 🧪 【测试工程·质量保障】 httptest：Go自带的HTTP测试框架，允许我们在不启动真实监听端口(不走TCP层)的情况下，将HTTP请求直接发往对应的Handler，降低网络栈开销，时间复杂度趋近于0。
	"net/http/httptest"
	"testing"

	// 🏗️ 【架构设计·模式对比】 gin：高性能Web框架，底层基于 `httprouter`的基数树(Radix Tree)，路由匹配效率极高(O(K)，K为路径长度)。相比原生http.ServeMux具有更好的参数绑定和中间件生态。
	"github.com/gin-gonic/gin"
	// 🧪 【测试工程·质量保障】 testify/assert：提供表意更明确的断言风格，失败时会打印详细的 expected 和 actual 对比。
	"github.com/stretchr/testify/assert"
)

// 🧪 【测试工程·质量保障】 
// 测试任务终止接口的 Red（红灯）阶段。遵循 TDD（测试驱动开发）流程，在此阶段我们只写测试断言，验证在没有实现业务逻辑前，系统的表现是否符合预期（返回404 Not Found 或 501 Not Implemented）。
// 🛡️ 【安全攻防·漏洞防线】
// 面试官可能会问：如果是真实终止任务，如何防止越权？
// 答：在后续测试中，需要加入权限验证（如 JWT 验证请求头、或者校验 user_id 与 task 的 owner_id 是否匹配），此处 Red 阶段暂未包含鉴权上下文，但必须在设计时考虑。
func TestTaskHandler_KillTask_Red(t *testing.T) {
	// ⚡ 【性能实战·生产调优】 开启 gin.TestMode 可关闭多余的控制台日志输出，避免在海量用例并行测试时由于 I/O 阻塞导致整体测试性能退化。
	gin.SetMode(gin.TestMode)
	// 🏗️ 【架构设计·模式对比】 gin.New() 创建一个没有附加任何中间件（如 Logger 和 Recovery）的纯净路由树，适合做单元测试，隔离外部干扰。
	router := gin.New()
	
	// 这里目前还没实现 KillTask，故意让它跑不通以满足 Red 阶段
	// 我们用一个空的 TaskHandler
	// 💀 【踩坑血泪·反面教材】 这里的 TaskHandler 为空指针，如果后续业务层直接访问其中的依赖（如数据库连接、服务层接口），将引发 nil pointer panic。正确做法应在此处通过依赖注入（DI）传入 Mock 对象，保持依赖解耦和测试物理无损。
	th := &TaskHandler{}
	
	router.POST("/api/tasks/:id/kill", th.KillTask)
	
	// 📌 【大厂面试·核心考点】 面试官：如何避免并发测试时的端口冲突？
	// 答：使用 `httptest.NewRecorder()` 代替 `http.ListenAndServe()`。前者只是在内存中捕获响应（写入 `bytes.Buffer`），无需绑定本地真实 TCP 端口，支持高并发无冲突运行。
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/tasks/9999/kill", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// 🧪 【测试工程·质量保障】 
// 流式日志推送测试（TDD 阶段：红灯）。
// 🔬 【底层原理·深度剖析】
// 如果要完善对 SSE 长连接的测试，此处必须考虑“并发推送”与“连接中断”的场景：
// 1. 模拟断开：可以通过 `ctx, cancel := context.WithCancel(context.Background())` 配合 `req = req.WithContext(ctx)`，并在后续通过 `cancel()` 模拟客户端主动断网请求。
// 2. 内存泄漏扫描：测试完毕后需要结合第三方库保证内部日志消费 Goroutine 已随请求结束而正确退出，否则在长时间的压力测试下会出现大量挂起的协程。
func TestTaskHandler_StreamTaskLog_Red(t *testing.T) {
	// ⚡ 【性能实战·生产调优】 开启测试模式屏蔽无效输出，极大加快单元测试执行速度。
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	th := &TaskHandler{}
	router.GET("/api/tasks/:id/stream", th.StreamTaskLog)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/tasks/9999/stream", nil)
	// ⚡ 【性能实战·生产调优】 对于 SSE 长连接真实的 Green 阶段测试，由于请求可能持续挂起，会消耗大量测试调度器资源。因此，测试长连接必须通过带有 timeout 的 Context (`context.WithTimeout`) 或者 select `time.After` 防止单测卡死，保障 CI/CD 流水线顺畅。
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusNotFound, w.Code)
}
