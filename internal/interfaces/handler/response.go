package handler

import (
	// 🔬 【底层原理·深度剖析】HTTP Content-Type Negotiation
	// ──────────────────────────────────────────────────
	// 当我们在 API 开发中返回 HTTP 响应时，客户端和服务器之间需要协商数据格式（通常是 JSON、XML 或 Protobuf）。
	// Go 的 net/http 包提供了基础的 HTTP 原语，而框架（如 Gin）则在此基础上封装了内容协商（Content Negotiation）能力。
	// 在 RESTful 规范中，客户端通过请求头 `Accept: application/json` 告诉服务器期望的格式，
	// 服务器则通过响应头 `Content-Type: application/json; charset=utf-8` 明确返回的数据格式。
	// 如果不显式设置，Go 的 http.DetectContentType 会尝试嗅探（Sniffing）前 512 字节的数据包，但这会导致额外的性能开销。
	// 
	// 📌 【大厂面试·核心考点】
	// Q: HTTP 状态码 200 和 业务状态码 code=0 有什么区别？
	// A: HTTP 状态码（如 200, 404, 500）是属于网络传输层面的状态，表示 HTTP 请求本身是否成功到达并被服务器处理。
	//    业务状态码（code）是应用层面的状态，表示具体的业务逻辑是否成功（例如：账号密码错误、余额不足）。
	//    大厂规范（如 Google API Design Guide）建议：尽量利用 HTTP 标准状态码表示通用错误（如 401 鉴权失败，403 权限不足），
	//    而在 Response Body 中携带细粒度的业务错误码和可读的 message。
	"net/http"

	// ⚡ 【性能实战·生产调优】JSON 序列化性能
	// ──────────────────────────────────────
	// Gin 框架默认使用的是 encoding/json（Go 标准库），其底层基于反射（Reflection）实现，在超高并发下会成为性能瓶颈。
	// 生产调优建议：
	// 1. 如果对性能要求极高，可以在编译 Gin 时使用 tags 替换 JSON 库，例如 `go build -tags=sonic` 
	//    （字节跳动开源的 sonic 库基于 JIT 技术，比标准库快 2-3 倍，并且大幅降低 CPU 和内存开销）。
	// 2. 避免在 Response 结构体中使用 interface{} 或 any，因为反射解析动态类型的开销更大，明确具体类型更有利于 JSON 序列化。
	// 3. 在极高频访问的网关层，可以提前序列化固定的错误响应结构并缓存为 []byte，使用 c.Data() 直接写回，实现零反射开销。
	"github.com/gin-gonic/gin"
)

// ┌─────────────────────────────────────────────────────────────────────────────┐
// │              🏗️ 【架构设计·模式对比】统一响应模型的设计思想                 │
// ├─────────────────────────────────────────────────────────────────────────────┤
// │ 统一响应模型是后端工程化最基础的标准，它决定了前端/客户端解析数据的难易程度。   │
// │                                                                           │
// │ 模式 A（松散模式 - 反面教材）：                                             │
// │ 成功返回：{"user_id": 1, "name": "Tom"}                                   │
// │ 失败返回：{"error": "user not found"}                                     │
// │ 缺点：前端每次解析都要去判断有没有 error 字段，甚至不同接口返回的错误字段名字    │
// │ 都不一样（有叫 err，有叫 msg），导致客户端崩溃率直线上升。                      │
// │                                                                           │
// │ 模式 B（统一信封模式 Envelope - 本项目采用）：                              │
// │ 统一格式：{"code": 0, "message": "ok", "data": {...}}                     │
// │ 优点：前端可以设置统一的 Axios Interceptor（拦截器）。只要 code != 0，统一拦截 │
// │ 抛出全局 Toast 提示，只有 code == 0 才将 data 透传给具体的业务组件。          │
// │                                                                           │
// │ 🛡️ 【安全攻防·漏洞防线】                                                   │
// │ ──────────────────────────                                                │
// │ 统一错误模型在处理内部异常（如数据库 Panic 或 SQL 执行错误）时，切忌将原始 Error  │
// │ 堆栈或 SQL 语句直接拼接到 `message` 中返回给前端！这会导致严重的**敏感信息泄露** │
// │ （例如泄露表结构、中间件版本、服务器路径）。                                     │
// │ 正确做法：系统级别错误（500）统一下发 "Internal Server Error" 或 "系统繁忙"， │
// │ 真实的 Error details 只能记录在服务端的日志系统中，并关联一个 TraceID 返回给      │
// │ 客户端用于快速排查定位。                                                      │
// └─────────────────────────────────────────────────────────────────────────────┘

// respondOK 返回成功响应（code=0）
//
// 🧪 【测试工程·质量保障】
// ──────────────────────────
// 测试此函数时，需要通过 httptest.NewRecorder() 捕获响应。
// 必须断言三点：
// 1. HTTP 状态码是否为 200 OK
// 2. Content-Type 是否为 application/json; charset=utf-8
// 3. 响应体的 JSON 结构解析后，code 必须为 0，data 必须全等匹配
func respondOK(c *gin.Context, data any) {
	// ⚡ 【性能实战·生产调优】
	// gin.H 底层是 map[string]any，每次调用都会在堆上分配。
	// 在高并发核心链路上，建议统一定义结构体 Response { Code int, Message string, Data any }，
	// 结构体可以分配在栈上（或复用 sync.Pool），从而避免因为 map 分配引发的 GC 压力。
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": data})
}

// respondOKMsg 返回成功响应（无 data，仅 message）
// 
// 📌 【大厂面试·核心考点】
// ──────────────────────────
// Q: 如果只是返回成功，为什么不用 HTTP 204 No Content？
// A: 204 No Content 是 RESTful 规范中非常纯粹的做法，表示请求处理成功但无返回体。
//    然而在真实业务中，国内主流做法是依然返回 HTTP 200 结合 code=0 的外壳包裹。
//    这样做有利于前端网络库统一解析和拦截（Axios Interceptor 一致性处理），
//    无需为了适配 204 的空响应体去写额外的判断逻辑。
func respondOKMsg(c *gin.Context, msg string) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": msg})
}

// respondError 返回错误响应（非 0 code，HTTP 状态码 = code）
// 
// 💀 【踩坑血泪·反面教材】
// ──────────────────────────
// 这里的实现存在一个设计争议：将业务的 code 直接等同于 HTTP 的 status code（即 c.JSON(code, ...)）。
// 这会导致一个严重问题：HTTP 标准状态码范围是 100-599，如果业务需要定义非常细分的错误码
// （例如 10001 表示 "用户密码过期"，40002 表示 "余额不足"），
// 此时如果传入 code=10001，c.JSON(10001, ...) 将引发 net/http 底层抛出异常或返回混乱头信息，
// 因为 10001 并非合法的 HTTP 状态码！
//
// 🏗️ 【架构设计·Microsoft/Google 错误码规范】
// ───────────────────────────────────────────
// 现代 API 设计规范（如 Google API Design Guide）建议：
//   1. 外层：复用有限的 HTTP 标准状态码（如 400 Bad Request, 401 Unauthorized, 404 Not Found, 500 Internal Error）作为 HTTP 状态传输。
//   2. 内层：在结构体内部提供特定业务错误定义，如：
//      {
//        "error": {
//           "code": 40001,
//           "message": "Invalid API key",
//           "status": "UNAUTHENTICATED",
//           "details": [ ... ]
//        }
//      }
// 💡 重构建议：将 respondError 的入参分离为 httpStatus int 和 bizCode int，不再将其混用。
func respondError(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"code": code, "message": msg})
}
