// ============================================================
// internal/interfaces/router/router.go - 路由配置
// 路由 = URL路径和处理器之间的对应关系表
// 比如：当用户访问 /api/tasks 时，调用 ListTasks 函数
// ============================================================
// 📌 【大厂面试·核心考点】
// 面试官问：Gin 框架的路由查找非常快，底层是怎么做到的？它是一个大 Map 吗？
// 答：不是大 Map，底层用的是【基数树（Radix Tree）】。
//
// 🔬 【底层原理·深度剖析】基数树 (Radix Tree) 在路由匹配中的时间复杂度优势
// 传统的路由匹配有两种常见做法：
// 1. 正则匹配（Regex）：按顺序遍历所有正则表达式尝试匹配。如果有 N 个路由，时间复杂度是 O(N)。如果有 1000 个路由，最差要匹配 1000 次，极度耗费 CPU。
// 2. 哈希表（Map）：只能精确匹配固定路径（如 /api/tasks），无法很好地支持参数路径（如 /api/tasks/:id）。
// 
// Radix Tree 是一种压缩前缀树。
// 假设有路由： "/api/tasks", "/api/tags", "/auth/login"
//
//              [ / ]  <-- 根节点
//             /     \
//       [api/t]     [auth/login] ---> 对应 Login 方法
//        /    \
//   [asks]    [ags]
//   对应      对应
// Task方法    Tag方法
// 
// 匹配时，只需要顺着字符向下比较。匹配的时间只和 URL 的长度 K 相关，与路由总数 N 无关！
// 时间复杂度：O(K)。就算你有 10万 个路由，查找 "/api/tasks" 也只需要比对寥寥几次字符。
// 
// ⚡ 【性能实战·生产调优】
// 在高并发场景下，Gin 的 Radix Tree 路由分配耗时仅需几个纳秒级别，而正则路由框架（如早期的 Django 或 Flask）可能需要微秒甚至毫秒级别。在 10W QPS 下，这种差异就是 CPU 占用率 10% 和 90% 的天壤之别！
// 
// 🛡️ 【安全攻防·漏洞防线】RESTful API 跨域问题 (CORS) 中的 Preflight Request (OPTIONS) 底层机制
// 当我们的前端应用和后端 API 不在同一个域名下（例如前端在 localhost:3000，后端在 localhost:8080），浏览器会触发“同源策略”限制。
// 对于复杂的跨域请求（如带有自定义 Header、或 PUT/DELETE 请求），浏览器会在真正发送请求前，偷偷发一个 OPTIONS 请求，这就是“预检请求（Preflight Request）”。
// - 目的：问问服务器，“哥，我要发跨域请求了，你允许吗？允许哪些方法和头信息？”
// - 防御：如果你不配置 CORS 中间件，或者不处理 OPTIONS 请求，预检请求会收到 404 或 403，导致真实请求被浏览器拦截（CORS Error）。
// *注：当前文件未显式配置 CORS 中间件，默认不支持跨站 API 调用。如果前端分离部署，必须增加 cors 中间件拦截 OPTIONS 请求。*
//
// 📌 【大厂面试·核心考点】
// 面试官问：什么是 SPA（单页应用）？为什么要在 `NoRoute` 里拦截所有找不到的页面，返回 `index.html`？
// 答：
// 【大厂高频前端概念】现在的网页不是以前那种“点一个链接刷新一下整个页面”了。现在的网页其实只有一页（index.html）。
// 当你点击“用户中心”跳到 `/user` 时，其实是浏览器里的 JavaScript 把它替换了，并没有真的去请求 `/user`。
// 如果直接在浏览器地址栏按回车请求 `https://xxx/user`，服务器找不到就会报 404。
// 所以后台必须做【兜底操作】：凡是找不到的非 API 路径，统统返回 `index.html`！
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
    // 🏗️ 【架构设计·模式对比】API 版本控制 (API Versioning) 的设计权衡
    // 为什么我们要用 r.Group("/api") 甚至未来演变成 r.Group("/api/v1")？
    // 1. URL Path 方案（例如 /api/v1/tasks）：
    //    - 优点：最直观，客户端调用极度简单，浏览器直接可见，不同版本彻底物理隔离。
    //    - 缺点：违反纯粹的 RESTful 理念（URL 应该只代表资源名词本身，不该混入版本控制）。
    // 2. HTTP Header 方案（例如 Accept: application/vnd.myapi.v1+json）：
    //    - 优点：完美符合 RESTful 语义，URL 永远干净纯粹。
    //    - 缺点：客户端测试调试困难（不能直接在浏览器按回车抓包），CDN 缓存易穿透（需要配 Vary: Accept）。
    // 3. Query Parameter 方案（例如 /api/tasks?version=1）：
    //    - 优点：简单粗暴。
    //    - 缺点：容易和业务查询参数混淆，显得极不专业。
    // 【大厂主流选择】：绝大多数互联网公司（如 GitHub, Stripe, 阿里云）都会务实地选择 URL Path 或 Header。
    // 💀 【踩坑血泪·反面教材】
    // 曾经有团队不做版本控制，直接在原有 API 上修改返回值字段类型（如把 string 变成 int），导致老版本的 App 客户端一运行到解析逻辑就闪退，最终引发 P0 级生产事故！API 一旦发布就是契约，破坏性修改必须升级版本号！
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
            
            // 🧪 【测试工程·质量保障】
            // 在测试此类 SPA 后台服务时，必须要有自动化 API 冒烟测试：
            // 验证请求 `/随便瞎写` 能够正常吐出 `index.html` 内容且状态码为 200；
            // 验证请求 `/api/随便瞎写` 能够吐出 404 JSON 数据，而不是网页。
            // 如果 API 错误吐出了 HTML，会导致前端 axios 或 fetch 执行 JSON.parse() 时抛出 SyntaxError，进而导致应用白屏！
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
