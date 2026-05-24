// ============================================================
// internal/circuit/circuit.go - HTTP熔断器
//
// 熔断器（Circuit Breaker）是一种保护机制，就像家里的保险丝：
// 当外部服务反复出错时，"熔断"连接，避免持续无效请求。
// 等一段时间（冷却期）后再尝试一次探测请求。
// 如果探测成功，恢复正常；如果失败，继续熔断。
//
// 三种状态：
//   Closed（关闭）= 正常状态，请求正常通过
//   Open（断开）= 拒绝所有请求
//   HalfOpen（半开）= 允许一次探测请求来判断服务是否恢复
//
// 状态转换：
//   Closed → 连续失败N次 → Open
//   Open → 冷却时间到 → HalfOpen
//   HalfOpen → 探测成功 → Closed
//   HalfOpen → 探测失败 → Open
// ============================================================
package circuit

import (
    "sync"   // 并发控制：互斥锁
    "time"   // 时间处理：冷却期计算
)

// CircuitState 是熔断器状态的类型定义
// iota 是Go的自动递增常量生成器
type CircuitState int

const (
    CircuitClosed   CircuitState = iota // 0: 关闭状态（正常通行）
    CircuitOpen                         // 1: 断开状态（拒绝请求）
    CircuitHalfOpen                     // 2: 半开状态（允许探测）
)

// CircuitBreaker 熔断器结构体，保护外部HTTP调用
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
        return true                                              // 允许探测请求通过
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
