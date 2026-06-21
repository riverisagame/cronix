package model

import (
	"testing"
)

// ============================================================
// 📌 【大厂面试·核心考点】
// 面试官：如果让你设计一个任务调度系统的依赖检测逻辑，你应该怎么写测试用例？
// 标准答案：不仅要测正常情况（单线依赖、菱形依赖、森林结构），更要着重测试【异常边界】情况！
// 比如：1. A依赖A（自环）；2. A依赖B，B依赖A（双向互环）；3. A->B->C->A（长链隐蔽闭环）。
// 以及极端的：完全断开的孤立节点组、极长依赖链等。只有覆盖了这些，才能证明图算法足够鲁棒。
//
// 🔬 【底层原理·深度剖析】
// 在关系型数据库中，TaskDep 表以“边”的形式（TaskID -> DependsOnID）存储图。
// 调度器在执行前，必须将这张表的数据提取到内存中，并将其重建为【邻接表（Adjacency List）】数据结构，
// 然后执行【Kahn算法（基于入度）】或【DFS（深度优先搜索）染色彩色法（白灰黑）】来检测是否是“有向无环图”（DAG）。
// 如果存在环（Cycle），在任务调度中就意味着“死锁”：A 等 B 结束，B 等 A 结束，两个任务永远都在排队。
//
// ⚡ 【性能实战·生产调优】
// 当任务图规模很大（如上万个节点）时，DFS 的时间复杂度是 O(V+E)（顶点数+边数）。
// 由于是在 Go 的内存中执行指针遍历，耗时极短（微秒级），
// 但如果不在入库阶段进行环检测，而是等运行时再去检测甚至不检测，
// 就会导致整个分布式任务线程池被永远挂起的 Zombie 任务耗尽（线程饥饿）。
//
// 🛡️ 【安全攻防·漏洞防线】
// 恶意用户可能会通过 OpenAPI 故意构造具有“长链循环”或“超高分支度（宽依赖）”的图，
// 来对调度引擎进行【逻辑型拒绝服务攻击（Algorithmic DoS）】。
// 如果算法内部采用了过深的递归（没有改用迭代或限制最大深度），可能导致引擎栈溢出（Stack Overflow）直接崩溃。
//
// 🏗️ 【架构设计·模式对比】
// DAG 的检测模式选型：
// 1. 入库时检测（Write-time Detection）：当前最推荐的架构！每次插入 TaskDep 时，都在内存里跑一遍全量检测，如果有环直接拒绝写入，将脏数据拦截在业务之外。
// 2. 运行时检测（Read-time Detection）：极其危险！调度器扫描时才发现环，此时任务已经被置为 Running，撤销的代价极高。
// 
// 💀 【踩坑血泪·反面教材】
// 曾经某云原生工作流引擎发生过重大事故，因为开发者只写了最简单的 A->B 单线依赖测试，
// 结果用户在生产环境配置了复杂的【菱形依赖（Diamond Dependency）】（A依赖B和C，B和C都依赖D），
// 引擎内部算法在回溯遍历时出现了重复计算（没有用哈希表记忆化），导致呈指数级爆炸，最终打爆了节点的CPU。
// 这就是为什么我们要写极其严密的拓扑测试用例的原因！
// ============================================================

// buildAdjacencyList 辅助函数：将数据库的关联数组转化为内存中的邻接表
// 这模拟了调度器在内存中重建图的过程
func buildAdjacencyList(deps []TaskDep) map[uint][]uint {
	// key: 任务ID (等待者), value: 被等待的任务ID切片
	adj := make(map[uint][]uint)
	for _, dep := range deps {
		adj[dep.TaskID] = append(adj[dep.TaskID], dep.DependsOnID)
	}
	return adj
}

// hasCycleDFS 辅助函数：利用 DFS 的三色标记法（白灰黑）检测有向图中是否存在环
func hasCycleDFS(adj map[uint][]uint) bool {
	// 状态字典：0-未访问(White), 1-访问中(Gray), 2-已访问并确认无环(Black)
	state := make(map[uint]int)

	var dfs func(node uint) bool
	dfs = func(node uint) bool {
		// 如果发现状态是 1 (Gray)，说明我们在一次深搜的路径上再次遇到了自己！构成闭环！
		if state[node] == 1 {
			return true
		}
		// 如果状态是 2 (Black)，说明该节点及其后续子图已经完全确认无环，不用重复遍历，这是防止【菱形依赖重复计算】的关键！
		if state[node] == 2 {
			return false
		}

		// 将当前节点标记为访问中（Gray）
		state[node] = 1

		// 继续深度探索它的所有前置依赖
		for _, neighbor := range adj[node] {
			if dfs(neighbor) {
				return true
			}
		}

		// 所有依赖探索完毕都未发现环，将其标记为安全结束（Black）
		state[node] = 2
		return false
	}

	// 遍历邻接表中所有的起始节点（可能存在多个不相交的森林，所以要遍历全部键）
	for node := range adj {
		if state[node] == 0 {
			if dfs(node) {
				return true
			}
		}
	}

	return false
}

// 🧪 【测试工程·质量保障】
// 测试用例设计规范：
// 1. 无环基础场景：单线、树状、菱形
// 2. 有环危险场景：自依赖、直接互锁、间接回环
// 3. 隔离场景：森林（互不相关的几组任务独立存在）
func TestTaskDependencyCycleDetection(t *testing.T) {
	// 表驱动测试（Table-Driven Testing）：Go 语言最推荐的单元测试模式
	tests := []struct {
		name     string
		deps     []TaskDep
		hasCycle bool
	}{
		{
			// 初二小白比喻：排队领书，你等他，他等她，一条直线，很清晰。
			name: "单线依赖 (A -> B -> C)",
			deps: []TaskDep{
				{TaskID: 3, DependsOnID: 2}, // 3等2
				{TaskID: 2, DependsOnID: 1}, // 2等1
			},
			hasCycle: false,
		},
		{
			// 初二小白比喻：组长收作业，所有人（1,2,3）都要先写完，组长（4）才能交上去。
			name: "多分支聚合依赖 (A,B,C -> D)",
			deps: []TaskDep{
				{TaskID: 4, DependsOnID: 1},
				{TaskID: 4, DependsOnID: 2},
				{TaskID: 4, DependsOnID: 3},
			},
			hasCycle: false,
		},
		{
			// 💀 【踩坑血泪·反面教材】
			// 这里就是前面提到的【菱形依赖】。如果在 DFS 遍历时不引入状态 2 (Black) 进行记忆化（Memoization），
			// 算法会在抵达节点 1 时重复遍历多次。节点数一旦庞大，会导致性能急剧衰退！
			name: "菱形依赖 (4等2和3，2和3等1)",
			deps: []TaskDep{
				{TaskID: 4, DependsOnID: 2},
				{TaskID: 4, DependsOnID: 3},
				{TaskID: 2, DependsOnID: 1},
				{TaskID: 3, DependsOnID: 1},
			},
			hasCycle: false,
		},
		{
			// 🛡️ 【安全攻防·漏洞防线】
			// 极端白痴情况：用户不小心提交了“我自己等我自己完成”的配置。
			// 如果没有进行环校验，这种脏数据直接入库，会产生一条永远无法执行完毕的幽灵任务。
			name: "自环依赖 (A -> A)",
			deps: []TaskDep{
				{TaskID: 1, DependsOnID: 1},
			},
			hasCycle: true,
		},
		{
			// 最典型的死锁现象。就像两个过独木桥的人，互不相让。
			name: "双向互环依赖 (A -> B -> A)",
			deps: []TaskDep{
				{TaskID: 1, DependsOnID: 2},
				{TaskID: 2, DependsOnID: 1},
			},
			hasCycle: true,
		},
		{
			// 面试官最爱考的隐蔽死锁：链路中间存在一条向回指的箭头。
			name: "长链隐蔽闭环 (A -> B -> C -> A)",
			deps: []TaskDep{
				{TaskID: 1, DependsOnID: 2},
				{TaskID: 2, DependsOnID: 3},
				{TaskID: 3, DependsOnID: 1},
			},
			hasCycle: true,
		},
		{
			name: "隔离森林 (A->B 和 C->D 互不干扰，但其中一棵树有环)",
			deps: []TaskDep{
				// 第一棵树，无环
				{TaskID: 2, DependsOnID: 1},
				// 第二棵树，有环 (3->4->3)
				{TaskID: 3, DependsOnID: 4},
				{TaskID: 4, DependsOnID: 3},
			},
			hasCycle: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adj := buildAdjacencyList(tt.deps)
			got := hasCycleDFS(adj)
			if got != tt.hasCycle {
				t.Errorf("用例 [%s] 测试失败: 期望 hasCycle = %v, 但是实际得到 %v", tt.name, tt.hasCycle, got)
			}
		})
	}
}
