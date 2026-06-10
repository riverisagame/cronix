# 任务 ID: Task-14
# 目标: 激活任务组的 DAG (有向无环图) 调度模式

## 1. 需求复核
- **核心逻辑**：在现有的 `TaskGroup`（任务组）调度体系下，增加第三种运行模式 `dag`。
- **避免影响**：手动执行（RunOnce/RunTaskNow）直接调用 `executeTask`，与组逻辑无关，确保绝对不发生“连动”或副作用。
- **调度细节**：按拓扑排序出的层级（Layers）逐层执行，层内并发。若某一层有任务失败，立即中断整个工作流，不再执行后续层级。

## 2. 修改文件及具体代码 (纳米级)

### 2.1 修改 `internal/model/task_group.go`
- **目标**：补充 `Mode` 字段的注释。
- **修改行**：
  ```go
  // mode="parallel" — all tasks run concurrently.
  // mode="sequential" — tasks run one by one in sort_order.
  // mode="dag" — tasks run layer by layer based on dependency graph.
  Mode        string    `gorm:"default:parallel" json:"mode"`
  ```

### 2.2 修改 `internal/scheduler/executor.go`
- **目标**：在 `RunGroup` 方法的 `switch g.Mode` 中增加 `case "dag":` 逻辑。
- **变量及执行逻辑**：
  ```go
  case "dag":
      // 1. 构建只包含组内成员的局部 DAG
      dag := e.buildDAG(members)
      // 2. 拓扑排序，按层级返回 tasks 的 ID 数组 [][]uint
      layers := dag.TopologicalSort()
      
      layerFailed := false
      for i, layer := range layers {
          if layerFailed {
              log.Warn().Str("group", g.Name).Int("layer", i).Msg("group (dag) stopped due to previous layer failure")
              break
          }
          
          var mu sync.Mutex
          var wg sync.WaitGroup
          
          // 层内并发执行
          for _, taskID := range layer {
              wg.Add(1)
              tid := taskID
              e.pool.Submit(func() {
                  defer wg.Done()
                  e.executeTask(tid)
                  var lastLog model.ExecutionLog
                  e.db.Where("task_id = ?", tid).Order("id DESC").First(&lastLog)
                  
                  mu.Lock()
                  if lastLog.Status == "success" {
                      success++
                  } else {
                      failed++
                      errMsg = lastLog.ErrorMsg
                      layerFailed = true // 标记当前层失败，阻断下一层
                  }
                  mu.Unlock()
              })
          }
          wg.Wait() // 等待当前层全部执行完毕
      }
      log.Info().Str("group", g.Name).Msg("group (dag) completed")
  ```

## 3. 测试方案 (RED-GREEN 强制测试驱动)
- **创建测试**：在 `internal/scheduler/executor_dag_test.go` 中编写 `TestExecutor_DAGGroupExecution`。
- **用例设计**：
  - 创建组并设置 `Mode = "dag"`。
  - 创建任务 A（耗时 50ms）、B（耗时 10ms，依赖 A）、C（耗时 10ms，依赖 A）。
  - 执行 `RunGroup`，验证 B 和 C 是否确实是在 A 结束后（T >= 50ms）才开始执行。
  - 验证 A 如果失败，B 和 C 是否不被执行。
- **安全保障**：测试数据库全部使用 mock 内存模式，严禁操作生产物理表。

## 4. 影响面审查 (Blast Radius)
- **直接影响**：仅影响设置了 `Mode="dag"` 的任务组调度流程，原有的 `parallel` 和 `sequential` 以及 `RunTaskNow` 全面无损。
- **风险对冲**：组内如果发生环形依赖，`TopologicalSort()` 也能处理（会跳出死循环），加上前面保存逻辑已有防环检测，内存安全度高。
