// ============================================================
// internal/scheduler/async_log_writer.go - 异步批量日志写入器
//
// 将执行日志的最终 Save 操作从同步阻塞改为异步缓冲+定时批量写入，
// 解放 SQLite 单连接的串行 I/O 瓶颈。
//
// 架构：
//   Executor.runTaskByTypeCtx → Enqueue(log) → saveCh
//                                               ↓
//                                     flusher goroutine
//                                         (定时/批量)
//                                               ↓
//                                   logRepo.SaveExecutionLog
//
// 安全保障：
//   1. Channel 满时自动降级为同步写入（绝不丢日志）
//   2. Stop() 先排空 Channel 再返回（优雅关闭）
//
// @Ref: docs/sps/plans/20260612_arch_hardening_plan.md | @Date: 2026-06-12
// ============================================================
package scheduler

import (
	"cronix/internal/domain/model"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// AsyncLogWriter 异步批量日志写入器
type AsyncLogWriter struct {
	logRepo       LogRepository           // 底层存储接口
	saveCh        chan *model.ExecutionLog // 缓冲通道
	done          chan struct{}            // 关闭信号
	wg            sync.WaitGroup          // 等待 flusher 退出
	flushInterval time.Duration           // 刷盘间隔
	batchSize     int                     // 单次批量上限
}

// NewAsyncLogWriter 创建异步日志写入器
// bufSize: Channel 缓冲大小（推荐 100-1000）
func NewAsyncLogWriter(repo LogRepository, bufSize int) *AsyncLogWriter {
	if bufSize <= 0 {
		bufSize = 100
	}
	return &AsyncLogWriter{
		logRepo:       repo,
		saveCh:        make(chan *model.ExecutionLog, bufSize),
		done:          make(chan struct{}),
		flushInterval: 200 * time.Millisecond,
		batchSize:     50,
	}
}

// Start 启动后台 flusher goroutine
func (w *AsyncLogWriter) Start() {
	w.wg.Add(1)
	go w.flusher()
}

// Enqueue 将日志入队等待异步写入
// 如果 Channel 已满，降级为同步写入（绝不丢日志）
func (w *AsyncLogWriter) Enqueue(execLog *model.ExecutionLog) {
	select {
	case w.saveCh <- execLog:
		// 成功入队，等待 flusher 批量写入
	default:
		// Channel 满，降级同步写入
		log.Warn().Msg("异步日志通道已满，降级为同步写入")
		if w.logRepo != nil {
			if err := w.logRepo.SaveExecutionLog(execLog); err != nil {
				log.Error().Err(err).Msg("同步降级写入日志失败")
			}
		}
	}
}

// Stop 排空队列并关闭 flusher
// 阻塞直到所有待写日志已刷盘
func (w *AsyncLogWriter) Stop() {
	close(w.done)
	w.wg.Wait()
}

// flusher 后台循环：定时或批量收集日志并写入
func (w *AsyncLogWriter) flusher() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	batch := make([]*model.ExecutionLog, 0, w.batchSize)

	for {
		select {
		case execLog := <-w.saveCh:
			batch = append(batch, execLog)
			// 达到批量上限时立即刷盘
			if len(batch) >= w.batchSize {
				w.flushBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			// 定时刷盘
			if len(batch) > 0 {
				w.flushBatch(batch)
				batch = batch[:0]
			}

		case <-w.done:
			// 收到关闭信号：排空 Channel 中的剩余日志
			for {
				select {
				case execLog := <-w.saveCh:
					batch = append(batch, execLog)
				default:
					// Channel 已空
					if len(batch) > 0 {
						w.flushBatch(batch)
					}
					return
				}
			}
		}
	}
}

// flushBatch 将一批日志逐条写入底层存储
// 因为每条日志的 ID 不同，是 UPDATE 操作，无法使用 CreateInBatches
func (w *AsyncLogWriter) flushBatch(batch []*model.ExecutionLog) {
	if w.logRepo == nil {
		return
	}
	for _, execLog := range batch {
		if err := w.logRepo.SaveExecutionLog(execLog); err != nil {
			log.Error().Err(err).Uint("log_id", execLog.ID).Msg("异步刷盘写入日志失败")
		}
	}
}
