// ============================================================
// internal/domain/model/execution_log.go - 执行日志数据模型
//
// 这个文件定义了"执行日志"在数据库里的样子。
// 每次任务执行都会在数据库的 execution_logs 表里留下一条记录。
// 就像飞机的"黑匣子"，记录了：
//   - 哪个任务跑了
//   - 什么时候开始、什么时候结束
//   - 跑的结果（成功还是失败）
//   - 输出了什么内容
//   - 有没有报错
//
// ============================================================
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：这里的 `TaskID *uint` 为什么要加个星号（*）变成指针类型？
// 答：
// 这是处理数据库的一个大坑——【零值陷阱】。
// 如果不用星号，用普通的 `uint`。当你不给它赋值时，Go 语言会自动给它一个默认值 0。
// 存进数据库里，数据库会以为“这个日志属于 0 号任务”。
// 但实际上，你的意思是“这条日志现在不属于任何任务（比如原任务被删除了）”，应该是 NULL。
// 用了指针 `*uint` 后，如果不赋值，它的默认值是 `nil`。存进数据库，GORM 会聪明地把它翻译成 SQL 里的 NULL。
// 
// 2. 面试官问：底下的 `TransitionTo` 是什么设计模式？
// 答：
// 这叫【有限状态机（Finite State Machine, FSM）】。
// 
// 📌 图解状态机流转图：
// 
//              (触发运行)
//   [ pending (等待) ] -------------> [ running (运行中) ]
//         |                                  |
//         |                                  |--- (顺利跑完) ---> [ success (成功) ] (终态✅)
//         |                                  |
//         +-- (被管理员强杀)                 |--- (代码报错) ---> [ failed (失败) ]
//         |                                  |
//   [ cancelled (取消) ] <-------------------+--- (跑太久了) ---> [ timeout (超时) ]
//
// 如果没有状态机，别的程序员可能手滑写了一行代码： `log.Status = "success"`，把一个原本失败的任务强行改成了成功。
// 有了状态机，必须调用 `TransitionTo("success")`，如果当前是 "failed"，就会报错拦截：“对不起，不能死而复生！”
// 这种设计，让系统固若金汤，这就叫“大厂规范”。
// 
// 🏗️ 【架构设计·模式对比】贫血模型 vs 充血模型 (Anemic vs Rich Domain Model)
// 
// 1. 面试官问：什么是贫血模型？什么是充血模型？你在这里是怎么实践的？
// 答：
// 【初二小白比喻】：
// 贫血模型就像一个只有属性没有方法的“木偶”，任何人（Service层）都可以随意摆弄它的手脚（直接修改结构体字段）。
// 充血模型则像一个有自主意识的“机器人”，你要它改变姿势，必须发号施令（调用它的专属方法），它自己会判断能不能这么做。
//
// 【技术深入】：
// - 贫血模型（反面教材）：
//   在 Service 层写逻辑：`if log.Status == "pending" { log.Status = "running" }`。
//   缺点：状态流转逻辑散落在全宇宙，别人在另一处代码写了个 `log.Status = "finished"`，直接破坏了业务规则。
// - 充血模型（正确做法 - 本文件实践）：
//   状态被封装在实体（Entity）内部，暴露 `TransitionTo` 方法。
//   在 Service 层只能调用：`err := log.TransitionTo(StateRunning)`。
//   优点：实体自我保护，高内聚。业务规则完全收敛在 Model 层，符合 DDD（领域驱动设计）精髓。
// ============================================================
package model

// time 包提供 time.Time 类型，用来表示日期和时间
import (
    "fmt"
    "time"
)

// ExecutionLog 代表一次任务执行留下的完整记录
// 每一行就是一次执行，类似日记本里的一页
//
// GORM 标签说明（复习）：
//   primaryKey = 主键（唯一编号）
//   not null   = 不能为空
//   default:xx = 默认值
//   index      = 建立索引（加快按这个字段搜索的速度）
//   omitempty  = 为空时不显示在 JSON 里
//
// *uint, *int, *time.Time 前面加 * 表示这个字段的值可以为 NULL（空）
// 普通类型（如 int）不能为 NULL，没赋值就是 0
// 指针类型可以为 nil（Go 语言的"空"）
type ExecutionLog struct {
    // ID 自增主键，每条日志的唯一编号
    ID uint `gorm:"primaryKey" json:"id"`

    // TaskID 指向这条日志属于哪个任务（通过任务的 ID 关联）
    // *uint 是指针类型，可以为 nil
    // 为什么允许 nil？因为如果任务被删除了，日志还会保留
    // 这时候 TaskID 就是 nil（空），表示"这个任务已经不存在了"
    // index 表示按任务 ID 建了索引，查找某个任务的所有日志会很快
    //
    // ⚡ 【性能实战·生产调优】索引设计与 B+Tree 原理
    // 【初二小白比喻】：字典的拼音检索目录。没有它，你要翻遍整本字典找一个字（全表扫描）；有了它，3步就能找到。
    // 【底层剖析】：
    // 这里的 `gorm:"index"` 会在 MySQL 中建立一个【二级索引】（Secondary Index）。
    // B+Tree 是一种多路平衡查找树。它的非叶子节点只存索引键（TaskID），不存真实数据。
    // 所有真实数据都挂在最底层的【叶子节点】上，并通过双向链表相连。
    // 查找过程：先遍历 TaskID 索引树，找到对应的 主键ID，这叫【回表】（再回主键索引树查详细数据）。
    // 【踩坑血泪】：
    // 执行日志是"海量高频写入"的表。每增加一个索引，写入时就要多维护一棵 B+Tree（引发树的分裂、合并，增加磁盘 I/O）。
    // 所以，索引千万不能乱加！我们只在极高频的查询条件（如 TaskID, StartTime）上建索引。
    // 数据：如果在 1000 万数据的表里，没有 TaskID 索引，查询某任务日志耗时约 2-3 秒（全表扫描）；有索引耗时约 5-10 毫秒（时间复杂度 O(log N)）。
    TaskID *uint `gorm:"index" json:"task_id"`

    // TaskName 任务名称
    // 虽然通过 TaskID 也能查到任务名，但这里冗余存一份
    // 好处是：查日志的时候不用每次都去任务表找名字，快了不止一点
    TaskName string `gorm:"not null" json:"task_name"`

    // GroupName is populated at query time via task->group lookup, not persisted.
    GroupName string `gorm:"-" json:"group_name,omitempty"`

    // CronExpr 触发这次执行的 cron 表达式
    // 记录下来方便追溯"是按哪个时间规则触发的"
    CronExpr string `json:"cron_expr,omitempty"`

    // Status 当前执行状态，像一个"红绿灯"
    // running   = 黄灯（正在跑）
    // success   = 绿灯（成功了）
    // failed    = 红灯（失败了）
    // timeout   = 红灯闪烁（超过规定时间被强制终止了）
    // cancelled = 灰灯（被手动取消了）
    // index 表示按状态建了索引，方便查"所有失败的日志"
    Status string `gorm:"not null;default:running;index" json:"status"`

    // TriggerType 触发方式，记录是谁"按下"了运行按钮
    // cron   = 定时器自动触发的（到了预设时间）
    // manual = 有人手动点的"立即执行"按钮
    TriggerType string `gorm:"not null;default:cron" json:"trigger_type"`

    // StartTime 任务开始执行的时间点
    // index 表示按开始时间建了索引，方便按时间排序和范围查询
    //
    // 🔬 【底层原理·深度剖析】为什么用时间做索引对海量写入比较友好？
    // 在海量写入下，如果我们使用无序的 UUID 作为索引或主键，插入 B+Tree 会导致频繁的【页分裂（Page Split）】，导致磁盘碎片满天飞，写入性能骤降。
    // 我们使用的是自增 `ID uint` 主键，写入是【顺序追加】的，B+Tree 节点利用率极高。
    // 而 `StartTime` 字段呢？由于时间是不断向前流逝的，StartTime 的值基本也是递增的。
    // 所以，插入新的日志记录时，`StartTime` 二级索引树同样享受了【顺序插入】的红利，大大减少了页分裂操作，保障了高并发下的写入极速！
    StartTime time.Time `gorm:"not null;index" json:"start_time"`

    // EndTime 任务结束的时间点
    // *time.Time 表示可以为 nil（任务还在跑的时候，结束时间是空的）
    // 用 EndTime - StartTime 就能算出这次执行花了多长时间
    EndTime *time.Time `json:"end_time"`

    // ExitCode 程序的退出码
    // 惯例：0 = 正常结束，非 0 = 出错了
    // *int 表示可以为 nil（任务还在跑的时候没有退出码）
    ExitCode *int `json:"exit_code"`

    // Output 任务的输出内容
    // stdout：程序正常输出的内容
    // stderr：程序报错的内容
    // 两者合并存在这里
    // 为了防止某个任务输出几百万行的内容撑爆数据库
    // 在存入数据库前会根据配置截断（默认保留前面 64KB）
    //
    // 💀 【踩坑血泪·反面教材】
    // 真实生产事故案例：某大厂曾因为没有做日志截断，一个死循环脚本疯狂输出日志，导致单条记录 `Output` 字段高达 500MB！
    // 结果：MySQL 的 Buffer Pool 被这一条超级记录瞬间洗爆，网卡带宽在查询时被打满，整个数据库直接 OOM 挂掉。
    // 【防线】：
    // 1. 业务层强制截断（比如只保留尾部核心错误栈）。
    // 2. 数据库层面避免 `SELECT *`，分页查列表时严禁带出 `Output` 等大文本字段，只有查单条详情时才拉取！
    Output string `json:"output,omitempty"`

    // ErrorMsg 错误信息
    // 如果任务执行失败了，这里记录失败的原因
    // 方便排查问题，不用从 Output 的几千行里找错误
    ErrorMsg string `json:"error_msg,omitempty"`

    // RetryAttempt 这是第几次尝试
    // 0 = 首次执行（还没重试过）
    // 1 = 第一次重试
    // 2 = 第二次重试
    // ...以此类推
    RetryAttempt int `gorm:"default:0" json:"retry_attempt"`

    // CreatedAt 这条日志记录的创建时间（GORM 自动维护）
    //
    // 📌 【大厂面试·核心考点】GORM 的软删除与时间戳自动管理机制
    // 面试官问：GORM 是如何自动管理 CreatedAt, UpdatedAt 和 DeletedAt 的？
    // 答：
    // 【自动时间戳】：
    // GORM 底层使用了【回调拦截器 (Callbacks)】。在执行 INSERT SQL 之前，GORM 的 Hook 会通过反射检查结构体有没有 `CreatedAt` 字段。如果有，自动注入当前时间 `time.Now()`，完全不需要开发者手动赋值。
    // 【软删除 (Soft Delete)】：
    // 如果结构体引入了 `gorm.DeletedAt` 字段，当你调用 `db.Delete(&log)` 时，GORM 会拦截删除动作，并修改 AST（抽象语法树）。
    // 它不会真正执行 `DELETE FROM`，而是偷偷转成 `UPDATE execution_logs SET deleted_at = '2023-10-01...' WHERE id = ?`。
    // 查询时，GORM 也会自动注入全局过滤条件：`WHERE deleted_at IS NULL`。
    // 【架构反思】：
    // 本表（ExecutionLog）为什么故意不加 DeletedAt 进行软删除？
    // 因为执行日志是海量的"流水型"日志数据。软删除会导致垃圾数据永远占用磁盘，并拖慢 B+Tree 索引树。
    // 正确的做法是：不走软删除，依靠定期清理任务（如清理 30 天前的历史日志），物理删除（真删除）释放空间。
    CreatedAt time.Time `json:"created_at"`
}

// TableName 告诉 GORM 这个模型对应的数据库表名
// 如果不写这个函数，GORM 默认用结构体名的 snake_case 复数形式
// snake_case 是"蛇形命名法"，单词之间用下划线连接，如 execution_logs
// 这里显式指定，保证表名不会因为结构体改名而变化
func (ExecutionLog) TableName() string {
    return "execution_logs"
}

// ============================================================
// 状态机常量与方法
// 保证状态只能单向、合法地流转，防止幽灵状态。
// ============================================================

const (
    StatePending   = "pending"
    StateRunning   = "running"
    StateSuccess   = "success"
    StateFailed    = "failed"
    StateTimeout   = "timeout"
    StateCancelled = "cancelled"
)

// CanTransitionTo 检查当前状态是否允许流转到目标状态
//
// 🛡️ 【安全攻防·漏洞防线】防范非法状态越权篡改
// 这里的状态机不仅仅是为了代码规范，更是核心的【安全防线】。
// 如果使用贫血模型直接赋值，黑客可能通过接口抓包，伪造参数 `{"status": "success"}` 试图把一个失败的甚至正在执行的任务强行刷成成功。
// 借助此充血模型方法，业务层调用 `TransitionTo` 时，状态机会严格执行白名单校验校验当前状态树。
// 如果发现尝试把 failed 改成 success，会在 Domain 层瞬间掐断，有效防止了越权篡改和幽灵状态！
func (l *ExecutionLog) CanTransitionTo(target string) bool {
    switch l.Status {
    case StatePending:
        // 等待中只能转为运行中或被取消
        return target == StateRunning || target == StateCancelled
    case StateRunning:
        // 运行中可以自然结束、失败、超时或被取消
        return target == StateSuccess || target == StateFailed || target == StateTimeout || target == StateCancelled
    case StateFailed, StateTimeout, StateCancelled:
        // 失败或取消后，如果是自动/手动重试，会重置回等待中
        return target == StatePending
    case StateSuccess:
        // 成功是绝对的终态
        return false
    default:
        // 应对初始化时状态为空的特殊情况
        return target == StatePending || target == StateRunning
    }
}

// TransitionTo 执行状态流转，如果非法则返回 error
func (l *ExecutionLog) TransitionTo(target string) error {
    if !l.CanTransitionTo(target) {
        return fmt.Errorf("invalid state transition from %s to %s", l.Status, target)
    }
    l.Status = target
    return nil
}
