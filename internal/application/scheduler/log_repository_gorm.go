// ============================================================
// internal/scheduler/log_repository_gorm.go - LogRepository 的 GORM 实现
//
// 【纳米级源码说明书 - 架构篇】
// 这是 log_repository.go 里那份《岗位说明书》的具体执行者（实习生）。
// 他叫 GormLogRepository，他的特长是使用 GORM（Go语言最火的ORM框架）来操作数据库。
//
// 只要这个实习生把接口里要求的所有方法都实现了（比如 CreateExecutionLog 等），
// Go 语言就会自动承认：“嗯，你就是一名合格的 LogRepository！”
// 这叫【鸭子类型（Duck Typing）】：只要你叫起来像鸭子，走起来像鸭子，那你就是鸭子。
// 不需要像 Java 那样显式地写 "implements LogRepository"。
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
// 结构体里只存了一个东西：db（数据库连接）。这是他干活的唯一工具。
type GormLogRepository struct {
	db *gorm.DB
}

// NewGormLogRepository 办理入职。把数据库钥匙（db）交给他。
func NewGormLogRepository(db *gorm.DB) *GormLogRepository {
	return &GormLogRepository{db: db}
}

// ---- 单任务执行日志 ----

// CreateExecutionLog 插入一条新的执行日志（发车）
// 面试官问：GORM 怎么插入数据？
// 答：只要把一个结构体指针传给 Create()，GORM 就会自动把它翻译成 INSERT INTO sql语句。
func (r *GormLogRepository) CreateExecutionLog(execLog *model.ExecutionLog) error {
	return r.db.Create(execLog).Error
}

// SaveExecutionLog 更新一条已有的执行日志（到站）
// Save 会更新所有字段，如果只有一两个字段变了，其实可以用 Updates() 更高效。
func (r *GormLogRepository) SaveExecutionLog(execLog *model.ExecutionLog) error {
	return r.db.Save(execLog).Error
}

// CountRunningLogs 统计指定任务当前处于 running 且未结束的日志条数
// 防重击穿：去数据库数一数，这个任务当前有几条 "running" 的记录。
func (r *GormLogRepository) CountRunningLogs(taskID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.ExecutionLog{}).
		Where("task_id = ? AND status = ? AND end_time IS NULL", taskID, model.StateRunning).
		Count(&count).Error
	return count, err
}

// GetLatestTaskLog 获取指定任务的最新一条执行日志（按 ID 降序）
// Order("id DESC").First()：把 ID 从大到小排，拿第一个（也就是最新插入的那个）。
func (r *GormLogRepository) GetLatestTaskLog(taskID uint) (*model.ExecutionLog, error) {
	var execLog model.ExecutionLog
	err := r.db.Where("task_id = ?", taskID).Order("id DESC").First(&execLog).Error
	if err != nil {
		return nil, err
	}
	return &execLog, nil
}

// CleanupOrphanedLogs 清理所有处于 running 状态但无结束时间的孤儿日志
// 停电恢复后，把所有还以为自己在 running 的假象打破，统统设为 failed。
func (r *GormLogRepository) CleanupOrphanedLogs(now time.Time) error {
	result := r.db.Model(&model.ExecutionLog{}).
		Where("status = ? AND end_time IS NULL", model.StateRunning).
		Updates(map[string]interface{}{ // 批量更新这三个字段
			"status":    model.StateFailed,
			"error_msg": "System restarted or crashed",
			"end_time":  now,
		})
	if result.Error != nil {
		return result.Error
	}
	// 如果真的扫出来垃圾了，打个日志通知一下
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
// 【大厂考点：慢SQL优化】
// 这里没有直接 DELETE FROM ... LIMIT N，而是先查出 ID，再用 IN 删除。
// 为什么？因为直接带 Limit 和 Order By 的 Delete，在很多数据库（比如MySQL）的主从复制环境下是不安全的，
// 而且容易引发大范围的锁表。分两步走：先精准锁定要杀的犯人（查ID），再执行枪决（删ID），更安全！
func (r *GormLogRepository) DeleteExcessLogs(maxRecords int) error {
	var count int64
	r.db.Model(&model.ExecutionLog{}).Count(&count) // 先数数总共多少条
	if count <= int64(maxRecords) {
		return nil // 没超标，不用删
	}
	excess := count - int64(maxRecords) // 超标了多少条（要删几个）
	var ids []uint
	
	// 第一步：抓犯人（Pluck 就是把查出来的某一列，单独抽出来变成一个切片/数组）
	err := r.db.Model(&model.ExecutionLog{}).
		Select("id").
		Order("id ASC").        // 从旧到新排
		Limit(int(excess)).     // 只抓多出来的那些
		Pluck("id", &ids).Error // 抽出 ID 存进 ids 切片
	if err != nil {
		return err
	}
	
	// 第二步：执行枪决
	if len(ids) > 0 {
		return r.db.Where("id IN (?)", ids).Delete(&model.ExecutionLog{}).Error
	}
	return nil
}

// DeleteExcessTaskLogs 清理单个任务的超额日志
// 逻辑和上面一模一样，只是加了一个 Where 条件（只管这个特定任务的垃圾）
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
// (以下逻辑与单任务完全一致，只是操作的表变成了 group_execution_logs)

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
