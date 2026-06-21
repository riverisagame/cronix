// ============================================================
// internal/executor/http_exec.go - HTTP任务执行器（带认证和熔断保护）
//
// 📌 【大厂面试·核心考点总览】
// 1. 网络层：TCP三次握手、TLS握手全流程、DNS解析缓存与防抖机制
// 2. 并发层：连接池(Connection Pool)的复用原理、Idle超时机制、上下文(Context)全链路超时控制
// 3. 容错层：微服务熔断(Circuit Breaker)原理、HTTP状态码分类与重试策略、防抖退避(Backoff)机制
// 4. 安全层：OAuth2.0授权码流、防OOM的流式读取(LimitReader)、TIME_WAIT资源泄露防护
//
// 支持的认证方式：
//   - Basic Auth（用户名+密码）
//   - Bearer Token（令牌）
//   - API Key（密钥，可以放在请求头或URL参数里）
//   - OAuth2 Client Credentials（自动获取令牌）
// 包含熔断器保护：当外部服务持续出错时自动切断请求
// ============================================================
package executor

import (
    "bytes"           // 字节缓冲区：底层基于 []byte 切片，避免频繁的内存分配，用于构造请求体
    "context"         // 上下文：用于跨 Goroutine 传递超时、取消信号，防止 Goroutine 泄露
    "crypto/tls"      // TLS加密：实现 TLS/SSL 协议层，保障 HTTPS 数据传输的机密性和完整性
    "encoding/base64" // Base64编码：Basic Auth 规范要求的凭证编码方式（注意：它不是加密）
    "encoding/json"   // JSON编解码：反射解析结构体，生产环境高频调用可考虑 easyjson 优化
    "fmt"             // 格式化输出：字符串拼接，底层会有内存分配开销
    "io"              // 输入输出接口：Go 语言最核心的流式处理接口(Reader/Writer)，实现零拷贝或低内存流转
    "net/http"        // HTTP客户端：封装了 HTTP/1.1 和 HTTP/2 协议实现，内含复杂的 Transport 连接池设计
    "strings"         // 字符串处理：提供高效的字符串操作，底层有针对性的汇编优化
    "sync"            // 并发控制：提供 Mutex/RWMutex 等原语，用于保护并发环境下的全局 map 或共享状态
    "time"            // 时间处理：基于底层的 timer 堆，控制超时和令牌过期等时间敏感操作

    "cronix/internal/infrastructure/circuit" // 熔断器模块
)

// HTTPAuthConfig 存储HTTP任务的认证参数
// 每个字段后面的 `json:"xxx"` 是JSON序列化时的字段名
type HTTPAuthConfig struct {
    Username     string `json:"username,omitempty"`      // Basic Auth的用户名
    Password     string `json:"password,omitempty"`      // Basic Auth的密码
    Token        string `json:"token,omitempty"`         // Bearer令牌
    HeaderName   string `json:"header_name,omitempty"`   // API Key的请求头名称
    APIKey       string `json:"api_key,omitempty"`       // API Key的值
    KeyIn        string `json:"key_in,omitempty"`        // API Key放哪："header"或"query"
    TokenURL     string `json:"token_url,omitempty"`     // OAuth2获取令牌的URL
    ClientID     string `json:"client_id,omitempty"`     // OAuth2客户端ID
    ClientSecret string `json:"client_secret,omitempty"` // OAuth2客户端密钥
    Scopes       string `json:"scopes,omitempty"`        // OAuth2权限范围
}

// HTTPResult 存放HTTP任务的执行结果
type HTTPResult struct {
    StatusCode int    // HTTP状态码：200=成功，404=未找到，500=服务器错误
    Body       string // 响应体的内容（截断后的）
    Error      error  // 错误信息（nil表示没有问题）
}

// 全局变量：熔断器缓存和OAuth2令牌缓存
// 用map存储，key是URL，value是相应的对象
var (
    circuitBreakers = make(map[string]*circuit.CircuitBreaker) // URL → 熔断器
    cbMu            sync.Mutex                                  // 保护circuitBreakers的锁
    oauthCache      = make(map[string]*oauthToken)              // TokenURL → OAuth令牌
    oauthMu         sync.Mutex                                  // 保护oauthCache的锁
)

// oauthToken 缓存的OAuth2令牌及其过期时间
type oauthToken struct {
    Token     string    // 令牌字符串
    ExpiresAt time.Time // 过期时间点
}

// getCircuitBreaker 获取或创建一个URL对应的熔断器
// 🔬 【底层原理·深度剖析】
// 熔断器(Circuit Breaker)的设计灵感来自家里的电闸。当电器漏电（外部服务频繁超时、5xx报错）时，
// 电闸会自动跳闸（断开）。跳闸后，所有后续请求直接在本地阻断，不再发向外部。
// 过了冷却期（半开状态），会放一个试探性请求过去，若成功则闭合电闸恢复正常，若失败则继续保持断开。
// 这样能有效避免系统因为某一个下游接口的堵塞，导致大量线程/Goroutine堆积耗尽，发生全站雪崩效应。
// 参数 key：URL标识（用于区分不同的服务）
// 参数 threshold：连续失败多少次后断开
// 参数 cooldown：断开后冷却多少秒
func getCircuitBreaker(key string, threshold int, cooldown int) *circuit.CircuitBreaker {
    cbMu.Lock()                                                 // 加锁（map不是线程安全的）
    defer cbMu.Unlock()
    if cb, ok := circuitBreakers[key]; ok {                     // 如果已经有了
        return cb                                               // 直接返回
    }
    cb := circuit.NewCircuitBreaker(threshold, cooldown)        // 创建新的熔断器
    circuitBreakers[key] = cb                                   // 存入缓存
    return cb
}

// ExecuteHTTP 执行一个HTTP请求（支持多种认证方式和熔断保护）
// 参数说明：
//   ctx: 上下文
//   method: HTTP方法（GET/POST/PUT/DELETE等）
//   url: 请求地址
//   headers: JSON格式的自定义请求头
//   body: 请求体内容
//   authType: 认证类型（none/basic/bearer/api_key/oauth2）
//   authConfig: JSON格式的认证配置
//   timeoutSec: 超时秒数
//   cbThreshold: 熔断阈值
//   cbCooldown: 熔断冷却秒数
func ExecuteHTTP(ctx context.Context, method, url, headers, body, authType, authConfig string, timeoutSec, cbThreshold, cbCooldown int) *HTTPResult {
    // 第一步：检查熔断器是否允许请求通过
    cb := getCircuitBreaker(url, cbThreshold, cbCooldown)       // 获取这个URL的熔断器
    if !cb.Allow() {                                             // 熔断器拒绝了请求！
        return &HTTPResult{Error: fmt.Errorf("circuit breaker open for %s", url)}
    }

    // 第二步：创建带超时的上下文
    // 🔬 【底层原理·深度剖析】
    // 就像你去餐厅点餐，给服务员一个秒表（Context），时间一到不管菜做没做好都直接走人。
    // Context 树的超时控制是基于时间堆(timer)实现的。当超时发生时，底层的 timer 会向
    // ctx.Done() 通道发送信号。http.Client 底层会监听这个信号，一旦收到就会主动关闭底层的 TCP 连接。
    //
    // 🏗️ 【架构设计·模式对比】
    // 这里的 timeoutSec 是总超时（Total Timeout）。但在生产级高并发应用中，完整的超时层级设计应该包含：
    // 1. 寻址超时 (DNS Lookup Timeout)
    // 2. 连接超时 (Dial Timeout，通常2-3秒)
    // 3. 握手超时 (TLS Handshake Timeout)
    // 4. 响应头超时 (Response Header Timeout)
    // 5. 总超时 (Total Timeout，即此处的配置，包含读取完整 Body 的时间)
    // 如果仅依赖粗粒度的总超时，遇到极慢速网络连接时（连接建立了但数据发得很慢），单个请求可能长时间霸占资源引发雪崩。
    tCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
    defer cancel()

    // 第三步：准备请求体
    var reqBody io.Reader                                        // io.Reader是Go中"可以读取数据"的接口
    if body != "" {
        reqBody = bytes.NewBufferString(body)                    // 把字符串包装成可读取的数据流
    }

    // 第四步：创建HTTP请求对象
    // ⚡ 【性能实战·生产调优】
    // NewRequestWithContext 会将 tCtx 绑定到生成的 req 上。底层的 RoundTripper 
    // 会启动一个 background Goroutine 持续监听 ctx.Done()。一旦触发超时或被主动取消，
    // 底层会立刻强制 Close() TCP 链接，丢弃未发送完或未接收完的数据流。
    // 此操作的时间开销极其微小（O(1)），但它避免了因为目标服务端挂死而导致的本地 FD（文件描述符）和内存句柄长期泄露。
    req, err := http.NewRequestWithContext(tCtx, method, url, reqBody) // NewRequestWithContext会在上下文取消时自动终止请求
    if err != nil {                                              // 创建请求失败（比如URL格式不对）
        cb.RecordFailure()                                       // 记录失败（熔断器会记住）
        return &HTTPResult{Error: err}
    }

    // 第五步：解析并设置自定义请求头
    if headers != "" {
        var hdrs map[string]string
        if err := json.Unmarshal([]byte(headers), &hdrs); err == nil { // JSON字符串转map
            for k, v := range hdrs {
                req.Header.Set(k, v)                             // 设置每个请求头
            }
        }
    }

    // 第六步：应用认证信息
    if err := applyHTTPAuth(req, authType, authConfig); err != nil {
        cb.RecordFailure()                                       // 认证设置失败也记录
        return &HTTPResult{Error: fmt.Errorf("auth error: %w", err)}
    }

    // 第七步：创建HTTP客户端（带TLS配置）
    // 💀 【踩坑血泪·反面教材】
    // 警告！！！这里每次执行请求都动态 `new` 了一个全新的 `http.Client` 甚至全新的 `http.Transport`！
    // 真实生产事故：由于 Transport 管理着底层 TCP 连接池，每次新建 Transport 意味着旧连接会被直接抛弃而无法复用（失去 Keep-Alive 优势）。
    // 在高并发压测下，这会导致系统迅速创建成千上万条短连接，服务器上出现海量的 TIME_WAIT 状态 Socket，最终耗尽端口报出 "cannot assign requested address" 并宕机。
    //
    // 🔬 【底层原理·深度剖析】
    // 当缺乏连接池复用时，每次请求都要经历极重的初始化：
    // 1. DNS 解析（若系统无 DNS 缓存，每次都会发起 UDP 查询，耗时几十 ms）
    // 2. TCP 三次握手（SYN -> SYN-ACK -> ACK，至少 1 个 RTT 开销）
    // 3. TLS 四次握手全流程（HTTPS专属开销）：
    //    a) ClientHello（发送支持的 Cipher Suites、TLS版本和随机数）
    //    b) ServerHello（确认套件、发回证书和随机数）
    //    c) Certificate & Key Exchange（验证证书链，使用非对称加密交换 Pre-Master Secret）
    //    d) Finished（转为轻量级对称加密进行后续通信）
    // 上述建立连接的全套耗时往往在 100ms-300ms 之间，远超发送数据的实际时间。
    //
    // 📌 【大厂面试·核心考点】
    // 面试官：如何正确优化 Go 的 HTTP 客户端以支持高并发？
    // 标准答案：将 http.Client 及核心的 http.Transport 提升为包级别的全局单例，然后在 Transport 中精心调优：
    //    - MaxIdleConns（最大全局空闲连接数，例如 100，默认 100）
    //    - MaxIdleConnsPerHost（单域名最大空闲连接，默认值只有 2，极易成为瓶颈，必须调大到 50-100）
    //    - IdleConnTimeout（空闲保持超时时间，如 90s，防止被服务端主动 RST 掉）
    client := &http.Client{
        Timeout: time.Duration(timeoutSec) * time.Second,        // 整个请求的总超时时间（包含拨号、握手、响应传输）
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: false}, // InsecureSkipVerify=false：强制校验服务器发来的 TLS 证书链
        },
    }

    // 第八步：执行请求
    resp, err := client.Do(req)                                  // Do发送请求并等待响应
    if err != nil {                                              // 请求失败了（网络不通等）
        cb.RecordFailure()
        return &HTTPResult{Error: err}
    }
    defer resp.Body.Close()                                      // 函数结束时关闭响应体

    // 第九步：读取响应体（最多读64KB，防止响应太大撑爆内存）
    // 🛡️ 【安全攻防·漏洞防线】
    // 试想用吸管喝饮料，如果坏人拿高压水泵（几个GB的超大恶意包）怼着吸管另一头，会直接把你撑爆（OOM）。
    // `io.LimitReader` 就像在吸管上安装了一个流量计安全阀，无论上游涌来多少数据，它只允许精确的 64KB 流过，到达上限直接 EOF。
    // 如果没有这一层防御，直接盲目 `io.ReadAll(resp.Body)`，Go 底层会以 2倍容量 不断扩容申请 `[]byte` 内存。
    // 当遇到恶意服务端或者死循环的数据流时，服务进程会急剧膨胀，最终触发操作系统的 OOM Killer 将其强制猎杀。
    // 性能数据：LimitReader 是对原始流行为的零拷贝代理包装，时间和空间复杂度均为 O(1)，开销几近于无。
    respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024)) // LimitReader限制最多读取64KB

    // 第十步：根据HTTP状态码更新熔断器状态
    // 🏗️ 【架构设计·模式对比】
    // 状态码分类、重试与退避（Backoff）策略：
    // 在完善的工业级执行器中，针对状态码要有不同的应对预案：
    // 1. 【不可重试区】：400(Bad Request参数错误)、401(未认证)、403(禁止访问)、404(不存在)。这些属于客户端引发的致命错误，重试1万次结果也一样，必须立刻 Fail-fast 终止。
    // 2. 【可重试区】：500(内部崩溃)、502(网关错误)、503(服务不可用)、504(网关超时)。代表服务端临时故障或抖动，值得重试。
    // 3. 【防重复机制(幂等性)】：重试时必须关注请求方法。GET/PUT/DELETE 天然具有幂等性（重复执行结果相同），而 POST 请求如果不携带业务幂等 Token，发生网络超时后盲目重试，极易造成订单被重复创建、资金被重复扣除。
    // 4. 【退避策略(Backoff)】：重试绝不能马上进行。简单的 线性退避 (固定等1秒) 容易引起资源拥挤；生产最佳实践是 Exponential Backoff + Full Jitter（指数退避 + 随机抖动），打散重试流量，彻底防止服务雪崩。
    if resp.StatusCode >= 500 {                                  // 5xx是服务器内部错误，表明依赖方存在不稳定
        cb.RecordFailure()                                       // 记录失败（累计突破阈值则触发断路，快速失败保护自身）
    } else {
        cb.RecordSuccess()                                       // 记录成功
    }

    // 第十一步：返回结果
    return &HTTPResult{
        StatusCode: resp.StatusCode,
        Body:       string(respBody),
    }
}

// applyHTTPAuth 根据认证类型给HTTP请求添加认证信息
// 参数 req：HTTP请求对象（会被修改）
// 参数 authType：认证类型
// 参数 authConfig：JSON格式的认证配置
func applyHTTPAuth(req *http.Request, authType, authConfig string) error {
    if authType == "none" || authConfig == "" {                  // 不需要认证
        return nil
    }

    // 解析认证配置JSON
    var cfg HTTPAuthConfig
    if err := json.Unmarshal([]byte(authConfig), &cfg); err != nil { // JSON字符串转结构体
        return err
    }

    // 根据不同类型设置认证头
    switch authType {
    case "basic":
        // Basic Auth：把 "用户名:密码" 用Base64编码，放在Authorization头里
        auth := base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password)) // Base64编码
        req.Header.Set("Authorization", "Basic "+auth)          // 设置请求头

    case "bearer":
        // Bearer Token：直接把令牌放在Authorization头里
        req.Header.Set("Authorization", "Bearer "+cfg.Token)

    case "api_key":
        // API Key：可以放在请求头里，也可以放在URL参数里
        if cfg.KeyIn == "query" {                               // 放在URL参数里（如 ?api_key=xxx）
            q := req.URL.Query()                                 // 获取当前URL的查询参数
            q.Add(cfg.HeaderName, cfg.APIKey)                    // 添加参数
            req.URL.RawQuery = q.Encode()                        // 更新URL的查询字符串
        } else {                                                 // 默认放在请求头里
            req.Header.Set(cfg.HeaderName, cfg.APIKey)
        }

    case "oauth2":
        // OAuth2 Client Credentials：自动从授权服务器获取令牌
        token, err := getOAuthToken(cfg)
        if err != nil {
            return err
        }
        req.Header.Set("Authorization", "Bearer "+token)
    }

    return nil
}

// getOAuthToken 通过OAuth2 Client Credentials流程获取令牌（带缓存）
// Client Credentials = 用客户端ID和密钥直接换取令牌，适合服务间调用
// 令牌会在过期前60秒刷新
func getOAuthToken(cfg HTTPAuthConfig) (string, error) {
    // 第一步：检查缓存中是否有可用的令牌
    oauthMu.Lock()
    if cached, ok := oauthCache[cfg.TokenURL]; ok && time.Now().Add(60*time.Second).Before(cached.ExpiresAt) {
        oauthMu.Unlock()                                         // 距离过期还有60秒以上，可以复用
        return cached.Token, nil
    }
    oauthMu.Unlock()

    // 第二步：构造获取令牌的请求体
    // grant_type=client_credentials 是OAuth2标准的一种授权方式
    data := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s",
        cfg.ClientID, cfg.ClientSecret)
    if cfg.Scopes != "" {
        data += "&scope=" + cfg.Scopes                           // 如果有权限范围要求，加上
    }

    // 第三步：发送令牌请求
    req, _ := http.NewRequest("POST", cfg.TokenURL, strings.NewReader(data)) // 创建POST请求
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")       // 表单格式
    // POST请求体里的Content-Type必须是x-www-form-urlencoded（HTML表单的标准格式）

    // 💀 【踩坑血泪·反面教材】
    // 同样的问题，这里再次新建了一个 http.Client，未能复用底层连接池资源！
    // 频发调用授权接口时，会造成与 OAuth 认证中心建立大量的短期并发连接，拖累整体吞吐量。
    client := &http.Client{Timeout: 30 * time.Second}            // 30秒超时（Total Timeout）
    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    // 第四步：解析响应中的令牌
    var result struct {
        AccessToken string `json:"access_token"`                 // 令牌字符串
        ExpiresIn   int    `json:"expires_in"`                   // 多少秒后过期
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil { // 从响应体解码JSON
        return "", err
    }

    // 第五步：把令牌存入缓存
    oauthMu.Lock()
    oauthCache[cfg.TokenURL] = &oauthToken{
        Token:     result.AccessToken,
        ExpiresAt: time.Now().Add(time.Duration(result.ExpiresIn) * time.Second), // 计算过期时间
    }
    oauthMu.Unlock()

    return result.AccessToken, nil
}
