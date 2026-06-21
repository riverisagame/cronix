/*
=============================================================================================
📌 【大厂面试·核心考点】
面试官会怎么问：
1. "分布式调度系统中，如果执行器节点突然宕机（OOM 或拔网线），原本处于 Running 状态的任务该如何处理？"
2. "什么是孤儿状态（Zombie/Orphan Task）？如何检测并恢复它们？"
3. "在测试断点续传或故障恢复时，如何模拟真实的 Crash 场景并验证逻辑的自洽性？"

标准答案：
1. 在无状态调度模型中，必须依赖持久化存储（如 DB 或 Redis）。每次 Executor 启动（重启）时，
   首先执行一个 Recovery Hook（恢复钩子），扫描本机负责（或全局，取决于分片策略）的所有处于
   `Running` 状态的记录。由于进程是全新启动的，这些旧的 `Running` 记录必然是上一任进程遗留下来的
   "幽灵"，必须将其标记为 `Failed` 并且附加特定的错误信息（如进程崩溃），释放系统锁。
2. 孤儿状态指数据库记录显示任务在跑，但物理进程/线程早就不存在了。检测方式包括：重启检测（如本测试）、
   心跳超时检测（Heartbeat Timeout）。本测试覆盖的是"重启时清理"机制。

🔬 【底层原理·深度剖析】
在操作系统层面，当进程收到 SIGKILL（kill -9）或者宿主机断电时，进程没有任何机会执行 `defer`
或者 graceful shutdown 代码段。这意味着内存中的状态瞬间丢失，写到一半的数据库事务可能被回滚，但
已经提交的 "Task Start" 日志（状态变为 Running）则被永久固化在了硬盘上。
这就造成了"脑裂"幻象：DB 认为任务在跑，实际上早挂了。本测试就是专门用于验证我们系统的自动自愈（Self-healing）能力。

🏗️ 【架构设计·模式对比】
恢复策略对比：
- 策略 A：重启时全量扫描（当前代码采用）：适用于单体调度器或轻量级集群。简单可靠，进程启动时把本地的 Running 任务扫一遍。
- 策略 B：Redis/Zookeeper 租约（Lease）：适用于大型分布式集群。Executor 周期性续期租约，一旦超时，
  调度中心主动将其摘除并把其关联的 Running 任务置为失败或转移（Failover）。

💀 【踩坑血泪·反面教材】
错误做法：
如果没有这段恢复逻辑，会导致：
1. 报表系统永远显示该任务在"执行中"。
2. 如果任务配置了"单机串行"（禁止并发），那么这个任务在下一次触发时，会因为检测到有实例在"运行"，而永远拒绝启动新实例！
   最终导致定时任务被永久卡死（Deadlock of Schedule）。
=============================================================================================
*/
package scheduler

import (
	"cronix/internal/infrastructure/config" // 📌 提供配置以初始化执行器（需指定线程池等参数），属于基础设施层依赖
	"cronix/internal/domain/model"          // 📌 核心领域模型，包含 Task、ExecutionLog 和状态机枚举（如 StateRunning）
	"testing"                               // 📌 Go 原生测试框架
	"time"                                  // 📌 提供时间相关的函数，如 time.Now() 用于模拟任务启动时间

	"github.com/stretchr/testify/assert"    // 📌 提供基于断言的测试语法，简化值对比和错误输出
	"github.com/stretchr/testify/require"   // 📌 类似 assert，但失败后会立即中断当前测试块（FailNow），防止空指针或无效状态继续执行
)

// TestExecutor_RecoverOrphanedLogs 测试启动时清理孤儿执行日志的功能
// 🧪 【测试工程·质量保障】
// 测试策略：模拟 "环境受损 -> 重启自愈 -> 状态核验" 的完整闭环。
// 物理零污染原则：本测试运行在隔离的 db 实例或隔离事务中（依赖 setupExecutorTestDB），绝对不会清理正式生产库的数据。
// 🛡️ 【安全攻防·漏洞防线】
// 防御性编程：对于所有可能导致状态不一致的外部干预（如进程 OOM 被系统杀掉），都需要相应的补偿机制。
func TestExecutor_RecoverOrphanedLogs(t *testing.T) {
	db := setupExecutorTestDB(t)

	// 🔬 【底层原理·深度剖析】
	// 这里通过代码强制在 DB 注入一个静止的任务元数据，它代表了业务定义的原始逻辑起点。
	// 先造 Task 是因为外键或逻辑约束可能要求 ExecutionLog 必须有一个合法父属。
	// 插入一个任务
	task := model.Task{
		Name:     "recovery-test-task",
		TaskType: "shell",
		Command:  "echo hello",
		Enabled:  true,
	}
	db.Create(&task)

	// 💀 【踩坑血泪·反面教材】
	// 反面案例：在很多新人的测试中，会去真实 mock 一个挂掉的进程，导致测试极其不稳定且缓慢。
	// 最佳实践：测试环境里直接从数据层制造一个"未闭环的执行态（Running）"即可达到完美等价模拟，这叫做状态驱动测试。
	// 插入一条由于崩溃残留的孤儿 RUNNING 执行记录
	now := time.Now()
	orphanedLog := model.ExecutionLog{
		TaskID:      &task.ID,
		TaskName:    task.Name,
		Status:      model.StateRunning,
		TriggerType: "daemon",
		StartTime:   now,
		// ⚡ 【性能实战·生产调优】
		// end_time 为空是识别孤儿任务的核心特征之一。
		// 在千万级日志表中，为了让启动时（或者补偿定时器）快速捞出孤儿数据，
		// 强烈建议在 (status, end_time) 或者单一 status 字段上建立复合索引，否则每次重启都会触发全表扫描引发 IO 尖刺。
		// end_time 为空，表示未结束，模拟崩溃残留
	}
	db.Create(&orphanedLog)

	// 验证插入成功
	var countBefore int64
	db.Model(&model.ExecutionLog{}).Where("status = ?", model.StateRunning).Count(&countBefore)
	assert.Equal(t, int64(1), countBefore, "预插入后应有 1 条 RUNNING 日志")

	// 📌 【大厂面试·核心考点】
	// 面试官：在架构里，补偿机制是定时器做，还是在启动阶段做？
	// 答案：本场景采用了 "启动时自检"（Init-time Sanity Check）模式。
	// `NewExecutor` 构造函数内部会隐式调用恢复例程。因为在接受新任务前，自身环境的清洁度必须得到保证。
	// 模拟重启，创建新 Executor
	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize: 1,
		},
	}
	engine := NewEngine(db)
	_, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	// 此时，NewExecutor 应该自动把 orphanedLog 置为 Failed
	// 🧪 【测试工程·质量保障】
	// 此处不仅要验证日志被清理了（COUNT=0），更要精准验证"它变成了什么状态"。
	// 如果由于 Bug 把它直接 DELETE 物理删除了，那就丢失了审计追溯的证据，在企业级金融系统是大忌！只能做逻辑软变更。
	var countAfter int64
	db.Model(&model.ExecutionLog{}).Where("status = ?", model.StateRunning).Count(&countAfter)
	assert.Equal(t, int64(0), countAfter, "启动时应该清理所有 RUNNING 状态的孤儿日志")

	var reloaded model.ExecutionLog
	db.First(&reloaded, orphanedLog.ID)
	
	// 🔬 【底层原理·深度剖析】
	// 强断言恢复的正确性。补偿结束时间的意义：如果没有 EndTime，前端计算耗时（Duration）时就会抛出异常或者显示极大的负数。
	// error_msg 的强断言，验证了这不是由于业务代码抛出 Error 导致的，而是系统的"防腐层"启动时主动介入并强制打标的结果。
	assert.Equal(t, model.StateFailed, reloaded.Status, "孤儿日志应该被置为 failed 状态")
	assert.NotNil(t, reloaded.EndTime, "孤儿日志应该补上结束时间")
	assert.Equal(t, "System restarted or crashed", reloaded.ErrorMsg, "应该包含由于崩溃导致的失败原因提示")
}
