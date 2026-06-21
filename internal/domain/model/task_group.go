// ============================================================
// internal/domain/model/task_group.go - 任务组模型
//
// ============================================================
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 面试官问：为什么要单独搞一张 task_groups 表，而不是在 tasks 表里加一个字段存“组长是谁”？
// 答：
// 涉及数据库设计的核心原则——【数据库范式（Database Normalization）】。
// 
// 📌 图解 1对多 关系表设计：
// 假设我们要把几个任务绑成一个组，有两种做法：
// 
// ❌ 错误做法（扁平塞一起，叫冗余）：
// [Tasks 表]
// 任务A | 属于：数据备份组 | 组模式：并行   | 组开启状态：开
// 任务B | 属于：数据备份组 | 组模式：并行   | 组开启状态：开
// -> 缺点：有一天你想把“并行”改成“串行”，你需要去改两行！如果有1000个任务在这个组，你要改1000次，这叫“数据异常”。
// 
// ✅ 正确做法（分离解耦，符合三大范式）：
// [Task_Groups 组表] 
// 组ID=1 | 组名：数据备份组 | 组模式：并行 | 状态：开
// 
// [Tasks 任务表]
// 任务A | 挂靠GroupID=1
// 任务B | 挂靠GroupID=1
// -> 优点：改组模式，只需要改 [组表] 里的一行代码，下面挂靠的1000个任务自动生效！非常清爽！
// ============================================================
package model

import "time"

// 🏗️ 【架构设计·模式对比】
// 微服务边界划分下的聚合根(Aggregate Root)设计
// 1. 初二小白比喻：聚合根就像是一个公司的“部门经理”（TaskGroup），对外代表整个部门。外部如果有事只能找部门经理，不能直接越过经理去给底下的“基层员工”（Task）派活。
// 2. DDD视角（领域驱动设计）：在 DDD 中，TaskGroup 是一个典型的 **聚合根 (Aggregate Root)**。聚合是一组强关联领域对象的集合，用来保证业务数据的原子性和一致性。
// 3. 选型理由：为什么要这样划分边界？如果直接修改 Task 而无视 TaskGroup，可能会导致数据违规（比如：组已经被禁用了，但是有人绕过组直接把组内某个任务强行开启）。
// 4. 最佳实践：所有对 Task 生命周期的管理（创建、修改、启停），都应当且仅应当通过 TaskGroup 这个“大门”作为入口进行控制，这叫做聚合根保护业务不变量。
//
// 🔬 【底层原理·深度剖析】
// 数据库级联删除与外键约束在分布式的利弊
// 1. 传统单体时代：以前喜欢用数据库物理外键（Foreign Key）配合 `ON DELETE CASCADE`。删了组，数据库底层自动把所属小弟全删了，代码写着很爽。
// 2. 微服务/分布式时代的抛弃：在海量并发下，物理外键被**强烈抵制**（如阿里Java开发手册强制要求禁止使用物理外键）。
//    - 性能损耗：每次对Task做插入或更新，数据库都要偷偷去查一下外键表（看组在不在），极度消耗数据库 CPU。
//    - 死锁风险：并发更新时，外键约束极易引发隐式的共享锁（S锁）等待，导致数据库大面积死锁卡壳。
//    - 分库分表灾难：当 TaskGroup 和 Task 随着业务壮大被拆分到了不同的物理数据库实例时，物理外键直接抓瞎，根本无法跨库生效！
// 3. 替代方案：在代码逻辑层实现“逻辑外键”（只存 GroupID，不建物理约束），并且使用“软删除（Soft Delete）”或基于 MQ 的异步事件来实现分布式场景下的级联清理（最终一致性）。

// TaskGroup represents a logical group of tasks with a shared execution mode.
// 任务组：代表了一系列任务的“带头大哥”。它可以号令下面的一群小弟（任务）按照某种队形去干活。
//
// 队形模式 (Mode)：
// mode="parallel" — 【乱拳打死老师傅】所有任务同时跑，并发执行（在 DAG 里相当于同一层）。
// mode="sequential" — 【排队买单】按照 tasks 表里的 sort_order 序号，一个接一个跑。谁倒下，后面的就不用跑了。
// mode="dag" — 【结网布阵】按照任务之间的依赖关系（这活必须等那活干完才能干），层层推进。
type TaskGroup struct {
    // 组长胸牌号：自增主键
    ID          uint      `gorm:"primaryKey" json:"id"`
    
    // 📌 【大厂面试·核心考点】
    // 面试官杀手锏：如果你在这里增加了一个 `Tasks []Task` 来表示 HasMany 关联关系，
    // 在查询所有任务组及旗下任务时，会遇到什么性能灾难？怎么解决？
    // 答：
    // - 灾难现场（N+1问题）：如果直接 db.Find(&groups)，然后 for 循环去 db.Model(&group).Association("Tasks").Find(&tasks)。
    //   假设有 100 个任务组，数据库会被查询 1次(查组) + 100次(循环查小弟) = 101 次查询！网络 IO 直接拉胯，瞬间把数据库打满。
    // - 标准解法（预加载 Eager Loading）：必须使用 GORM 的 db.Preload("Tasks").Find(&groups)。
    //   底层原理：GORM 会先把所有组查出来，然后在这边内存里把所有组 ID 提取成一个数组，
    //   最后拼接一条 IN 语句：`SELECT * FROM tasks WHERE group_id IN (1, 2, 3...)`。把 101 次查询暴力优化成了短短的 **2 次查询**！
    //
    // 💀 【踩坑血泪·反面教材】
    // 某一线大厂真实 P0 级生产事故：
    // 一位实习生在全表扫描导出 TaskGroup 报表数据时，在长长的 for 循环里隐式触发了延迟加载（Lazy Loading）获取关联小弟数据。
    // 表里当时有 10 万个组，这小段代码瞬间对 RDS 数据库发起了 10 万次小巧的 Select 请求。
    // 结果瞬间把数据库的连接池占满打爆，导致线上所有核心业务连不上数据库，系统全面宕机。
    // 启示：ORM 框架是一把双刃剑，它用优雅掩盖了 SQL 的复杂，但也极其容易隐藏“性能毒药”。代码 Review 必查 N+1，生产环境建议开启慢查询日志。
    
    // 组长的外号（唯一不重复）
    Name        string    `gorm:"uniqueIndex;not null" json:"name"`
    
    // 组的说明书
    Description string    `json:"description,omitempty"`
    
    // 组长带队风格（队形）
    Mode        string    `gorm:"default:parallel" json:"mode"` // "parallel" or "sequential" or "dag"
    
    // 这个组是否还在营业。false 就被整体封杀了。
    Enabled     bool      `gorm:"default:true" json:"enabled"`
    
    // 这个组统一的定时闹钟。如果是空的，那只能靠人手工点击触发。
    CronExpr    string    `json:"cron_expr,omitempty"`
    
    // 建组时间
    CreatedAt   time.Time `json:"created_at"`
    // 上次改组规的时间
    UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 是 GORM 的接头暗号。告诉它：这个模型对应的是数据库里哪一张真表。
func (TaskGroup) TableName() string {
    return "task_groups"
}
