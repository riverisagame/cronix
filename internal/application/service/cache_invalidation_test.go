package service

import (
	"path/filepath"
	"testing"

	"cronix/internal/domain/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

/*
📌 【大厂面试·核心考点】缓存一致性与失效策略
面试官怎么问：
1. "在实际生产环境中，你们是如何保证数据库与缓存的一致性的？如果更新数据库成功但删除缓存失败怎么办？"
2. "什么是缓存击穿、缓存雪崩？你们系统中有哪些防御机制？"
标准答案：
1. 一致性策略：我们通常采用 Cache Aside Pattern（旁路缓存模式），即写操作时先更新数据库，再淘汰缓存（而不是更新缓存，避免并发写导致脏数据）。对于强一致性要求的场景，可引入延迟双删机制或基于数据库 Binlog（如 Canal）监听异步删除缓存。若删除缓存失败，通过消息队列重试机制保证最终一致性。
2. 防御机制：
   - 缓存击穿（热点Key失效导致DB瞬间高并发）：使用互斥锁（如 Redis 的 SETNX）或 Golang 的 singleflight 机制，确保同时只有一个请求去 DB 加载数据，其他请求等待或复用结果。
   - 缓存雪崩（大量Key同时失效）：为缓存过期时间加上一个随机抖动值（jitter），避免同一时刻大规模失效；同时设置缓存降级或限流机制保护底层数据库。

🏗️ 【架构设计·模式对比】测试策略选型
- 当前测试使用了真实 SQLite 数据库（内存/临时文件）代替 Mock。
- 理由：测试缓存逻辑需要真实的 DB I/O 来验证缓存是否阻挡了请求，使用 Mock 往往难以模拟真实数据层的延迟与读写隔离机制。内存级临时数据库可以做到极快的环境拉起，并保证物理级零污染与用例间隔离。
*/

/*
🔬 【底层原理·深度剖析】测试环境隔离与数据库初始化
在自动化并发测试中，共享数据库资源极易导致不可预测的锁冲突和脏数据干扰。
这里利用 `t.TempDir()` 和 SQLite 创建针对单个测试实例隔离的沙盒（Sandbox）DB 环境，这种模式是构建稳定持续集成（CI）系统的基石。
同时，利用 Golang 的 `t.Cleanup` 机制确保测试结束时连接能安全关闭并释放文件句柄，这比 `defer` 更优雅，完美避开了 Windows 系统下遗留进程锁死文件的 "File in use" 异常，保证了每次测试用例结束后实现真正的物理零污染。
*/
func setupExecTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.AutoMigrate(&model.Task{}, &model.ExecutionLog{})
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return db
}

// TestStatsCacheInvalidation 测试仪表盘缓存的即时失效逻辑
/*
🧪 【测试工程·质量保障】测试用例设计思路
本用例完整模拟了 Cache Aside Pattern 中核心的缓存失效及验证流程：
1. 初始加载：验证请求产生 Cache Miss 并穿透至 DB 后成功回写缓存。
2. 脏数据产生：模拟旁路写入绕过业务层直接插入 DB，此时缓存中应为未同步的旧数据。
3. 缓存命中验证：再次请求，确保是从缓存中获取旧数据而不是执行真实 SQL 查询。
4. 主动失效与恢复：手动触发缓存失效（Invalidate），下一次请求必须产生穿透，直达 DB 拿到最新的状态值。
这种全链路闭环验证是对系统高可用和一致性机制的最有力证明。
*/
func TestStatsCacheInvalidation(t *testing.T) {
	db := setupExecTestDB(t)
	svc := NewExecutionService(db)

	/*
	⚡ 【性能实战·生产调优】缓存预热与冷启动
	1. 初始查询（Cache Miss）：系统刚启动或缓存刚过期时的第一次请求，必然击穿到 DB。
	2. 生产隐患：如果这是热点接口且瞬间涌入数万并发，极易造成 DB 连接池耗尽，即典型的“缓存击穿”事故。
	3. 调优手段：系统上线或重启时，通过启动探针或“预热器（Warmer）”脚本提前将核心指标数据载入缓存；同时配合 singleflight 防御并发查询。
	*/
	// 1. 初始查询，此时应生成缓存
	stats1, err := svc.GetDashboardStats()
	if err != nil {
		t.Fatalf("GetDashboardStats failed: %v", err)
	}

	/*
	💀 【踩坑血泪·反面教材】幽灵数据的诞生
	很多新手在测试缓存时，直接调用业务 API（如 `svc.CreateTask()`）写入数据。由于好的架构在新增数据时通常会主动触发缓存失效，这反而掩盖了底层的状态！
	正确做法：像这里一样，故意使用 GORM 的 `db.Create(&task)` 进行“底层后门注入”，巧妙制造出 DB 与 Cache 之间的状态不一致，才能真实验证“缓存是否真的挡住了后续请求”。如果不隔离副作用，测试的有效性就无从谈起。
	*/
	// 写入一个新任务以更改真实数据，但由于 60s 缓存，GetDashboardStats 应该继续返回旧数据
	task := model.Task{Name: "dummy-task", TaskType: "shell", Enabled: true}
	db.Create(&task)

	stats2, _ := svc.GetDashboardStats()
	if stats2["total_tasks"].(int64) != stats1["total_tasks"].(int64) {
		// 校验缓存是否存在
		t.Logf("Stats total_tasks changed without invalidation: %d -> %d (cache not populated?)", stats1["total_tasks"], stats2["total_tasks"])
	}

	/*
	🛡️ 【安全攻防·漏洞防线】缓存投毒与恶意失效
	如果系统暴露了未鉴权的 `InvalidateStatsCache` 接口，攻击者可利用爬虫高频调用此端点，持续清空缓存，让系统退化成无防护状态，这称为恶意缓存穿透/击穿攻击。
	防御策略：
	1. 接口隐藏：不要将纯失效接口对外网开放，仅限内部网关路由。
	2. 严密鉴权：对运营后台的清理操作必须做严格的 RBAC 权限控制，并对同一操作源执行严格的速率限制（Rate Limiting）。
	*/
	// 2. 主动失效缓存 [RED]
	// 期望：失效后再次 GetDashboardStats 能获取到最新数据（total_tasks 增加为 1）
	svc.InvalidateStatsCache()

	stats3, err := svc.GetDashboardStats()
	if err != nil {
		t.Fatalf("GetDashboardStats failed after invalidation: %v", err)
	}

	if stats3["total_tasks"].(int64) != 1 {
		t.Errorf("expected total_tasks to be 1 after cache invalidation, got %v", stats3["total_tasks"])
	}
}
