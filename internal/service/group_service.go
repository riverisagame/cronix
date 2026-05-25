package service

import (
    "fmt"

    "cronix/internal/model"
    "cronix/internal/scheduler"

    "gorm.io/gorm"
)

type GroupService struct {
    DB     *gorm.DB
    Engine *scheduler.Engine
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
    if s.Engine != nil { s.Engine.ReloadAll() }
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
    if s.Engine != nil { s.Engine.ReloadAll() }
    return nil
}

func (s *GroupService) DeleteGroup(id uint) error {
    s.DB.Model(&model.Task{}).Where("group_id = ?", id).Update("group_id", nil)
    if err := s.DB.Delete(&model.TaskGroup{}, id).Error; err != nil {
        return err
    }
    if s.Engine != nil { s.Engine.ReloadAll() }
    return nil
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
    return s.DB.Transaction(func(tx *gorm.DB) error {
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
}
