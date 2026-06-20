# 可观测性仪表盘 (Metrics & Observability) 实施计划

本计划旨在为 Cronix 引入内置的内存时序滑窗（Ring Buffer），提供任务执行吞吐量与耗时分布（P95/P99）的实时监控能力，并在前端 Dashboard 渲染折线图。

## User Review Required
> [!IMPORTANT]
> 引入图表需要安装第三方依赖 `echarts` 和 `vue-echarts`，这会让前端打包体积略微增加。后端完全纯 Go 实现，无额外依赖。请确认是否同意引入此依赖。

## Proposed Changes

---

### Backend: Scheduler & Metrics
增加调度器内存监控注册表，无缝记录任务执行耗时与状态。

#### [NEW] `internal/scheduler/metrics.go`
- 创建 `MetricsRegistry` 结构体，内部包含一个容量为 1000 的 `chan MetricEvent`，用于异步削峰接收指标。
- 定义 `MetricBucket`（包含计数器与耗时切片）。
- 实现两个环形队列：`minuteBuckets` (长度 60) 和 `hourBuckets` (长度 24)。
- 提供 `RecordExecution(durationMs int64, success bool)` 将指标投递到 chan。
- 提供后台 goroutine 持续从 chan 读取并聚合到相应的 bucket。
- 提供 `GetSnapshot()` 返回用于前端绘制图表的时序数组（包含时间标签、成功数、失败数、P95、P99）。

#### [MODIFY] `internal/scheduler/executor.go`
- 在 `ExecuteTaskWithContext` 函数的结尾（无论是成功还是失败退出），计算执行耗时 `duration := time.Since(startTime).Milliseconds()`。
- 调用 `metricsRegistry.RecordExecution(duration, err == nil)`。

#### [MODIFY] `internal/scheduler/scheduler.go`
- 在调度器初始化时，启动 `MetricsRegistry` 的后台聚合 goroutine。

---

### Backend: API & Router
暴露供前端拉取的聚合数据接口。

#### [MODIFY] `internal/service/execution_service.go`
- 新增 `GetDashboardMetrics()` 方法，直接透传调用 `scheduler.GlobalMetricsRegistry.GetSnapshot()`。

#### [MODIFY] `internal/handler/log.go`
- 新增 `GetDashboardMetrics(c *gin.Context)` 方法。
- 返回组装好的 JSON 给前端。

#### [MODIFY] `internal/router/router.go`
- 注册新路由：`api.GET("/dashboard/metrics", logHandler.GetDashboardMetrics)`。

---

### Frontend: Dashboard UI
引入图表库并升级首页看板。

#### [MODIFY] `web/package.json`
- 增加依赖 `"echarts": "^5.5.0"` 和 `"vue-echarts": "^6.6.9"`。

#### [MODIFY] `web/src/api/index.ts`
- 在 `dashboardAPI` 下方新增：`metrics: () => request.get('/api/dashboard/metrics')`。

#### [MODIFY] `web/src/main.ts`
- 全局注册 ECharts 组件：`import ECharts from 'vue-echarts'` 和 `app.component('v-chart', ECharts)`。

#### [MODIFY] `web/src/views/Dashboard.vue`
- 在第二行原本空白的部分（或新建一行）插入两个图表卡片。
- 卡片一：**Execution Throughput (吞吐量)** - 双折线图/柱状图展示最近 60 分钟每分钟的成功与失败任务数。
- 卡片二：**Task Latency (耗时分布)** - 折线图展示最近 60 分钟每分钟的 P95 与 P99 耗时。
- 在 `onMounted` 中增加拉取 `metrics` 的调用并组装 ECharts 的 `option`。

## Verification Plan

### Automated Tests
- 新增 `internal/scheduler/metrics_test.go`：
  - 测试异步接收指标，不丢数据。
  - 测试 P95/P99 的数学计算逻辑（向切片 push 100个数据，验证 95 分位值是否正确）。
  - 测试 Ring Buffer 的时间推移（过期 bucket 清理）。

### Manual Verification
- `cd web && npm install`，然后启动前后端。
- 创建一个每秒运行一次的高频任务。
- 刷新 Dashboard，验证吞吐量和耗时分布图表是否每分钟增加一个正确的采样点。
