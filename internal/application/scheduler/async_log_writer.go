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
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：为什么要用异步批量写入？直接写数据库不好吗？
// 答（小白比喻）：
// 假设双十一爆单，你开个小卖部，每卖出一包辣条就马上跑到银行去存一次钱（同步写数据库）。
// 你肯定会被累死，而且路上排队很久（I/O 阻塞）。
// 更好的办法是：准备一个收银箱（Channel），卖了钱先扔进去。
// 等晚上打烊了（定时），或者钱箱装满了100块（定量），再一口气跑一次银行存掉（批量刷盘）。
// 这样效率提升 100 倍！这就是这套代码的核心精髓！
//
// 2. 面试官问：如果那个收银箱（Channel）满了塞不下了怎么办？会丢账本吗？
// 答：
// 绝对不丢！看 Enqueue() 方法里的神级操作。如果 Channel 满了（default 分支触发），
// 车间主任就会叹口气说：“哎，小弟干活太慢了，我自己跑一趟银行吧！”（降级为同步直接写库）。
// 
// 3. 面试官问：如果系统突然关机，收银箱里还有没存银行的钱怎么办？
// 答：
// 看 Stop() 方法。它使用了优雅关机（Graceful Shutdown）。系统关机前，
// 必须等小弟把箱子里的最后一块钱存进银行（wg.Wait()），程序才能真正关闭退出。
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

