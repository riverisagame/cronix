// ============================================================
// internal/executor/shell_windows.go - Windows系统的Shell命令执行器
// Windows不支持进程组概念，所以用 cmd.exe /c 来执行命令
// 超时时用 Process.Kill() 终止进程
// ============================================================
package executor

import (
    "bytes"       // 字节缓冲区：用来收集命令的输出
    "context"     // 上下文：带超时的执行控制
    "fmt"
    "io"
    "os"
    "os/exec"     // 执行外部命令：调用系统的命令行
    "path/filepath"
    "sync"
    "time"        // 时间：用于超时计算
)

var RunningTaskCancels sync.Map

// ShellResult 存放Shell命令执行后的结果
// 这个结构体在shell_windows.go和shell_unix.go中都定义了（条件编译）
type ShellResult struct {
    Output   string // 命令的输出内容（标准输出+标准错误合并）
    ExitCode int    // 命令的退出码：0=成功，非0=失败，-1=异常
    Error    error  // 执行过程中的错误（nil表示没有错误）
}

// ExecuteShell 在Windows系统上执行Shell命令（带超时保护）
// 参数 ctx：上下文（备用，实际用独立的超时上下文）
// 参数 command：要执行的命令字符串
// 参数 workDir：工作目录（命令在哪个目录下执行），空字符串表示当前目录
// 参数 timeoutSec：超时时间（秒），超过这个时间就强制终止
// 参数 runAs：Windows下忽略
// 返回值：ShellResult指针，包含输出、退出码、错误信息
func ExecuteShell(ctx context.Context, command string, workDir string, timeoutSec int, runAs string, taskID uint) *ShellResult {
    // 第一步：创建上下文（超时为 0 时不限时，仅响应外部 ctx 取消）
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
    cmd := exec.CommandContext(tCtx, "cmd", "/c", command)      // CommandContext会在线程退出时自动终止命令
    if workDir != "" {                                          // 如果指定了工作目录
        cmd.Dir = workDir                                       // 设置命令的执行目录
    }

    // 第三步：准备输出缓冲区（标准输出和标准错误）
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
        cmd.Stdout = io.MultiWriter(&stdout, logFile)
        cmd.Stderr = io.MultiWriter(&stderr, logFile)
    } else {
        cmd.Stdout = &stdout
        cmd.Stderr = &stderr
    }

    // 第四步：运行命令（阻塞等待，直到命令结束或超时）
    err := cmd.Run()

    // 第五步：构造结果
    result := &ShellResult{
        Output: stdout.String() + stderr.String(),              // 合并标准输出和标准错误
    }

    // 第六步：分析错误（如果有的话）
    if err != nil {                                             // 命令执行出了错
        if tCtx.Err() == context.DeadlineExceeded {             // 是不是因为超时了？
            if cmd.Process != nil {                             // 如果进程对象存在
                cmd.Process.Kill()                              // 强制杀掉进程
            }
            result.Error = err                                  // 记录超时错误
            result.ExitCode = -1                                // 退出码设为-1表示被强制终止
            return result
        }
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
    result.ExitCode = 0                                         // 退出码为0表示一切正常
    return result
}

// CancelExecution 尝试手动强杀指定的正在运行的执行进程
func CancelExecution(taskID uint) bool {
    if cancel, ok := RunningTaskCancels.Load(taskID); ok {
        cancel.(context.CancelFunc)()
        return true
    }
    return false
}
