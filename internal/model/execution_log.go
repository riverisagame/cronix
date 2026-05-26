// ============================================================
// internal/model/execution_log.go - 执行日志数据模型
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
// 有了这些记录，你就能追溯每个任务的"一生"：
// 它什么时候被触发、跑了多久、结果怎样。
// ============================================================
package model

// time 包提供 time.Time 类型，用来表示日期和时间
import "time"

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
    CreatedAt time.Time `json:"created_at"`
}

// TableName 告诉 GORM 这个模型对应的数据库表名
// 如果不写这个函数，GORM 默认用结构体名的 snake_case 复数形式
// snake_case 是"蛇形命名法"，单词之间用下划线连接，如 execution_logs
// 这里显式指定，保证表名不会因为结构体改名而变化
func (ExecutionLog) TableName() string {
    return "execution_logs"
}
