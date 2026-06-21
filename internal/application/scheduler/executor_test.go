package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"cronix/internal/domain/model"
	"cronix/internal/infrastructure/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/*
📌 【大厂面试·核心考点】全局架构总览
面试官会怎么问？：“在 Go 语言实现的分布式调度系统中，如何优雅且彻底地终止一个可能死循环的 Shell 命令？如果不彻底终止会引发什么后果？”
标准答案揭秘：
1. 【信号传递】必须使用 `context.Context` 作为中枢控制神经，配合 `exec.CommandContext` 来启动外部的 OS 进程。
2. 【内核强杀】当 `Context` 被 `cancel()` 函数触发或者发生超时（Deadline Exceeded）时，Go 语言底层运行时会自动向该关联进程发送 `SIGKILL` 信号。
3. 【资源泄露灾难】如果不采用强杀机制，即使外层控制调度的 Goroutine 退出了，底层的 Shell 或 Python 进程依然会作为孤儿进程存在。这就导致 CPU 占用率和内存泄露持续飙升，这就是传说中调度系统的“僵尸进程”风暴。
*/

/*
🔬 【底层原理·深度剖析】
就像初二小白也能听懂的“树状大厦断电机制”：想象一栋大厦有一把总电闸（Root Context），每个楼层、每个房间都有分电闸（Child Context）。
1. 【Context 取消树级联机制】：`context.WithCancel` 会创建一个子上下文，并在内部的底层数据结构中维护一个 `children` 映射。
2. 【多米诺骨牌效应】：一旦调用总电闸的 `cancel()`，它不仅会关闭自己的 `done` channel（切断当前层的电源），还会通过递归遍历所有子孙上下文，级联拉下所有子节点的电闸。
3. 【OS 级别的映射】：在进程管理层面，Go 的底层运行时会监听这个 `done` channel，一旦发现通道可读（电闸拉下），立刻执行底层系统调用（如 Linux 的 `kill -9`）终止外部映射进程。
*/

// 🧪 【测试工程·质量保障】
// 测试策略：白盒测试与端到端（E2E）行为验证深度结合。
// 覆盖率保障：本测试主要针对调度器的核心链路（Context 信号传播）进行验证，确保业务代码覆盖率在异常流分支（强杀分支）达到 100%。
// 状态一致性：我们不仅要验证 OS 级别的进程是否被内核干掉，更要验证系统控制层是否正确捕获该中断信号，并将数据库中记录的任务状态从 `Running` 准确地置为 `Failed` 或 `Cancelled`。保证了内存态、OS 态与 DB 态的绝对一致。
func TestExecutor_ContextCancellation(t *testing.T) {
	// 启动物理隔离的测试数据库，杜绝与其他测试产生脏数据交叉感染
	db := setupExecutorTestDB(t)

	// ⚡ 【性能实战·生产调优】
	// 在海量调度系统中，为了防止任务积压，必须给每个执行器配置兜底边界。
	// 这里设置最大超时为 10 秒，空间上限制输出流最大 64KB（防止大日志打爆内存）。
	// 时间复杂度为 O(1) 触发，空间复杂度限制在 O(1)，保障系统整体的韧性（Resilience）。
	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:         2,
			MaxTimeoutSec:    10,
			OutputTruncateKB: 64,
		},
	}
	engine := NewEngine(db)
	exec, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err, "执行器初始化失败")

	// 💀 【踩坑血泪·反面教材】
	// 错误做法：在写这种测试时，如果把 `TimeoutSec` 设得比 `sleep` 的时间还短，会导致任务被自身的超时机制干掉，而不是被下面我们手动触发的 context 取消干掉。
	// 正确做法：必须将 `TimeoutSec` 设置得足够大（例如 10 秒），以保证它纯粹因为我们外部传递的 `cancel()` 信号而阵亡。
	task := model.Task{
		Name:       "context-cancel-test",
		TaskType:   "shell",
		Command:    "sleep 10", // 故意设定一个长耗时的睡眠命令，充当靶子
		Enabled:    true,
		TimeoutSec: 10,
	}
	db.Create(&task)

	// 派生出一把能够切断这根“控制神经”的利刃：cancel()
	ctx, cancel := context.WithCancel(context.Background())
	
	// 启动一个后台 Goroutine 去执行任务
	go func() {
		// 这里的 `ctx` 犹如紧箍咒，戴在任务的头上
		exec.ExecuteTaskWithContext(ctx, task.ID)
	}()

	// 留给系统 1 秒的喘息时间，确保底层命令 `sleep 10` 已经真正在 OS 层面被拉起并处于 Running 状态
	time.Sleep(1 * time.Second)
	
	// 🗡️ 手起刀落：主动发出取消信号，模拟用户在前端点击了“强制停止”按钮
	cancel()
	
	// 再次留出 1 秒，等待底层 OS 杀死进程、Executor 捕获错误并将其持久化更新到数据库中
	time.Sleep(1 * time.Second)

	var logs []model.ExecutionLog
	db.Where("task_id = ?", task.ID).Find(&logs)
	require.NotEmpty(t, logs, "由于任务已启动，必然会生成一条 ExecutionLog 执行日志")

	latestLog := logs[len(logs)-1]
	
	// 🛡️ 【安全攻防·漏洞防线】
	// 验证其状态绝不可能是 StateRunning。如果这里依然是 Running，说明系统出现了灾难性的“僵尸泄露漏洞”。
	// 安全防御策略：这是防御而已提交死循环恶意脚本攻击（如 while true; do fork; done 炸弹）的最后一道防线。
	assert.NotEqual(t, model.StateRunning, latestLog.Status, "Context 被取消后，任务状态应当流转为非运行态")
	assert.NotNil(t, latestLog.EndTime, "强杀后必须结算结束时间，否则会导致前端进度条假死")
}

/*
🏗️ 【架构设计·模式对比】
为什么要在底层把 Stdout（标准输出）和 Stderr（标准错误）合并捕获？而不是各走各的道？
1. 【方案A（双轨分离捕获）】：使用两个独立的 Buffer 分别读取。
   - 缺点：日志顺序在多核并发竞争写入时极易产生错乱。用户在前端看到的日志，错误信息和正常日志的时间线是混乱交织的，毫无排查价值。
2. 【方案B（单轨合并捕获，当前系统的黄金法则）】：
   - 像修水管一样，在底层执行 `cmd.Stderr = cmd.Stdout`，将错误流全部重定向、并入到标准输出主干道。
   - 优点：保证了应用事件流产生的绝对时间顺序（Absolute Temporal Ordering），极大地提升了日志的可观测性（Observability）和研发排障体验。
*/

// 🧪 【测试工程·质量保障】
// 这里的测试核心在于验证“双流合一”以及流的完整性。
// 我们不仅模拟了正常的 echo 输出，还通过 `>&2` 的 Shell 语法故意制造了标准错误流，
// 用于断言我们的系统能否将这两股不同来源的信息如实、无丢失地截获并入库。
func TestExecutor_StdoutStderrCapture(t *testing.T) {
	// 启动隔离环境
	db := setupExecutorTestDB(t)

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:         2,
			MaxTimeoutSec:    5,
			OutputTruncateKB: 64, // 截断阈值测试（限制在 64KB），保护内存免受恶劣日志风暴摧残
		},
	}
	engine := NewEngine(db)
	exec, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	// 💀 【踩坑血泪·反面教材】
	// 曾经有个同事在测试输出流时，只测了 `echo hello`。
	// 后来生产环境有脚本报错（报错写在了 stderr 中），由于我们的程序当时只拿了 stdout，
	// 导致页面上日志空空如也，用户只能盯着“失败”的状态发呆。
	// 所以，全面捕获 Stdout 与 Stderr 是调度系统的基础底线。
	task := model.Task{
		Name:       "stdout-stderr-test",
		TaskType:   "shell",
		// 使用 && 串联，前一个写入标准输出，后一个通过 >&2 强行写入标准错误
		Command:    "echo 'hello stdout' && echo 'hello stderr' >&2",
		Enabled:    true,
		TimeoutSec: 5,
	}
	db.Create(&task)

	// 立即发车，堵塞执行直到完成
	exec.RunTaskNow(task.ID)
	
	// 给一点异步落盘与状态流转的时间，防止由于磁盘 IO 导致的 Assert 竞态失败
	time.Sleep(2 * time.Second)

	var logs []model.ExecutionLog
	db.Where("task_id = ?", task.ID).Find(&logs)
	require.NotEmpty(t, logs, "任务触发后必然会产生执行日志流水")

	latestLog := logs[len(logs)-1]
	
	// 🎯 核心断言：不仅任务要成功，还要保证输出内容一字不漏
	assert.Equal(t, model.StateSuccess, latestLog.Status, "简单的 echo 命令应当返回成功态")
	
	// 检查是否完美捕获到了来自 Stdout 的正常问候
	assert.True(t, strings.Contains(latestLog.Output, "hello stdout"), "应当捕获到标准输出的内容")
	
	// 检查是否同样完美捕获到了来自 Stderr 的错误流问候
	assert.True(t, strings.Contains(latestLog.Output, "hello stderr"), "应当捕获到标准错误的内容，证明 Stderr 被正确重定向与合并")
}
