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
func TestExecuteShell_StreamingAndCancel(t *testing.T) {
	// Edge Case 1: 确保 data/logs 目录被正确自动创建
	logDir := filepath.Join("data", "logs")
	os.MkdirAll(logDir, 0755)

	taskID := uint(9999)
	logFile := filepath.Join(logDir, "exec_9999.log")
	os.Remove(logFile) // 清理之前的遗留

	// 根据操作系统选择长时间运行的脚本
	var script string
	if runtime.GOOS == "windows" {
		// Windows 延迟 3 秒：每秒 ping 一次
		script = `echo STREAM_START && ping 127.0.0.1 -n 4 > nul && echo STREAM_END`
	} else {
		// Linux 延迟 3 秒
		script = `echo STREAM_START; sleep 3; echo STREAM_END`
	}

	// 异步启动脚本
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
	success := CancelExecution(taskID)
	if !success {
		t.Errorf("【测试失败】CancelExecution(9999) 返回失败，没能找到或取消对应任务")
	}

	// 等待结果
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
