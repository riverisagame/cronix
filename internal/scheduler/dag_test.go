// ============================================================
// internal/scheduler/dag_test.go - DAG 引擎测试
//
// DAG = Directed Acyclic Graph = 有向无环图
// 用来表达任务之间的依赖关系：A完成 -> B才能跑
// ============================================================
package scheduler

import "testing"

// TestDAGNoCycle 测试：正常无环的依赖关系
// 场景：任务2和任务3都依赖任务1
//       任务1
//       /   \
//     任务2  任务3
func TestDAGNoCycle(t *testing.T) {
    // 创建一个空的 DAG（有向无环图）
    d := NewDAG()

    // 添加三个节点（节点 = 任务）
    d.AddNode(1)
    d.AddNode(2)
    d.AddNode(3)

    // 添加依赖边：任务2 依赖 任务1
    // 意思是：必须先完成1，才能执行2
    if err := d.AddEdge(1, 2); err != nil {
        t.Fatal(err)
    }

    // 添加依赖边：任务3 依赖 任务1
    if err := d.AddEdge(1, 3); err != nil {
        t.Fatal(err)
    }

    // 拓扑排序：按依赖层级排列
    // 期望结果：第一层[1]，第二层[2, 3]
    layers := d.TopologicalSort()

    // 验证：至少有两层
    if len(layers) < 2 {
        t.Fatalf("期望至少2层，实际 %d 层", len(layers))
    }

    // 验证：第一层应该包含任务1（它不依赖任何人）
    if len(layers[0]) != 1 || layers[0][0] != 1 {
        t.Errorf("第一层期望 [1]，实际 %v", layers[0])
    }

    // 验证：第二层应该包含任务2和任务3（顺序不重要）
    if len(layers[1]) != 2 {
        t.Errorf("第二层期望 2 个节点，实际 %d 个", len(layers[1]))
    }

    t.Logf("拓扑排序结果: %v", layers)
}

// TestDAGCycleDetection 测试：检测循环依赖
// 场景：任务1 -> 任务2 -> 任务3 -> 任务1（形成环）
// 这种配置是不合法的，应该被拒绝
func TestDAGCycleDetection(t *testing.T) {
    d := NewDAG()

    // 添加三个节点
    d.AddNode(1)
    d.AddNode(2)
    d.AddNode(3)

    // 建立链式依赖：1 -> 2 -> 3
    d.AddEdge(1, 2) // 2 依赖于 1
    d.AddEdge(2, 3) // 3 依赖于 2

    // 尝试添加：1 依赖 3（这会导致循环：1->2->3->1）
    // 期望返回错误（因为会产生循环）
    err := d.AddEdge(3, 1)

    // 验证：应该返回错误
    if err == nil {
        t.Fatal("期望检测到循环依赖，但没有返回错误")
    }

    t.Logf("正确检测到循环依赖: %v", err)
}

// TestDAGSelfDependency 测试：检测自身依赖
// 场景：任务依赖自己（这没有意义，应该被拒绝）
func TestDAGSelfDependency(t *testing.T) {
    d := NewDAG()
    d.AddNode(1)

    // 尝试让任务1依赖自己
    err := d.AddEdge(1, 1)

    // 期望返回错误
    if err == nil {
        t.Fatal("期望检测到自身依赖，但没有返回错误")
    }

    t.Logf("正确检测到自身依赖: %v", err)
}

// TestDAGLinearChain 测试：线性依赖链
// 场景：A -> B -> C -> D（每个任务依赖前一个）
// 期望拓扑排序：4层，每层1个节点
func TestDAGLinearChain(t *testing.T) {
    d := NewDAG()

    // 添加4个节点
    d.AddNode(10)
    d.AddNode(20)
    d.AddNode(30)
    d.AddNode(40)

    // 建立链式依赖
    d.AddEdge(10, 20) // 20依赖10
    d.AddEdge(20, 30) // 30依赖20
    d.AddEdge(30, 40) // 40依赖30

    layers := d.TopologicalSort()

    // 期望4层
    if len(layers) != 4 {
        t.Fatalf("期望4层，实际 %d 层", len(layers))
    }

    // 每层应该只有1个节点
    for i, layer := range layers {
        if len(layer) != 1 {
            t.Errorf("第%d层期望1个节点，实际 %d 个", i+1, len(layer))
        }
    }

    t.Logf("链式排序结果: %v", layers)
}

// TestDAGDiamond 测试：菱形依赖（现实中最常见的模式）
// 场景：      A
//           /   \
//          B     C
//           \   /
//             D
// 期望排序：[A], [B, C], [D]
func TestDAGDiamond(t *testing.T) {
    d := NewDAG()

    d.AddNode(1) // A
    d.AddNode(2) // B
    d.AddNode(3) // C
    d.AddNode(4) // D

    // B和C都依赖A
    d.AddEdge(1, 2)
    d.AddEdge(1, 3)

    // D依赖B和C
    d.AddEdge(2, 4)
    d.AddEdge(3, 4)

    layers := d.TopologicalSort()

    if len(layers) != 3 {
        t.Fatalf("期望3层，实际 %d 层", len(layers))
    }

    // 第1层：只有1（不依赖任何人）
    if len(layers[0]) != 1 || layers[0][0] != 1 {
        t.Errorf("第1层期望 [1]，实际 %v", layers[0])
    }

    // 第2层：2和3（都只依赖1，互不依赖，可以并发）
    if len(layers[1]) != 2 {
        t.Errorf("第2层期望2个节点，实际 %d 个", len(layers[1]))
    }

    // 第3层：只有4（依赖2和3都完成）
    if len(layers[2]) != 1 || layers[2][0] != 4 {
        t.Errorf("第3层期望 [4]，实际 %v", layers[2])
    }

    t.Logf("菱形排序结果: %v", layers)
}
