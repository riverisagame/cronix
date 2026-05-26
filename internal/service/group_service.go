package service

import (
    "fmt"

    "cronix/internal/model"
    "cronix/internal/scheduler"

    "gorm.io/gorm"
)

type GroupService struct {
	DB      *gorm.DB
	Engine  *scheduler.Engine
	ExecSvc *ExecutionService // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
}

func (s *GroupService) ListGroups() ([]model.TaskGroup, error) {
    var groups []model.TaskGroup
    if err := s.DB.Order("id ASC").Find(&groups).Error; err != nil {
        return nil, err
    }
    return groups, nil
}

func (s *GroupService) GetGroup(id uint) (*model.TaskGroup, error) {
    var g model.TaskGroup
    if err := s.DB.First(&g, id).Error; err != nil {
        return nil, err
    }
    return &g, nil
}

func (s *GroupService) CreateGroup(g *model.TaskGroup) error {
	if g.Name == "" {
		return fmt.Errorf("group name is required")
	}
	if g.Mode != "parallel" && g.Mode != "sequential" {
		return fmt.Errorf("mode must be parallel or sequential")
	}
	if err := s.DB.Create(g).Error; err != nil {
		return err
	}
	if s.Engine != nil {
		if err := s.Engine.UpdateGroupSchedule(*g); err != nil { // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
			return err
		}
	}
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return nil
}

func (s *GroupService) UpdateGroup(id uint, updates map[string]interface{}) error {
	if mode, ok := updates["mode"].(string); ok {
		if mode != "parallel" && mode != "sequential" {
			return fmt.Errorf("mode must be parallel or sequential")
		}
	}
	if err := s.DB.Model(&model.TaskGroup{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	if s.Engine != nil {
		var updatedGroup model.TaskGroup
		if err := s.DB.First(&updatedGroup, id).Error; err == nil {
			s.Engine.UpdateGroupSchedule(updatedGroup) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
		}
	}
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return nil
}

func (s *GroupService) DeleteGroup(id uint) (int64, int64, error) {
	var taskCount, logCount int64

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		tx.Model(&model.Task{}).Where("group_id = ?", id).Count(&taskCount)
		tx.Model(&model.GroupExecutionLog{}).Where("group_id = ?", id).Count(&logCount)
		tx.Model(&model.Task{}).Where("group_id = ?", id).Update("group_id", nil)
		if err := tx.Where("group_id = ?", id).Delete(&model.GroupExecutionLog{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.TaskGroup{}, id).Error
	})
	if err != nil {
		return 0, 0, err
	}
	if s.Engine != nil {
		s.Engine.RemoveGroupSchedule(id) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return taskCount, logCount, nil
}

// GetGroupMembers returns all tasks belonging to a group, ordered by sort_order.
func (s *GroupService) GetGroupMembers(groupID uint) ([]model.Task, error) {
	var tasks []model.Task
	if err := s.DB.Where("group_id = ?", groupID).Order("sort_order ASC, id ASC").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// SetGroupMembers updates which tasks belong to the group.
// taskIDs is the list of task IDs to include.
func (s *GroupService) SetGroupMembers(groupID uint, taskIDs []uint) error {
	// 先查出当前属于 groupID 的任务 ID 列表，用于增量调度更新
	var oldMemberIDs []uint
	s.DB.Model(&model.Task{}).Where("group_id = ?", groupID).Pluck("id", &oldMemberIDs)

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// Remove all existing members
		if err := tx.Model(&model.Task{}).Where("group_id = ?", groupID).Update("group_id", nil).Error; err != nil {
			return err
		}
		// Assign new members with sort_order based on array position
		for i, tid := range taskIDs {
			if err := tx.Model(&model.Task{}).Where("id = ?", tid).Updates(map[string]interface{}{
				"group_id":   groupID,
				"sort_order": i,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 增量同步受影响任务的定时调度状态，并失效仪表盘缓存
	affectedIDs := append(oldMemberIDs, taskIDs...)
	if len(affectedIDs) > 0 && s.Engine != nil {
		var affectedTasks []model.Task
		s.DB.Where("id IN ?", affectedIDs).Find(&affectedTasks)
		for _, t := range affectedTasks {
			s.Engine.UpdateTaskSchedule(t) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
		}
	}
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return nil
}

// GetGroupLogs returns execution logs for a group, paginated.
func (s *GroupService) GetGroupLogs(groupID uint, page, pageSize int) ([]model.GroupExecutionLog, int64, error) {
    var logs []model.GroupExecutionLog
    var total int64
    query := s.DB.Model(&model.GroupExecutionLog{}).Where("group_id = ?", groupID)
    query.Count(&total)
    offset := (page - 1) * pageSize
    if err := query.Order("id DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
        return nil, 0, err
    }
    return logs, total, nil
}
