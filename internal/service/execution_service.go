// ============================================================
// internal/service/execution_service.go - 执行日志服务层
// 负责查询执行日志、统计仪表盘数据、清理旧日志
// ============================================================
package service

import (
    "time"                        // 时间处理：计算截止日期
    "cronix/internal/model"       // 数据模型
    "gorm.io/gorm"                // GORM数据库操作
)

// ExecutionService 是执行日志的服务层
type ExecutionService struct {
    DB *gorm.DB                   // 数据库连接
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
    // 统计任务总数
    var totalTasks int64
    s.DB.Model(&model.Task{}).Count(&totalTasks)

    // 统计已启用的任务数（enabled = true）
    var enabledTasks int64
    s.DB.Model(&model.Task{}).Where("enabled = ?", true).Count(&enabledTasks)

    // 计算今天的起始时间（00:00:00）
    today := time.Now().Truncate(24 * time.Hour)                  // Truncate 向下取整到天级别

    // 统计今天的执行总数
    var todayTotal int64
    s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ?", today).Count(&todayTotal)

    // 统计今天的成功数
    var todaySuccess int64
    s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ? AND status = ?", today, "success").Count(&todaySuccess)

    // 统计今天的失败数
    var todayFailed int64
    s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ? AND status = ?", today, "failed").Count(&todayFailed)

    // 把统计结果放入map返回
    return map[string]interface{}{
        "total_tasks":    totalTasks,
        "enabled_tasks":  enabledTasks,
        "today_total":    todayTotal,
        "today_success":  todaySuccess,
        "today_failed":   todayFailed,
    }, nil
}

// CleanOldLogs 删除超过指定天数的旧日志
// 参数 retentionDays：保留天数（超过这个天数的日志会被删除）
func (s *ExecutionService) CleanOldLogs(retentionDays int) error {
    cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour) // 计算截止时间
    return s.DB.Where("created_at < ?", cutoff).Delete(&model.ExecutionLog{}).Error // 删除早于截止时间的记录
}

// ClearAllLogs 清空所有执行日志
func (s *ExecutionService) ClearAllLogs() (int64, error) {
    result := s.DB.Where("1 = 1").Delete(&model.ExecutionLog{})
    return result.RowsAffected, result.Error
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
