// ============================================================
// internal/notify/notifier.go - 通知发送器（Webhook和Email）
//
// 任务执行完成后（成功或失败），可以通过Webhook或Email通知相关人员。
// 当前版本Email是占位符（stub），需要配置SMTP服务器才能实际使用。
// Webhook功能完整可用。
// ============================================================
package notify

import (
    "bytes"           // 字节缓冲区：构造通知请求体
    "context"         // 上下文：控制通知发送器的生命周期
    "encoding/json"   // JSON编解码：序列化通知数据
    "fmt"             // 格式化：错误信息
    "net/http"        // HTTP客户端：发送Webhook
    "time"            // 时间处理：记录时间戳、重试间隔

    "cronix/internal/model"   // 数据模型：通知配置

    "github.com/rs/zerolog/log" // 日志库
)

// NotifyEvent 表示一条待发送的通知事件
type NotifyEvent struct {
    TaskName string              // 任务名称
    Status   string              // 执行状态（"success" 或 "failed"）
    Config   model.NotifyConfig  // 通知配置（Webhook地址/邮箱地址等）
}

// Notifier 通知发送器，在后台异步处理通知
type Notifier struct {
    notifyCh chan NotifyEvent   // 通知事件通道：其他地方把事件发到这里
    retry    int                // 发送失败时的重试次数
    interval time.Duration      // 重试间隔时长
}

// New 创建一个新的通知发送器
// 参数 retry：发送失败时最多重试几次
// 参数 interval：每次重试之间等多久
func New(retry int, interval time.Duration) *Notifier {
    return &Notifier{
        notifyCh: make(chan NotifyEvent, 256),                   // 缓冲区256，可以暂存256个待发送的通知
        retry:    retry,                                          // 重试次数
        interval: interval,                                       // 重试间隔
    }
}

// NotifyChan 返回通知通道的"只写"版本
// 其他模块通过这个通道发送通知事件，不用关心内部实现
func (n *Notifier) NotifyChan() chan<- NotifyEvent {
    return n.notifyCh
}

// Start 启动通知发送器的主循环
// 参数 ctx：上下文，当ctx取消时，通知发送器会停止
func (n *Notifier) Start(ctx context.Context) {
    for {                                                        // 无限循环，不断从通道读取事件
        select {
        case <-ctx.Done():                                       // 上下文被取消（程序要关闭了）
            return                                               // 停止循环
        case event := <-n.notifyCh:                              // 收到一个通知事件
            n.send(event)                                        // 发送它
        }
    }
}

// send 根据通知类型分发到具体的发送方法
func (n *Notifier) send(event NotifyEvent) {
    switch event.Config.NotifyType {                             // 根据通知方式走不同分支
    case "webhook":
        n.sendWebhook(event)                                     // 发送Webhook通知
    case "email":
        n.sendEmail(event)                                       // 发送邮件通知（当前是占位符）
    }
}

// sendWebhook 发送Webhook通知（带重试机制）
// Webhook = 向一个URL发送HTTP POST请求，把通知内容发给外部系统
func (n *Notifier) sendWebhook(event NotifyEvent) {
    // 第一步：构造通知内容（JSON格式）
    payload := map[string]interface{}{                            // 通知的JSON数据
        "task":      event.TaskName,                              // 哪个任务
        "status":    event.Status,                                // 执行成功了还是失败了
        "timestamp": time.Now().Format(time.RFC3339),              // 通知发送时间（RFC3339是标准时间格式）
    }
    body, _ := json.Marshal(payload)                              // 把map序列化为JSON字节

    // 第二步：发送HTTP POST请求（带重试）
    var lastErr error
    for i := 0; i <= n.retry; i++ {                              // 最多尝试 retry+1 次
        resp, err := http.Post(event.Config.WebhookURL, "application/json", bytes.NewReader(body)) // 发POST请求
        if err == nil && resp.StatusCode < 400 {                  // 请求成功且HTTP状态码正常（<400）
            resp.Body.Close()                                     // 关闭响应体
            return                                                // 发送成功，不用重试
        }
        if err != nil {                                           // 网络错误
            lastErr = err
        } else {                                                  // HTTP状态码不正常
            resp.Body.Close()
            lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
        }
        if i < n.retry {                                          // 还有重试机会
            time.Sleep(n.interval)                                // 等待重试间隔后再试
        }
    }
    // 第三步：所有重试都失败了，记录一条警告日志
    log.Warn().Err(lastErr).Str("task", event.TaskName).Msg("webhook notification failed after retries")
}

// sendEmail 发送邮件通知（占位符，需要配置SMTP才能使用）
// SMTP = 简单邮件传输协议，用来发邮件的标准协议
func (n *Notifier) sendEmail(event NotifyEvent) {
    log.Warn().Str("to", event.Config.EmailTo).Str("task", event.TaskName).
        Msg("email notification stub - configure SMTP for production use")
    // 这里只是打印了一条警告，实际发邮件需要配置SMTP服务器信息
}
