// ============================================================
// internal/application/service/group_service.go - 任务组业务逻辑服务
//
// 🏗️ 【架构设计·模式对比：事务聚合 (Unit of Work) 与领域驱动设计】
// 在这里，GroupService 扮演了应用服务（Application Service）的角色。
// 它通过事务聚合（Unit of Work）模式，确保领域模型的状态变更与外部系统（如调度引擎、缓存）的一致性。
// 相比于传统的 ActiveRecord 模式，这种设计将业务逻辑与数据访问解耦，使得服务更容易测试和扩展。
// 
// 🛡️ 【安全攻防·漏洞防线：级联操作的业务一致性保障】
// 在处理包含子元素的领域实体（如 TaskGroup 包含多个 Task）时，级联操作（如删除组时更新任务状态）极易出现数据孤岛。
// 本服务通过严格的数据库事务（DB Transaction）结合应用层的手动补偿机制（如更新内存调度器），
// 构筑了“数据库物理一致性 + 应用层逻辑一致性”的双重防线，防止出现脏任务（属于已删除组的任务被错误执行）。
//
// 【纳米级源码说明书 - 业务篇】
// 这里的角色是“项目组HR主管”。
// 负责组建项目组（TaskGroup）、修改组属性、解散项目组、分配组成员（Task）。
// 当项目组有变动时，他不仅要更新花名册（DB），还要同步通知：
// 1. 发动机车间主任（Engine / GroupReloader）：打铃策略变了！
// 2. 统计报表员（ExecSvc / StatsInvalidator）：有数据变了，之前的缓存报表作废！
//
// ============================================================
// 💡 【大厂面试·底层原理扩展：分布式事务与缓存一致性】
// 
// 场景重现 1：
// 面试官问：如果在高并发下，用户正在删除组（DeleteGroup），系统刚好掉电了，数据库里的数据会不一致吗？
//
// 底层剖析与大厂对冲方案（ACID 与 本地事务）：
// 1. OOM与断电的幽灵：如果不用事务，先删组，再删组内任务，只要中间系统崩溃，就会留下大量没有归属的“孤儿任务”。
// 2. ACID 保证：GORM 的 `tx.Transaction` 利用了数据库（如 MySQL/PostgreSQL）底层的 Undo Log（回滚日志）和 Redo Log（重做日志）。
//    执行动作前，它会把“怎么恢复这些数据”写进磁盘。一旦断电重启，数据库会自动沿着 Undo Log 把没做完的残次品全部擦除。
//    这就是事务的 Atomicity（原子性）：要么全有，要么全无。
//
// 场景重现 2：
// 面试官问：这里每次修改组（UpdateGroup）之后，都会调用 `InvalidateStatsCache()`。这是什么设计模式？
//
// 底层剖析与大厂对冲方案（缓存失效模式 - Cache Aside）：
// 1. 经典读写策略：这是业界标准的 Cache-Aside（旁路缓存）模式的写入部分。
//    更新策略永远是：先更新数据库（写 DB），成功后再使缓存失效（删 Cache）。
//    绝对不要“先删缓存，再写数据库”（容易产生并发读写带来的死数据），也不要试图去“更新缓存”（因为你不知道新缓存怎么算，干脆删了让别人重新算）。
// 2. 脏数据窗口期防御：即使删除了缓存，由于大屏缓存有 60s 的过期时间（TTL），如果在 60s 内不删除，
//    用户新建了组，大屏上的数据依然不会变，这叫脏数据（Stale Data）。主动调用 `InvalidateStatsCache` 就是主动撕碎脏数据，强迫下一次读请求走 DB 加载。
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
//
// 🔬 【底层原理·深度剖析：内存与存储的双写一致性】
// 创建操作本身较为简单，只有单一表的写入。然而，这里涉及到了"写库 -> 内存引擎更新 -> 缓存清除"的三步走策略。
// 若第二步或第三步失败，是否需要回滚第一步的写库操作？
// 在当前设计中，为了避免分布式事务的复杂性（如两阶段提交 2PC），采用了"最大努力通知 (Best-Effort Delivery)"机制，允许瞬间的不一致，通过缓存 TTL 和重启加载等手段实现最终一致性。
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
//
// 📌 【大厂面试·核心考点：并发场景下对同一个组修改的乐观锁 (Optimistic Locking) 控制机制】
// 面试官问：如果两个管理员同时编辑同一个组，会不会出现互相覆盖（Lost Update 丢失更新）的情况？
// 标准答案：
// 1. 当前实现使用了 GORM 的 Updates 方法进行局部更新（Patch），在一定程度上缓解了全量覆盖的问题，但仍存在状态不一致风险。
// 2. 生产级优化（乐观锁）：通常会在表结构中引入 `version` 字段。更新时加上条件 `UPDATE groups SET ..., version = version + 1 WHERE id = ? AND version = 旧版本号`。
//    如果影响行数为 0，说明数据在中途被其他人改了，系统直接返回 `ErrConcurrentUpdate` 或 HTTP 409 Conflict，提示用户刷新后重试。
//
// 💀 【踩坑血泪·反面教材：脏写覆盖】
// 曾有系统允许用户A和B同时打开组配置页面。A把模式改为了"串行"，B没有修改模式，但修改了组名并保存。
// 如果用全量结构体保存（Save），B保存时会带上旧的"并行"模式，导致A的修改被无声覆盖。
// 这里通过 map[string]interface{} 只更新指定字段，避免了部分并发覆盖问题，但对于高度敏感状态依然需要乐观锁保护业务一致性。
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
//
// 🔬 【底层原理·深度剖析：事务聚合 (Unit of Work) 与级联操作的业务一致性保障】
// 这个方法是 Unit of Work 模式的经典体现。解散一个组不仅是删一条记录，而是牵一发而动全身：
// 1. 任务解绑：切断 Task 与 Group 的级联关联。
// 2. 历史清理：删除 GroupExecutionLog。
// 3. 实体销毁：删除 Group 本身。
// 任何一步失败，必须全盘回滚。MySQL 在底层会分配一个事务 ID（XID），并在 Undo Log 记录所有前置镜像（Before Image）。
// 只要不执行 COMMIT，其他会话（基于 Read Committed 或 Repeatable Read 隔离级别）看不到中间状态。这即是保证业务一致性的底层基石。
//
// ⚡ 【性能实战·生产调优：大事务的危害】
// 此处的级联操作中，如果 logCount 达到百万级别，`Delete` 操作会长期持有行锁（甚至锁表），导致数据库主从延迟飙升、其他服务雪崩。
// 生产调优手段：把同步的硬删除（Hard Delete）改为异步软删除（Soft Delete），或者分批删除（Batch Delete），把大事务拆散为多个小事务。
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
//
// 🧪 【测试工程·质量保障：Mock 与 物理零污染】
// 测试此复杂级联操作时，必须采用 Mock 机制（如 sqlmock）或使用隔离的测试数据库。
// 如果直接在开发库跑测试代码，不仅可能破坏真实的 `group_id` 关系，还会误触发 `Engine.UpdateTaskSchedule`，
// 导致真实的调度器被注入脏数据（Meta-Test 的绝对禁忌）。
// 
// 🏗️ 【架构设计·模式对比：全量覆写 vs 增量更新】
// 这里采用了“全量清空 + 重新绑定”的模式，并在外层包裹了数据库事务 (DB.Transaction) 成为一个 Unit of Work。
// 优点：代码极其简单，天然防止遗漏，保证绝对的业务一致性保障；
// 缺点：如果组成员有1000个，但只调整了其中1个的位置，却要更新1000次记录。
// 生产架构设计中，若列表极长，应该采用“计算 Diff (增删改)”的增量更新策略，以降低 DB 写入压力，防止产生过长的事务锁定。
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

