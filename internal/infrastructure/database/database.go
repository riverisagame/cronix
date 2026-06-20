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
// 💡 【大厂面试·底层原理扩展（初二小白版）】
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

    "cronix/internal/domain/model"

    // glebarez/sqlite 是一个纯 Go 语言写的 SQLite 驱动
    // "纯 Go" 的意思是它不需要 C 语言编译器（不需要 CGO）
    // 这样在任何操作系统上都能直接编译，不挑环境
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
    sqlDB, _ := db.DB()

    // 强行单连接防锁机制
    sqlDB.SetMaxOpenConns(1)

    // WAL mode: better concurrent read/write performance
    if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
        log.Warn().Err(err).Msg("failed to set WAL mode")
    }
    // NORMAL sync is safe in WAL mode, much faster than FULL
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
