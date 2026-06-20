// ============================================================
// internal/handler/auth.go - 用户登录认证处理器
// 负责验证用户名密码、生成登录令牌（JWT）、以及登录频率限制
// ============================================================
package handler

import (
    "net/http" // 网络请求相关：HTTP状态码、请求响应处理
    "sync"     // 并发安全：互斥锁，防止多个请求同时修改数据
    "time"     // 时间处理：记录登录时间窗口、令牌过期时间

    "cronix/internal/infrastructure/config" // 本项目的配置模块：读取用户名、密码、JWT密钥等配置

    "github.com/gin-gonic/gin"           // Gin框架：用Go语言写的Web服务框架，处理HTTP请求
    "github.com/golang-jwt/jwt/v5"       // JWT库：用来生成和解析登录令牌（JSON Web Token）
    "golang.org/x/crypto/bcrypt"         // bcrypt加密库：对密码进行安全加密和比对
)

// AuthHandler 是登录认证的处理器结构体
// 它本身不存储任何数据，所有方法都挂在这个结构体上
type AuthHandler struct{}

// LoginRequest 是用户登录时发送的请求数据结构
// 前端（网页）会把用户名和密码放在JSON里发给后端
type LoginRequest struct {
    Username string `json:"username" binding:"required"` // 用户名，必须填写
    Password string `json:"password" binding:"required"` // 密码，必须填写
}

// 以下两个变量是全局变量，用来控制登录频率（防止暴力破解）
var (
    // loginAttempts 记录每个IP地址的登录尝试次数
    // key是IP地址，value是该IP的登录记录窗口
    loginAttempts = make(map[string]*loginWindow) // 创建一个空的映射表

    // loginMu 是一个互斥锁，保证多个请求同时访问loginAttempts时不会出错
    loginMu       sync.Mutex // Mutex = 互斥锁，同一时刻只允许一个操作修改数据
)

// loginWindow 记录某个IP在某个时间段内的登录尝试情况
type loginWindow struct {
    count     int       // 已经尝试登录了几次
    firstSeen time.Time // 第一次尝试登录的时间（用来判断时间窗口是否过期）
}

// 登录频率限制的常量（固定值，程序运行时不改变）
const maxLoginAttempts = 5                  // 每个IP每分钟最多允许尝试登录5次
const loginWindowDuration = time.Minute     // 时间窗口长度：1分钟（超过1分钟后重新计数）

// Login 处理用户登录请求
// 参数 c 是Gin框架的上下文对象，包含了请求和响应所需的所有信息
// 路由：POST /api/login
func (h *AuthHandler) Login(c *gin.Context) {
    // 第一步：解析前端发来的JSON数据，放入LoginRequest结构体
    var req LoginRequest                                    // 声明一个变量，用来存放解析后的请求数据
    if err := c.ShouldBindJSON(&req); err != nil {          // 尝试把请求中的JSON数据绑定到req变量
        respondError(c, http.StatusBadRequest, "invalid request") // 如果格式不对，返回400错误
        return                                              // 停止处理
    }

    // 第二步：获取发送请求的客户端IP地址
    clientIP := c.ClientIP() // 这个IP用来做频率限制的标识

    // 第三步：登录频率限制检查（防止有人用程序暴力猜密码）
    loginMu.Lock()                          // 加锁，确保同时只有一个请求能修改loginAttempts
    // 顺带清理超过 2 个窗口期的过期条目，防止 map 无限增长
    // @Ref: architect_review_20260609.md P0-2 | @Date: 2026-06-09
    now := time.Now()
    for ip, w := range loginAttempts {
        if now.Sub(w.firstSeen) > loginWindowDuration*2 {
            delete(loginAttempts, ip)
        }
    }
    w, exists := loginAttempts[clientIP]    // 查一下这个IP之前有没有登录过
    if !exists || time.Since(w.firstSeen) > loginWindowDuration { // 两种情况：第一次登录，或者距离首次登录已经超过1分钟
        loginAttempts[clientIP] = &loginWindow{count: 1, firstSeen: time.Now()} // 重置记录：次数为1，首次时间为现在
    } else {
        w.count++                          // 如果不是第一次且在时间窗口内，把尝试次数加1
        if w.count > maxLoginAttempts {    // 如果尝试次数超过了最大允许值（5次）
            loginMu.Unlock()               // 先解锁，再返回错误
            respondError(c, http.StatusTooManyRequests, "too many login attempts, try again later") // 返回429：请求过多
            return                         // 停止处理
        }
    }
    loginMu.Unlock()                       // 解锁，允许其他请求访问loginAttempts

    // 第四步：验证用户名是否正确
    // config.AppConfig.Auth.Username 是从配置文件里读取的管理员用户名
    if req.Username != config.AppConfig.Auth.Username {    // 比较用户输入和配置中的用户名
        respondError(c, http.StatusUnauthorized, "invalid username or password") // 用户名不对，返回401
        return
    }

    // 第五步：验证密码是否正确（使用bcrypt加密算法比较）
    // config.AppConfig.Auth.Password 是配置文件里存储的加密密码（不是明文！）
    // bcrypt.CompareHashAndPassword 会用加密算法比对用户输入的明文密码和存储的密文
    if err := bcrypt.CompareHashAndPassword([]byte(config.AppConfig.Auth.Password), []byte(req.Password)); err != nil {
        respondError(c, http.StatusUnauthorized, "invalid username or password") // 密码不对，返回401
        return
    }

    // 第六步：用户名和密码都正确！现在生成一个登录令牌（JWT）
    // 令牌里面包含了用户信息和过期时间
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{ // 创建一个新令牌，使用HS256签名算法
        "sub": req.Username,                       // "sub"=主体，这里存用户名
        "iat": time.Now().Unix(),                  // "iat"=签发时间，记录令牌是什么时候生成的
        "exp": time.Now().Add(24 * time.Hour).Unix(), // "exp"=过期时间，24小时后令牌自动失效
    })

    // 第七步：用密钥对令牌进行签名，生成最终的令牌字符串
    // SignedString 把令牌内容用密钥加密，生成一个字符串
    tokenString, err := token.SignedString([]byte(config.GetJWTSecret())) // GetJWTSecret()返回配置文件中的加密密钥
    if err != nil {
        respondError(c, http.StatusInternalServerError, "failed to generate token") // 签名失败，服务器内部错误
        return
    }

    // 第八步：登录成功！把令牌和过期时间返回给前端
    c.JSON(http.StatusOK, gin.H{                          // 返回200 OK
        "code":    0,                                     // code=0 表示成功
        "message": "ok",                                  // 提示信息
        "data": gin.H{                                    // data里放实际数据
            "token":      tokenString,                    // 登录令牌，前端拿到后要保存起来
            "expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339), // 过期时间，格式化为人可读的字符串
        },
    })
}
