// ============================================================
// internal/executor/cleanup.go - 文件清理执行器
//
// 功能：删除指定目录下匹配特定模式且超过一定时长的旧文件
// 典型用途：清理日志文件、临时文件等
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
    matches, err := filepath.Glob(pattern)                       // Glob = 全局匹配，找到所有符合模式的文件路径
    if err != nil {
        return &CleanupResult{Error: err}
    }

    // 第五步：逐个检查并删除"够旧"的文件
    var deleted int                                              // 已删除文件计数器
    for _, m := range matches {                                  // 遍历每个匹配到的文件路径
        // 检查上下文是否被取消（比如用户停止了清理任务）
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
