// ============================================================
// internal/scheduler/log_repository.go - 执行日志仓储接口
//
// 将 executor.go 中所有对 execution_logs / group_execution_logs 表的
// 数据库操作抽象为接口，解耦底层存储实现。
// 默认实现为 GormLogRepository（见 log_repository_gorm.go）。
//
// @Ref: docs/sps/plans/20260612_arch_hardening_plan.md | @Date: 2026-06-12
// ============================================================
package scheduler

import (
	"cronix/internal/model"
	"time"
)

// LogRepository 定义执行日志的存储操作接口
// 所有对 execution_logs 和 group_execution_logs 表的增删改查都通过此接口进行
type LogRepository interface {
	// ---- 单任务执行日志 ----

	// CreateExecutionLog 插入一条新的执行日志（状态通常为 running）
	CreateExecutionLog(log *model.ExecutionLog) error

	// SaveExecutionLog 更新一条已有的执行日志（如更新状态、结束时间等）
	SaveExecutionLog(log *model.ExecutionLog) error

	// CountRunningLogs 统计指定任务当前处于 running 且未结束的日志条数
	CountRunningLogs(taskID uint) (int64, error)

	// GetLatestTaskLog 获取指定任务的最新一条执行日志
	GetLatestTaskLog(taskID uint) (*model.ExecutionLog, error)

	// CleanupOrphanedLogs 清理所有处于 running 状态但无结束时间的孤儿日志
	// 将它们标记为 failed，附加错误信息和结束时间
	CleanupOrphanedLogs(now time.Time) error

	// DeleteLogsBefore 删除创建时间早于 cutoff 的执行日志
	DeleteLogsBefore(cutoff time.Time) (int64, error)

	// DeleteExcessLogs 当总日志数超过 maxRecords 时，删除最旧的记录
	DeleteExcessLogs(maxRecords int) error

	// DeleteExcessTaskLogs 清理单个任务的超额日志
	DeleteExcessTaskLogs(taskID uint, maxLogs int) error

	// ---- 组执行日志 ----

	// CreateGroupLog 插入一条新的组执行日志
	CreateGroupLog(log *model.GroupExecutionLog) error

	// SaveGroupLog 更新一条已有的组执行日志
	SaveGroupLog(log *model.GroupExecutionLog) error

	// DeleteGroupLogsBefore 删除创建时间早于 cutoff 的组执行日志
	DeleteGroupLogsBefore(cutoff time.Time) (int64, error)

	// DeleteExcessGroupLogs 当组日志总数超过 maxRecords 时，删除最旧的记录
	DeleteExcessGroupLogs(maxRecords int) error
}
