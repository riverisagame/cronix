// ============================================================
// internal/application/service/interfaces.go - 服务层依赖反转接口定义
//
// 【纳米级源码说明书 - 架构篇】
// 这里的角色是“甲方的岗位说明书（JD）”。
// 业务逻辑层（Service）需要让调度引擎（Scheduler）去干活，但它不想和具体的引擎“绑死”。
// 所以它定义了这些接口（JD）：不管你是谁，只要你能干这些活，我就能用你。
//
// ============================================================
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：什么是依赖倒置原则（DIP）？为什么要这么写？
// 答（小白比喻）：
// 假设你是一个老板（Service），你需要员工帮你送快递。
// 错误的做法是：你在合同里写“我只用顺丰快递的张三”。这叫【强耦合】。万一张三辞职了，你的公司就瘫痪了。
// 正确的做法是：你在合同（接口 Interface）里写：“我需要一个能【收货】和【送货】的人”。
// 这时，无论是顺丰、中通还是邮政，只要符合这个标准，都能来上班。
// 这就是依赖倒置：高级部门（Service）不依赖低级部门（具体的Scheduler实现），双方都依赖“抽象标准（接口）”。
//
// 2. 面试官问：为什么同级包里也要定义接口（比如 StatsInvalidator）？
// 答：为了【可测试性（Mocking）】。如果你想单独测试 A 代码，但 A 强依赖 B，你不得不同时把 B 也跑起来，
// 这就成了集成测试。有了接口，测试时就可以塞一个“假员工（Mock对象）”给 A，轻松验证 A 的逻辑。
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

