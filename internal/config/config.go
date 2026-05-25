// ============================================================
// internal/config/config.go - 配置文件管理模块
//
// 这个文件负责读取、解析、验证和监听 config.yaml 配置文件。
// 配置文件就是给程序看的"说明书"，告诉程序：
//   - 端口号是多少（用哪个门牌号接待访客）
//   - 数据库存在哪里（数据记在哪个本子上）
//   - 最多同时跑几个任务（请几个工人）
//   - 等等各种设置
//
// YAML 是一种人类容易读的配置格式，样子像这样：
//   server:
//     port: 8080
//   database:
//     path: ./data/cronix.db
//
// 本文件使用了 viper 库来读写配置，使用 fsnotify 来监听文件变化。
// 配置文件改了，程序能自动感知到并更新设置，不用重启。
// ============================================================
package config

import (
    // "crypto/rand" 是 Go 的安全随机数生成器
    // 用它来创建 JWT 签名密钥（一把别人猜不到的"暗号"）
    // 注意：不是 math/rand（那个是伪随机，不够安全）
    "crypto/rand"

    // "encoding/hex" 是十六进制编码/解码工具
    // 把随机的二进制数据转换成可读的十六进制字符串（如 "a1b2c3..."）
    "encoding/hex"

    // "fmt" 格式化错误信息
    "fmt"

    // "sync" 是并发安全工具
    // 提供读写锁（RWMutex），防止多个线程同时改数据导致混乱
    "sync"

    // "time" 时间处理
    // 用于解析配置文件中的时间间隔（如 "30s" = 30秒）
    "time"

    // fsnotify 是文件系统变化监听库
    // 当 config.yaml 被修改保存时，它能在第一时间通知程序
    "github.com/fsnotify/fsnotify"

    // viper 是一个强大的配置文件管理库
    // 支持 YAML、JSON、TOML 等多种格式
    // 支持默认值、环境变量覆盖、热加载等高级功能
    "github.com/spf13/viper"
)

// ============================================================
// 第1组：顶层配置结构体
// 就像一个文件柜，每个抽屉对应一个配置大类
// ============================================================

// Config 是整个配置的"总文件夹"
// 它把所有小类别的配置组合在一起
// mapstructure 标签告诉 viper："YAML 里的 server 对应这个 Server 字段"
// 每个字段的类型又是一个结构体，层层嵌套，形成一棵配置树
type Config struct {
    // Server 服务器设置（端口号、是否开启加密等）
    Server ServerConfig `mapstructure:"server"`
    // Auth 登录认证设置（用户名、密码、JWT 密钥等）
    Auth AuthConfig `mapstructure:"auth"`
    // Database 数据库设置（文件路径、性能参数等）
    Database DatabaseConfig `mapstructure:"database"`
    // Executor 任务执行器设置（最多同时跑几个任务等）
    Executor ExecutorConfig `mapstructure:"executor"`
    // Log 日志设置（日志保存多久、存哪里等）
    Log LogConfig `mapstructure:"log"`
    // Notify 通知设置（任务跑完了怎么通知你）
    Notify NotifyConfig `mapstructure:"notify"`
    // CircuitBreaker 熔断器设置（防止任务反复失败拖垮系统）
    // 熔断器就像一个保险丝：连续失败太多次就暂时停止执行，等冷静期过了再试
    CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}

// ============================================================
// 第2组：服务器相关配置
// ============================================================

// ServerConfig 定义 Web 服务器的行为
type ServerConfig struct {
    // Host 绑定的IP地址，"0.0.0.0" 表示监听所有网卡，"127.0.0.1" 表示仅本地访问
    Host string `mapstructure:"host"`
    // Port 端口号：相当于服务器的"门牌号"
    // 浏览器访问 http://localhost:8080 中的 8080 就是端口号
    // 范围必须是 1-65535
    Port int `mapstructure:"port"`

    // GracefulTimeout 优雅关闭的超时时间
    // 服务器收到关闭信号后，最多等这么久来处理完手头的请求
    // 超过这个时间还没处理完就强制关闭
    GracefulTimeout time.Duration `mapstructure:"graceful_timeout"`

    // TLS 加密传输配置（HTTPS 相关）
    TLS TLSConfig `mapstructure:"tls"`

    // WebUI 网页界面开关
    WebUI WebUIConfig `mapstructure:"webui"`

    // API 接口开关
    API APIConfig `mapstructure:"api"`
}

// TLSConfig 定义 HTTPS 加密传输相关设置
// TLS = Transport Layer Security（传输层安全），让数据在网络传输时加密
type TLSConfig struct {
    // Enabled 是否开启 HTTPS（true=加密, false=不加密明文传输）
    Enabled bool `mapstructure:"enabled"`
    // CertFile SSL 证书文件路径（相当于网站的"身份证"）
    CertFile string `mapstructure:"cert_file"`
    // KeyFile SSL 私钥文件路径（相当于网站的"签名笔迹"）
    KeyFile string `mapstructure:"key_file"`
}

// WebUIConfig 网页管理界面设置
type WebUIConfig struct {
    // Enabled 是否显示网页管理界面
    // true = 浏览器可以打开管理页面
    // false = 不提供网页，只能通过命令行或 API 操作
    Enabled bool `mapstructure:"enabled"`
}

// APIConfig API 接口设置
type APIConfig struct {
    // Enabled 是否开启 REST API 接口
    // true = 其他程序可以通过 HTTP 请求调用 Cronix
    // false = 只能在本地命令行操作
    Enabled bool `mapstructure:"enabled"`
}

// ============================================================
// 第3组：认证相关配置
// ============================================================

// AuthConfig 定义登录认证相关设置
type AuthConfig struct {
    // Username 管理员用户名（默认 admin）
    Username string `mapstructure:"username"`
    // Password 加密后的密码（bcrypt 哈希值，不是明文）
    Password string `mapstructure:"password"`
    // JWTSecret JWT 签名密钥（一个随机字符串，用来签发和验证登录令牌）
    // JWT = JSON Web Token，相当于一张"临时通行证"
    // 登录成功后服务器发一个 JWT，之后的请求带上它证明"我已登录"
    JWTSecret string `mapstructure:"jwt_secret"`
}

// ============================================================
// 第4组：数据库配置
// ============================================================

// DatabaseConfig 定义 SQLite 数据库的设置
type DatabaseConfig struct {
    // Path 数据库文件存放路径（如 "data/cronix.db"）
    Path string `mapstructure:"path"`

    // WALMode 是否开启 WAL 模式（Write-Ahead Logging，预写日志）
    // WAL 模式可以让读和写同时进行，大幅提升并发性能
    // 代价是会多生成两个临时文件（-wal 和 -shm）
    WALMode bool `mapstructure:"wal_mode"`

    // BusyTimeout 数据库忙时的等待时间（毫秒）
    // 当多个操作同时想写数据库时，后来的等多久再放弃
    BusyTimeout int `mapstructure:"busy_timeout"`

    // CacheSize 内存缓存大小（KB）
    // SQLite 在内存里缓存多少数据，缓存越多查询越快但占内存也越多
    CacheSize int `mapstructure:"cache_size"`
}

// ============================================================
// 第5组：任务执行器配置
// ============================================================

// ExecutorConfig 定义任务执行的各项参数
type ExecutorConfig struct {
    // PoolSize 工人池大小：最多同时执行几个任务
    // 就像厨房里最多有几个厨师同时炒菜
    // 设为 1 的话任务就是一个一个串行执行
    PoolSize int `mapstructure:"pool_size"`

    // OutputTruncateKB 输出截断大小（KB）
    // 任务的输出如果太长，只保留前面这么多，防止撑爆数据库
    OutputTruncateKB int `mapstructure:"output_truncate_kb"`

    // MemoryLimitMB 单个任务的内存上限（MB）
    // 如果一个 shell 任务吃内存超过这个数，就强制终止它
    MemoryLimitMB int `mapstructure:"memory_limit_mb"`
}

// ============================================================
// 第6组：日志配置
// ============================================================

// LogConfig 定义系统日志的设置
type LogConfig struct {
    // Level 日志级别：debug（调试）< info（信息）< warn（警告）< error（错误）
    // 设置 info 则只记录 info 及以上级别的日志
    Level string `mapstructure:"level"`

    // File 日志文件路径（不设置则只输出到控制台）
    File string `mapstructure:"file"`

    // MaxSizeMB 单个日志文件最大体积（MB），超了会自动切割
    MaxSizeMB int `mapstructure:"max_size_mb"`

    // MaxBackups 最多保留几个旧日志文件（超过的自动删除）
    MaxBackups int `mapstructure:"max_backups"`

    // MaxAgeDays 日志文件最多保留多少天（超过的自动删除）
    MaxAgeDays int `mapstructure:"max_age_days"`

    // RetentionDays 执行日志保留天数（超时的自动清理）
    RetentionDays int `mapstructure:"retention_days"`

    // MaxRecords 最多保留多少条执行日志记录（防数据库膨胀）
    MaxRecords int `mapstructure:"max_records"`
}

// ============================================================
// 第7组：通知配置
// ============================================================

// NotifyConfig 定义全局通知设置
type NotifyConfig struct {
    // Retry 通知发送失败后的重试次数
    Retry int `mapstructure:"retry"`

    // RetryInterval 两次重试之间的等待时间
    RetryInterval time.Duration `mapstructure:"retry_interval"`
}

// ============================================================
// 第8组：熔断器配置
// ============================================================

// CircuitBreakerConfig 定义熔断器的行为参数
// 熔断器的原理：
//   如果一个任务连续失败多次，就暂时"熔断"它
//   过一段时间（冷却期）后再重新尝试
//   就像家里电器功率太大跳闸了，等一会再合上
type CircuitBreakerConfig struct {
    // FailureThreshold 失败多少次后触发熔断
    FailureThreshold int `mapstructure:"failure_threshold"`
    // CooldownSeconds 熔断后冷静多少秒再尝试恢复
    CooldownSeconds int `mapstructure:"cooldown_seconds"`
}

// ============================================================
// 全局变量（整个 config 包都能访问）
// ============================================================

var (
    // AppConfig 存储当前生效的配置（程序运行时一直使用它）
    AppConfig *Config

    // appViper 是 viper 库的实例（负责读/写配置文件）
    appViper *viper.Viper

    // configFilePath 记录当前使用的配置文件路径
    configFilePath string

    // configMu 是一把"读写锁"，保护配置的并发安全
    // RWMutex 分两种锁：读锁（多个读者可以同时读，互不干扰）
    // 和写锁（写的时候别人不能读也不能写，独占）
    configMu sync.RWMutex
)

// ============================================================
// GenerateJWTSecret - 生成一个安全的随机密钥
// ============================================================

// GenerateJWTSecret 用加密安全的随机数生成一个 256 位的十六进制字符串
// 这个字符串用作 JWT 的签名密钥（相当于一枚只有服务器知道的"公章"）
// 256 位 = 32 字节 = 64 个十六进制字符
//
// 返回值：
//   string = 生成的密钥（如 "a1b2c3d4e5f6..." 共 64 个字符）
//   error  = 如果随机数生成失败则返回错误
func GenerateJWTSecret() (string, error) {
    // make([]byte, 32) 创建一个 32 字节的"空箱子"
    // byte = 字节，计算机存储的最小单元，一个字节能存 0-255 之间的一个数
    b := make([]byte, 32)
    // rand.Read 用加密安全的方式往箱子里填入 32 个随机字节
    // 加密安全的意思是：生成的随机数无法被预测或重现
    if _, err := rand.Read(b); err != nil {
        return "", err // 失败了返回空字符串和错误
    }
    // hex.EncodeToString 把二进制的字节转换成十六进制字符串
    // 例如字节 [1, 255] 会变成字符串 "01ff"
    // 32 个字节变成 64 个字符
    return hex.EncodeToString(b), nil
}

// ============================================================
// Load - 加载并解析配置文件
// ============================================================

// Load 读取 YAML 配置文件，解析成 Config 结构体
// 同时设置默认值、自动生成 JWT 密钥、启动文件变化监听
//
// 参数 configPath：配置文件所在的路径，如 "config.yaml"
//
// 返回值：
//   *Config = 解析好的配置对象（指针，指向实际存放配置的内存地址）
//   error   = 如果加载失败，返回错误原因
func Load(configPath string) (*Config, error) {
    // --- 第1步：创建 viper 实例 ---
    // viper.New() 创建一个全新的配置管理器
    v := viper.New()
    // 告诉 viper 配置文件在哪里
    v.SetConfigFile(configPath)
    // 告诉 viper 配置文件是 YAML 格式
    v.SetConfigType("yaml")

    // --- 第2步：设置默认值（如果用户没配置就用这些）---
    v.SetDefault("server.port", 8080)                           // 默认端口 8080
    v.SetDefault("server.host", "0.0.0.0")                     // 默认监听所有网卡
    v.SetDefault("server.graceful_timeout", "30s")              // 默认优雅退出等待 30 秒
    v.SetDefault("executor.pool_size", 32)                      // 默认最多同时执行 32 个任务
    v.SetDefault("executor.output_truncate_kb", 64)             // 默认输出截断到 64KB

    // --- 第3步：读取配置文件 ---
    _ = v.ReadInConfig() // 文件不存在或损坏不阻塞启动，用默认值兜底

    // --- 第4步：把原始数据"翻译"成 Go 的结构体 ---
    var cfg Config
    // 文件损坏可能导致 Unmarshal 失败，不阻塞
    _ = v.Unmarshal(&cfg)

    // --- 第4.5步：填充关键字段的兜底默认值 ---
    if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
        cfg.Server.Port = 8080
    }
    if cfg.Server.Host == "" {
        cfg.Server.Host = "0.0.0.0"
    }
    if cfg.Server.GracefulTimeout <= 0 {
        cfg.Server.GracefulTimeout = 30 * time.Second
    }
    if cfg.Executor.PoolSize <= 0 || cfg.Executor.PoolSize > 4096 {
        cfg.Executor.PoolSize = 32
    }
    if cfg.Executor.OutputTruncateKB <= 0 {
        cfg.Executor.OutputTruncateKB = 64
    }
    if cfg.Database.Path == "" {
        cfg.Database.Path = "./data/cronix.db"
    }
    if cfg.Log.RetentionDays <= 0 {
        cfg.Log.RetentionDays = 30
    }
    if cfg.Log.MaxRecords <= 0 {
        cfg.Log.MaxRecords = 100000
    }
    if cfg.CircuitBreaker.FailureThreshold <= 0 {
        cfg.CircuitBreaker.FailureThreshold = 5
    }
    if cfg.CircuitBreaker.CooldownSeconds <= 0 {
        cfg.CircuitBreaker.CooldownSeconds = 60
    }
    if cfg.Notify.Retry < 0 {
        cfg.Notify.Retry = 3
    }
    if cfg.Notify.RetryInterval <= 0 {
        cfg.Notify.RetryInterval = 5 * time.Second
    }
    if cfg.Server.API.Enabled == false && cfg.Server.WebUI.Enabled == false {
        // 如果都没配，默认全开
        cfg.Server.WebUI.Enabled = true
        cfg.Server.API.Enabled = true
    }
    if cfg.Auth.Username == "" {
        cfg.Auth.Username = "admin"
    }

    // --- 第5步：如果没有 JWT 密钥，自动生成一个 ---
    if cfg.Auth.JWTSecret == "" {
        secret, err := GenerateJWTSecret()
        if err != nil {
            // JWT 密钥生成失败不致命，用回退方案
            cfg.Auth.JWTSecret = "auto-generated-fallback-key-change-me"
        } else {
            cfg.Auth.JWTSecret = secret
        }
        v.Set("auth.jwt_secret", cfg.Auth.JWTSecret)
        // 保存回配置文件（下次启动就不用再生成了），失败不阻塞
        _ = v.WriteConfig()
    }

    // --- 第6步：开启配置文件变化监听 ---
    // v.WatchConfig() 让 viper 监听配置文件的变化
    // 你在外面用记事本改了 config.yaml，程序能立刻感知到
    v.WatchConfig()
    // v.OnConfigChange 注册一个回调函数：当文件变化时执行
    v.OnConfigChange(func(e fsnotify.Event) {
        // 加写锁，防止读配置的过程中配置被修改
        configMu.Lock()
        // defer 确保函数退出时一定解锁
        defer configMu.Unlock()

        // 重新解析配置
        var newCfg Config
        if err := v.Unmarshal(&newCfg); err == nil {
            // 验证新配置
            if err := newCfg.Validate(); err == nil {
                // 验证通过，替换当前配置
                // *AppConfig = newCfg 把 newCfg 的内容复制到 AppConfig 指向的地址
                *AppConfig = newCfg
            }
            // 如果解析或验证失败，静默忽略（保留旧配置继续用）
        }
    })

    // --- 第8步：保存到全局变量并返回 ---
    AppConfig = &cfg        // 保存配置对象到全局变量
    appViper = v           // 保存 viper 实例到全局变量
    configFilePath = configPath // 记录配置文件路径
    return &cfg, nil       // 返回配置对象的指针
}

// ============================================================
// Validate - 检查配置值是否在合理范围内
// ============================================================

// Validate 检查配置的各个字段是否合法
// 比如端口号必须在 1-65535 之间，工人池不能是 0
func (c *Config) Validate() error {
    // 端口号范围：1 到 65535
    // 1024 以下的端口通常被系统服务占用，但技术上可以用
    if c.Server.Port < 1 || c.Server.Port > 65535 {
        return fmt.Errorf("端口号不合法: %d（端口号必须在 1 到 65535 之间）", c.Server.Port)
    }
    // 工人池大小范围：1 到 4096
    // 至少 1 个（不然没人干活），最多 4096 个（避免资源耗尽）
    if c.Executor.PoolSize < 1 || c.Executor.PoolSize > 4096 {
        return fmt.Errorf("工人池大小不合法: %d（必须在 1 到 4096 之间）", c.Executor.PoolSize)
    }
    // 输出截断至少 1KB（不能为 0 或负数）
    if c.Executor.OutputTruncateKB < 1 {
        return fmt.Errorf("输出截断大小不合法: %d", c.Executor.OutputTruncateKB)
    }
    // 熔断阈值至少 1 次（不能为 0）
    if c.CircuitBreaker.FailureThreshold < 1 {
        return fmt.Errorf("熔断器失败阈值不合法: %d", c.CircuitBreaker.FailureThreshold)
    }
    return nil // 全部通过检查
}

// ============================================================
// GetJWTSecret - 获取 JWT 签名密钥（线程安全）
// ============================================================

// GetJWTSecret 返回当前生效配置里的 JWT 签名密钥
// 使用读锁保护，可以多个 goroutine 同时调用而不冲突
func GetJWTSecret() string {
    // RLock 加读锁（允许多人同时读）
    configMu.RLock()
    // defer RUnlock 确保函数结束时解锁
    defer configMu.RUnlock()
    return AppConfig.Auth.JWTSecret
}

// ============================================================
// IsPasswordSet - 检查管理员密码是否已设置
// ============================================================

// IsPasswordSet 检查配置里是否有密码
// 用于在启动服务器前确保密码已设置
func IsPasswordSet() bool {
    configMu.RLock()
    defer configMu.RUnlock()
    // 密码不为空字符串说明已经设置过了
    return AppConfig.Auth.Password != ""
}

// ============================================================
// SaveConfig - 保存运行时修改的配置
// ============================================================

// SaveConfig 把当前内存中的配置写回 config.yaml 文件
// 只保存那些允许在运行时修改的字段（如工人池大小）
// 像 JWT 密钥这种启动时生成的就不要覆盖
func SaveConfig() error {
    // 加读锁（因为只是读 AppConfig，不修改它）
    configMu.RLock()
    defer configMu.RUnlock()

    // 如果 viper 还没初始化，返回错误
    if appViper == nil {
        return fmt.Errorf("配置系统还未初始化")
    }

    // 把允许运行时修改的配置项写回 viper
    appViper.Set("executor.pool_size", AppConfig.Executor.PoolSize)
    appViper.Set("executor.output_truncate_kb", AppConfig.Executor.OutputTruncateKB)
    appViper.Set("log.retention_days", AppConfig.Log.RetentionDays)
    appViper.Set("log.max_records", AppConfig.Log.MaxRecords)
    appViper.Set("circuit_breaker.failure_threshold", AppConfig.CircuitBreaker.FailureThreshold)
    appViper.Set("circuit_breaker.cooldown_seconds", AppConfig.CircuitBreaker.CooldownSeconds)

    // 写入磁盘文件
    return appViper.WriteConfig()
}
