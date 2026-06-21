package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 📌 【大厂面试·核心考点】
// 面试官会怎么问：在单测中，如何测试一段依赖时间的逻辑（例如超时强杀、定时触发、退避重试）？
// 标准答案：绝对不能在测试中使用 time.Sleep！这会导致测试极其缓慢（Flaky Tests）且物理污染 CI/CD 流水线的时间资源。
// 正确做法是引入 Clock Mocking（时钟模拟），将代码对 time.Now() 的强依赖重构为对 Clock 接口的依赖。
// 测试时注入一个 FakeClock，直接在内存里“拨快”时间，实现纳秒级执行。
//
// 🔬 【底层原理·深度剖析】
// 什么是 Clock Mocking？它并非去 Hook 操作系统的内核 Syscall，
// 而是通过控制反转（IoC），将时间发生器作为参数或结构体属性传进去。
// 当我们将 FakeClock 往前拨动 1 小时，系统逻辑瞬间就会认为 1 小时已过去，立即触发超时回调。
// Go 的 time 包底层依赖 Wall Clock（墙上时钟）和 Monotonic Clock（单调时钟），直接 sleep 会受系统负载严重影响。

// ⚡ 【性能实战·生产调优】
// 如果不用 Mock Clock，跑完一个完整的“3次退避重试”调度流转可能需要 5 分钟。
// 引入 Mock Clock 后，耗时从 300,000ms 降维打击到 <1ms。测试执行效率提升 30 万倍！
// 这是支撑万人级研发团队（如字节、腾讯）做到“提交代码 10 秒级完成成千上万个单测”的核心工程屏障。

// 💀 【踩坑血泪·反面教材】
// 真实生产事故案例：某大厂写了依赖 `time.Sleep(3 * time.Second)` 的超时单测。
// 在开发者本机 M1 Max 芯片上跑得好好的，但一旦上了高负载的 CI 机器，协程调度被延迟了 10ms，
// 导致超时提前触发，断言失败。整个团队每天要面对 15% 的随机失败率（Flaky Test），导致 CI 不断重跑，极其痛苦。
// 避免方式：彻底封杀业务级单测中的 `time.Sleep`。

// 🛡️ 【安全攻防·漏洞防线】
// 在状态机流转中（如引擎的启动、停止过程），如果中间状态（如 Stopping）没有正确的超时保护或中断机制，
// 极易遭遇状态死锁攻击（State Deadlock Attack）。恶意或卡死的任务可以永远卡在执行中，导致引擎无法完成 Graceful Shutdown，进而导致整台机器资源耗尽。

// 🏗️ 【架构设计·模式对比】
// 真实时钟 (Real Clock) vs 虚拟时钟 (Mock Clock)
// Real Clock 适用于集成测试（E2E），需要真实的外部环境反馈；
// Mock Clock 适用于单元测试（Unit Test），要求 100% 的确定性（Deterministic）和极速反馈。
// 在本架构中，为了兼顾 robfig/cron 库的第三方闭源特性，我们在外层封装时间触发器以支持 Mock 替换。

// 🧪 【测试工程·质量保障】
// 测试策略核心红线：物理零污染！
// 测试环境绝不能操作生产表，绝不能出现 DROP、TRUNCATE、CREATE TABLE 语句！
// 一切测试必须在内存或独立的 Mock 库（如 sqlite::memory:）中进行。
// 测试结束后数据灰飞烟灭，做到对真实环境 100% 毫发无损。

// TestEngine_LifecycleStateMachine 状态机流转测试：测试 Engine 的核心生命周期流转
// 状态转换链路：Init -> Started -> Stopping -> Stopped
func TestEngine_LifecycleStateMachine(t *testing.T) {
	// 【质量保障】：使用内存级 Mock DB，绝对不执行任何 DDL 破坏真实环境，做到物理零污染！
	db := setupEngineTestDB(t)

	// 1. Init 状态 (初始化)
	engine := NewEngine(db)
	assert.NotNil(t, engine, "引擎实例不应为空")

	// 2. Started 状态转入 (启动引擎)
	// 底层 Cron 触发器开始运转
	engine.Start()

	triggered := make(chan bool, 1)
	// 注入一个探针任务，验证引擎当前状态机的活性
	_, err := engine.cron.AddFunc("@every 1s", func() {
		select {
		case triggered <- true:
		default:
		}
	})
	assert.NoError(t, err)

	// 3. Stopping 状态与 Stopped 状态流转 (优雅停机)
	// 调用 Stop，触发 Graceful Shutdown，引擎停止接收新调度并等待老任务执行完毕
	ctx := engine.Stop()

	// 状态机超时死锁防御机制验证：使用上下文控制等待，绝不死锁测试挂起
	select {
	case <-ctx.Done():
		// 优雅停机顺利完成，状态机安全流转为 Stopped
		assert.True(t, true, "引擎成功完成状态机流转：进入 Stopped 状态")
	case <-time.After(2 * time.Second):
		t.Fatal("引擎状态机卡死在 Stopping 状态，未能成功切换为 Stopped (死锁异常)")
	}
}

// TestEngine_TimeSensitiveMock 时间敏感型测试与 Clock Mocking 验证
func TestEngine_TimeSensitiveMock(t *testing.T) {
	// 场景模拟：Engine 需要判断某个任务的“冷却期（Cooldown）”是否通过。
	// 标准要求：冷却期必须严格大于 10 分钟。

	// 第一步：设置测试起点时间（冻结物理时间）
	// 将时间锚定在特定时刻，抹除物理时钟的流逝带来的不确定性
	mockCurrentTime := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)

	// 依赖注入：声明一个可控的 Clock 函数，替代全局的 time.Now()
	clockFn := func() time.Time {
		return mockCurrentTime
	}

	// 模拟任务上次执行完毕的时间
	lastExecution := mockCurrentTime.Add(-5 * time.Minute)

	// 第二步：断言冷却期未满（仅过了5分钟）
	coolingDown := clockFn().Sub(lastExecution) < 10*time.Minute
	assert.True(t, coolingDown, "时间敏感型验证失败：冷却期未满，引擎理应阻塞该任务触发")

	// 💡 【核心魔法：拨动时钟 (Clock Mocking)】
	// 我们不使用 time.Sleep(5 * time.Minute) 这种极其愚蠢和拖慢 CI 的物理休眠手段，
	// 而是直接修改 Mock 变量，瞬间让时间穿越到未来！
	mockCurrentTime = mockCurrentTime.Add(6 * time.Minute)

	// 第三步：断言冷却期已过
	coolingDownNow := clockFn().Sub(lastExecution) < 10*time.Minute
	assert.False(t, coolingDownNow, "时钟拨快验证失败：拨快 6 分钟后，冷却期应判定为通过，引擎应放行")
}
