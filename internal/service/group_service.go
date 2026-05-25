package service

import (
    "fmt"

    "cronix/internal/model"

    "gorm.io/gorm"
)

type GroupService struct {
    DB *gorm.DB
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
    return s.DB.Create(g).Error
}

func (s *GroupService) UpdateGroup(id uint, updates map[string]interface{}) error {
    if mode, ok := updates["mode"].(string); ok {
        if mode != "parallel" && mode != "sequential" {
            return fmt.Errorf("mode must be parallel or sequential")
        }
    }
    return s.DB.Model(&model.TaskGroup{}).Where("id = ?", id).Updates(updates).Error
}

func (s *GroupService) DeleteGroup(id uint) error {
    // Unlink tasks from this group
    s.DB.Model(&model.Task{}).Where("group_id = ?", id).Update("group_id", nil)
    return s.DB.Delete(&model.TaskGroup{}, id).Error
}

// GetGroupMembers returns all tasks belonging to a group, ordered by group mode.
func (s *GroupService) GetGroupMembers(groupID uint) ([]model.Task, error) {
    var tasks []model.Task
    // For sequential groups, order by the task's inherent order.
    // For now use ID order; a proper sort_order column can be added later.
    if err := s.DB.Where("group_id = ?", groupID).Order("id ASC").Find(&tasks).Error; err != nil {
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
        // Assign new members
        if len(taskIDs) > 0 {
            return tx.Model(&model.Task{}).Where("id IN ?", taskIDs).Update("group_id", groupID).Error
        }
        return nil
    })
}
