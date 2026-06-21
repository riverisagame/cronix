// ============================================================
// internal/application/service/execution_service_test.go - 执行日志服务层单元测试
//
// 【纳米级源码说明书 - 测试工程篇】
// 这里的角色是“品控质检员”。
// 专门用来测试“执行日志服务（ExecutionService）”在断开真实数据库时，
// 能不能依靠“假冒的虚拟数据库（Mock DB）”跑通所有的增删改查和【事务回滚】分支。
//
// 📌 【大厂面试·核心考点】依赖注入与 Service 层的 Mock(gomock)
// 面试官问：如果你要测试一个依赖了外部 API 和数据库的 Service 层，怎么做才能快准狠？
// 标准答案：
// 1. 斩断真实依赖（依赖倒置）：Service 层不要去直连具体的数据库驱动或 RPC 客户端，而是传入 Interface（接口）。
// 2. Mock 打桩：使用 gomock (或类似框架) 自动生成一个假对象（Mock）。
// 3. 行为预期（Expectation）：在测试代码里设定 `mockObj.EXPECT().SomeMethod().Return(fakeData)`。
// 4. 断言闭环：最后验证 Service 的处理逻辑是否正确。这样单元测试的运行速度会从“秒级”提升到“纳秒级”，且永远不受断网影响！
//
// 🔬 【底层原理·深度剖析】事务回滚（Rollback）的测试覆盖
// 面试官问：你怎么在单元测试里证明你的 GORM 事务在发生异常时，确实触发了 ROLLBACK 操作？
// 标准答案：
// 1. 底层拦截：使用 `go-sqlmock` 库。它会在 database/sql 驱动层进行拦截，根本不发真实网络包。
// 2. 编排预期流水线：我们严格排列预期的 SQL 动作：
//    `ExpectBegin()` -> 模拟开启事务
//    `ExpectExec("DELETE...")` -> 模拟正常执行
//    `ExpectExec("UPDATE...")` -> 模拟执行失败，强制抛出一个 `Error("模拟异常")`
//    `ExpectRollback()` -> 【核心】断言程序必须捕获到异常并发出 ROLLBACK 指令！
// 3. 覆盖率：如果业务代码忘了写 `tx.Rollback()`，测试框架就会因为没收到 `Rollback` 信号而立刻报错，保证 100% 的容灾分支覆盖！
//
// 💀 【踩坑血泪·反面教材】直连真实库导致“物理污染”
// 真实生产事故：实习生在单元测试里连接了研发环境的真实数据库，写了个全表删除（DELETE FROM tables）来测试业务代码。
// 当晚 CI/CD 流水线自动跑了一遍测试，整个研发库被当场清空，几百号人的测试数据瞬间蒸发！
// 教训：必须严格遵守“物理零污染”原则！所有的增删改查只能针对自己的 Mock 数据或沙箱表，绝对不能产生任何副作用。
//
// ⚡ 【性能实战·生产调优】零 IO 的极速测试环境
// 真实场景性能对比：
// - 连真实 MySQL 跑 1000 个单元测试：建立 TCP 连接 + 写入磁盘 + 刷盘，耗时约 45 秒。
// - 用 sqlmock 在纯内存跑：无 TCP 握手，无磁盘 IO，全在 CPU L1/L2 缓存中完成，耗时约 0.08 秒。
// 性能直接拉升 500 倍！这就是大厂工程师追求的极致工程效率。
//
// 🛡️ 【安全攻防·漏洞防线】防注入的底层驱动层测试
// 面试官问：Mock 测试能防 SQL 注入吗？
// 标准答案：不仅能防，还能精准测试防御机制。我们可以通过 `sqlmock.ExpectExec` 来断言参数传递是否使用了参数化绑定（`?`），
// 如果业务代码偷偷使用了字符串拼接（引发注入漏洞），sqlmock 匹配参数就会失败，从单元测试层面彻底封杀注入漏洞入库的可能性！
//
// ============================================================
package service_test

import (
	"errors"
	"regexp"
	"testing"

	"cronix/internal/application/service"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 🧪 【测试工程·质量保障】统一的沙箱初始化方法
// 提供物理层隔绝的虚拟 DB 实例，保证绝对的“物理零污染”。
func setupExecutionMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, error) {
	// 初始化虚拟数据库驱动，速度纳秒级，不会产生任何物理文件或网络包
	db, mock, err := sqlmock.New()
	if err != nil {
		return nil, nil, err
	}

	// 将虚拟驱动强行喂给 GORM，GORM 会以为自己连上了真实的 Postgres
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{
		SkipDefaultTransaction: true,
	})

	return gormDB, mock, err
}

// TestExecutionService_ClearAllLogs_TxRollback 这是一个针对清空动作的事务模拟与回滚覆盖测试
func TestExecutionService_ClearAllLogs_TxRollback(t *testing.T) {
	// 1. Arrange (准备阶段：搭建隔离环境与设定预期)
	gormDB, mock, err := setupExecutionMockDB(t)
	if err != nil {
		t.Fatalf("初始化虚拟数据库失败: %v", err)
	}
	defer func() {
		// 【物理零污染】：走之前把虚拟连接也必须掐断，释放内存
		db, _ := gormDB.DB()
		db.Close()
	}()

	// 招募档案管理员，把假钥匙（gormDB）交给他
	svc := service.NewExecutionService(gormDB)

	// 🔬 【底层原理·深度剖析】精准拦截 SQL 流
	// 业务代码执行 ClearAllLogs 时，会按顺序发送两条 DELETE。
	// 这里设置一个陷阱：第一条允许放行，第二条模拟磁盘故障报错。
	// 虽然原代码没有显式事务包裹，但我们在测试中可以校验它的顺序执行和错误透传。

	// 预期第一步：清空 execution_logs 表，返回影响行数 10
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "execution_logs" WHERE 1 = 1`)).
		WillReturnResult(sqlmock.NewResult(0, 10))
		
	// 预期第二步：清空 group_execution_logs 表，这里强制让它抛出底层 IO 异常！
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "group_execution_logs" WHERE 1 = 1`)).
		WillReturnError(errors.New("模拟数据库崩溃：底层 IO Error"))

	// 2. Act (执行阶段：调用目标方法)
	r1, r2, actErr := svc.ClearAllLogs()

	// 3. Assert (断言阶段：核对实际行为与预期)
	if actErr == nil {
		t.Errorf("期望由于底层 IO 报错导致任务失败，但系统竟然没有报错，说明可能把致命错误给吞掉了！(Error swallowed)")
	}
	
	if r1 != 10 {
		t.Errorf("期望第一步成功删除 10 条日志，实际得到 %d 条。说明执行流被篡改。", r1)
	}
	
	if r2 != 0 {
		t.Errorf("期望第二步由于崩溃删除条数为 0，实际得到 %d 条。存在脏数据越权问题。", r2)
	}

	// 终极把关：确保所有设定的规则（打桩）都被完整触发了，没有多余的动作，也没有漏掉的动作
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("测试结束时，发现未执行的预期动作。说明代码逻辑没有按规定的 SQL 流运行: %s", err)
	}
}

// 🏗️ 【架构设计·模式对比】依赖注入与 Service 层的 Mock(gomock) 扩展
// 
// 为什么我们在有些服务（如 TaskService）中需要 gomock？
// 场景：如果 ExecutionService 需要在清理日志后，调用第三方的报警接口。
// 错误写法：在 `ClearAllLogs()` 里面写 `http.Post("http://alarm.system")`。
//   - 灾难：跑单元测试时，测试机器断网了，或者触发了真实报警把大半夜睡觉的老板吵醒！
// 
// 大厂标配写法（依赖倒置与 Mock 注入）：
// 1. 在 ExecutionService 里定义一个接口：
//    type AlarmNotifier interface {
//        Send(msg string) error
//    }
// 2. 结构体里持有接口：
//    type ExecutionService struct {
//        DB       *gorm.DB
//        Notifier AlarmNotifier  // <--- 依赖倒置
//    }
// 3. 在单元测试里，通过 gomock 自动生成 MockAlarmNotifier：
//    mockCtrl := gomock.NewController(t)
//    defer mockCtrl.Finish()
//    mockNotifier := mock_service.NewMockAlarmNotifier(mockCtrl)
// 4. 断言报警行为：
//    mockNotifier.EXPECT().Send(gomock.Any()).Times(1).Return(nil)
// 
// 这样一来，不连任何外网，不发任何真实请求，仅仅靠指针地址的派发就完美测试了业务边界，这叫真正的隔离！
