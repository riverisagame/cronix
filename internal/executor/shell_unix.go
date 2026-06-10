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
	"strings"
	"sync"
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	// 执行一次过期的历史备份清理
	cleanExpiredBackups(dir, filepath.Base(filePath), maxAgeDays)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
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
			w.file, _ = os.OpenFile(w.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
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

	destFile, err := os.OpenFile(tempBackupPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
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
	file, err := os.OpenFile(w.filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
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
	_ = os.MkdirAll(logDir, 0o755)

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
// 3b. setIONice: 用 syscall 设置进程 I/O 调度优先级
//     不依赖外部 ionice 命令，兼容 Alpine/Debian slim 等精简环境
// ============================================================

// I/O 优先级相关的常量（定义在 linux/ioprio.h）
const (
	IOPRIO_CLASS_RT    = 1 // Realtime
	IOPRIO_CLASS_BE    = 2 // Best-effort
	IOPRIO_CLASS_IDLE  = 3 // Idle
	IOPRIO_WHO_PROCESS = 1
)

// ioprio_set 系统调用号
// 在 x86_64 上是 251，在 arm64 上是 30
// 使用 syscall.SYS_IOPRIO_SET 使其跨架构
func setIONice(pid int, class int) error {
	if class <= 0 || class > 3 {
		return nil // class=0 表示不设置
	}

	// 计算 ioprio 值：
	// 高 3 位是 class，低 13 位是 priority level（优先级数据，0 最高）
	// ioprio = (class << 13) | priority
	// 使用 class 的最低 3 位和 priority=0（最高优先级）
	classBits := (class & 0x7) << 13

	_, _, err := syscall.Syscall(syscall.SYS_IOPRIO_SET, IOPRIO_WHO_PROCESS, uintptr(pid), uintptr(classBits))
	if err != 0 && err != syscall.EINVAL {
		return err
	}
	return nil
}

// ============================================================
// 4a. getShellPath / probeShell: 探测系统中真实可用的 shell 路径
//
//	os.Stat 判断文件存在性不够——在 OpenCloudOS 等环境上，
//	seccomp/fapolicyd 等安全框架可能基于调用上下文选择性拦截 execve，
//	导致 stat 通过但 exec 失败（返回 ENOENT 作为欺骗）。
//	因此实际执行 `sh -c true` 来验证 execve 链路完整可用。
//	每次 ExecuteShell 都重新探测，避免安全策略异步加载导致的缓存过时。
//
// ============================================================
var (
	cachedShellPath string
	shellPathOnce   sync.Once
)

// getShellPath 探测系统中当前可用的 shell 路径并缓存。
// 由于探测过程涉及大量 fork+exec+wait，如果在紧凑循环（Tight Loop）中频繁调用，
// 极易导致文件描述符或进程数耗尽，进而触发 `fork/exec: resource temporarily unavailable` 错误。
// 此前为了防御 OpenCloudOS 安全策略异步加载而放弃了缓存，
// 但在遇到快速退出的 daemon 任务时，这种激进探测会放大系统的资源压力导致级联崩溃。
// 现在恢复全局缓存，对于安全框架异步加载导致权限变更的边角场景，由任务本身的失败退避机制来处理。
func getShellPath() string {
	shellPathOnce.Do(func() {
		cachedShellPath = probeShell()
	})
	return cachedShellPath
}

// probeShell 遍历候选 shell 路径，使用 Stat 快速验证以避免在受限环境（如 fapolicyd）被拦截导致误判
func probeShell() string {
	candidates := []string{
		"/bin/bash",
		"/usr/bin/bash",
		"/bin/sh",
		"/usr/bin/sh",
		"/bin/dash",
		"/usr/bin/dash",
	}
	for _, p := range candidates {
		// 先做快速 stat 过滤掉明显不存在的路径
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() || fi.Mode()&0o111 == 0 {
			continue
		}
		// 只要存在且有执行权限，就直接返回该路径。
		// 在 OpenCloudOS 等带有安全限制的环境中，执行 echo/true 可能会失败。
		return p
	}
	// 兜底：让 Go 的 PATH 查找决定
	if path, err := exec.LookPath("sh"); err == nil {
		return path
	}
	return "/bin/sh"
}

// 4. ExecuteShell: 带硬隔离、调度优先级和流式日志的主执行器
// ============================================================

func ExecuteShell(ctx context.Context, command string, workDir string, timeoutSec int, runAs string, taskID uint) *ShellResult {
	cfg := config.AppConfig
	if cfg == nil {
		cfg = &config.Config{}
	}

	// 1. 创建独立的超时/取消上下文
	//    Daemon 任务可能 timeoutSec=0（常驻进程不设超时），
	//    context.WithTimeout(ctx, 0) 会立即过期导致 cmd.Start() 返回 deadline exceeded。
	//    因此 timeoutSec<=0 时改用 WithCancel（仅响应外部取消，无超时）。
	// @Ref: docs/sps/plans/20260605_daemon_supervisor_feature.md | @Date: 2026-06-05
	var tCtx context.Context
	var cancel context.CancelFunc
	if timeoutSec > 0 {
		tCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	} else {
		tCtx, cancel = context.WithCancel(ctx)
	}
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

	// 判断当前进程身份，决定是否需要 sudo 切换用户
	// root 执行自己 → 直接 sh，无需 sudo（避免 fork/exec /usr/bin/sudo 风险）
	// root 切换到其他用户 → 用 sudo -u 切换
	// 非 root 用户 → 探测 sudo 可用性后决定

	var cmdArgs []string
	currentUserIsRoot := os.Geteuid() == 0
	shellPath := getShellPath()

	if currentUserIsRoot && targetUser == "root" {
		// 场景 A：root 执行自己的任务，无需 sudo，直接用检测到的 shell
		cmdArgs = []string{shellPath}
	} else if currentUserIsRoot {
		// 场景 B：root 需要切换到其他用户身份执行任务
		cmdArgs = []string{"sudo", "-u", targetUser, shellPath}
	} else {
		// 场景 C：非 root 用户，探测 sudo 可用性
		hasSudo := true
		if err := exec.Command("sudo", "-n", "true").Run(); err != nil {
			hasSudo = false
		}
		if hasSudo {
			cmdArgs = []string{"sudo", "-u", targetUser, shellPath}
		} else {
			cmdArgs = []string{shellPath}
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

		cmd = exec.Command(sysrunArgs[0], sysrunArgs[1:]...)
	} else {
		cmd = exec.Command(cmdArgs[0], cmdArgs[1:]...)
	}

	if workDir != "" {
		// 手动验证工作目录是否存在，防止在 Go 1.18 及以下版本中由于目录不存在
		// 导致产生非常迷惑的 "fork/exec /bin/sh: no such file or directory" 报错。
		if fi, err := os.Stat(workDir); err != nil || !fi.IsDir() {
			return &ShellResult{
				Error:    fmt.Errorf("working directory invalid: %s (error: %v)", workDir, err),
				ExitCode: -1,
			}
		}
		cmd.Dir = workDir
	}

	cmd.Stdin = strings.NewReader(command)

	// 进程组设置在 exec 之后由父进程调用 syscall.Setpgid 完成，
	// 不在 SysProcAttr 中设置 Setpgid=true，避免子进程在 execve 之前
	// 调用 setpgid(0,0) 改变进程上下文，导致 OpenCloudOS 的
	// fapolicyd/seccomp 基于新进程组应用不同规则拦截 execve。
	// 加入 Pdeathsig = syscall.SIGKILL，确保父进程意外死亡时子进程一并被内核收割。
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
	}

	// 5. 准备流式日志 Reader
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return &ShellResult{Error: fmt.Errorf("stdout pipe: %w", err), ExitCode: -1}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return &ShellResult{Error: fmt.Errorf("stderr pipe: %w", err), ExitCode: -1}
	}

	// 6. 启动外部命令（支持双层降级回退：cgroups -> sudo）
	var startErr error
	var execErrs []string
	if startErr = cmd.Start(); startErr != nil {
		execErrs = append(execErrs, fmt.Sprintf("[initial]: %v", startErr))
		// --- 第一层降级：cgroups 启动失败 → 去掉 systemd-run 再试 ---
		if cfg.Executor.EnableCGroups {
			cmd = exec.Command(cmdArgs[0], cmdArgs[1:]...)
			if workDir != "" {
				cmd.Dir = workDir
			}
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Pdeathsig: syscall.SIGKILL,
			}
			cmd.Stdin = strings.NewReader(command)
			stdoutPipe, err = cmd.StdoutPipe()
			if err != nil {
				return &ShellResult{Error: fmt.Errorf("fallback cgroup stdout pipe: %w", err), ExitCode: -1}
			}
			stderrPipe, err = cmd.StderrPipe()
			if err != nil {
				return &ShellResult{Error: fmt.Errorf("fallback cgroup stderr pipe: %w", err), ExitCode: -1}
			}
			startErr = cmd.Start()
			if startErr != nil {
				execErrs = append(execErrs, fmt.Sprintf("[cgroups fallback]: %v", startErr))
			}
		}
		// --- 第二层降级：sudo 启动失败 → 降级到检测到的 shell（不用 sudo -u） ---
		if startErr != nil && len(cmdArgs) > 0 && cmdArgs[0] == "sudo" {
			cmdArgs = []string{shellPath}
			cmd = exec.Command(shellPath)
			if workDir != "" {
				cmd.Dir = workDir
			}
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Pdeathsig: syscall.SIGKILL,
			}
			cmd.Stdin = strings.NewReader(command)
			stdoutPipe, err = cmd.StdoutPipe()
			if err != nil {
				return &ShellResult{Error: fmt.Errorf("fallback sudo stdout pipe: %w", err), ExitCode: -1}
			}
			stderrPipe, err = cmd.StderrPipe()
			if err != nil {
				return &ShellResult{Error: fmt.Errorf("fallback sudo stderr pipe: %w", err), ExitCode: -1}
			}
			startErr = cmd.Start()
			if startErr != nil {
				execErrs = append(execErrs, fmt.Sprintf("[sudo fallback]: %v", startErr))
			}
		}
		// 所有降级均失败，返回最终错误
		if startErr != nil {
			return &ShellResult{Error: fmt.Errorf("fallback error chain: %s", strings.Join(execErrs, " -> ")), ExitCode: -1}
		}
	}

	// 6b. 进程启动后，在父进程中设置进程组、CPU优先级和I/O优先级
	//     不在 SysProcAttr 中设 Setpgid 是为了避免子进程在 execve 之前
	//     调用 setpgid 改变安全上下文（OpenCloudOS fapolicyd 问题）。
	if cmd.Process != nil {
		pid := cmd.Process.Pid

		// 父进程设置子进程的进程组（替代 SysProcAttr.Setpgid）
		// 让子进程归入独立进程组，方便后续 Kill(-pid) 整体强杀
		if err := syscall.Setpgid(pid, pid); err != nil {
			// 非致命：设置失败仅警告，不影响任务执行
			fmt.Fprintf(os.Stderr, "[Warning] setpgid(%d,%d) failed: %v\n", pid, pid, err)
		}

		// 设置 CPU 调度优先级（nice 值）
		// syscall.Setpriority(which, who, niceval) 的 niceval 范围 -20~19
		if err := syscall.Setpriority(syscall.PRIO_PROCESS, pid, niceValue); err != nil {
			// 非致命：nice 设置失败时仅警告，不阻断执行
			fmt.Fprintf(os.Stderr, "[Warning] setpriority(PRIO_PROCESS,%d,%d) failed: %v\n", pid, niceValue, err)
		}

		// 设置 I/O 调度优先级（ionice）
		if err := setIONice(pid, ioNiceClass); err != nil {
			// 非致命：ionice 设置失败时仅警告，不阻断执行
			fmt.Fprintf(os.Stderr, "[Warning] set ionice(pid=%d,class=%d) failed: %v\n", pid, ioNiceClass, err)
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
