package model

import "time"

// TaskGroup represents a logical group of tasks with a shared execution mode.
// mode="parallel" — all tasks run concurrently (same DAG layer).
// mode="sequential" — tasks run one by one in sort_order.
// mode="dag" — tasks run layer by layer based on dependency graph.
type TaskGroup struct {
    ID          uint      `gorm:"primaryKey" json:"id"`
    Name        string    `gorm:"uniqueIndex;not null" json:"name"`
    Description string    `json:"description,omitempty"`
    Mode        string    `gorm:"default:parallel" json:"mode"` // "parallel" or "sequential"
    Enabled     bool      `gorm:"default:true" json:"enabled"`
    CronExpr    string    `json:"cron_expr,omitempty"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}

func (TaskGroup) TableName() string {
    return "task_groups"
}
