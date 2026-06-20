// ============================================================
// internal/interfaces/middleware/auth.go - 认证中间件
// 中间件 = 在请求到达最终处理函数之前，先经过的一道"关卡"
// 这里用来检查请求是否携带了合法的登录令牌（JWT）
// ============================================================
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：JWT（JSON Web Token）和传统的 Session 登录有什么区别？
// 答：
// 传统 Session 是【有状态】的：
// 就像你去洗浴中心，前台给你一个号码牌（SessionID），然后前台的小本本（服务器内存）上记着：
// “8号牌 = 张三”。你每次拿牌子去买饮料，前台都要去翻本子查一下。如果客人太多，本子会记不下（内存爆炸）。
//
// JWT 是【无状态】的：
// 就像你去游乐园买的一张“通票手环”。手环上直接印着：“姓名：张三，VIP级别，有效期一天”。
// 最重要的是，游乐园在手环上盖了一个无法伪造的【防伪钢印】（Signature）。
// 你拿着手环去玩项目，工作人员根本不用去查电脑，只要看看钢印是真的，看看日期没过期，直接让你进！
// （服务器不用存任何东西，省了无数内存，这叫水平扩展！）
//
// 2. 面试官问：JWT 具体长啥样？它是加密的吗？
// 答：
// 【大厂高频盲区】JWT 默认是不加密的！只是用 Base64 编码了。
// 
// 📌 图解 JWT 三段式结构（用点号 "." 隔开）：
// [头部 Header] . [载荷 Payload] . [签名 Signature]
//
//  Header  -> {"alg":"HS256"} (告诉服务器我用的是HS256算法盖的钢印)
//  Payload -> {"user":"admin", "exp":17000000} (存放张三的名字和过期时间。⚠️别放密码！它是明文可读的)
//  Signature -> 前两段内容加起来，再配上服务器保密的密码(JWT_SECRET)，经过一把大锁（哈希运算）算出来的钢印。
//
// 如果黑客自己偷偷把 Payload 里的 "user":"admin" 改成 "super_admin"，
// 传给服务器时，服务器用保密密码重新算一次钢印，发现和黑客传上来的钢印对不上！直接拒绝！
// 这就叫：防篡改，但不防偷看。
// ============================================================
package middleware

import (
    "net/http"      // HTTP状态码
    "strings"       // 字符串处理：分割、查找

    "cronix/internal/infrastructure/config" // 配置模块：获取JWT密钥

    "github.com/gin-gonic/gin"         // Gin框架
    "github.com/golang-jwt/jwt/v5"     // JWT库：用于验证令牌的钢印是否合法
)

// AuthMiddleware 返回一个Gin中间件函数
// 中间件的作用：在每个受保护的请求被处理之前，先检查用户是否已登录
// 检查方式是查看请求头里有没有合法的JWT令牌
func AuthMiddleware() gin.HandlerFunc {
    // 返回一个匿名函数，这就是Gin中间件的标准写法
    // c 是上下文对象，包含了请求的所有信息
    return func(c *gin.Context) {
        // 第一步：从请求头中获取 Authorization 字段
        // Authorization 是HTTP协议规定的认证信息存放位置
        authHeader := c.GetHeader("Authorization")               
        if authHeader == "" {                                    // 如果客人连手环都没戴
            c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "missing authorization"}) // 返回401：未授权（滚出去）
            c.Abort()                                            // 中断请求处理链（拦死在安检口，里面的服务员不会看到这个人）
            return
        }

        // 第二步：解析认证信息的格式
        // 标准格式是 "Bearer xxxxxxxx"（Bearer意思是持票人）
        // SplitN 把字符串按空格分割成最多2份
        parts := strings.SplitN(authHeader, " ", 2)              
        if len(parts) != 2 || parts[0] != "Bearer" {            // 格式不对：比如拿了一张假票来
            c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid format"}) 
            c.Abort()
            return
        }

        // 第三步：解析并验证JWT令牌
        // jwt.Parse 这一步非常硬核，它会做两件事：
        // 1. 看看手环上的过期时间（exp）是不是已经过了
        // 2. 拿配置里的 JWT_SECRET（这是服务器的绝密配方），重新算一遍钢印，看和手环上的一不一样。
        token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) { 
            return []byte(config.GetJWTSecret()), nil            
        })

        // 第四步：如果解析失败或令牌无效（比如是黑客伪造的，或者过期了），拒绝请求
        if err != nil || !token.Valid {                          
            c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid token"})
            c.Abort()
            return
        }

        // 第五步：令牌合法，钢印无误。放行！安检通过，让他进去。
        c.Next()
    }
}
