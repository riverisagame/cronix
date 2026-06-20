# 架构决策记录 (ADR): 任务执行引擎底盘强化优化

## 问题背景 (Context)
在验证常驻任务执行情况时，根据最新的线上日志，暴露了三个严重且隐秘的底层执行系统架构缺陷：

1. **退避机制击穿 (Daemon Monitor Backoff Loop Hole)**
   日志显示 `restart_count=70209`。如果在极短时间内拉起常驻进程失败，`DaemonMonitor` 陷入了疯狂的退避轮询循环，由于某种逻辑漏洞（可能没有休眠或休眠失效），导致短时间内产生了巨量的拉起动作，消耗了大量 CPU，并将 systemd journald 的缓冲区打爆（`Suppressed 15462 messages`）。

2. **进程组孤儿逃逸与权限竞态 (TOCTOU Process Group Orphan Escape)**
   日志显示大量 `[Warning] setpgid(PID, PID) failed: permission denied`。
   原因：系统调用手册明确指出，如果在子进程调用 `execve` 之后，父进程再对子进程调用 `setpgid`，将会返回 `EPERM`。在目前的逻辑中，为了规避 OpenCloudOS `fapolicyd` 审计问题，我们把 `setpgid` 从 `SysProcAttr`（子进程 exec 之前执行）移到了父进程。这就引发了极其致命的“竞态条件 (Race Condition)”：一旦机器负载较低或子进程非常轻量（如 `adm_job_evaluate_MallSiteScoreSync`），子进程就会在父进程发号施令前完成 `execve`。
   后果：`setpgid` 失败导致该进程没有属于自己的独立进程组。当任务超时或被系统杀死时，我们调用的 `syscall.Kill(-pid, SIGKILL)` 会返回 `ESRCH`，无法杀死子进程及其衍生孙子进程，造成**僵尸/孤儿进程泄露**。

3. **多级回退错误遮蔽 (Multi-Level Fallback Error Masking)**
   之前的问题排查证明了目前的执行回退链（`systemd-run` -> `sudo` -> `raw`）会将前几级的致命报错完全吞没，这导致排障过程极其痛苦，使用者被最终兜底的 `raw` 执行报错误导。

## 决策 (Decision)

1. **退避逻辑重构 (Daemon Backoff Rewrite)**:
   - 移除原先仅仅通过增加计数和简单 `time.Sleep` 构成的基础逻辑。
   - 引入带有最大睡眠时长 (Cap) 且具备严格 `context.Done()` 响应退出的退避算法，避免因为某种原因跳过休眠。

2. **多层级进程生命周期绑定 (Pdeathsig & Safe Setpgid)**:
   - 彻底解决孤儿泄露：在 `SysProcAttr` 中注入 `Pdeathsig = syscall.SIGKILL`。这是内核级别的物理绑定，只要 `cronix` 主进程意外死亡，内核瞬间对所有子进程发射 `SIGKILL`，绕过任何竞态。
   - 保留父进程的 `setpgid` 但不强求成功，因为我们现在有了 `Pdeathsig` 兜底。

3. **执行错误链式聚合 (Error Aggregation)**:
   - 在 `ExecuteShell` 的降级过程中，不直接覆盖 `startErr`，而是将每一步的错误进行字符串或 `errors.Join` 拼接，抛出完整的降级崩溃链路：`[cgroups: xxx] -> [sudo: xxx] -> [raw: xxx]`。

## 影响评估 (Impact)
**实现难度**：低，均为 Go 语言层面的原子逻辑修改。
**副作用/影响范围**：修改范围仅限 `internal/scheduler/daemon_monitor.go` 的重试控制流与 `internal/executor/shell_unix.go` 的底层执行器。通过最小化原则修改，不侵入核心调度与 API，风险极低。
**性能对冲**：优化了 CPU 空转和日志打爆问题，实际上**大幅降低了性能损耗**。引入 `Pdeathsig` 后进程回收更加绝对，内存不泄露，提升长期并发安全性。
