// ============================================================
// internal/service/execution_service.go - 执行日志服务层
// 负责查询执行日志、统计仪表盘数据、清理旧日志
// ============================================================
package service

import (
    "sync"                        // 并发安全的读写锁
    "time"                        // 时间处理：计算截止日期 / TTL 过期
    "cronix/internal/model"       // 数据模型
    "gorm.io/gorm"                // GORM数据库操作
)

type statsCache struct {
    mu       sync.RWMutex
    data     map[string]interface{}
    expireAt time.Time
}

// ExecutionService is the execution log service layer.
type ExecutionService struct {
    DB    *gorm.DB
    cache *statsCache
}

// NewExecutionService creates a new ExecutionService.
func NewExecutionService(db *gorm.DB) *ExecutionService {
    return &ExecutionService{DB: db, cache: &statsCache{}}
}

// GetTaskLogs 获取某个任务的执行日志（分页、支持按状态筛选）
// 参数 taskID：任务ID
// 参数 page：页码
// 参数 pageSize：每页条数
// 参数 status：按状态筛选（空字符串表示不筛选）
// 返回值：日志列表、总条数、可能发生的错误
func (s *ExecutionService) GetTaskLogs(taskID uint, page, pageSize int, status string) ([]model.ExecutionLog, int64, error) {
    var logs []model.ExecutionLog
    var total int64
    query := s.DB.Model(&model.ExecutionLog{}).Omit("output").Where("task_id = ?", taskID) // 筛选指定任务的日志
    if status != "" {                                              // 如果指定了状态筛选
        query = query.Where("status = ?", status)                  // 添加状态筛选条件
    }
    query.Count(&total)                                            // 统计总条数
    offset := (page - 1) * pageSize                                // 计算偏移量
    // 按开始时间倒序（最新的在前）
    if err := query.Order("start_time DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
        return nil, 0, err
    }
    return logs, total, nil
}

// GetAllLogs 获取所有任务的执行日志（分页、支持多种筛选）
// 参数 page, pageSize：分页参数
// 参数 taskName：按任务名模糊搜索
// 参数 status：按状态筛选
// 参数 since：只查最近多长时间内的（如 "24h"、"1h"）
// 返回值：日志列表、总条数、可能发生的错误
func (s *ExecutionService) GetAllLogs(page, pageSize int, taskName, status, since string) ([]model.ExecutionLog, int64, error) {
    var logs []model.ExecutionLog
    var total int64
    query := s.DB.Model(&model.ExecutionLog{}).Omit("output")                     // 不限定任务，查所有日志

    // 添加各种筛选条件
    if taskName != "" {                                            // 按任务名模糊搜索
        query = query.Where("task_name LIKE ?", "%"+taskName+"%")
    }
    if status != "" {                                              // 按状态精确筛选
        query = query.Where("status = ?", status)
    }
    if since != "" {                                               // 只查最近一段时间内的
        // time.ParseDuration 解析时间长度字符串："24h"=24小时，"1h30m"=1小时30分钟
        if d, err := time.ParseDuration(since); err == nil {
            query = query.Where("start_time > ?", time.Now().Add(-d)) // 开始时间 > 当前时间-时间段
        }
    }

    query.Count(&total)                                            // 统计总条数
    offset := (page - 1) * pageSize
    if err := query.Order("start_time DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
        return nil, 0, err
    }
    return logs, total, nil
}

// GetDashboardStats 获取仪表盘的摘要统计数据
// 返回值是一个map（映射表），包含以下字段：
//   total_tasks: 任务总数
//   enabled_tasks: 已启用的任务数
//   today_total: 今天执行的总次数
//   today_success: 今天成功的次数
//   today_failed: 今天失败的次数
func (s *ExecutionService) GetDashboardStats() (map[string]interface{}, error) {
    // Check cache (60s TTL)
    s.cache.mu.RLock()
    if s.cache.data != nil && time.Now().Before(s.cache.expireAt) {
        data := s.cache.data
        s.cache.mu.RUnlock()
        return data, nil
    }
    s.cache.mu.RUnlock()

    s.cache.mu.Lock()
    defer s.cache.mu.Unlock()
    // Double-check after acquiring write lock
    if s.cache.data != nil && time.Now().Before(s.cache.expireAt) {
        return s.cache.data, nil
    }

    // ---- existing stats queries (keep exactly as-is) ----
    var totalTasks int64
    s.DB.Model(&model.Task{}).Count(&totalTasks)

    var enabledTasks int64
    s.DB.Model(&model.Task{}).Where("enabled = ?", true).Count(&enabledTasks)

    today := time.Now().Truncate(24 * time.Hour)

    var todayTotal int64
    s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ?", today).Count(&todayTotal)

    var todaySuccess int64
    s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ? AND status = ?", today, "success").Count(&todaySuccess)

    var todayFailed int64
    s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ? AND status = ?", today, "failed").Count(&todayFailed)

    stats := map[string]interface{}{
        "total_tasks":   totalTasks,
        "enabled_tasks": enabledTasks,
        "today_total":   todayTotal,
        "today_success": todaySuccess,
        "today_failed":  todayFailed,
    }
    s.cache.data = stats
    s.cache.expireAt = time.Now().Add(60 * time.Second)
    return stats, nil
}

// CleanOldLogs 删除超过指定天数的旧日志
// 参数 retentionDays：保留天数（超过这个天数的日志会被删除）
func (s *ExecutionService) CleanOldLogs(retentionDays int) error {
    cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour) // 计算截止时间
    return s.DB.Where("created_at < ?", cutoff).Delete(&model.ExecutionLog{}).Error // 删除早于截止时间的记录
}

// ClearAllLogs deletes all execution logs and group execution logs.
func (s *ExecutionService) ClearAllLogs() (int64, int64, error) {
    r1 := s.DB.Where("1 = 1").Delete(&model.ExecutionLog{})
    if r1.Error != nil {
        return 0, 0, r1.Error
    }
    r2 := s.DB.Where("1 = 1").Delete(&model.GroupExecutionLog{})
    return r1.RowsAffected, r2.RowsAffected, r2.Error
}

// ClearTaskLogs 清空指定任务的执行日志
func (s *ExecutionService) ClearTaskLogs(taskID uint) (int64, error) {
    result := s.DB.Where("task_id = ?", taskID).Delete(&model.ExecutionLog{})
    return result.RowsAffected, result.Error
}

// DeleteLog 删除单条执行日志
func (s *ExecutionService) DeleteLog(id uint) error {
    return s.DB.Delete(&model.ExecutionLog{}, id).Error
}

// GetLog returns a single execution log with full output.
func (s *ExecutionService) GetLog(id uint) (*model.ExecutionLog, error) {
    var log model.ExecutionLog
    if err := s.DB.First(&log, id).Error; err != nil {
        return nil, err
    }
    return &log, nil
}

// ClearGroupLogs 清空指定组的执行日志
func (s *ExecutionService) ClearGroupLogs(groupID uint) (int64, error) {
    result := s.DB.Where("group_id = ?", groupID).Delete(&model.GroupExecutionLog{})
    return result.RowsAffected, result.Error
}
