/*
📌 【大厂面试·核心考点】
- 面试官问：如何测试一个持续产生输出的Shell脚本的流式读取和实时落盘？
- 标准答案：通过异步协程启动脚本，主协程定期轮询或使用 fsnotify 监听日志文件的变化。为了避免操作系统的 I/O 缓冲延迟，底层命令执行时需确保标准输出 (Stdout) 绑定到了无缓冲或手动 Sync 的文件句柄上，在测试侧则使用定时器配合文件读取进行断言。
- 面试官问：如何安全地测试任务超时或手动强杀机制？
- 标准答案：使用 context.WithCancel 或者 context.WithTimeout 传入执行器，测试强杀接口是否正确调用了 cancel()，并结合 select 等待结果通道。若超时未返回，则判定强杀失效，触发 fail。

🏗️ 【架构设计·模式对比】
- 轮询读取 vs 事件驱动 (fsnotify)：
  本测试使用 `time.Sleep` 轮询断言文件内容。虽然略显粗糙，但对于测试验证 "实时写" 行为已足够轻量且无外部依赖。在生产环境的流式推送（如 WebSocket 给前端）中，建议采用 Tail 模式或通过 `io.Pipe` 直接将命令输出接入到 Channel 或 WebSocket 中，避免磁盘 I/O 成为瓶颈。

🧪 【测试工程·质量保障】
- 物理零污染原则：本测试运行后产生的文件均使用特定的 taskID(9999) 避免冲突，且在开始时执行清理 (os.Remove)。更严谨的做法是使用 `t.TempDir()` 生成临时目录，测试结束后由 go test 自动销毁。
- 测试覆盖范围：包含 "日志实时落盘"（IO测试）与 "任务强杀拦截"（信号与并发控制测试）。
*/
package executor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestExecuteShell_StreamingAndCancel 验证流式日志能否实时落盘，以及手动强杀接口能否工作
/*
🔬 【底层原理·深度剖析】
在 Go 中，`exec.Cmd` 的 Stdout 和 Stderr 如果被赋值为文件句柄 `*os.File`，由于文件系统的缓冲机制，输出可能不会立即 flush 到磁盘，导致外部程序读取不到。
通常有两种解决底层缓冲的原理级方案：
1. `cmd.Stdout = f`：依赖操作系统的底层 page cache 刷新机制，Go 的 os.File 在不调用 Sync() 时，部分写入可能停留在内存中。但在较小数据量的测试中，echo 的写入通常能及时被读到。
2. 使用 `io.MultiWriter` 结合自定义的 `Write` 方法，在每次 Write 后显式调用 `f.Sync()`，这是最稳妥的"实时落盘"策略。

💀 【踩坑血泪·反面教材】
- 错误做法：测试中不加延时直接去读 `logFile`，导致 100% 出现 flaky test (偶发失败)，因为协程调度和文件创建有时间差。
- 错误做法：在 Windows 下测试强杀时只杀了父进程，遗留了 cmd.exe 的子进程，导致句柄泄露。
- 避免方式：本框架中应当使用 Process Group (如 syscall.SysProcAttr 的 Setpgid) 来保证强杀时杀死整个进程树。
*/
func TestExecuteShell_StreamingAndCancel(t *testing.T) {
	// Edge Case 1: 确保 data/logs 目录被正确自动创建
	logDir := filepath.Join("data", "logs")
	os.MkdirAll(logDir, 0755)

	taskID := uint(9999)
	logFile := filepath.Join(logDir, "exec_9999.log")
	os.Remove(logFile) // 清理之前的遗留

	// 根据操作系统选择长时间运行的脚本
	/*
	⚡ 【性能实战·生产调优】
	Windows 和 Linux/Darwin 的 Shell 环境截然不同。Windows 缺乏原生的 `sleep` 命令，常被 `ping 127.0.0.1 -n 4 > nul` 替代以实现约 3 秒的阻塞，或是使用 PowerShell 的 `Start-Sleep`。
	生产环境中编写跨平台执行引擎时，更推荐对底层进行封装，或者将命令本身打包为一个独立的 Worker 进程，而不是强依赖宿主机 Shell。
	*/
	var script string
	if runtime.GOOS == "windows" {
		// Windows 延迟 3 秒：每秒 ping 一次
		script = `echo STREAM_START && ping 127.0.0.1 -n 4 > nul && echo STREAM_END`
	} else {
		// Linux 延迟 3 秒
		script = `echo STREAM_START; sleep 3; echo STREAM_END`
	}

	// 异步启动脚本
	/*
	🛡️ 【安全攻防·漏洞防线】
	并发场景下，执行结果的投递采用了 `make(chan *ShellResult, 1)`。此处容量为 1 的缓冲通道是关键防线：
	如果不用缓冲通道，当主协程因断言失败提前退出 (`t.Fatalf`) 时，`go func()` 里的发送动作 `resultCh <- res` 将被永久阻塞，导致协程泄漏 (Goroutine Leak)。
	缓冲为 1 则确保无论主协程死活，工作协程都能顺利结束并被 GC 回收。
	*/
	resultCh := make(chan *ShellResult, 1)
	go func() {
		res := ExecuteShell(context.Background(), script, "", 10, "", taskID)
		resultCh <- res
	}()

	// 1. 测试流式日志写入 (等待一点时间让脚本输出第一行)
	time.Sleep(1 * time.Second)
	
	// 读取临时日志文件，期望看到 STREAM_START 且此时进程还在阻塞运行
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("【测试失败】流式日志临时文件未生成: %v", err)
	}
	if !strings.Contains(string(content), "STREAM_START") {
		t.Errorf("【测试失败】流式日志未实时落盘，当前内容: %s", string(content))
	}

	// 2. 测试主动强杀 (取消执行)
	// 这个 CancelExecution 函数是计划要新增的
	/*
	🔬 【底层原理·深度剖析】
	`CancelExecution` 背后通常是调用保存起来的 `context.CancelFunc`，它会触发 `exec.Cmd` 内部向目标进程发送 SIGKILL (或 Windows 下的 TerminateProcess)。
	值得注意的是，SIGKILL 无法被用户态程序捕获和忽略，是最高等级的强杀。如果业务需要让脚本有善后机会，应发送 SIGTERM (Linux) 或 CTRL_BREAK_EVENT (Windows)，等待超时后再使用 SIGKILL。
	*/
	success := CancelExecution(taskID)
	if !success {
		t.Errorf("【测试失败】CancelExecution(9999) 返回失败，没能找到或取消对应任务")
	}

	// 等待结果
	/*
	🧪 【测试工程·质量保障】
	基于 select 和 time.After 构建超时屏障是测试工程中必用的手段。
	它可以避免因为 CancelExecution 逻辑出现 bug（如死锁、死循环等）导致整个 `go test` 永远 hang 住。
	*/
	select {
	case res := <-resultCh:
		// 验证进程是否确实被中断
		if res.ExitCode == 0 {
			t.Errorf("【测试失败】预期脚本被强杀返回非 0，但返回了 0。输出: %s", res.Output)
		}
		if strings.Contains(res.Output, "STREAM_END") {
			t.Errorf("【测试失败】脚本未被成功强杀，仍然输出了 STREAM_END")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("【测试失败】CancelExecution 未能结束进程，导致 ExecuteShell 一直阻塞")
	}

	// 3. 测试清理机制（文件应在结束并 Dump 到 DB 后删除）
	// 注意：当前因为仅测试 Executor 层，若文件删除由上层(Scheduler)处理，此处断言可根据实际职责跳过
	// 但如果 Executor 负责产生临时文件，应暴露给调用方去读。测试暂且到此。
}
