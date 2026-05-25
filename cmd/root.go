// ============================================================
// cmd/root.go - 命令行工具的"根命令"（主菜单）
//
// 这个文件是整个命令行界面的骨架。
// 就像手机 App 的主页面：上面有各种按钮（子命令），
// 按不同的按钮干不同的事。
//
// 使用 cobra 这个框架来管理命令：
//   cobra 就像一个"菜单系统"，帮你定义有哪些按钮、按钮叫什么名字、
//   按下按钮后执行什么动作。
// ============================================================
package cmd

import (
    // "context" 是 Go 语言的"上下文"工具箱
    // 用来在程序的不同部分之间传递"我还没干完"或者"可以下班了"这种信号
    // 也用来控制并发任务的生命周期（比如"所有工人停下手里的活"）
    "context"

    // "embed" 是打包外部文件到程序里的工具
    // 这里用它来接收 embed.go 里打包进来的前端网页文件
    "embed"

    // "fmt" 是格式化输出的工具包
    "fmt"

    // "os" 是与操作系统打交道的工具包
    // 这里用它来获取命令行输入、控制程序退出
    "os"

    // "os/signal" 是操作系统的"信号监听器"
    // 信号是操作系统发给程序的消息，比如 Ctrl+C 就是发送"请退出"信号
    "os/signal"

    // "runtime/debug" 是 Go 运行时的"调试工具"
    // 这里用它来设置内存上限，避免程序吃掉太多内存导致电脑卡死
    "runtime/debug"

    // "syscall" 是底层的"系统调用"工具箱
    // 这里用它来识别 SIGINT（Ctrl+C 中断信号）和 SIGTERM（终止信号）
    "syscall"

    // 以下是我们自己写的项目内部模块
    // 每个模块负责一块独立的功能，就像汽车的不同零件
    "cronix/internal/config"   // 配置管理：读写 config.yaml 配置文件
    "cronix/internal/database" // 数据库管理：打开/关闭数据库，建表
    "cronix/internal/handler"  // HTTP 请求处理器：接收网页请求并返回数据
    "cronix/internal/router"   // 路由设置：定义"哪个 URL 地址访问哪个页面"
    "cronix/internal/scheduler" // 调度引擎：管理所有任务什么时候该跑
    "cronix/internal/service"  // 业务逻辑层：处理任务和日志的实际操作

    // 以下是第三方开源库
    // zerolog 是一个高性能的日志输出库
    // 日志就是程序的"日记本"，记录程序干了什么事
    "github.com/rs/zerolog"
    // zerolog/log 是零配置的全局日志子包，直接用 log.Info() 等函数写日志
    "github.com/rs/zerolog/log"
    // cobra 是最流行的 Go 命令行框架
    // 能自动生成帮助信息、解析参数、管理子命令
    "github.com/spf13/cobra"
)

// Execute 是暴露给 main.go 的唯一入口函数
// main.go 调用它来启动整个命令行系统
// 返回值：
//   error = nil 表示命令执行成功
//   error != nil 表示出错了，main.go 会把这个错误打印到屏幕上
func Execute() error {
    // rootCmd.Execute() 让 cobra 框架开始工作
    // 它会解析你在命令行输入的所有参数
    // 然后找到对应的子命令，执行那个子命令的处理函数
    return rootCmd.Execute()
}

// 以下是用 var 声明的"包级变量"（整个 cmd 包都可以用的变量）
// 它们像贴在包里的便签条，包里的任何函数都能看到和使用
var (
    // configPath 存储配置文件的路径
    // 用户在命令行可以用 --config 或 -c 来指定
    // 默认值是 "config.yaml"（当前目录下的配置文件）
    configPath string

    // Version 编译时通过 ldflags 注入的版本号，默认 "dev"
    Version = "dev"

    // webDist 存储由 embed.go 打包进来的前端网页文件
    // embed.FS 是一个特殊的"虚拟文件夹"，里面装着编译时嵌入的所有静态文件
    // 这个变量在 embed.go 的 init() 函数中被赋值
    webDist embed.FS

    // rootCmd 是 cobra 框架的"根命令"对象
    // 它就像一个菜单的根节点，所有子命令都挂在它下面
    // cobra.Command 是一个结构体（复合数据盒子），里面定义了命令的各种属性
    rootCmd = &cobra.Command{
        // Use 是命令的名字，也就是在终端里敲的关键字
        // 例如你敲 "cronix serve"，这个 "cronix" 就对应这里的 Use
        Use: "cronix",

        // Short 是一句话介绍，会显示在帮助信息里
        Short: "高性能的定时任务管理器，用来替代传统的 crontab 工具",

        // Long 是详细介绍，在用户输入 --help 时显示
        Long: `Cronix 是一个用 Go 语言写的高性能任务调度器。
支持四种任务类型：Shell命令、HTTP请求、清理任务、健康检查。
提供 Web 网页管理界面和命令行管理工具两种操作方式。`,

        // Version 显示编译时注入的版本号（通过 ldflags -X 设置）
        Version: Version,

        // PersistentPreRunE 在所有子命令执行前运行
        // --version / --help 不需要 root 权限，cobra 内置处理会跳过此检查
        PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
            euid := os.Geteuid()
            if euid != 0 && euid != -1 { // -1 on Windows, skip check
                return fmt.Errorf("cronix 必须以 root 用户运行")
            }
            return nil
        },

        // Run 是当用户只输入 "cronix"（不带子命令）时执行的函数
        // 这里直接显示帮助信息，告诉用户有哪些子命令可以用
        Run: func(cmd *cobra.Command, args []string) {
            _ = cmd.Help() // 显示帮助信息
        },
    }
)

// init() 函数在 main() 之前自动执行
// 它负责两件事：
//   1. 配置日志输出的格式（让日志看起来清爽）
//   2. 把各个子命令挂载到根命令上（就像把按钮装到面板上）
func init() {
    // --- 第1步：配置日志输出格式 ---

    // zerolog 是一个快速的日志库
    // 这里设置日志输出到控制台（命令行窗口），并且自动加上时间戳
    // ConsoleWriter 让日志输出变成人类易读的格式（而不是机器读的 JSON 格式）
    // Out: os.Stderr 表示日志写到标准错误输出（屏幕上红色/黄色显示）
    // With().Timestamp() 表示每条日志前面都加上时间
    log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

    // --- 第2步：创建并注册子命令 ---

    // 创建 "serve" 子命令（启动服务器）
    // 用户输入 "cronix serve" 就会执行这个命令
    serveCmd := &cobra.Command{
        Use:   "serve",              // 子命令的名字
        Short: "启动 Cronix 服务器（包括网页界面和 API 接口）", // 简短说明
        Run:   runServe,             // 按下这个"按钮"后执行的函数
    }
    // Flags().StringVarP 给 serve 命令加上一个可选的参数（也叫"命令行标志"）
    // StringVarP 的意思是："字符串类型的参数，支持短参数名"
    // 参数说明：
    //   &configPath = 用户输入的值存到这个变量里
    //   "config"    = 长参数名，用户可以用 --config 指定
    //   "c"         = 短参数名，用户可以用 -c 指定
    //   "config.yaml" = 如果用户没指定，就用这个默认值
    //   "config file path" = 帮助信息中显示的说明文字
    serveCmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "配置文件的路径")
    // rootCmd.AddCommand 把 serve 命令挂到根命令下面
    rootCmd.AddCommand(serveCmd)

    // 设置 passwd 子命令的参数（passwd 命令在 passwd.go 里定义，这里是给它加参数）
    // passwdConfigPath 是 passwd.go 里声明的变量，存配置文件的路径
    passwdCmd.Flags().StringVarP(&passwdConfigPath, "config", "c", "config.yaml", "配置文件的路径")
    // 把 passwd 命令挂到根命令下面（用户输入 "cronix passwd" 设置管理员密码）
    rootCmd.AddCommand(passwdCmd)

    // 把 logs 命令挂到根命令下面（用户输入 "cronix logs" 查看执行日志）
    // logs 命令在 logs.go 里定义
    rootCmd.AddCommand(logsCmd)
}

// SetWebDist 让外部（embed.go）把打包好的前端文件传进来
// 参数 dist：编译时嵌入的前端网页文件（HTML/JS/CSS 等）
// 这个方法就像"收货窗口"，embed.go 把打包好的东西交到这里
func SetWebDist(dist embed.FS) {
    webDist = dist // 存到包级变量里，供路由设置时读取
}

// runServe 是 "cronix serve" 命令的实际执行函数
// 它是整个服务器启动的核心流程：
//   1. 加载配置文件
//   2. 检查密码是否已设置
//   3. 初始化数据库
//   4. 启动任务调度引擎
//   5. 启动任务执行器
//   6. 配置 HTTP 路由
//   7. 启动 Web 服务器
//
// 参数 cmd：当前的 cobra 命令对象（可以从中读取命令行标志）
// 参数 args：用户在命令后面输入的额外参数（这里不用）
func runServe(cmd *cobra.Command, args []string) {
    // --- 第1步：设置内存安全上限 ---
    // debug.SetMemoryLimit 告诉 Go 的垃圾回收器：
    // "内存使用不要超过 512MB，快到了就赶紧清理垃圾"
    // 这是一个软限制（不是硬限制），目的是防止程序内存泄漏（内存被吃完不释放）导致电脑崩溃
    // 512 * 1024 * 1024 = 536870912 字节 = 512 MB（兆字节）
    debug.SetMemoryLimit(512 * 1024 * 1024)

    // --- 第2步：加载配置文件 ---
    // config.Load 读取 config.yaml 文件，把里面的配置解析到 cfg 变量里
    // configPath 来自命令行的 --config 参数，默认是 "config.yaml"
    cfg, err := config.Load(configPath)
    if err != nil {
        // log.Fatal() 记录一条"致命"级别的日志，然后立即退出程序
        // .Err(err) 表示把错误详情也写进日志
        // .Msg("...") 是日志的正文
        // 这里读配置文件就失败了，说明程序没有运行的基础，只能退出
        log.Fatal().Err(err).Msg("读取配置文件失败 - 请检查 config.yaml 是否存在")
    }
    // log.Info() 记录一条普通信息级别的日志
    // .Int("port", cfg.Server.Port) 在日志里附加一个整数：端口号
    log.Info().Int("port", cfg.Server.Port).Msg("配置文件加载成功")

    // --- 第3步：检查管理员密码是否已设置 ---
    // 为了保证安全，启动服务器前必须有一个管理员密码
    // 如果密码还没设置，提示用户先用 "cronix passwd" 命令设置密码
    if cfg.Auth.Password == "" {
        log.Fatal().Msg("管理员密码未设置 - 请先执行 'cronix passwd' 命令来设置密码，然后再启动服务器")
    }

    // --- 第4步：初始化数据库 ---
    // database.Init 打开 SQLite 数据库文件，并自动建表
    // 传入数据库文件路径（从配置文件里读取）
    if err := database.Init(cfg.Database.Path); err != nil {
        log.Fatal().Err(err).Msg("数据库初始化失败")
    }
    // defer 是 Go 语言的关键字，意思是"等整个函数执行完后再做这件事"
    // 这里的 database.Close() 会在 runServe 函数结束时自动调用
    // 确保数据库连接一定被关闭，不会遗漏
    defer database.Close()
    log.Info().Str("path", cfg.Database.Path).Msg("数据库初始化完成")

    // --- 第5步：初始化任务调度引擎 ---
    // scheduler.NewEngine 创建调度引擎（管理所有定时任务的核心大脑）
    // database.DB 是全局的数据库连接，传给它以便读写任务数据
    engine := scheduler.NewEngine(database.DB)
    // engine.ReloadAll() 从数据库中读取所有已启用的任务，装载到调度引擎里
    if err := engine.ReloadAll(); err != nil {
        log.Fatal().Err(err).Msg("加载任务列表失败")
    }
    // engine.Start() 启动调度引擎的循环，它开始按照 cron 时间表触发任务
    engine.Start()
    log.Info().Msg("任务调度引擎启动成功")

    // --- 第6步：初始化任务执行器 ---
    // scheduler.NewExecutor 创建执行器（负责真正运行任务的工人池子）
    // 参数：数据库连接、配置信息、调度引擎
    exec, err := scheduler.NewExecutor(database.DB, cfg, engine)
    if err != nil {
        log.Fatal().Err(err).Msg("创建任务执行器失败")
    }

    // --- 第7步：后台启动执行器 ---
    // context.Background() 创建一个空的"上下文"（就像一张空白的任务单）
    // context.WithCancel 创建一个可以主动取消的上下文
    // cancel 是一个函数，调用它就会发出"停止"信号
    ctx, cancel := context.WithCancel(context.Background())
    // defer cancel() 确保函数结束时会发出取消信号（防止资源泄漏）
    defer cancel()
    // go 关键字创建一个新的"协程"（goroutine，Go 的轻量级线程）
    // 它让 exec.Run(ctx) 在后台运行，不阻塞主线程
    // 主线程继续往下走，启动 Web 服务器
    go exec.Run(ctx)

    // --- 第8步：初始化服务层 ---
    // 服务层封装了业务逻辑，给上层的 HTTP 处理器调用
    // TaskService 提供任务的增删改查操作
    taskSvc := &service.TaskService{DB: database.DB, Engine: engine}
    // ExecutionService 提供执行日志的查询操作
    execSvc := &service.ExecutionService{DB: database.DB}

    // --- 第9步：初始化 HTTP 请求处理器 ---
    // 每个 Handler 处理一类 HTTP 请求
    // 就像饭店里的不同服务员：有的负责点菜、有的负责上菜、有的负责结账
    authH := &handler.AuthHandler{} // 登录认证相关的请求处理
    taskH := &handler.TaskHandler{TaskSvc: taskSvc, ExecSvc: execSvc, Executor: exec} // 任务管理相关的请求处理
    logH := &handler.LogHandler{ExecSvc: execSvc} // 日志查看相关的请求处理
    groupH := &handler.GroupHandler{GroupSvc: &service.GroupService{DB: database.DB}, TaskSvc: taskSvc, Executor: exec} // 任务组管理

    // --- 第10步：配置路由并启动 HTTP 服务器 ---
    // router.SetupRouter 创建一个 Gin 框架的路由引擎
    // 把所有的 URL 地址和对应的处理函数关联起来
    // 比如 "GET /api/tasks" -> taskH.ListTasks
    // webDist 是嵌入的前端文件，用于提供网页界面
    r := router.SetupRouter(cfg, authH, taskH, logH, groupH, webDist)

    // --- 第11步：设置优雅退出 ---
    // 当用户按下 Ctrl+C 或者服务器收到终止信号时
    // 程序应该先把手头的活干完再退出，而不是硬生生中断
    // 这就是"优雅退出"（Graceful Shutdown）
    go func() {
        // make 创建一个能装操作系统信号的"管道"（channel，像一个传送带）
        // 缓冲区大小为 1，表示最多暂存一个信号
        sigCh := make(chan os.Signal, 1)
        // signal.Notify 告诉操作系统："如果有 SIGINT（Ctrl+C）或 SIGTERM（终止）信号，
        // 请把它们放到 sigCh 这个传送带上"
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
        // <-sigCh 的意思是"等着，直到传送带上有东西传过来"
        // 这一行会阻塞住，直到用户按了 Ctrl+C
        <-sigCh
        // 收到了退出信号，开始优雅关闭
        log.Info().Msg("正在优雅关闭服务器...")
        cancel()          // 1. 先发信号让执行器停止接收新任务
        engine.Stop()     // 2. 停掉调度引擎（不再触发新任务）
        exec.Shutdown()   // 3. 等待正在执行的任务完成，然后关闭执行器
        os.Exit(0)        // 4. 正常退出（退出码 0 = 一切顺利）
    }()

    // --- 第12步：判断运行模式 ---
    // 根据配置文件判断程序以什么模式运行：
    //   full      = API 和网页界面都开启（完整功能）
    //   api-only  = 只开启 API 接口，不提供网页界面（给其他程序调用）
    //   headless  = 都不开启，只跑后台任务调度（像一个安静的后台工人）
    mode := "full"
    if !cfg.Server.WebUI.Enabled && cfg.Server.API.Enabled {
        mode = "api-only"
    } else if !cfg.Server.API.Enabled {
        mode = "headless"
    }
    log.Info().Str("mode", mode).Int("port", cfg.Server.Port).Msg("服务器正在启动...")

    // --- 第13步：启动 HTTP(S) 服务器 ---
    // fmt.Sprintf(":%d", cfg.Server.Port) 把端口号拼成地址字符串
    // 例如端口号 8080 会变成 ":8080"（冒号表示监听所有网络接口）
    addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

    if cfg.Server.API.Enabled {
        // API 模式已启用，启动 HTTP 服务器
        if cfg.Server.TLS.Enabled && cfg.Server.TLS.CertFile != "" && cfg.Server.TLS.KeyFile != "" {
            // 如果配置了 TLS（加密传输），启动 HTTPS 服务器
            // HTTPS = HTTP + SSL/TLS 加密，数据在网络上传输时别人看不到内容
            // 浏览器地址栏会显示小锁图标
            log.Info().Str("mode", mode).Int("port", cfg.Server.Port).Msg("正在启动 HTTPS 加密服务器...")
            if err := r.RunTLS(addr, cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile); err != nil {
                log.Fatal().Err(err).Msg("HTTPS 服务器启动失败")
            }
        } else {
            // 没有配置 TLS，启动普通 HTTP 服务器
            log.Info().Str("mode", mode).Int("port", cfg.Server.Port).Msg("正在启动 HTTP 服务器...")
            if err := r.Run(addr); err != nil {
                log.Fatal().Err(err).Msg("HTTP 服务器启动失败")
            }
        }
    } else {
        // Headless 模式：不启动 HTTP 服务器，只跑后台调度
        // select {} 是一个空的阻塞语句，让程序"发呆"等待
        // 实际上程序会一直运行，直到收到退出信号（Ctrl+C）
        // 如果没有这行，程序会直接走到末尾然后退出
        log.Info().Msg("Headless 模式 - 不启动网页服务器，仅在后台执行定时任务")
        select {}
    }
}
