package executor

import (
	"context"
	"sync"
)

// RunningTaskCancels 运行中任务的取消句柄映射
// key: taskID (uint), value: context.CancelFunc
var RunningTaskCancels sync.Map

// ShellResult 存放 Shell 命令执行后的结果（跨平台通用）
type ShellResult struct {
	// Output 命令的标准输出和标准错误合并内容（可能被截断）
	Output string
	// ExitCode 命令的退出码：0=成功，非0=失败，-1=异常
	ExitCode int
	// Error 执行过程中的错误（nil 表示没有错误）
	Error error
}

// CancelExecution 取消正在执行的任务（通过调用其 context.CancelFunc）
// 返回 true 表示成功取消，false 表示任务不存在或已完成
func CancelExecution(taskID uint) bool {
	if cancel, ok := RunningTaskCancels.Load(taskID); ok {
		cancel.(context.CancelFunc)()
		return true
	}
	return false
}
