// ============================================================
// internal/domain/model/group_log.go - 任务组执行日志数据模型
//
// 🔬 【底层原理·深度剖析】分布式追踪系统(TraceID/SpanID)理论雏形
// [生活比喻]：想象一下你在网上买了一个包裹。系统会给你生成一个全网唯一的“总运单号”（类似于这里的 TraceID）。
// 包裹从杭州发往北京，中间会经过几十个分拨中心、快递员，每一个环节流转都会生成一个“子流水号”（类似于 SpanID）。
// 只要系统拿着总运单号去查，就能把所有子流水的记录全部串接起来，形成一棵完整的树状路径。
// [底层映射]：在这个 group_log 设计中，本条执行记录的主键 `ID` 其实就是在扮演 `TraceID` 的核心角色（全链路唯一标识）。
// 它代表了“这批任务”的整体生命周期。而挂靠在该 ID 下执行的具体每一个 Task（存在 execution_logs 表），
// 则扮演了 `SpanID` 的角色。这正是现代 APM（如 SkyWalking、Jaeger、Zipkin）的基石模型。
// 
// 📌 【大厂面试·核心考点】分布式链路透传
// 面试官问：如果要在微服务体系下扩展这个日志模型，确保能追查到触发这个任务的 HTTP 接口请求，该怎么做？
// 标准答案：
// 1. 生成层：在请求网关（如 Nginx/API Gateway）分配一个全局唯一的 X-Trace-Id。
// 2. 传递层：通过 RPC/HTTP Header 以及 Go 的 context.Context 进行上下文透传。
// 3. 落盘层：在当前 GroupExecutionLog 表中增加 `TraceID` 字段，并在保存时将其落入 DB 或 ELK。这样当某组任务异常时，即可通过此 ID 反查整个业务链路的日志。
//
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：为什么要记录 MemberCount, SuccessCount, FailedCount？
// 答：
// 这叫【数据冗余（Data Redundancy）与反范式设计】。
// 如果不记这些，要看一个组里有多少成功、多少失败，我们就得去 execution_logs 表里，用 GroupID 把所有的子任务日志搜出来，
// 然后 `SELECT COUNT(*)` 算一遍。这在数据量小的系统中没问题。
// 但如果这是一个大厂系统，一天跑几千万个任务，你每次查界面都要去做大表 COUNT 聚合统计，数据库 CPU 直接拉满！
// 所以，我们在组跑完的那一刻，直接把成功数、失败数算好，存进这条日志里。
// 下次看的时候，直接 O(1) 拿出来，速度起飞。
// ============================================================
package model

import "time"

// 🔬 【底层原理·深度剖析】Go 时间处理与时区陷阱
// Go 的 time.Time 底层不仅记录了绝对时间（Wall Clock），还记录了不受系统时钟被人工篡改影响的单调时间（Monotonic Clock）。
// 在与 MySQL 等关系型数据库交互时，GORM 等 ORM 工具需要配置 `parseTime=True&loc=Local`，
// 这样才能将 DB 中的 DATETIME 无缝且正确地映射并解析为包含当前服务正确时区信息的 time.Time 对象。

// GroupExecutionLog records a single run of a task group.
// 
// 🏗️ 【架构设计·模式对比】贫血模型 vs 充血模型
// 目前这个 struct 是典型的“贫血模型”（Anemic Domain Model），它主要作为单纯的数据容器，缺乏业务聚合行为。
// 在持久化层（PO Persistent Object）这是标准化且被广泛接受的实践；但在严谨的领域驱动设计（DDD）中，
// 如果要在核心领域层使用它作为 Domain Model，我们通常还需要为其附加类似 `MarkAsFailed()` 
// 或 `CalculateProgress()` 等丰富的业务行为方法，从而保证自身状态流转的一致性不受外部业务代码肆意修改。
type GroupExecutionLog struct {
    ID           uint       `gorm:"primaryKey" json:"id"`
    GroupID      uint       `gorm:"index;not null" json:"group_id"`
    GroupName    string     `gorm:"not null" json:"group_name"`
    Mode         string     `gorm:"not null" json:"mode"`
    TriggerType  string     `gorm:"not null;default:cron" json:"trigger_type"`
    // 🛡️ 【安全攻防·漏洞防线】状态机越权篡改防御
    // Status 在真实的业务生命周期逻辑中应该被严密闭环控制，只有特定的状态机流转路线（如 running -> success/failed）才算合法。
    // 在对外暴露的 RESTful/GraphQL 更新 API 接口时，绝对禁止客户端直接提交并覆盖此字段，防范危险的批量赋值越权篡改漏洞（Mass Assignment）。
    Status       string     `gorm:"not null;default:running" json:"status"`
    MemberCount  int        `json:"member_count"`
    SuccessCount int        `json:"success_count"`
    FailedCount  int        `json:"failed_count"`
    
    // ⚡ 【性能实战·生产调优】时间范围检索的索引失效隐患
    // StartTime 加上了 `index` 索引，这是符合生产标准的决定。
    // 在生产环境中，日志系统最高频的后台查询场景往往是 "检索某段时间内产生的日志（例：统计当天的错报率）"。
    // 💀 踩坑预警：如果你在 SQL 中使用 `WHERE DATE(start_time) = '2023-10-01'`，会导致整个索引完全失效退化为全表扫描（对索引列套用函数的后果）！
    // 正确的做法必须是利用索引范围查找：`WHERE start_time >= '2023-10-01 00:00:00' AND start_time < '2023-10-02 00:00:00'`。
    StartTime    time.Time  `gorm:"not null;index" json:"start_time"`
    EndTime      *time.Time `json:"end_time"`
    // 📌 【大厂面试·核心考点】MySQL 大文本(TEXT/JSON)引发的页分裂与行溢出(Off-page)
    // 面试官极可能会问：如果系统异常极其严重，ErrorMsg 字段塞入了超级庞大的长堆栈日志，在 MySQL 存储侧会有什么性能隐患？
    // 标准答案：
    // 这极易触发 InnoDB 的【行溢出】（Off-page Overflow）与【页分裂】（Page Split）两大性能杀手机制！
    // 1. 行溢出：InnoDB 的默认数据页（Page）大小固定是 16KB。为了保证 B+ 树数据结构的二分查找效率，一页中至少要能存放下 2 行数据。
    // 当这个 ErrorMsg 文本长度激增（一般超过约 8KB 时），MySQL 就无法将当前这行完整塞在当前数据页中。
    // 此时，MySQL 会在聚簇索引树的叶子节点仅仅保留该字段的前 768 个字节和一个 20 字节大小的内存指针，
    // 余下那海量的数据会被强行塞到另外申请的 "Uncompressed BLOB Page"（溢出页）中。
    // 2. 页分裂：即便还没达到溢出触发红线，大量超长的文本也会导致一页能容纳的数据行数急剧变少（例如一页只能挤下三五条数据）。
    // 随着业务不断写入新记录，老的数据页极速填满，MySQL 被迫高频向磁盘申请新页，并将老页中一半的数据硬性搬迁过去。这会引发极其严重的磁盘写放大和多线程锁竞争。
    //
    // 🔬 【底层原理·深度剖析】
    // [生活比喻]：这就像你买了一个标准宜家格子书柜（16KB大小），平常每个格子轻轻松松能放十本普通小薄册子。突然有天你拿来一张巨幅高精度的卷轴世界地图（超大ErrorMsg）。
    // 书柜格子根本塞不进去！你无可奈何，只能在格子里放一张小贴纸写着：“地图本体已转移放置在地下室杂物间柜子的第1层”（行溢出）。
    // 到了月底查账找地图时，你就得多跑一趟遥远的地下室（触发额外的、极其缓慢的随机磁盘 I/O 动作，极大损耗性能）。
    //
    // 💀 【踩坑血泪·反面教材】
    // 真实生产惨案：某前线研发同事在排查问题时，为了省事，在后台 Web 的全量列表页直接使用了类似 `SELECT * FROM group_execution_logs ORDER BY id DESC LIMIT 50`。
    // 恰好那天系统外部网络抖动，产生了一大波冗长无比的 Java/Go 全链路堆栈报错填满了 ErrorMsg。
    // 这个由于无知而使用 `SELECT *` 的查询因为顺带全选了那些饱含溢出页的 ErrorMsg 字段，瞬间触发了海量的机械硬盘随机读取动作，
    // 导致数据库底层 IOPS 瞬间被抽干打满 100%，引发了全站其他重要业务模块的雪崩性超时崩溃！
    //
    // ⚡ 【性能实战·生产调优】
    // 面对这种大字段隐患，我们有三板斧方案：
    // 1. 【严禁 SELECT *】：在前端展示日志列表页（非详情页）时，严禁使用全列抓取，必须强制指定精简字段（例如只拿 GroupID, Status, StartTime）。
    // 2. 【截断式防守】：在业务代码持久化层严控长度界限，比如强行截断 `ErrorMsg = err.Error()[:2000]`，对于排查代码 BUG，绝大多数情况下前两千个字符已经足够定位出根因了。
    // 3. 【彻底冷热分离】：如果合规或业务要求必须 100% 完整保留异常堆栈，应将 ErrorMsg 字段从主表中彻底剥离独立成一张附属扩展表（保持一对一关联），或者干脆直接将其推送至成本更低、适合大文本搜索的中间件（如 OSS、ElasticSearch），而 MySQL 主表里只存入一个极其轻量的日志提取 URL 或 Document ID 即可。
    ErrorMsg     string     `json:"error_msg,omitempty"`
    CreatedAt    time.Time  `json:"created_at"`
}

// 🧪 【测试工程·质量保障】Mock 数据隔离与测试规范
// 根据系统强制的 [单元测试物理零污染] 规则：在执行相关 ORM 方法的集成测试时，
// 测试框架严禁针对 "group_execution_logs" 这个真实物理表执行 DROP、TRUNCATE。
// 必须通过 sqlmock 将底层的 DB driver 拦截，或者针对带有隔离前缀的独立库环境进行临时数据推演，
// 以保证所有测试用例对物理层数据达到【100% 毫发无损】。
func (GroupExecutionLog) TableName() string { return "group_execution_logs" }
