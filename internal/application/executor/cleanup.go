// ============================================================
// internal/executor/cleanup.go - 文件清理执行器
//
// 功能：删除指定目录下匹配特定模式且超过一定时长的旧文件
// 典型用途：清理日志文件、临时文件等
//
// 📌 【大厂面试·核心考点】
// 面试官问：你在Go里怎么做大批量文件清理的？遇到千万级文件目录怎么办？
// 标准答案：不能直接用 `filepath.Glob`（存在OOM风险，且无法中途取消）。生产级应用应该用 
// `filepath.WalkDir`（Go 1.16+）采用流式读取，并结合 `Context` 随时响应中断，
// 同时对大批量删除做批处理（如每批1000个）和限流（休眠释放CPU），防止IO打满拖垮业务系统。
//
// 🔬 【底层原理·深度剖析】
// 文件系统 inode 原理：删除文件（os.Remove）到底在删什么？
// 生活比喻：就像在图书馆里，书架上的书是“数据块”，而图书目录卡片就是“inode”。
// 删除文件实际上调用的是操作系统的 `unlink` 系统调用。它只做两件事：
// 1. 减少该文件 inode 的硬链接数（Hard Link Count）。
// 2. 在父目录的数据块（data block）中，将该文件名的条目擦除。
// 只有当满足两个条件时，磁盘空间才会被真正回收：
// - 硬链接数变成 0
// - 没有任何进程（fd）正在打开该文件
// 区别补充：软链接（Symlink）就像Windows快捷方式，删了快捷方式不影响源文件；硬链接（Hardlink）
// 则是多个文件名指向同一个物理 inode。必须删掉所有硬链接，数据才会被释放。
//
// 💀 【踩坑血泪·反面教材】
// 事故现场：某电商大促期间日志报警磁盘写满，运维执行了 `rm -rf *.log`，但 df 看到磁盘依然 100%没释放！
// 根本原因：虽然文件删了，但是有后台进程（比如Logstash或业务自身）还持有这些文件的文件描述符（fd）。
// 解决方案：使用 `lsof | grep deleted` 找出持有句柄的进程并重启它。更优雅的轮转策略是清空
// 文件内容 `> file.log` 而不是直接删除。
// ============================================================
package executor

import (
    "context"        // 上下文：支持取消操作
    "encoding/json"  // JSON编解码：解析清理配置
    "fmt"            // 格式化：错误信息包装
    "os"             // 操作系统接口：查看文件信息、删除文件
    "path/filepath"  // 文件路径处理：查找匹配的文件
    "time"           // 时间处理：判断文件是否"够旧"
)

// CleanupConfig 定义了清理任务的配置（从JSON解析而来）
type CleanupConfig struct {
    Path           string `json:"path"`              // 要扫描的目录路径
    Pattern        string `json:"pattern"`           // 文件匹配模式（通配符），如 "*.log" 匹配所有日志文件
    OlderThanHours int    `json:"older_than_hours"`  // 只删除超过这个小时数的文件
}

// CleanupResult 存放清理操作的执行结果
type CleanupResult struct {
    DeletedCount int   // 实际删除了多少个文件
    Error        error // 执行过程中的错误（nil表示正常）
}

// ExecuteCleanup 执行文件清理操作
//
// 🔬 【底层原理·深度剖析】
// Context 取消传播链（父取消子必取消）：
// 生活比喻：就像公司老总（根Context）宣布项目流产，部门经理（子Context）必须立刻通知下面的组员（孙子Context）全员停工。
// 在Go的底层源码实现中，子Context（如 cancelCtx）会把自己挂载到父Context的 `children` 字典（map）上。
// 当父Context调用 `cancel()` 函数时，它会遍历自己所有的 children，并递归调用它们的 `cancel()` 方法，
// 同时关闭自身的 `done` channel。这种基于 channel close 广播机制的设计，保证了哪怕有成千上万个 
// Goroutine 在等待，也能在 O(1) 的系统调用时间内被瞬间唤醒并安全退出，绝无泄漏风险。
//
// 参数 ctx：上下文（支持取消操作）
// 参数 configJSON：JSON格式的清理配置字符串
// 返回值：CleanupResult指针
func ExecuteCleanup(ctx context.Context, configJSON string) *CleanupResult {
    // 第一步：解析JSON配置
    var cfg CleanupConfig
    if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil { // JSON字符串转结构体
        return &CleanupResult{Error: fmt.Errorf("parse config: %w", err)}
    }

    // 第二步：计算"截止时间"——早于这个时间的文件才会被删除
    // time.Now() 获取当前时间，减去 OlderThanHours 小时
    cutoff := time.Now().Add(-time.Duration(cfg.OlderThanHours) * time.Hour) // 往前推N小时

    // 第三步：构建文件搜索模式
    // filepath.Join 把目录和文件模式拼接成完整路径（自动处理分隔符）
    pattern := filepath.Join(cfg.Path, cfg.Pattern)

    // 第四步：查找匹配的所有文件
    // 📌 【大厂面试·核心考点】
    // 面试官问：通配符匹配(Glob)和正则表达式(Regex)有什么本质区别？
    // 标准答案：
    // 1. 语法复杂度：Glob极简（*匹配任意字符，?匹配单字符），Regex极其强大且复杂（支持断言、捕获组等）。
    // 2. 底层实现：Go的 `filepath.Glob` 不会编译为状态机，而是单纯的字符串切片与递归匹配；`regexp` 会将模式编译为虚拟机指令序列或NFA状态机。
    // 3. 内存与性能缺陷：`filepath.Glob` 存在严重的扩展性瓶颈——它会一次性读取匹配的目录项，
    //    并把所有结果放在内存切片中。如果匹配到上百万个文件，会瞬间导致 OOM 并触发大规模 GC 停顿！
    // ⚡ 【性能实战·生产调优】
    // 真实场景数据：当单目录下文件超过10万个时，`filepath.Glob` 会消耗数十MB内存，耗时几百毫秒。
    // 优化手段：如果已知文件量可能极大，禁止使用 `Glob`。应采用 `filepath.WalkDir` 配合正则，
    // 空间复杂度从 O(N) 断崖式降为 O(1)。
    matches, err := filepath.Glob(pattern)                       // Glob = 全局匹配，找到所有符合模式的文件路径
    if err != nil {
        return &CleanupResult{Error: err}
    }

    // 第五步：逐个检查并删除"够旧"的文件
    var deleted int                                              // 已删除文件计数器
    for _, m := range matches {                                  // 遍历每个匹配到的文件路径
        // 检查上下文是否被取消（比如用户停止了清理任务）
        // 🔬 【底层原理·深度剖析】
        // select 语句的非阻塞检查模式（Non-blocking Channel Check）：
        // 生活比喻：就像保安巡逻时，顺便低头看一眼对讲机有没有紧急呼叫（select case <-ctx.Done()），
        // 如果没有呼叫（default），就头也不回地继续往前巡逻，而不是傻站在原地死等电话。
        // 编译器优化：当 select 中存在 default 分支且只有一个 case 时，Go编译器会将其优化为非阻塞操作。
        // 它会直接调用底层的 `runtime.selectnbrecv` 尝试从 channel 取数据：
        // 1. 如果 channel 已关闭（Context被取消），立即命中 case 返回。不消耗多余时间。
        // 2. 如果 channel 为空，立即走 default 分支，绝不挂起当前 M（系统线程），不产生任何上下文切换开销。
        // 这种高频轮询检查在长耗时的 for 循环或大量 IO 操作中极为关键，是优雅退出的教科书级实践。
        select {
        case <-ctx.Done():                                       // 如果上下文取消了
            return &CleanupResult{DeletedCount: deleted, Error: ctx.Err()} // 返回当前进度和取消错误
        default:                                                 // 没有取消，继续执行
        }

        // 获取文件信息（大小、修改时间等）
        info, err := os.Stat(m)                                  // Stat = 查看文件状态
        if err != nil {
            continue                                             // 跳过无法查看的文件
        }
        // 判断文件的最后修改时间是否早于截止时间
        if info.ModTime().Before(cutoff) {                       // Before = "在...之前"
            if err := os.Remove(m); err == nil {                 // Remove = 删除文件
                deleted++                                        // 删除成功，计数器加1
            }
        }
    }

    // 第六步：返回结果
    return &CleanupResult{DeletedCount: deleted}
}
