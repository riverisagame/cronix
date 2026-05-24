// ============================================================
// internal/executor/http_exec.go - HTTP任务执行器（带认证和熔断保护）
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
    "bytes"           // 字节缓冲区：构造请求体
    "context"         // 上下文：超时控制
    "crypto/tls"      // TLS加密：HTTPS安全连接
    "encoding/base64" // Base64编码：Basic Auth需要把用户名密码编码
    "encoding/json"   // JSON编解码：解析请求头、认证配置
    "fmt"             // 格式化输出
    "io"              // 输入输出接口：读取响应体
    "net/http"        // HTTP客户端：发送请求
    "strings"         // 字符串处理
    "sync"            // 并发控制：保护全局缓存
    "time"            // 时间处理：超时和令牌过期

    "cronix/internal/circuit" // 熔断器模块
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
    tCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
    defer cancel()

    // 第三步：准备请求体
    var reqBody io.Reader                                        // io.Reader是Go中"可以读取数据"的接口
    if body != "" {
        reqBody = bytes.NewBufferString(body)                    // 把字符串包装成可读取的数据流
    }

    // 第四步：创建HTTP请求对象
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
    client := &http.Client{
        Timeout: time.Duration(timeoutSec) * time.Second,        // 整个请求的超时时间
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: false}, // InsecureSkipVerify=false：验证HTTPS证书
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
    respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024)) // LimitReader限制最多读取64KB

    // 第十步：根据HTTP状态码更新熔断器状态
    if resp.StatusCode >= 500 {                                  // 5xx是服务器内部错误
        cb.RecordFailure()                                       // 记录失败
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

    client := &http.Client{Timeout: 30 * time.Second}            // 30秒超时
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
