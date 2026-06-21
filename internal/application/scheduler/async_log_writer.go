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
// 🏗️ 【架构设计·模式对比】
// 1. 同步直写（Sync Write）：业务线程直接调用 DB 写日志。优点是逻辑简单、数据不丢；缺点是 DB I/O 阻塞会直接卡死业务线，无法应对高并发。
// 2. 内存通道异步写（Channel Async）：即本架构。通过 Go Channel 解耦业务和 I/O，平滑写请求。当 DB 压力大时，Channel 提供缓冲。缺点是进程崩溃可能丢失少量缓冲数据。
// 3. 引入外部 MQ（Kafka/RabbitMQ）：极致的高可用。优点是进程挂了不丢数据，缺点是架构变重，运维成本高，网络 I/O 也有延迟。
// 4. 高性能环形队列（Ring Buffer）：如 Disruptor/go-ringbuffer。极致的无锁并发性能，避免 Channel 底层互斥锁的竞争。本场景下目前 Channel 性能已达标，暂不引入。
//
// 🔬 【底层原理·深度剖析】磁盘 IO 瓶颈与异步落盘
// 为什么异步写能大幅提升性能？
// 操作系统层面，磁盘 I/O（无论是 HDD 还是 SSD）通常比内存操作慢 3 到 6 个数量级。
// 如果业务逻辑（CPU/Memory）必须等待磁盘 I/O（阻塞），会导致 CPU 资源大量闲置，系统吞吐量急剧下降。
// 将“写库/写盘”动作移至独立 goroutine（后台队列模式）中执行，使得主业务流只进行内存级别的耗时操作（几十纳秒），
// 极大地释放了线程和协程资源，能够支撑万级以上的并发。
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
	// ⚡ 【性能实战·生产调优】：bufSize 大小权衡
	// 设得太大（如 100万）：一旦进程 OOM 崩溃，留在管道里未落盘的数据将全部丢失，而且占用巨大内存。
	// 设得太小（如 10）：稍有网络抖动导致数据库变慢，管道瞬间爆满，引发缓冲堆积问题，触发降级逻辑，使异步失去意义。
	// 实战建议值：预估 QPS * 可容忍的数据库最大延迟秒数，通常在 1000 - 10000 左右。
	saveCh        chan *model.ExecutionLog 
	
	// 关闭信号：老板说“下班了”，通过这个口哨吹一下。
	done          chan struct{}            
	
	// 【大厂考点：sync.WaitGroup】
	// 等待小弟退出。老板下班不能自己先走，得等所有小弟都打卡下班了，才能关门。
	// 🔬 【底层原理·深度剖析】：基于操作系统的信号量（Semaphore）实现，当计数器归零时唤醒所有调用 Wait() 阻塞的线程。
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
// 🛡️ 【安全攻防·漏洞防线】雪崩防御
// 如果不对高并发流量进行管控和削峰，极端突发请求会把系统资源（内存/DB连接数）瞬间耗干，引发雪崩。
// 本处的 select + default 是实现系统自保护的基石。在消息缓冲堆积严重时，强制限制上游处理速度（转为同步），从而形成系统级别的“背压”。
func (w *AsyncLogWriter) Enqueue(execLog *model.ExecutionLog) {
	// 【Go 语言神级语法：select 的非阻塞投递】
	// 💀 【踩坑血泪·反面教材】Channel 缓冲堆积引发的死锁
	// 初级工程师常犯错误：直接写 `w.saveCh <- execLog`，没有 default 分支。
	// 这会导致一旦 DB 挂了写入变慢，Channel 满后产生严重的缓冲堆积，所有调用 `Enqueue` 的外层业务 Goroutine 全部死锁阻塞！
	// 后果：应用服务 Goroutine 暴增至十万级别，最终 OOM 崩溃。
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
// 🧪 【测试工程·质量保障】优雅停机测试
// 测试建议：可在这个方法内 Mock 注入一个短时间的 Wait，模拟真实的落盘延迟。然后验证在发出 SIGINT 后，主程序是否真正等待了协程完结。
// 常见验收方式：利用 `kill -SIGTERM` 给容器发送信号，查看最后几条日志是否完整在 DB 中出现。
func (w *AsyncLogWriter) Stop() {
	close(w.done) // 吹响下班口哨
	w.wg.Wait()   // 老板坐等，直到小弟报到说：“老板，最后一张账单存完了，我下班了！”
}

// flusher 后台循环：定时或批量收集日志并写入（小弟的工作日常）
// 📌 【大厂面试·核心考点】Go 里的 Timer 内存泄漏陷阱
// 面试官问：如果没写 defer ticker.Stop() 会有什么后果？
// 答：ticker.C 是一个不断会收到时间的 Channel。如果不 Stop，其内部关联的 timer 会一直保留在 Go 运行时的四叉堆里，
// 导致这块内存和底层的时间调度永远无法被垃圾回收机制（GC）释放，久而久之会导致严重的内存泄漏（Memory Leak）。
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
				
				// ⚡ 【性能实战·生产调优】切片重置 (Slice Reuse)
				// batch = batch[:0] 是 Go 的经典性能优化技巧：它将切片长度设为 0，但保留了底层数组的容量（capacity）。
				// 这样下一次 append 不用重新在堆（Heap）上申请内存，彻底消灭了因小对象分配带来的 GC 停顿（STW）压力。
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
			// 🔬 【底层原理·深度剖析】For-Select 排空管道设计
			// 在外层 for 循环监听到 done 信号后，进入这里的内层无尽循环。
			// 此时只关注 saveCh 中残留的数据。配合 default 分支，它会以非阻塞的方式瞬间将缓冲堆积的管道内残余的数据全部取出装包。
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
//
// 💀 【踩坑血泪·反面教材】批量 Update 导致的事务耗时和死锁
// 若未来需要改成批量更新（如使用 `CASE WHEN` 拼接的长 SQL），需注意：
// 大批量操作会导致数据库表/行级锁的时间大幅拉长（MySQL InnoDB 中更新同一页的数据会导致页级锁开销），
// 极易引发在高并发下的死锁（Deadlock）。当前架构采用遍历执行，以略微牺牲吞吐换取极低的锁冲突风险。
// 建议在真实生产中，使用 ClickHouse/ES 等专门的列式/检索数据库异步处理这种日志，而不是传统关系型 DB。
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

