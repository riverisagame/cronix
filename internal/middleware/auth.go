// ============================================================
// internal/middleware/auth.go - 认证中间件
// 中间件 = 在请求到达最终处理函数之前，先经过的一道"关卡"
// 这里用来检查请求是否携带了合法的登录令牌（JWT）
// ============================================================
package middleware

import (
    "net/http"      // HTTP状态码
    "strings"       // 字符串处理：分割、查找

    "cronix/internal/config" // 配置模块：获取JWT密钥

    "github.com/gin-gonic/gin"         // Gin框架
    "github.com/golang-jwt/jwt/v5"     // JWT库：用于验证令牌是否合法
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
        authHeader := c.GetHeader("Authorization")               // 读取请求头中的认证信息
        if authHeader == "" {                                    // 如果没有认证信息
            c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "missing authorization"}) // 返回401：未授权
            c.Abort()                                            // 中断请求处理链，不再执行后续的处理器
            return
        }

        // 第二步：解析认证信息的格式
        // 标准格式是 "Bearer xxxxxxxx"（Bearer后面跟一个空格，然后跟令牌）
        // SplitN 把字符串按空格分割成最多2份
        parts := strings.SplitN(authHeader, " ", 2)              // 按第一个空格分割
        if len(parts) != 2 || parts[0] != "Bearer" {            // 格式不对：不是两份，或者第一份不是"Bearer"
            c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid format"}) // 格式不对
            c.Abort()
            return
        }

        // 第三步：解析并验证JWT令牌
        // jwt.Parse 会检查令牌的签名是否合法、是否过期
        token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) { // 匿名函数提供验证密钥
            return []byte(config.GetJWTSecret()), nil            // 返回配置中的JWT密钥，用来检查签名
        })

        // 第四步：如果解析失败或令牌无效，拒绝请求
        if err != nil || !token.Valid {                          // err!=nil表示解析出错，!token.Valid表示令牌无效
            c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid token"})
            c.Abort()
            return
        }

        // 第五步：令牌合法，放行！继续执行后续的处理器
        c.Next()
    }
}
