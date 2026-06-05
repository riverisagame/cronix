// ============================================================
// internal/service/task_service.go - 任务业务逻辑层
//
// 业务逻辑层位于"处理器"和"数据库"之间，负责：
// 1. 对数据增加额外的校验逻辑
// 2. 协调多个数据库操作
// 3. 在数据变更后通知调度器重新加载
// ============================================================
package service

import (
    "fmt"                            // 格式化输出
    "cronix/internal/model"          // 数据模型
    "gorm.io/gorm"                   // GORM数据库操作
)

// TaskService 是任务管理的业务服务层
// 它持有数据库连接和调度引擎，用来操作任务和通知调度器
type TaskService struct {
	DB        *gorm.DB         // 数据库连接对象
	Engine    TaskReloader     // 定时调度引擎（任务变更后要通知它重新加载）
	ExecSvc   StatsInvalidator // 执行日志服务层（用于缓存失效） // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	DaemonMon DaemonReloader   // 守护控制器：热更新 daemon 任务
}

// ListTasks 返回分页的任务列表，支持按名称搜索
// 参数 page：第几页（从1开始）
// 参数 pageSize：每页多少条
// 参数 search：搜索关键词（匹配任务名）
// 返回值：任务列表、总条数、可能发生的错误
func (s *TaskService) ListTasks(page, pageSize int, search string) ([]model.Task, int64, error) {
    var tasks []model.Task                                       // 存放查询结果
    var total int64                                              // 存放总条数
    query := s.DB.Model(&model.Task{})                            // 创建任务表的查询对象
    if search != "" {                                            // 如果用户传了搜索关键词
        // LIKE是SQL的模糊匹配，%表示任意字符
        query = query.Where("name LIKE ?", "%"+search+"%")       // 按名称模糊搜索
    }
    query.Count(&total)                                          // 先统计符合条件的总条数
    offset := (page - 1) * pageSize                              // 计算偏移量：跳过前面的页
    // 按创建时间倒序（最新的在前面）、分页、查询
    if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&tasks).Error; err != nil {
        return nil, 0, err
    }
    // 填充任务组名称
    var gids []uint
    for _, t := range tasks {
        if t.GroupID != nil {
            gids = append(gids, *t.GroupID)
        }
    }
    if len(gids) > 0 {
        var groups []model.TaskGroup
        s.DB.Where("id IN ?", gids).Find(&groups)
        gm := make(map[uint]string, len(groups))
        for _, g := range groups {
            gm[g.ID] = g.Name
        }
        for i := range tasks {
            if tasks[i].GroupID != nil {
                tasks[i].GroupName = gm[*tasks[i].GroupID]
            }
        }
    }

    // 填充任务依赖 ID 列表，实现拓扑图渲染所需的数据装载
    // @Ref: docs/sps/plans/20260527_topology_shutdown_plan.md | @Date: 2026-05-27
    if len(tasks) > 0 {
        var tids []uint
        for _, t := range tasks {
            tids = append(tids, t.ID)
        }
        var deps []model.TaskDep
        if err := s.DB.Where("task_id IN ?", tids).Find(&deps).Error; err == nil {
            depMap := make(map[uint][]uint)
            for _, d := range deps {
                depMap[d.TaskID] = append(depMap[d.TaskID], d.DependsOnID)
            }
            for i := range tasks {
                tasks[i].DependsOnIDs = depMap[tasks[i].ID]
            }
        }
    }

    return tasks, total, nil
}

// GetTask 根据ID获取单个任务的详细信息
// 参数 id：任务ID
// 返回值：任务对象指针、可能发生的错误
func (s *TaskService) GetTask(id uint) (*model.Task, error) {
    var task model.Task
    if err := s.DB.First(&task, id).Error; err != nil {          // First：按主键查找第一条记录
        return nil, err
    }
    if task.GroupID != nil {
        var g model.TaskGroup
        if err := s.DB.First(&g, *task.GroupID).Error; err == nil {
            task.GroupName = g.Name
        }
    }
    return &task, nil                                            // 返回任务指针
}

// CreateTask 创建一个新任务，然后通知调度器重新加载
func (s *TaskService) CreateTask(task *model.Task) error {
	// 第一步：检查任务名是否已存在（任务名必须唯一）
	var count int64
	s.DB.Model(&model.Task{}).Where("name = ?", task.Name).Count(&count) // 统计同名任务数量
	if count > 0 {
		return fmt.Errorf("task name already exists: %s", task.Name)
	}
	// 第二步：保存到数据库
	if err := s.DB.Create(task).Error; err != nil {               // Create 插入新记录
		return err
	}
	// 第三步：通知调度器增量更新，失效缓存
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return s.Engine.UpdateTaskSchedule(*task) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
}

// UpdateTask 更新一个已有任务的部分字段，然后通知调度器
func (s *TaskService) UpdateTask(id uint, updates map[string]interface{}) error {
	// 第一步：确认任务存在
	var task model.Task
	if err := s.DB.First(&task, id).Error; err != nil {           // 查找任务
		return err                                                // 任务不存在
	}
	// 第二步：更新指定的字段
	if err := s.DB.Model(&task).Updates(updates).Error; err != nil { // Updates只更新map中提供的字段
		return err
	}
	// 第三步：查询更新后的任务，执行调度增量更新，失效缓存
	var updatedTask model.Task
	if err := s.DB.First(&updatedTask, id).Error; err != nil {
		return err
	}
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	
	// @Ref: docs/sps/decisions/20260605_architect_review_daemon_supervisor.md | @Date: 2026-06-05
	// 热更新：如果是常驻守护任务，通知 DaemonMonitor 热重载；否则走旧逻辑
	if s.DaemonMon != nil {
		s.DaemonMon.ReloadDaemon(updatedTask)
	}
	return s.Engine.UpdateTaskSchedule(updatedTask) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
}

// DeleteTask 删除任务及其所有关联数据（依赖关系、通知配置、执行日志）
func (s *TaskService) DeleteTask(id uint) error {
	// 第一步：确认任务存在
	var task model.Task
	if err := s.DB.First(&task, id).Error; err != nil {
		return err
	}
	// 第二步：删除关联数据（GORM的级联删除，先删子表再删主表）
	s.DB.Where("task_id = ?", id).Delete(&model.TaskDep{})       // 删除依赖关系
	s.DB.Where("task_id = ?", id).Delete(&model.NotifyConfig{})   // 删除通知配置
	s.DB.Where("task_id = ?", id).Delete(&model.ExecutionLog{})   // 删除执行日志
	// 第三步：删除任务本身
	if err := s.DB.Delete(&task).Error; err != nil {
		return err
	}
	// 第四步：通知调度器安全移除，并失效缓存
	s.Engine.RemoveTaskSchedule(id) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	if s.DaemonMon != nil {
		s.DaemonMon.StopDaemon(id)
	}
	return nil
}

// GetTaskDeps 获取某个任务的所有依赖关系
func (s *TaskService) GetTaskDeps(taskID uint) ([]model.TaskDep, error) {
	var deps []model.TaskDep
	if err := s.DB.Where("task_id = ?", taskID).Find(&deps).Error; err != nil { // 查询该任务的所有依赖
		return nil, err
	}
	return deps, nil
}

// UpdateTaskDeps 替换任务的所有依赖关系（先删后增）
func (s *TaskService) UpdateTaskDeps(taskID uint, depIDs []uint) error {
	// 第一步：删除该任务的所有旧依赖
	s.DB.Where("task_id = ?", taskID).Delete(&model.TaskDep{})
	// 第二步：逐个创建新的依赖关系
	for _, depID := range depIDs {                               // 遍历每个依赖ID
		dep := model.TaskDep{TaskID: taskID, DependsOnID: depID} // 构造依赖记录（任务taskID依赖于depID）
		if err := s.DB.Create(&dep).Error; err != nil {           // 插入数据库
			return err
		}
	}
	// 第三步：通知调度器安全更新（可选，但安全），并失效缓存
	var task model.Task
	if err := s.DB.First(&task, taskID).Error; err == nil {
		s.Engine.UpdateTaskSchedule(task) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return nil
}
