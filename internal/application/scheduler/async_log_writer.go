// ============================================================
// internal/scheduler/async_log_writer.go - 异步批量日志写入器
//
// 【纳米级源码说明书 - 架构篇】
// 这里的角色是“记账小弟”（Flusher）。
// 车间主任（Executor）干完活，如果要自己一笔一划写在账本（数据库）上，会非常慢。
// 所以主任把账单扔进一个箱子（Channel），让记账小弟在后台慢慢写。
//
// 架构：
//   Executor.runTaskByTypeCtx → Enqueue(log) → 扔进 saveCh 通道
//                                               ↓
//                                     flusher goroutine (后台小弟)
//                                         (定时/批量)
//                                               ↓
//                                   logRepo.SaveExecutionLog (真正写库)
//
// ============================================================
// ============================================================
// 💡 【大厂面试·底层原理扩展：异步日志与高吞吐 (Async Logging)】
// 
// 1. 为什么无缓冲 Channel 会引发血案？
// 面试官问：这里的 saveCh 如果不设置 bufSize（变成无缓冲），会怎样？
// 答：无缓冲 Channel 必须在发送方和接收方同时准备好时才能传递数据。这就意味着，“车间主任”把账单递过去的瞬间，
// 必须等“记账小弟”伸出手来接（同步阻塞）。在日志洪峰（如 10000 QPS）到来时，数据库写入哪怕只是卡顿了 50 毫秒，
// 就会导致记账小弟卡住，进而让所有拿着新账单的车间主任全部排队阻塞在 `w.saveCh <- execLog`，最终拖垮整个系统的主业务逻辑。
//
// 2. 什么是背压（Backpressure）机制？
// 面试官问：如果那个收银箱（Buffered Channel）满了塞不下怎么办？
// 答：看下方 `Enqueue()` 方法里的神级操作。大厂架构中，下游如果处理不过来，绝对不能把上游给卡死（这叫背压反噬）。
// 当 Channel 满了（触发 default 分支），我们采用“丢弃”或“降级同步写入”策略。在这里，我们选择让车间主任亲自跑一趟银行
// （降级为同步阻塞写库）。虽然这会让单个任务的退出变慢，但保证了已经堆积在内存里的账单不会全部因为 OOM 丢失。
//
// 3. 优雅关机（Graceful Shutdown）与僵尸进程
// 面试官问：如果在 K8s 里容器被缩容收到了 SIGTERM 信号，收银箱里还没写到硬盘的钱不就灰飞烟灭了吗？
// 答：看 `Stop()` 方法。我们不使用粗暴的 `os.Exit(0)`，而是触发关闭信号 `close(w.done)`。
// 后台协程（Flusher）监听到口哨后，会通过特殊的 for-select 结构，继续把通道底部的最后一条账单全部榨干刷盘，
// 然后调用 `wg.Done()`。主协程在 `wg.Wait()` 处被唤醒后，才真正退出进程。这叫“不流失一条数据的从容退场”。
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

// AsyncLogWriter 异步批量日志写入器（记账小弟的装备箱）
type AsyncLogWriter struct {
	logRepo       LogRepository           // 底层存储接口（去哪家银行存钱）
	
	// 【大厂考点：带缓冲的通道 Buffered Channel】
	// 这就是上面说的“收银箱”。它可以暂存若干个账本，主任往里面丢，小弟从里面拿。
	saveCh        chan *model.ExecutionLog 
	
	// 关闭信号：老板说“下班了”，通过这个口哨吹一下。
	done          chan struct{}            
	
	// 【大厂考点：sync.WaitGroup】
	// 等待小弟退出。老板下班不能自己先走，得等所有小弟都打卡下班了，才能关门。
	wg            sync.WaitGroup          
	
	flushInterval time.Duration           // 刷盘间隔：多久跑一次银行（比如每 200 毫秒跑一次）
	batchSize     int                     // 单次批量上限：攒够多少张账单跑一次银行（比如 50 张）
}

// NewAsyncLogWriter 招募一个新的记账小弟
// bufSize: Channel 缓冲大小（收银箱能装多少张账单，推荐 100-1000）
func NewAsyncLogWriter(repo LogRepository, bufSize int) *AsyncLogWriter {
	if bufSize <= 0 {
		bufSize = 100 // 如果没指定大小，默认买个能装 100 张账单的箱子。
	}
	return &AsyncLogWriter{
		logRepo:       repo,
		saveCh:        make(chan *model.ExecutionLog, bufSize),
		done:          make(chan struct{}),
		flushInterval: 200 * time.Millisecond,
		batchSize:     50,
	}
}

// Start 启动后台 flusher goroutine（让小弟开始上班）
func (w *AsyncLogWriter) Start() {
	w.wg.Add(1) // 老板拿着花名册说：“记好，今天有一个人来上班了”。
	go w.flusher() // go 关键字就是派他去另一个空间默默干活。
}

// Enqueue 将日志入队等待异步写入
// 如果 Channel 已满，降级为同步写入（绝不丢日志！）
func (w *AsyncLogWriter) Enqueue(execLog *model.ExecutionLog) {
	// 【Go 语言神级语法：select 的非阻塞投递】
	select {
	case w.saveCh <- execLog:
		// 成功扔进收银箱，收工！
	default:
		// 箱子满了！扔不进去了！
		// 触发安全兜底：主任亲自跑一趟银行，直接同步写入数据库。
		log.Warn().Msg("异步日志通道已满，降级为同步写入")
		if w.logRepo != nil {
			if err := w.logRepo.SaveExecutionLog(execLog); err != nil {
				log.Error().Err(err).Msg("同步降级写入日志失败")
			}
		}
	}
}

// Stop 排空队列并关闭 flusher
// 阻塞直到所有待写日志已刷盘（优雅关机）
func (w *AsyncLogWriter) Stop() {
	close(w.done) // 吹响下班口哨
	w.wg.Wait()   // 老板坐等，直到小弟报到说：“老板，最后一张账单存完了，我下班了！”
}

// flusher 后台循环：定时或批量收集日志并写入（小弟的工作日常）
func (w *AsyncLogWriter) flusher() {
	// 下班打卡：不管中间发生了什么（哪怕小弟心脏病突发 panic 了），
	// 最后都会执行 Done()，告诉老板他走了。
	defer w.wg.Done()

	// 这是一个定时闹钟。每过 flushInterval（200毫秒）响一次。
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop() // 小弟下班走之前，把闹钟电池拔了，别让它继续瞎响。

	// 小弟随身带的小背包，专门用来把零散账单攒成一叠。
	// make([]..., 0, w.batchSize) 这种写法可以提前分配好背包空间，避免频繁换包（内存分配）。
	batch := make([]*model.ExecutionLog, 0, w.batchSize)

	for { // 无限循环，每天如此
		select {
		case execLog := <-w.saveCh:
			// 1. 从收银箱拿到了新账单！
			batch = append(batch, execLog) // 塞进小背包
			
			// 达到批量上限时（背包装满了 50 张！），立即跑一趟银行刷盘。
			if len(batch) >= w.batchSize {
				w.flushBatch(batch) // 跑去存钱
				batch = batch[:0]   // 存完了，把小背包清空（这种写法复用了原来的内存空间，极其高效！）
			}

		case <-ticker.C:
			// 2. 定时闹钟响了！（虽然包还没装满，但也得去存一次钱了，不能留在手里过夜）
			if len(batch) > 0 {
				w.flushBatch(batch)
				batch = batch[:0]
			}

		case <-w.done:
			// 3. 听到了老板吹响的下班口哨！
			// 不能马上跑，得看看收银箱（Channel）里还有没有钱。
			for {
				select {
				case execLog := <-w.saveCh:
					batch = append(batch, execLog) // 把箱底的钱掏出来装包里
				default:
					// 箱子彻底空了
					if len(batch) > 0 {
						w.flushBatch(batch) // 去银行存最后一波
					}
					return // 彻底下班回家！
				}
			}
		}
	}
}

// flushBatch 将一批日志逐条写入底层存储（去银行存钱的动作）
// 因为每条日志的 ID 不同，是 UPDATE 操作，无法使用 CreateInBatches（GORM不支持批量Update不同记录）。
func (w *AsyncLogWriter) flushBatch(batch []*model.ExecutionLog) {
	if w.logRepo == nil {
		return
	}
	// 小弟一张一张把账单递给柜员
	for _, execLog := range batch {
		if err := w.logRepo.SaveExecutionLog(execLog); err != nil {
			log.Error().Err(err).Uint("log_id", execLog.ID).Msg("异步刷盘写入日志失败")
		}
	}
}

