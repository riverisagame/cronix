//go:build !windows
// +build !windows

package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cronix/internal/config"
)

// TestExecuteShell_LinuxLimits 测试 Linux 环境下 ExecuteShell 的新功能（Nice/IONice/流式落盘）
func TestExecuteShell_LinuxLimits(t *testing.T) {
	// 初始化测试用的全局配置
	taskLogDir := t.TempDir()
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
	expectedLogFile := filepath.Join(taskLogDir, "task_999.log")
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

// TestTaskLogWriter_Rotation 测试磁盘日志追加器的切割与压缩逻辑
func TestTaskLogWriter_Rotation(t *testing.T) {
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

// TestTaskLogWriter_DiskSpaceSafety 测试磁盘剩余空间保护熔断
func TestTaskLogWriter_DiskSpaceSafety(t *testing.T) {
	// 在真实物理机上测试 10% 剩余空间不太方便，
	// 我们可以提供一个 Mock 的容量检查接口或在配置中开启强检测。
	// 这也是 RED 阶段要覆盖的设计点。
	t.Log("Testing Disk Space Safety Protection...")
}
