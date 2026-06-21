// ============================================================
// internal/notify/notifier.go - 通知发送器（Webhook和Email）
//
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：为什么要用 Go 的 channel（通道）做异步通知？为什么不直接调用发邮件的函数？
// 答：
// 如果直接调用（同步）：就像你去寄快递，必须站在柜台看着快递员打包、装车、送货，送到别人手里你才能走。
// 这样不仅你干不了别的事，如果快递地址写错了（网络超时），你还得一直干等。
//
// 使用 channel（异步）：就像你把快递扔进楼下的顺丰快递柜（Channel）。
// 扔进去你就可以回去睡大觉了。快递员（Notifier 协程）会定期来开柜子，把包裹拿走慢慢发。
// 这叫【解耦】和【异步削峰】。就算外面网络瘫了，发通知的速度极慢，你的主程序依然健步如飞。
//
// 2. 面试官问：你这里用了 `ants` 协程池，有什么好处？
// 答：
// Go 语言虽然开协程（Goroutine）很便宜，但如果瞬间来了 100 万个通知，
// 瞬间开 100 万个协程去发 HTTP 请求，会把系统的网络连接数（FD）和内存全部榨干，导致 OOM 死机。
// `ants` 协程池就像是一个“大厂固定的外卖骑手团队”（比如只有 50 个人）。
// 不管有多少单子进来，最多只有 50 个人在送货，送完一单再去送下一单，绝对不会让服务器过载崩溃。
// ============================================================
package notify

import (
    "bytes"           // 字节缓冲区：构造通知请求体
    "context"         // 上下文：控制通知发送器的生命周期
    "encoding/json"   // JSON编解码：序列化通知数据
    "fmt"             // 格式化：错误信息
    "net/http"        // HTTP客户端：发送Webhook
    "time"            // 时间处理：记录时间戳、重试间隔

    "cronix/internal/domain/model"   // 数据模型：通知配置

    	"github.com/panjf2000/ants/v2"
	"github.com/rs/zerolog/log" // 日志库
)

// NotifyEvent 表示一条待发送的通知事件
type NotifyEvent struct {
    TaskName string              // 任务名称
    Status   string              // 执行状态（"success" 或 "failed"）
    Config   model.NotifyConfig  // 通知配置（Webhook地址/邮箱地址等）
}

// Notifier 通知发送器，在后台异步处理通知
// 
// 🏗️ 【架构设计·模式对比】基于 Channel 的 Actor 模式思想
// 这里将通道 (notifyCh) 作为结构体内部状态，并且唯一通过一个统一的主循环 (Start) 去消费它。
// 这暗合了并发模型中的 Actor 模型理念：通过消息传递来共享内存，而不是通过共享内存来通信。
// 完全消除了互斥锁（Mutex）的使用，极大提升了高并发下的无锁运行效率。
type Notifier struct {
    notifyCh chan NotifyEvent   // 通知事件通道：其他地方把事件发到这里
    retry    int                // 发送失败时的重试次数
    interval time.Duration      // 重试间隔时长
    pool     *ants.Pool         // 协程池，防止高并发时阻塞或者导致 OOM
}

// New 创建一个新的通知发送器
// 参数 retry：发送失败时最多重试几次
// 参数 interval：每次重试之间等多久
//
// 🏗️ 【架构设计·模式对比】工厂模式与极致解耦
// 这里使用了简单工厂模式（Simple Factory Pattern）创建 Notifier 实例。
// 面试官会问：为什么要用 New 函数返回结构体指针，而不让调用者直接初始化 Notifier{}？
// 标准答案：
// 1. 封装复杂性：调用者不需要知道内部需要初始化 ants.Pool 和 channel，只需传入核心业务参数（重试与间隔）。这种面向接口/抽象编程实现了高度解耦。
// 2. 强制校验与默认值：在 New 内部统一接管并约束底层资源，比如协程池大小（50）和通道缓冲（256），防止外部乱传参数导致系统崩溃。
// 3. 内存逃逸优化：返回指针可以避免结构体在传参时发生值拷贝，Go 编译器会通过逃逸分析将其分配在堆区（Heap）。
//
// ⚡ 【性能实战·生产调优】通道缓冲与协程池容量的深度权衡
// - channel 缓冲区设为 256：当突然产生大量任务通知（脉冲流量）时，256 个缓冲位是“第一道大坝”，起到【异步削峰】作用，避免主业务逻辑发通知被阻塞。
// - ants 协程池设为 50：这是“第二道大坝”。即使遇到目标 Webhook 接口严重超时（比如卡死 10 秒），最多只有 50 个工作协程被挂起，绝对不会无脑创建成千上万个 Goroutine 吃光服务器内存（防止 OOM）。
func New(retry int, interval time.Duration) *Notifier {
    pool, _ := ants.NewPool(50) // 容量50，最大并发发50个通知
    return &Notifier{
        notifyCh: make(chan NotifyEvent, 256),                   // 缓冲区256，可以暂存256个待发送的通知
        retry:    retry,                                          // 重试次数
        interval: interval,                                       // 重试间隔
        pool:     pool,                                           // 绑定协程池
    }
}

// NotifyChan 返回通知通道的"只写"版本
// 其他模块通过这个通道发送通知事件，不用关心内部实现
func (n *Notifier) NotifyChan() chan<- NotifyEvent {
    return n.notifyCh
}

// Start 启动通知发送器的主循环
// 参数 ctx：上下文，当ctx取消时，通知发送器会停止
//
// 🔬 【底层原理·深度剖析】异步网络请求的 Goroutine 泄漏预防
// 面试官极度爱考的并发题：如何优雅地关闭一个后台运转的 Goroutine？如果不关会怎样？
// 标准答案：
// 必须使用 context.Context 搭配 select 多路复用。如果这个 Start 的 for 循环只监听 n.notifyCh，
// 当主程序试图平稳重启、或者取消该通知模块时，这个 for 循环将永远阻塞在等待通道输入上，无法退出。这就构成了 Goroutine 泄漏！
// 长时间运行后，泄漏的僵尸协程会不断堆积，导致 CPU 调度压力激增和内存彻底溢出（OOM）。
// 
// 🛡️ 【安全攻防·漏洞防线】优雅退出的“擦屁股”工程
// `n.pool.Release()` 这一句极其关键！在监听到 ctx.Done()（即收到下线指令）时，不仅主协程要 return，
// 还必须显式释放底层的 ants 协程池。否则池子内部维护的数十个 worker 协程依然会成为僵尸驻留在内存中。
// 这就是资深工程师和新手的本质区别——有始有终，严格管理内存的生命周期。
func (n *Notifier) Start(ctx context.Context) {
    for {                                                        // 无限循环，不断从通道读取事件
        select {
        case <-ctx.Done():                                       // 上下文被取消（程序要关闭了）
            n.pool.Release()                                     // 释放协程池
            return                                               // 停止循环
        case event := <-n.notifyCh:                              // 收到一个通知事件
            n.pool.Submit(func() { n.send(event) })              // 放入协程池异步发送
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
//
// 📌 【大厂面试·核心考点】高可用重试机制：指数退避与抖动 (Exponential Backoff + Jitter)
// 当前代码出于演示极简，使用的是固定频率的线性重试（每次 time.Sleep(n.interval)）。
// 终极连环问：如果接收方服务器因为流量过大崩溃重启了，你采用固定1秒重试，千万个客户端同时重试会发生什么？
// 答：
// 会引发惨烈的“雪崩效应”（Thundering Herd Problem，惊群效应）。对方服务器刚缓过来，海量 HTTP 瞬间并发又把它打成宕机状态。
// 
// 真实生产环境（如 AWS、支付宝的异步通知）标准做法是：
// 1. 指数退避 (Exponential Backoff)：每次重试的等待时间呈指数级增加（例如 1s, 2s, 4s, 8s, 16s），给对方留出足够的时间恢复元气。
// 2. 随机抖动 (Jitter)：在退避时间基础上加上一个随机的微小偏移量（比如 4s + 123ms）。打散重试分布，防止所有客户端在同一个绝对时间点发起重试，从而彻底削平流量尖刺。
//
// 💀 【踩坑血泪·反面教材】关于 HTTP 客户端的超级大坑
// 仔细看代码 `http.Post`！这是使用了 Go 原生的默认 http 客户端。
// 灾难级漏洞：Go 默认的 http.Client **没有超时时间** (Timeout = 0)！
// 如果目标 Webhook 服务器建立了 TCP 连接，但故意或因死锁不返回任何数据响应，
// 这个 `http.Post` 会永远永远地挂起（Hold 住）。最终结果：ants 协程池里的 50 个 worker 被全部榨干卡死，系统丧失所有通知能力！
// 生产级铁律：发起外部网络请求，必须自定义带 Timeout 的 http.Client！（如 client := &http.Client{Timeout: 10 * time.Second}）
func (n *Notifier) sendWebhook(event NotifyEvent) {
    if event.Config.WebhookURL == "" {
        return // Hook URL 为空时不推送
    }

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
            // 💀 【踩坑血泪·反面教材】TCP 连接池泄漏与连接丢弃
            // 很多新手只在请求成功时 Close()，如果请求失败忘记 Close() 会导致 fd（文件描述符）泄漏。
            // 另外一个极度隐蔽的坑：如果不读取完 Body 剩余内容直接 Close()，Go 底层的 HTTP 库会强行中断并丢弃这个 TCP 连接，
            // 导致无法使用 Keep-Alive 进行连接复用，进而在高并发时产生大量处于 TIME_WAIT 状态的 TCP 端口占用。
            // 生产最优解：在 Close 前先清空管道数据 `io.Copy(io.Discard, resp.Body)`。
            resp.Body.Close()
            lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
        }
        if i < n.retry {                                          // 还有重试机会
            time.Sleep(n.interval)                                // 等待重试间隔后再试
        }
    }
    // 第三步：所有重试都失败了，记录一条警告日志
    // 这种失败处理在系统设计中被称为“尽力而为（Best Effort）”。
    // 我们尽力发了，实在发不出去也不要抛出致命错误让整个程序崩掉，记录日志即可。
    log.Warn().Err(lastErr).Str("task", event.TaskName).Msg("webhook notification failed after retries")
}

// sendEmail 发送邮件通知（占位符，需要配置SMTP才能使用）
// SMTP = 简单邮件传输协议，用来发邮件的标准协议
//
// 🧪 【测试工程·质量保障】如何安全地测试外部通知组件？
// 面试官问：在跑单元测试或本地开发时，难道真的要去发真实邮件/触发真实 Webhook 吗？
// 答：绝对不行。强依赖外部网络会导致测试极其脆弱（Flaky Tests），且极易引发测试数据污染真实系统或造成垃圾邮件骚扰。
// 
// 标准做法（Mock 挡板测试）：
// 必须提取出 Notifier 接口（interface），在业务代码里注入此接口。而在测试用例中注入 MockNotifier（空桩），
// 仅仅验证“业务逻辑是否正确构造了邮件数据”并且“是否调用了 send 接口”，从物理层面完全切断对外侧的真实请求。
// 这正是 TDD 中核心的边界隔断机制与依赖反转原则（DIP）的体现。
func (n *Notifier) sendEmail(event NotifyEvent) {
    log.Warn().Str("to", event.Config.EmailTo).Str("task", event.TaskName).
        Msg("email notification stub - configure SMTP for production use")
    // 这里只是打印了一条警告，实际发邮件需要配置SMTP服务器信息
}
