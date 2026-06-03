package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestGracefulShutdownIntegration 测试优雅退出的集成表现
// 该测试通过启动子进程，发送 SIGINT 信号，并检查日志输出中是否包含优雅关闭物理资源的关键字。
// 规则：测试不修改任何物理持久化表，只读/内存操作。
func TestGracefulShutdownIntegration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping graceful shutdown integration test on Windows because SIGINT is not supported by os.Process.Signal")
	}
	// 1. 准备测试用的临时工作目录和配置文件
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config_test.yaml")
	dbPath := filepath.Join(tempDir, "test.db")

	configContent := `
server:
  host: "127.0.0.1"
  port: 28888
  api:
    enabled: true
  webui:
    enabled: false
  tls:
    enabled: false

database:
  path: "` + filepath.ToSlash(dbPath) + `"

auth:
  password: "testpassword"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config_test.yaml: %v", err)
	}

	// 2. 编译当前的最新代码生成临时二进制文件，以便子进程执行
	binPath := filepath.Join(tempDir, "cronix-test-bin")
	if os.PathSeparator == '\\' {
		binPath += ".exe"
	}

	// 找到 main.go 所在的根目录进行编译
	// 我们的测试在 cmd 目录运行，所以 main.go 在父目录
	buildCmd := exec.Command("go", "build", "-buildvcs=false", "-o", binPath, "../main.go")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build cronix-test-bin: %v\nOutput: %s", err, string(out))
	}
	defer os.Remove(binPath)

	// 3. 启动子进程，运行 serve 命令
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "serve", "-c", configPath)
	
	// 捕获 stdout/stderr 用于验证日志
	var outputBuf bytes.Buffer
	cmd.Stdout = &outputBuf
	cmd.Stderr = &outputBuf

	// 设置 SysProcAttr 以便在 Linux 上安全地发送信号
	// 在 Windows 上 os.Process.Signal(os.Interrupt) 会发送 CTRL_BREAK_EVENT
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start child process: %v", err)
	}

	// 4. 等待服务启动（监听日志中的关键字，例如 "服务器正在启动..."）
	startTimeout := time.After(3 * time.Second)
	started := false
	for !started {
		select {
		case <-startTimeout:
			t.Fatalf("Timeout waiting for server to start. Output: %s", outputBuf.String())
		default:
			if strings.Contains(outputBuf.String(), "服务器正在启动...") {
				started = true
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// 5. 向子进程发送 SIGINT 信号
	// 触发优雅关闭流程
	time.Sleep(500 * time.Millisecond) // 确保服务完全就绪
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("Failed to send SIGINT signal: %v", err)
	}

	// 6. 等待进程结束
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for process to exit. Output: %s", outputBuf.String())
	case err := <-done:
		if err != nil {
			// 如果退出码是非 0，且不是因为被 kill 掉，则可能是报错
			t.Logf("Process exited with error (expected normal exit): %v", err)
		}
	}

	output := outputBuf.String()
	t.Logf("Subprocess Output:\n%s", output)

	// 7. 验证逻辑：检查日志中是否有优雅退出的关键输出
	// 预期检查的日志：
	//  - "正在优雅关闭服务器..."
	//  - "HTTP 服务器已安全关闭"
	//  - "数据库连接已安全关闭"
	expectedKeywords := []string{
		"正在优雅关闭服务器...",
		"HTTP 服务器已安全关闭",
		"数据库连接已安全关闭",
	}

	missing := false
	for _, kw := range expectedKeywords {
		if !strings.Contains(output, kw) {
			t.Errorf("Missing expected graceful shutdown log keyword: %q", kw)
			missing = true
		}
	}

	if missing {
		t.Fail()
	}
}
