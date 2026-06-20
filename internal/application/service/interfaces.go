// ============================================================
// internal/application/service/interfaces.go - 服务层依赖反转接口定义
//
// 【纳米级源码说明书 - 架构篇】
// 这里的角色是“甲方的岗位说明书（JD）”。
// 业务逻辑层（Service）需要让调度引擎（Scheduler）去干活，但它不想和具体的引擎“绑死”。
// 所以它定义了这些接口（JD）：不管你是谁，只要你能干这些活，我就能用你。
//
// ============================================================
// 💡 【大厂面试·底层原理扩展：控制反转与防腐层设计】
// 
// 场景重现 1：
// 面试官问：什么是依赖倒置原则（DIP）？为什么要这么写？
//
// 底层剖析与大厂对冲方案（IoC 与 接口契约）：
// 1. 强耦合的灾难：假设你是一个老板（Service），你需要员工帮你送快递。如果你在代码里直接 import `scheduler.Engine` 结构体，
//    这就叫【强耦合】（你只用顺丰快递的张三）。万一以后微服务拆分，或者要换一套分布式的 Temporal 调度器，你的代码要全量重构。
// 2. 接口隔离（Interface Segregation）：正确的做法是，你在合同（接口 Interface）里写：“我需要一个能 UpdateTaskSchedule 的人”。
//    这时，不管底层实现怎么变，Service 层纹丝不动。这就是依赖倒置：高层（Service）不依赖低层（具体的Scheduler实现），双方都依赖“抽象标准（接口）”。
//
// 场景重现 2：
// 面试官问：为什么同级包里也要定义接口（比如 StatsInvalidator）？
// 
// 底层剖析与大厂对冲方案（Mock 测试与防环形依赖）：
// 1. 斩断循环依赖：如果 `TaskService` 引用了 `ExecutionService`，反过来又互相引用，Go 编译器会直接报 `import cycle not allowed`。
//    用接口作为桥梁，可以完美切断物理包层面的环形依赖。
// 2. 可测试性（Testability）：这是大厂的红线。你想单独跑 `TaskService` 的单元测试，如果没有接口，你就得连上真实的 DB、真实的 Redis。
//    有了接口，测试框架（如 GoMock）一秒钟就能生成一个假的 `StatsInvalidator` 塞进去，让单元测试在纳秒级跑完（不需要任何外部环境）。
//
// ============================================================
package service

import "cronix/internal/domain/model"

// TaskReloader 是调度引擎对外暴露的任务更新接口
// 将 scheduler.Engine 抽象为接口，打破 service 对 scheduler 具体指针的强依赖
// 【大厂实践】：微服务架构中，哪怕现在只有一个实现类，也会先定接口，为以后扩展（比如改成分布式调度）留后路。
type TaskReloader interface {
	UpdateTaskSchedule(task model.Task) error // 新增或更新一个定时任务的打铃时间
	RemoveTaskSchedule(id uint)               // 删除一个定时任务，把打铃器拆掉
}

// GroupReloader 是调度引擎对外暴露的任务组更新接口
type GroupReloader interface {
	UpdateGroupSchedule(group model.TaskGroup) error // 整个项目组（DAG图）的调度更新
	RemoveGroupSchedule(id uint)                     // 把整个项目组从引擎里删掉
	UpdateTaskSchedule(task model.Task) error        // 用于组成员变动时同步更新任务（因为组里包含单个任务）
}

// DaemonReloader 是常驻任务控制器对外暴露的热更新接口
// 将 scheduler.DaemonMonitor 抽象为接口，打破物理耦合
type DaemonReloader interface {
	ReloadDaemon(task model.Task) // 重启常驻保安（比如改了他要监控的进程路径）
	StopDaemon(taskID uint)       // 辞退常驻保安（停止常驻任务）
}

// StatsInvalidator 是执行日志服务（ExecutionService）对外暴露的缓存失效接口
// 解决 service 内部同级结构的互相直接引用（虽然在同一个包，但抽象出来更利于单元测试）
// 【大厂实践】：缓存一致性问题中，写操作（比如删除了日志）必须通知缓存清空。这叫 Cache Invalidation。
type StatsInvalidator interface {
	InvalidateStatsCache() // 撕掉旧报表（清空缓存），下次有人看报表时必须重新统计
}

