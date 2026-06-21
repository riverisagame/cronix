// ============================================================
// internal/handler/log.go - 执行日志和仪表盘处理器
// 负责查询所有日志、仪表盘统计数据、系统设置
// 📌 【大厂面试·核心考点】 面试官常问：日志系统如何设计？海量日志的查询如何分页？导出时如何防止OOM？
// 标准答案：
// 1. 存储选型：热数据存关系型DB或ES，冷数据归档到OSS。
// 2. 分页方案：放弃传统的Offset分页（深分页性能衰减），改用基于Cursor（游标，通常是上一页最后一条ID）的分页。
// 3. 数据导出：使用流式查询（Streaming Query）和流式写入，保持内存占用恒定。
// 🏗️ 【架构设计·模式对比】 
// RESTful API设计中，日志属于典型的只读型宽表。对于高频查询，推荐引入CQRS模式，将写入（大量Insert）和查询分离，
// 查询端可以利用倒排索引（如 Elasticsearch）或时序数据库增强检索性能。
// ============================================================
package handler

import (
    "encoding/csv" // CSV writer for streaming export
    "fmt"          // Sprintf for Content-Disposition header
    "net/http"     // HTTP状态码
    "strconv"      // 字符串转数字
    "time"         // current date for export filename

    "cronix/internal/infrastructure/config"   // 配置模块：读取和保存系统设置
    "cronix/internal/domain/model"    // 数据模型
    "cronix/internal/application/service"   // 服务层：业务逻辑

    "github.com/gin-gonic/gin"  // Gin框架
)

// LogHandler 是日志和仪表盘相关的处理器
type LogHandler struct {
    ExecSvc *service.ExecutionService // 执行服务：查询日志和统计数据
}

// GetAllLogs 获取所有任务的执行日志（分页、可筛选）
// 路由：GET /api/logs?page=1&page_size=20&task_name=xxx&status=success&since=24h
// 🔬 【底层原理·深度剖析】
// Offset 分页原理：数据库会扫描 Offset + Limit 条数据，然后抛弃掉前面的 Offset 条数据。
// 比如 Limit 20 Offset 1000000，数据库需要扫描 1000020 条数据，越往后越慢（时间复杂度 O(N)）。
// ⚡ 【性能实战·生产调优】
// 如果表数据量超过百万，强烈建议前端将 page 参数替换为 cursor（上一条记录的ID），
// 后端通过 `WHERE id < cursor ORDER BY id DESC LIMIT 20` 借助主键索引进行 O(1) 复杂度的分页。
// 🛡️ 【安全攻防·漏洞防线】
// 警惕“分页拒绝服务攻击（Pagination DDoS）”。攻击者恶意请求非常大的 page，导致数据库全表扫描，耗尽 CPU 和 IO。
// 防御：必须限制 page_size 上限（例如 100），并对极深的分页（如 page > 10000）直接拒绝或降级处理。
func (h *LogHandler) GetAllLogs(c *gin.Context) {
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))        // 页码，默认第1页
    pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20")) // 每页数量，默认20
    // 💀 【踩坑血泪·反面教材】 曾经有系统没对 pageSize 设限，被黑客传入 pageSize=1000000，直接把DB和微服务内存撑爆（OOM）。
    if pageSize > 100 {                                          // 最多每页100条，强制安全边界
        pageSize = 100
    }
    if page < 1 {                                                // 页码最小为1
        page = 1
    }
    // 读取各种筛选条件
    // 📌 【大厂面试·核心考点】 问：关系型数据库中模糊查询（LIKE '%xxx%'）会走索引吗？
    // 答：左模糊和全模糊（LIKE '%xx' 或 LIKE '%xx%'）会导致 B+树 索引失效，引发全表扫描。
    // 优化方案：如果 task_name 检索频率高，应该引入全文搜索引擎（如 ES），或在 DB 中使用倒排索引插件（如 PostgreSQL的 pg_trgm）。
    taskName := c.Query("task_name")                             // 按任务名模糊搜索
    status := c.Query("status")                                  // 按执行状态筛选：success / failed / running
    since := c.Query("since")                                    // 只查最近多长时间内的日志，如 "24h" 表示最近24小时

    // 调用服务层查询
    logs, total, err := h.ExecSvc.GetAllLogs(page, pageSize, taskName, status, since)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"items": logs, "total": total}})
}

// GetDashboardStats 获取仪表盘的统计数据
// 路由：GET /api/dashboard/stats
// 返回：任务总数、启用的任务数、今天执行次数、今天成功次数、今天失败次数
// ⚡ 【性能实战·生产调优】
// 这里的统计操作通常涉及对大表执行 COUNT(*) 或者复杂的按条件聚合。
// 如果每次请求都实时去 DB count，在并发高时会拖垮 DB（全表扫描的 CPU 开销极大）。
// 生活比喻：就像每次有人问超市今天卖了多少货，你都让理货员去把全超市的货架重新数一遍。
// 生产环境通常采用“增量统计+缓存”模式：
// 用 Redis 的 INCR 操作实时记录当天的成功/失败次数，或者通过定时任务每分钟聚合一次写到汇总统计表。
func (h *LogHandler) GetDashboardStats(c *gin.Context) {
    stats, err := h.ExecSvc.GetDashboardStats()                  // 调用服务层获取统计数字
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    respondOK(c, stats) // 返回统计数据
}

// GetSettings 读取当前系统设置
// 路由：GET /api/settings
// 返回：线程池大小、输出截断大小、日志保留天数、最大记录数、熔断阈值、冷却时间
func (h *LogHandler) GetSettings(c *gin.Context) {
    cfg := config.AppConfig                                      // 获取全局配置对象
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{
        "pool_size":          cfg.Executor.PoolSize,             // 同时最多执行几个任务
        "output_truncate_kb": cfg.Executor.OutputTruncateKB,     // 任务输出最大保存多少KB
        "retention_days":     cfg.Log.RetentionDays,             // 日志保留多少天
        "max_records":        cfg.Log.MaxRecords,                // 日志最多保留多少条
        "cb_threshold":       cfg.CircuitBreaker.FailureThreshold, // 熔断器：连续失败多少次后断开
        "cb_cooldown":        cfg.CircuitBreaker.CooldownSeconds,  // 熔断器：断开后冷却多少秒
    }})
}

// UpdateSettings 更新系统设置并保存到配置文件
// 路由：PUT /api/settings
// 请求体：JSON对象，可以只传要修改的字段
func (h *LogHandler) UpdateSettings(c *gin.Context) {
    var req map[string]interface{}                               // 用映射表接收任意字段的更新
    if err := c.ShouldBindJSON(&req); err != nil {               // 解析请求JSON
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    cfg := config.AppConfig                                      // 获取全局配置对象
    // 逐一检查每个可能的设置项，如果在请求中存在且大于0，就更新
    // 🛡️ 【安全攻防·漏洞防线】 类型断言安全：JSON中的数字在未指定结构体映射时，Go 默认会反序列化为 float64。
    // 这里严格检查并限定必须为数值型，防止类型注入或程序崩溃（Panic）。
    // 同时 `v > 0` 是非常关键的边界防御（防御式编程），防止配置被恶意设为负数或0，导致切片越界、死循环或除零异常。
    if v, ok := req["pool_size"].(float64); ok && v > 0 {       // JSON中的数字会被解析为float64类型
        cfg.Executor.PoolSize = int(v)                           // 转成整数赋值
    }
    if v, ok := req["output_truncate_kb"].(float64); ok && v > 0 {
        cfg.Executor.OutputTruncateKB = int(v)
    }
    if v, ok := req["retention_days"].(float64); ok && v > 0 {
        cfg.Log.RetentionDays = int(v)
    }
    if v, ok := req["max_records"].(float64); ok && v > 0 {
        cfg.Log.MaxRecords = int(v)
    }
    if v, ok := req["cb_threshold"].(float64); ok && v > 0 {
        cfg.CircuitBreaker.FailureThreshold = int(v)
    }
    if v, ok := req["cb_cooldown"].(float64); ok && v > 0 {
        cfg.CircuitBreaker.CooldownSeconds = int(v)
    }
    // 把修改后的配置保存到磁盘上的配置文件
    if err := config.SaveConfig(); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    respondOKMsg(c, "settings saved")
}

// ClearAllLogs deletes all execution logs.
// DELETE /api/logs
func (h *LogHandler) ClearAllLogs(c *gin.Context) {
    taskLogs, groupLogs, err := h.ExecSvc.ClearAllLogs()
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{
        "task_logs_deleted":  taskLogs,
        "group_logs_deleted": groupLogs,
    }})
}

// ClearTaskLogs deletes all execution logs for a specific task.
// DELETE /api/tasks/:id/logs
func (h *LogHandler) ClearTaskLogs(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    n, err := h.ExecSvc.ClearTaskLogs(uint(id))
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"deleted": n}})
}

// DeleteLog deletes a single execution log entry.
func (h *LogHandler) DeleteLog(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    if err := h.ExecSvc.DeleteLog(uint(id)); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    respondOKMsg(c, "ok")
}

// GetLog returns a single execution log with full output.
func (h *LogHandler) GetLog(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	log, err := h.ExecSvc.GetLog(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "log not found"})
		return
	}
	respondOK(c, log)
}

// GetDashboardMetrics 返回系统的运行指标
// @Ref: docs/sps/plans/20260605_metrics_plan.md | @Date: 2026-06-05
func (h *LogHandler) GetDashboardMetrics(c *gin.Context) {
	metrics := h.ExecSvc.GetDashboardMetrics()
	respondOK(c, metrics)
}

// ClearGroupLogs deletes all execution logs for a group.
func (h *LogHandler) ClearGroupLogs(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    n, err := h.ExecSvc.ClearGroupLogs(uint(id))
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"deleted": n}})
}

// ExportLogs exports execution logs as CSV or JSON.
// GET /api/logs/export?format=csv|json&task_name=&status=&since=&max=100000
// 📌 【大厂面试·核心考点】 面试官：如何设计一个支持导出几百万条数据的接口？
// 标准答案：绝对不能将数据一次性全部加载到内存中（如放到一个巨大的 Slice 里），否则必然发生 OOM。
// 必须采用流式导出（Streaming Export）：
// 1. 数据库层：使用 Cursor 或流式查询（如 GORM 的 Rows），一次从数据库取一批或一条。
// 2. 应用层：读取一条记录，进行格式化，然后立马通过流发送出去。
// 3. 网络层：通过 HTTP Chunked Transfer Encoding，或者一边 Write 一边 Flush，将数据持续推给客户端。
// 🔬 【底层原理·深度剖析】
// HTTP 响应对象本质上是一个 io.Writer。Gin 的 c.Writer 实现了 http.ResponseWriter。
// 当不断调用 `w.Write()` 和 `w.Flush()` 时，底层 TCP 协议栈和 HTTP 服务器会将数据分块发送给前端（或者下载工具），
// 从而实现内存占用的常量级别（O(1) 空间复杂度），即使导出 10GB 的数据，内存也只占用几 MB。
func (h *LogHandler) ExportLogs(c *gin.Context) {
    format := c.DefaultQuery("format", "csv")
    maxRows, _ := strconv.Atoi(c.DefaultQuery("max", "100000"))
    if maxRows > 100000 {
        maxRows = 100000
    }
    if maxRows < 1 {
        maxRows = 100000
    }

    taskName := c.Query("task_name")
    status := c.Query("status")
    since := c.Query("since")

    date := time.Now().Format("2006-01-02")

    // 💀 【踩坑血泪·反面教材】
    // 曾经有个导出接口，开发为了图省事，写了类似 `c.JSON(200, db.Find(&logs))` 的全量加载代码。
    // 平时测试数据量小没问题。上线后表中积累了一千万数据，用户一点导出，整个服务器内存瞬间飙升，
    // 触发系统的 OOM-killer 直接杀死了进程，导致微服务雪崩。
    // 因此这里即使是 JSON 导出，也会受制于 maxRows，并且由于 JSON 结构特性较难纯流式写入，它更倾向于批量模式。
    if format == "json" {
        // JSON: batch mode (limited to maxRows, acceptable for JSON consumers)
        var logs []model.ExecutionLog
        if err := h.ExecSvc.ExportLogsStream(taskName, status, since, maxRows, func(l model.ExecutionLog) error {
            logs = append(logs, l)
            return nil
        }); err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
            return
        }
        c.Header("Content-Type", "application/json; charset=utf-8")
        c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"cronix-logs-%s.json\"", date))
        c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": logs})
        return
    }

    // CSV: stream row-by-row, never materializes full result set
    // ⚡ 【性能实战·生产调优】 
    // CSV 是纯文本，没有 JSON 的大括号、键名等冗余符号，数据荷载占比极大，非常适合海量数据的网络传输。
    // 此处使用 encoding/csv 包流式写入 c.Writer。
    // 生活比喻：就像流水线打包快递，不是等所有的包裹都堆在仓库里才去发车，而是一个包裹打包好就立马扔上运输车运走。
    c.Header("Content-Type", "text/csv; charset=utf-8")
    c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"cronix-logs-%s.csv\"", date))
    w := csv.NewWriter(c.Writer)
    if err := w.Write([]string{"id", "task_name", "group_name", "status", "trigger_type", "start_time", "end_time", "duration_ms", "exit_code", "output_truncated", "error_msg"}); err != nil {
        return
    }
    if err := h.ExecSvc.ExportLogsStream(taskName, status, since, maxRows, func(l model.ExecutionLog) error {
        endTime := ""
        var durationMs int64
        if l.EndTime != nil {
            endTime = l.EndTime.Format("2006-01-02 15:04:05")
            durationMs = l.EndTime.Sub(l.StartTime).Milliseconds()
        }
        exitCode := ""
        if l.ExitCode != nil {
            exitCode = strconv.Itoa(*l.ExitCode)
        }
        outputTrunc := "false"
        truncateKB := config.AppConfig.Executor.OutputTruncateKB
        if truncateKB > 0 && len(l.Output) > truncateKB*1024 {
            outputTrunc = "true"
        }
        return w.Write([]string{
            strconv.FormatUint(uint64(l.ID), 10),
            l.TaskName,
            l.GroupName,
            l.Status,
            l.TriggerType,
            l.StartTime.Format("2006-01-02 15:04:05"),
            endTime,
            strconv.FormatInt(durationMs, 10),
            exitCode,
            outputTrunc,
            l.ErrorMsg,
        })
    }); err != nil {
        return
    }
    w.Flush()
}
