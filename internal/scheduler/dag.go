// ============================================================
// internal/scheduler/dag.go - 有向无环图引擎
//
// DAG = Directed Acyclic Graph = 有向无环图
// "有向"：边有方向（A→B 表示A执行完才能执行B）
// "无环"：不能形成循环（A→B→C→A 是不允许的，会死循环）
// 用途：表达任务之间的依赖关系——A完成后才能开始B
// ============================================================
package scheduler

import "fmt" // 格式化：用于返回错误信息

// DAG 表示一个有向无环图（依赖关系图）
type DAG struct {
    nodes    []uint            // 所有节点的ID列表（节点=一个任务）
    edges    map[uint][]uint   // 依赖关系映射：edges[B] = [A] 表示"B依赖A"（A必须先于B执行）
    inDegree map[uint]int      // 每个节点的入度（入度=该节点依赖多少个其他节点）
}

// NewDAG 创建一个空的有向无环图
func NewDAG() *DAG {
    return &DAG{
        edges:    make(map[uint][]uint),  // 初始化依赖关系映射表
        inDegree: make(map[uint]int),     // 初始化入度映射表
    }
}

// AddNode 向图中添加一个节点（一个任务）
// 参数 id：任务的ID编号
func (d *DAG) AddNode(id uint) {
    // 如果这个节点还没有在edges表中，给它创建一个空的依赖列表
    if _, ok := d.edges[id]; !ok {         // ok=false表示map中不存在这个key
        d.edges[id] = []uint{}             // 创建空列表
    }
    // 如果这个节点还没有在inDegree表中，把入度设为0
    if _, ok := d.inDegree[id]; !ok {
        d.inDegree[id] = 0                 // 没有依赖任何节点，入度为0
    }
    d.nodes = append(d.nodes, id)          // 把ID加入节点列表
}

// AddEdge 添加一条依赖边："to"依赖"from"（from必须先执行，to才能执行）
// 参数 from：被依赖的任务ID（先执行的）
// 参数 to：依赖别人的任务ID（后执行的）
// 返回值：如果添加这条边会导致循环依赖，返回错误
func (d *DAG) AddEdge(from, to uint) error {
    // 检查一：不能依赖自己（A不能依赖A）
    if from == to {
        return fmt.Errorf("self-dependency not allowed: node %d", from)
    }

    // 添加依赖关系：to 依赖 from
    d.edges[to] = append(d.edges[to], from) // 在to的依赖列表中加入from
    d.inDegree[to]++                        // to的入度+1（多了一个依赖）

    // 检查二：添加这条边后，会不会形成环？
    if d.hasCycle() {                                          // 如果形成了环
        // 回滚刚才的修改：把依赖关系撤销掉
        d.edges[to] = d.edges[to][:len(d.edges[to])-1]        // 移除最后添加的那个依赖
        d.inDegree[to]--                                        // 入度减回去
        return fmt.Errorf("adding edge %d -> %d would create a cycle", from, to)
    }

    return nil
}

// hasCycle 使用Kahn算法检查图中是否存在环
// 原理：如果能通过"逐层消除没有依赖的节点"来清空所有节点，说明无环
// 返回值：true=有环（不允许），false=无环（正常）
func (d *DAG) hasCycle() bool {
    // 第一步：复制一份入度数据（不破坏原数据）
    inDeg := make(map[uint]int)
    for k, v := range d.inDegree {
        inDeg[k] = v
    }

    // 第二步：构建反向依赖关系（被谁依赖了）
    // children[A] = [B, C] 表示 B和C都依赖A（A执行完了，B和C才能继续）
    children := make(map[uint][]uint)
    for node, deps := range d.edges {        // 遍历每个节点和它的依赖列表
        for _, dep := range deps {           // dep是node依赖的节点
            children[dep] = append(children[dep], node) // dep被node依赖
        }
    }

    // 第三步：收集所有入度为0的节点（不依赖任何人的节点，可以直接执行）
    queue := []uint{}                        // 队列：存放待处理的节点
    for _, n := range d.nodes {
        if inDeg[n] == 0 {                   // 这个节点没有依赖
            queue = append(queue, n)         // 加入队列
        }
    }

    // 第四步：逐层消除
    count := 0                               // 计数器：总共处理了几个节点
    for len(queue) > 0 {                     // 只要队列还有节点
        node := queue[0]                     // 取出队列的第一个节点
        queue = queue[1:]                    // 从队列中移除它
        count++                              // 处理了一个节点

        // 对于每个依赖当前节点的孩子节点，它们的入度减1
        for _, child := range children[node] {
            inDeg[child]--                   // 减少一个依赖
            if inDeg[child] == 0 {           // 如果孩子节点不再依赖任何人
                queue = append(queue, child) // 加入队列
            }
        }
    }

    // 如果能处理完所有节点，说明没有环；否则有环
    return count != len(d.nodes)
}

// TopologicalSort 拓扑排序：把节点按依赖关系分成多个层级
// 第0层：不依赖任何人的节点（可以同时执行）
// 第1层：只依赖第0层的节点
// 第N层：只依赖第0到N-1层的节点
// 返回值：二维数组，一维是层，二维是该层的节点ID列表
func (d *DAG) TopologicalSort() [][]uint {
    // 第一步：复制入度数据
    inDeg := make(map[uint]int)
    for k, v := range d.inDegree {
        inDeg[k] = v
    }

    // 第二步：构建反向依赖关系
    children := make(map[uint][]uint)
    for node, deps := range d.edges {
        for _, dep := range deps {
            children[dep] = append(children[dep], node)
        }
    }

    var layers [][]uint                          // 最终的分层结果
    remaining := make(map[uint]bool)             // 剩余未处理的节点集合
    for _, n := range d.nodes {
        remaining[n] = true
    }

    // 第三步：逐层处理
    for len(remaining) > 0 {                     // 只要还有未处理的节点
        var layer []uint                         // 当前层的节点列表
        for n := range remaining {                // 在所有剩余节点中
            if inDeg[n] == 0 {                   // 找到入度为0的节点
                layer = append(layer, n)         // 加入当前层
            }
        }
        if len(layer) == 0 {                     // 找不到入度为0的节点（说明有环！理论上不会发生）
            break
        }
        layers = append(layers, layer)           // 把这一层加到结果中
        for _, n := range layer {
            delete(remaining, n)                 // 从剩余集合中移除
            for _, child := range children[n] {
                inDeg[child]--                   // 减少子节点的入度
            }
        }
    }

    return layers
}
