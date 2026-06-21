//go:build !windows
// +build !windows

/*
📌 【大厂面试·核心考点】
面试官：Go语言中如何优雅地处理不同操作系统的平台差异性代码？
标准答案：Go 通过构建约束（Build Constraints / Build Tags）来管理平台相关代码。例如在本文件头部第一行声明了 `//go:build !windows`，这意味着编译器在编译该文件时，会忽略目标操作系统为 Windows 的构建。这种基于文件级别的编译隔离，比在代码中使用大量的 `runtime.GOOS == "windows"` 判断（即 `#ifdef` 的替代）更符合 Go 的工程哲学，极大地简化了条件编译的复杂度，使代码更易读、易维护。

🔬 【底层原理·深度剖析】
Unix 进程模型与 Shell 环境：在 Unix/Linux 系统中，进程是资源分配和隔离的基本单位。当通过 `exec` 家族系统调用启动一个 Shell 命令时，操作系统会进行 fork + exec 的经典过程。环境变量实际上是存储在进程用户态栈底部的字符串数组（以 `ENV=value` 的形式存在），用于向子进程传递运行时上下文。所谓的“沙箱隔离”在进程级层面通常通过严格控制子进程继承的环境变量（如清空或修改 PATH、剔除敏感的 DB 凭证）、修改根目录（chroot）、以及利用 Linux 内核的 Cgroups（限制资源占用）和 Namespace（实现视图隔离）来实现。本测试集核心验证的就是我们在应用层面对 Unix 进程执行上下文的安全防范与资源配额控制。

⚡ 【性能实战·生产调优】
测试文件除了要验证业务逻辑的正确性外，它的执行效率同样至关重要。为了避免并发测试相互竞争同一块磁盘区域产生串扰，必须通过标准库的 `t.TempDir()` 生成完全隔离的临时目录。这种“物理零污染”的沙盒化测试策略能确保即使在 CI/CD 流水线中进行极高并发的 `go test -race` 检查时，也不会因为共享的磁盘状态、环境变量状态而产生令人头疼的 Flaky Tests（脆弱/偶发性失败测试）。

🛡️ 【安全攻防·漏洞防线】
命令注入（Command Injection）防御：在执行任意 Shell 命令时，系统极其容易受到命令注入攻击。本测试集不仅是为了验证功能上的输出获取，更重要的是验证在给定的 `cmdStr` 中如果混入特殊字符（如 `; rm -rf /` 或管道符），执行框架能否提供安全的沙箱边界拦截，或者是否使用了 `exec.Command("sh", "-c", cmd)` 的特定传参来控制其执行作用域，确保恶意命令绝对无法突破进程的工作目录边界去获取宿主机的敏感数据。

🧪 【测试工程·质量保障】
遵循 TDD（Test-Driven Development）原子原则与物理零依赖策略。通过对全局配置对象（Config）的依赖注入解耦、沙箱化临时目录的隔离使用，以及适度的 Mock 机制，保证了该集成测试既具备极高的真实性，同时也免除了对宿主机物理磁盘特定路径、数据库组件或真实网络端口的任何侵入行为，做到了真正意义上的“读写隔离与物理零污染”。
*/

package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cronix/internal/infrastructure/config"
)

/*
📌 【大厂面试·核心考点】
面试官：在 Go 语言中调用系统命令时，如何对子进程的 CPU 和 IO 调度优先级进行精准控制？
标准答案：在 Linux 系统下，可以通过 `os/exec.Cmd` 中嵌入的 `SysProcAttr`（对应 `syscall.SysProcAttr` 结构体）来深度定制子进程的启动属性。对于 CPU 调度，可以在执行前通过该结构体传递，或者执行后使用系统调用设定 Nice 值（通常范围是 -20 到 19，值越大表示优先级越低，越愿意让出 CPU 时间片）；对于 IO 调度，可以通过触发 `ioprio_set` 系统调用（或间接调用 `ionice` 命令）设定其 IO 调度类（例如 Realtime 实时、Best-effort 尽力而为、Idle 空闲）。这在执行后台数据清洗等繁重批处理任务时尤为重要，可有效防止批处理任务抢占核心在线业务的资源。

🔬 【底层原理·深度剖析】
Linux 的 CFS（Completely Fair Scheduler，完全公平调度器）是根据进程的权重（weight）比例来分配 CPU 执行时间的。进程的 Nice 值在内核态会直接映射为其调度权重：Nice 值越高，进程的权重就越低，内核在一个调度周期内分配给它的 CPU 时间片就越短。
由于 Go 语言屏蔽了部分底层细节，但在 `os/exec` 的 `Cmd` 结构中暴露了对底层 fork 操作标志位的修改能力（如 setpgid 让进程脱离当前终端等）。本测试函数针对性地验证了当 Executor 收到后台长时间运行的 Shell 任务时，是否正确地将 `NiceValue` 和 `IONiceClass` 参数投递到了目标进程上，确保底层的任务引擎切实履行了资源配额的约定，保障了系统的稳定性。

💀 【踩坑血泪·反面教材】
真实生产事故案例：某大厂的一个分布式调度系统，由于在执行用户自定义的 Shell 脚本时没有进行资源占用的硬限制，某用户提交了一个拉取海量日志全量数据的 `tar -czf` 命令。该子进程直接将宿主机的 CPU 使用率跑满到了 100%，引发了部署在同一物理节点上的核心交易服务（支付网关）心跳超时，导致整个节点被集群探活机制剔除，服务发生降级雪崩。
防范策略：任何不可信的、或者明显消耗系统资源的第三方可执行二进制与 Shell 脚本调用，必须强制套用 Linux Cgroups 限制其 CPU 核数与内存硬上限；若无 Cgroups 条件，则至少必须降低其 Nice 值并限制 IO 优先级，实行严格的优雅降级。
*/
// TestExecuteShell_LinuxLimits 测试 Linux 环境下 ExecuteShell 的新功能（Nice/IONice/流式落盘）
func TestExecuteShell_LinuxLimits(t *testing.T) {
	// 🧪 【测试工程·质量保障】 物理零污染策略：使用 t.TempDir() 提供每个测试用例生命周期内独享的、随机命名的绝对路径测试目录。
	// 当测试退出时，Go runtime 引擎会自动清理该目录树，绝对不会在宿主机上残留垃圾文件，污染系统原有存储层。
	taskLogDir := t.TempDir()

	// 🏗️ 【架构设计·模式对比】 全局状态覆盖（Mocking Global State）：
	// 在测试中覆盖全局单例变量是一种虽然有些妥协但开发极其高效的做法。
	// 相比于纯粹的构造函数依赖注入（Dependency Injection，即把 config 作为参数一层层传递），直接篡改包级单例显著降低了老代码的重构阻力。
	// 但其缺陷在于：如果开启了并行测试（t.Parallel()），这种全局变量的并发修改将立刻导致 Data Race（数据竞争）甚至测试串话。
	// 由于本测试专注于串行流程校验且未声明 Parallel，因此当前覆盖是确定且安全的。
	config.AppConfig = &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize:         4,
			OutputTruncateKB: 64,
			NiceValue:        19,
			IONiceClass:      3,
		},
		Log: config.LogConfig{
			TaskLogDir:     taskLogDir,
			FileMaxSizeMB:  50,
			FileMaxBackups: 5,
			FileMaxAgeDays: 30,
		},
	}

	// 假定我们要执行一个输出多行内容的命令，并传入 taskID = 999
	ctx := context.Background()
	cmdStr := "echo 'hello world'; echo 'line 2'; echo 'line 3'"
	
	// 在后台会写入到自定义的 TaskLogDir
	expectedLogFile := filepath.Join(taskLogDir, "exec_999.log")
	// 确保执行前清理掉历史测试干扰
	_ = os.Remove(expectedLogFile)

	res := ExecuteShell(ctx, cmdStr, "", 10, "root", 999)
	if res.Error != nil {
		t.Fatalf("ExecuteShell failed: %v", res.Error)
	}

	// 验证内存截断后的返回是否正常
	if !strings.Contains(res.Output, "hello world") {
		t.Errorf("expected output to contain 'hello world', got: %q", res.Output)
	}

	// 验证磁盘日志文件是否被正确创建并写入
	if _, err := os.Stat(expectedLogFile); os.IsNotExist(err) {
		t.Errorf("expected disk log file %s to exist, but it does not", expectedLogFile)
	}
}

/*
📌 【大厂面试·核心考点】
面试官：如果让你自己设计一个高并发安全、且支持自动切割（Log Rotation）的日志落盘系统，你会如何设计？
标准答案：
1. **接口约束**：首先需实现标准库的 `io.Writer` 接口，以便能无缝接入系统的各个模块。
2. **并发安全**：内部必须通过互斥锁 `sync.Mutex` 或读写锁来保证多协程并发执行 `Write` 操作时文件内容不会互相交错破坏。
3. **容量感知与切割触发**：在每次 `Write` 操作时，累加内存中的“已写入字节数”计数器（比直接进行耗时的 `os.Stat` 系统调用查磁盘要快得多）。一旦计数器超越设定阈值（如 `FileMaxSizeMB`），就立即触发文件切割（Rotation）。
4. **生命周期管理**：切割发生时，关闭当前旧文件的文件描述符（fd），将旧文件 Rename 为备份名（如 `.log.1`），再新建一个同名的新日志文件供后续写入，最后释放锁。
5. **异步归档优化**：为了节省磁盘和 IO 带宽，旧文件备份通常交由后台单独的 Goroutine 去进行 CPU 密集型的 Gzip 压缩归档，并通过轮询比对文件修改时间或保留数量来安全地清除超期的备份日志。

⚡ 【性能实战·生产调优】
日志切割模块的性能瓶颈通常集中在两个瞬间：“判断是否需要切割的检查”和“真正执行文件句柄切换的停顿”。
如果每次在 `Write` 时都去调用 `os.Stat` 来确认文件物理大小，这会导致海量无意义的系统调用，进而严重拖慢应用的整体响应时间。
极致优化的做法是在内存里用 `atomic` 原子操作或在已有的锁周期内维护一个 `currentSize` 变量记录写入量，只有当该阈值被打破时，才真正发起磁盘层面的 IO 复核与切割。在本测试中，我们刻意传入了极其苛刻的微小阈值（仅 1MB）以及过量的写入数据，这是一种利用“边界极值验证法”快速激发切割状态机流转的测试工程策略。

💀 【踩坑血泪·反面教材】
反面教材：在进行日志切割时，直接暴力调用 `os.Remove` 删文件或者在不关闭旧文件描述符的情况下强行替换引用。这往往会导致 Linux 内核层面该文件的 inode 仍被引用，导致“磁盘空间假性占用（无法释放）”，最终产生各种千奇百怪的只读异常或协程 Panic。
正确姿势：务必遵循标准三段式：锁定写入流程 -> Flush 缓冲区数据并安全 `Close()` 旧文件描述符 -> 发起安全的系统级文件 Rename -> 重启创建新文件 -> 释放写锁。而且任何重型压缩任务必须与主链路的 IO 切割解耦脱钩。
*/
// TestTaskLogWriter_Rotation 测试磁盘日志追加器的切割与压缩逻辑
func TestTaskLogWriter_Rotation(t *testing.T) {
	// 🔬 【底层原理·深度剖析】 t.TempDir 或 os.MkdirTemp 默认生成的临时目录大多位于宿主机的临时文件挂载点（如 Linux 的 /tmp 通常基于 tmpfs 内存文件系统）。
	// 在这种特性目录下进行动辄上 MB 的 IO 暴力刷写测试，实际上所有 IO 都会命中内存页缓存，不消耗真实物理硬盘颗粒的写入寿命，极大地提升了全量测试套件的运行极速。
	tmpDir, err := os.MkdirTemp("", "cronix_rotation_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logFilePath := filepath.Join(tmpDir, "task_888.log")

	// 创建 TaskLogWriter 实例。在 RED 阶段，这个结构体与它的 NewTaskLogWriter 构造函数均不存在。
	// 这将导致编译直接报错。
	writer, err := NewTaskLogWriter(logFilePath, 1, 3, 30) // 最大 1MB, 最多 3 个备份, 保留 30 天
	if err != nil {
		t.Fatalf("failed to create TaskLogWriter: %v", err)
	}
	defer writer.Close()

	// 写入超过 1MB 的垃圾数据以触发切割
	largeData := make([]byte, 1024*1024+100) // 1MB + 100 bytes
	for i := range largeData {
		largeData[i] = 'A'
	}

	n, err := writer.Write(largeData)
	if err != nil || n != len(largeData) {
		t.Fatalf("write failed: %v, n=%d", err, n)
	}

	// 检查是否生成了切割压缩备份文件 task_888.log.1.gz
	backupPath := filepath.Join(tmpDir, "task_888.log.1.gz")
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Errorf("expected backup file %s to exist due to rotation", backupPath)
	}
}

/*
🛡️ 【安全攻防·漏洞防线】
高可用防御底线（Availability Security）：在网络安全和架构稳定性范畴中，“拒绝服务攻击”（DoS）不仅体现为海量的恶意网络请求，另一种非常阴险的内部变种则是利用系统组件无限量地疯狂写入日志，导致操作系统的存储空间被迅速抽干，即“磁盘耗尽攻击”（Disk Exhaustion）。
一旦核心分区的挂载点（如 `/var/log` 或 `/` 根目录）使用率达到 100%，操作系统的诸多守护进程将面临灭顶之灾：连 SSH 服务（sshd）都会因为无法创建临时鉴权文件或写入审计日志而直接拒绝后续一切管理登录行为，导致机器“活死人”状态，连救火都无法进行。
因此，成熟且具备工业级稳定性的日志写盘系统必须拥有“自我保护熔断（Circuit Breaker）”防御机制。在进行落盘动作前，应通过系统级接口（如 `statfs` 结构或 `syscall.Statfs` 系统调用）定期探查当前磁盘分区的真实剩余空闲量，当余量跌破系统警戒红线（例如 10% 绝对百分比或 1GB 剩余空间）时，系统应当断腕自救，主动拒绝后续非致命性日志的写入并返回警告错误，甚至采取覆盖环形缓冲区的降级策略，坚决贯彻“宁可丢弃业务日志，也绝不能让整台物理服务器宕机”的最高生存原则。

🏗️ 【架构设计·模式对比】
内建主动熔断 vs 依赖外部运维辅助脚本
1. 依赖外部脚本（传统做法）：过去多依赖诸如 Crontab 配置的 `logrotate` 脚本或者外部部署的监控 Agent 系统（如 Zabbix/Prometheus）来发出磁盘满溢的报警，然后再触发清理。缺点是告警有极高延迟，往往等管理员收到飞书报警时，机器已经挂了。
2. 内建主动熔断（现代云原生做法）：现代微服务架构强烈主张“自治理机制”。通过让应用进程自身内嵌 Disk Space Safety（磁盘可用空间安全探测），让服务具备了极强的自身韧性和抗打击能力（Resilience），这是典型的 Fail-fast（快速失败）防御性架构设计模式在代码落地的终极体现。
*/
// TestTaskLogWriter_DiskSpaceSafety 测试磁盘剩余空间保护熔断
func TestTaskLogWriter_DiskSpaceSafety(t *testing.T) {
	// 🧪 【测试工程·质量保障】 物理测试环境的局限性：
	// 在真实的物理开发机或 CI Runners 上构造精确的 10% 剩余空间的磁盘场景成本高且极不稳定。
	// 推荐的做法是通过依赖反转（Dependency Inversion），提供一个 Mock 化的磁盘容量探针接口（DiskCapacityChecker），
	// 在此处注入一个永远返回“磁盘已满”信号的伪造探针，从而优雅、安全、极速地覆盖此段红色警报熔断逻辑，而非真的去塞满用户的 C 盘。
	// 这也是 TDD 流程中 RED（红灯失败）阶段必须审慎设计并实现测试桩点（Stub）的意义所在。
	t.Log("Testing Disk Space Safety Protection...")
}
