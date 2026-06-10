# 执行计划: 常驻进程资源击穿及孤儿进程逃逸修复

## 目标 (Objective)
通过最小化修改，彻底根除高频重启击穿日志、子进程 `setpgid` 失败导致孤儿进程无法回收、以及多级降级报错被掩盖的严重线上事故。

## 原子任务拆解 (Atomic Tasks)

### 子任务 1：彻底根治 `DaemonMonitor` 退避循环击穿漏洞
**目标文件**：`internal/scheduler/daemon_monitor.go`
**改动逻辑**：
1. 定位到 `runDaemonLoop` 中的退避等待逻辑处 (约 261 行的 `time.Sleep` 或 `select` 块)。
2. 分析为什么 `restart_count` 会被刷到 `70209`。是因为 `backoff` 时间变量被意外重置，还是 `select` 中的定时器未生效。
3. 重写该段逻辑：
   使用强制退避公式，并确保休眠不会因为一些非 context_cancel 的情况被跳过。
   添加针对“极短时间（秒级内）连续失败”的短路惩罚机制。

### 子任务 2：多级降级执行报错链路聚合
**目标文件**：`internal/executor/shell_unix.go`
**改动逻辑**：
1. 定位 `ExecuteShell` 中的多级降级块 (约 500 行的 `startErr = cmd.Start()`)。
2. 引入一个新的切片 `var execErrs []string` 用于收罗每次降级的失败原因。
3. 当第一层 `cgroups` (systemd-run) 失败时，不直接用新的 `startErr` 覆盖，而是先：
   `execErrs = append(execErrs, fmt.Sprintf("[cgroups]: %v", startErr))`
4. 同理，第二层和最后一层失败时依次加入：
   `execErrs = append(execErrs, fmt.Sprintf("[raw]: %v", startErr))`
5. 最终如果全失败，返回：`fmt.Errorf("execution failed across all fallbacks: %s", strings.Join(execErrs, " -> "))`

### 子任务 3：引入物理级 Pdeathsig 兜底，防止孙子进程逃逸
**目标文件**：`internal/executor/shell_unix.go`
**改动逻辑**：
1. 定位到构建 `SysProcAttr` 的地方。
2. 无论走哪个降级分支，统一为 `cmd.SysProcAttr` 注入内核死亡信号：
   ```go
   cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
   ```
   **注意**: 若原本 `SysProcAttr` 已经创建（例如设了 `Credential`），在其上附加即可。

## 出口准则
- 必须进行严格的本地代码静态分析与构建测试（由于目前我们在 Windows 环境测试 Linux 功能，需依赖 `wsl` 编译验证语法的跨平台通过性）。
- 代码通过 `go build` 后，才可提交完成状态。
