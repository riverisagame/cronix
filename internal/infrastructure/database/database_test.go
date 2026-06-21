// ============================================================
// internal/database/database_test.go - database init tests
// ============================================================
/*
📌 【大厂面试·核心考点】 数据库高阶质量保障体系
面试官会问：在生产级 Go 项目中，如何通过测试保障数据库层面的健壮性？具体如何防范连接池泄漏、验证读写分离和保证事务一致性？
标准答案：
1. 绝对物理零污染：绝不直连任何带有业务数据的生产/测试库。利用 `t.TempDir()` 生成隔离的 SQLite 文件，或通过依赖注入和 `DATA-DOG/go-sqlmock` 实现纯内存无副作用的测试。如果已存在物理表，测试绝对禁止执行 DROP 或 TRUNCATE，所有操作仅限于自行创建的沙盒（Mock 数据）。
2. 数据库连接池泄漏检测：高并发下最致命的往往是连接池被打满。在压测或测试结束后，通过调用 `db.DB().Stats().InUse` 断言活动连接数是否归零，以此来检查所有 `sql.Rows` 或事务对象是否被正确 `Close()/Rollback()`。
3. 读写分离路由测试：微服务高可用架构下，通过 GORM `dbresolver` 配置主从。使用 `sqlmock` 注入两套底层 `*sql.DB` 实例，断言 `SELECT` 请求被正确分配到从库的 Mock 实例，而 `INSERT/UPDATE` 必定触发主库 Mock 的时序。
4. 事务回滚一致性：利用 `ExpectBegin()`、`ExpectExec()` 模拟并返回错误，随后严格断言 `ExpectRollback()` 的执行时序，确保即使业务代码发生 `panic` 也能完美保持事务的原子性。

🧪 【测试工程·质量保障】 SqlMock 高级测试参考实现（伪代码示例）
为了应对不能直接连接物理库的场景，或者严格校验底层执行时序，通常会使用如下 Mock 测试方法保证 100% 安全与逻辑精准：
```go
func TestTransactionRollbackWithMock(t *testing.T) {
    sqlDB, mock, err := sqlmock.New()
    // 断言测试完毕后没有多余预期外调用，有效拦截连接泄漏
    defer func() { assert.Equal(t, 0, sqlDB.Stats().InUse); sqlDB.Close() }()
    
    gormDB, _ := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
    
    // 预期开启事务
    mock.ExpectBegin()
    // 预期写入任务失败
    mock.ExpectExec("INSERT INTO `tasks`").WillReturnError(fmt.Errorf("mock constraint error"))
    // 核心考点：如果业务逻辑写得好，一定会触发 Rollback 以保证一致性
    mock.ExpectRollback()
    
    // 触发被测的业务操作
    err = CreateTaskWithDependencies(gormDB, task)
    assert.Error(t, err)
    assert.NoError(t, mock.ExpectationsWereMet())
}
```
*/
package database

import (
    "path/filepath"
    "testing"
    "cronix/internal/domain/model"
)

/*
🧪 【测试工程·质量保障】 零污染沙盒初始化
通过 `t.TempDir()`，测试过程与真实的数据库文件做到了 100% 的物理隔离。这里创建了一个基于操作系统的临时路径，每一次运行均会获得全新空荡荡的目录。测试运行结束后，垃圾回收或系统自动回收临时目录，做到“事后拂衣去，毫发无损”。这完全贯彻了【零物理污染】的要求，绝不会干扰任何现有数据或表结构。
*/
// TestInit verifies database initialization and table creation
func TestInit(t *testing.T) {
    tmpDir := t.TempDir()
    dbPath := filepath.Join(tmpDir, "test.db")

    /*
    🔬 【底层原理·深度剖析】 GORM 的 AutoMigrate 原理
    当我们调用 `Init(dbPath)` 时，底层最终使用了 GORM 的 `AutoMigrate`。该机制基于反射（Reflection）遍历 Struct 字段，并在数据库系统表中查询表和列是否存在。
    如果不存在，则动态拼接 `CREATE TABLE`。如果在测试环境中直接连接开发库进行 Migrate，极易修改被别人正在使用的字段结构，甚至发生覆盖。因此，这里的沙盒机制是测试架构的第一道铁血防线。
    */
    err := Init(dbPath)
    if err != nil {
        t.Fatalf("Init failed: %v", err)
    }
    
    // Close DB so TempDir cleanup works on Windows
    /*
    ⚡ 【性能实战·生产调优】 连接生命周期与句柄管理
    为什么必须 defer Close？打个比方，去图书馆借书（获取文件句柄 / 数据库连接），如果不主动登记归还（Close），图书管理员（操作系统）会认为书还在你手里，别人（清理进程）就无法操作该书。
    在 Windows OS 层面，如果 `*sql.DB` 持有了 `.db` 文件的排他锁，`t.TempDir` 的清理钩子将会因为权限被拒绝（Access Denied）而产生报错。对于长生命周期的服务，不主动释放资源会导致句柄耗尽 (Too many open files)。
    */
    defer Close()

    if DB == nil {
        t.Fatal("DB is nil after Init")
    }

    /*
    🏗️ 【架构设计·模式对比】 显式断言 vs 隐式通过
    在模型验证环节，使用 `HasTable` 进行显式确认比单纯的“无错误返回”更加坚固。
    如果哪一天 `Init` 内部的 `AutoMigrate` 被人不小心去掉了错误处理（Error swallow），这里的强断言测试依然能守住底线，大声报警。
    */
    if !DB.Migrator().HasTable(&model.Task{}) {
        t.Error("tasks table not created")
    }
    if !DB.Migrator().HasTable(&model.TaskDep{}) {
        t.Error("task_deps table not created")
    }
    if !DB.Migrator().HasTable(&model.ExecutionLog{}) {
        t.Error("execution_logs table not created")
    }
    if !DB.Migrator().HasTable(&model.NotifyConfig{}) {
        t.Error("notify_configs table not created")
    }
    t.Logf("DB init ok: %s", dbPath)
}

/*
🛡️ 【安全攻防·漏洞防线】 CRUD 测试安全隔离
安全防御策略：测试任何数据操作时，绝不应该用真实的系统数据，必须手工拼装纯粹的 Mock（模拟）实体。如果在测试中硬编码或读取真实的 PII 数据（个人身份信息），会有极高的脱敏失败和数据泄露风险。
*/
// TestTaskCRUD verifies basic CRUD operations on tasks
func TestTaskCRUD(t *testing.T) {
    tmpDir := t.TempDir()
    dbPath := filepath.Join(tmpDir, "test.db")

    if err := Init(dbPath); err != nil {
        t.Fatalf("Init failed: %v", err)
    }
    defer Close()

    /*
    📌 【大厂面试·核心考点】 数据库连接池泄漏检测与锁死防范
    在这个看似简单的 `Create` 操作背后，GORM 会从底层 `database/sql` 申请一个连接。
    如果在高并发真实系统中处理复杂的流式查询，而没有把 `Rows.Close()` 写在 `defer` 中，或者发生 panic 导致逻辑跳过，连接将被永远锁死。
    我们在进行严苛的黑盒测试时，可以通过 `sqlDb, _ := DB.DB(); active := sqlDb.Stats().InUse` 对操作后的连接数进行拦截断言。只有 `InUse` 回归为 0，才说明代码毫无泄漏死角。
    */
    // Create
    task := model.Task{
        Name:     "test-backup",
        CronExpr: "0 0 2 * * *",
        TaskType: "shell",
        Command:  "echo hello",
        Enabled:  true,
    }
    if err := DB.Create(&task).Error; err != nil {
        t.Fatalf("Create failed: %v", err)
    }
    if task.ID == 0 {
        t.Error("Task ID should be > 0 after create")
    }
    t.Logf("Created task ID=%d", task.ID)

    // Read
    /*
    ⚡ 【性能实战·生产调优】 读写分离路由测试推演
    当流量达到瓶颈，系统架构势必演进为一主多从（Master-Slave）。GORM 通过 `gorm.io/plugin/dbresolver` 插件实现读写分离。
    虽然本例中连接的是单体 SQLite DB，但在真实的微服务单元测试环境，若要严格检验这套架构逻辑，我们需要结合 `sqlmock` 设置多个 DSN（主和从）。
    在此进行 `First`（读）操作时，严格的测试用例会预埋 `mockSlave.ExpectQuery(...)` 断言，确保这条查询被准确无误地路由到了【从库】连接之上，而不给主库增加一丝一毫的额外负担。
    */
    var found model.Task
    if err := DB.First(&found, task.ID).Error; err != nil {
        t.Fatalf("Read failed: %v", err)
    }
    if found.Name != "test-backup" {
        t.Errorf("Name mismatch: want test-backup, got %s", found.Name)
    }
    t.Logf("Read ok: %s", found.Name)

    // Update
    /*
    🔬 【底层原理·深度剖析】 事务回滚一致性保证（ACID - Atomicity）
    如果更新不仅涉及 `task` 自身，还需要联动更新 `TaskDep` 或者 `ExecutionLog`，这就构成了强一致性要求。
    在关系型数据库底层，这种多表变更必须用 `BEGIN; ...; COMMIT/ROLLBACK` 裹住。
    如果在事务中途发生网络中断、业务报错或服务 OOM（如 Pod 被 K8s 强杀），未提交的更改会留在 Undo Log 内（MVCC机制下），等服务重启或连接断开后发生自动回滚（Rollback）。
    完善的逻辑不仅依赖 DB 的被动保障，更需要在 Go 层通过 `defer tx.Rollback()` 兜底。只有这样，无论何种极端错误，系统都不会产生部分更新的“脏数据”。
    */
    newName := "updated-task"
    if err := DB.Model(&task).Update("name", newName).Error; err != nil {
        t.Fatalf("Update failed: %v", err)
    }
    DB.First(&found, task.ID)
    if found.Name != newName {
        t.Errorf("Update mismatch: want %s, got %s", newName, found.Name)
    }
    t.Logf("Update ok: %s", found.Name)

    // Delete
    /*
    💀 【踩坑血泪·反面教材】 谨慎看待物理删除
    在目前的结构下，`Delete` 操作会将数据行从表空间彻底抹除（除非该 Model 结构体引入了软删除字段，如 `gorm.DeletedAt`）。
    真实生产事故：某金融平台因开发人员在测试后疏忽，加上线上管理后台的越权漏洞，导致物理删除了关键的清算流水，酿成了数百万资金无法对账的灾难。
    最佳实践：业务表尽可能使用“逻辑删除”。对高价值数据资产，应当永远遵守“只读和追加”（Append & Read-Only）范式，或利用状态机（status标识），坚决杜绝让数据物理“消失”。
    */
    if err := DB.Delete(&task).Error; err != nil {
        t.Fatalf("Delete failed: %v", err)
    }
    err := DB.First(&found, task.ID).Error
    if err == nil {
        t.Error("Record still exists after delete")
    }
    t.Log("Delete ok")
}
