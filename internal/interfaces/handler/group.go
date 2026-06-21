// ============================================================
// internal/interfaces/handler/group.go — 任务组 HTTP 处理器（Handler 层）
//
// ============================================================
//
// 🏗️ 【架构设计·整体定位】
//
// 想象一个大酒店：前台接待员 = Handler，经理 = Service，仓库管理员 = Repository。
// 本文件就是"前台接待员"。客人（HTTP 请求）进门，她的职责**只有三件事**：
//   1. 听清客人要什么（解析请求参数 / JSON Body）
//   2. 转告给经理去办（调用 Service 层方法）
//   3. 把结果告诉客人（返回 JSON 响应）
//
// 📌 【大厂面试·核心考点总览】
//
// Q1: Handler 层为什么不能写业务逻辑？
// A:  这是经典的 **单一职责原则（SRP）** 的体现。如果前台接待员又收银又炒菜又送餐，
//     那换个前台整个酒店就瘫了。Handler 只做 IO 转换，业务逻辑全沉在 Service 层：
//     - 方便单元测试（Mock 掉 Service 就行）
//     - 方便换框架（从 gin 换成 echo/fiber，只改 Handler，Service 纹丝不动）
//     - 方便并行开发（前端工程师看 Handler 签名写接口，后端独立改 Service 逻辑）
//
// Q2: RESTful 资源嵌套（/groups/:id/members）的设计原则是什么？
// A:  遵循 Richardson Maturity Model Level 2——用 URI 路径表达资源的层级归属关系：
//     - GET    /groups              → ListGroups（集合）
//     - POST   /groups              → CreateGroup（创建）
//     - GET    /groups/:id          → GetGroup（单体 + 子资源聚合）
//     - PUT    /groups/:id          → UpdateGroup（全量 / 部分更新）
//     - DELETE /groups/:id          → DeleteGroup（删除 + 级联清理）
//     - PUT    /groups/:id/members  → SetMembers（子资源批量设置）
//     - POST   /groups/:id/run      → RunGroup（动作型端点，RPC 风格的例外）
//     - GET    /groups/:id/logs     → GetGroupLogs（子资源分页查询）
//     嵌套最多不要超过 2 层（/groups/:id/members），否则 URL 又臭又长，是反模式。
//
// Q3: 组操作为什么需要原子性？级联删除策略怎么选？
// A:  DeleteGroup 涉及 3 张表的联动变更（Task 解绑 + GroupLog 删除 + Group 删除）。
//     如果中间崩溃，就会出现"孤儿日志"或"幽灵组员"。所以 Service 层用了数据库事务包裹。
//     本项目采用的级联策略是 **混合策略**：
//     - Task → Group 关系：SET NULL（解绑但不删任务，任务是核心资产，不能随组陪葬）
//     - GroupLog → Group：CASCADE（日志跟着组一起删，没有组的日志毫无意义）
//     - 对比 RESTRICT：如果用 RESTRICT，删组前必须手工清空所有关联数据，用户体验极差。
//
// 🔬 【底层原理·Handler 生命周期】
//
// 每个 HTTP 请求到达 Gin 框架后，会经历：
//   1. 路由匹配 → Gin 的 radix tree（基数树）在 O(k) 时间内找到对应 Handler（k=URL 段数）
//   2. 中间件链执行 → 日志、鉴权、CORS 等，像洋葱一样层层包裹
//   3. Handler 执行 → 本文件的函数被调用
//   4. 响应序列化 → gin.H（本质是 map[string]any）通过 encoding/json 序列化为 JSON
//   5. 连接回收 → *gin.Context 放回 sync.Pool（对象池），减少 GC 压力
//
// ⚡ 【性能实战·关键数据】
//
// - Gin 路由匹配：~300ns/op（基数树），vs net/http 默认的线性扫描 ~1μs/op
// - JSON 序列化 gin.H（5 个字段）：~800ns/op，若字段数超过 20 建议用 struct 代替 map
// - sync.Pool 复用 Context：每请求节省 ~256B 堆分配，高并发下 GC 停顿可降低 40%
//
// 🧪 【测试工程·质量保障】
//
// 📌 【大厂面试·核心考点】
// Q: 如何对包含依赖的 Handler 层进行单元测试？
// A: 必须使用 Mocking 技术。因为 Handler 依赖 Service 层，而 Service 层依赖数据库，
//    如果直接测试 Handler 会变成“集成测试”，不仅慢还容易因脏数据失败。
//    - 正确做法：使用 gomock 或手动实现 interface，模拟 Service 层的行为和返回值。
//    - 测试重心：不要在 Handler 层测试业务逻辑分支。Handler 测试重点在于：
//      1. 参数绑定：无效 JSON、超长字符串、类型错误是否正确返回 400
//      2. 状态映射：Service 返回错误时，是否正确转换为 500 或 404
//      3. 响应结构：JSON 返回值中字段格式是否正确（例如 nil 变为 [] 的断言）
//    - 实践手段：使用 net/http/httptest 包构造 httptest.NewRecorder() 捕获响应。
//
// ============================================================
package handler

// ============================================================
// 📦 【import 区域·底层作用详解】
// ============================================================
import (
    // net/http：Go 标准库的 HTTP 基础包。
    // 🔬 底层原理：这个包定义了 HTTP 状态码常量（如 StatusOK=200、StatusNotFound=404）。
    //    这些常量本质是 int 类型的枚举，编译期就确定了值，零运行时开销。
    //    用常量而不是裸写数字 200/404，是因为：
    //    - 防手滑（你写个 401 当 404 用，编译器不会帮你查）
    //    - 提高可读性（StatusNotFound 比 404 一目了然）
    "net/http"

    // strconv：Go 标准库的字符串转换包。
    // 🔬 底层原理：ParseUint/Atoi 内部实现是**手写的逐字符累加**，没有用 fmt.Sscanf。
    //    性能对比：strconv.Atoi 约 15ns/op，fmt.Sscanf 约 800ns/op，差 50 倍！
    //    为什么这么快？因为它直接操作 byte slice，没有反射、没有格式字符串解析。
    //    💀 踩坑提醒：ParseUint 的第二个参数是进制（10=十进制），第三个是位宽（64=uint64）。
    //    如果传错进制（比如传了 16），"123" 会被解析成十六进制的 0x123 = 291，结果完全不对！
    "strconv"

    // cronix/internal/domain/model：领域模型包，定义了 TaskGroup、Task 等核心实体。
    // 🏗️ 架构意义：Handler 直接引用 domain model 是 DDD（领域驱动设计）简化版的做法。
    //    严格的 DDD 会要求 Handler 只用 DTO（Data Transfer Object），不暴露领域模型。
    //    但对于中小项目，直接用 model 可以减少 50% 的样板代码，是务实的权衡。
    "cronix/internal/domain/model"

    // cronix/internal/application/scheduler：调度引擎包，负责任务的定时触发和执行。
    // 🏗️ 架构意义：Handler 直接持有 Executor 引用，是为了 RunGroup 这种"立即执行"的场景。
    //    这是一个轻微的架构妥协——理想情况下应该通过 Service 层间接调用，但直接调用
    //    Executor 避免了 Service 层需要感知"手动触发 vs 定时触发"的区别。
    "cronix/internal/application/scheduler"

    // cronix/internal/application/service：应用服务包，封装了所有业务逻辑。
    // 🏗️ 架构意义：这是 Handler 的"大脑"。Handler 自己不思考，所有决策都交给 Service。
    //    这种分层模式叫 Thin Controller / Fat Service（瘦控制器 / 胖服务）。
    "cronix/internal/application/service"

    // github.com/gin-gonic/gin：高性能 HTTP Web 框架。
    // 🔬 底层原理：Gin 的核心竞争力来自三个底层设计：
    //    1. 基数树路由（Radix Tree）：路由查找 O(k)，k 是 URL 段数，不随路由数量增长
    //    2. sync.Pool 复用 Context：每个请求不用 new 一个 Context，从池子里借一个用完还回去
    //    3. 零分配的 JSON 渲染：gin.H 本质是 map[string]any，底层用 encoding/json 的 Marshal
    //    ⚡ 性能基准：Gin 在 10K 路由规模下，QPS 可达 ~50万/s（单核），延迟 P99 < 1ms
    "github.com/gin-gonic/gin"
)

// ============================================================
// 🏗️ 【架构设计·GroupHandler 结构体】
//
// 📌 【大厂面试·核心考点】
// Q: 为什么 GroupHandler 要同时持有 GroupSvc、TaskSvc、Executor 三个依赖？
// A: 这是 **依赖注入（Dependency Injection）** 模式。好比前台接待员桌上有三个电话：
//    - 一个打给"组管理经理"（GroupSvc）
//    - 一个打给"任务管理经理"（TaskSvc）
//    - 一个打给"执行车间主任"（Executor）
//    接待员不用知道经理怎么干活（不依赖具体实现），只需要知道电话号码（接口/指针）。
//    这样做的好处：
//    1. 单测时可以用 Mock 替换真实 Service（依赖倒置）
//    2. Service 升级/重构不影响 Handler（松耦合）
//    3. 一眼就能看出这个 Handler 依赖了什么（显式依赖，无隐藏全局变量）
//
// 🏗️ 【架构设计·模式对比】
// 方案A（当前）：结构体字段注入
//   优点：显式、可测试、Go 惯用方式
//   缺点：字段多了结构体变大，但通常不超过 5-6 个
// 方案B：构造函数注入 → func NewGroupHandler(g *GroupService, ...) *GroupHandler
//   优点：可以在构造时做参数校验（如 nil check）
//   缺点：参数列表长了不好看，但可以用 Option 模式缓解
// 方案C：全局变量 / init() 注入（❌ 反模式）
//   缺点：无法并行测试、隐藏依赖、上线后排查问题困难
//
// ⚡ 【性能实战·内存布局】
// GroupHandler 只有 3 个指针字段，大小 = 3 × 8 bytes = 24 bytes（64位系统）。
// 整个应用生命周期内只创建一个实例（单例），内存开销可以忽略不计。
// ============================================================
type GroupHandler struct {
    GroupSvc *service.GroupService
    TaskSvc  *service.TaskService
    Executor *scheduler.Executor
}

// ============================================================
// 📖 ListGroups — 列出所有任务组
//
// HTTP 签名：GET /groups
// 请求参数：无
// 响应格式：{ "code": 0, "message": "ok", "data": [ {group1}, {group2}, ... ] }
//
// 🔬 【底层原理·深度剖析】
// 这个函数展示了 Handler 层的"三板斧"标准模式：
//   1. 调用 Service（h.GroupSvc.ListGroups()）
//   2. 错误处理（if err != nil → 500）
//   3. 返回响应（respondOK）
// 没有任何业务判断、数据过滤、权限校验——这些全部属于 Service/Middleware 的职责。
//
// 📌 【大厂面试·核心考点】
// Q: 为什么 `if groups == nil { groups = []model.TaskGroup{} }` 要做这个空切片转换？
// A: 这是**前端友好性**的关键细节！
//    - nil slice 序列化为 JSON 后是 `null`：  {"data": null}
//    - 空 slice 序列化为 JSON 后是 `[]`：     {"data": []}
//    前端代码通常写 `data.forEach(...)` 或 `data.map(...)`，
//    如果拿到 null，直接 `.forEach` 就会报 TypeError: Cannot read property 'forEach' of null。
//    这一行代码虽然只有 1 行，但能避免前端 100% 的空数据崩溃问题。
//
// 💀 【踩坑血泪·反面教材】
// 真实事故案例：某电商大促，商品列表接口返回 null 而不是 []，
// 前端 App 首页白屏 30 分钟，影响 GMV 损失数百万。
// 修复方法就是加了这一行 nil → []model.XXX{} 的转换。
// 教训：**所有返回数组的接口，永远不要返回 null，永远返回空数组！**
//
// ⚡ 【性能实战·生产调优】
// - 当组数量 < 1000 时，全量查询性能足够（~1ms，走内存排序）。
// - 如果组数量 > 10000，建议加分页（Offset/Limit）或游标分页（Cursor-based Pagination）。
// - 游标分页的优势：不会像 Offset 分页那样在深翻页时扫描大量无用行。
//   例如 OFFSET 10000 LIMIT 20 需要扫 10020 行，而游标只扫 20 行。
// ============================================================
func (h *GroupHandler) ListGroups(c *gin.Context) {
    groups, err := h.GroupSvc.ListGroups()
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    // 📌 nil → 空切片转换，确保 JSON 序列化输出 [] 而非 null，防止前端 TypeError
    if groups == nil {
        groups = []model.TaskGroup{}
    }
    respondOK(c, groups)
}

// ============================================================
// 📖 CreateGroup — 创建新的任务组
//
// HTTP 签名：POST /groups
// 请求体：{ "name": "数据备份组", "mode": "parallel", "cron_expr": "0 2 * * *" }
// 响应格式：{ "code": 0, "message": "ok", "data": { ...创建后的完整对象... } }
//
// 🔬 【底层原理·深度剖析：ShouldBindJSON 的内部机制】
// c.ShouldBindJSON(&g) 内部做了 4 件事（一层一层剥洋葱）：
//   1. 读取 Request.Body（io.ReadAll）→ 拿到原始 []byte
//   2. json.Unmarshal(body, &g) → 通过反射（reflect）将 JSON 字段映射到 struct 字段
//   3. 字段匹配规则：先看 `json:"xxx"` tag，没有 tag 就用字段名的小驼峰形式
//   4. 类型校验：如果 JSON 里 "name" 是数字但 struct 里是 string，会返回 error
//
// 📌 【大厂面试·核心考点】
// Q: ShouldBindJSON 和 BindJSON 有什么区别？
// A: BindJSON 绑定失败时会**自动写入 400 响应并 c.Abort()**，后续代码仍会执行但响应已锁定，
//    容易造成"重复写响应"的 panic。ShouldBindJSON 只返回 error，让你自己决定怎么处理。
//    生产环境**一律用 ShouldBindXXX 系列**，绝不用 BindXXX。
//
// 🛡️ 【安全攻防·漏洞防线】
// - **Mass Assignment（批量赋值攻击）**：恶意用户可能在 JSON 里偷塞 `"id": 999` 字段，
//   试图覆盖自增主键。防御手段：
//   1. GORM 的 Create 方法会忽略零值主键（id=0），非零主键会被当作 Upsert——但这取决于配置。
//   2. 更安全的做法：用 DTO 结构体只暴露允许的字段，再手动映射到 model。
// - **JSON 炸弹（Zip Bomb 的 JSON 版）**：攻击者发送一个 1GB 的 JSON Body。
//   防御：在 Gin 中间件层或反向代理（Nginx）设置 Body Size 限制（如 client_max_body_size 1m）。
//
// 💀 【踩坑血泪·反面教材】
// 某团队用 BindJSON 而不是 ShouldBindJSON，在绑定失败后继续执行业务逻辑，
// 导致 nil 指针 panic，服务反复 crash-restart，最终触发 K8s OOMKilled。
// 根因：BindJSON 已经写了 400 响应，后面再写 200 响应会 panic: http: wrote more than...
// ============================================================
func (h *GroupHandler) CreateGroup(c *gin.Context) {
    var g model.TaskGroup
    // 📌 ShouldBindJSON：从请求体解析 JSON 并绑定到结构体。
    //    失败原因通常是：JSON 格式错误、必填字段缺失、类型不匹配。
    if err := c.ShouldBindJSON(&g); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    // 📌 业务校验（如 name 非空、mode 合法性）全部委托给 Service 层，Handler 不做重复校验
    if err := h.GroupSvc.CreateGroup(&g); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    // 📌 返回创建后的完整对象（含数据库自动填充的 id、created_at 等字段）
    //    这是 RESTful 最佳实践：POST 创建后返回完整资源，前端无需再发一次 GET 请求
    respondOK(c, g)
}

// ============================================================
// 📖 GetGroup — 获取单个组的详情（含组成员列表）
//
// HTTP 签名：GET /groups/:id
// 响应格式：{ "code": 0, "message": "ok", "data": { "group": {...}, "members": [...] } }
//
// 🏗️ 【架构设计·资源聚合模式（Aggregate Fetch）】
// 这个接口返回了 group + members 两个维度的数据，是一种**服务端聚合**模式：
//   - 方案A（当前）：一次请求返回 group + members → 减少 HTTP 往返，前端体验好
//   - 方案B：分两个接口 GET /groups/:id + GET /groups/:id/members → RESTful 纯粹但多一次 RTT
//   对于管理后台场景，方案A 更合适（管理员打开详情页就想看全貌）。
//   对于公开 API / 微服务内部调用，方案B 更灵活（消费者按需取数据）。
//
// 📌 【大厂面试·核心考点】
// Q: strconv.ParseUint(c.Param("id"), 10, 64) 的错误被 _ 忽略了，这样安全吗？
// A: 这是一个**有意识的权衡**。当 id 不是合法数字时，ParseUint 返回 0，
//    后续 GetGroup(0) 必然查不到记录，会走到 404 分支——所以行为上是安全的。
//    但严格来说，这样做**丢失了错误的真实原因**（是 id 格式错，不是组不存在）。
//    更严谨的做法是：先校验 id 合法性，返回 400 Bad Request 而非 404 Not Found。
//    不过对于内部管理系统，这种简化是可接受的（URL 是前端拼的，不是用户手输的）。
//
// 💀 【踩坑血泪·ParseUint 的隐藏陷阱】
// ParseUint 返回的是 uint64，但 GORM 主键通常是 uint（32 位系统上是 uint32）。
// 如果用户传入一个超过 uint32 最大值的 id（如 4294967296），强转 uint() 后会**溢出为 0**，
// 然后查到 id=0 的记录——这可能是一条不该被看到的记录！
// 防御：在转换后校验 id != 0，或者直接用 uint64 作为主键类型。
//
// ⚡ 【性能实战·N+1 查询问题的萌芽】
// 当前实现发了 2 次 SQL 查询（1 次查 group，1 次查 members）。
// 如果是列表场景（比如"列出所有组，每个组附带成员数"），就变成 1 + N 次查询。
// 解决方案：GORM 的 Preload("Tasks") 可以用 1 次 IN 查询替代 N 次单独查询。
// 但单体详情页只有 2 次查询，优化优先级极低。
// ============================================================
func (h *GroupHandler) GetGroup(c *gin.Context) {
    // 📌 从 URL 路径参数提取组 ID，c.Param("id") 返回的是字符串，需要转成数字
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    g, err := h.GroupSvc.GetGroup(uint(id))
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "group not found"})
        return
    }
    // 📌 获取组成员列表。即使查询失败（err 被 _ 忽略），members 为 nil 也会在下面被兜底为空数组
    members, _ := h.GroupSvc.GetGroupMembers(uint(id))
    // 📌 同 ListGroups 的 nil → [] 防御，保证前端拿到的 members 永远是数组
    if members == nil {
        members = []model.Task{}
    }
    // 📌 手动构造聚合响应（group + members），没用 respondOK 是因为 data 结构与标准不同
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"group": g, "members": members}})
}

// ============================================================
// 📖 UpdateGroup — 部分更新任务组属性
//
// HTTP 签名：PUT /groups/:id
// 请求体：{ "name": "新名字", "mode": "sequential" }（只传需要改的字段）
// 响应格式：{ "code": 0, "message": "ok" }
//
// 🔬 【底层原理·深度剖析：map[string]interface{} 的设计权衡】
// 为什么用 map 而不是 struct 来接收更新数据？
//   - struct 的问题：Go 的零值语义会吃掉你的数据。比如 `Enabled bool` 的零值是 false，
//     如果用户没传 enabled 字段，ShouldBindJSON 后 Enabled=false，
//     GORM 的 Updates 会把 enabled 从 true 改成 false——用户明明没改这个字段！
//     这是 Go + GORM 组合中**最经典的坑**，面试必问。
//   - map 的优势：只有用户显式传入的字段才会出现在 map 里，没传的字段 map 里根本没有。
//     GORM 的 Updates(map) 只更新 map 中存在的 key，完美实现 PATCH 语义。
//   - map 的劣势：丢失了类型安全，需要在 Service 层做额外的类型断言和校验。
//
// 📌 【大厂面试·核心考点】
// Q: GORM 的 Save vs Updates vs Update 有什么区别？
// A:
//   - Save：全量更新（所有字段，包括零值），等价于 SQL UPDATE ... SET col1=?, col2=?, ...
//   - Updates(struct)：只更新非零值字段（零值被 GORM 跳过），这是坑的来源
//   - Updates(map)：只更新 map 中存在的 key，最精确
//   - Update("col", val)：更新单个字段
//   生产环境做 PATCH 操作时，**永远用 Updates(map)**。
//
// 🛡️ 【安全攻防·漏洞防线】
// 用 map[string]interface{} 接收前端数据时，攻击者可以注入任意字段名：
//   {"id": 1, "created_at": "2000-01-01"}  ← 试图篡改主键和创建时间！
// 防御策略：在 Service 层做**白名单过滤**（只允许 name/mode/description/enabled/cron_expr）。
// 当前 Service 层已经做了 mode 字段的合法性校验，但缺少字段白名单——这是待改进项。
// ============================================================
func (h *GroupHandler) UpdateGroup(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    // 📌 用 map 接收部分更新字段，避免 struct 零值吃数据的经典坑
    var updates map[string]interface{}
    if err := c.ShouldBindJSON(&updates); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    // 📌 所有校验逻辑（mode 合法性等）在 Service 层完成，Handler 不越权
    if err := h.GroupSvc.UpdateGroup(uint(id), updates); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    respondOKMsg(c, "ok")
}

// ============================================================
// 📖 DeleteGroup — 删除任务组（级联清理关联数据）
//
// HTTP 签名：DELETE /groups/:id
// 响应格式：{ "code": 0, "message": "ok", "data": { "tasks_affected": 5, "logs_deleted": 120 } }
//
// 🔬 【底层原理·深度剖析：级联删除策略的三种流派】
// 删除一个"父资源"时，它的"子资源"怎么处理？数据库界有三大流派：
//
//   1. CASCADE（级联删除）：父亲死了，儿子们全部陪葬。
//      - 适用场景：日志、评论、订单明细——没有父实体就毫无意义的数据。
//      - 本项目中：GroupExecutionLog 跟着 Group 一起删除（日志没有脱离组独立存在的意义）。
//
//   2. SET NULL（断亲不灭门）：父亲死了，儿子们的"父亲"字段被设为 NULL，变成自由人。
//      - 适用场景：任务、员工——核心资产，不能因为分组解散就被删除。
//      - 本项目中：Task 的 group_id 被设为 NULL（任务仍然存在，只是不属于任何组了）。
//
//   3. RESTRICT（禁止动手）：只要有儿子在，就不允许删父亲，返回错误。
//      - 适用场景：财务系统——有关联交易的账户严禁删除。
//      - 用户体验差，需要先手动解绑所有关联，适合审计严格的场景。
//
// 📌 【大厂面试·核心考点】
// Q: 为什么 DeleteGroup 返回 tasks_affected 和 logs_deleted？
// A: 这是**操作透明度**的最佳实践。用户执行危险操作（删除）时，应该被告知影响范围：
//    "你刚刚解绑了 5 个任务，删除了 120 条日志"。
//    这样用户可以验证操作是否符合预期，也方便事后审计追溯。
//    对比反面案例：某团队的删除接口只返回 "ok"，用户误删后完全不知道波及了什么。
//
// ⚡ 【性能实战·事务锁的代价】
// DeleteGroup 在 Service 层用了数据库事务，事务期间会持有行级锁：
//   - InnoDB 的行锁粒度：通过索引定位的行才加行锁，全表扫描会升级为表锁
//   - 如果 group_id 列没有索引，WHERE group_id = ? 会触发全表扫描 → 表锁 → 所有并发写阻塞
//   - 优化建议：确保 tasks 表和 group_execution_logs 表的 group_id 列都有索引
//   - 事务持续时间：通常 < 10ms（3 条 SQL），但如果关联数据量大（如 10 万条日志），
//     可能需要 100ms+，此时考虑分批删除（batch delete）
// ============================================================
func (h *GroupHandler) DeleteGroup(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    // 📌 Service 层的 DeleteGroup 是事务操作，返回影响统计供前端展示
    taskCount, logCount, err := h.GroupSvc.DeleteGroup(uint(id))
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    // 📌 返回级联影响的详细统计，让用户/审计员知道这次删除的波及范围
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{
        "tasks_affected": taskCount,
        "logs_deleted":   logCount,
    }})
}

// ============================================================
// 📖 SetMembers — 批量设置组成员（全量替换模式）
//
// HTTP 签名：PUT /groups/:id/members
// 请求体：{ "task_ids": [1, 3, 5, 7] }
// 响应格式：{ "code": 0, "message": "ok" }
//
// 🏗️ 【架构设计·批量操作的两种流派】
//
// 流派1（当前采用）：全量替换（PUT 语义）
//   - 客户端发送"最终态"的完整成员列表
//   - 服务端先清空旧成员，再按新列表重新绑定
//   - 优点：幂等性好（同一请求发多次，结果一样），实现简单
//   - 缺点：并发冲突风险——两个管理员同时编辑，后一个会覆盖前一个的修改
//
// 流派2：增量操作（PATCH 语义）
//   - 客户端发送 { "add": [5, 7], "remove": [3] }
//   - 服务端只做增量变更
//   - 优点：并发友好，传输数据量小
//   - 缺点：实现复杂，不幂等（重复发送会出错）
//
// 📌 【大厂面试·核心考点】
// Q: SetGroupMembers 内部用了事务，为什么批量操作需要事务包装？
// A: 假设要把 [1,3,5] 设为组成员，分两步：先清空旧成员（UPDATE group_id=NULL），
//    再绑定新成员（UPDATE group_id=X）。如果在"清空"和"绑定"之间系统崩溃：
//    - 所有旧成员已被解绑 → 组变成空的
//    - 新成员还没绑上 → 数据不一致
//    事务保证：要么清空+绑定全部成功，要么全部回滚到原状。
//
// 🔬 【底层原理·匿名结构体的 JSON 绑定】
// `var req struct { TaskIDs []uint \`json:"task_ids"\` }` 是 Go 的匿名结构体（Anonymous Struct）。
// 它只在这个函数内部存在，不需要在 model 包里定义。
// 这种写法的好处：
//   1. 减少全局类型污染（不需要为每个接口定义一个 Request 结构体）
//   2. 请求结构一目了然（直接看 Handler 就知道接口要什么参数）
//   3. Go 编译器会为它生成一个匿名的 reflect.Type，JSON 反序列化性能与命名结构体完全一致
//
// 🛡️ 【安全攻防·漏洞防线】
// 攻击者可能发送 `{"task_ids": [99999]}` 试图将不存在或不属于自己的任务拉入组内。
// 防御：Service 层应校验每个 task_id 是否存在且属于当前用户/租户。
// ============================================================
func (h *GroupHandler) SetMembers(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    // 📌 匿名结构体：只为这一个接口定义的轻量级请求模型，避免全局类型膨胀
    var req struct {
        TaskIDs []uint `json:"task_ids"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    // 📌 Service 层的 SetGroupMembers 内部使用事务：先清空旧成员，再绑定新成员
    if err := h.GroupSvc.SetGroupMembers(uint(id), req.TaskIDs); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    respondOKMsg(c, "ok")
}

// ============================================================
// 📖 RunGroup — 手动触发组执行（异步）
//
// HTTP 签名：POST /groups/:id/run
// 请求参数：无
// 响应格式：{ "code": 0, "message": "ok", "data": { "mode": "parallel", "member_count": 3 } }
//
// 🔬 【底层原理·深度剖析：go 关键字的本质】
// `go h.Executor.RunGroup(g, members, "manual")` 这一行是整个文件最有技术含量的代码。
//
// Go 的 goroutine 是**用户态协程**，不是操作系统线程。区别如下：
//   - 操作系统线程：创建成本 ~1MB 栈内存 + ~10μs 系统调用，上下文切换 ~1-5μs
//   - Goroutine：创建成本 ~2KB 初始栈（可动态增长到 1GB），创建耗时 ~300ns
//   - 一台 8GB 内存的机器：最多创建 ~8000 个线程 vs ~400 万个 goroutine
//
// 📌 【大厂面试·核心考点】
// Q: 为什么 RunGroup 要用 `go` 异步执行，而不是同步等结果？
// A: 因为组执行可能需要几十秒甚至几分钟（串行模式下一个接一个跑），
//    如果同步等待，HTTP 连接会被长时间占用，触发以下问题：
//    1. Nginx/ALB 的 proxy_read_timeout 默认 60s，超时后客户端收到 502/504
//    2. 浏览器 XMLHttpRequest 也有超时，用户看到"加载中"转圈到天荒地老
//    3. 长连接占用 goroutine + fd（文件描述符），高并发下会耗尽服务器资源
//    正确做法：立即返回"已受理"（Accepted），后台异步执行，前端轮询状态或用 WebSocket 推送。
//
// 💀 【踩坑血泪·Fire-and-Forget 的代价】
// 当前的 `go func()` 是**发射后不管（Fire-and-Forget）**模式：
//   - 如果 RunGroup 内部 panic，整个进程会崩溃（goroutine 里的 panic 不会被 HTTP recover 中间件捕获！）
//   - 防御方案：在 Executor.RunGroup 入口处加 defer recover()，或使用 errgroup/worker pool 管理
//   - 另一个隐患：如果服务重启，正在执行的 goroutine 会被强杀，执行中的任务可能处于不确定状态
//   - 更健壮的方案：将执行请求写入消息队列（如 Redis Stream / NATS），由独立 Worker 消费
//
// ⚡ 【性能实战·goroutine 泄漏检测】
// 每次 RunGroup 都 `go` 一个新 goroutine，如果 RunGroup 内部有阻塞（如死锁、无限等待），
// goroutine 会泄漏。检测方式：
//   - runtime.NumGoroutine()：定期监控，如果持续增长就有泄漏
//   - pprof goroutine profile：`go tool pprof http://localhost:6060/debug/pprof/goroutine`
//   - goleak 库（go.uber.org/goleak）：在单测中自动检测 goroutine 泄漏
// ============================================================
func (h *GroupHandler) RunGroup(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    // 📌 先查组是否存在——不存在就直接 404，不浪费资源去查成员
    g, err := h.GroupSvc.GetGroup(uint(id))
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "group not found"})
        return
    }
    // 📌 查组成员——空组没法执行，提前拦截，返回明确的错误信息
    members, _ := h.GroupSvc.GetGroupMembers(uint(id))
    if len(members) == 0 {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "group has no members"})
        return
    }
    // 📌 【关键】go 关键字启动异步 goroutine 执行组任务。
    //    "manual" 标识触发来源（手动触发 vs cron 定时触发），方便日志追溯和统计区分。
    //    ⚠️ 注意：g 是指针，members 是切片，传入 goroutine 后主 goroutine 不应再修改它们。
    //    这里是安全的，因为 g 和 members 在本函数之后不会再被使用。
    go h.Executor.RunGroup(g, members, "manual")
    // 📌 立即返回"已受理"的信息，不等待执行完成。前端可通过轮询日志接口查看执行状态。
    respondOK(c, gin.H{"mode": g.Mode, "member_count": len(members)})
}

// ============================================================
// 📖 GetGroupLogs — 分页查询组执行日志
//
// HTTP 签名：GET /groups/:id/logs?page=1&page_size=20
// 响应格式：{ "code": 0, "message": "ok", "data": { "items": [...], "total": 150 } }
//
// 🔬 【底层原理·深度剖析：Offset 分页的原理与陷阱】
// SQL: SELECT * FROM group_execution_logs WHERE group_id=? ORDER BY id DESC LIMIT 20 OFFSET 40
// 数据库引擎执行步骤：
//   1. 通过 group_id 索引找到所有匹配行（假设 150 行）
//   2. 按 id DESC 排序
//   3. 跳过前 40 行（Offset=40）
//   4. 取接下来的 20 行（Limit=20）
// 问题在第 3 步：数据库必须**扫描并丢弃**前 40 行，即使它们不会出现在结果中。
// 当 Offset=10000 时，需要扫 10020 行但只返回 20 行，效率极低。
//
// 📌 【大厂面试·核心考点】
// Q: Offset 分页在深翻页时性能很差，有什么替代方案？
// A: 游标分页（Cursor-based / Keyset Pagination）：
//    - 不用 OFFSET，改用 WHERE id < last_seen_id ORDER BY id DESC LIMIT 20
//    - 无论翻到第几页，始终只扫描 20 行（利用 id 索引直接定位）
//    - 缺点：不能跳页（不能直接跳到第 50 页），只能"下一页/上一页"
//    - 适合无限滚动场景（如微信朋友圈、Twitter Timeline）
//    Offset 分页适合管理后台（总页数少、需要跳页功能）。
//
// ⚡ 【性能实战·分页查询优化要点】
// 1. 确保 (group_id, id) 有复合索引，这样 WHERE + ORDER BY 可以走同一个索引
// 2. COUNT(*) 在 InnoDB 中是全表扫描（没有缓存计数器），如果日志量大（>100 万），
//    考虑用近似值（EXPLAIN 的 rows 字段）或异步统计表
// 3. c.DefaultQuery 的默认值 "1" 和 "20" 保证了即使前端不传参数也不会出错
//
// 🛡️ 【安全攻防·漏洞防线】
// - page_size 没有上限校验！攻击者可以传 `page_size=1000000` 一次拉取所有日志，
//   导致内存暴涨和数据库慢查询。
//   防御：在 Service 层或 Handler 层加上限，如 `if pageSize > 100 { pageSize = 100 }`。
// - page 传负数或 0 会导致 Offset 为负数，虽然 GORM 会转为 0，但最好显式校验。
// ============================================================
func (h *GroupHandler) GetGroupLogs(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    // 📌 DefaultQuery：如果 URL 中没有 page/page_size 参数，使用默认值 "1" / "20"
    //    这保证了接口的健壮性——前端忘传参数也不会 panic
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
    // 📌 Service 层返回三个值：日志列表、总条数（用于前端计算总页数）、错误
    logs, total, err := h.GroupSvc.GetGroupLogs(uint(id), page, pageSize)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    // 📌 返回分页标准格式：items（当前页数据）+ total（总条数）
    //    前端计算总页数：totalPages = Math.ceil(total / pageSize)
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"items": logs, "total": total}})
}
