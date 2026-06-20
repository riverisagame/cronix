// ============================================================
// internal/interfaces/router/router.go - 路由配置
// 路由 = URL路径和处理器之间的对应关系表
// 比如：当用户访问 /api/tasks 时，调用 ListTasks 函数
// ============================================================
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：Gin 框架的路由查找非常快，底层是怎么做到的？它是一个大 Map（字典）吗？
// 答：
// 不是大 Map。底层用的是一种叫【前缀树（Radix Tree / Trie Tree）】的数据结构。
// 
// 📌 图解 Radix Tree（基数树）：
// 假设我们有三个路由： "/api/tasks", "/api/tags", "/auth/login"
//
//              [ / ]  <-- 根节点
//             /     \
//       [api/t]     [auth/login] ---> 对应 Login 方法
//        /    \
//   [asks]    [ags]
//   对应      对应
// Task方法    Tag方法
// 
// 这种树状结构查找极快，如果有 10000 个路由，顺着树干找，几步就能定位，时间复杂度是 O(K)（K是URL长度），远超正则匹配！
//
// 2. 面试官问：什么是 SPA（单页应用）？为什么你要在 `NoRoute` 里拦截所有找不到的页面，返回 `index.html`？
// 答：
// 【大厂高频前端概念】现在的网页不是以前那种“点一个链接刷新一下整个页面”了。
// 现在的网页，其实只有一页（index.html）。当你点击“用户中心”跳到 `/user` 时，
// 其实是浏览器里的 JavaScript 代码把页面上的内容换成了用户中心，并没有真的去后台请求 `/user` 这个文件。
// 
// 如果用户手欠，直接在浏览器地址栏按回车请求 `https://网站.com/user`，
// 服务器后台根本没有叫 `user` 的文件夹！正常情况会报 404 Not Found。
// 所以后台必须做一个【兜底操作】：凡是找不到的路径，统统塞一张 `index.html` 给它！
// 前端代码加载后，一看“哦，你在访问 /user”，就会自己把页面渲染出来。
// ============================================================
package router

import (
    "embed"       // Go的内嵌文件系统：把前端网页文件打包进程序里
    "io/fs"       // 文件系统接口：用来读取嵌入的前端文件
    "net/http"    // HTTP状态码
    "strings"     // 字符串处理：前缀判断、后缀判断

    "cronix/internal/infrastructure/config"     // 配置模块
    "cronix/internal/interfaces/handler"    // 处理器模块：处理各种API请求
    "cronix/internal/interfaces/middleware"  // 中间件模块：认证检查

    "github.com/gin-gonic/gin"   // Gin框架，号称全网最快，底层用了 Radix Tree
    "github.com/rs/zerolog/log"  // 日志库
)

// SetupRouter 创建并配置所有的URL路由规则
func SetupRouter(
    cfg *config.Config, authH *handler.AuthHandler,
    taskH *handler.TaskHandler, logH *handler.LogHandler,
    groupH *handler.GroupHandler,
    webDist embed.FS,                                          
) *gin.Engine {
    // 设置为发布模式（关闭调试输出，提高性能）
    gin.SetMode(gin.ReleaseMode)

    // 创建一个新的Gin引擎（但不带默认中间件）
    r := gin.New()
    // 【大厂考点】Panic 恢复。如果某个处理函数里写了空指针，会导致整个进程崩溃。
    // Recovery 中间件能把它捕捉住（recover），并且返回 500 错误码，保证别的用户还能继续用。
    r.Use(gin.Recovery())

    // ========== 不需要登录就能访问的路由 ==========
    r.GET("/api/health", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{"status": "healthy", "version": "1.7.0"})
    })
    r.POST("/api/login", authH.Login)

    // ========== 需要登录才能访问的路由（加了认证中间件） ==========
    authMW := middleware.AuthMiddleware()                       
    api := r.Group("/api")                                     // 路由组，非常优雅的归类方式
    api.Use(authMW)                                            // 对这个组里的所有路由上锁（认证）
    {
        // ---- 任务管理 ----
        api.GET("/tasks", taskH.ListTasks)                     
        api.POST("/tasks", taskH.CreateTask)                   
        api.GET("/tasks/:id", taskH.GetTask)                   
        api.PUT("/tasks/:id", taskH.UpdateTask)                
        api.DELETE("/tasks/:id", taskH.DeleteTask)             
        api.POST("/tasks/:id/run", taskH.RunTask)              
        api.POST("/tasks/:id/kill", taskH.KillTask)            
        api.GET("/tasks/:id/stream", taskH.StreamTaskLog)      
        api.GET("/tasks/:id/logs", taskH.GetTaskLogs)          
        api.GET("/tasks/:id/deps", taskH.GetTaskDeps)          
        api.PUT("/tasks/:id/deps", taskH.UpdateTaskDeps)       
        api.GET("/tasks/:id/notify", taskH.GetTaskNotify)      
        api.PUT("/tasks/:id/notify", taskH.UpdateTaskNotify)   

        // ---- 常驻守护进程管理 ----
        api.POST("/tasks/:id/daemon/start", taskH.StartDaemon)    
        api.POST("/tasks/:id/daemon/stop", taskH.StopDaemon)      
        api.GET("/tasks/:id/daemon/status", taskH.GetDaemonStatus) 
        api.GET("/daemon/states", taskH.GetAllDaemonStates)       

        // ---- 日志和仪表盘 ----
        api.GET("/logs", logH.GetAllLogs)                      
        api.DELETE("/logs", logH.ClearAllLogs)                 
        api.DELETE("/logs/:id", logH.DeleteLog)               
        api.GET("/logs/export", logH.ExportLogs)                  
        api.GET("/logs/:id", logH.GetLog)                      
		api.DELETE("/tasks/:id/logs", logH.ClearTaskLogs)     
		api.DELETE("/groups/:id/logs", logH.ClearGroupLogs)  
		api.GET("/dashboard/stats", logH.GetDashboardStats)    
		api.GET("/dashboard/metrics", logH.GetDashboardMetrics) 
		api.GET("/settings", logH.GetSettings)                 
		api.PUT("/settings", logH.UpdateSettings)              

        // ---- 任务组管理 ----
        api.GET("/groups", groupH.ListGroups)                  
        api.POST("/groups", groupH.CreateGroup)                
        api.GET("/groups/:id", groupH.GetGroup)                
        api.PUT("/groups/:id", groupH.UpdateGroup)             
        api.DELETE("/groups/:id", groupH.DeleteGroup)          
        api.PUT("/groups/:id/members", groupH.SetMembers)      
        api.POST("/groups/:id/run", groupH.RunGroup)           
        api.GET("/groups/:id/logs", groupH.GetGroupLogs)       
    }

    // ========== 前端网页托管（SPA 单页应用核心逻辑） ==========
    if cfg.Server.WebUI.Enabled {                               
        distFS, subErr := fs.Sub(webDist, "web/dist")           
        if subErr != nil {                                      
            log.Warn().Err(subErr).Msg("fs.Sub failed")         
            r.GET("/", func(c *gin.Context) {
                c.String(http.StatusOK, "Frontend not built. Run: cd web && npm run build")
            })
        } else {
            // 这是处理静态资源文件（css/js/png）的中间件
            r.Use(func(c *gin.Context) {
                path := c.Request.URL.Path                      
                if strings.HasPrefix(path, "/assets/") {        
                    filePath := strings.TrimPrefix(path, "/")   
                    data, err := fs.ReadFile(distFS, filePath)   
                    if err == nil {                             
                        contentType := "application/octet-stream" 
                        if strings.HasSuffix(path, ".js") {     
                            contentType = "application/javascript"
                        } else if strings.HasSuffix(path, ".css") { 
                            contentType = "text/css"
                        } else if strings.HasSuffix(path, ".html") { 
                            contentType = "text/html; charset=utf-8"
                        } else if strings.HasSuffix(path, ".svg") { 
                            contentType = "image/svg+xml"
                        } else if strings.HasSuffix(path, ".png") { 
                            contentType = "image/png"
                        }
                        c.Data(http.StatusOK, contentType, data) 
                        c.Abort()                                
                        return
                    }
                }
                c.Next()                                         
            })

            indexData, _ := fs.ReadFile(distFS, "index.html")
            
            // SPA 的精髓：找不到后台 API？统统给你塞 index.html，让前端自己路由！
            r.NoRoute(func(c *gin.Context) {
                if strings.HasPrefix(c.Request.URL.Path, "/api/") { 
                    c.JSON(http.StatusNotFound, gin.H{"code": 404})  
                    return
                }
                c.Data(http.StatusOK, "text/html; charset=utf-8", indexData)
            })
        }
    }

    return r                                                     
}
