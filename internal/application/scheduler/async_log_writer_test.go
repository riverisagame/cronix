package scheduler

import (
	"cronix/internal/domain/model"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	time.Sleep(500 * time.Millisecond)

	writer.Stop()

	count := atomic.LoadInt64(&repo.saveCount)
	assert.Equal(t, int64(10), count, "所有 10 条日志应通过批量刷盘写入")
}

// TestAsyncLogWriter_GracefulShutdown 测试优雅关闭时排空 Channel
func TestAsyncLogWriter_GracefulShutdown(t *testing.T) {
	repo := &mockLogRepo{}
	writer := NewAsyncLogWriter(repo, 100)
	writer.Start()

	// 入队后立即 Stop（不等 flush ticker）
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
