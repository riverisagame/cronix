// ============================================================
// internal/handler/auth.go - 用户登录认证处理器
// 负责验证用户名密码、生成登录令牌（JWT）、以及登录频率限制
//
// 🏗️ 【架构设计·模式对比】 认证架构演进
// 初二小白比喻：以前进小区凭门禁卡（Session在物业电脑里有记录），现在进小区凭身份证（JWT自带信息，物业不存状态）。
// 传统 Session 架构：服务端存储状态，扩展性差，但在需要强制踢人下线时极其方便。
// 无状态 JWT 架构：服务端不存状态，天然支持分布式和微服务，但缺点是无法主动让单个 Token 失效（除非引入黑名单机制，这就又退化成了有状态）。
//
// 📌 【大厂面试·核心考点】 面试官会怎么问？
// 1. Q: JWT Token 泄露了怎么办？
//    A: 标准答案包含三板斧：1) 缩短 Token 有效期（如15分钟），配合 Refresh Token 机制；2) 关键操作验证二次凭证；3) 引入 Redis 黑名单主动封禁。
// 2. Q: 密码在数据库中应该怎么存？
//    A: 绝对不能存明文，也不能单纯用 MD5/SHA256。必须使用 bcrypt/argon2 这类带随机盐和慢速哈希算法，对抗彩虹表和算力破解。
// ============================================================
package handler

import (
    "net/http" // 网络请求相关：HTTP状态码、请求响应处理
    "sync"     // 并发安全：互斥锁，防止多个请求同时修改数据
               // 🔬 【底层原理·深度剖析】 Go sync.Mutex 底层机制
               // 初二小白比喻：就像公厕的门锁，一个人进去了别人就得在外面排队。
               // 底层分为两种模式：正常模式和饥饿模式。
               // 正常模式下，goroutine 发现锁被占用，会先自旋（Spin）几次，消耗 CPU 空转等锁（假设上个持有者马上就释放）。
               // 如果自旋失败，才会把 goroutine 挂起，放入等待队列，交由操作系统底层信号量（Semaphore）机制来唤醒。这种自旋+信号量的设计极大减少了上下文切换的开销。
    "time"     // 时间处理：记录登录时间窗口、令牌过期时间

    "cronix/internal/infrastructure/config" // 本项目的配置模块：读取用户名、密码、JWT密钥等配置

    "github.com/gin-gonic/gin"           // Gin框架：用Go语言写的Web服务框架，处理HTTP请求
    "github.com/golang-jwt/jwt/v5"       // JWT库：用来生成和解析登录令牌（JSON Web Token）
                                         // 🔬 【底层原理·深度剖析】 JWT 完整生命周期
                                         // JWT 由三部分组成：Header（头部：声明算法如 HS256）、Payload（载荷：用户信息如 username、过期时间）、Signature（签名：防止篡改）。
                                         // 三个部分都经过 Base64url 编码（将普通 Base64 中的 '+' 替换为 '-'，'/' 替换为 '_'，去掉末尾 '='，以适应 HTTP 传输参数要求）。
                                         // 最终拼接成：Header.Payload.Signature 的格式返回给客户端。每次请求携带在 Authorization 头部。
    "golang.org/x/crypto/bcrypt"         // bcrypt加密库：对密码进行安全加密和比对
                                         // 🔬 【底层原理·深度剖析】 bcrypt 哈希算法底层原理
                                         // 初二小白比喻：普通的MD5像秒级把大米磨成粉，而bcrypt像要求用小石碾把大米慢慢磨一天，黑客想大批量试密码就累死了。
                                         // bcrypt 基于 Blowfish 分组密码算法演变而来的 Eksblowfish（Expensive Key Schedule Blowfish）。
                                         // 它强制包含两个关键特性：1) Cost Factor（工作因子）：通过指数级增加迭代次数（如 cost=10 意味着 2^10 次运算），故意拖慢单次计算速度；2) 内置 Salt（盐）：每次加密自动生成128位随机盐，并直接拼装在最终的密文字符串中，彻底阻断彩虹表攻击。
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
//
// 🏗️ 【架构设计·模式对比】 Rate Limiting 四种实现对比
// 1. 固定窗口（Fixed Window）：本文使用的方案。实现简单，但有临界点突刺问题（如59秒请求5次，01秒请求5次，导致跨窗口两秒内实际放行10次，超频）。
// 2. 滑动窗口（Sliding Window Log）：记录每次请求的精确时间戳。极度精确，没有突刺问题，但内存消耗巨大，不适合高并发。
// 3. 漏桶（Leaky Bucket）：请求像水滴入桶，以固定速率流出。能绝对平滑突发流量，防止后端被打挂，但不允许突发处理。
// 4. 令牌桶（Token Bucket）：按固定速率向桶里放令牌，请求消耗令牌。允许一定程度的突发流量（桶里有余量即可瞬间消耗），是生产环境 API 网关最常用的方案。
//
// 🛡️ 【安全攻防·漏洞防线】 暴力破解防御策略
// 真正的生产环境防爆破，单靠限制单一 IP 的固定窗口是远远不够的（黑客有海量秒拨代理IP池）。完整纵深防线需要：
// 1. IP封禁：发现单IP高频错误则通过 WAF/防火墙封禁（风险：易误杀NAT出口下的整个公司网或同一个小区的用户）。
// 2. 账户锁定：连续输错N次密码锁定该账号特定时间（防止黑客换着海量IP爆破同一个高价值账号）。
// 3. 行为验证码：触发风控阈值时，自动升级拦截手段，弹出滑块/无感验证码，拦截机器自动化脚本。
// 4. 指数退避算法：每次登录失败后，强制该账号下次登录的等待时间呈指数增长（1秒、2秒、4秒、8秒），极大拉长破解时间成本。
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
    // ⚡ 【性能实战·生产调优】 并发与锁的争用
    // 这里的 loginMu.Lock() 在高并发场景下会成为全局单点性能瓶颈。
    // 每次用户请求都会锁住整个共享 map，导致所有登录请求的并发被强制串行化。
    // 生产环境优化手段：单机版可以使用分段锁（类似 Java ConcurrentHashMap思想，降低锁粒度），分布式版本则直接把限流下沉到 Redis 集群利用 INCR + EXPIRE 解决本地内存限流的单机瓶颈。
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
    // 
    // 💀 【踩坑血泪·反面教材】 这里的直接 return 会在时序上暴露用户名是否存在！黑客可以据此进行“用户名枚举攻击”。
    // 🛡️ 【安全攻防·漏洞防线】 Timing Attack 时序攻击与 constant-time comparison
    // 原理：如果系统先查用户名，不存在直接 return（耗时极短）；存在则往下走去计算 bcrypt（耗时极长）。
    // 黑客通过精准测量服务器响应时间：返回极快说明用户名不存在，返回极其慢说明用户名必定存在！
    // 正确防御措施：无论用户名是否存在，都要走一套伪造的、或者恒定时间（constant-time）的 bcrypt 计算消耗CPU，使得“用户名错误”和“密码错误”的接口整体返回耗时完全一致。本代码在错误提示上做到了模糊化（返回 invalid username or password），但在底层响应耗时上仍留有微小的破绽。
    if req.Username != config.AppConfig.Auth.Username {    // 比较用户输入和配置中的用户名
        respondError(c, http.StatusUnauthorized, "invalid username or password") // 用户名不对，返回401
        return
    }

    // 第五步：验证密码是否正确（使用bcrypt加密算法比较）
    // config.AppConfig.Auth.Password 是配置文件里存储的加密密码（不是明文！）
    // bcrypt.CompareHashAndPassword 会用加密算法比对用户输入的明文密码和存储的密文
    //
    // ⚡ 【性能实战·生产调优】 慢哈希的性能代价与系统雪崩
    // bcrypt 的 CompareHashAndPassword 极为耗 CPU（默认 cost factor 下通常耗时几十到上百毫秒量级）。
    // 在高并发登录峰值（如促销秒杀开始时，大量用户被踢出要求重新登录），海量的并发 bcrypt 计算会导致服务器 CPU 瞬间打满直至拒绝服务。
    // 生产调优：必须根据服务器 CPU 核数，结合信号量机制（如 make(chan struct{}, maxWorkers)）来限制同时进行 bcrypt 运算的最大 goroutine 数量，执行主动限流降级，防止系统雪崩宕机。
    if err := bcrypt.CompareHashAndPassword([]byte(config.AppConfig.Auth.Password), []byte(req.Password)); err != nil {
        respondError(c, http.StatusUnauthorized, "invalid username or password") // 密码不对，返回401
        return
    }

    // 第六步：用户名和密码都正确！现在生成一个登录令牌（JWT）
    // 令牌里面包含了用户信息和过期时间
    //
    // 📌 【大厂面试·核心考点】 JWT 签名算法选型：HS256 vs RS256
    // 面试官问：你这里用的是 SigningMethodHS256，它和 RS256 有什么本质区别？生产环境该怎么选？
    // 满分回答：HS256 属于对称加密方案，签发（生成Token）和验证（解析Token）都必须使用同一个密钥（Secret）。如果在微服务架构中，其他独立的子系统也想验证 Token，就必须把同一个密钥分发给所有子系统，极易导致密钥大范围泄露，安全性堪忧。
    // 生产标准：在真正的分布式或微服务架构中，应该采用 RS256 非对称加密。统一认证中心（SSO）妥善保管私钥用来进行数字签名生成 Token，其他各个微服务只需要拿着公开的公钥去验证这个签名即可。就算某个微服务被攻破泄露了公钥，黑客也绝不可能用公钥来伪造合法的登录 Token。
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
