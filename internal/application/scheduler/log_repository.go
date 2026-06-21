// ============================================================
// internal/scheduler/log_repository.go - 执行日志仓储接口
//
// 【纳米级源码说明书 - 架构篇】
// 这是什么？这是一个"接口（Interface）"。
// 接口就像是公司里的一份《岗位职责说明书》，规定了"日志管理员"每天要干什么活，
// 但绝不规定他是用什么工具（MySQL、SQLite、MongoDB还是写记事本）干的。
//
// 面试官问：为什么要定义这个接口，而不是直接在业务代码里写 SQL 操作数据库？
// 答（小白秒懂版）：
// 如果车间主任（Executor）直接用 GORM 操作 SQLite 数据库，那他就和 SQLite "绑死"了。
// 以后公司做大了，老板说："我们要把日志存到云端的 ElasticSearch 或者 MongoDB 去！"
// 那你就得把车间主任的代码全部改一遍，很容易改出 Bug。
//
// 现在有了这个接口（岗位说明书），车间主任只管发号施令："给我存一条日志！"
// 具体是谁去存？是底层的"实习生（具体的结构体，比如 GormLogRepository）"去存的。
// 以后换数据库，只需要新招一个实习生就行了，车间主任的代码一行都不用改！
// 这在设计模式中叫做【依赖倒置原则（Dependency Inversion Principle）】。
//
// @Ref: docs/sps/plans/20260612_arch_hardening_plan.md | @Date: 2026-06-12
// ============================================================
//
// ┌─────────────────────────────────────────────────────────────────────────────┐
// │                    📌 【大厂面试·核心考点总览】                                │
// │                                                                             │
// │  Q1: 什么是 Repository Pattern？它在 DDD 六边形架构中处于什么层？               │
// │  A1: Repository（仓储）是 DDD 中"领域层"和"基础设施层"之间的桥梁。                │
// │      它属于"应用层"或"领域层"的抽象定义，但其实现住在"基础设施层"。                 │
// │      生活比喻：你去银行柜台（Repository 接口）说"我要存钱"，                     │
// │      至于银行后台是用保险箱（MySQL）还是纸袋子（SQLite）存的，你不用管。           │
// │      六边形架构（Hexagonal Architecture）中，Repository 接口是                   │
// │      "端口（Port）"，具体的 GormLogRepository 是"适配器（Adapter）"。            │
// │                                                                             │
// │  Q2: 接口隔离原则（ISP）在这个文件中的体现？                                     │
// │  A2: 这个接口只定义了"日志增删改查"相关的方法，绝不掺入"任务调度"               │
// │      "用户管理"等无关方法。如果把所有数据库操作塞进一个 GodRepository             │
// │      万能接口，那么只想操作日志的模块也被迫依赖"用户管理"的方法签名，              │
// │      违反了 ISP。ISP 的核心理念：调用方不应该被迫依赖它不使用的方法。              │
// │      反面教材：一个 1000 行的 IDatabase 接口，里面什么都有，修改任何                │
// │      一个方法签名，全公司 200 个模块都要重新编译——这就是"胖接口"地狱。           │
// │                                                                             │
// │  Q3: 依赖倒置原则（DIP）怎么理解？                                              │
// │  A3: 传统依赖：高层模块（Scheduler）→ 直接依赖 → 低层模块（GORM/SQLite）        │
// │      DIP 后：高层模块（Scheduler）→ 依赖抽象接口（LogRepository）               │
// │              低层模块（GormLogRepository）→ 实现抽象接口（LogRepository）        │
// │      箭头反转了！高层不再依赖低层的实现细节，而是大家都依赖抽象。                  │
// │      Robert C. Martin（Uncle Bob）的原话：                                      │
// │      "High-level modules should not depend on low-level modules.              │
// │       Both should depend on abstractions."                                    │
// │                                                                             │
// │  Q4: Go 的接口 vs Java 的接口有什么本质区别？                                   │
// │  A4: Go 是隐式实现（Duck Typing / Structural Typing），                        │
// │      只要结构体拥有接口定义的所有方法签名，它就自动"实现"了该接口，               │
// │      不需要写 `implements` 关键字。                                             │
// │      Java 是显式实现（Nominal Typing），必须在类声明上写                         │
// │      `class Foo implements LogRepository`，否则编译器不认。                      │
// │                                                                             │
// │      Go 隐式实现的优势：                                                       │
// │        ① 解耦更彻底——实现方甚至可以不知道接口的存在（第三方包也能适配）          │
// │        ② 支持"先写实现，后抽接口"的敏捷开发模式                                 │
// │        ③ 跨包依赖更少——接口定义在调用方所在的包里（Go 最佳实践）                 │
// │      Go 隐式实现的劣势：                                                       │
// │        ① 编译器不会在实现处告诉你"你漏实现了哪个方法"                             │
// │           解决方案：在实现文件中加 var _ LogRepository = (*GormLogRepository)(nil) │
// │        ② IDE 的"查找所有实现"功能不如 Java 精确                                 │
// │                                                                             │
// │  Q5: 这个接口为什么定义在 application/scheduler 包，而不是 domain 包？          │
// │  A5: 这是一个务实的选择。纯 DDD 教科书会把 Repository 接口放在 domain 层，       │
// │      但在 Go 社区的最佳实践中，接口应该定义在"使用方"所在的包里                   │
// │      （Accept interfaces, return structs），这样可以最大化解耦。                 │
// │      Scheduler 是 LogRepository 的唯一消费者，所以接口定义在 scheduler 包       │
// │      完全合理，这样 domain/model 包零依赖，更加纯净。                            │
// └─────────────────────────────────────────────────────────────────────────────┘
//
// 🔬 【底层原理·深度剖析】Go 接口的内存表示
//
//   Go 的接口在运行时是一个叫 iface 的结构体（位于 runtime/runtime2.go）：
//
//     type iface struct {
//         tab  *itab          // 方法表指针（指向接口类型和实现类型的方法映射表）
//         data unsafe.Pointer // 数据指针（指向实际的 GormLogRepository 实例）
//     }
//
//   当你把一个 *GormLogRepository 赋值给 LogRepository 变量时，Go 编译器会：
//   1. 生成一个 itab（Interface Table），里面存着 LogRepository 接口的每个方法
//      在 GormLogRepository 上对应的函数指针（类似 C++ 的虚函数表 vtable）
//   2. 把 itab 指针和数据指针打包成 iface 结构体
//   3. 调用接口方法时，runtime 通过 itab 查表找到真正的函数地址，再间接调用
//
//   性能影响：接口方法调用比直接方法调用多一次指针解引用（~1-2ns），
//   在 99.9% 的场景中可以忽略不计。但在超高频热路径（如每秒百万次的 codec）中
//   可以考虑使用泛型（Go 1.18+）替代接口来消除这个开销。
//
// ⚡ 【性能实战·生产调优】接口 vs 泛型 vs 直接调用的性能对比
//
//   | 调用方式       | 单次开销（ns） | 适用场景                        |
//   |----------------|---------------|---------------------------------|
//   | 直接方法调用    | ~0.3ns        | 内部工具函数，不需要多态          |
//   | 接口方法调用    | ~1.5ns        | 需要多态/依赖注入的业务逻辑（本文件）|
//   | 泛型实例化      | ~0.3ns        | 高频热路径 + 编译期多态            |
//   | 反射调用        | ~200ns        | 框架内部，业务代码绝对禁止使用      |
//
//   结论：本文件使用接口是完全正确的选择——日志仓储的调用频率远低于 codec 级别，
//   但获得的可测试性和可替换性收益巨大（例如单元测试中可以注入 MockLogRepository）。
//
// 🏗️ 【架构设计·模式对比】Repository 的几种实现策略
//
//   | 策略                     | 优点                          | 缺点                           |
//   |--------------------------|-------------------------------|--------------------------------|
//   | ① 直接在 Service 里写 SQL | 简单快速，适合一次性脚本         | 完全不可测试，换库就死            |
//   | ② DAO（Data Access Object）| 数据访问集中管理               | 容易膨胀成"上帝 DAO"             |
//   | ③ Repository（本文件）    | DDD 标配，强解耦，可 Mock 测试  | 多一层抽象，小项目有过度设计之嫌   |
//   | ④ CQRS                   | 读写分离，超大规模系统           | 复杂度爆炸，需要事件溯源配合      |
//
//   本项目选择方案③，因为 Cronix 是一个需要长期维护的调度系统，
//   Repository 带来的可测试性和可替换性，远大于多一层抽象的认知成本。
//
// 💀 【踩坑血泪·反面教材】胖接口导致的真实事故
//
//   某公司的 IRepository 接口包含 50+ 方法，涵盖用户、订单、商品、日志等所有表。
//   某天，一个实习生给"用户注销"方法改了签名（加了个 context 参数），
//   导致全公司所有 7 个微服务的编译全部失败——因为每个服务都引用了这个万能接口，
//   即使它们根本不使用"用户注销"功能。
//   修复方式：把一个胖接口拆成 N 个小接口（ISP 接口隔离原则），
//   每个小接口只包含 3-5 个内聚方法。本文件的 LogRepository 就是这种最佳实践。
//
// ============================================================
package scheduler

// 📦 【import 深度解析】
import (
	// model 包：定义领域实体（Domain Entity），如 ExecutionLog、GroupExecutionLog。
	// 📌 注意：这里只依赖 model 包（纯数据结构），绝不依赖 GORM、SQL 等基础设施包。
	// 这正是 DIP 的体现——接口层只知道"领域对象长什么样"，不知道"数据库怎么操作"。
	//
	// 🔬 如果这里 import 了 "gorm.io/gorm"，那就意味着接口层和 GORM 耦合了，
	// 以后想换成 MongoDB 驱动，你连接口文件都得改——这就彻底违反了 DIP。
	"cronix/internal/domain/model"

	// time 包：Go 标准库的时间处理包。
	// 用于 CleanupOrphanedLogs(now time.Time) 和 DeleteLogsBefore(cutoff time.Time)。
	// 📌 为什么不用 int64 的 Unix 时间戳？因为 time.Time 是类型安全的，
	// 编译器能阻止你把"文件大小"和"时间戳"搞混——int64 做不到这一点。
	// 而且 time.Time 自带时区信息（Location），避免了跨时区 Bug。
	"time"
)

// ============================================================
// 🏗️ 【架构设计·接口职责拓扑图】
//
//   调用链路：
//   Scheduler（调度器）→ LogRepository（接口）→ GormLogRepository（GORM 实现）→ SQLite/MySQL
//                                            → MockLogRepository（测试 Mock）→ 内存 Map
//                                            → ESLogRepository（未来扩展）→ ElasticSearch
//
//   关键洞察：Scheduler 只持有 LogRepository 接口的引用，
//   具体实现在 main.go 或 wire.go 中通过"依赖注入"绑定。
//   这意味着：
//     - 单元测试时：注入 MockLogRepository，零数据库依赖，测试飞快（<1ms）
//     - 集成测试时：注入 GormLogRepository + SQLite :memory:，真实但轻量
//     - 生产环境时：注入 GormLogRepository + MySQL/PostgreSQL，全量功能
//
// 📌 【大厂面试·高频追问】
//   Q: 为什么方法参数用指针 *model.ExecutionLog 而不是值 model.ExecutionLog？
//   A: 三个原因：
//      ① 性能：ExecutionLog 结构体可能包含 string、time.Time 等字段，
//         按值传递会触发内存拷贝（~100-500 bytes），指针传递只需 8 bytes（64位系统）。
//      ② 语义：Create 操作后，GORM 会回填 ID 字段（AutoIncrement），
//         如果按值传递，调用方拿不到新生成的 ID——因为 Go 是值传递语义。
//      ③ 一致性：GORM 的 Create/Save 方法本身要求指针参数。
//
//   Q: 为什么返回值是 error 而不是 (bool, error) 或自定义错误类型？
//   A: Go 的惯用法（idiom）：能用 error 就用 error，别过度设计。
//      如果需要区分"记录不存在"和"数据库连接断开"，在实现层用
//      errors.Is(err, gorm.ErrRecordNotFound) 判断即可，不需要在接口层暴露。
//      接口层越简单，实现方的自由度越大。
// ============================================================

// LogRepository 定义执行日志的存储操作接口（日志管理员的岗位说明书）
// 所有对 execution_logs 和 group_execution_logs 表的增删改查都通过此接口进行
//
// 🔬 【底层原理·深度剖析】Go 接口的编译期检查机制
//   Go 编译器在编译期会检查"接口赋值"处的类型兼容性：
//     var repo LogRepository = NewGormLogRepository(db)  // 编译期检查
//   如果 GormLogRepository 少实现了任何一个方法，编译器会报错：
//     "GormLogRepository does not implement LogRepository (missing method XXX)"
//   但这个检查只发生在"赋值处"，如果你从不把它赋值给接口变量，
//   编译器不会主动告诉你"你漏了方法"。
//   所以 Go 社区有一个惯用的编译期断言技巧（见 log_repository_gorm.go 的注释）。
//
// 🛡️ 【安全攻防·漏洞防线】接口层的安全考量
//   ① 所有删除方法（DeleteLogsBefore, DeleteExcessLogs 等）都需要在实现层
//     加入参数校验——比如 cutoff 不能是未来时间，maxRecords 不能是负数。
//   ② CleanupOrphanedLogs 的 now 参数由调用方传入而非内部 time.Now()，
//     这是为了可测试性（测试时可以传入固定时间），但生产环境中调用方
//     必须确保传入的是真实的当前时间，否则会误杀正在运行的合法任务。
//   ③ 接口不暴露原始 *gorm.DB，防止调用方绕过 Repository 直接操作数据库。
type LogRepository interface {
	// ---- 单任务执行日志 ----

	// CreateExecutionLog 插入一条新的执行日志（发车：状态通常为 running）
	//
	// 📌 【大厂面试·核心考点】
	//   Q: 为什么 Create 和 Save 要分成两个方法？能不能用一个 Upsert 代替？
	//   A: 语义不同。Create 对应 SQL INSERT，要求记录不存在；Save 对应 SQL UPDATE，要求记录已存在。
	//      Upsert（INSERT ... ON CONFLICT UPDATE）虽然功能上可以合并，但它模糊了业务意图：
	//      - 如果本该 Create 的场景意外走了 Update 分支，说明有 Bug（比如重复触发），
	//        Upsert 会默默吞掉这个错误，导致你永远发现不了问题。
	//      - 分开两个方法，让调用方明确表达意图："我是在创建新记录"还是"我在更新已有记录"。
	//
	// ⚡ 【性能实战】
	//   单条 INSERT 在 SQLite 上的延迟约 50-200μs（微秒），瓶颈在 fsync 磁盘同步。
	//   如果未来需要批量写入（如高频任务每秒产生 100+ 日志），应在实现层增加
	//   BatchCreateExecutionLogs 方法，利用 GORM 的 CreateInBatches 将多条
	//   INSERT 合并为一次磁盘 fsync，性能可提升 10-50 倍。
	CreateExecutionLog(log *model.ExecutionLog) error

	// SaveExecutionLog 更新一条已有的执行日志（到站：更新状态为 success/failed、记录结束时间等）
	//
	// 💀 【踩坑血泪】Save vs Updates 的陷阱
	//   GORM 的 Save() 会更新所有字段（包括零值），这意味着：
	//   如果你只想更新 status 和 end_time，但 ExecutionLog 的 output 字段恰好是空字符串""，
	//   Save() 会把 output 也覆盖为 ""——即使数据库里原来有内容！
	//   更安全的做法是用 Updates(map[string]interface{}{...}) 只更新需要的字段。
	//   但本项目选择 Save() 是合理的，因为 ExecutionLog 是一个"完整状态对象"，
	//   调用方在 Save 前总是会先把所有字段都填好，不存在"部分更新"的场景。
	SaveExecutionLog(log *model.ExecutionLog) error

	// CountRunningLogs 统计指定任务当前处于 running 且未结束的日志条数
	// 【防重击穿神器】：防止同一个任务被同时触发 100 次，挤爆服务器。
	//
	// 🔬 【底层原理·并发控制】
	//   这个方法是 Scheduler 实现"最大并发数控制"的基石。
	//   调用链：Scheduler.executeTask() → CountRunningLogs(taskID) → 如果 count >= maxConcurrency → 跳过本次触发
	//
	//   ⚠️ 并发安全警告：在高并发场景下，"先 Count 再决策"存在 TOCTOU 竞态：
	//     T1: Count() → 返回 0 → 决定执行
	//     T2: Count() → 返回 0 → 决定执行（此时 T1 还没来得及 Create）
	//     结果：两个任务同时执行了！
	//   Cronix 的解决方案：SQLite 天然串行写入（WAL 模式下写锁互斥），
	//   加上 Scheduler 本身是单 goroutine 事件循环，所以不存在此竞态。
	//   但如果未来迁移到 MySQL/PostgreSQL 多实例部署，必须引入分布式锁或数据库行锁。
	CountRunningLogs(taskID uint) (int64, error)

	// GetLatestTaskLog 获取指定任务的最新一条执行日志
	// 组任务（Sequential 模式）必须要看上一条日志是不是成功了，才会决定要不要跑下一条。
	//
	// 📌 【大厂面试·核心考点】
	//   Q: 如果指定 taskID 没有任何日志记录，这个方法应该返回什么？
	//   A: 返回 (nil, gorm.ErrRecordNotFound)。调用方需要用 errors.Is() 判断：
	//      if errors.Is(err, gorm.ErrRecordNotFound) { /* 正常情况：任务从未执行过 */ }
	//      这也是 Go 错误处理的最佳实践——用哨兵错误（Sentinel Error）区分"真正的错误"
	//      和"业务上的正常情况"。
	//
	// ⚡ 【性能实战】
	//   这个查询在 task_id 上建索引后，时间复杂度为 O(log N)（B-Tree 索引查找）。
	//   如果没有索引，就是全表扫描 O(N)，10 万条日志时延迟从 0.1ms 飙升到 50ms。
	GetLatestTaskLog(taskID uint) (*model.ExecutionLog, error)

	// CleanupOrphanedLogs 清理所有处于 running 状态但无结束时间的孤儿日志
	// 【系统自愈机制】：如果服务器突然断电，数据库里还有没跑完的任务状态。
	// 下次重启时，必须把它们揪出来，强制标记为 failed（失败），并写上 "系统崩溃"。
	//
	// 🔬 【底层原理·幂等性设计】
	//   "幂等"是什么？就像你按电梯按钮——按 1 次和按 100 次效果一样，电梯只来一次。
	//   这个方法天生是幂等的：
	//     第一次调用：找到 10 条孤儿日志 → 全部标记为 failed → 返回 nil
	//     第二次调用：找到 0 条孤儿日志（因为上次已经清完了）→ 啥也不做 → 返回 nil
	//   幂等性在分布式系统中极其重要——如果网络抖动导致方法被调用了两次，
	//   幂等方法不会造成任何数据不一致。
	//
	// 🏗️ 【架构设计·Crash Recovery 模式】
	//   这属于经典的"崩溃恢复（Crash Recovery）"模式：
	//   ① Write-Ahead Logging (WAL)：数据库层面的崩溃恢复（SQLite/MySQL 自带）
	//   ② Application-Level Recovery：应用层面的崩溃恢复（就是这个方法）
	//   两层配合，确保系统在任何时刻断电后都能恢复到一致状态。
	CleanupOrphanedLogs(now time.Time) error

	// DeleteLogsBefore 删除创建时间早于 cutoff 的执行日志
	// 扫地大妈的策略一：按时间过期清理（比如只保留 30 天的日志）
	//
	// ⚡ 【性能实战·生产调优】
	//   返回值 int64 表示实际删除的行数——这不仅仅是为了日志记录，
	//   更是为了让调用方做"流控"：如果一次删了 100 万条，说明清理周期太长了，
	//   应该缩短清理间隔（比如从每天一次改为每小时一次），避免单次删除造成
	//   长时间的表锁和 I/O 风暴。
	//
	// 💀 【踩坑血泪】
	//   生产事故案例：某公司的日志清理任务设置为"保留 365 天"，
	//   一年后第一次触发清理，一次性删除 5000 万条记录，
	//   导致 MySQL 主从复制延迟飙升到 30 分钟，期间所有读请求返回脏数据。
	//   解决方案：分批删除（每次最多删 1 万条，循环执行），本项目的
	//   DeleteExcessLogs 方法的"先查 ID 再删"思路就是这种分批策略的雏形。
	DeleteLogsBefore(cutoff time.Time) (int64, error)

	// DeleteExcessLogs 当总日志数超过 maxRecords 时，删除最旧的记录
	// 扫地大妈的策略二：按条数硬截断（比如不管几天，只要超过 10 万条，最老的全删）
	//
	// 🏗️ 【架构设计·双重清理策略】
	//   为什么要同时有"按时间清理"和"按条数清理"两种策略？
	//   - 按时间清理（DeleteLogsBefore）：保证"最近 N 天"的日志一定在
	//   - 按条数清理（DeleteExcessLogs）：保证磁盘不会被撑爆
	//   两者互补：
	//     场景A：任务很少，30天才100条 → 按时间清理就够了，按条数不会触发
	//     场景B：任务爆炸，1天就100万条 → 按时间来不及清，按条数兜底
	//   这叫"多维度防线"，和网络安全的"纵深防御（Defense in Depth）"是同一个思想。
	DeleteExcessLogs(maxRecords int) error

	// DeleteExcessTaskLogs 清理单个任务的超额日志
	// 防止某一个特别高频的任务（比如每秒跑一次）把总容量配额全抢光了。
	//
	// 📌 【大厂面试·核心考点】
	//   Q: 这个方法和 DeleteExcessLogs 有什么区别？为什么两个都要有？
	//   A: DeleteExcessLogs 是"全局总量控制"——整个系统最多 N 条日志；
	//      DeleteExcessTaskLogs 是"单任务配额控制"——每个任务最多 M 条日志。
	//      类比：公司总预算 1000 万（全局），但每个部门最多只能花 200 万（单任务），
	//      防止某个部门独吞全部预算。
	//      这是"公平调度（Fair Scheduling）"思想在日志管理中的应用。
	DeleteExcessTaskLogs(taskID uint, maxLogs int) error

	// ---- 组执行日志 ----
	//
	// 📌 【大厂面试·核心考点】
	//   Q: 为什么组日志和单任务日志的方法定义在同一个接口里，而不是拆成两个接口？
	//   A: 这是一个权衡。拆成 TaskLogRepository 和 GroupLogRepository 两个接口
	//      更符合 ISP，但在本项目中：
	//      ① 两者总是被同一个消费者（Scheduler）同时使用
	//      ② 两者的底层存储（同一个 SQLite 数据库）是一致的
	//      ③ 方法总数只有 12 个，远未达到"胖接口"的程度
	//      所以合并在一起是合理的，减少了不必要的类型数量。

	// CreateGroupLog 插入一条新的组（Group）执行日志
	CreateGroupLog(log *model.GroupExecutionLog) error

	// SaveGroupLog 更新一条已有的组执行日志
	SaveGroupLog(log *model.GroupExecutionLog) error

	// DeleteGroupLogsBefore 按时间清理组日志
	DeleteGroupLogsBefore(cutoff time.Time) (int64, error)

	// DeleteExcessGroupLogs 按条数硬截断清理组日志
	DeleteExcessGroupLogs(maxRecords int) error
}

