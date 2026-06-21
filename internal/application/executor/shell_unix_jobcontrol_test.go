//go:build !windows
// +build !windows

/*
📌 【大厂面试·核心考点】
面试官常问：在 Go/Linux 下启动子进程，如果主进程崩溃或者超时被杀，子进程衍生的“孙子进程”会不会变成孤儿进程导致资源泄露？
标准答案：会！默认情况下 `exec.Command` 启动的进程被 `kill` 时，信号只会发送给该进程本身。如果它派生了后台子进程（即孙子进程），孙子进程会被系统 init (PID 1) 或 sub-reaper 接管，继续占用系统资源（端口、CPU、内存等），造成严重资源泄露。
解决方案：必须使用 Unix 进程组控制（Process Group）。通过设置 `SysProcAttr.Setpgid = true`，让子进程成为一个新进程组的 Leader。杀进程时，通过给 `-PGID`（负数的进程组ID）发送 `SIGTERM/SIGKILL` 信号，实现整个进程树的级联查杀。

🏗️ 【架构设计·模式对比】
| 方案 | 优点 | 缺点 | 适用场景 |
| --- | --- | --- | --- |
| Kill 进程本身 | 简单，Go 默认行为 | 无法杀死衍生进程，导致孤儿泄漏 | 确定无子进程的单一短任务 |
| Kill 进程组 (-PGID) | 彻底查杀进程树 | 依赖 Unix 特性，跨平台兼容性差 (Windows 不支持) | 复杂 Shell 脚本、后台常驻任务 |
| CGroup 查杀 | 最安全彻底，防止 double-fork 逃逸 | 需要 Root 权限和 CGroup 挂载配置 | Kubernetes 等容器化环境 |
*/

package executor

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"cronix/internal/infrastructure/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/*
🔬 【底层原理·深度剖析】
在 Linux 内核中，进程有 PID（进程ID）、PGID（进程组ID）、SID（会话ID）。
当我们执行 `bash -c` 脚本并在内部使用 `&` 放入后台时，它发生了一次 `fork()`。如果不进行特殊干预，它默认与主程序共享同一个 PGID。
如果我们在 Go 中因超时杀死了 `bash`，但没有杀其所在的进程组，后台派生进程不会收到 SIGTERM/SIGKILL，从而沦为孤儿进程。
这也是为什么本测试被称为 "Escape" (逃逸) 测试——我们要验证并证明：如果不使用进程组（或 CGroup），恶意/顽固的后台孙子进程是如何逃脱制裁的。

🧪 【测试工程·质量保障】
该测试函数采用 TDD (Test-Driven Development) 中的 "RED" 阶段哲学（即：在重构或开发防御机制前，测试必须先失败，暴露问题）。
测试策略：
1. 注入故意产生逃逸的后台进程脚本（双重 Fork / 常驻后台）。
2. 捕获并断言其逃逸成功（证明在没有 Process Group 防护下存在漏洞）。
3. 物理清理：利用 `kill -9` 进行强制兜底清理（严格遵循测试数据的"物理零污染"原则，保持测试机环境纯洁无副作用）。
*/
func TestExecutor_JobControl_Escape_Red(t *testing.T) {
	// 确保 CGroups 关闭，使用基础 Shell 回退
	config.AppConfig = &config.Config{}
	config.AppConfig.Executor.EnableCGroups = false

	/*
	💀 【踩坑血泪·反面教材】
	真实生产事故案例：某大厂定时任务调度系统，用户提交了包含 `nohup wget ... &` 的拉取脚本。
	任务超时后，Go 调度器调用 `cancel()` 取消了 Context。虽然上层的 bash 进程退出了，但底层的 `wget` 进程仍在后台疯狂运行，最终耗尽了物理机的网络带宽和磁盘 IO。
	如何避免：永远不要信任用户的脚本行为，必须将每次调度隔离并封装在独立的进程组（或 Namespace/Cgroup）中进行生命周期强制管控。
	*/
	// 该脚本启动一个常驻后台的孙子进程
	// 并输出其 PID 到标准输出以便我们在测试中读取
	command := `
bash -c 'echo "GRANDCHILD_PID=$$"; while true; do sleep 1; done' &
sleep 5
`

	/*
	⚡ 【性能实战·生产调优】
	此处使用 `context.WithTimeout` 提供 2 秒超时，而脚本本体会故意 sleep 5 秒以触发超时控制。
	时间复杂度：O(1)，严格受限于 Context 的超时机制与操作系统的上下文切换效率。
	生产级调优手段：在真实的生产级 Executor 中，对于涉及大量资源分配的 Shell，应当先发送 `SIGTERM` (15) 给其 PGID，留出约 3-5 秒的优雅退出时间（Graceful Shutdown），若进程在宽限期后仍在运行，再发送 `SIGKILL` (9) 强制绞杀。
	*/
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res := ExecuteShell(ctx, command, "", 5, "", 0)

	// 解析输出找到孙子进程的 PID
	output := res.Output
	var grandchildPID string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "GRANDCHILD_PID=") {
			grandchildPID = strings.TrimPrefix(line, "GRANDCHILD_PID=")
			break
		}
	}

	require.NotEmpty(t, grandchildPID, "Could not find grandchild PID in output: %s", output)

	/*
	🛡️ 【安全攻防·漏洞防线】
	安全漏洞类型：资源耗尽型攻击（Denial of Service - DoS）。恶意用户可通过无限 Fork 子进程（Fork Bomb）占满系统的 PIDs 表，导致机器瘫痪。
	防御策略：结合 Linux 的 `ulimit -u` (RLIMIT_NPROC) 限制最大进程数，并配合完美的进程组级联查杀机制。
	探针技巧：这里的 `kill -0` 是一种无害的探活技巧，它并不会向进程发送实际的终止信号，内核仅执行访问权限检查和进程存在性检查。非常适合作为极低开销的健康状态探针。
	*/
	// 测试孙子进程是否存活
	// kill -0 <pid> 可以检查进程是否存在
	err := exec.Command("kill", "-0", grandchildPID).Run()
	
	// 如果 err == nil，说明进程存活（逃逸成功！）
	// 在 RED 阶段，这会触发 assert 失败（因为我们期望进程必须被杀掉）
	assert.Error(t, err, "Grandchild process %s escaped and is still running! This is a severe process leak.", grandchildPID)

	// 为了不污染测试机，如果它还活着，我们需要强杀它
	// ❗️此动作严格遵循“测试物理零污染”的铁律：即使在 RED 阶段预期进程会发生逃逸，测试结束前也绝对必须亲手清理掉产生的衍生垃圾进程，绝不在宿主机留下隐患。
	if err == nil {
		_ = exec.Command("kill", "-9", grandchildPID).Run()
	}
}
