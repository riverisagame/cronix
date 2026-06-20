// ============================================================
// internal/scheduler/metrics.go - 实时大屏指标采集引擎
//
// 【纳米级源码说明书 - 架构篇】
// 这里的角色是“车间统计员”。
// 他拿着秒表，记录每一个任务是成功了还是失败了，花了多少时间。
// 他的数据直接供给前端的“监控大屏（Dashboard）”使用，画出漂漂亮亮的曲线图。
//
// ============================================================
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：什么是 P95 和 P99 延迟？为什么要算这个？
// 答（小白比喻）：
// 假设班里 100 个人考试，平均分是 80 分。你觉得这班成绩不错。
// 但其实有 90 个人考了 100 分，剩下 10 个人考了 0 分！平均数会骗人。
// P99 耗时，就是把这 100 个人按交卷时间从快到慢排好队，站在第 99 个位置的那个人交卷用的时间。
// 如果 P99=2秒，意思是：100次任务里，有 99 次都是在 2 秒内完成的。
// 大厂不看“平均耗时”，只看 P99 耗时，这代表了系统给绝大多数用户的真实体验。
//
// 2. 面试官问：在高并发下，统计员（Metrics）怎么做到不拖慢车间主任（Executor）干活的速度？
// 答：
// 看 RecordExecution 方法。它也用到了【无阻塞 Channel】。
// 统计员如果因为算数算慢了卡住了（Channel满），车间主任直接把秒表记录扔进垃圾桶（丢弃），
// 绝不停下来等他。监控数据丢一两条无所谓，但绝不能影响主营业务！这叫【旁路监控（Bypass Monitoring）】。
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
	
	// 【大厂面试考点：前端画图为什么不能直接返回 Map？】
	// 如果你把上面的 map[string]*MetricBucket 直接序列化成 JSON 给前端，
	// 前端画出来的折线图会是乱序的（因为 Map 无序），而且如果有 1 分钟没人访问，图表上就会断档缺一个点。
	// 所以后端必须“手动按时间顺序补齐数据”，这叫数据对齐（Data Alignment）。
	
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

