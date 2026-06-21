package cache_test

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"cronix/internal/infrastructure/cache"

	"github.com/stretchr/testify/assert"
)

// ============================================================
// internal/infrastructure/cache/cache_test.go - 缓存机制深度测试与验证
//
// 📌 【大厂面试·核心考点】
// 面试官问：在 Go 里面如何排查并证明你的缓存是并发安全的？
// 答：在编写并发测试时，启动多个 goroutine 分别进行读写操作，并在执行 `go test` 时强制带上 `-race` 参数。
// （例如：`go test -v -race ./...`）。Data Race Detector 会通过编译器插桩，在运行期监控内存访问。
// 测试代码中必须包含对同一个 Key 甚至整个结构的高频读写交织。只要检测器没有报警，结合互斥锁/读写锁的原理，即可证明并发安全性。
//
// 🧪 【测试工程·质量保障】
// 严守【物理零污染】底线：
// 本测试文件绝对纯粹。由于被测对象 `Cache` 是纯粹的内存映射结构（map），未注入任何外部持久化句柄，
// 所以测试全程在内存沙盒中分配和销毁，绝对不会污染真实的业务数据库、不会生成物理残留文件。
// 物理源码层绝对不存在、也无需任何 DROP、TRUNCATE 或 CREATE TABLE 等高危 DDL 语句。
//
// 🔬 【底层原理·深度剖析】
// 这里的 `TestCache_ConcurrentReadWrite` 是如何利用 sync.WaitGroup 引爆高并发冲突的？
// 传统的顺序测试无法触发竞争。我们启动了 1000 个写协程和 1000 个读协程，几乎同一纳秒对相同范围的 Key 进行突发（Burst）读写。
// 若无底层的 `sync.RWMutex`，Go runtime 在检测到 `concurrent map read and map write` 时会直接引发不可恢复的 `fatal error`（panic 甚至不能被 recover 捕获）。
// 测试稳定通过即证明 RWMutex 的临界区屏障（Barrier）严密无缺。
//
// ⚡ 【性能实战·生产调优】
// 本地缓存测试中要注意时间粒度的问题。如果 TTL 设为 10 秒，切勿直接使用 `time.Sleep(10 * time.Second)`！
// 在真实的持续集成（CI）千亿级系统中，毫秒级以上的 Sleep 会严重阻塞测试流水线，导致 Pipeline 拥堵。
// （由于目前底层代码未注入虚拟时钟 Mock Clock，为了不改变底层代码的前提下展现真实的惰性删除，
// 我们取了一个折中的 50ms 极短 TTL。最优雅的做法是重构底层，传入时钟依赖）。
//
// 💀 【踩坑血泪·反面教材】
// 新人写并发测试时，经常将外层的变量在 Go routine 中不传参直接引用，导致典型的“循环变量捕获问题”
// 导致实际上所有的 goroutine 都在操作同一个 `i`（尽管 Go 1.22+ 修复了此问题，但为了向下兼容与工程规范，必须将变量做参数传递）。
//
// 🛡️ 【安全攻防·漏洞防线】
// 在 TTL 淘汰测试中，对于过期时间的判定往往存在“临界缝隙”问题（Off-By-One 或纳秒级时间差）。
// 攻击者可能利用过期判定算法的取整漏洞读取“已废弃的会话Token”。
// 因此测试必须验证：过期后返回的是绝对的 `false` 以及安全无害的零值。
//
// 🏗️ 【架构设计·模式对比】
// 【自我攻击】这个 `map` 的实现在高并发写入下就是垃圾。由于无容量上限（Capacity Limit），无主动扫描逐出（Active Eviction），
// 如果遭到黑客的恶意扫描，不断生成一次性随机 Key，内存将极速膨胀并导致 OOM。
// 另外，Go 的 Map `delete` 操作并不缩容。在海量增删场景下，这里极易形成内存黑洞。本测试只能验证逻辑正确性，无法修补架构层缺陷。
// ============================================================

// TestCache_TTL_Eviction 测试缓存的写入、获取以及 TTL（保质期）过期淘汰机制
func TestCache_TTL_Eviction(t *testing.T) {
	// 初始化缓存，设置极其短暂的 TTL：50 毫秒，避免长时间阻塞测试
	c := cache.New[string](50 * time.Millisecond)

	t.Run("写入并立即读取_应该命中", func(t *testing.T) {
		c.Set("user_1001", "张三")
		val, ok := c.Get("user_1001")
		assert.True(t, ok, "缓存应该命中")
		assert.Equal(t, "张三", val, "缓存的值应该一致")
	})

	t.Run("等待TTL过期后读取_应该阻断并返回false", func(t *testing.T) {
		// 阻塞等待 60 毫秒，确保一定超过 50 毫秒的 TTL，进行极速过期验证
		time.Sleep(60 * time.Millisecond)

		val, ok := c.Get("user_1001")
		// 🛡️ 漏洞防线：验证惰性删除是否无情地拦截了该脏数据
		assert.False(t, ok, "缓存已过期，必须返回 false，防止脏数据泄漏")
		assert.Equal(t, "", val, "过期后应当返回泛型的干净零值，绝不残留业务数据")
	})

	t.Run("安全删除与清空测试", func(t *testing.T) {
		c.Set("user_1002", "李四")
		c.Delete("user_1002")
		_, ok := c.Get("user_1002")
		assert.False(t, ok, "手动 Delete 后，数据必须被安全销毁，不应再被读取到")

		c.Set("user_1003", "王五")
		c.Set("user_1004", "赵六")
		c.Clear()
		_, ok1 := c.Get("user_1003")
		_, ok2 := c.Get("user_1004")
		assert.False(t, ok1, "执行 Clear 清空后，所有数据均应当消失")
		assert.False(t, ok2, "执行 Clear 清空后，所有数据均应当消失")
	})
}

// TestCache_ConcurrentReadWrite 极限并发读写测试（Data Race 测试核心）
func TestCache_ConcurrentReadWrite(t *testing.T) {
	c := cache.New[int](1 * time.Minute)
	var wg sync.WaitGroup

	goroutines := 1000

	// ⚡ 【性能对冲】并发写测试：利用 1000 个协程模拟流量洪峰，对相同的少量 key 造成极致的哈希碰撞与锁争抢
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			key := "key_" + strconv.Itoa(id%10) // 刻意制造哈希键冲突（聚拢到仅仅 10 个独特的 key 上）
			c.Set(key, id)                      // 并发高频覆写
		}(i)
	}

	// ⚡ 并发读测试：与写测试深度交织在一起，极大增加 RWMutex 读写锁状态机的颠簸率
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			key := "key_" + strconv.Itoa(id%10)
			// 物理零污染读取：我们并不关心读到什么（因为写操作正在乱序竞争），
			// 只要不引发 runtime panic，且配合 go test -race 零报警，即可证明并发壁垒是安全的。
			_, _ = c.Get(key)
		}(i)
	}

	// 并发删除测试：进一步制造混沌测试 (Chaos Testing)
	wg.Add(50)
	for i := 0; i < 50; i++ {
		go func(id int) {
			defer wg.Done()
			key := "key_" + strconv.Itoa(id%10)
			c.Delete(key) // 混入删除操作，干扰底层的 bmap 状态
		}(i)
	}

	// 阻塞主协程，严谨地等待所有的读、写、删协程全部执行完毕
	wg.Wait()
}
