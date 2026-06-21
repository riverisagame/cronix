/*
📌 【大厂面试·核心考点】 
- 面试官追问：你们项目怎么保证架构不腐化？如果有人偷偷在领域层调了数据库该怎么办？
- 标准答案：单纯的口头约束和 Code Review 是靠不住的。我们通过“架构守卫”（Architecture Guard）理念将其代码化，像这个文件一样，在 CI/CD 流程中加入依赖方向的单元测试。利用 Go AST（抽象语法树）解析所有文件的 import 声明块，断言领域层 `domain/` 绝不能出现 `infrastructure/` 等底层设施的关键字，一旦有人违规立刻熔断 Pipeline，阻止合并。

🏗️ 【架构设计·模式对比】
- 正确做法（依赖倒置原则）：内层（Domain）定义接口协议，外层（Infrastructure）负责实现这些接口。依赖的箭头方向永远是由外向内，内层的核心业务对外部的物理实现一无所知。
- 错误做法（漏水架构）：为了图一时省事，直接在 Domain 层 import 数据库的 ORM 对象或者 Redis 客户端。这会导致核心业务逻辑被具体技术栈死死绑架，将来一旦更换数据库或者重构框架，代码会像剥洋葱一样互相牵连，辣眼且重构成本巨大。
- 本测试的核心使命：就是用自动化代码死死守护这道“防腐层”（Anti-Corruption Layer）屏障。

🧪 【测试工程·质量保障】 
- 业界规范：虽然这里直接使用了 Go 原生的 `go/parser` 纯手工编写，但在大厂真实的商业级落地中，通常会结合 `testify/assert` 库来做更直观和优雅的断言。或者直接引入类似 Java ArchUnit 的思想体系（Go 生态中有类似 arch-go 或 archtest 等开源库）来实现更加 DSL 化、语义化的规则编写。
- 物理零污染原则：此测试纯靠静态读取和分析源码文件来工作，绝不执行任何真正的业务逻辑代码，更不会触碰物理的数据库或生成临时垃圾数据，测试全过程 100% 绿色无损。

💀 【踩坑血泪·反面教材】
- 真实事故：某交易团队的实习生为了快点完成需求，在 `domain/order.go` 里面悄悄 import 了 `infrastructure/redis` 来做缓存判定逻辑。上线后由于机房 Redis 出现网络抖动，导致本该只负责纯算价的内存级核心函数发生了雪崩级超时，引发大量资损。
- 避免方法：通过这种 Architecture Guard 尽早拦截（Shift-Left Testing，也就是测试左移），将架构腐败的问题精准掐死在代码提交的萌芽阶段！
*/
package architecture_test

import (
	// 🔬 【底层原理·深度剖析】
	// go/parser 和 go/token：这是 Go 语言官方极其强大的编译器前端开放能力。
	// parser 就像是一把庖丁解牛的解剖刀，能够精准地把一长串的 Go 源码字符文本彻底解析成结构化的 AST（抽象语法树）。
	// 相比于性能低下、且极易踩坑的正则表达式（Regex）字符串匹配，AST 能够具有上下文感知能力，精确区分哪些是真正的 import 代码、哪些只是不小心带有 import 字样的多行注释。
	// 它的时间复杂度通常被优化在 O(N)（N为文件的字节数），在几十万行级的大型工程中有效避免了“包含特定字符串但不一定是依赖引用”的假阳性误报难题。
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDDDArchitectureBoundaries
// ⚡ 【性能实战·生产调优】
// 请注意此函数在调用 parser.ParseFile 时巧妙地利用了 `parser.ImportsOnly` 标志位。这是一种极致的性能调优技巧。
// 它命令语法分析引擎仅仅解析每个文件最顶部的 package 声明和 import 块，一旦 import 解析完毕立刻暴力中断，绝不往下去解析庞杂的函数体、结构体和长篇的业务逻辑表达式。
// 这一招四两拨千斤，让单个文件的解析耗时直接从几十毫秒被极限压缩至微秒级别，其内存的空间复杂度也出现了断崖式下降（因为无需在堆内存中构建庞大的全量业务语法树）。
// 凭借着这种极限的静态扫描手段，即使你们的项目代码库不断膨胀达到数百万行，跑一趟全面的架构依赖拦截测试也只需要不到一两秒钟时间，对整个 CI/CD 流水线的速度可谓是毫无拖累。
func TestDDDArchitectureBoundaries(t *testing.T) {
	// 1. Verify that the necessary DDD directories exist
	// 🏗️ 【架构设计·模式对比】
	// 用最简单的初二小白都能懂的生活比喻来拆解标准 DDD 的经典四层模型：
	// - Domain（领域层）：是整个公司的核心机密与灵魂心脏（比如独家饮料配方、核心计费算力），它不依赖任何人。
	// - Application（应用层）：是公司的总经理，负责把不同的业务流程请求派发给具体的核心专业人员，自己绝不亲手去写具体的配方计算逻辑。
	// - Infrastructure（基础设施层）：是公司底层的搬运工、快递员和物理仓库，只负责机械的底层干活和数据落盘（MySQL、Redis存储引擎）。
	// - Interfaces/Adapter（接口/适配器层）：是公司的前台接待大厅，专门负责接待形形色色讲不同语言的外部客户（HTTP REST、gRPC、GraphQL 请求解析）。
	requiredDirs := []string{
		"domain",
		"application",
		"infrastructure",
		"interfaces",
	}

	for _, dir := range requiredDirs {
		path := filepath.Join(".", dir)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Errorf("DDD Violation: Required directory %s does not exist", path)
		}
	}

	// If directories don't exist, we can't test imports yet, so stop here to fail predictably
	// 🧪 【测试工程·质量保障】
	// 这是一种防范于未然的极致防御性测试设计模式（Fail-Fast，快速失败）。
	// 当检测到基础目录都不存在时，果断使用 t.FailNow() 当场切断当前 Test 函数的执行流。
	// 这样就彻底避免了下方路径遍历（Walk）找不到目录而产生大量无厘头的错误连环爆炸，让 CI 日志输出保持清爽、精准且有的放矢。
	if t.Failed() {
		t.FailNow()
	}

	// 2. Verify Dependency Rule: Domain layer must not depend on Application, Infrastructure, or Interfaces
	domainDir := filepath.Join(".", "domain")
	fset := token.NewFileSet()

	err := filepath.Walk(domainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			// 🔬 【底层原理·深度剖析】
			// 这里的 token.NewFileSet() 其实扮演着一个“高精度内存坐标定位地图”的关键角色。它忠实记录着接下来解析出的每一个 AST 词法节点所对应的绝对文件行列号信息。
			// 配合 ParseFile 使用时，底层的词法分析器（Scanner）会将纯文本代码切割剥离为一块块标准的 token 词缀，语法分析器（Parser）随后再将其层层嵌套组装为 AST 树状模型。
			// 整个扫描和组装过程是完全物理无状态的，它压根不需要从网络或者 vendor 加载任何真实依赖，也不会去执行任何的二进制编译生成指令，可以说是纯粹靠编译器前端魔法在玩转代码文本。
			node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("Failed to parse Go file %s: %v", path, err)
			}

			for _, imp := range node.Imports {
				// 🛡️ 【安全攻防·漏洞防线】
				// 这一步对于提取出的路径执行 Trim("\"") 操作可以说是重中之重，生死攸关！
				// 原因在于从 AST 语法树结构中直接获取的 imp.Path.Value 取出来的不是普通的字符串，而是带有原始双引号的纯代码“字面量”（String Literal），例如它拿到的是 `"cronix/internal/infrastructure"`。
				// 如果在这里不彻底清洗掉首尾的包裹引号，下方的 strings.HasPrefix 匹配逻辑将会被那颗该死的双引号干扰导致 100% 匹配失败，这等同于精心构筑的安全防御墙瞬间形同虚设，架构漏洞被人悄无声息地合法绕过。
				importPath := strings.Trim(imp.Path.Value, "\"")
				
				// Domain must not depend on outer layers
				forbiddenPrefixes := []string{
					"cronix/internal/application",
					"cronix/internal/infrastructure",
					"cronix/internal/interfaces",
				}

				for _, forbidden := range forbiddenPrefixes {
					// 💀 【踩坑血泪·反面教材】
					// 不良研发习惯：总有开发人员在 Domain 领域的某个极其核心的计算模型类里，想要临时打一行带有系统底层环境信息的日志排查 Bug，
					// 于是代码提示器一按，顺手就偷偷 import 了 `cronix/internal/infrastructure/logger` 这个底层库。就这样，纯净、隔离的纯领域模型层被具体的业务基建设施直接实现了硬性物理污染！
					// 这个 HasPrefix 的条件判断，就好比是一道设置在海关安检门处的灵敏金属探测仪。任何携带了下层非领域依赖的“违禁带刺”代码闯关，都会在此处引发警报器轰鸣，被测试引擎无情爆破并打回重做！
					if strings.HasPrefix(importPath, forbidden) {
						t.Errorf("DDD Violation in %s: Domain layer must not import %s", path, importPath)
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to walk domain directory: %v", err)
	}
}
