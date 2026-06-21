// ============================================================
// internal/circuit/circuit.go - HTTP熔断器
//
// ============================================================
// internal/circuit/circuit.go - HTTP熔断器
//
// 【小白秒懂课堂：什么是熔断器（Circuit Breaker）？】
// 想象家里的电闸（保险丝）：
// 如果你家里同时开空调、烤箱、吹风机，电流太大，保险丝就会“啪”地跳闸（熔断）。
// 为什么要跳闸？为了防止电线起火，把整栋楼都烧了！
//
// 软件系统也是一样。假设你要去请求微信的接口，但是微信服务器挂了。
// 如果你一直疯狂请求（重试），不仅微信彻底死机，你自己的服务器也会被卡死，
// 进而导致调用你的服务器也卡死……这在微服务架构里叫【雪崩效应（Cascading Failure）】。
// 
// 熔断器的作用就是：当微信接口连续失败 N 次后，我就主动“拉闸”（Open）。
// 接下来别人再来请求，我直接秒回“失败”，不再去骚扰微信了，保护大家都不死。
//
// 💡 【大厂面试·底层原理扩展】
// 面试官问：熔断器怎么知道服务器什么时候修好了？
// 答：状态机的【半开（Half-Open）状态】！
// 熔断后，等一段时间（冷却期），偷偷放**一个请求**过去试探一下：
//   - 如果试探成功（微信修好了），合上电闸（Closed）。
//   - 如果试探失败（微信还没修好），继续保持拉闸（Open），重新计算冷却时间。
//
// 经典的三种状态：
//   Closed（关闭电闸）= 正常通电，请求正常通过
//   Open（拉下电闸）= 拒绝所有请求，秒回失败
//   HalfOpen（半开）= 允许一次探测请求来判断服务是否恢复
//
// 状态转换：
//   Closed → 连续失败N次 → Open
//   Open → 冷却时间到 → HalfOpen
//   HalfOpen → 探测成功 → Closed
//   HalfOpen → 探测失败 → Open
//
// 🏗️ 【架构设计·模式对比】
// 熔断器模式（Circuit Breaker）vs 重试模式（Retry）vs 限流模式（Rate Limiting）：
// - 熔断器：关注于“快速失败（Fail-Fast）”，保护调用方和被调用方，防止雪崩。当依赖服务已经不可用时，阻断后续请求。
// - 重试：关注于“克服瞬态故障”，处理偶尔的网络抖动。若滥用重试（不加退避策略），容易引发雪崩（Retry Storm）。
// - 限流：关注于“自我保护”，限制进入系统的流量，确保系统不被大流量打垮。熔断是调用端视角的保护，限流是服务端视角的保护。
// 在微服务架构中，这三者通常组合使用（如：限流 + 熔断 + 带抖动的指数退避重试）以构建高可用系统。
//
// 💀 【踩坑血泪·反面教材】
// 生产事故案例：某大厂曾因某个非核心依赖（如日志收集服务）接口响应变慢，且未配置超时与熔断器。
// 导致调用方的所有 Goroutine 在等待超时中耗尽阻塞，最终引发核心交易链路全盘崩溃（协程池耗尽、内存溢出 OOM）。
// 血泪教训：所有跨网络调用（HTTP/RPC/DB/Redis）必须配置绝对超时时间与熔断器，在架构层面物理剥离强依赖与弱依赖。
// ============================================================
package circuit

import (
    "sync"   // 并发控制：互斥锁
    "time"   // 时间处理：冷却期计算
)

// CircuitState 是熔断器状态的类型定义
// iota 是Go的自动递增常量生成器
//
// 📌 【大厂面试·核心考点】
// 面试官：请描述熔断器的三大状态及其流转引擎？
// 标准答案：
// 1. Closed (关闭状态)：电路连通，所有请求正常放行。当错误次数或错误率（如达到50%）超过阈值时，状态流转为 Open。
// 2. Open (断开状态)：电路断开，启动快速失败（Fail-Fast，直接返回 Error 或触发 Fallback 兜底数据），切断真实的下游网络调用。同时启动冷却定时器。
// 3. Half-Open (半开状态)：冷却时间到期后，进入半开探测阶段。此时系统会以极小流量（通常仅放行1个探针请求）试探下游健康度：
//    - 若探针调用成功，说明被调用方已恢复，流转回 Closed 状态。
//    - 若探针调用失败，说明故障依旧存在，回退到 Open 状态，重置冷却倒计时。
type CircuitState int

const (
    CircuitClosed   CircuitState = iota // 0: 关闭状态（正常通行）
    CircuitOpen                         // 1: 断开状态（拒绝请求）
    CircuitHalfOpen                     // 2: 半开状态（允许探测）
)

// CircuitBreaker 熔断器结构体，保护外部HTTP调用
// 
// 🔬 【底层原理·深度剖析】
// 简易计数 vs 滑动时间窗口原理（Sliding Window）与无锁计数（Lock-free）：
// 1. 当前代码：采用了最朴素的绝对计数（failures）+ Mutex 全局互斥锁。这种实现在轻量级场景下非常可靠，但是当流量极大时存在两处缺陷：
//    - 没有时间衰减：如果在一天内零散失败了N次也会触发熔断，这是不合理的，应该限定在“单位时间”内。
//    - 锁竞争瓶颈：万级 QPS 时，所有 Goroutine 争抢同一把 sync.Mutex 会引发大量内核态 Context Switch。
// 2. 工业级进阶（如 Sentinel-Go / Hystrix）：
//    - 引入“滑动时间窗口”机制，比如设定窗口长度 1 秒，切分为 10 个 100ms 的 Bucket。环形数组滚动记录，仅统计当前存活 Bucket 内的成功/失败总数计算错误率。
//    - 搭配 `sync/atomic` 执行原子累加（atomic.AddInt64）和 CAS 状态变更，达成完全无锁（Lock-Free）的高性能状态流转引擎。
//
// ⚡ 【性能实战·生产调优】
// 性能数据参考：sync.Mutex 在无冲突时的耗时约 10-20ns，激烈竞争下可能飙升至数百毫秒。
// 生产环境调优建议：如果 QPS > 2000，必须舍弃 Mutex 计数，改用 atomic 进行多 Bucket 分段累加。
type CircuitBreaker struct {
    mu          sync.Mutex    // 互斥锁：保证多goroutine并发访问安全
    state       CircuitState  // 当前状态（Closed/Open/HalfOpen）
    failures    int           // 连续失败的次数
    lastFailure time.Time     // 上一次失败的时间（用于计算冷却期）
    threshold   int           // 阈值：连续失败多少次后熔断
    cooldown    time.Duration // 冷却时长：熔断后多久可以尝试探测
}

// NewCircuitBreaker 创建新的熔断器
// 参数 threshold：连续失败多少次后触发熔断
// 参数 cooldownSec：熔断后冷却多少秒再尝试恢复
func NewCircuitBreaker(threshold int, cooldownSec int) *CircuitBreaker {
    return &CircuitBreaker{
        state:     CircuitClosed,                                          // 初始状态：关闭（正常）
        threshold: threshold,                                              // 失败阈值
        cooldown:  time.Duration(cooldownSec) * time.Second,               // 冷却时长转成标准时间长度
    }
}

// Allow 检查当前是否允许请求通过
// 返回值：true=允许通过，false=拒绝
//
// 🛡️ 【安全攻防·漏洞防线】
// 并发状态下的“惊群效应”（Thundering Herd Problem）与流量穿透危险：
// 注意这里的 Half-Open 逻辑陷阱。在多并发下，当冷却期结束，第一个请求通过 `time.Since` 检查将 state 置为 HalfOpen。
// 但因为这里在 HalfOpen 分支里直接 `return true`，如果该探针请求执行非常缓慢（比如 5 秒），
// 这 5 秒内涌入的另外 1000 个并发请求检查到 `cb.state == CircuitHalfOpen` 时也会统统 `return true` 穿透过去！
// 这将导致本来刚苏醒的脆弱微服务瞬间被这波并发探针再次打挂。
// 进阶防御：在 HalfOpen 状态下，必须引入探针计数器或 CAS 标志位，确保在探针未返回结果前，仅仅放行“绝对的 1 个”请求，其余全部阻挡！
func (cb *CircuitBreaker) Allow() bool {
    cb.mu.Lock()                                                // 加锁
    defer cb.mu.Unlock()                                        // 函数结束时解锁

    switch cb.state {                                            // 根据当前状态决定
    case CircuitClosed:                                          // 关闭状态
        return true                                              // 正常通行
    case CircuitOpen:                                            // 断开状态
        // 检查冷却期是否已过
        if time.Since(cb.lastFailure) > cb.cooldown {            // 距离上次失败已经超过冷却时间
            cb.state = CircuitHalfOpen                           // 进入半开状态
            return true                                          // 允许一次探测请求
        }
        return false                                             // 冷却期未过，拒绝请求
    case CircuitHalfOpen:                                        // 半开状态
        return true                                              // 允许探测请求通过（⚠生产警告：此处存在多并发穿透漏洞，详见注释）
    }
    return false
}

// RecordSuccess 记录一次成功的请求
// 成功意味着服务恢复正常，关闭熔断器
func (cb *CircuitBreaker) RecordSuccess() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    cb.failures = 0                                             // 清空失败计数
    cb.state = CircuitClosed                                    // 回到正常状态
}

// RecordFailure 记录一次失败的请求
// 如果连续失败次数达到阈值，打开熔断器
//
// 🧪 【测试工程·质量保障】
// 时钟测试隔离与边界测试（Boundary & Mock Clock）：
// - 边界测试：必须构造单元测试，精准验证 failures == threshold - 1 时电路不断开，恰好等于 threshold 时触发 CircuitOpen。
// - 时钟隔离：此代码硬编码了 time.Now() 和 time.Since()。在高质量的工程中，这种“外部依赖”是测试毒药（会导致 time.Sleep() 的慢单测与 flaky test）。
//   正确解法是将 `time.Now()` 抽象为 `type Clock func() time.Time` 注入进去。测试时可直接篡改时间来跳过冷却期，实现纳秒级单测！
func (cb *CircuitBreaker) RecordFailure() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    cb.failures++                                               // 失败次数+1
    cb.lastFailure = time.Now()                                 // 记录失败时间
    if cb.failures >= cb.threshold {                            // 连续失败次数达到阈值
        cb.state = CircuitOpen                                  // 打开熔断器
    }
}

// State 返回熔断器当前的状态
func (cb *CircuitBreaker) State() CircuitState {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    return cb.state
}
