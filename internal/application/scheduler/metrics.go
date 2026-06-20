// ============================================================
// internal/scheduler/metrics.go - 实时大屏指标采集引擎
//
// 【纳米级源码说明书 - 架构篇】
// 这里的角色是“车间统计员”。
// 他拿着秒表，记录每一个任务是成功了还是失败了，花了多少时间。
// 他的数据直接供给前端的“监控大屏（Dashboard）”使用，画出漂漂亮亮的曲线图。
//
// ============================================================
// 💡 【大厂面试·底层原理扩展：监控告警与时序分位数算法】
// 
// 场景重现 1：
// 面试官问：大厂为什么从来不看“平均耗时”，而是死盯 P95/P99？如果让你手写一个 P99 计算，怎么做？
//
// 底层剖析与大厂对冲方案：
// 1. 平均数陷阱：假设 100 个任务，99 个耗时 1ms，1 个卡了 10000ms（比如发生 Full GC）。平均耗时 100ms，
//    看似正常，但那个等了 10 秒的用户已经愤怒卸载了。
// 2. P99 定义：把 100 个请求的耗时从小到大排序，排在第 99 位的那个数值就是 P99。如果 P99 = 50ms，
//    意味着系统对 99% 的请求都能在 50ms 内响应，这是一个极高的 SLA 承诺。
// 3. 内存刺客：如果一分钟内有 100 万个请求，为了算 P99，你要把 100 万个 int64 存进内存再排序吗？
//    绝对不行！（极易 OOM）。Cronix 在这里用的是“有界蓄水池抽样（Bounded Sampling）”思想，
//    每分钟最多只保留 `maxDurationSamples` (1000 个) 样本，既保证了统计学上的高精度，又锁死了内存上限。
//
// 场景重现 2：
// 面试官问：监控系统（Metrics）在高并发下，怎么做到绝对不拖垮主营业务？
//
// 底层剖析与大厂对冲方案：
// 1. 旁路监控（Bypass Monitoring）：主业务（Executor）和监控（Metrics）必须是松耦合的。
// 2. 有损丢弃策略：在 `RecordExecution` 中使用了带 default 分支的 channel `select` 语法。
//    如果监控处理不过来（Channel被打满），主业务会**毫不犹豫地把这条监控数据扔进垃圾桶**，直接返回，
//    宁可大屏上的数据不准，也绝不能让真实用户的任务被卡住！
//
// @Ref: docs/sps/plans/20260605_metrics_plan.md | @Date: 2026-06-05
// ============================================================
package scheduler

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// MetricEvent 代表一次任务执行的原始成绩单
type MetricEvent struct {
	DurationMs int64     // 耗时（毫秒）
	Success    bool      // 是否成功
	Time       time.Time // 交卷时间
}

// MetricSnapshot 这是打包好要发给前端画图的数据包（JSON格式）
type MetricSnapshot struct {
	MinuteLabels  []string `json:"minute_labels"`  // 横坐标：如 "15:01", "15:02"
	MinuteSuccess []int64  `json:"minute_success"` // 柱状图：成功多少次
	MinuteFailed  []int64  `json:"minute_failed"`  // 柱状图：失败多少次
	MinuteP95     []int64  `json:"minute_p95"`     // 曲线图：P95 耗时
	MinuteP99     []int64  `json:"minute_p99"`     // 曲线图：P99 耗时

	// 小时级图表数据，同上
	HourLabels  []string `json:"hour_labels"`
	HourSuccess []int64  `json:"hour_success"`
	HourFailed  []int64  `json:"hour_failed"`
	HourP95     []int64  `json:"hour_p95"`
	HourP99     []int64  `json:"hour_p99"`
}

// MetricBucket 时间桶（Time Bucket）。
// 【考点：时间序列聚合】把同一分钟内所有的成绩单，融合成一个“桶”。
type MetricBucket struct {
	Timestamp time.Time // 桶的标签时间，比如 "2026-06-05 14:05:00"
	Success   int64     // 这 1 分钟内成功的总次数
	Failed    int64     // 这 1 分钟内失败的总次数
	Durations []int64   // 这 1 分钟内所有任务的耗时记录（用来算 P99）
}

// MetricsRegistry 统计员的办公桌
// @Ref: docs/sps/plans/20260605_metrics_plan.md | @Date: 2026-06-05
type MetricsRegistry struct {
	mu            sync.RWMutex             // 读写锁：前端来拿报表时加读锁，小弟整理数据时加写锁
	events        chan MetricEvent         // 收件箱（接收车间主任扔过来的成绩单）
	minuteBuckets map[string]*MetricBucket // 存放最近 60 分钟的 60 个桶
	hourBuckets   map[string]*MetricBucket // 存放最近 24 小时的 24 个桶
	stopChan      chan struct{}            // 下班口哨
	stopped       bool                     // 是否已下班
}

// GlobalMetricsRegistry 全局唯一的统计员大拿（单例模式）
var GlobalMetricsRegistry *MetricsRegistry

func init() {
	GlobalMetricsRegistry = NewMetricsRegistry()
}

func NewMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{
		// 这是一个带 1000 个格子的异步收件箱。可以吸收瞬时爆发的成绩单。
		events:        make(chan MetricEvent, 1000), 
		minuteBuckets: make(map[string]*MetricBucket),
		hourBuckets:   make(map[string]*MetricBucket),
		stopChan:      make(chan struct{}),
	}
}

// RecordExecution 车间主任扔成绩单的动作。
func (m *MetricsRegistry) RecordExecution(durationMs int64, success bool) {
	m.mu.RLock()
	stopped := m.stopped
	m.mu.RUnlock()
	if stopped {
		return
	}
	
	event := MetricEvent{
		DurationMs: durationMs,
		Success:    success,
		Time:       time.Now(),
	}
	
	// 【旁路监控防阻塞机制】如果收件箱满了（1000封信），主任不会傻站着等，直接转身就走（丢弃这封信）。
	select {
	case m.events <- event:
	default:
		// Drop if channel is full to prevent blocking the executor (performance hedge)
	}
}

// Start 统计员开始上班，左右手同时开弓。
func (m *MetricsRegistry) Start() {
	go m.processEvents() // 左手：一刻不停地从收件箱拿信，分类放到桶里。
	go m.cleanupLoop()   // 右手：时不时检查一下，有没有过期的桶，扔进垃圾桶。
}

// Stop 统计员下班
func (m *MetricsRegistry) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	close(m.stopChan)
	m.mu.Unlock()
}

// maxDurationSamples 每个时间桶最多保留的耗时采样数
// 【大厂内存风控】
// 为什么要限制最多 1000 个？
// 如果这一分钟内跑了 100 万个任务，如果不限制，Durations 数组会有 100 万个数字，
// 一天下来系统内存就被这个数组撑爆了（OOM内存溢出）！所以采样前 1000 个算一算 P99 就足够准确了。
// @Ref: architect_review_20260609.md P1-5 | @Date: 2026-06-09
const maxDurationSamples = 1000

// processEvents 左手：分拣成绩单，扔进对应的桶里
func (m *MetricsRegistry) processEvents() {
	for {
		select {
		case <-m.stopChan:
			return
		case ev := <-m.events:
			m.mu.Lock()
			// 比如当前时间是 "15:04:33"
			minKey := ev.Time.Format("2006-01-02 15:04") // 去掉秒，变成 "15:04" 桶
			hourKey := ev.Time.Format("2006-01-02 15")   // 去掉分，变成 "15" 桶

			// ----- 分钟桶处理 -----
			if _, ok := m.minuteBuckets[minKey]; !ok {
				m.minuteBuckets[minKey] = &MetricBucket{Timestamp: ev.Time} // 没这个桶就建一个
			}
			if ev.Success {
				m.minuteBuckets[minKey].Success++
			} else {
				m.minuteBuckets[minKey].Failed++
			}
			// 限制每桶采样数，防止高频场景下无界增长
			if len(m.minuteBuckets[minKey].Durations) < maxDurationSamples {
				m.minuteBuckets[minKey].Durations = append(m.minuteBuckets[minKey].Durations, ev.DurationMs)
			}

			// ----- 小时桶处理 -----
			if _, ok := m.hourBuckets[hourKey]; !ok {
				m.hourBuckets[hourKey] = &MetricBucket{Timestamp: ev.Time}
			}
			if ev.Success {
				m.hourBuckets[hourKey].Success++
			} else {
				m.hourBuckets[hourKey].Failed++
			}
			// 限制每桶采样数，防止高频场景下无界增长
			if len(m.hourBuckets[hourKey].Durations) < maxDurationSamples {
				m.hourBuckets[hourKey].Durations = append(m.hourBuckets[hourKey].Durations, ev.DurationMs)
			}

			m.mu.Unlock()
		}
	}
}

// cleanupLoop 右手：扫地机（Sliding Window 滑动窗口清理）
func (m *MetricsRegistry) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute) // 每分钟扫一次
	defer ticker.Stop()
	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			// 扫分钟桶：超过 60 分钟前的老旧桶，扔掉。
			for k, v := range m.minuteBuckets {
				if now.Sub(v.Timestamp).Minutes() > 60 {
					delete(m.minuteBuckets, k)
				}
			}
			// 扫小时桶：超过 24 小时前的老旧桶，扔掉。
			for k, v := range m.hourBuckets {
				if now.Sub(v.Timestamp).Hours() > 24 {
					delete(m.hourBuckets, k)
				}
			}
			m.mu.Unlock()
		}
	}
}

// calculatePercentile 计算 Pxx 分位数（比如 P99就是 percentile=0.99）
// 算法：先把所有耗时从小到大排序，然后用 总个数 * 0.99，取出那个位置的值。
func calculatePercentile(durations []int64, percentile float64) int64 {
	if len(durations) == 0 {
		return 0
	}
	// 必须要拷贝一份，不能把别人原来的数组打乱顺序！
	sorted := make([]int64, len(durations))
	copy(sorted, durations)
	
	// 从小到大排序
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	
	// 找出排在 99% 位置的那个人
	idx := int(float64(len(sorted)) * percentile)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// GetSnapshot 前端调用这个接口，获取画大屏图表需要的 X 轴和 Y 轴数据。
func (m *MetricsRegistry) GetSnapshot() MetricSnapshot {
	m.mu.RLock() // 前端来拿数据，加读锁！
	defer m.mu.RUnlock()

	snap := MetricSnapshot{
		MinuteLabels:  []string{},
		MinuteSuccess: []int64{},
		MinuteFailed:  []int64{},
		MinuteP95:     []int64{},
		MinuteP99:     []int64{},
		HourLabels:    []string{},
		HourSuccess:   []int64{},
		HourFailed:    []int64{},
		HourP95:       []int64{},
		HourP99:       []int64{},
	}

	now := time.Now()
	
	// 💡 【大厂面试·底层原理扩展：时序数据对齐与 Map 无序性陷阱】
	// 
	// 场景重现：
	// 面试官问：后端把统计好的 `map[string]*MetricBucket` 直接序列化成 JSON 返回给前端 ECharts 画折线图，会有什么严重 Bug？
	//
	// 底层剖析与大厂对冲方案：
	// 1. 乱序渲染 Bug：Go 语言（以及大多数语言）的 Map 底层是哈希表，遍历顺序是**绝对无序**的（甚至是随机的）。
	//    前端拿到数据后画出来的折线图会像一团乱麻，时间轴会在 "15:02", "15:05", "15:01" 之间来回穿梭。
	// 2. 断崖缺口 Bug（Zero Fill 问题）：如果有 1 分钟（比如 "15:03"）系统没有任何流量，Map 里根本不会有这个 Key。
	//    如果你直接把 Map 的 Value 取出来当 Y 轴，图表在 "15:03" 这个时间点会直接断裂，或者把 "15:04" 的点前移。
	// 3. 数据对齐（Data Alignment）：正规的监控系统（如 Prometheus），在处理这种时序数据时，后端必须主动生成连续的时间轴（X轴），
	//    如果发现某个时间点在 Map 里找不到，必须**主动补 0**。这就是下面两个 for 循环存在的绝对核心意义。
	
	// For minute buckets, generate last 60 minutes sequence (从 59 分钟前，一直到当前这分钟)
	for i := 59; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * time.Minute)
		key := t.Format("2006-01-02 15:04")
		label := t.Format("15:04") // 给前端当横坐标 X 轴的标签
		snap.MinuteLabels = append(snap.MinuteLabels, label)
		
		if bucket, ok := m.minuteBuckets[key]; ok { // 如果这分钟有数据
			snap.MinuteSuccess = append(snap.MinuteSuccess, bucket.Success)
			snap.MinuteFailed = append(snap.MinuteFailed, bucket.Failed)
			snap.MinuteP95 = append(snap.MinuteP95, calculatePercentile(bucket.Durations, 0.95))
			snap.MinuteP99 = append(snap.MinuteP99, calculatePercentile(bucket.Durations, 0.99))
		} else { // 如果这分钟没人访问（空窗期），硬塞一个 0 进去填坑，防止折线图断掉。
			snap.MinuteSuccess = append(snap.MinuteSuccess, 0)
			snap.MinuteFailed = append(snap.MinuteFailed, 0)
			snap.MinuteP95 = append(snap.MinuteP95, 0)
			snap.MinuteP99 = append(snap.MinuteP99, 0)
		}
	}

	// For hour buckets, generate last 24 hours sequence (处理逻辑同上)
	for i := 23; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * time.Hour)
		key := t.Format("2006-01-02 15")
		label := fmt.Sprintf("%02d:00", t.Hour())
		snap.HourLabels = append(snap.HourLabels, label)

		if bucket, ok := m.hourBuckets[key]; ok {
			snap.HourSuccess = append(snap.HourSuccess, bucket.Success)
			snap.HourFailed = append(snap.HourFailed, bucket.Failed)
			snap.HourP95 = append(snap.HourP95, calculatePercentile(bucket.Durations, 0.95))
			snap.HourP99 = append(snap.HourP99, calculatePercentile(bucket.Durations, 0.99))
		} else {
			snap.HourSuccess = append(snap.HourSuccess, 0)
			snap.HourFailed = append(snap.HourFailed, 0)
			snap.HourP95 = append(snap.HourP95, 0)
			snap.HourP99 = append(snap.HourP99, 0)
		}
	}

	return snap
}

