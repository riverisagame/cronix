# Linux 资源硬隔离与日志硬限额纳米级执行计划

本计划旨在实装 Linux 下大脚本执行的安全防御机制，包括 CPU/Memory (Nice/IONice/CGroups) 资源隔离、大日志流式落盘与自动滚动、磁盘空闲空间保障以及数据库日志 Task-level 双重限制配额。

## 1. 变更文件与受影响范围

| 文件路径 | 变更类型 | 影响范围 | 预计代码行数 |
| --- | --- | --- | --- |
| `config.yaml` | [MODIFY] | 增加 CPU 限制、Nice、CGroup 开关、单任务日志数、磁盘保障等配置项 | ~15 行 |
| `internal/config/config.go` | [MODIFY] | 将新增的配置项解析到 Config 结构体并设定安全默认值 | ~45 行 |
| `internal/executor/shell_unix.go` | [MODIFY] | 实现流式日志、磁盘滚动分割、CGroup (systemd-run) / Nice / IONice 调度限制、磁盘剩余空间检测 | ~180 行 |
| `internal/scheduler/executor.go` | [MODIFY] | 调用更新后的 ExecuteShell 签名；在日志持久化后同步执行单任务配额清理；用原子计数器异步触发全局清理 | ~50 行 |
| `internal/executor/shell_windows.go` | [MODIFY] | 兼容签名改动（增加 taskID 参数，内部忽略即可） | ~5 行 |

---

## 2. 纳米级执行步骤

每个子任务代码变动严格控制在 10-30 行以内。

### 阶段一：[RED 阶段] 编写失败的单元测试 (TDD)
- **[T1.1]** 在 `internal/executor/shell_unix_test.go` 中新增测试用例：
  - 测试 CGroup 与 Nice 调度限制逻辑（模拟其包装效果）。
  - 测试流式追加日志文件滚动和压缩备份。
  - 测试磁盘空间监测熔断机制。
- **[T1.2]** 在 `internal/scheduler/executor_quota_test.go` 中新增测试用例：
  - 测试 Task-level 单任务日志限额：确保插入 1005 条日志后，物理表中该任务日志只剩下 1000 条，而其他任务日志不受影响。

### 阶段二：[GREEN 阶段] 最小化实现

#### 第一步：配置项定义与装配
- **[S2.1]** 更改 `config.yaml`：
  - 增加 `executor.cpu_quota` (默认 50)
  - 增加 `executor.enable_cgroups` (默认 false)
  - 增加 `executor.nice_value` (默认 19)
  - 增加 `executor.ionice_class` (默认 3)
  - 增加 `log.max_logs_per_task` (默认 1000)
  - 增加 `log.file_max_size_mb` (默认 50)
  - 增加 `log.file_max_backups` (默认 5)
  - 增加 `log.file_max_age_days` (默认 30)
  - 增加 `log.min_free_disk_space_percent` (默认 10)
  - 增加 `log.min_free_disk_space_gb` (默认 10)
- **[S2.2]** 更改 `internal/config/config.go`：
  - 在 `ExecutorConfig` 和 `LogConfig` 中定义上述结构体字段。
  - 在 `Load()` 中添加上述字段的安全兜底默认值。
  - 在 `Validate()` 中对 Nice 值 (-20 到 19)、IONiceClass (0 到 3) 范围进行验证。

#### 第二步：流式日志与磁盘滚动追加器实现
- **[S2.3]** 更改 `internal/executor/shell_unix.go`：
  - 定义 `TaskLogWriter` 结构体：实现 `io.Writer` 接口，内部实现文件大小检测（`FileMaxSizeMB`）、自动重命名归档（`.gz`）、删除超出 `MaxBackups` 数量的最旧备份（`.gz`）。
  - 使用 `compress/gzip` 压缩历史日志备份，每天定时（或触发时）扫描并删除超过 `MaxAgeDays` 的过期备份。
  - 定义 `Statfs` 辅助函数检测存储分区的空闲容量。如果空闲率低于 `MinFreeDiskSpacePercent` 或空闲 GB 小于限制，`TaskLogWriter` 拦截写入并仅向内存 Ring Buffer 返回空闲熔断提示。

#### 第三步：CGroup 隔离与 Nice/IONice 调度限制
- **[S2.4]** 更改 `internal/executor/shell_unix.go` 中的 `ExecuteShell` 逻辑：
  - 扩展签名以接收 `taskID uint`。
  - 构建命令：当 `EnableCGroups` 开启时，尝试拼接 `systemd-run --scope -p MemoryMax=512M -p CPUQuota=50% --slice=cronix-tasks.slice ...`。
  - 运行命令前加入 `nice -n 19 ionice -c 3` 前缀，如果 `systemd-run` 失败，降级退化到纯 `nice` 命令包裹。
  - 改用 `cmd.StdoutPipe()` / `cmd.StderrPipe()`，将 Reader 流式读取，分流写入磁盘 `TaskLogWriter` 和带截断上限的内存 `SafeBuffer`。
- **[S2.5]** 更改 `internal/executor/shell_windows.go`：
  - 兼容 `ExecuteShell` 的新签名（加上 `taskID uint` 参数，内部不作处理）。

#### 第四步：调度执行器的对接与数据库限额实装
- **[S2.6]** 更改 `internal/scheduler/executor.go`：
  - 更新调用 `executor.ExecuteShell` 的位置，将 `task.ID` 作为最后一个参数传入。
  - 引入一个原子写入计数器 `logWriteCounter uint64`。
  - 在保存日志 `e.db.Save(execLog)` 后，如果 `log.max_logs_per_task > 0`，则对当前 `task_id` 执行局部截断：
    `DELETE FROM execution_logs WHERE task_id = ? AND id NOT IN (SELECT id FROM (SELECT id FROM execution_logs WHERE task_id = ? ORDER BY id DESC LIMIT ?) as tmp)` （使用子查询兼容 SQLite/MySQL 语法限制）。
  - 累加 `logWriteCounter`，如果达到 500 的倍数，异步启动 `go e.cleanupOldLogs()` 进行全局日志被动限制清理。

---

## 3. 验证方案

1. **编译验证**：在 WSL Debian/Linux 下执行 `go build -o cronix .`，确保编译无误。
2. **单元测试验证**：
   - 运行新编写的 `shell_unix_test.go` 和 `executor_quota_test.go`。
3. **磁盘熔断手动验证**：
   - 临时将 `MinFreeDiskSpacePercent` 调大至 99%，执行一个 shell 任务，验证是否能成功触发“磁盘空间空闲比例熔断”且不致主系统崩溃，结束后将配置还原。
4. **日志滚动测试**：
   - 将 `FileMaxSizeMB` 设为 1（1MB），启动脚本产生大日志，检查 `data/logs/` 下是否生成对应的 `.gz` 压缩文件，并维持最多备份数限额。
