// ============================================================
// internal/scheduler/engine.go - 定时任务调度引擎
//
// 这个模块是定时系统的"心脏"——它用 robfig/cron 库来管理所有定时任务。
// 工作流程：
//   1. 定时器到点 → 2. 把任务ID放入通道 → 3. DAG解析依赖 → 4. 线程池执行
// ============================================================
package scheduler

import (
    "context"     // 上下文：用来传递取消信号、超时控制
    "strings"     // 字符串处理：检查cron字段数量
    "sync"        // 并发控制：互斥锁

    "cronix/internal/model" // 数据模型：任务结构体

    "github.com/robfig/cron/v3" // robfig/cron：Go语言最流行的定时任务库
    "github.com/rs/zerolog/log"  // zerolog/log：日志记录
    "gorm.io/gorm"              // GORM：Go语言的对象关系映射库，操作数据库
)

// Engine 是定时任务调度引擎
// 它管理所有定时任务，在正确的时间触发任务执行
type Engine struct {
    cron      *cron.Cron             // robfig/cron实例：底层定时器，支持精确到秒的6字段Cron表达式
    db        *gorm.DB               // 数据库连接：用来查询有哪些任务需要调度
    triggerCh chan uint              // 触发通道：当定时器到点时，把任务ID放入这个缓冲通道
    mu        sync.Mutex             // 互斥锁：保证重新加载任务列表时不被其他操作干扰
    entryMap  map[uint]cron.EntryID  // 映射表：任务ID → Cron内部条目ID，用于删除或修改任务
}

// NewEngine 创建一个新的调度引擎
// 参数 db：数据库连接对象
// 返回值：初始化好的Engine指针
func NewEngine(db *gorm.DB) *Engine {
    return &Engine{
        cron:      cron.New(cron.WithSeconds()), // 创建Cron实例，WithSeconds()表示支持秒级定时（6字段：秒 分 时 日 月 周）
        db:        db,                            // 保存数据库连接
        triggerCh: make(chan uint, 1024),         // 创建缓冲大小为1024的通道，可以暂存1024个待处理的任务触发
        entryMap:  make(map[uint]cron.EntryID),   // 创建空的任务映射表
    }
}

// TriggerChan 返回触发通道的"只读"版本
// 返回只读通道是为了安全：外部只能读取，不能往里面写数据
func (e *Engine) TriggerChan() <-chan uint {
    return e.triggerCh
}

// Start 启动定时调度器
// 调用后，所有已注册的定时任务开始按照Cron表达式计时
func (e *Engine) Start() {
    e.cron.Start()
}

// Stop 优雅地停止定时调度器
// 返回一个上下文对象，当所有正在运行的任务都完成后，这个上下文会结束
func (e *Engine) Stop() context.Context {
    return e.cron.Stop() // Stop会等待所有正在执行的任务完成
}

// ReloadAll 从数据库重新加载所有"已启用"的任务到定时器中
// 单个任务的 cron 表达式无效时跳过并告警，不影响其他任务和服务启动
func (e *Engine) ReloadAll() error {
    // 第一步：在加锁之前先从数据库查询任务（查询数据库比较慢，不放在锁里）
    var tasks []model.Task                                      // 声明任务列表
    if err := e.db.Where("enabled = ?", true).Find(&tasks).Error; err != nil { // 查询所有enabled=true的任务
        return err                                              // 查询失败，返回错误
    }

    // 第二步：加锁，开始修改定时器
    e.mu.Lock()                                                 // 获取锁
    defer e.mu.Unlock()                                         // 函数结束时自动释放锁

    // 第三步：删除定时器中所有旧的任务条目
    for taskID, entryID := range e.entryMap {                   // 遍历映射表中的所有条目
        e.cron.Remove(entryID)                                  // 从Cron定时器中移除
        delete(e.entryMap, taskID)                              // 从映射表中删除
    }

    // 第四步：把每个启用的任务注册到定时器中
    var skipped int                                             // 跳过的无效任务计数
    for _, task := range tasks {                                // 遍历所有查询到的任务
        taskID := task.ID                                       // 保存任务ID（闭包用，避免循环变量问题）
        expr := task.CronExpr                                   // 获取原始cron表达式
        // 兼容5字段cron：缺少秒位则自动补充 "0 " 前缀
        if len(strings.Fields(expr)) == 5 {
            expr = "0 " + expr
        }
        // AddFunc 给Cron添加一个函数，到时间就执行
        entryID, err := e.cron.AddFunc(expr, func() {           // expr是定时表达式，如"0 */5 * * * *"表示每5分钟
            e.triggerCh <- taskID                               // 到点后把任务ID发到触发通道
        })
        if err != nil {
            // 单个任务无效不阻塞启动，跳过并记录告警
            log.Warn().Err(err).Str("task", task.Name).Str("cron", task.CronExpr).Msg("跳过无效cron表达式的任务")
            skipped++
            continue
        }
        e.entryMap[taskID] = entryID                            // 记录任务ID和Cron条目ID的对应关系
    }

    if skipped > 0 {
        log.Warn().Int("skipped", skipped).Int("loaded", len(tasks)-skipped).Msg("部分任务因无效cron被跳过")
    } else {
        log.Info().Int("loaded", len(tasks)).Msg("所有任务加载成功")
    }
    return nil                                                   // 加载成功
}
