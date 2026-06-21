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
// 📌 【大厂面试·核心考点】：Go语言的鸭子类型（Duck Typing）与接口设计
// 面试官：Go的接口和其他语言（如Java）有什么不同？优劣势是什么？
// 标准答案：
// 1. 隐式实现（Duck Typing）：在Go中，"如果它走起来像鸭子，叫起来像鸭子，那它就是鸭子"。结构体不需要显式声明 `implements Interface`。
// 2. 优势：极致的解耦与灵活性。比如我们在 `service` 层定义了接口，不管外部的 `scheduler` 是怎么实现的，只要实现了对应的方法就能注入。这种非侵入式设计使得我们可以轻松地为第三方库的代码抽象接口（你无法修改第三方库源码去写 implements）。
// 3. 劣势：缺乏编译期的强约束提示，重构时如果不小心改了方法签名，只有在使用该接口赋值的地方才会报错，而不是在实现处。此外，运行时通过 `itab` 进行动态派发，会比直接调用有轻微的性能损耗（约 1-2ns 的额外开销）。
//
// 🔬 【底层原理·深度剖析】：依赖倒置原则（DIP）的终极解释
// 就像现实生活中，老板（高层模块）不需要知道员工（低层模块）是坐地铁还是开车来上班，老板只定KPI（接口）。
// DIP 的核心在于：高层模块不应该依赖低层模块，两者都应该依赖其抽象；抽象不应该依赖细节，细节应该依赖抽象。
// 在本文件中：`service` 包是高层应用逻辑，`scheduler` 是低层基础设施。如果不定义这里的接口，`service` 就要 `import "cronix/internal/scheduler"`，这就破坏了洋葱架构的依赖从外向内的原则。通过在 `service` 内定义所需能力的接口，我们将依赖方向倒置：现在变成了 `scheduler` 依赖于 `service` 所定义的接口标准。这就是好莱坞原则：“Don't call us, we'll call you.”
//
// ============================================================
package service

import "cronix/internal/domain/model"

// TaskReloader 是调度引擎对外暴露的任务更新接口
// 将 scheduler.Engine 抽象为接口，打破 service 对 scheduler 具体指针的强依赖
// 【大厂实践】：微服务架构中，哪怕现在只有一个实现类，也会先定接口，为以后扩展（比如改成分布式调度）留后路。
// 
// 🏗️ 【架构设计·模式对比】：接口隔离原则 (ISP) 与最小权限法则
// 正确做法（当前）：定义专门的 `TaskReloader`，只给调用方提供 `Update` 和 `Remove` 两个必要能力。
// 错误做法：直接传递一个包含几十个方法的 `Scheduler` 庞大核心接口（胖接口）。
// 理由：如果你传了一个包含 `StopAllTasks()` 的胖接口给某个只需要更新单个任务的业务逻辑，这就像把核弹发射按钮交给了一个只负责送信的快递员。一旦该服务被攻破或代码写错，整个调度系统可能被意外清空。遵循 ISP，按需分配接口，是系统安全性与稳定性的重要基石。
type TaskReloader interface {
	UpdateTaskSchedule(task model.Task) error // 新增或更新一个定时任务的打铃时间
	RemoveTaskSchedule(id uint)               // 删除一个定时任务，把打铃器拆掉
}

// GroupReloader 是调度引擎对外暴露的任务组更新接口
//
// 💀 【踩坑血泪·反面教材】：不要把接口定义在实现方
// 真实生产事故：某业务线把 `SchedulerInterface` 定义在了底层的 `scheduler` 包里，然后高层的 `service` 包去引用它。
// 后来为了解耦，他们想把 `scheduler` 拆分出去做成独立的微服务（通过gRPC调用）。结果发现 `service` 层满地都是对 `scheduler.SchedulerInterface` 的强依赖，导致重构时出现灾难级的导包错误。
// 经验教训：接口是给调用方（Consumer）用的，必须定义在调用方的包里（Consumer Header Pattern）。
// 这里的 `GroupReloader` 就是在调用方（service包）定义的。这样即使底层的组调度引擎怎么换，我们的 Service 业务逻辑代码连 `import` 路径都不用改！这才是真正的依赖倒置。
type GroupReloader interface {
	UpdateGroupSchedule(group model.TaskGroup) error // 整个项目组（DAG图）的调度更新
	RemoveGroupSchedule(id uint)                     // 把整个项目组从引擎里删掉
	UpdateTaskSchedule(task model.Task) error        // 用于组成员变动时同步更新任务（因为组里包含单个任务）
}

// DaemonReloader 是常驻任务控制器对外暴露的热更新接口
// 将 scheduler.DaemonMonitor 抽象为接口，打破物理耦合
//
// ⚡ 【性能实战·生产调优】：接口动态派发（Dynamic Dispatch）的性能损耗与对冲
// 很多初级开发者盲目追求性能，排斥使用接口。让我们用底层数据对冲：
// 1. 直接调用指针方法：耗时约 1.5ns（通常可被编译器进行内联优化消除开销）。
// 2. 通过接口调用：耗时约 2.5ns - 3ns，因为需要通过 `itab`（Interface Table）动态查找到运行时的真实类型方法地址，然后进行间接跳转调用（Indirect Call），且阻碍了编译器的内联优化。
// 3. 生产对冲策略：这 1-2 纳秒的差距在绝大多数 IO 密集型业务（如这里的守护进程调度，通常涉及磁盘或系统调用）中可以完全忽略不计。只有对于极高频的纳秒级热点循环（如每秒百万次内存解析），才应尽量避免接口派发。在此场景下，用 1ns 的微小开销换取架构的极致解耦，是高内聚低耦合的最佳实践。
type DaemonReloader interface {
	ReloadDaemon(task model.Task) // 重启常驻保安（比如改了他要监控的进程路径）
	StopDaemon(taskID uint)       // 辞退常驻保安（停止常驻任务）
}

// StatsInvalidator 是执行日志服务（ExecutionService）对外暴露的缓存失效接口
// 解决 service 内部同级结构的互相直接引用（虽然在同一个包，但抽象出来更利于单元测试）
// 【大厂实践】：缓存一致性问题中，写操作（比如删除了日志）必须通知缓存清空。这叫 Cache Invalidation。
//
// 🧪 【测试工程·质量保障】：Mock 测试驱动设计 (TDD) 的护城河
// 为什么我们在同级目录，没有物理依赖环路的情况下还要搞个接口？
// 场景：我们要写 `ExecutionService.AddLog()` 的单元测试，它内部需要调用 `StatsInvalidator` 清除统计缓存。
// 如果直接依赖具体的对象，每次跑单元测试还得先搭一个 Redis 环境来模拟缓存，测试脆弱且极其缓慢！
// 1. 物理层隔离与 Mock 注入：有了接口，我们可以用 `mockgen` 自动生成一个 `MockStatsInvalidator`（鸭子类型的天然优势）。
// 2. 行为验证：在测试用例中写 `mockStats.EXPECT().InvalidateStatsCache().Times(1)` 验证行为逻辑。
// 3. 收益对冲：测试物理级零污染。测试过程不需要任何外部真实的 DB 或 Redis，响应时间从毫秒级降至微秒级。我们甚至可以主动注入延迟或故障，测试并发安全。没有接口作为边界，这些都将成为妄想。
type StatsInvalidator interface {
	InvalidateStatsCache() // 撕掉旧报表（清空缓存），下次有人看报表时必须重新统计
}

