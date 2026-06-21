// ============================================================
// internal/handler/task.go - 任务增删改查（CRUD）处理器
// 包含手动触发任务和依赖管理功能
// ============================================================
// 🏗️ 【架构设计·模式对比】
// RESTful API 设计规范 (Richardson 成熟度模型)：
// 本文件遵循 Level 2 成熟度（使用 HTTP 动词和资源 URI）：
// - Level 0: 只有单一端点和 POST 方法（如 SOAP/RPC）
// - Level 1: 引入资源 URI（如 /api/tasks/1），但仍滥用 POST
// - Level 2: 引入 HTTP 动词（GET/POST/PUT/DELETE 分别代表查/增/改/删）
// - Level 3 (最高级 HATEOAS): 响应中包含相关操作的超链接（本项目未采用，因会增加前端解析复杂度）。
//
// 📌 【大厂面试·核心考点】
// 面试官：RESTful 规范中 PUT 和 PATCH 的区别是什么？
// 标准答案：PUT 要求发送整个资源的完整数据（全量更新），如果字段没传，理论上应该被置空。
// 而 PATCH 是局部更新，只修改传过去的字段。本文件中的 UpdateTask 虽路由用 PUT，
// 但实际逻辑支持局部修改（只从 JSON 取有的字段），严格意义上它应该被定义为 PATCH 接口。
// ============================================================
package handler

import (
    "fmt"
    "io"
    "net/http"     // 网络请求：HTTP状态码定义
    "os"
    "path/filepath"
    "regexp"       // 正则表达式：输入校验
    "strconv"      // 字符串和数字互转：把URL参数中的字符串转成整数
    "strings"      // 字符串处理：字段拆分

    "cronix/internal/application/executor"  // 底层执行器
    "cronix/internal/domain/model"     // 本项目的数据模型：任务结构体定义
    "cronix/internal/application/scheduler"  // 本项目的调度器：定时任务执行引擎
    "cronix/internal/application/service"    // 本项目的服务层：业务逻辑处理

    "github.com/gin-gonic/gin"  // Gin框架：处理HTTP请求
)

// TaskHandler 是任务管理相关的处理器
// 
// 【小白秒懂课堂：Handler 是什么？】
// Handler 在 Web 开发中通常被称为“路由处理器”或“控制器(Controller)”。
// 它就像餐厅里的服务员：
// 1. 负责迎客（接收 HTTP 请求，比如 GET, POST）。
// 2. 听客人点菜（解析 URL 参数和 JSON 请求体）。
// 3. 把菜单递给后厨大厨（调用 Service 层的函数）。
// 4. 把做好的菜端给客人（返回 JSON 格式的 HTTP 响应，比如 200 OK）。
//
// 🔬 【底层原理·深度剖析】
// Gin 框架的中间件链与 Context 传播机制：
// c *gin.Context 是贯穿整个 HTTP 请求生命周期的核心。
// 1. Context 内部包含 Request 和 Writer，同时维护了一个 handlers 数组（即中间件+当前处理器）。
// 2. 通过 c.Next() 可以执行下一个中间件，c.Abort() 会阻止后续中间件执行。
// 3. 💀 踩坑血泪：如果在 Handler 中开启 Goroutine，绝对不能直接传递 c！
//    因为当 HTTP 响应返回后，c 会被回收到 sync.Pool 中重用，Goroutine 继续读写 c 会导致数据错乱或 Panic。
//    正确做法是传入 c.Copy() 的副本。
//
// 它持有三个依赖对象，用来完成各种操作
type TaskHandler struct {
    TaskSvc   *service.TaskService     // 任务服务：处理任务的增删改查业务逻辑
    ExecSvc   *service.ExecutionService // 执行日志服务：查询任务的运行记录
    Executor  *scheduler.Executor      // 执行器：手动触发任务时用到的底层引擎
    DaemonMon *scheduler.DaemonMonitor // 常驻守护控制器：管理 daemon 模式任务的启停
}

// validateTask 校验任务输入，返回错误信息或空串
func validateTask(t *model.Task) string {
    t.Name = strings.TrimSpace(t.Name)
    if t.Name == "" {
        return "任务名称不能为空"
    }
    if len(t.Name) > 128 {
        return "任务名称不能超过128个字符"
    }

    t.CronExpr = strings.TrimSpace(t.CronExpr)
    if t.CronExpr != "" {
        // 🛡️ 【安全攻防·漏洞防线】
        // 输入校验策略对比：
        // 1. 【正则表达式】（如本处的 cron 校验）：适合固定格式的文本。
        //    缺点是容易引发 ReDoS（正则表达式拒绝服务攻击）。防线建议：限制正则执行时间，或者避免复杂嵌套 `(a+)+`。
        // 2. 【黑名单】（如过滤 "DROP TABLE"）：极度危险！黑客总能找到变体（如 "DrOp  tABle"、URL编码）绕过。
        // 3. 【白名单】（如下方的任务类型校验）：最高安全级别！只允许已知的安全值，非预期值一律拦截。
        if ok, _ := regexp.MatchString(`^[\d\*\/\-\,\s]{9,64}$`, t.CronExpr); !ok {
            return "cron表达式格式无效"
        }
        fields := strings.Fields(t.CronExpr)
        if len(fields) < 5 || len(fields) > 6 {
            return "cron表达式需要5或6个字段（如: 0 30 * * * 或 0 0 30 * * *）"
        }
    }

    t.TaskType = strings.TrimSpace(t.TaskType)
    switch t.TaskType {
    case "shell", "http", "cleanup", "healthcheck":
        // 采用【白名单校验】防御非法类型输入
    case "":
        t.TaskType = "shell"
    default:
        return "不支持的任务类型: " + t.TaskType + "（支持: shell, http, cleanup, healthcheck）"
    }

    if t.TaskType == "http" || t.TaskType == "healthcheck" {
        t.HTTPURL = strings.TrimSpace(t.HTTPURL)
        if t.HTTPURL == "" {
            return "HTTP/Healthcheck 类型必须提供URL"
        }
    }

    if t.TimeoutSec < 1 {
        t.TimeoutSec = 300
    }
    if t.TimeoutSec > 86400 {
        return "超时时间不能超过86400秒（24小时）"
    }
    if t.TimeoutSec > 3600 {
        // 超过1小时告警但允许（可能被全局上限进一步限制）
    }
    if t.RetryCount < 0 || t.RetryCount > 100 {
        return "重试次数范围0-100"
    }

    return ""
}

// ListTasks 获取任务列表（支持分页和搜索）
// 路由：GET /api/tasks?page=1&page_size=20&search=关键词
func (h *TaskHandler) ListTasks(c *gin.Context) {
    // 从URL参数中读取页码，默认第1页
    // 💀 【踩坑血泪·反面教材】
    // strconv.Atoi 忽略错误的模式：`page, _ := strconv.Atoi(...)`
    // 真实生产事故：如果黑客恶意传入 ?page=9999999999999999999999 （超出整数范围），
    // 或者 ?page=abc，这里 Atoi 会返回 0 和 error。由于忽略了 error，page 变成了 0。
    // 虽然下方有 `if page < 1 { page = 1 }` 兜底保命，但如果不小心漏写这行兜底，
    // SQL 引擎执行 LIMIT/OFFSET 计算时就可能出现负数，导致数据库直接报错甚至进程崩溃。
    // 推荐做法：明确处理 error，如果有错直接返回 400 Bad Request，拦截恶意参数。
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))         // 字符串转整数，Atoi = ASCII to Integer
    // 从URL参数中读取每页数量，默认20条
    pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
    if pageSize > 100 {                                          // 限制每页最多100条，防止一次查太多
        pageSize = 100
    }
    if page < 1 {                                                // 页码不能小于1
        page = 1
    }
    search := c.Query("search")                                  // 读取搜索关键词（可选，用于按任务名模糊搜索）

    // 调用服务层查询任务数据，返回任务列表和总数量
    tasks, total, err := h.TaskSvc.ListTasks(page, pageSize, search)
    if err != nil {
        respondError(c, http.StatusInternalServerError, err.Error()) // 查询出错，返回500
        return
    }
    // 查询成功，返回任务列表和总数
    respondOK(c, gin.H{"items": tasks, "total": total})
}

// CreateTask 创建新任务
// 路由：POST /api/tasks
// 请求体：JSON格式的任务数据
func (h *TaskHandler) CreateTask(c *gin.Context) {
    var task model.Task                                          // 声明一个Task结构体，存放前端发来的任务数据
    if err := c.ShouldBindJSON(&task); err != nil {              // 把请求中的JSON绑定到task变量
        respondError(c, http.StatusBadRequest, err.Error()) // 数据格式错误，返回400
        return
    }
    if task.Name == "" {                                         // 任务名是必填项，不能为空
        respondError(c, http.StatusBadRequest, "task name is required")
        return
    }
    if task.TaskType == "" {                                     // 任务类型默认 shell
        task.TaskType = "shell"
    }
    // cron 不再强求：无 cron 的任务靠 group 触发或手动执行
    if msg := validateTask(&task); msg != "" {             // 输入校验
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": msg})
        return
    }
    if err := h.TaskSvc.CreateTask(&task); err != nil {          // 调用服务层保存任务到数据库
        // 📌 【大厂面试·核心考点】
        // 面试官：业务里如何防御 SQL 注入？底层原理是什么？
        // 标准答案：
        // 1. 参数化查询 (Parameterized Query)：这是防注入的根本。MySQL 驱动收到带 `?` 的预编译 SQL 后，
        //    会将 SQL 结构与数据参数分开解析。不管参数里有多少个单引号或 `DROP TABLE`，都会被纯粹当成值对待，无法修改语法树。
        // 2. ORM 层防御：底层 GORM 的 `db.Create(&task)` 默认全部使用参数化查询，杜绝了拼接 SQL 引发的注入。
        // 🚨 警惕：若在 GORM 中滥用 `db.Raw("...WHERE name = " + req.Name)` 仍会被注入。

        // ⚡ 【性能实战·生产调优】 HTTP 状态码语义规范
        // 此处创建失败如果返回 400 Bad Request，说明是客户端传参有误（例如唯一键冲突、字段超长）。
        // 4xx 代表客户端错误，5xx 代表服务端错误（如数据库连不上）。
        // 严格的 API 应该解析 err 的具体类型，如果是 DB 挂了应返回 500，不能笼统兜底为 400，影响链路监控的准确定位。
        respondError(c, http.StatusBadRequest, err.Error()) // 保存失败（比如名字重复）
        return
    }
    respondOK(c, task) // 创建成功，返回刚创建的任务
}

// GetTask 获取单个任务的详细信息
// 路由：GET /api/tasks/:id （:id 是任务编号）
func (h *TaskHandler) GetTask(c *gin.Context) {
    // 从URL路径参数中解析任务ID（uint = 无符号整数，只能是正数）
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 把字符串ID转成64位无符号整数
    task, err := h.TaskSvc.GetTask(uint(id))                    // 调用服务层查询任务
    if err != nil {
        respondError(c, http.StatusNotFound, "task not found") // 任务不存在，返回404
        return
    }
    // 安全处理：如果任务配置了HTTP认证信息，用***替换，防止泄露密码
    if task.HTTPAuthConfig != "" {
        task.HTTPAuthConfig = "***"
    }
    respondOK(c, task) // 返回任务详情
}

// UpdateTask 更新任务信息
// 路由：PUT /api/tasks/:id
// 请求体：JSON对象，包含要修改的字段（可以只传想改的字段）
func (h *TaskHandler) UpdateTask(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 获取任务ID
    var updates map[string]interface{}                           // 声明一个映射表，key是字段名，value是新值
    if err := c.ShouldBindJSON(&updates); err != nil {           // 把请求JSON绑定到映射表
        respondError(c, http.StatusBadRequest, err.Error())
        return
    }
    // 清除只读/计算字段，防止前端传入这些值被写入数据库
    delete(updates, "id")
    delete(updates, "group_name")
    delete(updates, "created_at")
    delete(updates, "updated_at")
    // 输入校验：验证每个可能更新的字段
    if name, ok := updates["name"].(string); ok {
        name = strings.TrimSpace(name)
        if name == "" {
            c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "任务名称不能为空"})
            return
        }
        updates["name"] = name
    }
    if expr, ok := updates["cron_expr"].(string); ok {
        expr = strings.TrimSpace(expr)
        if expr != "" {
            if ok, _ := regexp.MatchString(`^[\d\*\/\-\,\s]{9,64}$`, expr); !ok {
                c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "cron表达式格式无效"})
                return
            }
            fields := strings.Fields(expr)
            if len(fields) < 5 || len(fields) > 6 {
                c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "cron表达式需要5或6个字段"})
                return
            }
        }
        updates["cron_expr"] = expr
    }
    if taskType, ok := updates["task_type"].(string); ok {
        switch taskType {
        case "shell", "http", "cleanup", "healthcheck":
        default:
            c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "不支持的任务类型: " + taskType})
            return
        }
    }
    if err := h.TaskSvc.UpdateTask(uint(id), updates); err != nil { // 调用服务层更新任务
        respondError(c, http.StatusBadRequest, err.Error())
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})    // 更新成功
}

// DeleteTask 删除任务（同时删除关联的执行记录、依赖关系、通知配置）
// 路由：DELETE /api/tasks/:id
func (h *TaskHandler) DeleteTask(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 获取要删除的任务ID
    if err := h.TaskSvc.DeleteTask(uint(id)); err != nil {       // 调用服务层删除任务
        respondError(c, http.StatusInternalServerError, err.Error())
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})    // 删除成功
}

// RunTask 手动触发一次任务执行（不等待Cron定时）
// 路由：POST /api/tasks/:id/run
func (h *TaskHandler) RunTask(c *gin.Context) {
       id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 获取任务ID
       // 常驻守护任务由 DaemonMonitor 托管，不允许手动触发
       if h.TaskSvc != nil {
               if task, err := h.TaskSvc.GetTask(uint(id)); err == nil && task.RunMode == "daemon" {
                       respondError(c, http.StatusBadRequest, "常驻守护任务不允许手动触发，请使用 /daemon/start")
                       return
               }
       }
       if h.Executor != nil {                                      // 如果执行器对象存在
               h.Executor.RunTaskNow(uint(id))                         // 立即触发任务（在后台执行）
       }
       respondOKMsg(c, "manual trigger queued") // 返回"已排队"提示
}

// GetTaskLogs 获取某个任务的执行日志（分页）
// 路由：GET /api/tasks/:id/logs?page=1&page_size=20&status=success
func (h *TaskHandler) GetTaskLogs(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 获取任务ID
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))        // 页码，默认1
    pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20")) // 每页数量，默认20
    status := c.Query("status")                                 // 按状态筛选：success（成功）/ failed（失败）/ running（运行中）
    // 调用执行服务查询日志
    logs, total, err := h.ExecSvc.GetTaskLogs(uint(id), page, pageSize, status)
    if err != nil {
        respondError(c, http.StatusInternalServerError, err.Error())
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"items": logs, "total": total}})
}

// GetTaskDeps 获取某个任务的前置依赖列表
// 路由：GET /api/tasks/:id/deps
// 依赖的意思：任务A设置了依赖B，那A必须等B执行成功后才能开始
func (h *TaskHandler) GetTaskDeps(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 获取任务ID
    deps, err := h.TaskSvc.GetTaskDeps(uint(id))                // 查询该任务的依赖关系
    if err != nil {
        respondError(c, http.StatusInternalServerError, err.Error())
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": deps}) // 返回依赖列表
}

// UpdateTaskDeps 更新任务的依赖关系（先删掉旧的依赖，再添加新的）
// 路由：PUT /api/tasks/:id/deps
// 请求体：{"dep_ids": [1, 2, 3]}  — 依赖的任务ID列表
func (h *TaskHandler) UpdateTaskDeps(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 获取任务ID
    // 定义请求体的结构（这里直接写在函数内，因为只在这里用）
    var req struct {
        DepIDs []uint `json:"dep_ids"`                          // 依赖的任务ID数组，前端传递的JSON字段名为dep_ids
    }
    if err := c.ShouldBindJSON(&req); err != nil {               // 解析请求JSON
        respondError(c, http.StatusBadRequest, err.Error())
        return
    }
    if err := h.TaskSvc.UpdateTaskDeps(uint(id), req.DepIDs); err != nil { // 更新依赖关系
        respondError(c, http.StatusBadRequest, err.Error())
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})    // 更新成功
}

// GetTaskNotify 获取通知配置
func (h *TaskHandler) GetTaskNotify(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	cfg, err := h.TaskSvc.GetTaskNotify(uint(id))
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": cfg})
}

// UpdateTaskNotify 更新通知配置
func (h *TaskHandler) UpdateTaskNotify(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var cfg model.NotifyConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.TaskSvc.UpdateTaskNotify(uint(id), &cfg); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": cfg})
}

// ================================================================
// 常驻守护进程 (Daemon / Supervisor) 管理 API
// @Ref: docs/sps/plans/20260605_daemon_supervisor_feature.md | @Date: 2026-06-05
// ================================================================

// StartDaemon 手动启动一个常驻守护任务
// 路由：POST /api/tasks/:id/daemon/start
func (h *TaskHandler) StartDaemon(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    if h.DaemonMon == nil {
        c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "daemon monitor not initialized"})
        return
    }
    h.DaemonMon.StartDaemon(uint(id))
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "daemon start signal sent"})
}

// StopDaemon 手动停止一个常驻守护任务
// 路由：POST /api/tasks/:id/daemon/stop
func (h *TaskHandler) StopDaemon(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    if h.DaemonMon == nil {
        c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "daemon monitor not initialized"})
        return
    }
    h.DaemonMon.StopDaemon(uint(id))
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "daemon stop signal sent"})
}

// GetDaemonStatus 查询常驻守护任务的当前运行状态
// 路由：GET /api/tasks/:id/daemon/status
func (h *TaskHandler) GetDaemonStatus(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    if h.DaemonMon == nil {
        c.JSON(http.StatusOK, gin.H{"code": 0, "message": "daemon monitor disabled", "data": nil})
        return
    }
    state, exists := h.DaemonMon.GetDaemonState(uint(id))
    if !exists {
        respondOK(c, gin.H{"status": scheduler.DaemonStopped})
        return
    }
    respondOK(c, state)
}

// GetAllDaemonStates 批量获取所有常驻守护任务的状态
// 路由：GET /api/daemon/states
// @Ref: docs/sps/plans/20260605_daemon_supervisor_feature.md | @Date: 2026-06-05
func (h *TaskHandler) GetAllDaemonStates(c *gin.Context) {
    if h.DaemonMon == nil {
        c.JSON(http.StatusOK, gin.H{"code": 0, "message": "daemon monitor disabled", "data": make(map[string]interface{})})
        return
    }
    states := h.DaemonMon.GetAllDaemonStates()
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": states})
}

// KillTask 强制结束某个正在运行的任务执行
// 路由：POST /api/tasks/:id/kill
func (h *TaskHandler) KillTask(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    success := executor.CancelExecution(uint(id))
    if success {
        c.JSON(http.StatusOK, gin.H{"code": 0, "message": "task killed successfully"})
    } else {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "no running execution found for this task"})
    }
}

// StreamTaskLog 流式获取正在执行中的任务日志
// 路由：GET /api/tasks/:id/stream?offset=N
// offset: 从第 N 字节开始读取（0=全量），用于增量轮询
// 响应包含 daemon 状态快照（仅当任务受 DaemonMonitor 管理时）
//
// 🏗️ 【架构设计·模式对比】
// 【流式数据传输：轮询 vs SSE vs WebSocket】
// 本接口名为 Stream，但实质是【客户端增量轮询 (HTTP Polling)】：客户端每隔几秒带 offset 请求一次。
// - 缺点：大量无效的 HTTP 握手开销；频繁新建连接消耗服务端资源；实时性受轮询间隔限制。
// 生产环境流式传输优化方案对比：
// 1. SSE (Server-Sent Events): 
//    - 底层：单向通道（服务端 -> 客户端），基于普通 HTTP 协议，设置 `Content-Type: text/event-stream`。
//    - 优点：完美契合"服务端只管发日志"场景，轻量，原生支持浏览器断线重连，对 Nginx 代理友好。
// 2. WebSocket:
//    - 底层：双向全双工通信，协议升级（HTTP -> WS）。
//    - 优点：实时性极强。缺点：需要维持心跳、开发成本较高，对于单向日志推送显得大材小用。
func (h *TaskHandler) StreamTaskLog(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    offset, _ := strconv.ParseInt(c.DefaultQuery("offset", "0"), 10, 64)

    logPath := filepath.Join("data", "logs", fmt.Sprintf("exec_%d.log", id))

    fi, err := os.Stat(logPath)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "log stream not found"})
        return
    }
    fileSize := fi.Size()

    var content string
    if offset < fileSize {
        f, ferr := os.Open(logPath)
        if ferr != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to open log"})
            return
        }
        defer f.Close()
        if offset > 0 {
            f.Seek(offset, 0)
        }
        data, _ := io.ReadAll(f)
        content = string(data)
    }

    payload := gin.H{
        "content": content,
        "size":    fileSize,
    }

    // 注入 daemon 状态快照（若存在）
    if h.DaemonMon != nil {
        if state, exists := h.DaemonMon.GetDaemonState(uint(id)); exists {
            payload["daemon"] = state
        }
    }

    c.JSON(http.StatusOK, gin.H{"code": 0, "data": payload})
}
