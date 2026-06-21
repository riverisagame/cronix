package scheduler

/*
📌 【大厂面试·核心考点】
面试官：如何保证分布式调度系统中，不同任务执行器（Executor）之间的环境沙箱彻底隔离？
标准答案：
1. **命名空间隔离（Namespace）**：利用 Linux 的 PID/Mount/Network/User 命名空间，让任务运行在独立的进程树与隔离的文件系统中，互不干扰。
2. **工作目录隔离（CWD挂载）**：每个任务执行前，动态创建唯一的临时目录作为 CWD（Current Working Directory），执行完毕后进行安全销毁。严禁任务直接访问宿主机核心文件系统。
3. **环境变量隔离（Env Pollution）**：任务进程不仅要清空宿主机的默认环境变量，只注入该任务需要的白名单变量（如执行参数、临时 Token），还必须防止任务内部通过恶意手段修改全局环境配置。
4. **资源硬性限制（Cgroups）**：限制任务所能使用的 CPU、内存以及磁盘 IO，防止单一异常任务“饿死”其他正常任务，保障多租户资源隔离与配额控制。

🔬 【底层原理·深度剖析】
本文件目前主要测试调度引擎执行器（Executor）在“触发器与调度器层面”的**逻辑隔离性**。在此之上，真正的物理沙箱隔离还需要在 `Executor.Run` 的底层命令封装中实现：
- 进程级的环境变量隔离注入（基于 `os/exec.Cmd.Env`），绝对不能复用全局进程环境变量或上一次任务残留。
- 独立的工作目录挂载点（基于 `os/exec.Cmd.Dir`），防止同一默认工作目录下的临时文件读写冲突（例如写死同名输出文件导致的覆盖、脏读或文件锁死锁）。

💀 【踩坑血泪·反面教材】
**真实生产事故案例**：
某大厂的离线调度系统曾出现一个致命故障：所有脚本任务都在同一个默认工作目录（如 `/tmp/jobs/`）下并发执行。某个数据清洗脚本执行完毕后使用了 `rm -rf *` 试图清理自身产生的临时缓存文件，结果瞬间把正在同一目录并发执行的其他核心定时任务的代码包、配置和输出结果全部删除，导致全网调度瘫痪。
**防御策略**：
必须为每次执行（Execution）生成形如 `/var/run/cronix/exec_UUID/` 的绝对隔离且唯一的工作目录，赋予最低执行读写权限，并在执行结束后由守护进程安全清理回收。

🛡️ 【安全攻防·漏洞防线】
- **变量污染攻击（Env Pollution）**：恶意任务尝试通过注入 `LD_PRELOAD` 或 `PATH` 等危险环境变量，拦截并篡改系统级动态库函数调用，从而逃逸沙箱获取宿主机权限。防御方法是在执行环境启动时对环境变量进行强效白名单净化。
- **目录穿越攻击（Path Traversal）**：命令或者脚本参数中传入 `../../` 试图访问沙箱外部的核心配置文件（如 `/etc/passwd`）。防御方法是在配置 CWD 时，利用 `chroot` 或限制后续衍生进程的一切路径解析操作不得跨越该 CWD 物理边界。
*/

import (
	"cronix/internal/infrastructure/config"
	"cronix/internal/domain/model"
	"testing"
	"time"
)

// TestExecutor_ExecutionIsolation 测试任务触发的隔离性，
// 确保触发某个任务时不会导致其他无关任务被附带执行（避免曾经的第0层全量执行Bug）。
/*
🧪 【测试工程·质量保障】
- 测试策略：基于真实的数据库驱动进行集成测试，确保任务触发逻辑在 ORM 层和调度引擎层的流转无串扰。
- 物理零污染原则：使用测试专属的数据库实例或内存级 SQLite 引擎进行 `setupExecutorTestDB`，所有生成的任务与执行日志数据均在测试环境闭环运转，测试结束后自动销毁，绝不对生产数据库或现有业务产生任何 DDL（结构修改）或 DML（数据修改）污染。
- 隔离验证覆盖：此用例目前旨在证明事件触发器机制的“精确路由”。为了保证执行器沙箱的系统级完备性，配套的 CWD 读写互斥物理隔离测试与 Env 黑白名单沙箱注入测试也是同等重要的防御面。
*/
func TestExecutor_ExecutionIsolation(t *testing.T) {
	db := setupExecutorTestDB(t)

	// 创建两个独立的测试任务 A 和 B
	/*
	🏗️ 【架构设计·模式对比】
	在构建测试基线数据（Test Stub）时，我们通过代码硬编码动态构建最小化的 Mock Task 模型：
	- 错误做法：读取或连接生产环境的任务配置进行回归测试。这样极易造成测试指令与真实生产指令混淆，引发不可预知的脏数据执行和灾难性覆盖。
	- 正确做法（当前）：从零构造绝对纯净的虚拟任务。`Command: "echo 'A'"` 是极为轻量的 Shell 验证命令，它不仅验证了触发引擎的任务间逻辑隔离，也能作为后续验证 CWD 挂载和 Env 环境变量是否精准注入的基础微载体。
	*/
	taskA := model.Task{
		ID:         1,
		Name:       "task-a",
		TaskType:   "shell",
		Command:    "echo 'A'",
		Enabled:    true,
		TimeoutSec: 10,
	}
	taskB := model.Task{
		ID:         2,
		Name:       "task-b",
		TaskType:   "shell",
		Command:    "echo 'B'",
		Enabled:    true,
		TimeoutSec: 10,
	}

	if err := db.Create(&taskA).Error; err != nil {
		t.Fatalf("create task A: %v", err)
	}
	if err := db.Create(&taskB).Error; err != nil {
		t.Fatalf("create task B: %v", err)
	}

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:         4,
			OutputTruncateKB: 64,
		},
	}

	engine := NewEngine(db)
	executor, err := NewExecutor(db, cfg, engine)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}

	// 模拟触发 Task A (比如手动触发或定时器触发)
	// 在修复前，这会导致 Task B 也被执行。修复后，只有 Task A 被执行。
	/*
	⚡ 【性能实战·生产调优】
	- RunTaskNow 和 handleTrigger 在底层会将任务执行请求安全地推入一个有界任务队列（Bounded Channel），随后交由配置的协程池（此处 PoolSize = 4）执行。
	- 这种隔离与池化设计确保了调度引擎主体不会因为突发的并发任务海啸（Task Thundering Herd）而被耗尽系统级 Go 协程资源，严格保障了宿主机的内存与 CPU 负载维持在安全水位。
	*/
	executor.RunTaskNow(taskA.ID)
	executor.handleTrigger(taskA.ID)

	// 等待异步执行完成
	/*
	💀 【踩坑血泪·反面教材】
	- 异步测试隐患：使用硬编码的 `time.Sleep(1 * time.Second)` 会无意义地拖慢整体 CI 测试套件的执行效率。在 CI 服务器并发极高、CPU 出现饥饿调度时，1秒的等待可能依然无法完成异步执行，从而引发难以排查的 Flaky Test（时过时不过的随机闪烁测试）。
	- 工程进阶做法：更严谨的测试工程实践应当注入 `sync.WaitGroup`、利用回调机制（Channel Callback 通知），或者引入退避轮询来探测数据库状态（带重试兜底和超时异常控制），以实现100%确定性（Deterministic）的并发控制与等待。
	*/
	time.Sleep(1 * time.Second)

	var countA, countB int64
	/*
	🔬 【底层原理·深度剖析】
	验证执行器沙箱与触发隔离性的“黄金法则”：
	在隔离测试模型或沙箱越权渗透测试中，“反向安全验证（Negative Testing）”往往比“正向流转验证”更为致命与关键。
	我们不仅要确信任务 A 被引擎正确拉起且写出了沙箱执行日志（countA > 0），更要以绝对零容忍的工程态度验证任务 B 的执行记录严格恒等于 0（countB == 0）。
	这就是系统级安全隔离的终极奥义：每一次任务执行操作只能产生它被明确授权的直接副作用，绝不容许产生任何非预期的资源外溢或逻辑越权影响。
	*/
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", taskA.ID).Count(&countA)
	db.Model(&model.ExecutionLog{}).Where("task_id = ?", taskB.ID).Count(&countB)

	// 触发了两次 Task A（一次 RunTaskNow，一次 handleTrigger），所以 countA 应该 >= 2
	// 有时候 handleTrigger 是完全异步的，所以给一点等待时间，这里主要验证不为0即可
	if countA == 0 {
		t.Errorf("expected task A to be executed, but got 0 logs")
	}

	// 核心验证：Task B 不应有任何执行记录
	if countB != 0 {
		t.Errorf("FATAL BUG: Task B was unexpectedly executed %d times when only Task A was triggered", countB)
	}
}
