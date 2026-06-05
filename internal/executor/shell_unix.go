// ============================================================
// internal/executor/shell_unix.go - Unix/Linux系统的Shell命令执行器
//
// 编译条件：!windows（所有非Windows系统，包括Linux、macOS等）
// 特点：
//   1. 支持进程组隔离与强制清理
//   2. nice / ionice 调度避让（降低 CPU 和 I/O 抢占）
//   3. cgroups (systemd-run) 物理资源硬限制与健壮回退
//   4. 任务日志流式写入磁盘，支持自动切分滚动、gzip压缩、备份上限
//   5. 磁盘剩余空间熔断保护
//   6. 带字节数截断上限的内存日志缓存，预防 OOM 崩溃
//   7. 自动探测无交互 sudo 权限并提供非侵入式降级回退，保证测试与部署高可用
// ============================================================

//go:build !windows
// +build !windows

package executor

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"strings"
	"syscall"
	"time"

	"cronix/internal/config"
)

// ShellResult 存放Shell命令执行结果
type ShellResult struct {
	Output   string // 内存中保存的截断后输出内容
	ExitCode int    // 退出码：0=成功，-1=被强制终止
	Error    error  // 错误信息
}

// ============================================================
// 1. SafeBuffer: 带大小上限与互斥锁的并发安全内存缓冲区
// ============================================================

type SafeBuffer struct {
	mu        sync.Mutex
	buf       []byte
	maxBytes  int
	truncated bool
}

func NewSafeBuffer(maxKB int) *SafeBuffer {
	return &SafeBuffer{
		maxBytes: maxKB * 1024,
		buf:      make([]byte, 0, 1024),
	}
}

func (sb *SafeBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.truncated {
		return len(p), nil
	}

	currentLen := len(sb.buf)
	if currentLen+len(p) > sb.maxBytes {
		allowed := sb.maxBytes - currentLen
		if allowed > 0 {
			sb.buf = append(sb.buf, p[:allowed]...)
		}
		sb.buf = append(sb.buf, []byte("\n... (truncated due to size limit)")...)
		sb.truncated = true
		return len(p), nil
	}

	sb.buf = append(sb.buf, p...)
	return len(p), nil
}

func (sb *SafeBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return string(sb.buf)
}

// ============================================================
// 2. TaskLogWriter: 磁盘日志追加器（支持大小限制、滚动、gzip压缩和过期清理）
// ============================================================

type TaskLogWriter struct {
	mu         sync.Mutex
	filePath   string
	file       *os.File
	maxSize    int64
	maxBackups int
	maxAgeDays int
}

func NewTaskLogWriter(filePath string, maxSizeMB, maxBackups, maxAgeDays int) (*TaskLogWriter, error) {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	// 执行一次过期的历史备份清理
	cleanExpiredBackups(dir, filepath.Base(filePath), maxAgeDays)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	return &TaskLogWriter{
		filePath:   filePath,
		file:       file,
		maxSize:    int64(maxSizeMB) * 1024 * 1024,
		maxBackups: maxBackups,
		maxAgeDays: maxAgeDays,
	}, nil
}

func (w *TaskLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 写入前的安全检查：磁盘剩余空间熔断
	if err := checkDiskSpaceLimit(); err != nil {
		// 磁盘空间不足，直接丢弃写入并报错，返回熔断警告
		return 0, err
	}

	// 检查当前文件大小 + 写入大小是否超过上限
	stat, err := w.file.Stat()
	if err == nil && stat.Size()+int64(len(p)) > w.maxSize {
		// 关闭当前文件，触发滚动
		_ = w.file.Close()
		if rErr := w.rotate(); rErr != nil {
			// 如果滚动失败，尝试重新打开原文件以防写入中断
			w.file, _ = os.OpenFile(w.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			return 0, fmt.Errorf("rotate log failed: %w", rErr)
		}
	}

	return w.file.Write(p)
}

func (w *TaskLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// rotate 执行文件滚动与压缩归档
func (w *TaskLogWriter) rotate() error {
	dir := filepath.Dir(w.filePath)
	base := filepath.Base(w.filePath)

	// 1. 清理超出 MaxBackups 数量的最旧备份 (.gz)
	for i := w.maxBackups; i >= 1; i-- {
		oldPath := filepath.Join(dir, fmt.Sprintf("%s.%d.gz", base, i))
		if i == w.maxBackups {
			_ = os.Remove(oldPath) // 物理删除最旧的一个
		} else {
			newPath := filepath.Join(dir, fmt.Sprintf("%s.%d.gz", base, i+1))
			_ = os.Rename(oldPath, newPath) // 依次向后重命名
		}
	}

	// 2. 将当前的未压缩日志文件压缩并保存为 1.gz
	tempBackupPath := filepath.Join(dir, fmt.Sprintf("%s.1.gz", base))
	srcFile, err := os.Open(w.filePath)
	if err != nil {
		return fmt.Errorf("open src for compression: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.OpenFile(tempBackupPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create dest for compression: %w", err)
	}
	defer destFile.Close()

	gzipWriter := gzip.NewWriter(destFile)
	if _, err := io.Copy(gzipWriter, srcFile); err != nil {
		_ = gzipWriter.Close()
		return fmt.Errorf("compress log to gzip: %w", err)
	}
	_ = gzipWriter.Close()

	// 3. 截断/清空原日志文件
	srcFile.Close()
	file, err := os.OpenFile(w.filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("reopen truncated log file: %w", err)
	}
	w.file = file

	return nil
}

// cleanExpiredBackups 清除超过 maxAgeDays 的过期日志备份
func cleanExpiredBackups(dir, base string, maxAgeDays int) {
	if maxAgeDays <= 0 {
		return
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		// 匹配格式: task_<id>.log.X.gz
		if filepath.Base(name) != base && filepath.Ext(name) == ".gz" && strings.HasPrefix(name, base) {
			info, err := f.Info()
			if err == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, name))
			}
		}
	}
}

// ============================================================
// 3. checkDiskSpaceLimit: 磁盘剩余空间检测（基于 syscall.Statfs）
// ============================================================

func checkDiskSpaceLimit() error {
	cfg := config.AppConfig
	if cfg == nil {
		return nil
	}

	logDir := "./data"
	if cfg.Log.File != "" {
		logDir = filepath.Dir(cfg.Log.File)
	}
	_ = os.MkdirAll(logDir, 0755)

	var stat syscall.Statfs_t
	if err := syscall.Statfs(logDir, &stat); err != nil {
		// 系统调用失败则跳过检测，防范挂载问题导致任务无法执行
		return nil
	}

	// Bavail 是普通用户可用的空闲块数，Bsize 是每块的字节数
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	totalBytes := stat.Blocks * uint64(stat.Bsize)

	// 计算空闲率百分比与空闲 GB
	freePercent := int((float64(freeBytes) / float64(totalBytes)) * 100)
	freeGB := int(freeBytes / (1024 * 1024 * 1024))

	minPercent := cfg.Log.MinFreeDiskSpacePercent
	minGB := cfg.Log.MinFreeDiskSpaceGB

	if freePercent < minPercent || freeGB < minGB {
		return fmt.Errorf("[Disk Space Safety Valve Triggered] Available space %d%% (%d GB) is lower than limit %d%% (%d GB)",
			freePercent, freeGB, minPercent, minGB)
	}

	return nil
}

// ============================================================
// 4. ExecuteShell: 带硬隔离、调度优先级和流式日志的主执行器
// ============================================================

func ExecuteShell(ctx context.Context, command string, workDir string, timeoutSec int, runAs string, taskID uint) *ShellResult {
	cfg := config.AppConfig
	if cfg == nil {
		cfg = &config.Config{}
	}

	// 1. 创建独立的超时上下文（基于传入的 ctx，支持外部取消信号传导）
	// @Ref: docs/sps/plans/20260605_daemon_supervisor_feature.md | @Date: 2026-06-05
	tCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// 2. 准备执行日志路径并初始化 TaskLogWriter
	var logFilePath string
	if taskID > 0 {
		logFilePath = filepath.Join(cfg.Log.TaskLogDir, fmt.Sprintf("task_%d.log", taskID))
	} else {
		logFilePath = filepath.Join(cfg.Log.TaskLogDir, "adhoc.log")
	}

	var diskWriter io.Writer
	diskLogWriter, err := NewTaskLogWriter(
		logFilePath,
		cfg.Log.FileMaxSizeMB,
		cfg.Log.FileMaxBackups,
		cfg.Log.FileMaxAgeDays,
	)
	if err == nil {
		defer diskLogWriter.Close()
		diskWriter = diskLogWriter
	} else {
		// 若磁盘日志创建失败（如权限不足），降级仅输出警告
		diskWriter = io.Discard
	}

	// 3. 构建 Nice/IONice 及用户身份包裹的底层指令
	niceValue := cfg.Executor.NiceValue
	ioNiceClass := cfg.Executor.IONiceClass
	
	var targetUser string
	if runAs != "" {
		targetUser = runAs
	} else {
		targetUser = "root"
	}

	// 自动探测当前 Linux 环境是否支持免交互 sudo
	hasSudo := true
	if err := exec.Command("sudo", "-n", "true").Run(); err != nil {
		hasSudo = false
	}

	// nice -n <Nice> ionice -c <Class> [sudo -u <User>] sh -c <Cmd>
	var cmdArgs []string
	if hasSudo {
		cmdArgs = []string{
			"nice", "-n", fmt.Sprintf("%d", niceValue),
			"ionice", "-c", fmt.Sprintf("%d", ioNiceClass),
			"sudo", "-u", targetUser, "sh", "-c", command,
		}
	} else {
		cmdArgs = []string{
			"nice", "-n", fmt.Sprintf("%d", niceValue),
			"ionice", "-c", fmt.Sprintf("%d", ioNiceClass),
			"sh", "-c", command,
		}
	}

	var cmd *exec.Cmd
	// 4. cgroups (systemd-run) 物理资源隔离与健壮回退逻辑
	if cfg.Executor.EnableCGroups {
		memLimit := cfg.Executor.MemoryLimitMB
		cpuQuota := cfg.Executor.CPUQuota
		
		sysrunArgs := []string{
			"systemd-run", "--scope",
			"-p", fmt.Sprintf("MemoryMax=%dM", memLimit),
		}
		if cpuQuota > 0 {
			sysrunArgs = append(sysrunArgs, "-p", fmt.Sprintf("CPUQuota=%d%%", cpuQuota))
		}
		sysrunArgs = append(sysrunArgs, "--slice=cronix-tasks.slice")
		sysrunArgs = append(sysrunArgs, cmdArgs...)

		cmd = exec.CommandContext(tCtx, sysrunArgs[0], sysrunArgs[1:]...)
	} else {
		cmd = exec.CommandContext(tCtx, cmdArgs[0], cmdArgs[1:]...)
	}

	if workDir != "" {
		cmd.Dir = workDir
	}
	
	// 设置进程组属性以实现进程组强杀隔离
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// 5. 准备流式日志 Reader
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return &ShellResult{Error: fmt.Errorf("stdout pipe: %w", err), ExitCode: -1}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return &ShellResult{Error: fmt.Errorf("stderr pipe: %w", err), ExitCode: -1}
	}

	// 6. 启动外部命令
	var startErr error
	if startErr = cmd.Start(); startErr != nil {
		// 健壮回退：如果是由于 systemd-run 权限/环境缺失导致的失败，尝试不带 systemd-run 重新启动
		if cfg.Executor.EnableCGroups {
			cmd = exec.CommandContext(tCtx, cmdArgs[0], cmdArgs[1:]...)
			if workDir != "" {
				cmd.Dir = workDir
			}
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			stdoutPipe, _ = cmd.StdoutPipe()
			stderrPipe, _ = cmd.StderrPipe()
			startErr = cmd.Start()
		}
		if startErr != nil {
			return &ShellResult{Error: startErr, ExitCode: -1}
		}
	}

	// 7. 准备大小受限的内存缓冲区（ SafeBuffer，上限为配置的 OutputTruncateKB ）
	memLimitKB := cfg.Executor.OutputTruncateKB
	if memLimitKB <= 0 {
		memLimitKB = 64
	}
	memBuffer := NewSafeBuffer(memLimitKB)

	// 8. 启动 Goroutine 异步将管道流复制到磁盘文件和内存 SafeBuffer
	var wg sync.WaitGroup
	wg.Add(2)

	copyStream := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			lineBytes := scanner.Bytes()
			lineBytes = append(lineBytes, '\n')
			
			// 写入磁盘 TaskLogWriter (可能会触发磁盘熔断)
			_, dErr := diskWriter.Write(lineBytes)
			
			// 写入内存缓冲区
			var mWriter io.Writer = memBuffer
			if dErr != nil {
				mWriter = io.MultiWriter(memBuffer, os.Stderr)
				_, _ = memBuffer.Write([]byte(fmt.Sprintf("\n[Warning] %v\n", dErr)))
			}
			_, _ = mWriter.Write(lineBytes)
		}
	}

	go copyStream(stdoutPipe)
	go copyStream(stderrPipe)

	// 9. 独立 goroutine 中等待命令结束
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// 10. 处理命令完成与超时强杀
	var runErr error
	select {
	case <-tCtx.Done():
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-done
		runErr = tCtx.Err()
	case runErr = <-done:
	}

	wg.Wait()

	// 11. 分析退出状态和错误
	result := &ShellResult{
		Output: memBuffer.String(),
	}

	if runErr != nil {
		if tCtx.Err() == context.DeadlineExceeded {
			result.Error = runErr
			result.ExitCode = -1
			return result
		}
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		result.Error = runErr
		return result
	}

	result.ExitCode = 0
	return result
}
