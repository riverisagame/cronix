// ============================================================
// internal/scheduler/log_repository_gorm.go - LogRepository 的 GORM 实现
//
// 将 executor.go 中分散的数据库操作集中到此处，
// 实现 LogRepository 接口的全部方法。
// 每个方法直接搬运 executor.go 中已验证的 DB 操作逻辑。
//
// @Ref: docs/sps/plans/20260612_arch_hardening_plan.md | @Date: 2026-06-12
// ============================================================
package scheduler

import (
	"cronix/internal/domain/model"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// GormLogRepository 使用 GORM 实现 LogRepository 接口
type GormLogRepository struct {
	db *gorm.DB
}

// NewGormLogRepository 创建基于 GORM 的日志仓储
func NewGormLogRepository(db *gorm.DB) *GormLogRepository {
	return &GormLogRepository{db: db}
}

// ---- 单任务执行日志 ----

// CreateExecutionLog 插入一条新的执行日志
func (r *GormLogRepository) CreateExecutionLog(execLog *model.ExecutionLog) error {
	return r.db.Create(execLog).Error
}

// SaveExecutionLog 更新一条已有的执行日志
func (r *GormLogRepository) SaveExecutionLog(execLog *model.ExecutionLog) error {
	return r.db.Save(execLog).Error
}

// CountRunningLogs 统计指定任务当前处于 running 且未结束的日志条数
func (r *GormLogRepository) CountRunningLogs(taskID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.ExecutionLog{}).
		Where("task_id = ? AND status = ? AND end_time IS NULL", taskID, model.StateRunning).
		Count(&count).Error
	return count, err
}

// GetLatestTaskLog 获取指定任务的最新一条执行日志（按 ID 降序）
func (r *GormLogRepository) GetLatestTaskLog(taskID uint) (*model.ExecutionLog, error) {
	var execLog model.ExecutionLog
	err := r.db.Where("task_id = ?", taskID).Order("id DESC").First(&execLog).Error
	if err != nil {
		return nil, err
	}
	return &execLog, nil
}

// CleanupOrphanedLogs 清理所有处于 running 状态但无结束时间的孤儿日志
// 这些日志通常是由于进程崩溃或强杀导致的残留
func (r *GormLogRepository) CleanupOrphanedLogs(now time.Time) error {
	result := r.db.Model(&model.ExecutionLog{}).
		Where("status = ? AND end_time IS NULL", model.StateRunning).
		Updates(map[string]interface{}{
			"status":    model.StateFailed,
			"error_msg": "System restarted or crashed",
			"end_time":  now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		log.Warn().Int64("count", result.RowsAffected).Msg("已清理孤儿 running 日志")
	}
	return nil
}

// DeleteLogsBefore 删除创建时间早于 cutoff 的执行日志
func (r *GormLogRepository) DeleteLogsBefore(cutoff time.Time) (int64, error) {
	result := r.db.Where("created_at < ?", cutoff).Delete(&model.ExecutionLog{})
	return result.RowsAffected, result.Error
}

// DeleteExcessLogs 当总日志数超过 maxRecords 时，删除最旧的记录
func (r *GormLogRepository) DeleteExcessLogs(maxRecords int) error {
	var count int64
	r.db.Model(&model.ExecutionLog{}).Count(&count)
	if count <= int64(maxRecords) {
		return nil
	}
	excess := count - int64(maxRecords)
	var ids []uint
	err := r.db.Model(&model.ExecutionLog{}).
		Select("id").
		Order("id ASC").
		Limit(int(excess)).
		Pluck("id", &ids).Error
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return r.db.Where("id IN (?)", ids).Delete(&model.ExecutionLog{}).Error
	}
	return nil
}

// DeleteExcessTaskLogs 清理单个任务的超额日志
// 搬运自 executor.go 的 limitTaskLogs 方法
func (r *GormLogRepository) DeleteExcessTaskLogs(taskID uint, maxLogs int) error {
	var count int64
	r.db.Model(&model.ExecutionLog{}).Where("task_id = ?", taskID).Count(&count)
	if count <= int64(maxLogs) {
		return nil
	}
	excess := count - int64(maxLogs)
	var ids []uint
	err := r.db.Model(&model.ExecutionLog{}).
		Select("id").
		Where("task_id = ?", taskID).
		Order("id ASC").
		Limit(int(excess)).
		Pluck("id", &ids).Error
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		result := r.db.Where("id IN (?)", ids).Delete(&model.ExecutionLog{})
		if result.Error != nil {
			return result.Error
		}
		log.Debug().Int64("deleted", result.RowsAffected).Uint("task_id", taskID).Msg("limitTaskLogs pruned excess logs")
	}
	return nil
}

// ---- 组执行日志 ----

// CreateGroupLog 插入一条新的组执行日志
func (r *GormLogRepository) CreateGroupLog(glog *model.GroupExecutionLog) error {
	return r.db.Create(glog).Error
}

// SaveGroupLog 更新一条已有的组执行日志
func (r *GormLogRepository) SaveGroupLog(glog *model.GroupExecutionLog) error {
	return r.db.Save(glog).Error
}

// DeleteGroupLogsBefore 删除创建时间早于 cutoff 的组执行日志
func (r *GormLogRepository) DeleteGroupLogsBefore(cutoff time.Time) (int64, error) {
	result := r.db.Where("created_at < ?", cutoff).Delete(&model.GroupExecutionLog{})
	return result.RowsAffected, result.Error
}

// DeleteExcessGroupLogs 当组日志总数超过 maxRecords 时，删除最旧的记录
func (r *GormLogRepository) DeleteExcessGroupLogs(maxRecords int) error {
	var count int64
	r.db.Model(&model.GroupExecutionLog{}).Count(&count)
	if count <= int64(maxRecords) {
		return nil
	}
	excess := count - int64(maxRecords)
	var ids []uint
	err := r.db.Model(&model.GroupExecutionLog{}).
		Select("id").
		Order("id ASC").
		Limit(int(excess)).
		Pluck("id", &ids).Error
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return r.db.Where("id IN (?)", ids).Delete(&model.GroupExecutionLog{}).Error
	}
	return nil
}
