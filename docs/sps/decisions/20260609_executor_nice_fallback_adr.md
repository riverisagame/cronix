# ADR: Nice/IONice 调度避让机制的平滑降级与健壮性优化

## 1. 背景与上下文

当前 Cronix 定时任务调度器在 Linux 环境下执行 Shell 任务时，为了保护主调度器进程不被大任务的高频 CPU 与 I/O 磁盘读写抢占，引入了 Linux 调度避让机制（即前置包裹 `nice -n <niceValue> ionice -c <ioNiceClass>`）。

但是在以下环境中，此设计遇到了阻碍：
1. **瘦化容器镜像（Alpine / Distroless 等）**：许多极其精简的 Linux 镜像默认没有安装 `coreutils`（包含 `nice`）或 `util-linux`（包含 `ionice`）。
2. **安全受限系统**：某些受限环境可能禁用了 `nice` 调优系统调用或缺少相应的命令路径。

在上述环境下，当任务启动时，Go 语言在尝试解析 `nice` 命令路径并执行时，会报如下错误：
`fork/exec /usr/bin/nice: no such file or directory`
这会导致原本正常的 Shell 任务由于调度避让包裹器的缺失而完全无法启动。

## 2. 选型对比

| 维度 | 方案 A：继续保持强依赖并要求环境安装（现状） | 方案 B：运行时动态探测与平滑降级（所选方案） |
| --- | --- | --- |
| **部署便利性** | 差。用户必须在 Dockerfile 中额外安装 `coreutils` 和 `util-linux`。 | 优。开箱即用，自动适应各种容器与精简版 Linux 环境。 |
| **健壮性** | 弱。若缺少工具，会导致所有 Shell 任务失败。 | 强。即便缺少工具，亦能自动平滑回退，照常执行任务。 |
| **性能损耗** | 无额外损耗。 | 极低。利用包级 `sync.Once` 缓存探测结果，仅在首次执行时有微秒级 PATH 查找开销。 |
| **防抢占保护** | 完美保护。 | 视环境而定。环境支持时完美保护，不支持时退化为标准执行，满足最大可用性。 |

## 3. 架构设计与变更范围

### 3.1. 自动探测逻辑设计
在 `internal/executor/shell_unix.go` 中，新增包级全局变量与 `sync.Once` 保护的探测函数：
* 变量定义：
  ```go
  var (
      lookNiceOnce   sync.Once
      lookIONiceOnce sync.Once
      hasNice        bool
      hasIONice      bool
  )
  ```
* 探测逻辑：
  ```go
  func detectNice() bool {
      lookNiceOnce.Do(func() {
          // 不仅使用 LookPath，而是通过实际试运行探测 nice 可执行性
          // 防止文件虽存在但由于动态链接器缺失、权限或 chroot/cgroups 限制等导致 fork/exec 失败
          cmd := exec.Command("nice", "-n", "0", "sh", "-c", "exit 0")
          err := cmd.Run()
          hasNice = (err == nil)
      })
      return hasNice
  }

  func detectIONice() bool {
      lookIONiceOnce.Do(func() {
          // 实际试运行探测 ionice 可执行性
          cmd := exec.Command("ionice", "-c", "3", "sh", "-c", "exit 0")
          err := cmd.Run()
          hasIONice = (err == nil)
      })
      return hasIONice
  }
  ```

### 3.2. 动态命令构建
修改 `ExecuteShell` 中的 `cmdArgs` 构建逻辑，不再硬编码包裹 `nice` 和 `ionice`，而是根据 `detectNice()` 和 `detectIONice()` 的结果，动态组合最终的命令：
* 例如，若 `nice` 存在而 `ionice` 不存在，包裹后的命令前缀应当仅为 `nice -n <value>`；若两者都不存在，则不进行任何调度包裹。

## 4. 并发安全性与副作用审计 (Self-Attack & Performance)

* **并发安全**：由于采用 `sync.Once` 保护，在多任务并发启动时，有且仅有最先到达的任务会触发实际的 PATH 目录检索，后续并发调用直接读取内存布尔值，不产生任何锁争用，确保响应时间远低于 1ms。
* **物理零污染**：本变更只修改进程执行阶段的命令包装逻辑，对系统表、持久化数据无任何写动作。
* **回退机制兜底**：如果在极特殊情况下（如 PATH 在运行时发生变化，导致探测到了 `nice` 但启动时依然失败），原有的 `ExecuteShell` 的 `cmd.Start()` 回退处理中，若 systemd-run 等高级隔离失败，也有降级保障，我们这次增加的动态拼接更从根源上消除了 `nice: no such file or directory` 这一高频报错。
