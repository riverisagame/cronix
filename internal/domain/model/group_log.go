// ============================================================
// internal/domain/model/group_log.go - 任务组执行日志数据模型
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
