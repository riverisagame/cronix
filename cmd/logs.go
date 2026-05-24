// ============================================================
// cmd/logs.go - 命令行日志查看子命令
//
// 这个文件实现了 "cronix logs" 命令。
// 让你在命令行窗口里直接查看任务的执行历史记录。
// 不用打开网页界面，在终端里就能快速查看哪个任务成功、哪个失败了。
//
// 支持的过滤选项：
//   --task   按任务名称搜索（支持模糊匹配）
//   --status 按执行状态过滤（success=成功, failed=失败, timeout=超时）
//   --since  只看最近一段时间内的日志（比如 --since 1h 表示最近一小时）
//   --last   只看最后 N 条记录
// ============================================================
package cmd

import (
    // "fmt" 格式化输出，把日志格式化后打印到屏幕上
    "fmt"

    // "os" 操作系统工具，用于程序退出和错误输出
    "os"

    // "time" 时间处理工具
    // 这里用它来解析 --since 参数（如 "1h30m" 表示一小时三十分钟）
    "time"

    // 以下是我们项目自己的模块
    "cronix/internal/config"   // 读取配置文件，获取数据库路径
    "cronix/internal/database" // 打开数据库
    "cronix/internal/model"    // 日志数据模型（ExecutionLog 结构体）

    // cobra 命令行框架
    "github.com/spf13/cobra"
)

// 这些是命令行参数的变量
// 用 var 声明，让 cobra 框架可以把用户输入的值填进来
var (
    // logFollow 对应 --follow / -f 参数
    // 设为 true 时，像 tail -f 一样持续输出新日志（实时跟踪）
    logFollow bool

    // logTaskName 对应 --task 参数
    // 按任务名字过滤，支持模糊搜索（包含即可）
    logTaskName string

    // logStatus 对应 --status 参数
    // 按执行状态过滤：success / failed / timeout
    logStatus string

    // logSince 对应 --since 参数
    // 只看一段时间内的日志，格式如 "1h"（1小时）、"30m"（30分钟）
    logSince string

    // logLast 对应 --last 参数
    // 只看最后 N 条日志，默认 20 条
    logLast int
)

// logsCmd 是日志查看子命令的定义
var logsCmd = &cobra.Command{
    Use:   "logs",                  // 命令名：用户输入 "cronix logs" 触发
    Short: "在命令行窗口查看任务执行日志", // 简短说明
    Run:   runLogs,                 // 执行函数
}

// init() 在包加载时自动执行
// 这里给 logs 命令加上各种可选的过滤参数
// 就像给遥控器加上各种按钮：频道按钮、音量按钮、静音按钮...
func init() {
    // --follow / -f：实时跟踪日志（一直输出新日志，不退出）
    // BoolVarP 表示"布尔类型的参数，支持短参数"
    logsCmd.Flags().BoolVarP(&logFollow, "follow", "f", false, "持续跟踪日志输出（类似 tail -f 的效果）")

    // --task：按任务名称过滤
    // StringVar 表示"字符串类型的参数，只有长参数名"
    logsCmd.Flags().StringVar(&logTaskName, "task", "", "按任务名称过滤（支持模糊匹配，输入部分名称即可）")

    // --status：按状态过滤
    logsCmd.Flags().StringVar(&logStatus, "status", "", "按执行状态过滤（可选值: success=成功, failed=失败, timeout=超时）")

    // --since：按时间范围过滤
    logsCmd.Flags().StringVar(&logSince, "since", "", "只看最近一段时间内的日志（例如: 1h 表示一小时, 30m 表示三十分钟）")

    // --last：限制显示条数
    // IntVar 表示"整数类型的参数"
    logsCmd.Flags().IntVar(&logLast, "last", 20, "只显示最后 N 条日志记录")
}

// runLogs 是 "cronix logs" 命令的实际处理函数
// 它的流程：加载配置 -> 打开数据库 -> 按条件查询日志 -> 格式化输出到屏幕
func runLogs(cmd *cobra.Command, args []string) {
    // --- 第1步：加载配置文件（目的是获取数据库文件路径）---
    cfg, err := config.Load("config.yaml")
    if err != nil {
        // 如果配置文件读不到，只警告不退出
        // 因为可能用户只是临时查看日志，没有配置文件也能用默认路径
        fmt.Fprintf(os.Stderr, "警告：无法加载配置文件: %v\n", err)
        // 下面会使用默认的数据库路径
    }

    // --- 第2步：确定数据库文件路径 ---
    // 默认路径是 "data/cronix.db"（项目 data 目录下的 cronix.db 文件）
    dbPath := "data/cronix.db"
    // 如果配置文件加载成功了，用配置文件里指定的路径
    if cfg != nil {
        dbPath = cfg.Database.Path
    }

    // --- 第3步：打开数据库 ---
    // database.Init 打开 SQLite 数据库连接
    if err := database.Init(dbPath); err != nil {
        // 打不开数据库就没办法了，直接退出
        fmt.Fprintf(os.Stderr, "打开数据库失败: %v\n", err)
        os.Exit(1)
    }
    // defer 确保函数退出前一定关闭数据库连接（防止数据损坏）
    defer database.Close()

    // --- 第4步：构建查询条件 ---
    // database.DB 是全局的数据库连接
    // .Model(&model.ExecutionLog{}) 告诉 GORM："我要查 execution_logs 这张表"
    // &model.ExecutionLog{} 是一个空的 ExecutionLog 结构体，GORM 根据它推断表名
    query := database.DB.Model(&model.ExecutionLog{})
    // .Where 是 SQL 里 WHERE 子句（筛选条件）的 Go 写法
    // 以下每个 if 都在检查用户有没有指定对应的过滤参数
    // 指定了就加上对应的筛选条件

    if logTaskName != "" {
        // 如果用户指定了 --task，按任务名模糊搜索
        // LIKE 是 SQL 的模糊匹配，"%关键词%" 表示包含这个关键词就算匹配
        // 例如 --task backup 会找到名为 "daily_backup" 和 "db_backup" 的任务
        query = query.Where("task_name LIKE ?", "%"+logTaskName+"%")
    }
    if logStatus != "" {
        // 如果用户指定了 --status，按状态精确过滤
        // = ? 是精确匹配，必须完全一致才算
        query = query.Where("status = ?", logStatus)
    }
    if logSince != "" {
        // 如果用户指定了 --since，先解析时间长度
        // time.ParseDuration 把 "1h30m" 这样的字符串翻译成时间长度
        // 例如 "1h" = 1小时 = 3600秒，"30m" = 30分钟 = 1800秒
        if d, err := time.ParseDuration(logSince); err == nil {
            // 解析成功才加条件
            // time.Now() 是当前时间
            // .Add(-d) 是往前推 d 这么长的时间
            // start_time > (当前时间 - d) 等价于 "最近 d 时间内的记录"
            query = query.Where("start_time > ?", time.Now().Add(-d))
        }
        // 如果时间格式解析失败，静默忽略（不给用户报错了，只是不生效）
    }

    // --- 第5步：执行查询 ---
    // 声明一个日志数组来存放查询结果
    var logs []model.ExecutionLog
    // .Order("start_time DESC") 按开始时间倒序排列（最新的在前面）
    // DESC = descending（降序，从大到小）
    // .Limit(logLast) 最多取 logLast 条（默认 20 条）
    // .Find(&logs) 执行查询，把结果填到 logs 数组里
    query.Order("start_time DESC").Limit(logLast).Find(&logs)

    // --- 第6步：格式化输出 ---
    // 从后往前遍历（因为数据库返回的是倒序，需要反转成时间从早到晚）
    // for i := len(logs) - 1; 从最后一个元素开始
    // i >= 0; 一直到第一个元素
    // i-- 每次往前移一个位置
    for i := len(logs) - 1; i >= 0; i-- {
        l := logs[i] // 当前要处理的这条日志记录

        // --- 第6.1步：根据状态设置图标 ---
        // 用不同的缩写图标表示不同的执行结果
        statusIcon := "?" // 默认未知状态
        switch l.Status {
        case "success":
            statusIcon = "OK"    // 成功
        case "failed":
            statusIcon = "FAIL"  // 失败
        case "timeout":
            statusIcon = "TIMEOUT" // 超时
        case "running":
            statusIcon = "RUN"   // 正在运行
        }

        // --- 第6.2步：打印一行日志 ---
        // l.StartTime.Format("15:04:05") 把时间格式化成 "时:分:秒" 的样子
        // %-8s 表示占 8 个字符宽度，左对齐（让表格列整齐）
        // %s 就是普通的字符串占位符
        fmt.Printf("[%s] %s | %-8s | %s",
            l.StartTime.Format("15:04:05"), // 时间
            statusIcon,                      // 状态图标
            l.TaskName,                      // 任务名称
            l.TriggerType,                   // 触发方式（cron=定时, manual=手动）
        )
        // 如果有错误信息，追加打印出来
        if l.ErrorMsg != "" {
            fmt.Printf(" | 错误: %s", l.ErrorMsg)
        }
        // 换行，这一条日志打印完毕
        fmt.Println()
    }
}
