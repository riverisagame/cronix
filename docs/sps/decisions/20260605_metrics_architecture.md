# [ADR] 20260605: 可观测性面板与性能指标采集 (Metrics Architecture)

## 1. 背景与目标 (Background & Goals)
用户选择了“路线 B：可观测性仪表盘”。
当前系统的 Dashboard 仅提供总量、今日运行量等粗粒度聚合，缺乏时间序列的性能分布和实时吞吐监控。
目标是实现“企业级调度的监控逼格”，即包含 QPS、执行耗时分布 (P95/P99) 和调度延迟，并且在前端绘制折线图。

## 2. 方案对比与自我攻击 (Options & Adversarial Audit)

### 方案 1：纯数据库聚合查询 (Database Aggregation)
- **实现**：前端定时请求，后端执行类似 `SELECT strftime('%Y-%m-%d %H', start_time) as time, count(*), avg(duration) FROM execution_logs GROUP BY time`。
- **优点**：数据持久化，重启不丢失，实现简单。
- **缺点（攻击点）**：
  1. 数据库不支持原生的 P95/P99 聚合计算（SQLite 没有 percentile，MySQL 计算复杂）。
  2. 性能对冲失败：如果日志表达到百万级，高频（如每 5 秒）聚合查询会直接打挂数据库导致 CPU 100%。

### 方案 2：外接 Prometheus + Grafana (External Observability)
- **实现**：系统仅暴露 `/metrics` 接口（使用 `prometheus/client_golang`），由外部 Prometheus 抓取。
- **优点**：业界标准，功能极其强大。
- **缺点（攻击点）**：
  1. 违背了系统“单二进制、开箱即用”的设计初衷，用户需要额外部署一套监控栈，运维成本过高。

### 方案 3：内置内存时序滑窗 (In-Memory Ring Buffer / Time-Series Window) —— 【推荐】
- **实现**：
  - 在 `internal/scheduler/` 包中引入轻量级的内存监控注册表（Metrics Registry）。
  - 按时间维度（如“最近 60 分钟”、“最近 24 小时”）在内存中维护 Bucket。
  - 任务执行结束时，异步向内存 Bucket 推送执行时长和状态。
  - 提供 `/api/metrics/realtime` 接口，计算出各时间窗口的 P95/P99、总次数，直接给前端。
- **优点**：对数据库 0 压力，支持高性能实时查询，前端直接用 ECharts 画图，开箱即用。
- **缺点**：应用重启后，最近 1 小时的实时内存指标会重置（但历史执行日志还在数据库）。对于实时监控屏而言，这种重置是可以接受的。

## 3. 最终决策 (Decision)
采用 **方案 3：内置内存时序滑窗 + 前端 ECharts 渲染**。
- **后端**：设计无锁/低锁的内存环形队列，控制内存增长（如最多保留 60 个 1分钟 Bucket）。
- **前端**：引入 `echarts` 和 `vue-echarts`，在 Dashboard 增加“吞吐量 (Requests)”和“耗时分布 (Latency P95/P99)”折线图。

## 4. 对现有功能的影响评估 (Impact Analysis)
- `internal/scheduler/executor.go`：任务执行完毕需增加一行向 Metrics 注册表推送数据的代码（使用 Channel 异步推送，确保不阻塞执行器）。
- 路由与前端构建：需要修改前端依赖树，可能略微增加打包体积，但对核心调度逻辑**完全无侵入**。
