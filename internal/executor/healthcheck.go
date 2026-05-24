// ============================================================
// internal/executor/healthcheck.go - 健康检查执行器
//
// 功能：对指定URL发送GET请求，检查服务是否正常运行
// 任何非2xx的HTTP状态码都视为失败
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
