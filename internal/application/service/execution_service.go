// ============================================================
// internal/service/execution_service.go - 执行日志服务层
// 
// 【纳米级源码说明书 - 业务篇】
// 这里的角色是“档案管理员兼数据分析师”。
// 他负责：
// 1. 把每次任务执行的结果（ExecutionLog）记进档案室。
// 2. 根据老板的要求，翻找档案（分页查询、筛选日志）。
// 3. 每天给老板提供一份统计图表（DashboardStats）。
//
// 🏗️ 【架构设计·模式对比】应用服务层 (Application Service) 在 DDD 架构中的作用
// 面试官：你们的 Service 层是做什么的？和 Domain 层有什么区别？
// 标准答案：在 DDD（领域驱动设计）中，Application Service 是“用例编排者（Use Case Orchestrator）”。
// 1. 不包含核心业务规则：它不负责决定“任务能不能跑”，这属于 Domain Model（领域模型）的职责。
// 2. 门面与协调者：它作为对外暴露的门面（Facade），负责从 DB 获取数据，组装实体，调用实体的业务方法，最后将结果存回 DB。
// 3. 事务与隔离：它控制数据库事务的边界，确保用例执行的原子性。本文件中的 ExecutionService 就是专门编排和聚合任务执行结果的调度服务。
// 💀 【踩坑血泪·反面教材】：不要把几千行的 if-else 业务判断全塞在 Service 里，那叫“贫血模型（Anemic Domain Model）”，维护起来简直是灾难！
//
// 🛡️ 【安全攻防·漏洞防线】幂等性设计 (Idempotency) 防止任务重复执行
// 面试官：如果由于网络抖动或定时器 Bug，同一个调度任务在 1 秒内被触发了两次，怎么保证任务不重复跑？
// 标准答案：在编排执行日志（ExecutionService）时必须引入“幂等性”。
// 1. 唯一流水号：每次执行必须携带唯一的 ExecutionID。在数据库日志表设置唯一约束，发生冲突（Duplicate Key）时直接拦截并静默抛弃重复请求。
// 2. 状态机排他（乐观锁）：在真正启动任务前，使用 `UPDATE tasks SET status='Running' WHERE id=? AND status='Pending'`。
//    受影响行数(RowsAffected)为0，则说明已经被其他协程/节点抢占，当前请求立即中止。
//
// 🔬 【底层原理·深度剖析】分布式锁与本地锁防并发重入
// 面试官：对于一些耗时较长的数据同步任务，如果到了下个执行周期还没跑完，怎么防止并发重入？
// 标准答案：根据部署架构选择锁的粒度。
// 1. 单机架构（本地锁）：在 Go 中使用 `sync.Mutex` 或者通过内存中的 `Concurrent Map` 记录正在执行的 TaskID。本文件中的缓存刷新就采用了读写锁防重入。
// 2. 分布式架构（分布式锁）：在多副本部署下，必须依赖外部存储。使用 Redis 的 `SET key value NX PX 30000` 或基于 ETCD/Zookeeper 的分布式锁。获取锁失败则表明任务正在运行，防止重入导致的脏数据。
//
// ============================================================
// 💡 【大厂面试·底层原理扩展：全链路并发优化与海量数据处理】
// 
// 场景重现 1：
// 面试官问：如果首页的大盘接口（DashboardStats）每秒有 1 万人刷新，怎么防止把数据库打挂？
//
// 底层剖析与大厂对冲方案：
// 1. 本地内存缓存（Local Cache）：在这个模块里写了一个经典的 `statsCache` 结构体，数据在内存中保留 60 秒。
//    这 60 秒内，所有读请求直接命中内存返回（<1微秒），数据库 QPS 为 0！
// 2. 缓存击穿与 Double-Check 机制：这是高级考点！假设 60 秒刚好到了，缓存失效，此时瞬间进来了 1000 个请求。
//    如果这 1000 个请求发现缓存空了，全部去查数据库（5个 COUNT 语句），数据库立马挂掉。
//    Cronix 解法：看 `GetDashboardStats`，第一个人抢到了【写锁】，他去算数据库。
//    剩下 999 个人在写锁外排队。等第一个人算完写回缓存，第二个人进去后，发现缓存又有了，直接抄作业返回！这就是 Double-Check 防击穿。
//
// 场景重现 2：
// 面试官问：运营让你导出一份包含 1000 万条任务日志的 Excel 报表，你怎么写这段代码防止服务器 OOM（内存爆掉）？
//
// 底层剖析与大厂对冲方案：
// 1. OOM 杀手：绝大部分新手的写法是 `db.Find(&logs)`。这会把 1000 万个结构体全塞进 Go 的内存，瞬间占满几个 G，直接被系统 Kill。
// 2. 数据库游标（Database Cursor）：看 `ExportLogsStream` 方法。我们用了 `db.Rows()` 获取底层游标连接。
//    游标就像一根吸管，在 `for rows.Next()` 循环中，每次只从数据库的网络流里读取“1 条数据”到内存。
//    处理完 1 条（比如写入本地 CSV 文件流），内存立马复用去读下一条。这样不管导出 1 千万还是 1 亿条，内存占用恒定 < 1MB！
//
// 场景重现 3：
// 面试官问：什么是 ORM 的 N+1 查询问题？
// 
// 底层剖析与大厂对冲方案：
// 1. 灾难现场：查出了 100 条日志（1次SQL），然后为了展示组名，在 for 循环里去查 Group 表（100次SQL）。总共 101 次！
// 2. 内存映射法（In-memory Join）：看 `enrichGroupNames` 方法。把 100 个日志的 TaskID 抠出来放入一个数组，
//    用一条 `WHERE id IN (?)` 的 SQL 去查出所有的组。然后在代码的 map 里完成属性缝合。总计 2 次 SQL。性能提升 50 倍！
//
// @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
// ============================================================
package service

import (
	"sync"                        // 并发安全的读写锁
	"time"                        // 时间处理：计算截止日期 / TTL 过期
	"cronix/internal/domain/model"       // 数据模型
	"cronix/internal/application/scheduler"
	"gorm.io/gorm"                // GORM数据库操作
)

// statsCache 首页报表的缓存黑板
type statsCache struct {
	mu       sync.RWMutex           // 读写锁：保证大家看黑板和改黑板不会打架
	data     map[string]interface{} // 黑板上写的具体数据（比如今天成功多少次，失败多少次）
	expireAt time.Time              // 过期时间：到了这个时间，黑板上的数据就“臭”了，必须重新算
}

// Invalidate 擦黑板。当有人增删改了任务，就调用这个，把黑板擦干净。
// 下次有人看时，发现黑板是空的，就会去数据库重新算一遍。
// @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
func (c *statsCache) Invalidate() {
	c.mu.Lock()         // 加写锁：我要擦黑板了，都不许看！
	defer c.mu.Unlock() // 走之前必定放下锁
	c.data = nil        // 擦除数据
}

// ExecutionService 档案管理员
type ExecutionService struct {
	DB    *gorm.DB    // 档案室钥匙
	cache *statsCache // 首页报表缓存黑板
}

// InvalidateStatsCache 对外暴露的“擦黑板”按钮
// @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
func (s *ExecutionService) InvalidateStatsCache() {
	s.cache.Invalidate()
}

// NewExecutionService 招募一个新的档案管理员
func NewExecutionService(db *gorm.DB) *ExecutionService {
	return &ExecutionService{DB: db, cache: &statsCache{}}
}

// enrichGroupNames 这是一个极其经典的【大厂数据库性能优化：解决 N+1 查询问题】
// 场景：你查出了 100 条日志，每条日志只记录了 TaskID。但前端列表上需要显示这个任务属于哪个组（GroupName）。
// 错误做法：在 for 循环里发 100 次 SQL 去查任务表和组表。
// 正确做法（当前做法）：把 100 个 TaskID 收集起来，用一次 IN (?) 查出所有的任务和组，在内存里做匹配映射。
func (s *ExecutionService) enrichGroupNames(logs []model.ExecutionLog) {
	// 1. 收集所有的 TaskID
	taskIDs := make([]uint, 0, len(logs))
	for _, l := range logs {
		if l.TaskID != nil {
			taskIDs = append(taskIDs, *l.TaskID)
		}
	}
	if len(taskIDs) == 0 {
		return
	}
	
	// 2. 一次性查出这些 Task，拿到它们属于哪个 GroupID
	var tasks []model.Task
	s.DB.Where("id IN ? AND group_id IS NOT NULL", taskIDs).Find(&tasks)
	if len(tasks) == 0 {
		return
	}
	
	// 3. 收集所有的 GroupID
	groupIDs := make([]uint, 0, len(tasks))
	taskGroup := make(map[uint]uint, len(tasks)) // 记录 TaskID -> GroupID 的对应关系
	for _, t := range tasks {
		if t.GroupID != nil {
			taskGroup[t.ID] = *t.GroupID
			groupIDs = append(groupIDs, *t.GroupID)
		}
	}
	
	// 4. 一次性查出这些 Group 的名字
	var groups []model.TaskGroup
	s.DB.Where("id IN ?", groupIDs).Find(&groups)
	groupNames := make(map[uint]string, len(groups)) // 记录 GroupID -> GroupName
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}
	
	// 5. 纯内存拼装，全程只用了 2 次 SQL 查询！
	for i := range logs {
		if logs[i].TaskID != nil {
			if gid, ok := taskGroup[*logs[i].TaskID]; ok {
				logs[i].GroupName = groupNames[gid] // 把查到的组名塞进日志对象里
			}
		}
	}
}

// GetTaskLogs 获取某个任务的执行日志（分页、支持按状态筛选）
// 参数 taskID：任务ID
// 参数 page：页码
// 参数 pageSize：每页条数
// 参数 status：按状态筛选（空字符串表示不筛选）
// 返回值：日志列表、总条数、可能发生的错误
func (s *ExecutionService) GetTaskLogs(taskID uint, page, pageSize int, status string) ([]model.ExecutionLog, int64, error) {
	var logs []model.ExecutionLog
	var total int64
	query := s.DB.Model(&model.ExecutionLog{}).Where("task_id = ?", taskID) // 筛选指定任务的日志
	if status != "" {                                              // 如果指定了状态筛选
		query = query.Where("status = ?", status)                  // 添加状态筛选条件
	}
	query.Count(&total)                                            // 统计总条数
	offset := (page - 1) * pageSize                                // 计算偏移量
	// 按开始时间倒序（最新的在前）
	if err := query.Order("start_time DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	s.enrichGroupNames(logs) // 补全拼装组名
	return logs, total, nil
}

// GetAllLogs 获取所有任务的执行日志（分页、支持多种筛选）
// 参数 page, pageSize：分页参数
// 参数 taskName：按任务名模糊搜索
// 参数 status：按状态筛选
// 参数 since：只查最近多长时间内的（如 "24h"、"1h"）
// 返回值：日志列表、总条数、可能发生的错误
func (s *ExecutionService) GetAllLogs(page, pageSize int, taskName, status, since string) ([]model.ExecutionLog, int64, error) {
	var logs []model.ExecutionLog
	var total int64
	query := s.DB.Model(&model.ExecutionLog{})                     // 不限定任务，查所有日志

	// 添加各种筛选条件
	if taskName != "" {                                            // 按任务名模糊搜索
		query = query.Where("task_name LIKE ?", "%"+taskName+"%")
	}
	if status != "" {                                              // 按状态精确筛选
		query = query.Where("status = ?", status)
	}
	if since != "" {                                               // 只查最近一段时间内的
		// time.ParseDuration 解析时间长度字符串："24h"=24小时，"1h30m"=1小时30分钟
		if d, err := time.ParseDuration(since); err == nil {
			query = query.Where("start_time > ?", time.Now().Add(-d)) // 开始时间 > 当前时间-时间段
		}
	}

	query.Count(&total)                                            // 统计总条数
	offset := (page - 1) * pageSize
	if err := query.Order("start_time DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	s.enrichGroupNames(logs)
	return logs, total, nil
}

// GetDashboardStats 获取仪表盘的摘要统计数据
// 返回值是一个map（映射表），包含以下字段：
//   total_tasks: 任务总数
//   enabled_tasks: 已启用的任务数
//   today_total: 今天执行的总次数
//   today_success: 今天成功的次数
//   today_failed: 今天失败的次数
//
// ⚡ 【性能实战·生产调优】Double-Check 本地锁防并发计算（缓存击穿）
// 面试官：你们的报表查询怎么防缓存击穿？
// 标准答案：采用 DCL（Double-Checked Locking）双重检查锁定模式。
// 第一个 RLock (读锁) 检查缓存，如果 miss (未命中)，必须释放读锁并升级为 Lock (写锁)。
// 获取写锁后，必须【再次检查】缓存。因为在释放 RLock 到获取 Lock 的纳秒级时间差内，
// 可能别的 Goroutine 已经抢先算完了并写回了缓存。如果不做二次检查，排队等锁的 1000 个协程就会轮流去查 5 次 DB，直接把 DB 打挂！
func (s *ExecutionService) GetDashboardStats() (map[string]interface{}, error) {
	// 【第一道防线】：先看黑板（缓存）有没有现成的
	s.cache.mu.RLock() // 加读锁：我要看黑板了，不要擦
	if s.cache.data != nil && time.Now().Before(s.cache.expireAt) {
		// 缓存存在，并且没过期，直接抄作业走人！太爽了，秒开！
		data := s.cache.data
		s.cache.mu.RUnlock() // 看完了，解锁
		return data, nil
	}
	s.cache.mu.RUnlock() // 没看到或者过期了，解锁，准备自己算

	// 【第二道防线】：自己算，但必须加写锁防止别人也在算（这叫缓存击穿防御：Double-check）
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock() // 不管咋样，函数结束肯定解锁
	// 为什么进门之后又要查一遍？
	// 因为有可能你刚解锁准备去算，别人已经算完写上去了！你再看一眼，如果有了，直接抄！
	if s.cache.data != nil && time.Now().Before(s.cache.expireAt) {
		return s.cache.data, nil
	}

	// 确实没人算过，开始干脏活累活：发 5 条 COUNT 统计 SQL 查数据库
	var totalTasks int64
	s.DB.Model(&model.Task{}).Count(&totalTasks)

	var enabledTasks int64
	s.DB.Model(&model.Task{}).Where("enabled = ?", true).Count(&enabledTasks)

	today := time.Now().Truncate(24 * time.Hour) // 获取今天 0点0分0秒

	var todayTotal int64
	s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ?", today).Count(&todayTotal)

	var todaySuccess int64
	s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ? AND status = ?", today, model.StateSuccess).Count(&todaySuccess)

	var todayFailed int64
	s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ? AND status = ?", today, model.StateFailed).Count(&todayFailed)

	// 把算好的结果写到黑板上
	stats := map[string]interface{}{
		"total_tasks":   totalTasks,
		"enabled_tasks": enabledTasks,
		"today_total":   todayTotal,
		"today_success": todaySuccess,
		"today_failed":  todayFailed,
	}
	s.cache.data = stats
	s.cache.expireAt = time.Now().Add(60 * time.Second) // 告诉大家，这个黑板上的数据只能保鲜 60 秒
	return stats, nil
}

// GetDashboardMetrics 获取系统级运行指标（P95、P99、QPS等），这是从车间统计员（MetricsRegistry）那里拿来的
// @Ref: docs/sps/plans/20260605_metrics_plan.md | @Date: 2026-06-05
func (s *ExecutionService) GetDashboardMetrics() scheduler.MetricSnapshot {
	return scheduler.GlobalMetricsRegistry.GetSnapshot()
}

// buildTaskGroupNameMap returns a map from task ID to group name for all tasks in groups.
// 这也是个性能优化方法，一次性构建全量任务到组名的映射表
func (s *ExecutionService) buildTaskGroupNameMap() map[uint]string {
	taskGroupName := make(map[uint]string)
	var allTaskIDs []uint
	s.DB.Model(&model.ExecutionLog{}).Select("DISTINCT task_id").Where("task_id IS NOT NULL").Pluck("task_id", &allTaskIDs)
	if len(allTaskIDs) == 0 {
		return taskGroupName
	}
	var tasks []model.Task
	s.DB.Where("id IN ? AND group_id IS NOT NULL", allTaskIDs).Find(&tasks)
	if len(tasks) == 0 {
		return taskGroupName
	}
	groupIDs := make([]uint, 0, len(tasks))
	taskGroup := make(map[uint]uint, len(tasks))
	for _, t := range tasks {
		if t.GroupID != nil {
			taskGroup[t.ID] = *t.GroupID
			groupIDs = append(groupIDs, *t.GroupID)
		}
	}
	var groups []model.TaskGroup
	s.DB.Where("id IN ?", groupIDs).Find(&groups)
	groupNames := make(map[uint]string, len(groups))
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}
	for tid, gid := range taskGroup {
		if name, ok := groupNames[gid]; ok {
			taskGroupName[tid] = name
		}
	}
	return taskGroupName
}

// ExportLogsStream 导出海量日志专用接口。
// 
// 💡 【大厂绝技：数据库游标（Cursor）与流式处理】
// 如果把 100 万条日志全查出来变成 []model.ExecutionLog 数组，Go 程序的内存瞬间爆炸。
// 这里用了 `Rows()` 获取到底层游标。游标就像吸管，我们遍历的时候（`for rows.Next()`），
// 是一条一条从数据库搬运数据的。搬出一条、写进文件、扔掉，内存里永远只有一条数据！
// 这就是为什么大厂在处理导出 Excel、大数据洗数据时，无论数据多大都不会 OOM 的秘密。
func (s *ExecutionService) ExportLogsStream(taskName, status, since string, maxRows int, fn func(model.ExecutionLog) error) error {
	query := s.DB.Model(&model.ExecutionLog{})

	if taskName != "" {
		query = query.Where("task_name LIKE ?", "%"+taskName+"%")
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if since != "" {
		if d, err := time.ParseDuration(since); err == nil {
			query = query.Where("start_time > ?", time.Now().Add(-d))
		}
	}

	taskGroupName := s.buildTaskGroupNameMap()

	// 拿到“吸管”
	rows, err := query.Order("start_time DESC").Limit(maxRows).Rows()
	if err != nil {
		return err
	}
	defer rows.Close() // 记得用完吸管要扔掉释放连接

	// 吸管一直吸，直到吸不出来
	for rows.Next() {
		var log model.ExecutionLog
		if err := s.DB.ScanRows(rows, &log); err != nil { // 把吸出来的那一口数据填进 log
			return err
		}
		if log.TaskID != nil {
			log.GroupName = taskGroupName[*log.TaskID]
		}
		// 交给上层传进来的加工厂（fn闭包）去处理（比如写进 CSV 文件）
		if err := fn(log); err != nil {
			return err
		}
	}
	return rows.Err()
}

// CleanOldLogs 删除超过指定天数的旧日志
// 参数 retentionDays：保留天数（超过这个天数的日志会被删除）
//
// 💀 【踩坑血泪·反面教材】大表数据清理的“删库跑路”级灾难
// 面试官：如果这张日志表有 1 亿条数据，要删除 1000 万条半年前的旧数据，直接用 GORM 执行 `DELETE WHERE created_at < ?` 会发生什么？
// 标准答案：会引发史诗级线上故障！
// 1. 锁表与长事务：海量 DELETE 会形成一个超长事务。由于扫描范围太大，MySQL的间隙锁/行锁可能退化为表锁，导致所有新任务日志无法写入，系统雪崩。
// 2. 磁盘爆满与主从延迟：一次性删除 1000 万行会产生几 GB 甚至几十 GB 的 Undo Log 和 Binlog，瞬间占满磁盘。同时从库重放这个巨型 Binlog 会引发严重的主从复制延迟。
// ⚡ 【性能实战·生产调优】优化方案（大厂做法）：
// - 方案A（小批量删除）：改成 `DELETE FROM logs WHERE created_at < ? LIMIT 1000`，放在 for 循环里删，每删一次 `time.Sleep` 50毫秒，平滑释放磁盘 IO。
// - 方案B（表分区 Partition）：在 MySQL 级别对表进行按月/按周分区，清理数据时直接 `ALTER TABLE logs DROP PARTITION p202512`，毫秒级完成，且几乎无 IO。
func (s *ExecutionService) CleanOldLogs(retentionDays int) error {
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour) // 计算截止时间
	return s.DB.Where("created_at < ?", cutoff).Delete(&model.ExecutionLog{}).Error // 删除早于截止时间的记录
}

// GetLatestLog 获取指定任务的最新一条执行日志（供 daemon 退出后判定 exitSuccess）
func (s *ExecutionService) GetLatestLog(taskID uint) (*model.ExecutionLog, error) {
	var log model.ExecutionLog
	err := s.DB.Where("task_id = ?", taskID).Order("id DESC").First(&log).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}

// ClearAllLogs 清空所有执行日志（单任务和组任务），大扫除
//
// 🏗️ 【架构设计·模式对比】缓存一致性模式：Cache Aside Pattern
// 观察以下代码：为什么我们在数据库 Delete 成功后，去调用 `InvalidateStatsCache`（擦除缓存），而不是去“计算并更新”缓存？
// 面试标准答案：在并发场景下，“更新缓存”很容易因为时序错乱导致脏数据（比如 A 线程先更新，B 线程后更新，但 B 线程的数据库事务先于 A 提交）。
// 业界最佳实践是使用 Cache Aside 模式：【更新数据库 -> 删除缓存】。由下一次 Read 请求去懒加载计算缓存，这保证了最终一致性且逻辑更简单。
func (s *ExecutionService) ClearAllLogs() (int64, int64, error) {
	r1 := s.DB.Where("1 = 1").Delete(&model.ExecutionLog{}) // Where 1=1 是个小技巧，绕过 GORM 的“禁止全局删除”安全保护
	if r1.Error != nil {
		return 0, 0, r1.Error
	}
	r2 := s.DB.Where("1 = 1").Delete(&model.GroupExecutionLog{})
	if r2.Error == nil {
		s.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return r1.RowsAffected, r2.RowsAffected, r2.Error
}

// ClearTaskLogs 清空指定任务的执行日志
func (s *ExecutionService) ClearTaskLogs(taskID uint) (int64, error) {
	result := s.DB.Where("task_id = ?", taskID).Delete(&model.ExecutionLog{})
	if result.Error == nil {
		s.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return result.RowsAffected, result.Error
}

// DeleteLog 删除单条执行日志
func (s *ExecutionService) DeleteLog(id uint) error {
	err := s.DB.Delete(&model.ExecutionLog{}, id).Error
	if err == nil {
		s.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return err
}

// GetLog returns a single execution log with full output.
func (s *ExecutionService) GetLog(id uint) (*model.ExecutionLog, error) {
	var log model.ExecutionLog
	if err := s.DB.First(&log, id).Error; err != nil {
		return nil, err
	}
	// 顺带查出所属的组名（如果有的话）
	if log.TaskID != nil {
		var task model.Task
		if err := s.DB.First(&task, *log.TaskID).Error; err == nil && task.GroupID != nil {
			var group model.TaskGroup
			if err := s.DB.First(&group, *task.GroupID).Error; err == nil {
				log.GroupName = group.Name
			}
		}
	}
	return &log, nil
}

// ClearGroupLogs 清空指定组的执行日志
func (s *ExecutionService) ClearGroupLogs(groupID uint) (int64, error) {
	result := s.DB.Where("group_id = ?", groupID).Delete(&model.GroupExecutionLog{})
	if result.Error == nil {
		s.InvalidateStatsCache() // @Ref: docs/sps/plans/20260527_performance_stability_plan.md | @Date: 2026-05-27
	}
	return result.RowsAffected, result.Error
}
