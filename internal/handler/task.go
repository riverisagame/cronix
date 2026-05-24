// ============================================================
// internal/handler/task.go - 任务增删改查（CRUD）处理器
// 包含手动触发任务和依赖管理功能
// ============================================================
package handler

import (
    "net/http"     // 网络请求：HTTP状态码定义
    "strconv"      // 字符串和数字互转：把URL参数中的字符串转成整数

    "cronix/internal/model"     // 本项目的数据模型：任务结构体定义
    "cronix/internal/scheduler"  // 本项目的调度器：定时任务执行引擎
    "cronix/internal/service"    // 本项目的服务层：业务逻辑处理

    "github.com/gin-gonic/gin"  // Gin框架：处理HTTP请求
)

// TaskHandler 是任务管理相关的处理器
// 它持有三个依赖对象，用来完成各种操作
type TaskHandler struct {
    TaskSvc   *service.TaskService     // 任务服务：处理任务的增删改查业务逻辑
    ExecSvc   *service.ExecutionService // 执行日志服务：查询任务的运行记录
    Executor  *scheduler.Executor      // 执行器：手动触发任务时用到的底层引擎
}

// ListTasks 获取任务列表（支持分页和搜索）
// 路由：GET /api/tasks?page=1&page_size=20&search=关键词
func (h *TaskHandler) ListTasks(c *gin.Context) {
    // 从URL参数中读取页码，默认第1页
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
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()}) // 查询出错，返回500
        return
    }
    // 查询成功，返回任务列表和总数
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"items": tasks, "total": total}})
}

// CreateTask 创建新任务
// 路由：POST /api/tasks
// 请求体：JSON格式的任务数据
func (h *TaskHandler) CreateTask(c *gin.Context) {
    var task model.Task                                          // 声明一个Task结构体，存放前端发来的任务数据
    if err := c.ShouldBindJSON(&task); err != nil {              // 把请求中的JSON绑定到task变量
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()}) // 数据格式错误，返回400
        return
    }
    if task.Name == "" {                                         // 任务名是必填项，不能为空
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "task name is required"})
        return
    }
    if task.CronExpr == "" {                                     // Cron表达式是必填项，决定任务的执行时间
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "cron expression is required"})
        return
    }
    if err := h.TaskSvc.CreateTask(&task); err != nil {          // 调用服务层保存任务到数据库
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()}) // 保存失败（比如名字重复）
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": task}) // 创建成功，返回刚创建的任务
}

// GetTask 获取单个任务的详细信息
// 路由：GET /api/tasks/:id （:id 是任务编号）
func (h *TaskHandler) GetTask(c *gin.Context) {
    // 从URL路径参数中解析任务ID（uint = 无符号整数，只能是正数）
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 把字符串ID转成64位无符号整数
    task, err := h.TaskSvc.GetTask(uint(id))                    // 调用服务层查询任务
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "task not found"}) // 任务不存在，返回404
        return
    }
    // 安全处理：如果任务配置了HTTP认证信息，用***替换，防止泄露密码
    if task.HTTPAuthConfig != "" {
        task.HTTPAuthConfig = "***"
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": task}) // 返回任务详情
}

// UpdateTask 更新任务信息
// 路由：PUT /api/tasks/:id
// 请求体：JSON对象，包含要修改的字段（可以只传想改的字段）
func (h *TaskHandler) UpdateTask(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 获取任务ID
    var updates map[string]interface{}                           // 声明一个映射表，key是字段名，value是新值
    if err := c.ShouldBindJSON(&updates); err != nil {           // 把请求JSON绑定到映射表
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    if err := h.TaskSvc.UpdateTask(uint(id), updates); err != nil { // 调用服务层更新任务
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})    // 更新成功
}

// DeleteTask 删除任务（同时删除关联的执行记录、依赖关系、通知配置）
// 路由：DELETE /api/tasks/:id
func (h *TaskHandler) DeleteTask(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 获取要删除的任务ID
    if err := h.TaskSvc.DeleteTask(uint(id)); err != nil {       // 调用服务层删除任务
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})    // 删除成功
}

// RunTask 手动触发一次任务执行（不等待Cron定时）
// 路由：POST /api/tasks/:id/run
func (h *TaskHandler) RunTask(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)           // 获取任务ID
    if h.Executor != nil {                                      // 如果执行器对象存在
        h.Executor.RunTaskNow(uint(id))                         // 立即触发任务（在后台执行）
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "manual trigger queued"}) // 返回"已排队"提示
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
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
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
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
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
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    if err := h.TaskSvc.UpdateTaskDeps(uint(id), req.DepIDs); err != nil { // 更新依赖关系
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})    // 更新成功
}
