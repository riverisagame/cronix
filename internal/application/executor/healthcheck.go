// ============================================================
// internal/executor/healthcheck.go - 健康检查执行器
//
// 功能：对指定URL发送GET请求，检查服务是否正常运行
// 任何非2xx的HTTP状态码都视为失败
//
// 📌 【大厂面试·核心考点】
// 面试官问：K8s的三种探针（Liveness/Readiness/Startup）有什么本质区别？你在生产中怎么配置？
// 标准答案：
// 1. Liveness (存活探针)：探测应用是否陷入死锁或线程耗尽，一旦探测连续失败，K8s 会无情地杀掉并重启 Pod。
// 2. Readiness (就绪探针)：探测应用是否准备好接收外部流量，一旦失败，K8s 会将该 Pod 从 Service 的 
//    Endpoint 路由列表中摘除，不再转发新请求，但绝不会重启 Pod。
// 3. Startup (启动探针)：保护慢启动容器（比如需要预加载数十GB缓存的Java/AI应用），在它成功之前，
//    前两种探针会被强制挂起屏蔽，防止应用还没启动完就被反复误杀。
//
// 🏗️ 【架构设计·模式对比】
// 健康检查的设计模式：浅检 (Shallow Check) vs 深检 (Deep Check)
// - 浅检（通常对应 /ping 或 /healthz）：只检查 HTTP 服务器主事件循环是否活着，绝不碰数据库、不查Redis，
//   速度极快（微秒级响应）。主要用于 Liveness 探针，防止因为数据库偶尔变慢导致应用自身被无故重启。
// - 深检（通常对应 /ready）：不仅自己活着，还要探测关键依赖（尝试 ping 数据库、检查 Redis 连接池、
//   甚至执行一次简单的 SELECT 1）。主要用于 Readiness 探针。 
// 生活比喻：浅检就像护士问你“还活着吗？”，你哼了一声；深检就像问你“准备好跑马拉松了吗？”，你需要全面检查心肺、鞋带、水壶。
// ============================================================
package executor

import (
    "context"      // 上下文：支持超时和取消
    "fmt"          // 格式化：生成错误信息
    "net/http"     // HTTP客户端
    "time"         // 时间：超时设置
)

// HealthCheckResult 存放健康检查的结果
type HealthCheckResult struct {
    StatusCode int   // HTTP状态码（200表示一切正常）
    Error      error // 错误信息（非2xx状态码或网络错误）
}

// ExecuteHealthCheck 对指定URL执行健康检查
// 参数 ctx：上下文（用于超时控制）
// 参数 url：要检查的目标网址
// 参数 timeoutSec：超时秒数（超过则视为失败）
// 返回值：HealthCheckResult指针
func ExecuteHealthCheck(ctx context.Context, url string, timeoutSec int) *HealthCheckResult {
    // 第一步：创建带超时的HTTP客户端
    // 🔬 【底层原理·深度剖析】
    // TCP/HTTP 检查的底层差异：
    // 生活比喻：TCP检查是敲一下你家大门，只要有人过来开门（建立连接）就算过了；
    // 而 HTTP 检查是进门后，还要问“暗号是啥”，你回答“天王盖地虎”（返回 200 OK 完整报文）才算过。
    // - TCP 检查：仅发起 TCP 三次握手。这完全在操作系统内核协议栈层面完成。如果应用层的 Goroutine
    //   池已经全部死锁或者阻塞了，内核依然能自动完成握手，导致 TCP 检查显示"健康"，但实际业务已瘫痪！
    // - HTTP 检查：建立 TCP 连接后，还要发送 HTTP Request 报文，并强制等待应用层代码去读取解析、
    //   处理并返回 HTTP Response。它能真实反映应用程序（应用层协议）的健康状态，防假死能力远超 TCP。
    // 
    // ⚡ 【性能实战·生产调优】
    // 踩坑预警：在 Go 中创建 http.Client 时【绝对不能】使用默认的无超时客户端（http.DefaultClient）。
    // 真实事故：如果对端服务器建立了连接但突然挂起（不返回任何数据），默认 Client 会在这个 socket
    // 上永久死等，导致发起检查的 Goroutine 永远泄露，最终因 OOM 引发血崩。显式设置 Timeout 是底层铁律。
    // 
    // 超时 = 连接时间 + 等待响应时间 + 读取响应体时间的总上限
    client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}

    // 第二步：创建GET请求
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil) // nil表示没有请求体
    if err != nil {                                              // URL格式错误等
        return &HealthCheckResult{Error: err}
    }

    // 第三步：发送请求
    resp, err := client.Do(req)                                  // Do发送请求并等待响应
    if err != nil {                                              // 网络错误、超时等
        return &HealthCheckResult{Error: err}
    }
    defer resp.Body.Close()                                      // 函数结束时关闭响应体（释放连接）

    // 第四步：构造结果
    result := &HealthCheckResult{StatusCode: resp.StatusCode}

    // 第五步：判断HTTP状态码是否在成功范围内（200-299之间）
    // HTTP状态码约定：
    //   2xx = 成功（200 OK, 201 Created...）
    //   3xx = 重定向
    //   4xx = 客户端错误（404 Not Found, 401 Unauthorized...）
    //   5xx = 服务器错误（500 Internal Server Error...）
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {         // 不是2xx就视为不健康
        result.Error = fmt.Errorf("health check failed: status %d", resp.StatusCode)
    }

    return result
}
