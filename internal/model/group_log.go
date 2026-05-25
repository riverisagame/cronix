package model

import "time"

// GroupExecutionLog records a single run of a task group.
type GroupExecutionLog struct {
    ID           uint       `gorm:"primaryKey" json:"id"`
    GroupID      uint       `gorm:"index;not null" json:"group_id"`
    GroupName    string     `gorm:"not null" json:"group_name"`
    Mode         string     `gorm:"not null" json:"mode"`
    TriggerType  string     `gorm:"not null;default:cron" json:"trigger_type"`
    Status       string     `gorm:"not null;default:running" json:"status"`
    MemberCount  int        `json:"member_count"`
    SuccessCount int        `json:"success_count"`
    FailedCount  int        `json:"failed_count"`
    StartTime    time.Time  `gorm:"not null;index" json:"start_time"`
    EndTime      *time.Time `json:"end_time"`
    ErrorMsg     string     `json:"error_msg,omitempty"`
    CreatedAt    time.Time  `json:"created_at"`
}

func (GroupExecutionLog) TableName() string { return "group_execution_logs" }
