package service

import "cronix/internal/domain/model"

// TaskReloader 是调度引擎对外暴露的任务更新接口
// 将 scheduler.Engine 抽象为接口，打破 service 对 scheduler 具体指针的强依赖
type TaskReloader interface {
	UpdateTaskSchedule(task model.Task) error
	RemoveTaskSchedule(id uint)
}

// GroupReloader 是调度引擎对外暴露的任务组更新接口
type GroupReloader interface {
	UpdateGroupSchedule(group model.TaskGroup) error
	RemoveGroupSchedule(id uint)
	UpdateTaskSchedule(task model.Task) error // 用于组成员变动时同步更新任务
}

// DaemonReloader 是常驻任务控制器对外暴露的热更新接口
// 将 scheduler.DaemonMonitor 抽象为接口，打破物理耦合
type DaemonReloader interface {
	ReloadDaemon(task model.Task)
	StopDaemon(taskID uint)
}

// StatsInvalidator 是执行日志服务（ExecutionService）对外暴露的缓存失效接口
// 解决 service 内部同级结构的互相直接引用（虽然在同一个包，但抽象出来更利于单元测试）
type StatsInvalidator interface {
	InvalidateStatsCache()
}
