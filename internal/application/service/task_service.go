// ============================================================
// internal/service/task_service.go - 任务业务逻辑层
//
// 🏗️ 【架构设计·模式对比：CQRS 命令查询职责分离初步体现】
// 在当前 TaskService 中，隐式落地了 CQRS 思想的雏形：
// - Command（命令端）：CreateTask、UpdateTask、DeleteTask 等方法改变系统状态（写DB），
//   同时伴随副作用操作（Side Effects），如失效缓存（InvalidateStatsCache）和重载引擎（UpdateTaskSchedule）。
// - Query（查询端）：ListTasks、GetTask 为纯粹的读取逻辑，不产生副作用。未来高并发下，
//   可将 Query 层拆分并走读库（Read-Replica）甚至异构系统（Elasticsearch）查询，彻底剥离负担。
//
// 📌 【大厂面试·核心考点：缓存与数据库双写一致性 (Cache-Aside Pattern)】
// 面试官问：任务数据变更时，如何保证 DB 和 Redis/本地缓存一致？
// 标准答案：采用 Cache-Aside（旁路缓存）模式 —— 先更新数据库（DB），再淘汰/失效缓存（Invalidate）。
// 反问原理：为何不“先淘汰缓存，再更新DB”？因如果先清缓存，此刻读请求涌入会把 DB 的旧值塞回缓存；
// 为何是“淘汰缓存”而非“更新缓存”？因并发写易导致缓存值相互覆盖错乱，且直接淘汰的计算成本最低，保证最终一致性。
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
//
// 🔬 【底层原理·深度剖析：状态机模式 (State Machine) 在任务流转中的应用】
// 尽管本服务偏重静态元数据管理，但由其驱动执行的任务生命周期严格受制于状态机模式。
// - 核心流转：Pending(就绪排队) -> Running(执行中) -> Success(成功) / Fail(失败) / Timeout(超时)
// - 原理对冲：状态机的转换必须是**单向且原子**的。在执行模块更新状态时，必须使用乐观锁思想：
//   `UPDATE logs SET status='Running' WHERE id=? AND status='Pending'`
//   借此解决并发调度时同一个任务被多个 Worker 抢占导致的重复执行问题。
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
// ⚡ 【性能实战·生产调优：深分页踩坑 (Deep Pagination)】
// 当任务量突破百万级，传统 `OFFSET 1000000 LIMIT 20` 会引发严重性能瓶颈，因 DB 需先遍历并丢弃前100万条。
// 优化手段：
// 1. 游标分页（Cursor Pagination）：摒弃 Page，改为传入最后一条记录的主键 ID (`WHERE id < last_id LIMIT 20`)。
// 2. 延迟关联（Deferred Join）：先用覆盖索引查出 ID，再通过 `IN (ids)` 拉取详情记录。
//
// 🧪 【测试工程·质量保障：Mock 与隔离】
// 对此类携带复杂连表装配逻辑的查询进行单测时，应通过注入 sqlmock 或通过 Testcontainers 启动真实容器 DB 进行隔离测试。
// 严禁对物理全量库发起直接读取验证，测试数据的构造和清理必须确保零侵入与 100% 幂等恢复。
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
//
// 💀 【踩坑血泪·反面教材：双写不一致灾难与并发防重击破】
// 1. 双写不一致：如果代码误写成先 `InvalidateStatsCache()` 再 `Create()`。此时极大概率出现：缓存被清空，并发读请求进库捞到空数据写入缓存，
//    紧接着 Create 事务完成写入。结果 DB 里有新任务，缓存却处于真空遗漏状态！必须坚持写操作：**先持久化落地，最后一步失效缓存**。
// 2. 并发重复写入：当前用 Count 校验存在并发竞争 (Race Condition) 缝隙。极端并发下 A,B 请求均发现 Count 为 0 导致重复创建。
//    终极防御：必须在数据库层针对 `name` 字段附加 `UNIQUE INDEX`（唯一索引），靠 DB 级别的原子约束进行兜底防线拦截。
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
//
// 🏗️ 【架构设计·模式对比：增量更新 vs 全量更新】
// 这里巧妙采用了 `map[string]interface{}` 传递增量更新参数，而非 struct 对象。
// 若用 struct，在 GORM 中默认零值（0, false, ""）会被忽略，导致无法清空字段数据；使用 Map 则可精准声明需要覆盖的字段域，
// 这符合 RESTful API 中的 PATCH 增量更新语义，极大减小了全量更新引发的 ABA 并发覆盖风险。
//
// ⚡ 【性能实战·生产调优：大事务剥离铁律】
// 注意这里的 Cache Invalidate 与 Engine Reload 都没有包裹在 DB 事务闭包内部。
// 事务必须极度纯粹轻量。若将外部 RPC、重载网络调用或 Redis 强求放入 DB 事务，会造成事务拉长（Long-lived Transaction），
// 并发突增时将瞬时打爆数据库连接池，引发服务雪崩。
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

// 🛡️ 【安全攻防·漏洞防线：越权操作 (IDOR) 与硬删除隐患】
// 1. 横向越权 (IDOR)：当前入参仅依赖于 URL 中的 `id`，若外层网关无鉴权，黑客可写脚本递增 ID 扫平所有任务表。必须补充租户 ID/鉴权过滤。
// 2. 物理硬删除风险：当前的直接 Delete 为物理擦除，万一发生手抖或攻击，数据瞬间灰飞烟灭不可追责。
//    企业级生产强烈建议通过 GORM `DeletedAt` 特性实现“软删除（Soft Delete）”，将 DELETE 改为 UPDATE 操作。
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
//
// 💀 【踩坑血泪·反面教材：隐形死锁与环形图风暴 (Cyclic DAG)】
// 1. 事务缺失：这种“先推翻再重建”的操作极度脆弱，若第一步 Delete 执行后恰逢程序 OOM 被杀或机器重启，原有的依赖关系将全部不翼而飞。严苛环境下应辅以事物包裹（Transaction）。
// 2. 有向环形图风暴：当前没有使用如 DFS/Tarjan 算法对 `depIDs` 进行“环路检测 (Cycle Detection)”。若攻击者恶意配置 A 依赖 B，B 依赖 A，调度器解析 DAG 拓扑时将陷入无限递归并引爆栈溢出 (Stack Overflow)！
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
//
// 💀 【踩坑血泪·反面教材：非原子 UPSERT 带来的并发冲突】
// 此处采用“先查询 First 判断有无，再决定 Create 还是 Save”的二段式逻辑。在秒杀等高并发场景下，
// 两个线程可能同时 First 判定无数据并同时进入 Create 分支，引发 Duplicate Key 的物理冲突。
// 大厂高标准实现应通过 DB 的原子级 UPSERT 语句应对，例如 MySQL 的 `INSERT ... ON DUPLICATE KEY UPDATE`，
// 对应 GORM 即为使用 `clause.OnConflict{...}` 模块避免应用层判断。
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

