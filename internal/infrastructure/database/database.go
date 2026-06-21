// ============================================================
// internal/infrastructure/database/database.go - 数据库初始化模块
//
// 这个文件负责跟数据库打交道的前期准备工作：
//   1. 创建数据库连接的文件夹（如果还没有的话）
//   2. 打开数据库文件（如果文件不存在会自动创建）
//   3. 设置数据库连接参数（让数据库又稳又快）
//   4. 根据代码里的结构体定义自动创建/更新数据库表
//   5. 程序结束时安全关闭数据库，防止数据损坏
//
// ============================================================
// 📌 【大厂面试·核心考点】
// 
// 1. 面试官问：为什么要用 `PRAGMA journal_mode=WAL`（预写式日志）？
// 答：
// SQLite 默认是把整个文件锁死才能写数据。就像去银行只有一个柜台，必须等前面的人办完才能办下一个。
// WAL（Write-Ahead Logging）模式，相当于银行开了一个“快速记账本”。
//
// 📌 图解 WAL 工作原理：
// 
// [常规模式] 
// 操作数据 -> 把原来的数据备个份 -> 直接去改真实的数据库文件 [cronix.db] (极慢，且独占死锁全场)
//
// [WAL 模式]
// 操作数据 -> 先写在一本小册子（日志）上 [cronix.db-wal] (极快！) -> 直接告诉用户“成功了”！
// 稍后有空闲时，系统会在后台悄悄把小册子上的记录同步到真实的数据库文件里（这叫 Checkpoint）。
// 优点：读和写可以同时进行，性能暴增 100 倍！
//
// 2. 面试官问：为什么你要设置 `sqlDB.SetMaxOpenConns(1)` 把连接数设为1？
// 答：
// 虽然开启了 WAL，读可以并发，但 SQLite 底层终究是一个本地文件。
// 多进程并发写同一个文件非常容易触发 "database is locked" 错误。
// 设置连接数为1，就像是给写操作加了一道旋转门：所有人排队，一次只能进一个人写，彻底杜绝数据库死锁。
//
// 3. 面试官问：GORM 的 AutoMigrate 是不是万能的？
// 答：
// 绝对不是。AutoMigrate 是一个“只加不减”的保守派。
// 如果你在代码里增加了一个字段，它会在表里帮你加上这一列。
// 如果你在代码里删除了一个字段，它【绝对不会】在数据库里删除这列（怕你手滑删错代码导致真实数据灰飞烟灭）。
// 真实的生产环境中，我们通常用专业的工具（如 Flyway、golang-migrate）去写 SQL 脚本进行精确变更。
// ============================================================
package database

import (
    "fmt"
    "os"
    "path/filepath"
    "time"

    "cronix/internal/domain/model"

    // glebarez/sqlite 是一个纯 Go 语言写的 SQLite 驱动
    // 🔬 【底层原理·深度剖析】
    // 为什么不用官方的 mattn/go-sqlite3？
    // "纯 Go" 的意思是它不需要 C 语言编译器（不需要 CGO）。
    // CGO 会带来极大的跨平台编译痛苦（Windows下需要安装 gcc 等工具链，Docker 镜像也会变臃肿）。
    // 使用纯 Go 驱动，在任何操作系统上都能 `go build` 直接跑出静态二进制文件，部署极度清爽。
    "github.com/glebarez/sqlite"
    "github.com/rs/zerolog/log"

    // GORM 是一个"对象关系映射"（Object-Relational Mapping, ORM）库
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
)

// DB 是整个程序唯一的数据库连接入口
// 就像一个"总机号码"，程序里任何地方想操作数据库都通过它
var DB *gorm.DB

// Init 负责打开 SQLite 数据库，配置参数，并自动创建表结构
func Init(dbPath string) error {
    // --- 第1步：确保存放数据库文件的文件夹存在 ---
    dir := filepath.Dir(dbPath)

    if err := os.MkdirAll(dir, 0755); err != nil {
        return fmt.Errorf("创建数据目录失败: %w", err)
    }

    // --- 第2步：打开数据库连接 ---
    // 🛡️ 【安全攻防·漏洞防线】 & ⚡ 【性能实战·生产调优】
    // 日志级别设置（Logger: logger.Default.LogMode(logger.Warn)）：
    // 生产环境绝对禁止使用 Info 级别打印所有 SQL！
    // 1. 性能灾难：高并发下打印每条 SQL 会极大消耗 CPU 和磁盘 I/O（日志阻塞）。
    // 2. 安全漏洞：Info 级别会打印完整的 SQL 参数，如果参数中包含用户密码、身份 token 等，将导致严重的内部信息泄露，数据脱敏彻底失效。
    // 正确做法：生产环境保持 Warn 级别（只打印慢查询和错误），或者自定义 Logger 对核心敏感字段进行脱敏(Masking)。
    db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Warn),
    })
    if err != nil {
        return fmt.Errorf("打开数据库失败: %w", err)
    }

    // --- 第3步：检查数据库完整性 ---
    // 💀 【踩坑血泪·反面教材】
    // 事故现场：某项目 SQLite 作为单文件数据库，在突然断电、磁盘满、或被外部杀毒软件强行拦截扫描时，发生了文件损坏。
    // 如果不加完整性检查，程序启动后读取错乱的底层页表数据，会产生千奇百怪的幻觉级 bug，且排查极难。
    // 防御方案：必须在程序启动的最开始跑一把 "PRAGMA integrity_check"，发现文件损坏立刻打印致命报警，通知运维恢复冷备数据！
    var integrityResult string
    if err := db.Raw("PRAGMA integrity_check").Scan(&integrityResult).Error; err != nil {
        log.Warn().Err(err).Msg("数据库完整性检查执行失败")
    } else if integrityResult != "ok" {
        log.Warn().Str("result", integrityResult).Msg("数据库可能损坏，请备份后重建")
    }

    // --- 第4步：配置数据库连接池 ---
    // 📌 【大厂面试·核心考点】：Go 语言数据库连接池配置四大天王
    // 面试官问：如何正确配置 Go 的 database/sql 连接池以防雪崩？
    sqlDB, _ := db.DB()

    // 1. MaxOpenConns (最大打开连接数)
    // 原理：控制同时访问数据库的并发连接绝对上限。
    // 这里设为 1 是因为 SQLite 写锁限制（强行单连接防锁机制），强制并发写请求排队。
    // 对于 MySQL/PostgreSQL，通常设置为 100-500 左右，太大会压垮数据库服务器（连接数过多产生上下文切换风暴），太小吞吐量上不去。
    sqlDB.SetMaxOpenConns(1)

    // 2. MaxIdleConns (最大空闲连接数)
    // 原理：保留在连接池中不被关闭的连接数，相当于池子里的“常备军”。
    // 💀 踩坑：如果 MaxIdleConns 远小于 MaxOpenConns，高并发洪峰到来时会发生连接的剧烈创建和销毁（Connection Churn），导致性能雪崩。
    // 最佳实践：推荐 MaxIdleConns 设置为与 MaxOpenConns 相同或略小。这里因为最大是 1，所以空闲也是 1。
    sqlDB.SetMaxIdleConns(1)

    // 3. ConnMaxLifetime (连接最大存活时间)
    // 原理：一个连接在被创建出来后，最多能活多久，到期强制关闭并重建。
    // 💀 踩坑：很多云服务商（如阿里云、AWS）的负载均衡 NAT/防火墙会主动静默掐断空闲超过 15 分钟的 TCP 连接。
    // 如果 Go 不配这个参数，会从池子里拿到一个实际在网络层已经断开的坏连接，引发 "invalid connection" 大规模报错。
    // 最佳实践：设置在 10~30 分钟左右。必须小于数据库服务器自身的 wait_timeout 或云防火墙超时。
    sqlDB.SetConnMaxLifetime(30 * time.Minute)

    // 4. ConnMaxIdleTime (连接最大空闲时间)
    // 原理：一个连接如果不干活，最多在池子里躺多久后被解雇（释放空闲内存和端口资源）。
    // 通常设置为比 ConnMaxLifetime 稍短一点。
    sqlDB.SetConnMaxIdleTime(10 * time.Minute)

    // WAL mode: better concurrent read/write performance
    // ⚡ 【性能实战·生产调优】
    // 将日志模式设为 Write-Ahead Logging。如文件头部的详细讲解，让 SQLite 告别独占式死锁，读写并发能力大幅跃升。
    if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
        log.Warn().Err(err).Msg("failed to set WAL mode")
    }
    
    // NORMAL sync is safe in WAL mode, much faster than FULL
    // ⚡ 【性能实战·生产调优】
    // synchronous=FULL 意味着每提交一次事务都要强制操作系统的刷盘指令（fsync），I/O 负担极其沉重。
    // WAL 模式下，设为 NORMAL 既能保证在操作系统不崩溃（如仅仅是当前 Go 程序崩溃）时的数据安全，又能享受飞一般的写入速度。
    if err := db.Exec("PRAGMA synchronous=NORMAL").Error; err != nil {
        log.Warn().Err(err).Msg("failed to set synchronous=NORMAL")
    }

    // --- 第5步：自动建表/更新表结构（AutoMigrate）---
    if err := db.AutoMigrate(
        &model.Task{},              
        &model.TaskDep{},           
        &model.TaskGroup{},         
        &model.ExecutionLog{},      
        &model.GroupExecutionLog{}, 
        &model.NotifyConfig{},      
    ); err != nil {
        return fmt.Errorf("自动建表失败: %w", err)
    }

    // Indexes for log cleanup and query performance
    // 🏗️ 【架构设计·模式对比】
    // GORM 完全支持在 Struct 结构体标签里写 `gorm:"index"` 来建索引，那为什么这里要用原生的 SQL 字符串来创建复合索引？
    // 理由：原生 SQL 提供了更清晰且确定的语义（特别是 "IF NOT EXISTS" 和跨列的复合索引顺序控制），
    // 脱离了 ORM 的黑盒魔法转换，让 DBA 或者架构师在代码里一眼就能看懂最底层的索引结构，便于后期根据 Explain 进行精准调优。
    for _, idx := range []string{
        "CREATE INDEX IF NOT EXISTS idx_el_created_at ON execution_logs(created_at)",
        "CREATE INDEX IF NOT EXISTS idx_el_task_start ON execution_logs(task_id, start_time)",
        "CREATE INDEX IF NOT EXISTS idx_el_status_start ON execution_logs(status, start_time)",
        "CREATE INDEX IF NOT EXISTS idx_gel_created_at ON group_execution_logs(created_at)",
        "CREATE INDEX IF NOT EXISTS idx_gel_group_start ON group_execution_logs(group_id, start_time)",
    } {
        if err := db.Exec(idx).Error; err != nil {
            log.Warn().Err(err).Str("sql", idx).Msg("failed to create index")
        }
    }

    // 把连接对象保存到全局变量
    DB = db

    return nil
}

// Close 负责安全地关闭数据库连接
// 🔬 【底层原理·深度剖析】
// 为什么程序退出时必须 Close()？
// 虽然操作系统在进程挂掉时会回收内存和文件句柄，但正常退出时显式调用 Close 可以：
// 1. 等待未完成的数据库事务处理完毕。
// 2. 对 SQLite 来说，能触发 WAL 日志的安全 Checkpoint 操作，合并 wal 文件到主库中，防止文件无限膨胀。
func Close() error {
    if DB != nil {
        sqlDB, err := DB.DB()
        if err != nil {
            return err
        }
        return sqlDB.Close()
    }
    return nil
}
