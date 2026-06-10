# 纳米级计划：Shell Job Control 完美隔离方案 (2026-06-10)

## 目标
彻底解决子进程 `setpgid` 竞态导致的 `EPERM` 警告日志污染问题，同时完美修复由于进程组设置失败引发的任务超时无法强杀（孙子进程逃逸）的隐患。

## 现状分析与痛点
1. 当前在 `internal/executor/shell_unix.go` 中，父进程尝试对刚启动的子进程调用 `syscall.Setpgid(pid, pid)`。由于子进程启动极快，通常已经完成了 `execve`，导致该调用返回 `EPERM` 并打印警告。
2. 由于 `setpgid` 失败，子进程并未创建独立的进程组。在任务超时或取消时，代码执行 `syscall.Kill(-pid, SIGKILL)` 试图杀死整个进程组，但这会因为找不到对应的 PGID 而返回 `ESRCH`。
3. 结果是：不仅产生大量日志噪音，而且超时控制**实质上已经失效**（只有 `Pdeathsig` 在主程序崩溃时能兜底杀死第一层子进程，日常的超时取消无法杀死子进程及孙子进程）。

## 改造方案 (方案 B)
我们将放弃在 Go 语言层面的并发强杀，转而利用 Shell 原生的 Job Control（作业控制）机制来实现完美的进程树生命周期管理。

### 1. 修改执行脚本封装 (Command Wrapper)
在 `internal/executor/shell_unix.go` 中，将用户原本的 `command` 包装在带有 `set -m` 的 Bash 上下文中。
```bash
set -m
(
  # 用户原始脚本
) &
child=$!
trap 'kill -9 -$child 2>/dev/null; exit 143' TERM
wait $child
exit $?
```
**原理：**
- `set -m` 开启了作业控制，使得后台运行的 `( ... ) &` 会被 Bash 自动分配到一个**全新的独立进程组**中，且过程是原生的，不会触发 OpenCloudOS 的 `fapolicyd`。
- `$child` 既是子 Shell 的 PID，也是其全新的 PGID。
- `trap` 捕获 `SIGTERM` 信号。当 Go 调度器需要终止任务时，只需向 Bash 发送 `SIGTERM`，Bash 就会把 `SIGKILL` 广播给 `$child` 代表的整个底层进程树。

### 2. 移除父进程的无用 `Setpgid`
在 `internal/executor/shell_unix.go` 的 `if cmd.Process != nil` 块中，删除以下无用代码：
```go
if err := syscall.Setpgid(pid, pid); err != nil {
    fmt.Fprintf(os.Stderr, "[Warning] setpgid(%d,%d) failed: %v\n", pid, pid, err)
}
```

### 3. 修改超时取消逻辑
在 `tCtx.Done()` 分支中，将原先错误的进程组强杀，修改为优雅的单体发送：
```go
// 旧代码:
// _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)

// 新代码:
// 发送 SIGTERM 触发 Bash 的 Trap 清理孙子进程树
_ = syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
// 兜底机制：1 秒后直接硬杀 bash 本身防止假死
go func(p *os.Process) {
    time.Sleep(1 * time.Second)
    _ = p.Kill()
}(cmd.Process)
```

## 影响面评估
- 变更影响范围：仅限 Unix/Linux 环境下的 Shell 任务执行层。
- 兼容性：`bash -m` 和 `trap` 均属于 POSIX / Bash 核心标准，跨发行版表现极度稳定。
- 前置依赖：无任何额外依赖。

请 Review。若无误，请回复“继续”开始执行。
