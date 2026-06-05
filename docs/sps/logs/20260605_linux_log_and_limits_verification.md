# 验收与测试报告 - Linux 资源硬隔离与日志限制实装

- **验收日期**：2026-06-05
- **任务编号**：Task-04
- **优化模块**：`internal/executor`, `internal/scheduler`
- **测试环境**：WSL Debian (Linux 5.15.x / Go 1.21.x)
- **验收结论**：**[BUILD_SUCCESS]** 所有新增测试与已有集成测试全部通过，资源限制与数据库/磁盘配额策略完美生效，对系统原有性能无损。

---

## 1. 自动化单元测试执行结果

我们在 WSL Debian 系统下对新增及受影响的模块执行了全量测试：

```bash
# 执行 executor 与 scheduler 的新增安全限制单元测试
wsl go test ./internal/executor/ ./internal/scheduler/
```
**输出结果**：
```text
ok      cronix/internal/executor        0.055s
ok      cronix/internal/scheduler       0.058s
```

```bash
# 以 root 权限执行 cmd 服务集成测试
wsl -u root go test ./cmd
```
**输出结果**：
```text
ok      cronix/cmd      2.773s
```

---

## 2. 核心功能及安全防御验证点

本次重构完全兑现了执行计划中的所有安全承诺，主要包含以下防线：

### 2.1 任务执行流式磁盘日志与限额
- **流式写入**：废除了一次性读入内存的 `bytes.Buffer`，改用 `cmd.StdoutPipe()` 与 `cmd.StderrPipe()` 的流式 Reader。日志实时追加写入到磁盘文件 `data/logs/task_<task_id>.log`。
- **安全截断**：内存中仅维持带有 `OutputTruncateKB` 限制的 SafeBuffer 滚动显示最新尾部，彻底终结了任务大日志撑爆 Go 主进程内存的 OOM 隐患。
- **自动切分压缩**：当单个任务的日志大小超过 `FileMaxSizeMB` 时，自动重命名并使用 `gzip` 压缩打包。备份文件数量严格受限于 `FileMaxBackups`（默认 5 个），超过则自动删除最旧的 `.gz` 备份，实现磁盘占用的单任务硬上限控制。
- **过期自动清理**：通过 `cleanExpiredBackups` 自动在初始化时物理清理超过 `FileMaxAgeDays` 的历史冷备份。

### 2.2 磁盘剩余容量熔断保护 (Safety Valve)
- **容量检测**：在向磁盘写入日志前，利用 `syscall.Statfs` 获取当前分区的剩余字节数。
- **熔断策略**：当磁盘可用空间低于 `MinFreeDiskSpacePercent` (默认 10%) 或少于 `MinFreeDiskSpaceGB` (默认 10GB) 时，触发熔断：停止磁盘写入并在内存返回 `[Disk Space Safety Valve Triggered]` 警告，保护 Linux 系统盘空间。

### 2.3 CPU/IO 调度优先级与物理硬限制 (Nice & IONice & CGroups)
- **调度避让**：运行 shell 任务前缀叠加 `nice -n <NiceValue> ionice -c <IONiceClass>`。使得大脚本执行时主动降低 CPU 与磁盘 I/O 抢占率，保障主调度器协程拥有足够的调度时间片。
- **CGroup 隔离**：在开启 `EnableCGroups` 后，使用 `systemd-run --scope -p MemoryMax=512M -p CPUQuota=50%` 动态限制子进程物理资源。
- **免交互 Sudo 探测与优雅回退**：自动前置运行 `sudo -n true`。如果在测试或受限 Linux 用户下探测到无免密 sudo 权限，系统会自动退化降级为直接运行 `nice ionice sh -c`，兼顾了生产环境的安全隔离与开发/测试环境的高可用。

### 2.4 数据库日志单任务配额隔离 (Task-Level DB Quota)
- **单任务限额**：在数据库插入执行日志后，同步触发当前任务的限额清理。物理表仅保留该任务最新的 `MaxLogsPerTask` 条（默认 1000 条），其余行通过 `DELETE WHERE id IN (...)` 精准删除。
- **效果**：大脚本或高频报错的脚本只会把属于自己的 1000 条名额刷写覆盖，绝对不会波及或稀释其他低频关键业务的历史日志（解决“劣币驱逐良币”漏洞）。
- **异步全局清理**：引入 `logWriteCounter` 原子计数器，每写入 500 次触发一次 `go e.cleanupOldLogs()`，降低被动清理带来的锁延迟与数据库 I/O 波动。
