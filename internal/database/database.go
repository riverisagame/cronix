// ============================================================
// internal/database/database.go - 数据库初始化模块
//
// 这个文件负责跟数据库打交道的前期准备工作：
//   1. 创建数据库连接的文件夹（如果还没有的话）
//   2. 打开数据库文件（如果文件不存在会自动创建）
//   3. 设置数据库连接参数（让数据库又稳又快）
//   4. 根据代码里的结构体定义自动创建/更新数据库表
//   5. 程序结束时安全关闭数据库，防止数据损坏
//
// 这里用的是 SQLite 数据库，它的特点是：
//   - 轻量：所有数据存在一个文件里（比如 cronix.db）
//   - 零配置：不需要安装额外的数据库服务，打开文件就能用
//   - 嵌入式：数据库引擎直接嵌在程序里，不依赖外部服务
//
// GORM 是一个"翻译官"，让你用 Go 的结构体操作数据库，
// 不用手写 SQL 语句。比如 db.Find(&tasks) 就相当于
// SELECT * FROM tasks 这句话。
// ============================================================
package database

import (
    // "fmt" 格式化错误信息，让报错更好读
    "fmt"

    // "os" 操作系统工具，用来创建文件夹
    "os"

    // "path/filepath" 处理文件路径的工具
    // 比如从 "./data/cronix.db" 中提取出 "./data" 目录部分
    "path/filepath"

    // 导入我们自己定义的数据模型（数据库里每张表对应一个模型结构体）
    // 就像建筑蓝图，GORM 照着蓝图建表
    "cronix/internal/model"

    // glebarez/sqlite 是一个纯 Go 语言写的 SQLite 驱动
    // "纯 Go" 的意思是它不需要 C 语言编译器（不需要 CGO）
    // 这样在任何操作系统上都能直接编译，不挑环境
    // 而且编译出来的程序体积更小、部署更简单
    "github.com/glebarez/sqlite"
    "github.com/rs/zerolog/log"

    // GORM 是一个"对象关系映射"（Object-Relational Mapping, ORM）库
    // 它的作用是把 Go 的结构体自动翻译成数据库表
    // 把结构体的字段自动翻译成表的列
    // 你操作 Go 对象，GORM 自动帮你生成对应的数据库语句
    "gorm.io/gorm"

    // GORM 的日志子包，控制数据库操作日志的输出级别
    // 比如设置为 Warn 级别，就只在警告和错误时才输出
    "gorm.io/gorm/logger"
)

// ============================================================
// 全局变量
// ============================================================

// DB 是整个程序唯一的数据库连接入口
// 就像一个"总机号码"，程序里任何地方想操作数据库都通过它
// *gorm.DB 是一个指针，指向内存中实际的数据库连接对象
// 变量名大写开头表示它可以被其他包访问（Go 的导出规则）
var DB *gorm.DB

// ============================================================
// Init 函数 - 初始化数据库
// ============================================================

// Init 负责打开 SQLite 数据库，配置参数，并自动创建表结构
//
// 参数 dbPath: 数据库文件的完整路径
//   例如 "C:\myapp\data\cronix.db" 或 "./data/cronix.db"
//   如果文件不存在，SQLite 会自动创建一个空文件
//
// 返回值 error:
//   nil   = 一切顺利，数据库已就绪
//   非 nil = 出错了，调用方应该处理这个错误
func Init(dbPath string) error {
    // --- 第1步：确保存放数据库文件的文件夹存在 ---

    // filepath.Dir 从一个文件路径中提取出"目录"部分
    // 例如：filepath.Dir("./data/cronix.db") 返回 "./data"
    // 就好像从地址 "北京路100号3楼" 中提取 "北京路100号"
    dir := filepath.Dir(dbPath)

    // os.MkdirAll 递归创建目录（如果目录链中有任何一层不存在，全部创建）
    // 第二个参数 0755 是 Unix 风格的权限码（Windows 上会被忽略）：
    //   7 = 所有者可以读(4)+写(2)+执行(1)
    //   5 = 同组用户可以读(4)+执行(1)
    //   5 = 其他用户可以读(4)+执行(1)
    if err := os.MkdirAll(dir, 0755); err != nil {
        // %w 把原始错误包装起来，链式保留完整的错误信息
        return fmt.Errorf("创建数据目录失败: %w", err)
    }

    // --- 第2步：打开数据库连接 ---

    // gorm.Open 是 GORM 框架的"开门"函数
    // 第一个参数：sqlite.Open(dbPath) 打开 SQLite 数据库
    //   - 如果 dbPath 文件存在，就打开它
    //   - 如果不存在，SQLite 自动创建一个空文件
    // 第二个参数：&gorm.Config{...} 是给 GORM 本身的设置
    //   Logger 设置数据库操作的日志级别
    //   logger.Default 用默认的日志配置
    //   .LogMode(logger.Warn) 只记录"警告"和"错误"级别的日志
    //   这样正常的查询语句就不会刷屏，只有出了问题才显示
    db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Warn),
    })
    if err != nil {
        return fmt.Errorf("打开数据库失败: %w", err)
    }

    // --- 第3步：检查数据库完整性 ---
    var integrityResult string
    if err := db.Raw("PRAGMA integrity_check").Scan(&integrityResult).Error; err != nil {
        log.Warn().Err(err).Msg("数据库完整性检查执行失败")
    } else if integrityResult != "ok" {
        log.Warn().Str("result", integrityResult).Msg("数据库可能损坏，请备份后重建")
    }

    // --- 第4步：配置数据库连接池 ---

    // db.DB() 方法从 GORM 的包装中取出底层的 *sql.DB 对象
    // GORM 是高级封装，*sql.DB 是 Go 标准库的底层数据库连接
    sqlDB, _ := db.DB()

    // SetMaxOpenConns(1) 设置最大同时打开的连接数为 1
    // 为什么要设为 1？
    // SQLite 是基于文件的数据库，不像 MySQL/PostgreSQL 有独立的服务进程
    // 多个连接同时写 SQLite 会导致 "database is locked"（数据库被锁定）错误
    // 设为 1 保证同一时间只有一个操作在写数据库，避免锁冲突
    // 虽然只有一个连接，但对 Cronix 这种场景（任务调度）来说性能足够了
    sqlDB.SetMaxOpenConns(1)

    // --- 第4步：自动建表/更新表结构（AutoMigrate）---

    // AutoMigrate 是 GORM 提供的一个非常方便的功能
    // 它会自动检查数据库中的表是否与 Go 结构体定义一致：
    //   - 表不存在 --> 创建新表
    //   - 表中缺少某个列 --> 添加新列
    //   - 索引不存在 --> 创建索引
    // 但是它不会：
    //   - 删除已有的列（怕数据丢失，安全设计）
    //   - 修改列的类型（可能导致数据损坏）
    //
    // &model.Task{} 是一个空的 Task 结构体的地址
    // GORM 用它来推断表名（tasks）和列定义
    if err := db.AutoMigrate(
        &model.Task{},         // 任务表（存每个定时任务的配置）
        &model.TaskDep{},      // 任务依赖关系表（存"A 依赖 B"这种关系）
        &model.TaskGroup{},    // 任务组表（存任务分组和执行模式）
        &model.ExecutionLog{}, // 执行日志表（存每次任务执行的历史记录）
        &model.NotifyConfig{}, // 通知配置表（存每个任务的通知设置）
    ); err != nil {
        return fmt.Errorf("自动建表失败: %w", err)
    }

    // --- 第5步：把连接对象保存到全局变量 ---

    // 赋值给包级变量 DB，这样程序任何地方导入 database 包后
    // 都可以用 database.DB 来操作数据库
    DB = db

    // 返回 nil 表示一切正常，没有错误
    return nil
}

// ============================================================
// Close 函数 - 安全关闭数据库连接
// ============================================================

// Close 负责安全地关闭数据库连接
// 应该在程序退出前调用，确保所有数据都完整写入磁盘
//
// 返回值 error:
//   nil   = 关闭成功
//   非 nil = 关闭过程中出了错
func Close() error {
    // 先检查 DB 是否已经被初始化（可能是某个测试分支没调用 Init）
    if DB != nil {
        // 从 GORM 中取出底层的 sql.DB
        sqlDB, err := DB.DB()
        if err != nil {
            return err
        }
        // sqlDB.Close() 关闭连接池里的所有连接
        // 同时确保 SQLite 的 WAL 日志被完整写入磁盘
        return sqlDB.Close()
    }
    // DB 是 nil，说明从未初始化过，没有需要关闭的东西
    return nil
}
