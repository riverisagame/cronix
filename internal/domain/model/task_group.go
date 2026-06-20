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
