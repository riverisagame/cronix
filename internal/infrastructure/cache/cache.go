// ============================================================
// internal/cache/cache.go - 内存缓存（带过期时间）
//
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 
// 1. 面试官问：什么是缓存（Cache）？为什么要有 TTL（保质期）？
// 答：
// 数据库就像是“新华字典”，里面什么词都有，但查起来很慢，要一页一页翻（磁盘 I/O）。
// 缓存就像是“临时写字板”。如果你刚刚查过“苹果”这个词，就把它写在小板子上。
// 下次再问“苹果”，直接看一眼小板子（内存读取）就回答了，速度快 1000 倍。
// 但小板子空间有限，所以要设 TTL（Time To Live，保质期）。比如 5 分钟后擦掉这个词，把位置腾给别人。
//
// 2. 面试官问：什么是 Go 1.18 引入的泛型 (Generics)？
// 答：
// 以前写程序，如果你要给“苹果”做个盒子，就得写一个“苹果盒子”结构体；如果要装“香蕉”，就得写一个“香蕉盒子”。
// 这叫类型强绑定，代码啰嗦得很。
// 泛型（Generics，代码里的 `[T any]`）就像是造了一个“万能魔术盒”。
// 你告诉盒子 T 是苹果，它就变成苹果盒子；你告诉盒子 T 是香蕉，它就变成香蕉盒子。代码只要写一遍！
//
// 3. 面试官问：你这里的过期清理是“惰性删除 (Lazy Expiration)”还是“定期删除”？
// 答：
// 大厂必考点！我们这里用的是【惰性删除】。
// 也就是数据过期了，它还躺在内存里。只有当你去 `Get` 它的时候，发现它过期了，才假装它不存在（返回 false）。
// 这种方式最省 CPU（不用专门开个后台线程去扫），但缺点是如果有过期数据永远没人查，它就永远占着内存。
// 专业的 Redis 会把【惰性删除】和【定期随机抽查删除】结合起来用。
// ============================================================
package cache

import (
    "sync"   // 并发控制：读写锁
    "time"   // 时间处理：判断是否过期
)

// Item 表示一条缓存数据，包含值和过期时间
// [T any] 是Go的泛型语法，T可以是任何类型（字符串、数字、结构体等）
type Item[T any] struct {
    Value     T         // 缓存的实际数据（类型在创建缓存时确定）
    ExpiresAt time.Time // 过期时间点（超过这个时间就认为数据作废了）
}

// Cache 是一个支持泛型的带过期时间的内存缓存
// T 表示缓存中存储的数据类型
type Cache[T any] struct {
    mu    sync.RWMutex         // 读写锁（RWMutex）：允许多个读者同时读，但写者独占
    items map[string]Item[T]   // 存储缓存数据的映射表，key是字符串，value是Item
    ttl   time.Duration        // TTL（保质期）：每条缓存数据从存入起能活多久
}

// New 创建一个新的缓存实例
// 参数 ttl：缓存数据的保质期（例如 5*time.Minute 表示5分钟）
// 返回值：Cache指针
func New[T any](ttl time.Duration) *Cache[T] {
    return &Cache[T]{
        items: make(map[string]Item[T]), // 初始化空的映射表
        ttl:   ttl,                       // 设置保质期
    }
}

// Get 从缓存中读取一条数据
// 参数 key：数据的键（唯一标识）
// 返回值：数据本身、是否找到（true=找到了，false=没找到或已过期）
func (c *Cache[T]) Get(key string) (T, bool) {
    c.mu.RLock()                                                  // 加读锁（允许多人同时读）
    defer c.mu.RUnlock()                                          // 函数结束时释放读锁
    item, ok := c.items[key]                                      // 查找key对应的缓存项
    if !ok {                                                      // 没找到
        var zero T                                                // 创建T类型的零值（数字的零值是0，字符串是""）
        return zero, false                                        // 返回零值和false
    }
    if time.Now().After(item.ExpiresAt) {                         // 找到了但已过期（After = "在...之后"）
        var zero T
        return zero, false                                        // 过期等同于不存在
    }
    return item.Value, true                                       // 返回有效数据
}

// Set 向缓存中存入一条数据
// 参数 key：数据的键
// 参数 value：要存储的值
func (c *Cache[T]) Set(key string, value T) {
    c.mu.Lock()                                                   // 加写锁（写操作需要独占）
    defer c.mu.Unlock()
    c.items[key] = Item[T]{                                       // 创建缓存项
        Value:     value,                                         // 存储的值
        ExpiresAt: time.Now().Add(c.ttl),                         // 过期时间 = 现在 + 保质期
    }
}

// Delete 从缓存中删除一条数据
// 参数 key：要删除的键
func (c *Cache[T]) Delete(key string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    delete(c.items, key)                                          // Go内建的delete函数，从map中删除键
}

// Clear 清空缓存中的所有数据
func (c *Cache[T]) Clear() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items = make(map[string]Item[T])                            // 创建新的空map，旧数据会被垃圾回收
}
