/*
📌 【大厂面试·核心考点】
1. Windows vs Linux 进程管理差异：面试官常问“在Go中如何可靠地杀死一个包含了子进程的进程树？”。标准答案是：Linux下使用进程组（Setpgid），而Windows下必须使用Job Object（作业对象），否则单纯调用 `Process.Kill()` 只会杀死父进程（如cmd.exe），导致其拉起的子进程变成孤儿进程，发生资源泄漏。
2. Context超时控制的底层实现：Go的 `exec.CommandContext` 是如何工作的？答：它会启动一个后台goroutine监听 `ctx.Done()`，一旦触发就会调用系统级的Kill信号（Windows上是 `TerminateProcess` API）来终止直接关联的子进程。

🔬 【底层原理·深度剖析】
1. Windows Job Object（作业对象）：就好像是Linux的Cgroups，它允许将一组进程作为一个单元进行管理。当把一个主进程放进Job Object后，它派生的所有子进程默认也会在此Job中。通过关闭Job Object的句柄或终止Job，可以一次性清理整个进程树。这是Windows平台防范“孤儿进程”的唯一正确姿势。
2. cmd.exe vs PowerShell 执行差异：`cmd /c` 是非常轻量级的命令包装器，启动速度快（约10-20ms），但只支持批处理语法；而 PowerShell 启动极慢（约200-500ms），因为它需要加载整个 .NET CLR 环境，但支持复杂的面向对象管道和高级安全策略（Execution Policy）。
3. Windows 安全模型与进程组：Windows 没有严格意义上的进程组（Process Group），而是以 Session（会话）、Window Station、Desktop 作为隔离边界。服务进程通常运行在 Session 0，这意味着如果用Shell拉起的命令弹出GUI窗口，是完全不可见的（Session 0 Isolation）。

⚡ 【性能实战·生产调优】
- 性能数据：使用 `cmd /c` 执行空命令的开销约为2MB内存，耗时15ms；而执行 `powershell -c` 耗时往往超过200ms，内存峰值达到30MB以上。在高频执行短任务的场景下，绝对禁止使用 PowerShell。
- 优化手段：如果不依赖Shell特性（如管道 `|`，重定向 `>`，内部命令 `dir` 等），应直接执行目标二进制文件（如 `exec.Command("git", "status")`），这能避免 `cmd.exe` 作为中间层带来的数十毫秒延迟和进程创建开销。

🛡️ 【安全攻防·漏洞防线】
- 命令注入漏洞（Command Injection）：如果传入的 `command` 包含未经过滤的用户输入（例如 `ping ` + 用户输入），攻击者可以通过追加 `& rm -rf /`（Windows下是 `& del /f /s /q C:\*`）执行任意恶意代码。
- 防御策略：强烈建议避免拼接整个Shell字符串，而是将可执行文件与参数分开，利用 `exec.Command(exe, args...)`，让Go底层调用 `CreateProcessW` 的防逃逸机制处理参数，而不是通过 `cmd.exe` 解析。

🏗️ 【架构设计·模式对比】
- Go SysProcAttr 的 Windows 扩展：在理想的架构下，Windows的命令执行应当设置 `cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}` 来隔离Ctrl+C信号，或者自己使用系统调用结合 `CreateJobObject` 实现进程树的生命周期绑定。
- 当前架构妥协：本文件采用 `exec.CommandContext` 和简单的 `cmd /c` 方案。好处是极简、跨平台兼容性好；缺点是如果执行类似 `cmd /c start_server.bat`，bat再拉起Java进程，超时时仅会杀掉cmd进程，Java进程会发生泄漏。

💀 【踩坑血泪·反面教材】
- 真实事故：某生产环境的定时任务调度器使用 `cmd /c python script.py` 执行任务，并配置了30秒超时。如果脚本死锁，超时触发，Go通过 `cmd.Process.Kill()` 只杀死了 `cmd.exe`，导致残留的 `python.exe` 进程堆积，最终耗尽服务器 128GB 内存致使节点崩溃。
- 如何避免：在Windows生产环境中，必须引入第三方库（如go-winjob）或自行使用 `syscall` 调用 Win32 API 创建Job Object，将进程分配进Job中，超时时对Job发出Terminate操作。

🧪 【测试工程·质量保障】
- 测试策略：测试本模块时，不能只测普通的快速命令。必须写一个能生成孙进程（父生子，子生孙）的测试用例（如用Go写一个休眠5秒并拉起子进程的小程序），然后设置2秒超时，断言不仅父进程被杀，更要检查系统中是否残留了孙进程，以此倒逼架构改进为Job Object模式。
*/
// ============================================================
// internal/executor/shell_windows.go - Windows系统的Shell命令执行器
// Windows不支持进程组概念，所以用 cmd.exe /c 来执行命令
// 超时时用 Process.Kill() 终止进程
// ============================================================
package executor

import (
    "bytes"         // 📌 【底层原理】字节缓冲区：用于在内存中聚集输出，避免频繁的系统调用（Syscall）开销。像一个可以自动扩容的水桶，避免产生大量字符串碎片。
    "context"       // 📌 【核心考点】上下文：Go并发控制的灵魂，通过 Done() channel 实现超时与取消的级联广播。
    "fmt"           // 📌 【架构设计】格式化：用于路径拼接等轻量级字符串操作。
    "io"            // 📌 【底层原理】接口抽象：MultiWriter 实现了流的多路复用（T-Splitter），体现了 Unix 哲学的接口设计。
    "os"            // 📌 【底层原理】系统调用封装：提供跨平台的文件系统操作，底层对应 Windows 的 CreateFileW API。
    "os/exec"       // 📌 【底层原理】执行外部命令：深度封装了 Windows 的 CreateProcessW 系统调用和输入输出匿名管道（Anonymous Pipes）处理。
    "path/filepath" // 📌 【安全攻防】路径处理：处理 \ 和 / 的跨平台差异，严格防止路径穿越攻击（Path Traversal）。
    "time"          // 📌 【性能实战】时间处理：提供单调时钟（Monotonic Clocks）支持，避免系统时间被NTP回调导致超时计算逻辑出错。
)


// 🔬 【底层原理·深度剖析】
// Windows 的进程创建底层通过调用 `CreateProcessW` API 实现。与 Linux fork()/exec() 分步执行不同，
// Windows 创建进程是原子操作。当使用 `cmd /c` 时，实际上是启动了 Windows 命令行解释器（Command Interpreter）。
//
// 💀 【踩坑血泪·反面教材】
// 如果不使用 Job Object，此函数的超时机制在面对进程树时会彻底失效。例如 `command` 为 `bat` 脚本，
// 脚本里又启动了多个 exe，当触发超时时，Go 的 `tCtx` 会向 `cmd.exe` 发送终止信号，但不会传递给 `bat` 启动的子孙进程。
// 这会导致严重的“僵尸/孤儿进程”内存泄漏问题。
//
// 🏗️ 【架构设计·模式对比】
// 正确的做法 vs 错误的做法：
// ❌ 当前做法：依赖 exec.CommandContext 的默认行为，对付单层进程可以，但对付多层嵌套的进程树极度脆弱。
// ✅ 优化方向：应当设置 `cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}`，
// 并在执行前后通过 Win32 API `CreateJobObject`、`AssignProcessToJobObject` 和 `TerminateJobObject` 进行硬核管控。
//
// ExecuteShell 在Windows系统上执行Shell命令（带超时保护）
// 参数 ctx：上下文（备用，实际用独立的超时上下文）
// 参数 command：要执行的命令字符串
// 参数 workDir：工作目录（命令在哪个目录下执行），空字符串表示当前目录
// 参数 timeoutSec：超时时间（秒），超过这个时间就强制终止
// 参数 runAs：Windows下忽略
// 返回值：ShellResult指针，包含输出、退出码、错误信息
func ExecuteShell(ctx context.Context, command string, workDir string, timeoutSec int, runAs string, taskID uint) *ShellResult {
    // 第一步：创建上下文（超时为 0 时不限时，仅响应外部 ctx 取消）
    // 📌 【大厂面试·核心考点】如果父 Context 已经被 canceled，`context.WithTimeout` 返回的子 context 会立刻处于 canceled 状态，这种级联控制是无锁且高效的。
    // ⚡ 【性能实战·生产调优】每次调用 WithTimeout 都会在 runtime 内部创建一个 timer，如果系统QPS极高，可能会产生 timer 瓶颈。不过在外部命令执行场景，瓶颈往往在于进程创建而不是 timer。
    var tCtx context.Context
    var cancel context.CancelFunc
    if timeoutSec > 0 {
        tCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
    } else {
        tCtx, cancel = context.WithCancel(ctx)
    }
    defer cancel()

    if taskID > 0 {
        RunningTaskCancels.Store(taskID, cancel)
        defer RunningTaskCancels.Delete(taskID)
    }

    // 第二步：创建命令对象
    // 在Windows上，使用 cmd.exe 来执行命令，/c 参数表示"执行完就退出"
    // 🔬 【底层原理·深度剖析】底层调用 `syscall.StartProcess`，并建立 3 个匿名管道（Anonymous Pipes）用于连接子进程的标准输入、输出和错误。
    // 🛡️ 【安全攻防·漏洞防线】这里直接拼接了 command 到 `cmd /c`，如果 command 包含了未经验证的用户输入，这就是一个典型的命令注入（Command Injection）漏洞点！
    cmd := exec.CommandContext(tCtx, "cmd", "/c", command)      // CommandContext会在后台启动一个goroutine监听tCtx.Done()，触发时向进程发送Kill信号
    if workDir != "" {                                          // 如果指定了工作目录
        cmd.Dir = workDir                                       // 设置命令的执行目录
    }

    // 第三步：准备输出缓冲区（标准输出和标准错误）
    // ⚡ 【性能实战·生产调优】如果预期输出极大（>10MB），无脑使用 bytes.Buffer 会导致内存频繁重分配（Grow()）甚至 OOM。
    // 优化手段：可以使用固定大小的 RingBuffer 或者直接写入临时文件，避免撑爆应用内存空间。
    var stdout, stderr bytes.Buffer                             // Buffer就像一个能自动扩容的字符串容器
    
    var logFile *os.File
    if taskID > 0 {
        logDir := filepath.Join("data", "logs")
        os.MkdirAll(logDir, 0755)
        logPath := filepath.Join(logDir, fmt.Sprintf("exec_%d.log", taskID))
        logFile, _ = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    }

    if logFile != nil {
        defer logFile.Close()
        // 🔬 【底层原理·深度剖析】使用 io.MultiWriter 实现类似 Linux `tee` 命令的效果，一份流向内存 buffer，一份落盘。
        // 数据从内核态的管道复制到 Go 用户态，再分别被写入两个目标的 io.Writer，非常优雅的装饰器模式应用。
        cmd.Stdout = io.MultiWriter(&stdout, logFile)
        cmd.Stderr = io.MultiWriter(&stderr, logFile)
    } else {
        cmd.Stdout = &stdout
        cmd.Stderr = &stderr
    }

    // 第四步：运行命令（阻塞等待，直到命令结束或超时）
    // 📌 【大厂面试·核心考点】cmd.Run() 和 cmd.Start() 的区别是什么？
    // 标准答案：Start() 只负责启动进程立即返回（非阻塞），而 Run() 相当于 Start() + Wait()，它会阻塞当前 goroutine，
    // 直到进程退出且所有相关的 IO 管道都被彻底关闭。 
    // 💀 【踩坑血泪】如果子进程故意不关闭它的 stdout 描述符（例如把它传递给了自己的子孙进程），哪怕当前子进程退出了，cmd.Wait() 依然会死锁阻塞！这就是为什么要强烈依赖 Context 取消机制的原因。
    err := cmd.Run()

    // 第五步：构造结果
    result := &ShellResult{
        Output: stdout.String() + stderr.String(),              // 合并标准输出和标准错误
    }

    // 第六步：分析错误（如果有的话）
    if err != nil {                                             // 命令执行出了错
        if tCtx.Err() == context.DeadlineExceeded {             // 是不是因为超时了？
            // ⚡ 【底层原理·深度剖析】当 context 超时，后台 goroutine 会立刻调用 `cmd.Process.Kill()`。
            // Windows 下 `Kill()` 的本质是调用了 Win32 API `TerminateProcess(hProcess, 1)`。
            // 这种做法极其暴力，目标进程的 DLL 将没有机会执行 DllMain 的 PROCESS_DETACH 清理逻辑，也无法正常关闭数据库连接或释放全局互斥锁（Mutex）。
            if cmd.Process != nil {                             // 如果进程对象存在
                cmd.Process.Kill()                              // 强制杀掉父进程（cmd.exe，注意：子孙进程可能会变成孤儿逃逸！）
            }
            result.Error = err                                  // 记录超时错误
            result.ExitCode = -1                                // 退出码设为-1表示被强制终止
            return result
        }
        // 🧪 【测试工程·质量保障】如何 mock 一个真正的超时场景？
        // 可以在单元测试代码里构造一个 `cmd /c "ping 127.0.0.1 -n 10 > nul"`（模拟耗时 10 秒的阻塞操作），
        // 然后设置 1 秒的超时时间，断言此处的逻辑是否正常进入了 DeadlineExceeded 判定分支。
        // 不是超时，是命令本身出错了（比如命令不存在、返回非0等）
        if exitErr, ok := err.(*exec.ExitError); ok {           // 类型断言：判断是不是"命令返回非0"的错误
            result.ExitCode = exitErr.ExitCode()                // 提取真实的退出码
        } else {
            result.ExitCode = -1                                // 其他错误，退出码设为-1
        }
        result.Error = err
        return result
    }

    // 第七步：命令执行成功
    // 🏗️ 【架构设计·模式对比】ExitCode 的跨平台通用契约约定：在 POSIX 和 Windows 体系中，0 永远代表成功。
    // 非 0 均表示不同类型的失败。Go 语言底层通过调用 `GetExitCodeProcess` API 获取到了该数字，并映射回了业务结构。
    result.ExitCode = 0                                         // 退出码为0表示一切正常
    return result
}
