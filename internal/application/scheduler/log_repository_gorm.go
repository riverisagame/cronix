// ============================================================
// internal/scheduler/log_repository_gorm.go - LogRepository 的 GORM 实现
//
// 【纳米级源码说明书 - 架构篇】
// 这是 log_repository.go 里那份《岗位说明书》的具体执行者（实习生）。
// 他叫 GormLogRepository，他的特长是使用 GORM（Go语言最火的ORM框架）来操作数据库。
//
// 🏗️ 【架构设计·模式对比】
// 鸭子类型（Duck Typing）：只要这个结构体把接口里要求的所有方法都实现了，
// Go 语言就会自动承认它是该接口的实现。不需要像 Java 那样显式写 "implements"。
// 仓储模式（Repository Pattern）：屏蔽底层数据库细节，将 DB 层面的 "表操作"
// 转换为 Domain 层的 "领域模型对象操作"（如 ExecutionLog）。上层调用者完全
// 不用关心底层用的是 SQLite 还是 MySQL，甚至内存数组。
//
// 🔬 【底层原理·深度剖析】
// GORM 底层 SQL 生成过程：
// 1. 链式调用：调用 `db.Where().First()` 时，实际是在构建内部的 Statement（AST 抽象语法树）。
// 2. 编译组装：触发执行动作（如 Find/Create/Delete/Count）时，SQL Builder 会解析 AST 的各个
//    子句（Clauses），将其合并成带有 `?` 占位符的预编译 SQL 字符串。
// 3. 驱动适配：调用底层的 database/sql 驱动，结合具体方言（Dialector）发送给数据库。
//
// ⚡ 【性能实战·生产调优】
// 本项目默认使用 SQLite，需注意单连接限制与写入串行化：
// SQLite 是文件级锁（WAL 模式下虽支持读写并发，但写写依然互斥）。如果高频触发
// Create/Update，底层只有一个连接能真正写入。高并发场景下必须控制数据库写入
// 频次（如引入异步批处理或者切换至 MySQL）。
//
// 🛡️ 【安全攻防·漏洞防线】
// GORM 所有的带有 `?` 的传参，底层一律走 `stmt.Exec(args...)`（Prepare Statement 预编译）。
// 这从根本上杜绝了 SQL 注入漏洞。但如果误用拼接字符串（如 Where("id=" + reqId)），则会导致破防。
//
// @Ref: docs/sps/plans/20260612_arch_hardening_plan.md | @Date: 2026-06-12
// ============================================================
package scheduler

import (
	"cronix/internal/domain/model"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// GormLogRepository 使用 GORM 实现 LogRepository 接口
// 结构体里只存了一个东西：db（数据库连接）。这是他干活的唯一工具。
type GormLogRepository struct {
	db *gorm.DB
}

// NewGormLogRepository 办理入职。把数据库钥匙（db）交给他。
func NewGormLogRepository(db *gorm.DB) *GormLogRepository {
	return &GormLogRepository{db: db}
}

// ---- 单任务执行日志 ----

// CreateExecutionLog 插入一条新的执行日志（发车）
//
// 📌 【大厂面试·核心考点】
// 面试官问：GORM 怎么插入数据？其底层的反射开销如何优化？
// 标准答案：把一个结构体指针传给 Create()。GORM 会通过 Go 的 reflect 机制扫描结构体的 Tag，
// 映射为表名和列名。为了解决反射的性能问题，GORM 内部实现了 Schema 缓存池（sync.Map），
// 同一类型的结构体只会在首次解析时走反射，后续全部命中内存缓存，开销极低。
//
// 🔬 【底层原理·深度剖析】事务隔离级别与插入
// 默认情况下，数据库的事务隔离级别多为 READ COMMITTED（读已提交）或 REPEATABLE READ（可重复读）。
// 当 GORM 执行 Create 时，如果外部没有显式开启事务，它会自动将单条 Insert 包装在一个短事务中
// （GORM 默认的 SkipDefaultTransaction 配置为 false 时），保证插入行为的原子性。
func (r *GormLogRepository) CreateExecutionLog(execLog *model.ExecutionLog) error {
	return r.db.Create(execLog).Error
}

// SaveExecutionLog 更新一条已有的执行日志（到站）
//
// ⚡ 【性能实战·生产调优】Save vs Updates
// 错误做法：每次改一个状态，都把整个结构体拉出来用 `Save()` 保存。
// `Save` 对应的是 `UPDATE table SET a=?, b=?, c=? WHERE id=?`，会盲目覆盖所有字段，
// 甚至包括未修改的零值。在高并发下可能覆盖掉其他线程刚刚更新的数据（Lost Update 丢失更新）。
//
// 正确做法：如果在真实的千万级并发场景，更推荐使用 `Updates(map[string]interface{}{"status": "success"})`。
// 这不仅减少了网络包传输大小，还在 MySQL 层级减少了 binlog 的产生量，降低主从同步延迟。
// 不过对于我们这个轻量级单机 Cronix 系统，Save 的语义更简单直接。
func (r *GormLogRepository) SaveExecutionLog(execLog *model.ExecutionLog) error {
	return r.db.Save(execLog).Error
}

// CountRunningLogs 统计指定任务当前处于 running 且未结束的日志条数
// 防重击穿：去数据库数一数，这个任务当前有几条 "running" 的记录。
//
// 💀 【踩坑血泪·反面教材】
// 如果表里有 1000 万条日志数据，且未在 (task_id, status) 上建立复合索引。
// 这句简单的 Count 查询将引发全表扫描（Full Table Scan），在高峰期直接把数据库 CPU 打满。
// 解决办法：在模型层定义 `gorm:"index:idx_task_status"`。
//
// 🔬 【底层原理·深度剖析】幻读与隔离级别
// 当使用 COUNT(*) 进行高并发任务调度防重判定时，如果当前事务隔离级别是 READ UNCOMMITTED（读未提交），
// 则可能读到别的事务刚写一半还没提交的数据（脏读）。
// 如果是 REPEATABLE READ（可重复读），虽然解决了脏读，但并发场景下依然会有【幻读】风险：
// 比如你查到 count=0 刚准备发车，另一个线程也查到 0 发了车，导致同一个任务启动两次。
// 真正的严谨调度（如 Quartz）会依赖 FOR UPDATE 排他锁或乐观锁进行状态抢占。
func (r *GormLogRepository) CountRunningLogs(taskID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.ExecutionLog{}).
		Where("task_id = ? AND status = ? AND end_time IS NULL", taskID, model.StateRunning).
		Count(&count).Error
	return count, err
}

// GetLatestTaskLog 获取指定任务的最新一条执行日志（按 ID 降序）
//
// 📌 【大厂面试·核心考点】
// 为什么这里用 `Order("id DESC")` 而不用 `Order("created_at DESC")`？
// 标准答案：因为在自增主键的表中，ID 的单调递增性与写入时间是严格保持一致的。
// 对整型自增主键（id）排序走的是聚簇索引（B+树最底层直接相连），速度极快；
// 如果对时间字段排序，除非显式建立时间索引，否则会引发极其昂贵的 filesort（文件排序）。
func (r *GormLogRepository) GetLatestTaskLog(taskID uint) (*model.ExecutionLog, error) {
	var execLog model.ExecutionLog
	err := r.db.Where("task_id = ?", taskID).Order("id DESC").First(&execLog).Error
	if err != nil {
		return nil, err
	}
	return &execLog, nil
}

// CleanupOrphanedLogs 清理所有处于 running 状态但无结束时间的孤儿日志
// 停电恢复后，把所有还以为自己在 running 的假象打破，统统设为 failed。
//
// 🏗️ 【架构设计·模式对比】幂等性设计
// 这个清理动作是绝对【幂等（Idempotent）】的。意思是，不管你执行 1 次还是 10000 次，
// 对系统最终状态的影响都是一样的。它只寻找 `status = running AND end_time IS NULL` 的僵尸数据。
//
// ⚡ 【性能实战·生产调优】行锁爆炸
// 这个 Updates 在执行时：`UPDATE logs SET status='failed'... WHERE status='running'`。
// 在 MySQL InnoDB 中，如果 WHERE 子句的条件没有命中索引，UPDATE 操作会退化为表级锁（Table Lock）
// 或触发行锁（Row Lock）扩散，将全表锁死！这叫做“更新无索引字段导致的惨案”。
// 必须确保 status 字段是轻量级状态机，且表规模可控，否则定期清理会造成数据库瞬间不可用。
func (r *GormLogRepository) CleanupOrphanedLogs(now time.Time) error {
	result := r.db.Model(&model.ExecutionLog{}).
		Where("status = ? AND end_time IS NULL", model.StateRunning).
		Updates(map[string]interface{}{ // 批量更新这三个字段
			"status":    model.StateFailed,
			"error_msg": "System restarted or crashed",
			"end_time":  now,
		})
	if result.Error != nil {
		return result.Error
	}
	// 如果真的扫出来垃圾了，打个日志通知一下
	if result.RowsAffected > 0 {
		log.Warn().Int64("count", result.RowsAffected).Msg("已清理孤儿 running 日志")
	}
	return nil
}

// DeleteLogsBefore 删除创建时间早于 cutoff 的执行日志
//
// 💀 【踩坑血泪·反面教材】批量删除的性能陷阱
// 这种基于时间的历史数据大面积删除：`DELETE FROM logs WHERE created_at < ?`。
// 如果符合条件的数据高达几百万条，直接执行这条语句会导致：
// 1. 事务日志（Undo/Redo Log 或 WAL）暴涨，磁盘 I/O 瞬间拉满；
// 2. 长时间持有大量锁，阻断其他正常读写；
// 3. MySQL 甚至会因为事务过大直接抛出 "The total number of locks exceeds the lock table size" 异常。
// 生产环境的正规做法：按时间切片（例如每次删一天），外加 LIMIT 分批次循环删除。
func (r *GormLogRepository) DeleteLogsBefore(cutoff time.Time) (int64, error) {
	result := r.db.Where("created_at < ?", cutoff).Delete(&model.ExecutionLog{})
	return result.RowsAffected, result.Error
}

// DeleteExcessLogs 当总日志数超过 maxRecords 时，删除最旧的记录
//
// 📌 【大厂面试·核心考点】
// 问：为什么不直接写一条 `DELETE FROM logs ORDER BY id ASC LIMIT N`？
// 答：因为带 LIMIT 和 ORDER BY 的 DELETE 语句在 MySQL 基于 Statement 的主从复制模式（SBR）下
// 被标记为【非确定性语句（Unsafe Statement）】。主从库如果在锁机制上有细微差异，可能删掉的不是同一批行，
// 从而导致主从数据不一致。分成两步（先查 ID 列表，再用 ID 删）是最安全的架构级最佳实践。
//
// ⚡ 【性能实战·生产调优】WHERE id IN (百万人) 的崩溃
// 虽然我们分了两步走，但第二步 `WHERE id IN (?)` 也存在致命陷阱：
// 1. 网络包大小限制：MySQL 的 `max_allowed_packet` 默认可能只有 4MB，ID 太多拼装出的 SQL 会超长报错。
// 2. 数据库占位符限制：SQLite 的预编译占位符 `?` 数量存在硬性上限（老版本999个，新版本32766个）。
// 3. 内存撑爆：取出百万 ID 到 Go 切片中，不仅引发大量 GC 扫描，还会 OOM。
// 如果超出配置，这部分代码必须改为 Chunk 分批机制（例如每 1000 个 ID 执行一次 Delete）。
func (r *GormLogRepository) DeleteExcessLogs(maxRecords int) error {
	var count int64
	r.db.Model(&model.ExecutionLog{}).Count(&count) // 先数数总共多少条
	if count <= int64(maxRecords) {
		return nil // 没超标，不用删
	}
	excess := count - int64(maxRecords) // 超标了多少条（要删几个）
	var ids []uint
	
	// 第一步：抓犯人（Pluck 就是把查出来的某一列，单独抽出来变成一个切片/数组）
	err := r.db.Model(&model.ExecutionLog{}).
		Select("id").
		Order("id ASC").        // 从旧到新排
		Limit(int(excess)).     // 只抓多出来的那些
		Pluck("id", &ids).Error // 抽出 ID 存进 ids 切片
	if err != nil {
		return err
	}
	
	// 第二步：执行枪决
	if len(ids) > 0 {
		return r.db.Where("id IN (?)", ids).Delete(&model.ExecutionLog{}).Error
	}
	return nil
}

// DeleteExcessTaskLogs 清理单个任务的超额日志
// 逻辑和上面一模一样，只是加了一个 Where 条件（只管这个特定任务的垃圾）
func (r *GormLogRepository) DeleteExcessTaskLogs(taskID uint, maxLogs int) error {
	var count int64
	r.db.Model(&model.ExecutionLog{}).Where("task_id = ?", taskID).Count(&count)
	if count <= int64(maxLogs) {
		return nil
	}
	excess := count - int64(maxLogs)
	var ids []uint
	err := r.db.Model(&model.ExecutionLog{}).
		Select("id").
		Where("task_id = ?", taskID).
		Order("id ASC").
		Limit(int(excess)).
		Pluck("id", &ids).Error
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		result := r.db.Where("id IN (?)", ids).Delete(&model.ExecutionLog{})
		if result.Error != nil {
			return result.Error
		}
		log.Debug().Int64("deleted", result.RowsAffected).Uint("task_id", taskID).Msg("limitTaskLogs pruned excess logs")
	}
	return nil
}

// ---- 组执行日志 ----
// (以下逻辑与单任务完全一致，只是操作的表变成了 group_execution_logs)
// 
// 🧪 【测试工程·质量保障】
// 针对同构多态表（相似逻辑针对不同表操作），在测试层通常推荐使用【数据驱动测试（Table-Driven Tests）】。
// 无需为 SingleLog 和 GroupLog 写两套近乎相同的测试用例。只需将 Repository 作为接口参数注入到测试方法中，
// 构建不同的 Mock 对象进行参数化验证，即可保证逻辑的覆盖率，并大幅降低测试代码维护成本。

// CreateGroupLog 插入一条新的组执行日志
func (r *GormLogRepository) CreateGroupLog(glog *model.GroupExecutionLog) error {
	return r.db.Create(glog).Error
}

// SaveGroupLog 更新一条已有的组执行日志
func (r *GormLogRepository) SaveGroupLog(glog *model.GroupExecutionLog) error {
	return r.db.Save(glog).Error
}

// DeleteGroupLogsBefore 删除创建时间早于 cutoff 的组执行日志
func (r *GormLogRepository) DeleteGroupLogsBefore(cutoff time.Time) (int64, error) {
	result := r.db.Where("created_at < ?", cutoff).Delete(&model.GroupExecutionLog{})
	return result.RowsAffected, result.Error
}

// DeleteExcessGroupLogs 当组日志总数超过 maxRecords 时，删除最旧的记录
func (r *GormLogRepository) DeleteExcessGroupLogs(maxRecords int) error {
	var count int64
	r.db.Model(&model.GroupExecutionLog{}).Count(&count)
	if count <= int64(maxRecords) {
		return nil
	}
	excess := count - int64(maxRecords)
	var ids []uint
	err := r.db.Model(&model.GroupExecutionLog{}).
		Select("id").
		Order("id ASC").
		Limit(int(excess)).
		Pluck("id", &ids).Error
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return r.db.Where("id IN (?)", ids).Delete(&model.GroupExecutionLog{}).Error
	}
	return nil
}
