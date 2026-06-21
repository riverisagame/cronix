package scheduler

import (
	"cronix/internal/domain/model"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/*
📌 【大厂面试·核心考点】
- 面试官问：异步日志系统在测试中面临哪些难点？
- 标准答案：并发资源竞争、异步转同步的等待验证、优雅关闭（Graceful Shutdown）时未排空（Drain）的数据丢失验证等。
- 面试官问：Go 中 channel 满载时的降级策略如何测试验证？
- 标准答案：通过构建容量受限（如 cap=2）且故意暂不启动消费者（Consumer协程）的 channel，强行制造生产溢出场景，来验证降级（Fallback）执行同步操作的逻辑。

🔬 【底层原理·深度剖析】
- **异步批处理与缓冲测试原理**：异步写系统通常由生产者（入队）、缓冲区（Channel）、消费者（循环刷盘协程）、定时器（Ticker）组成。单元测试需要精确验证生产者到消费者的完整闭环，尤其是并发场景下的数据一致性边界（如通道满、优雅关闭）。

🧪 【测试工程·质量保障】
- **物理零污染与隔离策略**：所有的测试代码均对 `mockLogRepo` 进行操作。此处的 Mock 设计彻底隔离了实际存储层（MySQL/PostgreSQL），我们只校验交互层面的行为，绝不在物理 DB 层产生任何 CREATE/DROP 或插入假数据的行为，保证了整个 DB 的绝对零污染。
*/

// 🛡️ 【安全攻防·漏洞防线】
// 在高并发场景的测试中，统计计数极易出现 Data Race（数据竞争）。
// 正确做法是使用 `sync/atomic` 包进行原子操作，或者使用 `sync.Mutex` 进行加锁保护。
// 💀 【踩坑血泪·反面教材】：直接使用 `m.saveCount++` 会导致竞态条件，一旦在 CI 环境执行 `go test -race` 会立刻报错阻断发布。
// mockLogRepo 是一个记录调用次数的 LogRepository mock
type mockLogRepo struct {
	saveCount int64
	logs      []*model.ExecutionLog
}

func (m *mockLogRepo) CreateExecutionLog(log *model.ExecutionLog) error { return nil }
func (m *mockLogRepo) SaveExecutionLog(log *model.ExecutionLog) error {
	atomic.AddInt64(&m.saveCount, 1)
	m.logs = append(m.logs, log)
	return nil
}
func (m *mockLogRepo) CountRunningLogs(taskID uint) (int64, error) { return 0, nil }
func (m *mockLogRepo) GetLatestTaskLog(taskID uint) (*model.ExecutionLog, error) {
	return nil, nil
}
func (m *mockLogRepo) CleanupOrphanedLogs(now time.Time) error              { return nil }
func (m *mockLogRepo) DeleteLogsBefore(cutoff time.Time) (int64, error)     { return 0, nil }
func (m *mockLogRepo) DeleteExcessLogs(maxRecords int) error                { return nil }
func (m *mockLogRepo) DeleteExcessTaskLogs(taskID uint, maxLogs int) error  { return nil }
func (m *mockLogRepo) CreateGroupLog(log *model.GroupExecutionLog) error    { return nil }
func (m *mockLogRepo) SaveGroupLog(log *model.GroupExecutionLog) error      { return nil }
func (m *mockLogRepo) DeleteGroupLogsBefore(cutoff time.Time) (int64, error) { return 0, nil }
func (m *mockLogRepo) DeleteExcessGroupLogs(maxRecords int) error           { return nil }

// ⚡ 【性能实战·生产调优】
// **异步批量刷盘（Batch Flush）机制测试**
// 在真实生产环境中，如果每一条日志都实时执行 DB 的 INSERT 操作，高并发调度下数据库的 IOPS 会迅速打满。
// 生活比喻：就像快递员（DB）送件，如果有一件就送一件（同步写），累死且效率极低；
// 聪明的做法是在快递站放一个大篮子（Channel 缓冲），等篮子满了或者过了半天（定时器 Ticker），装个三轮车一次性打包派送（批量写入）。
// 此测试验证的就是：数据放入 Channel 后，是否能在预设的延时时间后，由 Flusher 协程正确批量消费并写入 Repo。
// TestAsyncLogWriter_BatchFlush 测试批量刷盘机制
func TestAsyncLogWriter_BatchFlush(t *testing.T) {
	repo := &mockLogRepo{}
	writer := NewAsyncLogWriter(repo, 100)
	writer.Start()

	// 入队 10 条日志
	now := time.Now()
	for i := 0; i < 10; i++ {
		taskID := uint(1)
		writer.Enqueue(&model.ExecutionLog{
			ID:       uint(i + 1),
			TaskID:   &taskID,
			TaskName: "async-test",
			Status:   model.StateSuccess,
			EndTime:  &now,
		})
	}

	// 等待 flusher 自动刷盘（默认 200ms 间隔 + 缓冲）
	// 注意：在要求严苛的测试中，大量使用 time.Sleep() 被称为 Sleepy Test (反模式)，容易导致测试不稳定 (Flaky)。
	// 更好的做法是注入一个可控的 Clock，或者通过另一个 Channel 通知，但由于其内部逻辑使用了真实的 timer，故在此增加一定余量的睡眠等待。
	time.Sleep(500 * time.Millisecond)

	writer.Stop()

	count := atomic.LoadInt64(&repo.saveCount)
	assert.Equal(t, int64(10), count, "所有 10 条日志应通过批量刷盘写入")
}

// 💀 【踩坑血泪·反面教材】
// **优雅关闭（Graceful Shutdown）与通道排空（Drain）测试**
// 真实事故案例：某大厂的微服务在发版重启时，经常发现总是丢失部署前最后几秒的执行日志。
// 根本原因就是应用收到 SIGTERM 信号后，协程被强制 Kill 掉，导致内存 Channel 中积压的几百条日志随进程一同烟消云散。
// 修复与测试：`Stop()` 方法必须保证两件事：
// 1. 关闭接收入口，或者向内部发送停止信号；
// 2. 必须等待（Block）内部工作协程把 Channel 里的存货全部刷盘（Drain）后，才能释放阻塞，让主进程退出。
// TestAsyncLogWriter_GracefulShutdown 测试优雅关闭时排空 Channel
func TestAsyncLogWriter_GracefulShutdown(t *testing.T) {
	repo := &mockLogRepo{}
	writer := NewAsyncLogWriter(repo, 100)
	writer.Start()

	// 入队后立即 Stop（不等 flush ticker）
	// 这正是模拟重启或退出的瞬间情况，测试能否不丢任何哪怕刚进 Channel 的数据。
	now := time.Now()
	for i := 0; i < 5; i++ {
		taskID := uint(1)
		writer.Enqueue(&model.ExecutionLog{
			ID:       uint(i + 1),
			TaskID:   &taskID,
			TaskName: "shutdown-test",
			Status:   model.StateFailed,
			EndTime:  &now,
		})
	}

	writer.Stop()

	count := atomic.LoadInt64(&repo.saveCount)
	assert.Equal(t, int64(5), count, "Stop 应排空所有待写日志后再返回")
}

// 🏗️ 【架构设计·模式对比】
// **通道满载时的降级处理（Fallback to Synchronous）测试**
// 架构设计难点：当消费慢于生产，或者数据库卡顿导致 Channel 满了怎么办？
// - 方案 A（静默丢弃）：直接丢弃。适用于不重要的 Metrics 采集。
// - 方案 B（阻塞等待）：卡住生产者。适用于绝对不能丢失且能容忍高延迟的场景，但这可能拖死整个上游业务调用链。
// - 方案 C（降级同步）：退化为直接同步写库。这里采用的就是方案 C，虽然会拉长本次请求响应时间，但保证了该条日志不丢。
// 测试策略巧妙：故意给极其受限的容量（bufSize=2），并且**故意不启动消费者（不调用 Start）**，这会死死卡住 Channel，此时超出容量的日志应当立刻触发直接调用 repo 的同步回退逻辑。
// TestAsyncLogWriter_FullChannelFallback 测试通道满时降级为同步写入
func TestAsyncLogWriter_FullChannelFallback(t *testing.T) {
	repo := &mockLogRepo{}
	// 创建一个缓冲极小的 writer（bufSize=2）
	writer := NewAsyncLogWriter(repo, 2)
	// 故意不 Start()，让 channel 积满

	now := time.Now()
	// 入队 5 条，前 2 条进 channel，后 3 条应降级同步写
	for i := 0; i < 5; i++ {
		taskID := uint(1)
		writer.Enqueue(&model.ExecutionLog{
			ID:       uint(i + 1),
			TaskID:   &taskID,
			TaskName: "fallback-test",
			Status:   model.StateSuccess,
			EndTime:  &now,
		})
	}

	// 降级同步写应该产生 3 次直接调用
	count := atomic.LoadInt64(&repo.saveCount)
	assert.GreaterOrEqual(t, count, int64(3),
		"通道满时应降级为同步写入，至少 3 条应已同步写入")

	// 现在 Start + Stop 清理 channel 中剩余的
	writer.Start()
	writer.Stop()

	totalCount := atomic.LoadInt64(&repo.saveCount)
	assert.Equal(t, int64(5), totalCount, "最终所有 5 条日志都应写入")
}

// TestAsyncLogWriter_NewRequiresRepo 测试构造函数参数校验
func TestAsyncLogWriter_NewRequiresRepo(t *testing.T) {
	writer := NewAsyncLogWriter(nil, 100)
	require.NotNil(t, writer, "即使 repo 为 nil 也应返回非 nil 实例（由调用方保证）")
}
