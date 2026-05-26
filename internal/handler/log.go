// ============================================================
// internal/handler/log.go - 执行日志和仪表盘处理器
// 负责查询所有日志、仪表盘统计数据、系统设置
// ============================================================
package handler

import (
    "encoding/csv" // CSV writer for streaming export
    "fmt"          // Sprintf for Content-Disposition header
    "net/http"     // HTTP状态码
    "strconv"      // 字符串转数字
    "time"         // current date for export filename

    "cronix/internal/config"   // 配置模块：读取和保存系统设置
    "cronix/internal/service"   // 服务层：业务逻辑

    "github.com/gin-gonic/gin"  // Gin框架
)

// LogHandler 是日志和仪表盘相关的处理器
type LogHandler struct {
    ExecSvc *service.ExecutionService // 执行服务：查询日志和统计数据
}

// GetAllLogs 获取所有任务的执行日志（分页、可筛选）
// 路由：GET /api/logs?page=1&page_size=20&task_name=xxx&status=success&since=24h
func (h *LogHandler) GetAllLogs(c *gin.Context) {
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))        // 页码，默认第1页
    pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20")) // 每页数量，默认20
    if pageSize > 100 {                                          // 最多每页100条
        pageSize = 100
    }
    if page < 1 {                                                // 页码最小为1
        page = 1
    }
    // 读取各种筛选条件
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
func (h *LogHandler) GetDashboardStats(c *gin.Context) {
    stats, err := h.ExecSvc.GetDashboardStats()                  // 调用服务层获取统计数字
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": stats}) // 返回统计数据
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
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "settings saved"})
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
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

// GetLog returns a single execution log with full output.
func (h *LogHandler) GetLog(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    log, err := h.ExecSvc.GetLog(uint(id))
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "log not found"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": log})
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

    logs, err := h.ExecSvc.ExportLogs(taskName, status, since, maxRows)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }

    date := time.Now().Format("2006-01-02")

    if format == "json" {
        c.Header("Content-Type", "application/json; charset=utf-8")
        c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"cronix-logs-%s.json\"", date))
        c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": logs})
        return
    }

    // Default: CSV
    c.Header("Content-Type", "text/csv; charset=utf-8")
    c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"cronix-logs-%s.csv\"", date))
    w := csv.NewWriter(c.Writer)
    w.Write([]string{"id", "task_name", "status", "trigger_type", "start_time", "end_time", "exit_code", "error_msg", "created_at"})
    for _, l := range logs {
        endTime := ""
        if l.EndTime != nil {
            endTime = l.EndTime.Format("2006-01-02 15:04:05")
        }
        exitCode := ""
        if l.ExitCode != nil {
            exitCode = strconv.Itoa(*l.ExitCode)
        }
        w.Write([]string{
            strconv.FormatUint(uint64(l.ID), 10),
            l.TaskName,
            l.Status,
            l.TriggerType,
            l.StartTime.Format("2006-01-02 15:04:05"),
            endTime,
            exitCode,
            l.ErrorMsg,
            l.CreatedAt.Format("2006-01-02 15:04:05"),
        })
    }
    w.Flush()
}
