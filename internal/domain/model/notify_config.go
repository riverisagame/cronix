// ============================================================
// internal/model/notify_config.go - 通知配置数据模型
//
// 这个文件定义了"任务通知规则"在数据库里的样子。
//
// 什么是任务通知？
//   任务跑完以后，你可能想知道结果（特别是失败了需要及时处理）。
//   通知就是自动给你发消息，告诉你任务跑得怎么样。
//
// 支持的通知方式：
//   webhook = 往一个指定的网址发消息（如企业微信机器人、钉钉机器人）
//   email   = 发电子邮件
//
// 通知的触发条件：
//   OnSuccess = 成功时也通知（默认关闭，不然太吵了）
//   OnFailure = 失败时通知（默认开启，因为失败了需要人处理）
//
// 每个任务可以单独配置自己的通知规则
// 就像每个房间可以独立决定要不要装门铃
// ============================================================
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：为什么不用外键约束（Foreign Key Constraint）来保证 TaskID 一定对应一个真实的任务？
// 答：
// 在大厂（比如阿里、字节）的数据库军规里，通常是【严禁使用物理外键】的。
// 物理外键就是让数据库强制检查：如果任务表里没有这个ID，通知表里就不准插这条数据。
// 为什么不用呢？因为外键会带来额外的锁开销，而且一旦分库分表（比如任务存在A库，通知存在B库），物理外键就直接失效了。
// 大厂的做法是使用【逻辑外键】，也就是在代码逻辑里去保证数据一致性（比如查不到任务就忽略发送）。
//
// 2. 面试官问：Webhook 发通知万一失败了怎么办？
// 答：
// 设计系统时必须假设“网络是不可靠的”。
// Webhook URL 可能是别人公司的服务器，如果他们服务器挂了，或者网络抖动，你的请求就会失败。
// 所以在设计通知系统时，一定会加上【重试机制（Retry）】和【指数退避（Exponential Backoff）】，
// 并且在多次失败后，绝对不能让主程序崩溃，而是默默记下错误日志，这叫【故障隔离】和【尽力而为（Best-Effort Delivery）】。
// ============================================================
package model

// time 包提供 time.Time 类型，用于记录配置的创建时间
import "time"

// NotifyConfig 代表一个任务的通知配置
// 每个任务最多有一条通知配置（通过 TaskID 关联）
// 如果任务没有通知配置，就不发送任何通知
type NotifyConfig struct {
    // ID 自增主键，唯一标识这条配置记录
    ID uint `gorm:"primaryKey" json:"id"`

    // TaskID 这条通知配置属于哪个任务
    // 通过这个字段关联到 tasks 表的某一行
    // index 表示建了索引，可以快速查找"某个任务的通知配置"
    TaskID uint `gorm:"index" json:"task_id"`

    // OnSuccess 任务执行成功时是否发送通知
    // default:false 默认不发送（否则每次成功都通知会很烦）
    // 建议只对关键任务开启成功通知
    OnSuccess bool `gorm:"default:false" json:"on_success"`

    // OnFailure 任务执行失败时是否发送通知
    // default:true 默认开启（失败需要人处理，所以默认通知）
    OnFailure bool `gorm:"default:true" json:"on_failure"`

    // NotifyType 通知方式（选一种）
    // webhook = 调用一个外部的网址（HTTP 请求）
    //           比如企业微信/钉钉/飞书的机器人地址
    // email   = 发送电子邮件
    // default:webhook 默认使用 webhook 方式（配置最简单）
    NotifyType string `gorm:"not null;default:webhook" json:"notify_type"`

    // WebhookURL Webhook 的目标网址
    // 当 NotifyType = "webhook" 时使用这个字段
    // 比如企业微信机器人的地址：https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx
    // omitempty 表示如果不填，JSON 里就不显示这个字段
    WebhookURL string `json:"webhook_url,omitempty"`

    // EmailTo 邮件接收人的邮箱地址
    // 当 NotifyType = "email" 时使用这个字段
    // 比如 "admin@example.com"
    // omitempty 表示不填时不显示
    EmailTo string `json:"email_to,omitempty"`

    // CreatedAt 这条通知配置的创建时间（GORM 自动维护）
    CreatedAt time.Time `json:"created_at"`
}

// TableName 显式指定数据库表名为 "notify_configs"
// GORM 调用这个函数来知道用哪个表名
func (NotifyConfig) TableName() string {
    return "notify_configs"
}
