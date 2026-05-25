// ============================================================
// internal/executor/shell_unix.go - Unix/Linux系统的Shell命令执行器
//
// 编译条件：!windows（所有非Windows系统，包括Linux、macOS等）
// 特点：支持进程组隔离——可以一次性杀掉主进程和它启动的所有子进程
// ============================================================

//go:build !windows
// +build !windows

package executor

import (
    "bytes"       // 字节缓冲区
    "context"     // 上下文
    "os/exec"     // 执行外部命令
    "syscall"     // 系统调用：用于发送信号、设置进程组
    "time"        // 时间处理
)

// ShellResult 存放Shell命令执行结果
type ShellResult struct {
    Output   string // 命令的输出内容
    ExitCode int    // 退出码：0=成功，-1=被强制终止
    Error    error  // 错误信息
}

// ExecuteShell 在Unix/Linux系统上执行Shell命令（带超时和进程组管理）
// 参数 ctx：上下文（备用）
// 参数 command：要执行的命令
// 参数 workDir：工作目录
// 参数 timeoutSec：超时秒数
// 参数 runAs：以哪个用户执行，空串表示当前用户
// 返回值：ShellResult指针
func ExecuteShell(ctx context.Context, command string, workDir string, timeoutSec int, runAs string) *ShellResult {
    // 第一步：创建独立的超时上下文
    // 使用 context.Background() 而不是传入的ctx，是为了让超时独立管理
    tCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
    defer cancel()                                              // 函数结束时取消上下文

    // 第二步：创建命令对象
    var cmd *exec.Cmd
    if runAs != "" {
        // 以指定用户身份执行：sudo -u <user> sh -c "command"
        cmd = exec.CommandContext(tCtx, "sudo", "-u", runAs, "sh", "-c", command)
    } else {
        cmd = exec.CommandContext(tCtx, "sh", "-c", command)
    }
    if workDir != "" {
        cmd.Dir = workDir                                       // 设置工作目录
    }
    // 设置进程组属性：Setpgid=true 表示创建新的进程组
    // 进程组的好处：可以一次性杀掉这个组里的所有进程（包括子进程的子进程）
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

    // 第三步：准备输出缓冲区
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    // 第四步：启动命令（非阻塞，Start会立即返回）
    // 这里用 Start + Wait 的方式，而不是 Run，是为了方便处理超时
    if err := cmd.Start(); err != nil {                          // 启动失败
        return &ShellResult{Error: err, ExitCode: -1}
    }

    // 第五步：在一个单独的goroutine中等待命令结束
    done := make(chan error, 1)                                  // 创建容量为1的管道
    go func() {                                                  // 启动新goroutine
        done <- cmd.Wait()                                       // Wait阻塞直到命令结束，结果发到管道
    }()

    // 第六步：等待命令结束或者超时
    var runErr error
    select {                                                     // select：同时等待多个管道
    case <-tCtx.Done():                                          // 超时了！
        if cmd.Process != nil {                                  // 进程还存在
            // 关键操作：杀掉整个进程组
            // 负的PID表示"这个进程组的所有进程"（Unix惯例）
            syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)     // SIGKILL=强制终止信号（-9）
        }
        <-done                                                   // 等待Wait()完成（确保进程资源被回收）
        runErr = tCtx.Err()                                      // 记录超时错误
    case runErr = <-done:                                        // 命令正常结束了
        // 正常完成，runErr接收Wait的结果
    }

    // 第七步：构造结果
    result := &ShellResult{
        Output: stdout.String() + stderr.String(),
    }

    // 第八步：分析错误
    if runErr != nil {
        if tCtx.Err() == context.DeadlineExceeded {              // 超时错误
            result.Error = runErr
            result.ExitCode = -1
            return result
        }
        if exitErr, ok := runErr.(*exec.ExitError); ok {         // 命令执行错误（非0退出码）
            result.ExitCode = exitErr.ExitCode()
        } else {
            result.ExitCode = -1
        }
        result.Error = runErr
        return result
    }

    // 第九步：成功
    result.ExitCode = 0
    return result
}
