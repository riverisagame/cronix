package scheduler

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// MetricEvent represents a single task execution event
type MetricEvent struct {
	DurationMs int64
	Success    bool
	Time       time.Time
}

// MetricSnapshot contains the flattened arrays for frontend charting
type MetricSnapshot struct {
	MinuteLabels  []string `json:"minute_labels"`
	MinuteSuccess []int64  `json:"minute_success"`
	MinuteFailed  []int64  `json:"minute_failed"`
	MinuteP95     []int64  `json:"minute_p95"`
	MinuteP99     []int64  `json:"minute_p99"`

	HourLabels  []string `json:"hour_labels"`
	HourSuccess []int64  `json:"hour_success"`
	HourFailed  []int64  `json:"hour_failed"`
	HourP95     []int64  `json:"hour_p95"`
	HourP99     []int64  `json:"hour_p99"`
}

type MetricBucket struct {
	Timestamp time.Time
	Success   int64
	Failed    int64
	Durations []int64
}

// @Ref: docs/sps/plans/20260605_metrics_plan.md | @Date: 2026-06-05
type MetricsRegistry struct {
	mu            sync.RWMutex
	events        chan MetricEvent
	minuteBuckets map[string]*MetricBucket // key: "2006-01-02 15:04"
	hourBuckets   map[string]*MetricBucket // key: "2006-01-02 15"
	stopChan      chan struct{}
	stopped       bool
}

var GlobalMetricsRegistry *MetricsRegistry

func init() {
	GlobalMetricsRegistry = NewMetricsRegistry()
}

func NewMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{
		events:        make(chan MetricEvent, 1000), // Async channel buffer to absorb bursts
		minuteBuckets: make(map[string]*MetricBucket),
		hourBuckets:   make(map[string]*MetricBucket),
		stopChan:      make(chan struct{}),
	}
}

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
	select {
	case m.events <- event:
	default:
		// Drop if channel is full to prevent blocking the executor (performance hedge)
	}
}

func (m *MetricsRegistry) Start() {
	go m.processEvents()
	go m.cleanupLoop()
}

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
// 防止高频任务场景下 Durations slice 无界增长导致内存泄漏
// @Ref: architect_review_20260609.md P1-5 | @Date: 2026-06-09
const maxDurationSamples = 1000

func (m *MetricsRegistry) processEvents() {
	for {
		select {
		case <-m.stopChan:
			return
		case ev := <-m.events:
			m.mu.Lock()
			minKey := ev.Time.Format("2006-01-02 15:04")
			hourKey := ev.Time.Format("2006-01-02 15")

			if _, ok := m.minuteBuckets[minKey]; !ok {
				m.minuteBuckets[minKey] = &MetricBucket{Timestamp: ev.Time}
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

func (m *MetricsRegistry) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			// Keep last 60 minutes
			for k, v := range m.minuteBuckets {
				if now.Sub(v.Timestamp).Minutes() > 60 {
					delete(m.minuteBuckets, k)
				}
			}
			// Keep last 24 hours
			for k, v := range m.hourBuckets {
				if now.Sub(v.Timestamp).Hours() > 24 {
					delete(m.hourBuckets, k)
				}
			}
			m.mu.Unlock()
		}
	}
}

func calculatePercentile(durations []int64, percentile float64) int64 {
	if len(durations) == 0 {
		return 0
	}
	sorted := make([]int64, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	
	idx := int(float64(len(sorted)) * percentile)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func (m *MetricsRegistry) GetSnapshot() MetricSnapshot {
	m.mu.RLock()
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

	// For minute buckets, generate last 60 minutes sequence
	now := time.Now()
	for i := 59; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * time.Minute)
		key := t.Format("2006-01-02 15:04")
		label := t.Format("15:04")
		snap.MinuteLabels = append(snap.MinuteLabels, label)
		
		if bucket, ok := m.minuteBuckets[key]; ok {
			snap.MinuteSuccess = append(snap.MinuteSuccess, bucket.Success)
			snap.MinuteFailed = append(snap.MinuteFailed, bucket.Failed)
			snap.MinuteP95 = append(snap.MinuteP95, calculatePercentile(bucket.Durations, 0.95))
			snap.MinuteP99 = append(snap.MinuteP99, calculatePercentile(bucket.Durations, 0.99))
		} else {
			snap.MinuteSuccess = append(snap.MinuteSuccess, 0)
			snap.MinuteFailed = append(snap.MinuteFailed, 0)
			snap.MinuteP95 = append(snap.MinuteP95, 0)
			snap.MinuteP99 = append(snap.MinuteP99, 0)
		}
	}

	// For hour buckets, generate last 24 hours sequence
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
