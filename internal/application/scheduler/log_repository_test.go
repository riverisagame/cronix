/*
📌 【大厂面试·核心考点】
面试官：海量定时任务场景下，如何保证日志表的查询与写入性能？在写日志仓储库测试时，需要注意哪些问题？
标准答案：
1. 性能问题：日志表通常只增不改或极少修改（状态流转）。生产上会使用时间分区表（如按月、按天归档），或者冷热数据分离设计，因此仓储测试中要重点验证 DeleteExcessTaskLogs 或清理逻辑的边界情况，避免大事务锁表。
2. 慢查询测试：在获取最新日志（GetLatestTaskLog）、统计任务执行状态（CountRunningLogs）时，依赖复合索引（TaskID, Status, StartTime/ID 等），测试中应关注这些索引在海量数据下的执行计划。
3. 物理零污染：测试绝不能直接 DROP 生产表。通常采用 sqlite 内存库，或用事务包裹并在结束时 Rollback（Transactional Tests）来实现。这里 Gorm 的 setup 应当保障物理数据的严格隔离，做到执行前后无痕迹。

🔬 【底层原理·深度剖析】
日志表结构演进与分表策略：
初创期：单表 CRUD。
爆发期：单表体积膨胀导致慢查询。优化方案为水平分表（根据 TaskID 取模，或者 StartTime 按月分区）。
在分表场景下的测试考量：如果要测试分表逻辑，这里需要 Mock 分表路由或时间函数，验证跨分片/跨时间窗的查询统计聚合是否正确，避免跨片 Join。
*/
package scheduler

import (
	"cronix/internal/domain/model"
	"testing"
	"time"

	// 🧪 【测试工程·质量保障】
	// require 和 assert 的核心区别：require 失败会导致当前 Goroutine 立即终止（FailNow），
	// 而 assert 失败会继续执行后续检查。在数据库测试中，如果 DB 连接创建失败等前置条件不满足，应该使用 require 及时止损。
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGormLogRepository_CreateAndSave 测试 GORM 实现的基本创建和保存
func TestGormLogRepository_CreateAndSave(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	task := model.Task{Name: "repo-test-task", TaskType: "shell", Command: "echo hi", Enabled: true}
	db.Create(&task)

	// 创建执行日志
	now := time.Now()
	execLog := &model.ExecutionLog{
		TaskID:      &task.ID,
		TaskName:    task.Name,
		Status:      model.StateRunning,
		TriggerType: "cron",
		StartTime:   now,
	}
	err := repo.CreateExecutionLog(execLog)
	require.NoError(t, err)
	assert.NotZero(t, execLog.ID, "创建后应分配 ID")

	// 更新执行日志
	endTime := time.Now()
	execLog.Status = model.StateSuccess
	execLog.EndTime = &endTime
	err = repo.SaveExecutionLog(execLog)
	require.NoError(t, err)

	// 验证更新生效
	var reloaded model.ExecutionLog
	db.First(&reloaded, execLog.ID)
	assert.Equal(t, model.StateSuccess, reloaded.Status)
	assert.NotNil(t, reloaded.EndTime)
}

/*
⚡ 【性能实战·生产调优】
- 真实场景：任务执行监控大盘如果频繁调用 `CountRunningLogs`，单表几百万数据下 `count(*)` 会退化为慢查询。
- 调优策略：1. 在 `(task_id, status)` 创建联合索引；2. 业务侧考虑采用 Redis 缓存（任务启动 incr，结束 decr），定期通过 DB 对账，降低主库只读压力。
🧪 【测试工程·质量保障】
- 测试边界：必须覆盖“已完成”、“运行中”、“已取消”等所有状态的计数组合，并确认不同 trigger_type 维度下的隔离性。
*/
// TestGormLogRepository_CountRunningLogs 测试统计运行中日志
func TestGormLogRepository_CountRunningLogs(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	task := model.Task{Name: "count-test", TaskType: "shell", Command: "echo", Enabled: true}
	db.Create(&task)

	// 初始应该为 0
	count, err := repo.CountRunningLogs(task.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	// 插入一条 running 日志
	now := time.Now()
	db.Create(&model.ExecutionLog{
		TaskID: &task.ID, TaskName: task.Name,
		Status: model.StateRunning, TriggerType: "cron", StartTime: now,
	})

	count, err = repo.CountRunningLogs(task.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// 已完成的不应计入
	endTime := time.Now()
	db.Create(&model.ExecutionLog{
		TaskID: &task.ID, TaskName: task.Name,
		Status: model.StateSuccess, TriggerType: "cron", StartTime: now, EndTime: &endTime,
	})
	count, err = repo.CountRunningLogs(task.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "已完成日志不应计入 running 统计")
}

/*
🛡️ 【安全攻防·漏洞防线】
- 场景：调度节点 OOM 宕机，重启后存在一直处于 running 的幽灵任务（Orphaned Logs）。
- 机制：此清理功能是“自愈（Self-Healing）”的核心。如果不加防御地全部置为 Failed，可能会把其他健康节点的正在运行日志也错误判断。
- 进阶验证：在分布式集群中，这里通常要配合 heartbeat 租约时间，条件语句需带有 `updated_at < now - heartbeat_timeout` 及 `worker_node_id = me`。这里的测试应当能补充对边界时间（刚好超时 1ms 和未超时）的严格物理隔离验证。
*/
// TestGormLogRepository_CleanupOrphanedLogs 测试孤儿日志清理
func TestGormLogRepository_CleanupOrphanedLogs(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	task := model.Task{Name: "orphan-test", TaskType: "shell", Command: "echo", Enabled: true}
	db.Create(&task)

	// 插入孤儿 running 日志
	now := time.Now()
	db.Create(&model.ExecutionLog{
		TaskID: &task.ID, TaskName: task.Name,
		Status: model.StateRunning, TriggerType: "daemon", StartTime: now,
	})

	// 清理
	err := repo.CleanupOrphanedLogs(time.Now())
	require.NoError(t, err)

	// 验证被置为 failed
	// 💀 【踩坑血泪·反面教材】
	// 错误做法：测试中硬编码等待 `time.Sleep` 来模拟过期或运行状态，会导致测试极度缓慢甚至产生 Flaky Tests。
	// 正确做法：利用依赖注入，通过控制时间游标（比如 mock 的 time.Now 函数）实现精确的时间穿梭测试。
	var count int64
	db.Model(&model.ExecutionLog{}).Where("status = ?", model.StateRunning).Count(&count)
	assert.Equal(t, int64(0), count, "孤儿日志应被清理")

	var log model.ExecutionLog
	db.First(&log)
	assert.Equal(t, model.StateFailed, log.Status)
	assert.Equal(t, "System restarted or crashed", log.ErrorMsg)
	assert.NotNil(t, log.EndTime)
}

/*
🔬 【底层原理·深度剖析】
- MySQL 的 `ORDER BY ID DESC LIMIT 1` vs `ORDER BY StartTime DESC LIMIT 1`：
在单表自增 ID 时两者等价，按 ID 排序更快。但在分布式 ID（如雪花算法并发时序回拨）或跨分表场景下，依赖 ID 判断“最新”可能存在坑，最好配合精确的时间戳或逻辑时钟（Logical Clock）。测试这里通过并发写入验证最新一条的准确性是更健壮的做法。
*/
// TestGormLogRepository_GetLatestTaskLog 测试获取最新日志
func TestGormLogRepository_GetLatestTaskLog(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	task := model.Task{Name: "latest-test", TaskType: "shell", Command: "echo", Enabled: true}
	db.Create(&task)

	now := time.Now()
	// 插入两条日志
	db.Create(&model.ExecutionLog{
		TaskID: &task.ID, TaskName: task.Name,
		Status: model.StateSuccess, TriggerType: "cron", StartTime: now,
	})
	db.Create(&model.ExecutionLog{
		TaskID: &task.ID, TaskName: task.Name,
		Status: model.StateFailed, TriggerType: "cron", StartTime: now,
	})

	latest, err := repo.GetLatestTaskLog(task.ID)
	require.NoError(t, err)
	require.NotNil(t, latest, "应返回非 nil 的日志对象")
	assert.Equal(t, model.StateFailed, latest.Status, "应返回最新（ID 最大）的日志")
}

/*
🏗️ 【架构设计·模式对比】
- 清理策略选型：
  方案 A（当前同步删）：保留 N 条，超额 Delete。优点逻辑简单，缺点是高并发下如果有千万级历史数据，触发深分页查询+批量删除可能导致长事务锁表（Gap Lock），甚至引发从库同步延迟。
  方案 B（异步定期删）：后台任务批量按时间分区 Drop（Partitioning），或者 `DELETE ... LIMIT 1000` 循环。对于海量日志的大规模清理，方案 B 是标准解。
*/
// TestGormLogRepository_DeleteExcessTaskLogs 测试单任务超额日志清理
func TestGormLogRepository_DeleteExcessTaskLogs(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	task := model.Task{Name: "excess-test", TaskType: "shell", Command: "echo", Enabled: true}
	db.Create(&task)

	now := time.Now()
	// 插入 5 条日志
	// ⚡ 【性能实战·生产调优】
	// 压测数据：如果在 MySQL 中循环单条 INSERT 5 条，会有 5 次网络 RTT 和 5 次事务提交开销。
	// 生产中如果需要批量记录日志（如子任务派发），必须使用 `db.CreateInBatches` 批量写入，降低系统 QPS 压力。
	for i := 0; i < 5; i++ {
		db.Create(&model.ExecutionLog{
			TaskID: &task.ID, TaskName: task.Name,
			Status: model.StateSuccess, TriggerType: "cron", StartTime: now,
		})
	}

	// 保留 3 条，应删除 2 条
	err := repo.DeleteExcessTaskLogs(task.ID, 3)
	require.NoError(t, err)

	var count int64
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", task.ID).Count(&count)
	assert.Equal(t, int64(3), count, "应保留 3 条日志")
}

// TestGormLogRepository_GroupLogCRUD 测试组日志的创建和保存
func TestGormLogRepository_GroupLogCRUD(t *testing.T) {
	db := setupExecutorTestDB(t)
	repo := NewGormLogRepository(db)

	now := time.Now()
	glog := &model.GroupExecutionLog{
		GroupID:     1,
		GroupName:   "test-group",
		Mode:        "parallel",
		TriggerType: "cron",
		MemberCount: 3,
		Status:      model.StateRunning,
		StartTime:   now,
	}
	err := repo.CreateGroupLog(glog)
	require.NoError(t, err)
	assert.NotZero(t, glog.ID)

	endTime := time.Now()
	glog.Status = model.StateSuccess
	glog.EndTime = &endTime
	err = repo.SaveGroupLog(glog)
	require.NoError(t, err)

	var reloaded model.GroupExecutionLog
	db.First(&reloaded, glog.ID)
	assert.Equal(t, model.StateSuccess, reloaded.Status)
}
