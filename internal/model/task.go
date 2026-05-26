// ============================================================
// internal/model/task.go - 任务数据模型
//
// 这个文件定义了"任务"在数据库里的样子。
// 你可以把"模型"理解为一张表格的"设计图"：
//   - 表格叫什么名字（这里叫 tasks）
//   - 表格有哪些列（每个字段是一列）
//   - 每列存什么类型的数据（文本、数字、时间等）
//   - 哪些列不能为空、哪些列有默认值
//
// GORM 是一个"翻译官"，它读了这个设计图之后，
// 就会自动在 SQLite 数据库里建一张对应的表。
// 然后你就可以用 Go 的语法来增删改查，不用写数据库语句。
//
// 每个字段后面有两个标签：
//   gorm:"..."  -> 给 GORM 看的，告诉它这个列在数据库里怎么存
//   json:"..."  -> 给 JSON 序列化看的，告诉它转成 JSON 时这个字段叫什么名字
//
// 标签里常用的关键词：
//   primaryKey   = 主键（每行的唯一编号，就像身份证号）
//   uniqueIndex  = 唯一索引（这个列的值在整个表里不能重复）
//   not null     = 不允许为空（必须填）
//   default:xxx  = 如果没填就用这个默认值
//   index        = 创建索引（给这列做个"目录"，加快查找速度）
//   omitempty    = 如果这个字段是空的，JSON 里就不出现，保持简洁
//   -            = 完全忽略（不存数据库，也不出现在 JSON 里）
// ============================================================
package model

// "time" 是 Go 语言自带的时间处理工具包
// time.Time 类型用来表示日期时间（年-月-日 时:分:秒）
import "time"

// Task 结构体代表一个计划任务
// 就像一张"任务登记卡"，记录了任务的所有信息
// GORM 会根据这个结构体在数据库里创建 tasks 表
type Task struct {
    // ID 是自增主键——每新增一个任务，这个编号自动 +1
    // 就像餐厅的取餐号：第一个客人拿 1 号，第二个拿 2 号...
    // uint = 正整数类型（只能是 0, 1, 2, 3... 不能是负数）
    ID uint `gorm:"primaryKey" json:"id"`

    // Name 任务名称，用来辨认是哪个任务
    // uniqueIndex 表示：整个表里不能有两个同名任务（给你起名字一样会乱）
    // not null 表示：这个字段必须填，不能空着
    Name string `gorm:"uniqueIndex;not null" json:"name"`

    // CronExpr 是 cron 表达式，决定了任务在什么时候自动执行
    // cron 表达式由 5 或 6 个数字组成，格式是：
    //   秒 分 时 日 月 星期
    // 例如 "0 30 8 * * *" 表示：每天 8 点 30 分 0 秒执行
    // * 号是通配符，意思是"每一个都匹配"
    CronExpr string `gorm:"not null" json:"cron_expr"`

    // TaskType 任务类型，决定这个任务怎么"跑"
    // 可选项：shell（执行命令）、http（发网络请求）、cleanup（清理）、healthcheck（健康检查）
    // default:shell 表示如果没填，默认就是 shell 类型
    TaskType string `gorm:"not null;default:shell" json:"task_type"`

    // ================================================================
    // 以下字段是 Shell 类型专用的
    // Shell = 命令行脚本，就像你在终端里敲命令一样
    // ================================================================

    // Command 要执行的命令内容
    // 比如 "echo hello" 就是在屏幕上打印 hello
    // omitempty 表示如果是空字符串，JSON 里不显示这个字段
    Command string `json:"command,omitempty"`

    // ================================================================
    // 以下字段是 HTTP 类型专用的
    // HTTP = 访问网页地址，就像在浏览器里输入网址
    // ================================================================

    // HTTPMethod HTTP 方法，相当于浏览器请求网页的方式
    // GET（获取数据）、POST（提交数据）、PUT（更新数据）、DELETE（删除数据）
    HTTPMethod string `json:"http_method,omitempty"`

    // HTTPURL 要请求的目标网址，比如 "https://example.com/api/data"
    HTTPURL string `json:"http_url,omitempty"`

    // HTTPHeaders 自定义的请求头（以 JSON 格式存储）
    // 请求头是附带在请求里的附加信息，比如身份令牌
    HTTPHeaders string `json:"http_headers,omitempty"`

    // HTTPBody 请求体内容（POST/PUT 时附带的数据）
    HTTPBody string `json:"http_body,omitempty"`

    // HTTPAuthType 认证类型（访问需要登录的接口时用）
    // 可选值：none（不需要认证）、basic（用户名密码）、bearer（令牌）、api_key（密钥）、oauth2（第三方授权）
    // default:none 表示默认不需要认证
    HTTPAuthType string `gorm:"default:none" json:"http_auth_type,omitempty"`

    // HTTPAuthConfig 认证所需的配置信息（可能包含密码或令牌）
    // json:"-" 表示这个字段永远不出现在 JSON 输出里
    // 为什么？因为这里可能存密码等敏感信息，不能通过网络传出去
    HTTPAuthConfig string `json:"-"`

    // ================================================================
    // 以下是所有类型通用的配置
    // ================================================================

    // TimeoutSec 任务超时时间（秒）
    // 比如设置 300 秒，任务跑了超过 5 分钟还没结束就强制终止
    // 防止某个任务卡死了永远不结束
    // default:300 表示默认 300 秒（5 分钟）
    TimeoutSec int `gorm:"default:300" json:"timeout_sec"`

    // RetryCount 失败后最多重试几次
    // 比如设置为 3，任务失败后会自动重跑最多 3 次
    // default:0 表示默认不重试，失败了就失败了
    RetryCount int `gorm:"default:0" json:"retry_count"`

    // RetryIntervalSec 两次重试之间的等待秒数
    // 比如设为 10，失败后等 10 秒再重试第一次
    // default:10 表示默认等 10 秒
    RetryIntervalSec int `gorm:"default:10" json:"retry_interval_sec"`

    // MaxConcurrent 同一个任务最多允许同时跑几个
    // 比如设为 1，上一次触发还没跑完，下一次触发就得等
    // default:1 表示同一个任务同一时间最多只有 1 个在跑
    MaxConcurrent int `gorm:"default:1" json:"max_concurrent"`

    // Enabled 是否启用这个任务
    // true = 启用，定时器到了就自动跑
    // false = 暂停，暂时不跑了（但配置还在，随时可以重新启用）
    // default:true 表示新建的任务默认是启用状态
    Enabled bool `gorm:"default:true" json:"enabled"`

    // Description 任务描述（可以写一些备注，说明这个任务是干什么的）
    // 选填，不写也没关系
    Description string `json:"description,omitempty"`

    // GroupID 所属任务组ID，nil 表示不属于任何组
    GroupID *uint `gorm:"index" json:"group_id,omitempty"`

    // SortOrder 在组内的排序位置（sequential 模式用），数字越小越先执行
    SortOrder int `gorm:"default:0" json:"sort_order"`

    // GroupName 任务组名称（查询时计算，不存入数据库）
    GroupName string `gorm:"-" json:"group_name,omitempty"`

    // WorkDir 工作目录（shell 任务在哪个文件夹下面执行）
    // 比如指定为 "/home/user/scripts"，执行命令时会先切换到这个目录
    // 选填，不写就在当前目录下执行
    WorkDir string `json:"work_dir,omitempty"`

    // RunAs 以哪个用户身份执行（仅 Shell 类型生效）
    // 非空时通过 sudo -u <user> 切换用户，需在 sudoers 中授权 cronix 用户
    // 选填，不写默认以 root 身份执行
    RunAs string `json:"run_as,omitempty"`

    // CreatedAt 任务创建时间
    // time.Time 类型存的是完整的日期时间（如 2024-05-01 12:30:00）
    // GORM 自动管理这个字段：插入记录时自动填当前时间
    CreatedAt time.Time `json:"created_at"`

    // UpdatedAt 任务最后修改时间
    // GORM 自动管理：每次更新记录时自动刷新为当前时间
    UpdatedAt time.Time `json:"updated_at"`
}

// TableName 告诉 GORM 这个模型对应的数据库表名叫什么
// 如果不写这个函数，GORM 会自动把结构体名 Task 转成复数 tasks
// 这里显式指定 "tasks"，确保表名准确无误
// 这个函数是 GORM 框架要求的，名字和格式是固定的
func (Task) TableName() string {
    return "tasks"
}
