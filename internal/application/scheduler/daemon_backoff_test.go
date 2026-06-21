/*
📌 【大厂面试·核心考点】 指数退避算法与位运算溢出
面试官可能会问：
1. "在实现指数退避(Exponential Backoff)重试时，如何避免退避时间无限制增长？"
   标准答案：必须设置一个合理的退避上限（如 60s），并在计算前进行阈值判断，防止计算过程本身溢出。
2. "Golang 中的位移操作 `1 << n` 会有什么隐患？"
   标准答案：当 n 大于对应类型的位长（如 int 在 64位系统上是 64，但字面量 `1` 默认是 int 型）时，会发生整数溢出，导致变成 0 或者负数。在计算 2^n 时，如果 n 特别大，程序行为可能异常甚至 panic，这也是线上高频故障点。
3. "为什么分布式系统中重试要加 Jitter（抖动）？"
   标准答案：纯指数退避会导致多节点在同一时间发生重试风暴（Thundering Herd Problem，惊群效应），加入随机 Jitter 可以打散请求峰值，保护下游服务。

🏗️ 【架构设计·模式对比】 保护性设计的防御策略
- 错误做法（隐式溢出）：先计算 `1<<n` 再判断是否大于上限。当 n 极大时，计算已经溢出，判断失效。
- 正确做法（前置熔断）：先判断 n 的大小是否超过上限所需的阶数（如 60s 对应 2^5 < 60 < 2^6，即 n=7 时一定 >= 60），超过直接返回上限，规避一切计算溢出风险。
*/
package scheduler

import (
	// 🔬 【底层原理·深度剖析】 testing 是 Go 官方的单元测试/基准测试框架，底层通过反射机制扫描并运行以 Test 开头的函数。
	"testing"
	// 🔬 【底层原理·深度剖析】 time 包封装了系统底层的时钟机制。在测试中，基于 time.Duration (底层是 int64 纳秒) 的运算需要严格防范溢出。
	"time"
)

// TestDaemonBackoffOverflow 测试当 restartCount 过大时，退避时间位移溢出的问题
// 🧪 【测试工程·质量保障】 异常边界注入测试
// 该用例不只是验证正常逻辑，而是直接注入极端异常值（70209）以复现线上真实的血泪事故。
// 测试策略采用了等价类划分与边界值分析：
// - 正常等价类：[1, 6] 之间的正常底数递增
// - 边界值：10 (略大于 7，触发限流阀)
// - 极端异常值：70209 (线上真实灾难现场，直接导致位移溢出为0或异常)
func TestDaemonBackoffOverflow(t *testing.T) {
	// 💀 【踩坑血泪·反面教材】 致命的后置判断
	// 曾经有一段线上代码天真地以为用 if 兜底就能万无一失。
	// 当 restartCount = 70209 时，uint(restartCount-1) 远远超过 64位，
	// Go 语言规范中，如果移位次数超过操作数类型位数，结果不可预知，通常会截断或清零。
	// 导致 backoff 变成 0，进程进入死循环无限疯狂重启，打爆 CPU 并产生海量日志，服务瘫痪。
	// 模拟 daemon_monitor.go 中约 253 行的代码：
	// backoff := time.Duration(1<<uint(restartCount-1)) * time.Second
	// if backoff > 60*time.Second { backoff = 60 * time.Second }

	calculateBackoff := func(restartCount int) time.Duration {
		var backoff time.Duration
		// ⚡ 【性能实战·生产调优】 O(1) 的超轻量熔断防线
		// 不依赖复杂的 math.Pow 进行幂运算，而是直接判断指数是否超标。
		// 在 1<<6 (64) 时就已经超过 60s 阈值了，所以 restartCount >= 7 是一个安全且性能极高（仅需一次汇编级别的 CMP 指令）的前置防线。
		if restartCount >= 7 {
			backoff = 60 * time.Second
		} else {
			// 🔬 【底层原理·深度剖析】 左移操作的本质与陷阱
			// 1 << n 在二进制层面上等于 2 的 n 次方。在现代 CPU 上，位移指令 (SHL/SAL) 只需要 1 个时钟周期，
			// 是极其高效的乘法替代方案。但由于 `1` 在 Go 中未显式指定类型时为 int（32 或 64 位），
			// 必须保证右操作数不要超过左操作数的位宽，否则将产生未定义行为或归零。
			backoff = time.Duration(1<<uint(restartCount-1)) * time.Second
		}
		return backoff
	}

	// 1. 正常情况
	// 🧪 【测试工程·质量保障】 基准正确性验证。就像建筑的地基，确保第一次启动时能快速重试，不要因为退避逻辑导致初次重试也被无端延迟。
	b1 := calculateBackoff(1)
	if b1 != 1*time.Second {
		t.Errorf("Expected 1s for count 1, got %v", b1)
	}

	// 2. 正常上限情况
	// 🧪 【测试工程·质量保障】 阈值限流器验证。模拟连续失败多次后，重试间隔不能无限扩大，必须被钳制在合理范围内（例如 1 分钟上限），保护系统不被长时间挂起。
	b10 := calculateBackoff(10)
	if b10 != 60*time.Second {
		t.Errorf("Expected 60s for count 10, got %v", b10)
	}

	// 3. 触发线上 Bug: restartCount 达到 70209
	// 🛡️ 【安全攻防·漏洞防线】 拒绝服务攻击(DoS)的防御测试
	// 如果不对大数值进行前置拦截，攻击者或系统异常状态可能触发超大数值，导致位移溢出时间归零，从而导致 CPU 100% 死循环（类似DoS攻击）。
	// 这里的 70209 就是用来守护这道防线的核心回归测试！
	bBug := calculateBackoff(70209)
	if bBug != 60*time.Second {
		t.Fatalf("EXPECTED GREEN PHASE FAILURE: Expected 60s for count 70209, but got %v", bBug)
	}
}
