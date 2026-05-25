// ============================================================
// internal/router/router.go - 路由配置
// 路由 = URL路径和处理器之间的对应关系表
// 比如：当用户访问 /api/tasks 时，调用 ListTasks 函数
// ============================================================
package router

import (
    "embed"       // Go的内嵌文件系统：把前端网页文件打包进程序里
    "io/fs"       // 文件系统接口：用来读取嵌入的前端文件
    "net/http"    // HTTP状态码
    "strings"     // 字符串处理：前缀判断、后缀判断

    "cronix/internal/config"     // 配置模块
    "cronix/internal/handler"    // 处理器模块：处理各种API请求
    "cronix/internal/middleware"  // 中间件模块：认证检查

    "github.com/gin-gonic/gin"   // Gin框架
    "github.com/rs/zerolog/log"  // 日志库：记录警告信息
)

// SetupRouter 创建并配置所有的URL路由规则
// 参数说明：
//   cfg: 系统配置（WebUI开关等）
//   authH: 登录认证处理器
//   taskH: 任务管理处理器
//   logH: 日志和仪表盘处理器
//   webDist: 前端网页的打包文件（编译时嵌入的）
// 返回值：配置好的Gin引擎（本质是HTTP请求的入口）
func SetupRouter(
    cfg *config.Config, authH *handler.AuthHandler,
    taskH *handler.TaskHandler, logH *handler.LogHandler,
    groupH *handler.GroupHandler,
    webDist embed.FS,                                          // embed.FS 是Go的嵌入文件系统类型
) *gin.Engine {
    // 设置为发布模式（关闭调试输出，提高性能）
    gin.SetMode(gin.ReleaseMode)

    // 创建一个新的Gin引擎（但不带默认中间件）
    r := gin.New()
    // 添加恢复中间件：如果程序崩溃了，自动恢复并返回500错误，而不是整个程序挂掉
    r.Use(gin.Recovery())

    // ========== 不需要登录就能访问的路由 ==========

    // 健康检查接口：用于确认服务是否正常运行
    r.GET("/api/health", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{"status": "healthy", "version": "1.1.0"})
    })

    // 登录接口：用户输入用户名密码获取令牌
    r.POST("/api/login", authH.Login)

    // ========== 需要登录才能访问的路由（加了认证中间件） ==========

    authMW := middleware.AuthMiddleware()                       // 创建认证中间件
    api := r.Group("/api")                                     // 创建一个路由组，所有URL都以 /api 开头
    api.Use(authMW)                                            // 对这个组里的所有路由都启用认证检查
    {
        // ---- 任务管理 ----
        api.GET("/tasks", taskH.ListTasks)                     // 获取任务列表
        api.POST("/tasks", taskH.CreateTask)                   // 创建新任务
        api.GET("/tasks/:id", taskH.GetTask)                   // 获取单个任务（:id是URL参数）
        api.PUT("/tasks/:id", taskH.UpdateTask)                // 更新任务
        api.DELETE("/tasks/:id", taskH.DeleteTask)             // 删除任务
        api.POST("/tasks/:id/run", taskH.RunTask)              // 手动触发任务执行
        api.GET("/tasks/:id/logs", taskH.GetTaskLogs)          // 查询任务执行日志
        api.GET("/tasks/:id/deps", taskH.GetTaskDeps)          // 查询任务依赖关系
        api.PUT("/tasks/:id/deps", taskH.UpdateTaskDeps)       // 更新任务依赖关系

        // ---- 日志和仪表盘 ----
        api.GET("/logs", logH.GetAllLogs)                      // 获取所有日志
        api.DELETE("/logs", logH.ClearAllLogs)                 // 清空所有日志
        api.DELETE("/logs/:id", logH.DeleteLog)               // 删除单条日志
        api.DELETE("/tasks/:id/logs", logH.ClearTaskLogs)     // 清空指定任务日志
        api.DELETE("/groups/:id/logs", logH.ClearGroupLogs)  // 清空组日志
        api.GET("/dashboard/stats", logH.GetDashboardStats)    // 仪表盘统计数据
        api.GET("/settings", logH.GetSettings)                 // 读取系统设置
        api.PUT("/settings", logH.UpdateSettings)              // 修改系统设置

        // ---- 任务组管理 ----
        api.GET("/groups", groupH.ListGroups)                  // 获取组列表
        api.POST("/groups", groupH.CreateGroup)                // 创建组
        api.GET("/groups/:id", groupH.GetGroup)                // 获取组详情（含成员）
        api.PUT("/groups/:id", groupH.UpdateGroup)             // 更新组
        api.DELETE("/groups/:id", groupH.DeleteGroup)          // 删除组
        api.PUT("/groups/:id/members", groupH.SetMembers)      // 设置组成员
        api.POST("/groups/:id/run", groupH.RunGroup)           // 手动触发整组
        api.GET("/groups/:id/logs", groupH.GetGroupLogs)       // 查询组执行日志
    }

    // ========== 前端网页托管（如果开启了WebUI功能） ==========
    if cfg.Server.WebUI.Enabled {                               // 检查配置中是否开启了网页界面
        // 尝试从嵌入的文件系统中提取前端文件目录 "web/dist"
        distFS, subErr := fs.Sub(webDist, "web/dist")           // Sub 创建一个子文件系统，指向 web/dist 目录
        if subErr != nil {                                      // 如果找不到前端文件（可能还没编译）
            log.Warn().Err(subErr).Msg("fs.Sub failed")         // 记录一条警告日志
            // 显示提示信息给访问者
            r.GET("/", func(c *gin.Context) {
                c.String(http.StatusOK, "Frontend not built. Run: cd web && npm run build")
            })
        } else {
            // 前端文件存在，配置静态文件服务
            // 这是一个中间件，专门处理 /assets/ 路径下的静态资源（JS、CSS、图片等）
            r.Use(func(c *gin.Context) {
                path := c.Request.URL.Path                      // 获取请求的URL路径
                if strings.HasPrefix(path, "/assets/") {        // 如果路径以 /assets/ 开头（前端打包后的资源目录）
                    filePath := strings.TrimPrefix(path, "/")   // 去掉开头的 /，得到文件系统中的相对路径
                    data, err := fs.ReadFile(distFS, filePath)   // 从嵌入的文件系统中读取文件内容
                    if err == nil {                             // 文件存在
                        contentType := "application/octet-stream" // 默认文件类型：二进制流
                        // 根据文件扩展名设置正确的文件类型（让浏览器能正确显示）
                        if strings.HasSuffix(path, ".js") {     // JavaScript脚本文件
                            contentType = "application/javascript"
                        } else if strings.HasSuffix(path, ".css") { // CSS样式文件
                            contentType = "text/css"
                        } else if strings.HasSuffix(path, ".html") { // HTML网页文件
                            contentType = "text/html; charset=utf-8"
                        } else if strings.HasSuffix(path, ".svg") { // SVG矢量图
                            contentType = "image/svg+xml"
                        } else if strings.HasSuffix(path, ".png") { // PNG图片
                            contentType = "image/png"
                        }
                        c.Data(http.StatusOK, contentType, data) // 把文件内容和类型返回给浏览器
                        c.Abort()                                // 中断后续处理（已经找到文件了）
                        return
                    }
                }
                c.Next()                                         // 不是静态资源，继续处理下一个处理器
            })

            // 读取前端的主页面文件 index.html
            indexData, _ := fs.ReadFile(distFS, "index.html")
            // NoRoute 处理所有没有匹配到的URL路径（也就是前端路由）
            // 在单页应用（SPA）中，所有非API路径都要返回 index.html
            r.NoRoute(func(c *gin.Context) {
                if strings.HasPrefix(c.Request.URL.Path, "/api/") { // 如果是/api/开头的路径且没有匹配到
                    c.JSON(http.StatusNotFound, gin.H{"code": 404})  // 返回404
                    return
                }
                // 否则返回 index.html，由前端JavaScript处理路由
                c.Data(http.StatusOK, "text/html; charset=utf-8", indexData)
            })
        }
    }

    return r                                                     // 返回配置好的路由引擎
}
