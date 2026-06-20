// ============================================================
// internal/scheduler/log_repository.go - 执行日志仓储接口
//
// 【纳米级源码说明书 - 架构篇】
// 这是什么？这是一个“接口（Interface）”。
// 接口就像是公司里的一份《岗位职责说明书》，规定了“日志管理员”每天要干什么活，
// 但绝不规定他是用什么工具（MySQL、SQLite、MongoDB还是写记事本）干的。
//
// 面试官问：为什么要定义这个接口，而不是直接在业务代码里写 SQL 操作数据库？
// 答（小白秒懂版）：
// 如果车间主任（Executor）直接用 GORM 操作 SQLite 数据库，那他就和 SQLite “绑死”了。
// 以后公司做大了，老板说：“我们要把日志存到云端的 ElasticSearch 或者 MongoDB 去！”
// 那你就得把车间主任的代码全部改一遍，很容易改出 Bug。
//
// 现在有了这个接口（岗位说明书），车间主任只管发号施令：“给我存一条日志！”
// 具体是谁去存？是底层的“实习生（具体的结构体，比如 GormLogRepository）”去存的。
// 以后换数据库，只需要新招一个实习生就行了，车间主任的代码一行都不用改！
// 这在设计模式中叫做【依赖倒置原则（Dependency Inversion Principle）】。
//
// @Ref: docs/sps/plans/20260612_arch_hardening_plan.md | @Date: 2026-06-12
// ============================================================
package scheduler

import (
	"cronix/internal/domain/model"
	"time"
)

// LogRepository 定义执行日志的存储操作接口（日志管理员的岗位说明书）
// 所有对 execution_logs 和 group_execution_logs 表的增删改查都通过此接口进行
type LogRepository interface {
	// ---- 单任务执行日志 ----

	// CreateExecutionLog 插入一条新的执行日志（发车：状态通常为 running）
	CreateExecutionLog(log *model.ExecutionLog) error

	// SaveExecutionLog 更新一条已有的执行日志（到站：更新状态为 success/failed、记录结束时间等）
	SaveExecutionLog(log *model.ExecutionLog) error

	// CountRunningLogs 统计指定任务当前处于 running 且未结束的日志条数
	// 【防重击穿神器】：防止同一个任务被同时触发 100 次，挤爆服务器。
	CountRunningLogs(taskID uint) (int64, error)

	// GetLatestTaskLog 获取指定任务的最新一条执行日志
	// 组任务（Sequential 模式）必须要看上一条日志是不是成功了，才会决定要不要跑下一条。
	GetLatestTaskLog(taskID uint) (*model.ExecutionLog, error)

	// CleanupOrphanedLogs 清理所有处于 running 状态但无结束时间的孤儿日志
	// 【系统自愈机制】：如果服务器突然断电，数据库里还有没跑完的任务状态。
	// 下次重启时，必须把它们揪出来，强制标记为 failed（失败），并写上 "系统崩溃"。
	CleanupOrphanedLogs(now time.Time) error

	// DeleteLogsBefore 删除创建时间早于 cutoff 的执行日志
	// 扫地大妈的策略一：按时间过期清理（比如只保留 30 天的日志）
	DeleteLogsBefore(cutoff time.Time) (int64, error)

	// DeleteExcessLogs 当总日志数超过 maxRecords 时，删除最旧的记录
	// 扫地大妈的策略二：按条数硬截断（比如不管几天，只要超过 10 万条，最老的全删）
	DeleteExcessLogs(maxRecords int) error

	// DeleteExcessTaskLogs 清理单个任务的超额日志
	// 防止某一个特别高频的任务（比如每秒跑一次）把总容量配额全抢光了。
	DeleteExcessTaskLogs(taskID uint, maxLogs int) error

	// ---- 组执行日志 ----

	// CreateGroupLog 插入一条新的组（Group）执行日志
	CreateGroupLog(log *model.GroupExecutionLog) error

	// SaveGroupLog 更新一条已有的组执行日志
	SaveGroupLog(log *model.GroupExecutionLog) error

	// DeleteGroupLogsBefore 按时间清理组日志
	DeleteGroupLogsBefore(cutoff time.Time) (int64, error)

	// DeleteExcessGroupLogs 按条数硬截断清理组日志
	DeleteExcessGroupLogs(maxRecords int) error
}

