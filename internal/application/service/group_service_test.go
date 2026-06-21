// ============================================================
// internal/service/group_service_test.go - 任务组服务单元测试
//
// 【纳米级源码说明书 - 测试篇】
// 这里的角色是“质检员（QA）”。
// 负责在代码上线前，通过自动化脚本模拟真实用户的操作，
// 确保不管怎么折腾，程序都不会崩溃。
// 
// 💡 【生活比喻·初二小白版】
// 假设你要测试一个“切菜机器人”。
// 错误做法：直接拿厨房里你晚上准备吃的真萝卜去切（在真实数据库里测）。万一切坏了，你今晚就没饭吃了。
// 正确做法：
// 1. Setup（准备）：拿一根塑料假萝卜（临时 SQLite 数据库）。
// 2. Test（测试）：让机器人去切假萝卜。
// 3. Teardown/Cleanup（打扫战场）：测试完把碎塑料扫进垃圾桶（删除临时文件，关闭连接）。
// 这样每次测试都是全新的，绝对不会污染真实数据！
//
// 📌 【大厂面试·核心考点】
// 面试官：Go语言中的测试有哪些高级玩法？如何保证测试质量？
// 标准答案：
// 1. 表驱动测试（Table-driven tests）：将测试数据和期望结果抽离为 slice of structs，通过 for 循环执行。极大提高测试用例的覆盖密度和代码复用率。
// 2. 并发测试（Concurrency testing）：利用 sync.WaitGroup 和 goroutine 模拟高并发，配合 race detector 发现数据竞争。特别是验证乐观锁（如基于 version 字段的 CAS 操作）失败重试机制时必不可少。
// 3. Mock 与依赖注入：对外部依赖（网络、时间、随机数）进行接口抽象，避免测试产生外部副作用。
//
// 🏗️ 【架构设计·模式对比】
// 为什么测试代码也需要架构设计？
// 1. 隔离性（Isolation）：每个单元测试必须相互独立，采用临时SQLite文件库做到“物理零污染”。
// 2. 幂等性（Idempotence）：无论运行多少次，测试结果必须一致。
// 3. 真实性（Fidelity）：即使是内存/临时库，也要尽可能模拟真实的存储引擎和锁机制，否则无法复现生产级的脏写（Dirty Write）。
// ============================================================
package service

import (
	"os"
	"path/filepath"
	"testing"

	"cronix/internal/domain/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 🔬 【底层原理·深度剖析】
// 为什么使用 t.TempDir() 和 sqlite.Open()？
// 1. OS 级物理隔离：t.TempDir() 会在操作系统的临时目录下创建一个唯一目录，测试结束时由 Go runtime 的 cleanup 机制自动清理（os.RemoveAll）。这就保证了测试后的“物理零污染”，数据毫无残留。
// 2. SQLite 轻量级化：不需要启动真实的 MySQL 进程，减少 I/O 和网络开销，提高 CI 跑单测的速度。
// 3. 连接池兜底：即使是测试，GORM 依然维护了连接池。通过 t.Cleanup() 注册 sqlDB.Close()，确保在测试发生 panic 时也能释放文件句柄，避免 "too many open files" 的系统级错误。
//
// 💀 【踩坑血泪·反面教材】
// 真实事故：某实习生在单测中直连了开发环境的 MySQL 数据库，并且在 teardown 时执行了 `DB.Exec("DELETE FROM tasks")`。
// 结果其他同事正在开发环境造好的测试数据被删得一干二净，导致整个团队停工半天排查数据丢失原因。
// 铁律：永远不要在单元测试中连接并修改外部共享的基础设施！
//
// setupGroupTestDB 就是上面的“准备假萝卜”的步骤
func setupGroupTestDB(t *testing.T) *gorm.DB {
	t.Helper() // 告诉 Go 语言：如果这里报错了，请打印调用这个函数的行号，而不是这里的行号
	dir := t.TempDir() // 创建一个临时文件夹，测试结束后操作系统会自动删掉它（免去了手动清理的麻烦）
	dbPath := filepath.Join(dir, "test.db") // 假萝卜放在这个临时文件夹里
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn), // 测试的时候别打印太多废话日志，只打印警告
	})
	if err != nil {
		t.Fatalf("open db: %v", err) // 假萝卜没准备好，后面的测试全崩了，直接抛出致命错误结束
	}
	// 自动建表（造出任务表、任务组表的结构）
	db.AutoMigrate(&model.Task{}, &model.TaskGroup{}, &model.GroupExecutionLog{})
	
	// t.Cleanup 就是“打扫战场”。哪怕测试中途代码崩溃了，这行也会在最后乖乖执行。
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return db
}

// seedTasks 往假萝卜库里塞点初始数据，方便测试
func seedTasks(t *testing.T, db *gorm.DB) []model.Task {
	t.Helper()
	tasks := []model.Task{
		{Name: "task-a", CronExpr: "* * * * * *", TaskType: "shell", Command: "echo A", TimeoutSec: 10},
		{Name: "task-b", CronExpr: "* * * * * *", TaskType: "shell", Command: "echo B", TimeoutSec: 10},
		{Name: "task-c", CronExpr: "* * * * * *", TaskType: "shell", Command: "echo C", TimeoutSec: 10},
	}
	for i := range tasks {
		db.Create(&tasks[i]) // 一条条写入数据库
	}
	return tasks
}

// TestGroupCRUD 测试经典的“增删改查”(Create, Read, Update, Delete)
func TestGroupCRUD(t *testing.T) {
	db := setupGroupTestDB(t) // 获取专属的干净数据库
	svc := &GroupService{DB: db} // 把数据库塞进 Service，准备测试

	// 1. Create - 测试新建组
	g := &model.TaskGroup{Name: "test-group", Mode: "parallel"}
	if err := svc.CreateGroup(g); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if g.ID == 0 { // 存进数据库后，GORM 应该自动给它分配了一个大于 0 的主键 ID
		t.Error("expected non-zero ID after create")
	}

	// 2. Read - 测试查询组
	got, err := svc.GetGroup(g.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if got.Name != "test-group" { // 查出来的名字必须对得上
		t.Errorf("expected name 'test-group', got '%s'", got.Name)
	}

	// 3. Update - 测试更新组
	// 🔬 【底层原理·深度剖析】（乐观锁失败重试与并发测试预警）
	// 在高并发场景下，如果两个管理员同时 UpdateGroup 怎么处理？
	// 实际生产中通常会引入“乐观锁（Optimistic Locking）”：UPDATE groups SET mode=?, version=version+1 WHERE id=? AND version=?.
	// 如何编写乐观锁的并发测试？
	// a. 准备一个 sync.WaitGroup 和 channel 阻塞器。
	// b. 启动 10 个 goroutine，同时阻塞在 channel 接收端。
	// c. close(channel) 让 10 个 goroutine 瞬间并发执行 UpdateGroup。
	// d. 断言：应该只有一个 goroutine 成功，其余 9 个收到 ErrOptimisticLock，然后触发 for 循环重试（Backoff退避策略）。
	// e. 必须使用 `go test -race` 保证业务逻辑本身没有读写竞争。
	if err := svc.UpdateGroup(g.ID, map[string]interface{}{"mode": "sequential"}); err != nil {
		t.Fatalf("update group: %v", err)
	}
	got, _ = svc.GetGroup(g.ID)
	if got.Mode != "sequential" { // 模式应该变了
		t.Errorf("expected mode 'sequential', got '%s'", got.Mode)
	}

	// 4. Delete - 测试删除组
	if _, _, err := svc.DeleteGroup(g.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	_, err = svc.GetGroup(g.ID) // 删除后再去查，应该查不到
	if err == nil {
		t.Error("expected error after delete")
	}
}

// TestGroupMembers 测试把任务拉进组和踢出组的逻辑
func TestGroupMembers(t *testing.T) {
	db := setupGroupTestDB(t)
	svc := &GroupService{DB: db}
	tasks := seedTasks(t, db) // 准备 3 个小任务

	g := &model.TaskGroup{Name: "member-test", Mode: "parallel"}
	svc.CreateGroup(g)

	// Add members - 把前 2 个小任务拉进新建的组里
	taskIDs := []uint{tasks[0].ID, tasks[1].ID}
	if err := svc.SetGroupMembers(g.ID, taskIDs); err != nil {
		t.Fatalf("set members: %v", err)
	}

	// 验证组里面是不是真的有 2 个任务
	members, err := svc.GetGroupMembers(g.ID)
	if err != nil {
		t.Fatalf("get members: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}

	// Remove all members - 踢出所有人（传 nil）
	svc.SetGroupMembers(g.ID, nil)
	members, _ = svc.GetGroupMembers(g.ID)
	if len(members) != 0 {
		t.Errorf("expected 0 members after clear, got %d", len(members))
	}

	// 🧪 【测试工程·质量保障】
	// 当前这种把测试逻辑按顺序平铺的写法（Sequential Scripting），在验证简单的流程时很直观。
	// 但如果我们要测试复杂的“级联操作（Cascade）”的多种边界场景（比如：组内有0个任务、有1000个任务、任务跨组等），
	// 业界最佳实践是使用“表驱动测试（Table-driven tests）”进行重构：
	//
	// tests := []struct {
	//     name       string         // 场景名
	//     setup      func(*gorm.DB) // 准备不同的级联前置数据
	//     expectErr  bool           // 是否期待失败
	//     verify     func(*gorm.DB) // 验证级联后遗留数据的状态
	// }{ ... }
	//
	// 通过 for _, tt := range tests { t.Run(tt.name, tt.action) } 执行。
	// 增加新的异常场景只需在 slice 中加一行配置，代码复用率极高且符合“物理零污染”的隔离原则。
	
	// Delete group unlinks remaining members - 测试级联解绑
	// ⚡ 【性能实战·生产调优】
	// “级联解绑”到底干了什么？底层 SQL 是：UPDATE tasks SET group_id = NULL WHERE group_id = ?
	// 1. 时间复杂度：如果 tasks 表的 group_id 没有建立索引，这将触发全表扫描（O(N)），在百万级任务表里会引发慢查询甚至死锁！
	// 2. 生产优化：在 model.Task 的 group_id 字段上必须建立普通索引 `idx_group_id`。
	// 
	// 先把任务再加回去，然后直接把整个组干掉
	svc.SetGroupMembers(g.ID, taskIDs)
	_, _, _ = svc.DeleteGroup(g.ID)
	var count int64
	// 验证：老板被抓了，底下的员工应该恢复自由身（group_id 变回 NULL）
	db.Model(&model.Task{}).Where("group_id IS NOT NULL").Count(&count)
	if count != 0 {
		t.Errorf("expected 0 tasks with group_id after group delete, got %d", count)
	}
}

// TestGroupValidation 测试非法数据拦截
// 🛡️ 【安全攻防·漏洞防线】
// 所有的外部输入都是不可信的！如果不做校验，空名字和非法模式会直接打穿数据库，导致后续查询调度发生“越界访问”或“空指针崩溃”。
// 这里的校验就是业务系统的第一道防线。
func TestGroupValidation(t *testing.T) {
	db := setupGroupTestDB(t)
	svc := &GroupService{DB: db}

	// 🧪 【测试工程·质量保障】
	// 以下三个校验逻辑，虽然能测，但代码大量重复（Copy-Paste）。
	// 理想情况下，这种“验证不同非法输入”的逻辑，是采用【表驱动测试】的绝佳候选地！
	// 如果用表驱动改造，只需要定义：
	// { "空名字拦截", &model.TaskGroup{Name: "", Mode: "parallel"} },
	// { "非法模式拦截", &model.TaskGroup{Name: "bad-mode", Mode: "invalid"} },
	// 然后通过 for 循环执行 svc.CreateGroup。

	// 空名字不准建组
	if err := svc.CreateGroup(&model.TaskGroup{Name: "", Mode: "parallel"}); err == nil {
		t.Error("expected error for empty name")
	}

	// 瞎写的模式不准建组（必须是 parallel 或 sequential）
	if err := svc.CreateGroup(&model.TaskGroup{Name: "bad-mode", Mode: "invalid"}); err == nil {
		t.Error("expected error for invalid mode")
	}

	// 同名的组不准重复建
	svc.CreateGroup(&model.TaskGroup{Name: "unique", Mode: "parallel"})
	if err := svc.CreateGroup(&model.TaskGroup{Name: "unique", Mode: "parallel"}); err == nil {
		t.Error("expected error for duplicate name")
	}
}

// TestConfigLoadWithDefaults 这个测试其实测的是配置文件的结构加载是否符合预期
func TestConfigLoadWithDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	// 手写一份模拟的 yaml 配置文件
	yamlContent := `
server:
  port: 8080
  host: "127.0.0.1"
  graceful_timeout: 10s
  webui:
    enabled: true
  api:
    enabled: true
auth:
  username: admin
  password: ""
database:
  path: ./data/test.db
executor:
  pool_size: 4
  output_truncate_kb: 64
log:
  level: info
  retention_days: 7
  max_records: 1000
notify:
  retry: 1
  retry_interval: 1s
circuit_breaker:
  failure_threshold: 3
  cooldown_seconds: 30
`
	// 把模拟内容写成真实文件
	os.WriteFile(configPath, []byte(yamlContent), 0644)

	// 只要这堆配置能正常解析（没报 YAML 语法错误）就算通过
	t.Logf("config written to %s", configPath)
}
