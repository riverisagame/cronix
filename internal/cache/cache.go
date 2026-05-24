// ============================================================
// internal/cache/cache.go - 内存缓存（带过期时间）
//
// 缓存就像是"临时记事本"——把常用的数据暂存在内存里，
// 下次需要时直接从内存读取，比从数据库查快得多。
// 每条缓存数据都有一个"保质期"（TTL = Time To Live），过期自动作废。
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
