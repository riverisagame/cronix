# Cronix — 任务调度规则

## 一、任务组定时规则

### 注册阶段

引擎启动 `ReloadAll` 时遍历所有已启用任务：

```
if 任务.CronExpr == ""  →  跳过，不注册定时器
if 任务.GroupID != nil  →  跳过，由组的 cron 统一触发
if 无组且有 cron        →  注册到 cron 引擎，按个人表达式触发
```

### 触发阶段（组 cron 到期）

```
组 cron 到期 → groupTrigger(groupID)
  → 加载组内全部成员 (WHERE group_id = ? ORDER BY sort_order)
  → RunGroup(group, members, "cron")
    ├─ parallel   → 所有成员并发执行
    └─ sequential → 按 sort_order 逐个执行
```

### 规则总结

| 条件 | 触发者 |
|------|--------|
| 任务在组内 + 组有 cron | **组的 cron** |
| 任务在组内 + 组无 cron | 只能手动触发组 |
| 任务无组 + 自己有 cron | **自己的 cron** |
| 任务无组 + 自己无 cron | 只能手动触发 |

> 任务加入组后，**自己的 cron 完全被忽略**。组的 cron 控制整个组的执行节奏。

---

## 二、任务依赖规则 (DAG)

### 数据模型

```
task_deps 表:  { TaskID, DependsOnID }
                 ↓          ↓
              等着的      被等的
读法："TaskID 依赖 DependsOnID，DependsOnID 必须先执行成功"
```

### 约束

| 规则 | 说明 |
|------|------|
| 禁止自依赖 | A 不能依赖 A |
| 禁止循环依赖 | A→B→A 会在保存时被拒绝 |
| 只纳入已启用任务 | buildDAG 只查 `enabled=true` |

### 执行机制

```
所有启用任务 → buildDAG() → TopologicalSort() → 分层

层0: [A, B]     ← 入度=0，可并行
层1: [C]        ← 依赖 A 或 B
层2: [D]        ← 依赖 C
```

- 层内**并行**执行（线程池）
- 层间**串行**：等上一层全部跑完才进入下一层
- 依赖**只控制顺序，不检查成败**：A 失败 B 依然会执行

### 触发场景

| 触发方式 | DAG 行为 |
|----------|----------|
| 定时触发（cron） | 全量 DAG 解析，从头到尾执行所有层 |
| 手动触发（RunTaskNow） | 从层0 执行到目标任务所在的层 |
| 组触发（RunGroup） | **不走 DAG**，按组 own 的 parallel/sequential 模式执行 |

### 时序注意

```
B 依赖 A，A cron=6:00，B cron=3:00

3:00 → B 的 cron 到 → DAG 解析 → A 先跑 → B 后跑
6:00 → A 的 cron 到 → DAG 解析 → A 又跑 → B 又跑
```

**依赖链上任何一个任务触发，整条链都会执行。** 被依赖方（A）会被提前拉起来跑。设置依赖时应让被依赖方的 cron 早于依赖方。

---

## 三、任务组 + 依赖 交互

| 组合 | 行为 |
|------|------|
| 任务在组 + 有依赖 | 组触发时走 RunGroup（组模式），不走 DAG；手动触发任务时走 DAG |
| 组内任务互依赖 | 组 sequential 模式下按 sort_order 执行；parallel 模式不保证顺序 |
| 跨组依赖 | 依赖关系跨组生效，手动触发时 DAG 会解析 |
