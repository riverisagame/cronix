// =============================================================================
// 📦 文件：shell_common.go
// 📁 路径：internal/application/executor/shell_common.go
// 🏗️ 层级：Application 层 → Executor 子层 → Shell 跨平台通用定义
// 📝 职责：定义 Shell 命令执行的跨平台通用数据结构和任务取消逻辑
// =============================================================================
//
// ┌─────────────────────────────────────────────────────────────────────────────┐
// │              🏗️ 【架构设计·跨平台 Shell 执行器总览】                         │
// ├─────────────────────────────────────────────────────────────────────────────┤
// │                                                                           │
// │  📌 跨平台抽象设计（初二小白版比喻）                                          │
// │  ────────────────────────────────────                                      │
// │  想象你有一个"万能遥控器"，无论对着索尼电视、三星电视还是小米电视，              │
// │  按"开机"按钮都能开机。你不需要知道每台电视内部电路怎么接的——                  │
// │  遥控器帮你屏蔽了这些差异。                                                  │
// │                                                                           │
// │  这个文件就是那个"万能遥控器的公共按钮定义"：                                  │
// │  - ShellResult（执行结果）：无论是 Linux 上用 /bin/sh 还是 Windows 上用        │
// │    cmd.exe 执行命令，返回的结果格式都是一样的（输出 + 退出码 + 错误）。        │
// │  - RunningTaskCancels（取消按钮）：无论在哪个平台，都可以通过同一个              │
// │    Cancel 函数来停止正在运行的任务。                                          │
// │  - CancelExecution（按下取消按钮）：统一的取消操作入口。                        │
// │                                                                           │
// │  而"每台电视的内部电路"（平台特定的 Shell 实现），则分别定义在：                │
// │    - shell_unix.go    （Linux/macOS，使用 Build Tag: !windows）              │
// │    - shell_windows.go （Windows，使用 Build Tag: windows）                   │
// │                                                                           │
// │  🔬 【底层原理·Go Build Tags 条件编译】                                      │
// │  ──────────────────────────────────────                                    │
// │  Go 的条件编译不像 C/C++ 的 #ifdef，而是通过 **文件命名约定** 和               │
// │  **//go:build 指令** 两种机制实现：                                           │
// │                                                                           │
// │  机制一：文件名后缀约定（本项目使用此方式）                                    │
// │    文件名格式：<name>_GOOS.go 或 <name>_GOOS_GOARCH.go                       │
// │    示例：                                                                   │
// │      shell_windows.go  → 只在 GOOS=windows 时编译                           │
// │      shell_linux.go    → 只在 GOOS=linux 时编译                             │
// │      shell_unix.go     → 需要配合 //go:build 指令（unix 不是 GOOS 值）       │
// │    注意：shell_common.go 中的 "common" 没有特殊含义，只是一个普通的文件名        │
// │    后缀，所以这个文件在 **所有平台** 都会被编译——这正是"通用层"的含义。         │
// │                                                                           │
// │  机制二：//go:build 指令（Go 1.17+ 新语法）                                  │
// │    在文件首行写 //go:build <条件表达式>                                       │
// │    支持布尔运算：                                                            │
// │      //go:build linux || darwin      → Linux 或 macOS                       │
// │      //go:build !windows             → 非 Windows（等价于 Unix 全家族）      │
// │      //go:build linux && amd64       → 仅 Linux x86_64                     │
// │      //go:build ignore               → 永远不编译（用于示例文件）             │
// │                                                                           │
// │  📌 【大厂面试·核心考点】                                                    │
// │  ──────────────────────────                                                │
// │  Q1: "Go 是如何实现跨平台编译的？GOOS/GOARCH 是什么？"                       │
// │  A1: Go 编译器是 **交叉编译（Cross-Compilation）** 的原生支持者。              │
// │      设置 GOOS 和 GOARCH 两个环境变量即可编译出目标平台的二进制文件：           │
// │        GOOS=linux   GOARCH=amd64  go build → Linux x86_64 ELF 二进制       │
// │        GOOS=windows GOARCH=amd64  go build → Windows x86_64 PE 二进制      │
// │        GOOS=darwin  GOARCH=arm64  go build → macOS Apple Silicon Mach-O    │
// │      编译矩阵（常见组合）：                                                  │
// │      ┌──────────┬─────────────────────────────────────────────┐             │
// │      │ GOOS     │ 可用 GOARCH                                │             │
// │      ├──────────┼─────────────────────────────────────────────┤             │
// │      │ linux    │ amd64, arm64, 386, arm, mips, riscv64, ... │             │
// │      │ windows  │ amd64, arm64, 386                          │             │
// │      │ darwin   │ amd64, arm64                                │             │
// │      │ freebsd  │ amd64, arm64, 386                          │             │
// │      └──────────┴─────────────────────────────────────────────┘             │
// │      运行 `go tool dist list` 可查看所有支持的 GOOS/GOARCH 组合              │
// │      （Go 1.21 支持约 40+ 种组合）。                                         │
// │                                                                           │
// │  Q2: "sync.Map 和普通 map + sync.Mutex 有什么区别？什么时候该用哪个？"        │
// │  A2: 见下方 RunningTaskCancels 变量的详细注释。                               │
// │                                                                           │
// │  Q3: "context.CancelFunc 的底层原理是什么？"                                 │
// │  A3: 见下方 CancelExecution 函数的详细注释。                                  │
// │                                                                           │
// │  ⚡ 【性能实战·生产调优】                                                    │
// │  ──────────────────────────                                                │
// │  本文件定义的数据结构和函数都是轻量级的：                                      │
// │  - sync.Map 的读操作是无锁的（基于 atomic.Value），写操作才加锁。              │
// │  - CancelExecution 只是一次 map 查找 + 函数调用，耗时 < 100ns。              │
// │  - ShellResult 是一个值类型 struct，分配在栈上（如果不逃逸的话）。              │
// │                                                                           │
// │  💀 【踩坑血泪·反面教材】                                                    │
// │  ──────────────────────────                                                │
// │  1. **任务取消后未清理 map**：如果只调用 CancelFunc 但不从 RunningTaskCancels  │
// │     中删除该 key，会导致：                                                   │
// │     - 内存泄漏：map 中积累大量已完成任务的 CancelFunc（虽然每个很小）          │
// │     - 重复取消：对已完成的任务再次调用 CancelFunc 不会 panic（幂等的），        │
// │       但语义上不正确。                                                       │
// │     正确做法：在任务执行完毕的 defer 中调用 RunningTaskCancels.Delete(taskID)。│
// │  2. **ExitCode = -1 的含义不统一**：本项目定义 -1 表示"异常"，                 │
// │     但 Unix 信号杀死的进程退出码为 128+信号编号（如 SIGKILL=137），            │
// │     Windows 上异常退出码可以是任意负数。需要在平台特定代码中统一映射。           │
// │                                                                           │
// └─────────────────────────────────────────────────────────────────────────────┘

package executor

import (
	// 🔬 【底层原理·深度剖析】context 包
	// ─────────────────────────────────────
	// context 是 Go 语言处理"取消传播"和"超时控制"的核心机制。
	// 它解决的问题是：当一个 HTTP 请求被取消时，如何通知所有下游的 goroutine 停止工作？
	//
	// 📌 （初二小白版比喻）
	// 想象老师布置了一个小组作业，组长分配任务给 5 个组员。突然老师说"这个作业取消了"。
	// 组长不需要一个一个去通知组员——他只需要在微信群里发一条"取消"消息（关闭 channel），
	// 所有组员（goroutine）通过 select 监听这个群消息，就会自动停下来。
	// context 就是这个"微信群"。
	//
	// context.WithCancel(parent) 返回一个子 context 和一个 CancelFunc。
	// 调用 CancelFunc() 时：
	//   1. 关闭 context 内部的 done channel（close(c.done)）
	//   2. 所有 <-ctx.Done() 的 select 分支立即被触发
	//   3. 递归取消所有子 context（级联取消）
	//
	// 📌 【大厂面试·核心考点】
	// Q: "context 取消后，正在执行的系统调用（如 exec.CommandContext 等待子进程）会怎样？"
	// A: exec.CommandContext 内部监听 ctx.Done()，当收到取消信号时，
	//    会向子进程发送 os.Kill 信号（Unix）或调用 TerminateProcess（Windows）。
	//    但这不是瞬时的——子进程可能还需要一些时间来清理资源。
	//    如果需要优雅关闭，应该先发 SIGTERM，等待一段时间后再发 SIGKILL。
	"context"

	// 🔬 【底层原理·深度剖析】sync 包
	// ─────────────────────────────────
	// sync 包提供了 Go 语言的同步原语：Mutex、RWMutex、WaitGroup、Once、Pool、Map 等。
	// 这里使用的 sync.Map 是 Go 1.9 引入的并发安全 map。
	//
	// 📌 【大厂面试·核心考点】
	// Q: "sync.Map 的内部实现是什么？为什么读操作是无锁的？"
	// A: sync.Map 内部使用"双 map"架构（read map + dirty map）：
	//    ┌─────────────────────────────────────────────────────────────┐
	//    │  sync.Map 内部结构                                          │
	//    │  ┌────────────┐    ┌────────────┐                          │
	//    │  │  read map   │    │  dirty map  │                         │
	//    │  │ (atomic)    │    │ (mutex)     │                         │
	//    │  │ 无锁快速读  │    │ 有锁写入    │                         │
	//    │  └────────────┘    └────────────┘                          │
	//    │       ↑                  ↑                                  │
	//    │    Load 优先查  ──miss→ 查 dirty                            │
	//    │    Store ──────────────→ 写 dirty                           │
	//    │    miss 次数达阈值 → dirty 提升为 read（swap）              │
	//    └─────────────────────────────────────────────────────────────┘
	//    read map 通过 atomic.Value 存储，读取时无需加锁（CAS 操作）；
	//    写入时加 Mutex 锁写入 dirty map；
	//    当 read map 的 miss 次数达到 dirty map 的长度时，
	//    dirty map 被原子地提升（promote）为新的 read map。
	//
	// Q: "什么场景适合用 sync.Map？什么场景应该用 map + RWMutex？"
	// A: sync.Map 适合以下两种场景（官方文档明确说明）：
	//    1. **读多写少**：key 被写入一次后反复读取（如缓存、配置）。
	//    2. **不同 goroutine 操作不同的 key 集合**：减少锁竞争。
	//    不适合的场景：频繁对相同 key 进行读写（此时 RWMutex 更高效）。
	//    本项目的 RunningTaskCancels 正是典型的"写入一次（任务启动时），
	//    读取一次（取消时），删除一次（任务结束时）"模式，非常适合 sync.Map。
	//
	// ⚡ 【性能实战】
	// sync.Map vs map+RWMutex 基准测试（参考数据，16核机器）：
	//   场景：95% 读 + 5% 写，1000 个不同的 key
	//     sync.Map:     ~50ns/op   (读路径无锁，几乎无竞争)
	//     map+RWMutex:  ~120ns/op  (读锁虽然共享，但仍有 cache line 竞争)
	//   场景：50% 读 + 50% 写，同一个 key
	//     sync.Map:     ~300ns/op  (频繁 miss，需要加锁查 dirty)
	//     map+RWMutex:  ~150ns/op  (写锁虽独占，但无 double-map 开销)
	"sync"
)

// RunningTaskCancels 运行中任务的取消句柄映射
// key: taskID (uint), value: context.CancelFunc
//
// 🔬 【底层原理·深度剖析】
// ─────────────────────────
// 这是一个**包级别的全局变量**，在整个 executor 包内共享。
// 使用 sync.Map 而非普通 map 的理由：
//   1. **并发安全**：多个 goroutine（HTTP Handler goroutine + Shell 执行 goroutine）
//      可能同时读写这个 map，普通 map 在并发读写时会 panic（Go 1.6+）：
//        fatal error: concurrent map read and map write
//   2. **无需初始化**：sync.Map 的零值即可使用（var m sync.Map），
//      而普通 map 必须 make(map[K]V) 初始化，否则写入会 panic。
//   3. **读写模式匹配**：任务的生命周期是"创建时写入 → 取消时读取 → 结束时删除"，
//      每个 key 的读写次数很少，且不同任务操作不同的 key，完美匹配 sync.Map 的优化场景。
//
// 📌 【大厂面试·核心考点】
// Q: "Go 的 map 为什么不是并发安全的？为什么不像 Java 的 ConcurrentHashMap 那样内置并发？"
// A: Go 语言设计哲学：不为不需要的功能付出性能代价。
//    绝大多数 map 只在单个 goroutine 中使用，加锁会拖慢所有用户。
//    Go 在运行时通过 hashWriting 标志位做了"竞态检测"——
//    如果检测到并发写入，直接 throw("concurrent map writes") 让程序崩溃，
//    而不是默默产生数据损坏（这比 C++ 的 undefined behavior 安全得多）。
//
// 💀 【踩坑血泪·反面教材】
// 如果用普通 map + sync.Mutex 替代 sync.Map，忘记在某个路径加锁：
//   var mu sync.Mutex
//   var cancels = make(map[uint]context.CancelFunc)
//   // goroutine A：加了锁
//   mu.Lock(); cancels[1] = cancelFn; mu.Unlock()
//   // goroutine B：忘记加锁 💥
//   if fn, ok := cancels[1]; ok { fn() }  // 可能与 goroutine A 并发，panic！
// sync.Map 的优势：所有操作（Load/Store/Delete）都是原子性的，不可能忘记加锁。
var RunningTaskCancels sync.Map

// ShellResult 存放 Shell 命令执行后的结果（跨平台通用）
//
// 🏗️ 【架构设计·模式对比】
// ─────────────────────────
// 为什么要定义一个统一的 ShellResult 而不是直接返回 (string, int, error) 三元组？
//
// 方案 A（三元组返回）：
//   func RunShell(cmd string) (output string, exitCode int, err error)
//   优点：Go 风格，简洁。
//   缺点：当需要扩展字段（如执行耗时 Duration、PID、是否超时）时，
//         必须修改所有调用方的签名——这违反了开放封闭原则（OCP）。
//
// 方案 B（结构体返回，本项目采用）：
//   func RunShell(cmd string) ShellResult
//   优点：扩展字段时只需修改 struct 定义，不影响调用方签名。
//   缺点：稍微多了一层封装（但 Go 编译器会内联优化掉）。
//
// 本项目选择方案 B，为未来可能的字段扩展预留了空间。
//
// 📌 【大厂面试·核心考点】
// Q: "Go 函数返回 struct vs 返回 *struct，性能有什么区别？"
// A: 取决于 struct 的大小和逃逸分析（escape analysis）：
//    - 小 struct（<= 64 字节，如 ShellResult 约 40 字节）：
//      值返回时通常分配在栈上，无需 GC，性能更好。
//    - 大 struct（> 几百字节）：值拷贝开销大，建议返回指针。
//    - 逃逸分析：如果返回的 struct 被外部持有（如存入 slice/map），
//      Go 编译器会自动将其"逃逸"到堆上，此时值返回和指针返回区别不大。
//    - 查看逃逸分析：go build -gcflags="-m" 会打印哪些变量逃逸到堆上。
//
// ⚡ 【性能实战】
// ShellResult 的内存布局（64位系统）：
//   Output   string → 16 字节（指针 8B + 长度 8B，string header）
//   ExitCode int    → 8 字节（64位 int）
//   Error    error  → 16 字节（接口 = 类型指针 8B + 数据指针 8B）
//   总计：约 40 字节 + padding ≈ 40 字节
//   这么小的 struct，完全适合值传递（栈分配），无需用指针。
type ShellResult struct {
	// Output 命令的标准输出和标准错误合并内容（可能被截断）
	//
	// 🔬 【底层原理】
	// 标准输出（stdout，文件描述符 1）和标准错误（stderr，文件描述符 2）被合并到同一个字符串中。
	// 合并方式通常是在 exec.Cmd 中设置 cmd.Stdout = &buf; cmd.Stderr = &buf（共享同一个 buffer）。
	// 这意味着 stdout 和 stderr 的输出会交错在一起，无法区分哪些行来自 stdout、哪些来自 stderr。
	// 对于 Cron 任务日志来说，这种合并是合理的——运维人员只关心"命令输出了什么"，不区分来源。
	//
	// 💀 【踩坑血泪】
	// "可能被截断"意味着有最大长度限制。如果命令输出了 100MB 的日志，
	// 全部存入 Output 会导致内存暴涨。平台特定实现中应该做截断处理。
	Output string
	// ExitCode 命令的退出码：0=成功，非0=失败，-1=异常
	//
	// 🔬 【底层原理】
	// Unix 进程退出码约定（POSIX 标准）：
	//   0         → 成功
	//   1-125     → 程序自定义的错误码
	//   126       → 命令找到了但无法执行（权限不足）
	//   127       → 命令未找到（command not found）
	//   128+N     → 被信号 N 杀死（如 128+9=137 表示被 SIGKILL 杀死）
	//   255       → 退出码超出范围（exit 256 实际变成 exit 0）
	//
	// Windows 进程退出码：
	//   0         → 成功
	//   非0       → 失败（无标准约定，各程序自定义）
	//   负数      → 异常终止（如 STATUS_ACCESS_VIOLATION = 0xC0000005 = -1073741819）
	//
	// 本项目约定 ExitCode = -1 表示"Go 层面的异常"（如命令启动失败、超时等），
	// 与 Unix/Windows 的退出码语义做了统一映射。
	ExitCode int
	// Error 执行过程中的错误（nil 表示没有错误）
	//
	// 🔬 【底层原理】
	// Error 和 ExitCode 的关系：
	//   - Error == nil && ExitCode == 0：命令成功执行并正常退出
	//   - Error == nil && ExitCode != 0：命令执行了但返回了非零退出码（业务错误）
	//   - Error != nil && ExitCode == -1：命令无法启动（如文件不存在、权限不足）
	//   - Error != nil && ExitCode > 0：命令执行过程中被中断（如超时、被取消）
	//
	// 📌 【大厂面试·核心考点】
	// Q: "Go 的 error 接口底层是什么？为什么它能表示任何错误？"
	// A: error 是一个只有一个方法的接口：
	//      type error interface { Error() string }
	//    任何实现了 Error() string 方法的类型都满足 error 接口。
	//    常见的 error 实现：
	//      - errors.New("msg") → *errors.errorString（最简单，只包含一个 string）
	//      - fmt.Errorf("msg: %w", err) → *fmt.wrapError（支持错误链/Unwrap）
	//      - 自定义 struct（如 *os.PathError, *exec.ExitError）
	//    errors.Is() 和 errors.As() 可以沿着错误链（Unwrap 链）查找特定错误。
	Error error
}

// CancelExecution 取消正在执行的任务（通过调用其 context.CancelFunc）
// 返回 true 表示成功取消，false 表示任务不存在或已完成
//
// 🔬 【底层原理·深度剖析】
// ─────────────────────────
// 这个函数做了两件事：
//   1. 从 sync.Map 中查找 taskID 对应的 CancelFunc（Load 操作，无锁）
//   2. 调用 CancelFunc()（关闭 context 的 done channel，触发级联取消）
//
// context 取消的传播路径（以 Shell 执行为例）：
//   CancelExecution(taskID)
//     → cancel()                          // 关闭 done channel
//       → exec.CommandContext 感知到取消  // select { case <-ctx.Done(): }
//         → 发送 os.Kill/TerminateProcess // 杀死子进程
//           → cmd.Wait() 返回             // 收获子进程退出状态
//             → ShellResult.ExitCode = -1 // 标记为异常退出
//
// 📌 【大厂面试·核心考点】
// ─────────────────────────
// Q: "cancel.(context.CancelFunc)() 这个类型断言会 panic 吗？"
// A: 如果 sync.Map 中存储的 value 类型不是 context.CancelFunc，
//    这个类型断言会 panic。但在本项目中，写入方（任务启动时）总是存入
//    context.CancelFunc 类型，所以不会 panic。
//    更安全的写法是使用 comma-ok 模式：
//      if fn, ok := cancel.(context.CancelFunc); ok { fn() }
//    但这会引入不必要的复杂度——如果类型不匹配，说明有更严重的 bug。
//    在这种"内部一致性有保证"的场景下，直接断言是合理的。
//
// Q: "这个函数是线程安全的吗？两个 goroutine 同时取消同一个任务会怎样？"
// A: 是线程安全的。
//    - sync.Map.Load() 本身是并发安全的。
//    - context.CancelFunc 是幂等的——多次调用同一个 CancelFunc，
//      只有第一次会关闭 done channel，后续调用是空操作（no-op）。
//      源码：context.go 中的 cancel 函数使用 sync.Once 确保只执行一次关闭。
//    - 所以两个 goroutine 同时取消同一个任务，结果是正确的，不会 panic 或 double-close。
//
// ⚡ 【性能实战·生产调优】
// 整个函数的执行耗时分析：
//   sync.Map.Load():          ~30-50ns（原子读取 read map，无锁）
//   类型断言:                  ~1-2ns（编译器内联优化）
//   context.CancelFunc():     ~100-200ns（关闭 channel + 通知子 context）
//   总计:                      < 300ns，可以认为是"瞬时"操作。
//
// 🛡️ 【安全攻防·漏洞防线】
// ─────────────────────────
// taskID 来自外部请求（HTTP API），需要确保：
//   1. taskID 经过鉴权——用户只能取消自己创建的任务，不能取消他人的任务。
//   2. taskID 不能被遍历——sync.Map 没有 Len() 方法，攻击者无法枚举所有任务。
//      但 sync.Map 有 Range() 方法，需确保不暴露给外部。
//
// 💀 【踩坑血泪·反面教材】
// ─────────────────────────
// 1. **取消后不删除**：如果只调用 CancelFunc 但不从 RunningTaskCancels 中删除，
//    那么同一个 taskID 如果被复用（数据库自增 ID），后续的新任务可能被误杀。
//    正确做法：任务执行完毕后，在 defer 中调用 RunningTaskCancels.Delete(taskID)。
//
// 2. **取消 vs 超时的区别**：
//    - 取消（Cancel）：人为主动触发，立即生效。
//    - 超时（Timeout）：由 context.WithTimeout 自动触发，到时间后自动取消。
//    两者的 CancelFunc 行为完全一致，但 ctx.Err() 返回值不同：
//      - 取消时：ctx.Err() == context.Canceled
//      - 超时时：ctx.Err() == context.DeadlineExceeded
//    上层代码可以根据 ctx.Err() 区分取消原因，提供不同的日志/提示。
func CancelExecution(taskID uint) bool {
	// sync.Map.Load(key) 返回 (value any, ok bool)
	// 如果 key 存在，ok=true，value 是存储的值（类型为 any，需要类型断言）。
	// 如果 key 不存在，ok=false，value=nil。
	// Load 操作在 read map 命中时是完全无锁的（atomic.Value.Load()），
	// 只有在 read map miss 时才需要加 Mutex 锁去查 dirty map。
	if cancel, ok := RunningTaskCancels.Load(taskID); ok {
		// cancel.(context.CancelFunc)() 是一个类型断言 + 立即调用的组合写法。
		// 等价于：
		//   fn := cancel.(context.CancelFunc)  // 类型断言：any → context.CancelFunc
		//   fn()                                // 调用 CancelFunc，触发 context 取消
		// 注意这里没有使用 comma-ok 模式（fn, ok := cancel.(context.CancelFunc)），
		// 因为写入方保证了类型一致性。如果类型不匹配，说明有严重的代码逻辑错误，
		// 此时 panic 反而是合理的——"fail fast" 原则。
		cancel.(context.CancelFunc)()
		return true
	}
	// 任务不存在（可能已完成并被清理，或 taskID 无效）
	// 返回 false 让调用方知道取消操作未生效。
	return false
}

