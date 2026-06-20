// ============================================================
// internal/service/task_service.go - 任务业务逻辑层
//
// 业务逻辑层位于"处理器"和"数据库"之间，负责：
// 1. 对数据增加额外的校验逻辑
// 2. 协调多个数据库操作
// 3. 在数据变更后通知调度器重新加载
//
// 💡 【大厂面试·底层原理扩展：架构分层与贫血/充血模型】
//
// 场景重现：
// 面试官问：什么是 MVC？为什么要把数据库操作（DAO层）和业务逻辑（Service层）拆开？
// 
// 底层剖析与大厂对冲方案：
// 1. 贫血模型与防腐层：如果没有 Service 层，前端的 HTTP 接口（Handler）直接去调 DB（甚至直接写 SQL），
//    这会导致所有的“业务规则”（比如：重名不能添加、删除任务必须删除关联日志）散落在各个接口里。
//    Service 层就是一个防腐层（Anti-Corruption Layer），它把“高内聚”的业务逻辑包裹起来。
// 2. 解耦与可测试性：如果以后我们不用 GORM，换成 MongoDB，只需要重写 DB 层，Service 的逻辑一行不用改。
//    这就是高内聚低耦合。
// ============================================================
package service

import (
	"fmt"                            // 格式化输出
	"cronix/internal/domain/model"   // 数据模型
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
//
// 【大白话解释：什么是分页查询（Pagination）？】
// 假如数据库里有 1000 万个任务，一次性全查出来不仅网卡卡死，浏览器也会崩溃。
// 分页就像看书，你只翻开第 1 页，一页只看 20 行。
// Limit = 每页多少条，Offset = 跳过前面几条。
//
// 💡 【大厂面试·底层原理扩展：海量数据检索与 N+1 查询风暴】
// 
// 场景重现：
// 面试官问：如果列表有 100 条任务，你需要查出每个任务对应的组名。你会怎么写 SQL？
//
// 底层剖析与大厂对冲方案：
// 1. N+1 查询风暴：如果你在一个 for 循环里去 `SELECT * FROM group WHERE id = ?`，这就发起了 100 次网络请求。
//    如果每次网络往返耗时 2ms，这就白白浪费了 200ms。高并发下，这会瞬间把数据库连接池打满，引发连接超时雪崩。
// 2. 内存反查聚合（In-memory Join）：看下面的代码，大厂标配解法。
//    第一步：遍历这 100 个任务，提取出所有非空的 GroupID，组成一个切片。
//    第二步：只发 1 次 SQL：`WHERE id IN (?)`，把这几十个 Group 一次性捞出来。
//    第三步：在 Go 内存里建一个 map，遍历 100 个任务去 map 里查出名字赋值。网络 I/O 从 101 次降到了 2 次。
//
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
	
	// 【数据拼装】填充任务组名称（解决 N+1 查询问题）
	var gids []uint
	for _, t := range tasks {
		if t.GroupID != nil {
			gids = append(gids, *t.GroupID)
		}
	}
	if len(gids) > 0 {
		var groups []model.TaskGroup
		s.DB.Where("id IN ?", gids).Find(&groups) // 一次性批量查出所有的 Group
		gm := make(map[uint]string, len(groups))  // 内存里建个字典
		for _, g := range groups {
			gm[g.ID] = g.Name
		}
		for i := range tasks {                    // 在内存里组装，不给数据库增加压力
			if tasks[i].GroupID != nil {
				tasks[i].GroupName = gm[*tasks[i].GroupID]
			}
		}
	}

	// 填充任务依赖 ID 列表，实现拓扑图（DAG）渲染所需的数据装载
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
	return &task, nil
}

// CreateTask 创建一个新任务，然后通知调度器重新加载
func (s *TaskService) CreateTask(task *model.Task) error {
	// 第一步：检查任务名是否已存在（任务名必须唯一，防重复录入）
	var count int64
	s.DB.Model(&model.Task{}).Where("name = ?", task.Name).Count(&count) 
	if count > 0 {
		return fmt.Errorf("task name already exists: %s", task.Name)
	}
	// 第二步：保存到数据库（落库）
	if err := s.DB.Create(task).Error; err != nil {
		return err
	}
	// 第三步：通知统计员撕掉旧报表（缓存失效）
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	// 第四步：通知车间主任把新任务加入排期表（增量更新）
	return s.Engine.UpdateTaskSchedule(*task) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
}

// UpdateTask 更新一个已有任务的部分字段，然后通知调度器
func (s *TaskService) UpdateTask(id uint, updates map[string]interface{}) error {
	// 第一步：确认任务存在
	var task model.Task
	if err := s.DB.First(&task, id).Error; err != nil {
		return err // 任务不存在
	}
	
	// 第二步：更新指定的字段
	if err := s.DB.Model(&task).Updates(updates).Error; err != nil { // Updates只更新map中提供的字段
		return err
	}
	
	// 第三步：查询更新后的任务完整数据
	var updatedTask model.Task
	if err := s.DB.First(&updatedTask, id).Error; err != nil {
		return err
	}
	
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	
	// @Ref: docs/sps/decisions/20260605_architect_review_daemon_supervisor.md | @Date: 2026-06-05
	// 热更新策略：
	// 如果这是一个“常驻保安”（Daemon任务），通知保安队长（DaemonMonitor）换岗热重载。
	// 否则通知车间主任（Engine）更新定时打铃计划。
	if s.DaemonMon != nil {
		s.DaemonMon.ReloadDaemon(updatedTask)
	}
	return s.Engine.UpdateTaskSchedule(updatedTask) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
}

// DeleteTask 删除任务及其所有关联数据（依赖关系、通知配置、执行日志）
//
// 💡 【大厂面试·底层原理扩展：级联删除与分布式柔性事务】
// 
// 场景重现：
// 面试官问：在微服务里，删除一个用户，要连带删除他的订单、积分、日志。如果中途某个服务挂了怎么办？
//
// 底层剖析与大厂对冲方案：
// 1. 单体本地事务：当前代码演示的是单机 DB 事务。GORM 的 `tx.Transaction` 保证了强一致性。
//    只要 `TaskDep`、`NotifyConfig`、`ExecutionLog`、`Task` 四张表里有任何一个 Delete 失败，就会触底反弹（Rollback）。
// 2. 微服务演进（Saga 模式）：如果这四张表分布在四个不同的数据库里，本地事务就没用了。大厂会怎么做？
//    - 柔性事务（最终一致性）：记录一个“删除进行中”的状态，发 MQ 消息异步删。
//    - 补偿机制（Compensating）：如果在删订单时报错了，系统会发一个“回滚通知”，把已经删掉的积分给补回来。
//

// @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-06-09
func (s *TaskService) DeleteTask(id uint) error {
	// 第一步：确认任务存在
	var task model.Task
	if err := s.DB.First(&task, id).Error; err != nil {
		return err
	}
	// 第二步：用事务原子地删除关联数据和任务本身
	// 若任何一步失败，整体回滚，防止孤儿数据（残留的依赖/日志/通知配置）
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", id).Delete(&model.TaskDep{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", id).Delete(&model.NotifyConfig{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", id).Delete(&model.ExecutionLog{}).Error; err != nil {
			return err
		}
		return tx.Delete(&task).Error
	}); err != nil {
		return err
	}
	
	// 第三步：事务成功后通知调度器移除排期，并撕毁报表缓存
	s.Engine.RemoveTaskSchedule(id) // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	if s.ExecSvc != nil {
		s.ExecSvc.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	if s.DaemonMon != nil {
		s.DaemonMon.StopDaemon(id)
	}
	return nil
}

// GetTaskDeps 获取某个任务的所有依赖关系（即：它是谁的小弟）
func (s *TaskService) GetTaskDeps(taskID uint) ([]model.TaskDep, error) {
	var deps []model.TaskDep
	if err := s.DB.Where("task_id = ?", taskID).Find(&deps).Error; err != nil {
		return nil, err
	}
	return deps, nil
}

// UpdateTaskDeps 替换任务的所有依赖关系（先删后增）
// 
// 💡 【大厂模式：全量覆盖式更新】
// 为什么不对比旧依赖和新依赖去找“谁新增了”、“谁被删了”然后做增量更新？
// 因为依赖关系通常只有几条，写代码去对比计算不仅容易出 Bug，而且对于 DB 来说，
// Delete 几条再 Insert 几条的性能极高。这种“先推翻再重建”的做法在业务系统里非常常见，简单粗暴不出错。
func (s *TaskService) UpdateTaskDeps(taskID uint, depIDs []uint) error {
	// 第一步：把旧的依赖图谱关系一把火烧光
	s.DB.Where("task_id = ?", taskID).Delete(&model.TaskDep{})
	
	// 第二步：逐个建新连接（重新认大哥）
	for _, depID := range depIDs {
		dep := model.TaskDep{TaskID: taskID, DependsOnID: depID}
		if err := s.DB.Create(&dep).Error; err != nil {
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

// GetDaemonTasks 返回所有启用且模式为 daemon 的任务（供 DaemonMonitor 扫描使用，相当于查保安花名册）
func (s *TaskService) GetDaemonTasks() ([]model.Task, error) {
	var tasks []model.Task
	err := s.DB.Where("run_mode = ? AND enabled = ?", "daemon", true).Find(&tasks).Error
	return tasks, err
}

// GetTaskNotify 获取任务的通知配置（发邮件、发钉钉等）
func (s *TaskService) GetTaskNotify(taskID uint) (*model.NotifyConfig, error) {
	var cfg model.NotifyConfig
	// 使用 First 查找，如果没有就返回一个默认空壳而不报错
	err := s.DB.Where("task_id = ?", taskID).First(&cfg).Error
	if err != nil {
		return &model.NotifyConfig{TaskID: taskID}, nil
	}
	return &cfg, nil
}

// UpdateTaskNotify 更新任务的通知配置
func (s *TaskService) UpdateTaskNotify(taskID uint, cfg *model.NotifyConfig) error {
	cfg.TaskID = taskID // 确保绑定的TaskID是URL里的ID，防止越权修改
	
	var exist model.NotifyConfig
	if err := s.DB.Where("task_id = ?", taskID).First(&exist).Error; err != nil {
		// 如果原来没有配置，就无中生有（Create）
		return s.DB.Create(cfg).Error
	}
	
	// 如果原来有配置，就借用它的主键 ID 进行覆盖更新（Save / Update）
	cfg.ID = exist.ID
	return s.DB.Save(cfg).Error
}

