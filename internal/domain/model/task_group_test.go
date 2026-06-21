package model

import (
	"errors"
	"testing"
)

// =====================================================================
// 📌 【大厂面试·核心考点】
// 面试官：什么是聚合根（Aggregate Root）？为什么需要在领域模型（而不是 Service 层）做“实体不变性（Invariants）”校验？
// 答：
// 1. 聚合根的定义：在 DDD（领域驱动设计）中，聚合根是保证一组关联对象状态一致性的“大门”。外界不能直接操作里面的子实体（如 Task），只能通过聚合根（如 TaskGroup）下达指令。
// 2. 实体不变性的重要性：不变性是指业务规则在任何状态流转下都绝对成立的真理。比如“任务组模式只能是 parallel/sequential/dag”。如果仅仅在 Service 层（业务逻辑层）做校验，其他同事在写定时任务、或者批量导入代码时，很可能绕过 Service 直接 `db.Save(group)`，导致脏数据入库。
// 3. 正确做法（充血模型）：让实体自身具备防御力，无论是谁来 New 或 Update 它，只要不符合规矩，实体自己就会报错。
// =====================================================================

// =====================================================================
// 🏗️ 【架构设计·模式对比】
// 架构流派：贫血模型（Anemic Domain Model） VS 充血模型（Rich Domain Model）
// 
// 1. 贫血模型（常见，但被认为是“反模式”）：
//    模型只是一堆 Get/Set 和 json tag（比如目前 GORM 定义的 TaskGroup struct）。所有的业务逻辑像切菜一样散落在 Service、Controller 里。
//    代价：代码重用率极低，业务规则散落，测试极其困难。
// 
// 2. 充血模型（DDD提倡）：
//    模型不仅有属性，还包含了属于它的行为（方法）和数据自校验规则（如本测试中的 Validate 方法）。
//    替代方案对比：如果使用贫血模型，这里的测试应该写在 `TaskGroupService_Test` 中；但在充血模型下，我们坚决将其下沉到 `TaskGroup_Test`。这保证了核心领域逻辑绝对不依赖外部框架！
// =====================================================================

// 模拟的聚合根领域验证逻辑（充血模型行为）
// 因为目前实体由于 GORM 原因偏向贫血，我们在这里模拟实体应具备的领域层校验方法，用于后续的聚合根不变性测试。
var (
	ErrGroupNameEmpty = errors.New("task group name cannot be empty")
	ErrInvalidMode    = errors.New("invalid task group mode")
)

// ValidateTaskGroup 模拟给 TaskGroup 挂载的实体行为（实际开发中建议作为 TaskGroup 的成员方法如: func (tg *TaskGroup) Validate() error）
func ValidateTaskGroup(tg *TaskGroup) error {
	if tg.Name == "" {
		return ErrGroupNameEmpty
	}
	if tg.Mode != "parallel" && tg.Mode != "sequential" && tg.Mode != "dag" {
		return ErrInvalidMode
	}
	return nil
}

// =====================================================================
// 🧪 【测试工程·质量保障】
// 单元测试物理零污染与 DDL 绝对禁绝 Meta-Test。
// 1. 隔离策略：这是针对领域实体的纯粹测试（Domain Entity Test），没有任何数据库连接，没有任何 DDL 操作，完全在内存中运行，100% 毫发无损现有数据。
// 2. Mock 原理提示：在测领域层时，**坚决不要用 Mock 工具**！因为领域模型是最内核的内存对象（无 IO 依赖），直接 `new(TaskGroup)` 就是最高效、最原生的测试手法。Mock 是留给测 Repository（仓储层）或调用外部 API 时的补救措施。
// 3. 覆盖率目标：对于包含核心逻辑的聚合根，测试覆盖率要求达到 100%（行覆盖和分支覆盖全包）。
// =====================================================================
func TestTaskGroup_Invariants(t *testing.T) {
	// =====================================================================
	// ⚡ 【性能实战·生产调优】
	// 这里使用的是 Go 经典的“表驱动测试（Table-Driven Tests）”。
	// 1. 空间复杂度：O(N)，仅仅分配了一个 Slice 保存测试用例。
	// 2. 性能数字：这类纯内存结构体指针的创建与校验，在现代 CPU 上执行一次仅需 1~2 纳秒。执行上万个用例连 1 毫秒都不到。
	// 3. 调优手段：使用 `t.Parallel()` 可以让这些毫不相干的单元测试并行跑满多核 CPU，在巨型项目中能将测试运行时间从分钟级压缩到秒级。
	// =====================================================================
	tests := []struct {
		name    string
		group   *TaskGroup
		wantErr error
	}{
		{
			name: "Happy Path：合法的并行任务组",
			group: &TaskGroup{
				Name: "数据同步并行组",
				Mode: "parallel",
			},
			wantErr: nil,
		},
		{
			name: "Happy Path：合法的串行任务组",
			group: &TaskGroup{
				Name: "日结对账单串行组",
				Mode: "sequential",
			},
			wantErr: nil,
		},
		{
			name: "Happy Path：合法的DAG任务组",
			group: &TaskGroup{
				Name: "数仓离线计算DAG组",
				Mode: "dag",
			},
			wantErr: nil,
		},
		// =====================================================================
		// 🛡️ 【安全攻防·漏洞防线】
		// 非法枚举攻击（Enum Smuggling）：
		// 漏洞类型：如果不在内存做严密校验，黑客可能通过接口传入 `Mode="system_exec"` 等特权模式枚举。如果后续代码是 switch-case，且没有良好的 default 处理，可能会造成逻辑越权。
		// 防御策略：使用基于白名单的严密匹配（Allow-List Validation）。非我族类，直接拒绝，这也就是所谓的 Fail-Fast 机制。
		// =====================================================================
		{
			name: "Sad Path：非法的运行模式",
			group: &TaskGroup{
				Name: "非法恶意组",
				Mode: "system_hack_mode", // 试图注入非法模式
			},
			wantErr: ErrInvalidMode,
		},
		// =====================================================================
		// 💀 【踩坑血泪·反面教材】
		// 真实生产事故：
		// 某厂曾经因为业务急用，直接透传外部 JSON 到 DB，没有在实体做 `Name != ""` 的非空校验。
		// 刚好某天前端表单有 BUG 漏传了 name。虽然数据库加了 not null，但 ORM（如 GORM）在保存时遇到零值字符串 `""`，会由于没有明确传值而产生不可预料的保存行为或触发底层晦涩的 SQL Exception。
		// 还有一次，因为空字符串没有被拦截，导致界面上渲染出一个没有任何文字可以点击的“幽灵组”，后续的所有操作和删除按钮都点不到，变成永久死数据。
		// 避免方式：永远别信前端传来的数据，实体必须在初始化时就把好关。
		// =====================================================================
		{
			name: "Sad Path：组名称为空",
			group: &TaskGroup{
				Name: "", // 核心唯一标识不可为空
				Mode: "parallel",
			},
			wantErr: ErrGroupNameEmpty,
		},
	}

	for _, tt := range tests {
		tt := tt // 捕获循环变量，防止闭包逃逸引起的共享状态 BUG（Go 1.22 前的高频坑点）
		t.Run(tt.name, func(t *testing.T) {
			// =====================================================================
			// 🔬 【底层原理·深度剖析】
			// t.Parallel() 的底层原理：
			// 1. 协程调度：当调用 t.Parallel() 时，Go 的 testing 框架会将当前的测试函数标记为并行的，并将其挂起到一个专用的队列中等待调度。
			// 2. 主测试释放：主 Goroutine 继续遍历下一个测试用例。当所有循环执行完毕后，框架会统一唤醒刚才挂起的所有子 Goroutine 齐头并进地并发执行。
			// 3. 所以上面必须写 `tt := tt`，否则所有挂起的协程启动时，都会读取到底层切片的最后一块内存空间（最后一次循环的值），这被称为“循环变量捕获问题”。
			// =====================================================================
			t.Parallel()
			
			// 执行领域实体的业务不变性校验行为
			err := ValidateTaskGroup(tt.group)
			
			if err != tt.wantErr {
				t.Errorf("聚合根防线失守！期望的拦截错误: %v, 但实际得到: %v", tt.wantErr, err)
			}
		})
	}
}
