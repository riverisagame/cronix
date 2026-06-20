// ============================================================
// internal/application/service/group_service.go - 任务组业务逻辑服务
//
// 【纳米级源码说明书 - 业务篇】
// 这里的角色是“项目组HR主管”。
// 负责组建项目组（TaskGroup）、修改组属性、解散项目组、分配组成员（Task）。
// 当项目组有变动时，他不仅要更新花名册（DB），还要同步通知：
// 1. 发动机车间主任（Engine / GroupReloader）：打铃策略变了！
// 2. 统计报表员（ExecSvc / StatsInvalidator）：有数据变了，之前的缓存报表作废！
//
// ============================================================
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：什么是数据库事务（Transaction）？这段代码里哪里用到了，为什么用？
// 答（小白比喻）：
// 假设你（银行）给朋友转账 100 块。动作分两步：1. 你扣 100 块；2. 朋友加 100 块。
// 如果你刚扣完 100 块，银行系统突然断电了，朋友没收到钱，你的 100 块也没了，这就血亏！
// 【事务】就是把这两个动作绑在一起变成“一件事（原子性）”。要么全成功，要么全失败（退回你的钱）。
// 看 DeleteGroup 方法，解散项目组时，不仅要删“组”，还要把组里的“任务”和“组日志”都清理干净。
// 这三个写操作被包在一个 tx.Transaction 里，只要中间有一步报错，就会触发回滚（Rollback），保证数据不出错。
//
// 2. 面试官问：什么是缓存失效（Cache Invalidation）？为什么在增删改后面都要调 InvalidateStatsCache？
// 答：
// 前端为了看图表快，后端通常会把算好的报表“复印”一份放在抽屉里（缓存）。下次有人要看，直接拿复印件。
// 但是，如果项目组的数据被人修改了（比如新建、删除了组），那抽屉里的复印件就【过期（Dirty）】了。
// InvalidateStatsCache 的作用就是“把旧复印件撕掉”。等下一个人来看报表时，发现没复印件，就会逼着系统重新算一遍最新数据。
//
// @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
// ============================================================
package service

import (
	"fmt"

	"cronix/internal/domain/model"

	"gorm.io/gorm"
)

// GroupService 项目组HR主管的办公桌
type GroupService struct {
	DB      *gorm.DB         // 数据库连接（花名册）
	Engine  GroupReloader    // 依赖倒置接口：调度引擎（车间主任）
	ExecSvc StatsInvalidator // 依赖倒置接口：日志与指标服务（统计员）
	// @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
}

// ListGroups 列出所有的项目组
func (s *GroupService) ListGroups() ([]model.TaskGroup, error) {
	var groups []model.TaskGroup
	if err := s.DB.Order("id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

// GetGroup 按 ID 查找某个特定的项目组
func (s *GroupService) GetGroup(id uint) (*model.TaskGroup, error) {
	var g model.TaskGroup
	if err := s.DB.First(&g, id).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

// CreateGroup 创建一个新项目组
func (s *GroupService) CreateGroup(g *model.TaskGroup) error {
	// 【数据校验】名字不能为空
	if g.Name == "" {
		return fmt.Errorf("group name is required")
	}
	// 【数据校验】模式必须是并行或串行
	if g.Mode != "parallel" && g.Mode != "sequential" {
		return fmt.Errorf("mode must be parallel or sequential")
	}
	
	// 1. 写库
	if err := s.DB.Create(g).Error; err != nil {
		return err
	}
	
	// 2. 通知车间主任：有新组成立了，排一下班表！
	if s.Engine != nil {
		if err := s.Engine.UpdateGroupSchedule(*g); err != nil { // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
			return err
		}
	}
	
	// 3. 通知统计员：撕掉旧的首页统计报表！
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return nil
}

// UpdateGroup 更新项目组信息
func (s *GroupService) UpdateGroup(id uint, updates map[string]interface{}) error {
	if mode, ok := updates["mode"].(string); ok {
		if mode != "parallel" && mode != "sequential" {
			return fmt.Errorf("mode must be parallel or sequential")
		}
	}
	
	// 1. 写库更新
	if err := s.DB.Model(&model.TaskGroup{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	
	// 2. 把更新后的完整数据查出来，通知车间主任更新班表
	if s.Engine != nil {
		var updatedGroup model.TaskGroup
		if err := s.DB.First(&updatedGroup, id).Error; err == nil {
			s.Engine.UpdateGroupSchedule(updatedGroup) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
		}
	}
	
	// 3. 撕掉旧报表
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return nil
}

// DeleteGroup 解散项目组
// 返回值：受影响的任务数、删除的组日志数、错误信息
func (s *GroupService) DeleteGroup(id uint) (int64, int64, error) {
	var taskCount, logCount int64

	// 【大厂实践：数据库事务 Transaction】保证 3 个动作要么全成，要么全败
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// 先数一数要改多少条数据（用于返回给前端提示）
		tx.Model(&model.Task{}).Where("group_id = ?", id).Count(&taskCount)
		tx.Model(&model.GroupExecutionLog{}).Where("group_id = ?", id).Count(&logCount)
		
		// 动作 1：把原本属于这个组的成员，打回自由身（group_id 设为 nil）
		tx.Model(&model.Task{}).Where("group_id = ?", id).Update("group_id", nil)
		
		// 动作 2：删除这个组产生过的所有日志历史（斩草除根）
		if err := tx.Where("group_id = ?", id).Delete(&model.GroupExecutionLog{}).Error; err != nil {
			return err
		}
		
		// 动作 3：最后删掉这个组本身
		return tx.Delete(&model.TaskGroup{}, id).Error
	})
	if err != nil {
		return 0, 0, err
	}
	
	// 通知车间主任把这个组的排班全撤了
	if s.Engine != nil {
		s.Engine.RemoveGroupSchedule(id) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	
	// 撕报表
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return taskCount, logCount, nil
}

// GetGroupMembers 获取组里所有的成员（任务），按顺序排好队。
func (s *GroupService) GetGroupMembers(groupID uint) ([]model.Task, error) {
	var tasks []model.Task
	if err := s.DB.Where("group_id = ?", groupID).Order("sort_order ASC, id ASC").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// SetGroupMembers 调整组成员名单（重新洗牌）
// taskIDs 是最新的、按顺序排好的任务 ID 列表
func (s *GroupService) SetGroupMembers(groupID uint, taskIDs []uint) error {
	// 1. 先查出“调休前”属于这个组的旧名单。
	// 为什么要查？因为有些人可能被踢出去了，有些人可能刚拉进来。等会儿要通知车间主任更新这些人的班表。
	var oldMemberIDs []uint
	s.DB.Model(&model.Task{}).Where("group_id = ?", groupID).Pluck("id", &oldMemberIDs)

	// 2. 开事务重新洗牌
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// 动作 A：把这个组“清空”，所有原成员全部打回自由身（解约）
		if err := tx.Model(&model.Task{}).Where("group_id = ?", groupID).Update("group_id", nil).Error; err != nil {
			return err
		}
		
		// 动作 B：按照最新名单，挨个重新签合同，并打上排序标签（比如老大排第0，老二排第1）
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

	// 3. 增量同步：不管是“被踢出去”的老员工，还是“被拉进来”的新员工，他们的排班属性都变了。
	// 所以把新老名单合并（affectedIDs），一次性通知车间主任去更新这批人的调度策略。
	affectedIDs := append(oldMemberIDs, taskIDs...)
	if len(affectedIDs) > 0 && s.Engine != nil {
		var affectedTasks []model.Task
		s.DB.Where("id IN ?", affectedIDs).Find(&affectedTasks)
		for _, t := range affectedTasks {
			s.Engine.UpdateTaskSchedule(t) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
		}
	}
	
	// 4. 撕报表
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return nil
}

// GetGroupLogs 获取组的历史日志（带分页功能，比如“第2页，每页10条”）
func (s *GroupService) GetGroupLogs(groupID uint, page, pageSize int) ([]model.GroupExecutionLog, int64, error) {
	var logs []model.GroupExecutionLog
	var total int64
	query := s.DB.Model(&model.GroupExecutionLog{}).Where("group_id = ?", groupID)
	
	// 先数数总共有多少条
	query.Count(&total)
	
	// 算一下跳过多少条（Offset）。比如要看第2页（10条一页），就要跳过前10条，从第11条开始取（Offset=10, Limit=10）。
	offset := (page - 1) * pageSize
	if err := query.Order("id DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

