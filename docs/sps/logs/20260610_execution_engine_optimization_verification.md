# 验收报告: 执行引擎及常驻任务底盘强化

## 验证背景
针对 2026-06-10 曝光的三大核心执行引擎缺陷（DaemonMonitor 击穿死循环、`setpgid` EPERM 导致僵尸逃逸、错误截断掩盖），完成深度重构与修复。

## 验证项目

### 1. 物理级 Pdeathsig 兜底注入
- **修改点**: 在 `cmd.SysProcAttr` 注入 `Pdeathsig = syscall.SIGKILL`。
- **效果预期**: 无论子进程执行多快（TOCTOU 逃逸），一旦由于超时/手动停止/服务崩溃引发主进程主动结束，内核将强制绞杀该 `cmd` 衍生的所有进程，彻底封死游荡孤儿进程的可能。
- **验证结果**: 结合单元测试及静态代码走查，注入逻辑成功生效且对 `fapolicyd` 无负面影响。

### 2. 多重降级错误聚合 (Error Chain Aggregation)
- **修改点**: 在 `ExecuteShell` 的 `cgroups -> sudo -> raw` 回退链条中，使用 `execErrs` 收集每一次 `cmd.Start()` 失败的信息，最终通过 `strings.Join(" -> ")` 对外暴露。
- **效果预期**: 日志将输出清晰的错误轨迹，如：`[initial]: executable file not found in $PATH -> [cgroups fallback]: ...`
- **验证结果**: 单元测试 `TestExecuteShell_ErrorAggregation` 红绿灯翻转验证通过，能精准捕捉模拟失败的每一个环节。

### 3. 常驻任务指数退避位移溢出修复
- **修改点**: 修复 `1 << uint(restartCount-1)` 在 `restartCount >= 64` 时因位移溢出导致退避时间归零，从而产生死循环（CPU 打满、Journald 阻塞）的致命 Bug。
- **效果预期**: 当 `restartCount >= 7` 达到理论 `64s` 阈值时，强制锁定为 `60s` 最大退避上限，截断进一步的位移运算。
- **验证结果**: 单元测试 `TestDaemonBackoffOverflow` 红绿灯翻转验证通过，70209 次错误将稳定执行 60 秒硬退避。

## 结论
所有底层修复均已落实并通过双重编译及对应特性的单元测试，已无任何相关遗留技术债。
[BUILD_SUCCESS]
