package scheduler

import (
	"cronix/internal/infrastructure/config"
	"cronix/internal/domain/model"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

/*
📌 【大厂面试·核心考点】
面试官可能会问：
1. 如何测试复杂DAG调度引擎的正确性？（答：通过构建具有特定拓扑和失败节点的DAG，验证下游节点的阻断和状态流转是否符合预期，使用真实DB或者Mock来验证持久化状态）。
2. 在单元测试中如何验证任务间的先后依赖关系？（答：通过提取执行日志的时间戳进行前置任务EndTime和后置任务StartTime的严格偏序对比）。
3. 遇到上游任务失败，下游任务应该如何处理？（答：应当阻断执行，并且下游任务应当转为Skip或不执行状态，并在Group级别标识为Partial/Failed）。

🔬 【底层原理·深度剖析】
DAG（有向无环图）执行的核心在于**拓扑排序（Topological Sorting）**和**动态并发控制**。
在测试场景下，我们需要验证：
- 出度节点的独立并发（B和C是否能同时开始）
- 入度节点的严格阻塞（B和C必须在A完成后才能开始）
- 失败级联阻断（C失败后，依赖C的D不会被投递到执行线程池）

🧪 【测试工程·质量保障】
- 测试策略：白盒测试，集成测试。通过设置物理可验证的 `db` 状态验证执行器行为。
- 物理零污染规则：虽然这里使用的是 in-memory DB (由 setupExecutorTestDB 提供)，但我们仍要验证数据流转过程不对其它并发测试产生副作用，这里通过动态创建并校验属于特定组 (DAG_Test_Group) 的数据来实现隔离。
- 覆盖率考量：需要覆盖DAG正常执行流、中断流以及时间偏序约束。

💀 【踩坑血泪·反面教材】
错误做法：在测试中使用 `time.Sleep` 来模拟前置任务执行时间，然后去断言时间差。
后果：导致测试变得Flaky（易碎）。CI服务器负载高时会导致随机失败。
正确做法：直接依赖底层引擎的同步或异步回调结果，记录真实的 `StartTime` 和 `EndTime` 并进行 `Before/After` 断言，而不是写死时长。
*/

// ⚡ 【性能实战·生产调优】
// 测试中的并发度（PoolSize: 10）反映了真实生产中执行器的吞吐量。
// 如果DAG拓扑非常宽（例如一个A节点同时触发1000个子节点），
// 这里必须要验证在受限的 Goroutine Pool 下，任务是否会因为资源不足产生死锁（Deadlock），
// 本测试隐式验证了调度线程和执行线程的分离设计是否健壮。

// 🏗️ 【架构设计·模式对比】
// 这里采用了基于DB状态机轮询/回调的模式测试。
// 对比 Actor 模型或纯消息队列模式，基于DB的状态持久化具有更高的容错性（Crash Recovery），
// 虽然牺牲了少量性能，但是对于调度系统，正确性优于极限性能。
// TestExecutor_DAGGroupExecution_Red verifies DAG mode layered execution and blocking mechanisms.
func TestExecutor_DAGGroupExecution_Red(t *testing.T) {
	// 1. Setup in-memory DB and Executor
	db := setupExecutorTestDB(t)

	cfg := &config.Config{
		Executor: config.ExecutorConfig{
			PoolSize: 10,
		},
	}
	engine := NewEngine(db)
	exec, err := NewExecutor(db, cfg, engine)
	require.NoError(t, err)

	// 2. Create Tasks
	// 🛡️ 【安全攻防·漏洞防线】
	// 在真实的命令注入（Command Injection）测试中，我们应该警惕 task.Command 包含危险字符。
	// 但在这里，我们主要是构造受控的 exit status（比如 exit 1）供状态机判断任务失败。
	// Task A: returns success (作为DAG的起点 Root Node)
	taskA := model.Task{
		Name:    "Task A",
		Command: "echo A",
		TaskType: "shell",
		Enabled: true,
	}
	db.Create(&taskA)

	// Task B: returns success, depends on A
	taskB := model.Task{
		Name:    "Task B",
		Command: "echo B",
		TaskType: "shell",
		Enabled: true,
	}
	db.Create(&taskB)

	// Task C: returns error (exit 1), depends on A
	taskC := model.Task{
		Name:    "Task C",
		Command: "exit 1",
		TaskType: "shell",
		Enabled: true,
	}
	db.Create(&taskC)

	// Task D: depends on C (should not run because C fails)
	taskD := model.Task{
		Name:    "Task D",
		Command: "echo D",
		TaskType: "shell",
		Enabled: true,
	}
	db.Create(&taskD)

	// 3. Create Dependencies
	// 🔬 【底层原理·深度剖析】
	// 这里构建的拓扑图是：
	//       -> Task B (成功)
	// Task A
	//       -> Task C (失败) -> Task D (受阻/Skip状态)
	// 这种经典的菱形或分叉拓扑能够最大程度地触发调度器的各种边界条件。
	// 在这套体系中，依赖关系的持久化为有向边 (Edge)，记录在 model.TaskDep 表。
	
	// B -> A (B 依赖 A)
	db.Create(&model.TaskDep{TaskID: taskB.ID, DependsOnID: taskA.ID})
	// C -> A (C 依赖 A)
	db.Create(&model.TaskDep{TaskID: taskC.ID, DependsOnID: taskA.ID})
	// D -> C (D 依赖 C)
	db.Create(&model.TaskDep{TaskID: taskD.ID, DependsOnID: taskC.ID})

	// 4. Create Group
	group := model.TaskGroup{
		Name: "DAG_Test_Group",
		Mode: "dag",
	}
	db.Create(&group)

	members := []model.Task{taskA, taskB, taskC, taskD}

	// 5. Run Group
	exec.RunGroup(&group, members, "cron")

	// 6. Assertions
	// Verify group execution log status
	var gLog model.GroupExecutionLog
	err = db.Last(&gLog).Error
	require.NoError(t, err)

	// Since C failed, group should be partial or failed
	assert.NotEqual(t, "success", gLog.Status, "Group should not succeed since Task C failed")

	// Get individual task execution logs
	var logA, logB, logC, logD model.ExecutionLog
	
	db.Where("task_id = ?", taskA.ID).First(&logA)
	db.Where("task_id = ?", taskB.ID).First(&logB)
	db.Where("task_id = ?", taskC.ID).First(&logC)
	
	// 🧪 【测试工程·质量保障】
	// 下游任务跳过(Skip)状态流转断言：
	// 验证当上游(C)失败时，调度器是否能够正确裁剪执行图（Prune Execution Graph），拦截下游(D)的触发。
	// D should not exist in execution logs because it was blocked by C's failure (级联阻断)
	errD := db.Where("task_id = ?", taskD.ID).First(&logD).Error
	assert.ErrorIs(t, errD, gorm.ErrRecordNotFound, "Task D should NOT run because its dependency Task C failed")

	// 📌 【大厂面试·核心考点】
	// 问：为什么断言时间先后时要考虑 `Equal` 的情况？
	// 答：在极端高并发或时间精度不高的系统中（如MySQL默认的datetime类型可能只精确到秒），
	// 两个连续的任务可能会在同一个毫秒级甚至秒级的时间戳内完成和开始。
	// 因此，时间偏序断言必须是 `StartTime >= EndTime` 而不仅仅是 `>`。

	// Verify timing constraint: B and C must run strictly AFTER A completes (验证DAG拓扑阻塞生效)
	assert.True(t, logB.StartTime.After(*logA.EndTime) || logB.StartTime.Equal(*logA.EndTime), "Task B must wait for Task A to finish (DAG constraint)")
	assert.True(t, logC.StartTime.After(*logA.EndTime) || logC.StartTime.Equal(*logA.EndTime), "Task C must wait for Task A to finish (DAG constraint)")

	assert.NotEqual(t, "success", gLog.Status, "Group should not succeed since Task C failed")
}
