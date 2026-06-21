/*
===========================================================================
🏗️ 【架构设计·模式对比】
这个测试文件验证了调度器的核心设计原则之一：幂等性（Idempotency）与并发防重（Concurrency Deduplication）。
在分布式任务调度场景下，防重机制是系统的“定海神针”，常见的防重模式有：
1. 悲观锁模式（Pessimistic Locking）：使用 DB 的 SELECT FOR UPDATE。缺点是占用连接时间长，容易引发死锁。
2. 乐观锁模式（Optimistic Locking）：使用版本号/状态机。例如 UPDATE log SET status='running' WHERE id=1 AND status='pending'。
3. 唯一索引模式（Unique Index）：基于 (task_id, status) 创建局部唯一索引。优点是数据库原生支持，缺点是表达复杂业务条件（如时间差）有局限。
4. 分布式锁模式（Distributed Lock，如 Redis SETNX 或 ZooKeeper）：现代分布式系统最常用的手段。
本文件所测试的策略旨在保障：对于单例任务（Single Instance Task），在任一时刻全局仅允许一个运行中实例。

📌 【大厂面试·核心考点】
面试官连环炮：
Q: “如何防止分布式调度系统中的任务重复执行（即任务雪崩）？”
标准答案：
1. **事前防御**：在派发任务前，检查任务状态（如查询数据库是否存在 state=RUNNING 的记录），这是第一道防线。
2. **执行时拦截**：通过 Redis 的 SETNX 或 DB 的唯一约束获取执行锁（Lock），获取失败即证明已有实例运行，主动放弃执行。
3. **优雅降级**：当发现并发冲突时，可根据业务采取丢弃（Discard）、排队（Queue）或覆盖替换（Replace）等策略。
4. **超时自愈（极重要）**：必须为执行锁设置 TTL（超时时间），配合心跳续期机制，防止执行节点宕机导致死锁（即永远阻塞下次调度）。

🧪 【测试工程·质量保障】
测试原则：物理零污染（Zero Physical Pollution）。
此测试全程利用 setupExecutorTestDB 建立隔离的测试内存库/事务库，对现有真实的生产或开发数据做到毫发无损。同时禁止执行会污染全局的 DDL，纯粹的单元边界防护。
===========================================================================
*/

package scheduler

import (
	"cronix/internal/infrastructure/config"
	"cronix/internal/domain/model"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 💀 【踩坑血泪·反面教材】
// 真实生产事故案例：某一线大厂营销系统在双十一期间，一个“对账定时任务”设定每5分钟执行一次，
// 但某次大促数据量暴增，单次执行耗时超过了10分钟。
// 由于缺乏此处的 Deduplication（并发去重）机制，调度器像没有感情的机器一样持续疯狂派发新实例。
// 结果：几十上百个对账任务同时疯狂扫描全表进行聚合运算，瞬间打爆了主数据库 CPU 和 IOPS，导致整个支付链路雪崩瘫痪。
// 防范之道：严禁无状态定时任务在执行时间重叠时产生叠加爆发！永远要像防洪一样防并发重入。
//
// ⚡ 【性能实战·生产调优】
// 此处的防重逻辑，底层通常会使用类似 `SELECT COUNT(*) WHERE task_id = ? AND end_time IS NULL` 的查询。
// 在承载千万级执行日志表的大型系统中，如果没有合理的索引，这将是一场拖垮数据库的灾难。
// 调优手段：必须为 ExecutionLog 表创建复合索引：(task_id, status) 或是 (task_id, end_time)。
// 使得每次调度前的防重检查能利用 Index Range Scan 极速返回（< 5ms），绝不允许走全表扫描（Full Table Scan）。
//
// TestExecutor_DedupRunningLog 测试 executeTask 的防重逻辑：
// 如果同一任务已存在未完成的执行记录，不应创建第二条 RUNNING 日志
func TestExecutor_DedupRunningLog(t *testing.T) {
	db := setupExecutorTestDB(t)

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize: 1,
		},
	}
	engine := NewEngine(db)
	exec, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	// 创建一个测试任务
	task := model.Task{
		Name:     "dedup-test-task",
		TaskType: "shell",
		Command:  "echo hello",
		Enabled:  true,
	}
	db.Create(&task)

	// 🔬 【底层原理·深度剖析】
	// 我们可以用生活中的场景来理解：这就好比你去热门餐厅排队，前台小妹（Scheduler）看到属于你的那张桌子（Task）上的人还在吃（existingLog 未结账），
	// 就绝不会再让第二拨人过去坐下。
	// 在分布式调度系统底层，`end_time IS NULL`（或 status=RUNNING）被用作一个天然的系统信号量（Semaphore）。
	// 此模式强依赖于数据库 ACID 特性中的隔离性（Isolation）。如果在极端高并发下，为了防止脏读/不可重复读引发的防重击穿，
	// 这步检查及后续的写入动作必须在一个具有防并发机制的原子事务（Atomic Transaction）中，或配合分布式锁完成。
	//
	// 预插入一条 RUNNING 状态的执行记录（模拟已有未完成执行）
	now := time.Now()
	existingLog := model.ExecutionLog{
		TaskID:      &task.ID,
		TaskName:    task.Name,
		Status:      model.StateRunning,
		TriggerType: "cron",
		StartTime:   now,
		// end_time 为空，表示未结束
	}
	db.Create(&existingLog)

	// 记录当前该任务的执行日志数量
	var countBefore int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&countBefore)
	assert.Equal(t, int64(1), countBefore, "预插入后应有 1 条日志")

	// 🛡️ 【安全攻防·漏洞防线】
	// 这里防范的是一个经典安全/并发漏洞模型：竞态条件（Race Condition，尤指 TOCTOU：Time of Check to Time of Use）。
	// 如果恶意操作者极速连续发送两次手动触发该任务的 HTTP 请求：
	// 若代码写成了松散的 `1. SELECT check -> （网络延迟） -> 2. INSERT log`
	// 两次请求可能会同时完成步骤1的 check，导致防重失效，创建出两条 RUNNING 数据。
	// 这个测试虽然验证了被拦截的结果，但也要求开发者在底层 `exec.executeTask` 中确保检查+写入的绝对原子性（如同 CAS 操作）。
	//
	// 触发 executeTask（应该被防重逻辑拦截）
	exec.executeTask(task.ID)

	// 等待异步 goroutine 完成（若有的话，executeTask 是同步的）
	time.Sleep(200 * time.Millisecond)

	// 验证：执行日志数量应该仍然是 1
	var countAfter int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&countAfter)
	assert.Equal(t, int64(1), countAfter, "防重逻辑应阻止创建第二条 RUNNING 日志")

	// 验证：existingLog 的 end_time 仍然为空（未被篡改）
	var reloaded model.ExecutionLog
	db.First(&reloaded, existingLog.ID)
	assert.Nil(t, reloaded.EndTime, "原有的 RUNNING 记录的 end_time 不应被修改")
}

// 🧪 【测试工程·质量保障】
// 测试闭环黄金法则：既要证明“不该运行的被死死拦住了”（Negative Test，即上方的 TestExecutor_DedupRunningLog），
// 也要证明“该运行的顺滑地跑通了”（Positive Test）。
// 如果仅有上面的防重测试，很可能系统实现者的代码是“永远拒绝一切执行”，虽然通过了上个用例，但系统其实已经瘫痪了。
// 本测试通过完整的闭环验证，确保了防重机制不是一刀切的物理死锁，而是随着任务生命周期流动（结束即释放）的动态解锁机制。
// 
// TestExecutor_DedupAfterCompletion 测试：当之前的执行完成后，新调用应该能正常执行
func TestExecutor_DedupAfterCompletion(t *testing.T) {
	db := setupExecutorTestDB(t)

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize: 1,
		},
	}
	engine := NewEngine(db)
	exec, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	task := model.Task{
		Name:     "dedup-after-complete-task",
		TaskType: "shell",
		Command:  "echo test",
		Enabled:  true,
	}
	db.Create(&task)

	// 🔬 【底层原理·深度剖析】
	// 为什么调度引擎选择判断 `end_time IS NOT NULL` 作为完成释放的标志，而不是直接从数据库中将已完成的记录物理删除？
	// 这就是分布式系统著名的“软状态设计（Soft State & Audit Trail）”。
	// 执行日志的留存具有两大关键作用：
	// 1. 业务审计（Audit）：用于执行耗时分析、失败链路追溯。
	// 2. 数据库性能考量：比起物理删除（DELETE）会产生大量的 MVCC 垃圾、索引碎片并引发频繁的 Page Split，
	//    保留记录并仅做 UPDATE 填充时间戳的方式，既实现了执行锁的释放逻辑，又避免了昂贵的 IO 开销，可谓一石二鸟。
	//
	// 插入一条已完成（有 end_time）的执行记录 — 这不影响新触发
	now := time.Now()
	completedLog := model.ExecutionLog{
		TaskID:      &task.ID,
		TaskName:    task.Name,
		Status:      model.StateSuccess,
		TriggerType: "cron",
		StartTime:   now,
		EndTime:     &now, // 已完成
	}
	db.Create(&completedLog)

	// 触发 executeTask — 应该正常创建新 RUNNING 日志
	exec.executeTask(task.ID)
	time.Sleep(200 * time.Millisecond)

	// 验证：应该有 2 条日志（1 条已完成 + 1 条新的 running/成功/失败）
	var totalCount int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&totalCount)
	assert.GreaterOrEqual(t, totalCount, int64(2),
		"已完成的任务不影响新触发，应有新的执行记录")
}
