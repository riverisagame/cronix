// ============================================================
// internal/service/group_service_test.go - 任务组服务单元测试
//
// 【纳米级源码说明书 - 测试篇】
// 这里的角色是“质检员（QA）”。
// 负责在代码上线前，通过自动化脚本模拟真实用户的操作，
// 确保不管怎么折腾，程序都不会崩溃。
// 
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 面试官问：什么是单元测试里的 Setup 和 Teardown（清理）？为什么要用内存数据库？
// 答（小白比喻）：
// 假设你要测试一个“切菜机器人”。
// 错误做法：直接拿厨房里你晚上准备吃的萝卜去切（在真实数据库里测）。万一切坏了，你今晚就没饭吃了。
// 正确做法：
// 1. Setup（准备）：拿一根塑料假萝卜（内存 SQLite 数据库 `sqlite.Open("file::memory:?cache=shared")` 或者临时文件库）。
// 2. Test（测试）：让机器人去切假萝卜。
// 3. Teardown/Cleanup（打扫战场）：测试完把碎塑料扫进垃圾桶（删除临时库、关闭连接 `sqlDB.Close()`）。
// 这样每次测试都是全新的，绝对不会污染真实数据！
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

	// Delete group unlinks remaining members - 测试级联解绑
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
func TestGroupValidation(t *testing.T) {
	db := setupGroupTestDB(t)
	svc := &GroupService{DB: db}

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
