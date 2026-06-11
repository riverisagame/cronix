<!--
  TaskList.vue -- 任务列表页面组件。
  这是任务管理的核心页面，用户可以在这里：
   - 查看所有任务（支持搜索和按类型筛选）
   - 创建新任务、编辑已有任务
   - 手动触发任务执行
   - 查看任务的执行历史（侧边抽屉）
   - 删除任务
   - 切换任务的启用/禁用状态
-->

<template>
  <div>
    <!-- 页面头部：左侧标题，右侧"新建任务"按钮 -->
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
      <h2 style="margin:0">Task Management</h2>
      <!--
        el-button type="primary" 蓝色按钮
        @click="router.push('/tasks/new')" 点击后跳转到新建任务页面
        /tasks/new 路由中，new 会被当作 :id 参数，TaskEdit.vue 根据 id==='new' 判断是新建模式
      -->
      <el-button type="primary" @click="router.push('/tasks/new')" data-testid="btn-new-task">
        <el-icon><Plus /></el-icon> New Task
      </el-button>
    </div>

    <el-card shadow="hover">
      <!--
        操作栏行：搜索框 + 类型筛选 + 刷新按钮
      -->
      <el-row :gutter="16" style="margin-bottom:16px">
        <!-- 搜索框，占 8/24 宽度 -->
        <el-col :span="8">
          <!--
            el-input：搜索输入框
            v-model="search" 双向绑定搜索关键词
            placeholder="Search by name..." 占位提示文字
            clearable 属性：输入框右边出现一个"x"按钮，点击清空内容
            @clear="load" 清空时自动刷新列表
            @keyup.enter="load" 按回车键时自动搜索
          -->
          <el-input v-model="search" placeholder="Search by name..." clearable @clear="load" @keyup.enter="load" data-testid="task-search" size="large">
            <!-- #prefix 插槽：在输入框左侧放一个搜索图标 -->
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>

        <!-- 类型筛选下拉框，占 4/24 宽度 -->
        <el-col :span="4">
          <!--
            el-select：下拉选择框
            v-model="filterType" 双向绑定选中的类型值
            placeholder="All Types" 未选择时显示"全部类型"
            clearable 可清空选择（清空 = 显示全部）
            @change="load" 选择变化时自动刷新列表
            style="width:100%" 宽度撑满
          -->
          <el-select v-model="filterType" placeholder="All Types" clearable @change="load" style="width:100%" data-testid="task-type-filter" size="large">
            <!-- el-option：下拉选项，label 是显示文字，value 是实际值 -->
            <el-option label="Shell" value="shell" />
            <el-option label="HTTP" value="http" />
            <el-option label="Cleanup" value="cleanup" />
            <el-option label="Healthcheck" value="healthcheck" />
          </el-select>
        </el-col>

        <!-- 刷新按钮，占 4/24 宽度 -->
        <el-col :span="4">
          <!-- @click="load" 点击刷新按钮时重新从后端加载任务列表 -->
          <el-button size="large" @click="load" data-testid="btn-refresh-tasks"><el-icon><Refresh /></el-icon> Refresh</el-button>
        </el-col>
      </el-row>

      <!--
        任务数据表格
        :data="tasks" 数据来源是 tasks 响应式数组
        stripe 斑马纹（隔行变色，更易读）
        v-loading="loading" 当 loading 为 true 时，表格显示加载动画（覆盖层）
        :row-class-name="rowClass" 动态设置每一行的 CSS 类名：
          已禁用的任务行会加上 disabled-row 类（透明度降低，视觉上变灰）
      -->
      <template v-if="viewMode === 'table'">
        <el-table v-if="tasks.length > 0" :data="tasks" stripe v-loading="loading" :row-class-name="rowClass" data-testid="task-table">
          <!-- ID 列，宽度 60px -->
          <el-table-column prop="id" label="ID" width="60" />

        <!--
          任务名称列
          min-width="160" 最小宽度 160px，内容多时自动撑宽
        -->
        <el-table-column prop="name" label="Task Name" min-width="160">
          <template #default="{ row }">
            <!--
              已禁用的任务名称变灰（使用 ElementPlus 的 CSS 变量）
              var(--el-text-color-primary) 是主题的主要文字颜色
              var(--el-text-color-disabled) 是禁用状态文字颜色（更浅更灰）
            -->
            <span :style="{color: row.enabled ? 'var(--el-text-color-primary)' : 'var(--el-text-color-disabled)'}">{{ row.name }}</span>
          </template>
        </el-table-column>

        <!--
          Cron 表达式列 或 常驻任务状态列
        -->
        <el-table-column label="Cron / Mode" width="160">
          <template #default="{ row }">
            <template v-if="row.run_mode === 'daemon'">
              <el-tag :type="daemonStatusColor(getDaemonStatus(row.id))">{{ getDaemonStatus(row.id) }}</el-tag>
              <div v-if="getDaemonStatus(row.id) === 'RUNNING'" style="font-size:12px;color:var(--text-secondary);margin-top:2px">Up: {{ getDaemonUptime(row.id) }}</div>
            </template>
            <template v-else>
              <!-- 用 el-tag 标签显示 Cron 表达式，type="info" 灰色标签 -->
              <el-tag type="info">{{ row.cron_expr || 'None' }}</el-tag>
            </template>
          </template>
        </el-table-column>

        <!-- 任务类型列 -->
        <el-table-column label="Type" width="110">
          <template #default="{ row }">
            <!--
              :type="typeColor(row.task_type)" 调用 typeColor 函数，
              根据任务类型返回对应的 ElementPlus 标签颜色名
            -->
            <el-tag :type="typeColor(row.task_type)">{{ row.task_type }}</el-tag>
          </template>
        </el-table-column>

        <!-- 所属任务组列 -->
        <el-table-column label="Group" width="130">
          <template #default="{ row }">
            <el-tag v-if="row.group_name" type="warning">{{ row.group_name }}</el-tag>
            <span v-else style="color:var(--text-secondary);font-size:12px">—</span>
          </template>
        </el-table-column>

        <!-- 启用/禁用开关列，居中对齐 -->
        <el-table-column label="Status" width="110" align="center">
          <template #default="{ row }">
            <!--
              el-switch：开关组件
              :model-value="row.enabled" 单向绑定开关状态（只读显示，不直接修改）
              @change="(val:boolean) => toggleTask(row, val)" 当用户拨动开关时，
                触发 toggleTask 函数（发请求给后端更新任务状态）
                (val:boolean) 是 TypeScript 的类型标注，说明 val 参数是布尔类型
              active-text="ON" 开关打开时显示 ON
              inactive-text="OFF" 开关关闭时显示 OFF
              inline-prompt 文字显示在开关内部
            -->
            <el-switch
              :model-value="row.enabled"
              @change="(val:boolean) => toggleTask(row, val)"
              active-text="ON" inactive-text="OFF"
              inline-prompt
              data-testid="task-toggle"
            />
          </template>
        </el-table-column>

        <!--
          描述列
          show-overflow-tooltip 内容过长时省略并支持悬停查看完整文字
        -->
        <el-table-column prop="description" label="Description" show-overflow-tooltip min-width="140" />

        <!--
          操作列：固定在表格右侧（fixed="right"），不随横向滚动
          width="260" 因为按钮比较多，给宽一点
        -->
        <el-table-column label="Actions" width="260" fixed="right">
          <template #default="{ row }">
            <!--
              编辑按钮：圆形图标按钮（circle），蓝色（primary）
              @click="router.push('/tasks/'+row.id)" 跳转到任务编辑页
            -->
            <el-button type="primary" @click="router.push('/tasks/'+row.id)" circle><el-icon><Edit /></el-icon></el-button>

            <!--
              执行/启停按钮：根据任务模式区分
            -->
            <template v-if="row.run_mode === 'daemon'">
              <el-button type="success" @click="startDaemonTask(row)" :disabled="getDaemonStatus(row.id) === 'RUNNING'" circle title="Start Daemon"><el-icon><VideoPlay /></el-icon></el-button>
              <el-button type="danger" @click="stopDaemonTask(row)" :disabled="getDaemonStatus(row.id) === 'STOPPED' || getDaemonStatus(row.id) === 'FATAL'" circle title="Stop Daemon"><el-icon><VideoPause /></el-icon></el-button>
            </template>
            <template v-else>
              <el-button type="success" @click="runTask(row)" :loading="runningId===row.id" circle data-testid="btn-run-task" title="Run Once"><el-icon><VideoPlay /></el-icon></el-button>
            </template>

            <!--
              查看日志按钮：圆形默认按钮
              @click="showLogs(row)" 打开侧边抽屉显示该任务的执行历史
            -->
            <el-button @click="showLogs(row)" circle data-testid="btn-task-logs"><el-icon><Tickets /></el-icon></el-button>

            <!--
              删除按钮：使用 el-popconfirm 包裹，点击后弹出确认气泡
              title="Delete this task?" 确认气泡里显示的文字
              @confirm="deleteTask(row.id)" 用户点击"确认"后才真正删除
            -->
            <el-popconfirm title="Delete this task?" @confirm="deleteTask(row.id)" data-testid="btn-delete-task">
              <!--
                #reference 插槽：定义触发弹出框的元素
                这里是红色圆形删除按钮
              -->
              <template #reference><el-button type="danger" circle><el-icon><Delete /></el-icon></el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-else description="您还没有创建任何任务，点击右上方 [New Task] 开始吧！">
        <el-button type="primary" @click="router.push('/tasks/new')">New Task</el-button>
      </el-empty>
    </template>

      <!-- 拓扑依赖网络 DAG 视图 (原生无依赖 SVG 方案) -->
      <!-- @Ref: docs/sps/plans/20260527_topology_shutdown_plan.md | @Date: 2026-05-27 -->
      <div v-else class="topology-container" v-loading="loading">
        <div class="topology-wrapper">
          <svg :width="layoutData.width" :height="layoutData.height" style="background:transparent">
            <!-- 关系流向箭头与特效 -->
            <defs>
              <marker id="arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
                <path d="M 0 1.5 L 10 5 L 0 8.5 z" fill="var(--text-secondary)" />
              </marker>
              <marker id="arrow-active" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
                <path d="M 0 1.5 L 10 5 L 0 8.5 z" fill="#10b981" />
              </marker>
              <filter id="neon-glow" x="-20%" y="-20%" width="140%" height="140%">
                <feGaussianBlur stdDeviation="3" result="blur" />
                <feMerge>
                  <feMergeNode in="blur" />
                  <feMergeNode in="SourceGraphic" />
                </feMerge>
              </filter>
            </defs>

            <!-- 激光依赖边 (Glow Path) -->
            <g>
              <path
                v-for="(edge, i) in layoutData.edges"
                :key="'edge-'+i"
                :d="`M ${edge.fromX} ${edge.fromY} C ${(edge.fromX + edge.toX)/2} ${edge.fromY}, ${(edge.fromX + edge.toX)/2} ${edge.toY}, ${edge.toX} ${edge.toY}`"
                fill="none"
                :class="[edge.active ? 'neon-line-active' : 'neon-line']"
                :marker-end="edge.active ? 'url(#arrow-active)' : 'url(#arrow)'"
              />
            </g>

            <!-- 节点容器 (Nodes) -->
            <g
              v-for="node in layoutData.nodes"
              :key="'node-'+node.id"
              :transform="`translate(${node.x}, ${node.y})`"
              @mouseenter="hoveredNodeId = node.id"
              @mouseleave="hoveredNodeId = null"
              style="cursor:pointer"
            >
              <!-- 节点背景毛玻璃底板 -->
              <rect
                width="160"
                height="60"
                rx="6"
                class="glass-node-rect"
                :class="{ 'node-hovered': hoveredNodeId === node.id }"
                @click="showLogs(node)"
                @dblclick="router.push('/tasks/' + node.id)"
              />

              <!-- 状态呼吸灯圆点 -->
              <circle
                cx="18"
                cy="20"
                r="4.5"
                :class="[node.enabled ? 'active-dot' : 'inactive-dot']"
              />

              <!-- 节点 ID 数显 -->
              <text
                x="30"
                y="24"
                class="node-id"
              >#{{ node.id }}</text>

              <!-- 节点任务名称 -->
              <text
                x="16"
                y="46"
                class="node-name"
              >{{ truncateName(node.name) }}</text>

              <!-- 类型状态徽章 -->
              <rect
                x="105"
                y="10"
                width="45"
                height="16"
                rx="3"
                class="type-tag-rect"
                :class="node.task_type"
              />
              <text
                x="127"
                y="21"
                class="type-tag-text"
              >{{ node.task_type.slice(0, 4).toUpperCase() }}</text>

              <!-- 内置单击手动触发逻辑 -->
              <g
                class="quick-run-btn"
                @click.stop="runTask(node)"
                :class="{ 'btn-loading': runningId === node.id }"
                transform="translate(132, 34)"
              >
                <circle cx="10" cy="10" r="8" class="run-circle" />
                <polygon points="8,6 14,10 8,14" class="run-polygon" />
              </g>
            </g>
          </svg>
        </div>
      </div>

      <!-- 分页组件 -->
      <div v-if="viewMode === 'table'" style="margin-top:16px;text-align:right">
        <el-pagination v-model:current-page="page" :total="total" :page-size="20" layout="total,prev,pager,next" @current-change="load" />
      </div>
    </el-card>

    <!--
      el-drawer：侧边抽屉组件（从右侧滑出的面板）
      v-model="drawerVisible" 绑定显示/隐藏状态
    -->
    <el-drawer v-model="drawerVisible" :title="logTaskName" size="80%" direction="rtl" @close="onDrawerClose">
      <el-tabs v-model="activeTab" class="drawer-tabs" style="height: 100%; display: flex; flex-direction: column;">
        
        <!-- Live Console Tab -->
        <el-tab-pane label="Live Console" name="live" style="height: 100%; display: flex; flex-direction: column;">
          <div ref="fullscreenWrapperRef" class="fullscreen-wrapper" style="height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color);">
            <div class="terminal-header">
              <div class="terminal-status">
              <span class="status-dot" :class="liveStatus.toLowerCase()"></span>
              <span class="status-text">{{ liveStatus }}</span>
              <span class="status-time" v-if="liveStatus === 'RUNNING'" style="margin-left: 12px; font-size: 12px; color: #a8b2c1; font-family: var(--font-mono)">Elapsed: {{ liveDurationFormatted }}</span>
            </div>
            
            <el-input v-model="liveSearch" placeholder="Search logs..." class="terminal-search" clearable size="small" data-testid="live-search">
              <template #prefix><el-icon><Search /></el-icon></template>
            </el-input>

            <el-button size="small" type="primary" plain @click="toggleFullscreen" data-testid="btn-fullscreen">
              <el-icon><FullScreen /></el-icon> Fullscreen
            </el-button>

            <el-popconfirm title="Are you sure to kill this task?" confirm-button-type="danger" @confirm="killLiveTask" :disabled="liveStatus !== 'RUNNING'">
              <template #reference>
                <el-button type="danger" size="small" :disabled="liveStatus !== 'RUNNING'" data-testid="btn-kill-task">
                  <el-icon><VideoPause /></el-icon> Kill Task
                </el-button>
              </template>
            </el-popconfirm>
          </div>

          <div 
            class="terminal-body" 
            ref="liveTerminalRef" 
            @scroll="handleLiveScroll"
          >
            <!-- 渲染高亮 HTML，如果没有输入则直接展示内容 -->
            <pre v-if="liveLogs" class="terminal-content" v-html="highlightedLogs"></pre>
            <div v-else class="terminal-empty">Waiting for execution logs...</div>
            
            <!-- Auto-scroll indicator/button -->
            <div class="scroll-resume-btn" v-show="!liveAutoScroll" @click="resumeAutoScroll">
              Resume auto-scroll
            </div>
          </div>
          </div>
        </el-tab-pane>

        <!-- Execution History Tab -->
        <el-tab-pane label="Execution History" name="history">
          <div v-if="taskLogs.length===0" style="text-align:center;padding:40px;color:var(--text-secondary)">
            <p>No executions yet</p>
          </div>

          <el-timeline v-else style="padding: 16px;">
            <el-timeline-item v-for="log in taskLogs" :key="log.id" :timestamp="log.start_time" placement="top"
              :color="log.status==='success'?'#67C23A':log.status==='failed'?'#F56C6C':'#E6A23C'">
              <el-card shadow="hover" style="cursor: pointer; transition: transform 0.2s ease, box-shadow 0.2s ease;" onmouseover="this.style.transform='translateY(-2px)'" onmouseout="this.style.transform='translateY(0)'">
                <div style="display:flex;align-items:center;gap:10px;margin-bottom:8px">
                  <el-tag :type="log.status==='success'?'success':'danger'" effect="dark" size="small">{{ log.status?.toUpperCase() }}</el-tag>
                  <el-tag size="small" type="info" effect="plain">{{ log.trigger_type }}</el-tag>
                  <span v-if="log.exit_code!==null" style="font-size:12px;color:var(--el-text-color-secondary);font-family:var(--font-mono)">{{ log.start_time?.substring(11,19) }}</span>
                  <span style="font-size:12px;color:var(--el-text-color-secondary);margin-left:10px;font-family:var(--font-mono)" v-if="log.end_time">Duration: {{ calculateDuration(log.start_time, log.end_time) }}</span>
                  <div style="margin-left:auto; display:flex; gap:8px;">
                    <el-button size="small" text type="primary" @click.stop="downloadLog(log)"><el-icon><Download /></el-icon></el-button>
                    <el-popconfirm title="Delete this log?" @confirm="deleteLogRecord(log.id)">
                      <template #reference>
                        <el-button size="small" text type="danger" @click.stop><el-icon><Delete /></el-icon></el-button>
                      </template>
                    </el-popconfirm>
                  </div>
                </div>
                <pre v-if="log.output" style="background:#f5f7fa;color:#303133;padding:10px;border-radius:6px;font-size:12px;white-space:pre-wrap;word-break:break-all;max-height:400px;overflow:auto;margin:0">{{ log.output }}</pre>
                <pre v-if="log.error_msg" style="background:#fef0f0;color:#F56C6C;padding:10px;border-radius:6px;font-size:12px;white-space:pre-wrap;word-break:break-all;max-height:300px;overflow:auto;margin:0;margin-top:8px">{{ log.error_msg }}</pre>
              </el-card>
            </el-timeline-item>
          </el-timeline>
          <div v-if="taskLogs.length > 0" style="margin-top:16px;text-align:right">
            <el-pagination v-model:current-page="historyPage" :total="historyTotal" :page-size="10" layout="total,prev,pager,next" @current-change="loadHistory" />
          </div>
        </el-tab-pane>

      </el-tabs>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
// 导入 Vue 的响应式工具
import { ref, onMounted, onUnmounted, computed, nextTick, watch } from 'vue'
// 导入路由工具
import { useRouter } from 'vue-router'
// 导入任务 API 函数
import { taskAPI, daemonAPI, logAPI } from '../api/index'
// 导入图标组件
import { Plus, Search, Refresh, VideoPlay, VideoPause, Delete, Edit, Tickets, FullScreen, Download } from '@element-plus/icons-vue'
// ElMessage 是 ElementPlus 的消息提示工具（用来在页面上方弹出"操作成功"等提示）
import { ElMessage } from 'element-plus'

// 获取路由跳转工具
const router = useRouter()

// 常驻任务相关状态
const daemonStates = ref<Record<number, any>>({})
let daemonTimer: any = null

const fetchDaemonStates = async () => {
  try {
    const res: any = await daemonAPI.getAllStates()
    if (res && res.data && res.data.data) {
      daemonStates.value = res.data.data
    }
  } catch (e) {
    console.error('Failed to fetch daemon states', e)
  }
}

const getDaemonStatus = (id: number) => {
  return daemonStates.value[id]?.status || 'STOPPED'
}

const getDaemonUptime = (id: number) => {
  return daemonStates.value[id]?.uptime || ''
}

const daemonStatusColor = (status: string) => {
  switch (status) {
    case 'RUNNING': return 'success'
    case 'FATAL': return 'danger'
    case 'BACKOFF': return 'warning'
    default: return 'info'
  }
}

const startDaemonTask = async (row: any) => {
  try {
    await taskAPI.startDaemon(row.id)
    ElMessage.success('Daemon start triggered')
    fetchDaemonStates()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || 'Failed to start daemon')
  }
}

const stopDaemonTask = async (row: any) => {
  try {
    await taskAPI.stopDaemon(row.id)
    ElMessage.success('Daemon stop triggered')
    fetchDaemonStates()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || 'Failed to stop daemon')
  }
}

// 视图切换状态：'table' 列表视图，'topology' 依赖拓扑图视图
// @Ref: docs/sps/plans/20260527_topology_shutdown_plan.md | @Date: 2026-05-27
const viewMode = ref<'table' | 'topology'>('table')
const hoveredNodeId = ref<number | null>(null)

// 切换视图模式，并重置分页拉取数据
function toggleView() {
  viewMode.value = viewMode.value === 'table' ? 'topology' : 'table'
  page.value = 1
  load()
}

// 辅助名字截断函数
function truncateName(name: string) {
  return name.length > 13 ? name.slice(0, 11) + '..' : name
}

// Kahn 算法简易分层排版拓扑数据计算属性，对冲循环依赖与大量节点拉取
// @Ref: docs/sps/plans/20260527_topology_shutdown_plan.md | @Date: 2026-05-27
const layoutData = computed(() => {
  if (tasks.value.length === 0) return { nodes: [], edges: [], width: 800, height: 500 }

  // 1. 构建节点映射
  const nodeMap = new Map<number, any>()
  tasks.value.forEach(t => {
    nodeMap.set(t.id, {
      ...t,
      level: 0,
      x: 0,
      y: 0
    })
  })

  // 2. 建立有向依赖图邻接表与入度信息
  const inDegree = new Map<number, number>()
  const adj = new Map<number, number[]>()
  tasks.value.forEach(t => {
    inDegree.set(t.id, 0)
    adj.set(t.id, [])
  })

  tasks.value.forEach(t => {
    if (t.depends_on_ids) {
      t.depends_on_ids.forEach((depId: number) => {
        if (nodeMap.has(depId)) {
          // depId 指向 t.id
          adj.get(depId)!.push(t.id)
          inDegree.set(t.id, (inDegree.get(t.id) || 0) + 1)
        }
      })
    }
  })

  // 3. 执行层级划分
  const queue: number[] = []
  inDegree.forEach((deg, id) => {
    if (deg === 0) queue.push(id)
  })

  const levels = new Map<number, number>()
  tasks.value.forEach(t => levels.set(t.id, 0))

  let count = 0
  // 设置最大处理上限，防止循环依赖导致栈溢出
  while (queue.length > 0 && count < tasks.value.length * 2) {
    const curr = queue.shift()!
    count++
    const currLevel = levels.get(curr) || 0
    const neighbors = adj.get(curr) || []
    
    neighbors.forEach(next => {
      const nextLevel = Math.max(levels.get(next) || 0, currLevel + 1)
      levels.set(next, nextLevel)
      inDegree.set(next, inDegree.get(next)! - 1)
      if (inDegree.get(next) === 0) {
        queue.push(next)
      }
    })
  }

  // 4. 将节点分配入层级组
  const levelGroups: Map<number, number[]> = new Map()
  levels.forEach((lvl, id) => {
    const safeLvl = Math.min(lvl, 9) // 限制深度，对冲超长画布
    if (!levelGroups.has(safeLvl)) {
      levelGroups.set(safeLvl, [])
    }
    levelGroups.get(safeLvl)!.push(id)
  })

  // 排版定位参数
  const nodeWidth = 160
  const nodeHeight = 60
  const gapX = 230
  const gapY = 90
  const paddingX = 40
  const paddingY = 45

  const sortedLevels = Array.from(levelGroups.keys()).sort((a, b) => a - b)
  const nodes: any[] = []

  sortedLevels.forEach(lvl => {
    const ids = levelGroups.get(lvl) || []
    ids.forEach((id, idx) => {
      const node = nodeMap.get(id)
      if (node) {
        node.level = lvl
        node.x = paddingX + lvl * gapX
        node.y = paddingY + idx * gapY
        nodes.push(node)
      }
    })
  })

  // 5. 计算连接边线并标记激活态
  const edges: any[] = []
  tasks.value.forEach(t => {
    if (t.depends_on_ids) {
      t.depends_on_ids.forEach((depId: number) => {
        const fromNode = nodes.find(n => n.id === depId)
        const toNode = nodes.find(n => n.id === t.id)
        if (fromNode && toNode) {
          const active = hoveredNodeId.value === depId || hoveredNodeId.value === t.id
          edges.push({
            fromId: depId,
            toId: t.id,
            fromX: fromNode.x + nodeWidth,
            fromY: fromNode.y + nodeHeight / 2,
            toX: toNode.x,
            toY: toNode.y + nodeHeight / 2,
            active
          })
        }
      })
    }
  })

  // 6. SVG 画布视口尺寸动态分配
  const maxX = nodes.reduce((max, n) => Math.max(max, n.x + nodeWidth), 0) + paddingX
  const maxY = nodes.reduce((max, n) => Math.max(max, n.y + nodeHeight), 0) + paddingY

  return {
    nodes,
    edges,
    width: Math.max(maxX, 900),
    height: Math.max(maxY, 450)
  }
})

// ---- 以下都是"响应式数据"，数据变化时页面自动更新 ----

// 任务列表数据（数组，每个元素是一条任务记录）
const tasks = ref<any[]>([])

// 任务总条数（用于分页组件显示"共 X 条"）
const total = ref(0)

// 当前页码（从 1 开始）
const page = ref(1)

// 搜索关键词（用户在搜索框输入的文字）
const search = ref('')

// 类型筛选值（空字符串 = 全部显示）
const filterType = ref('')

// 表格加载状态（true 时表格显示加载动画）
const loading = ref(false)

// 正在手动运行的任务 ID（用于显示哪个按钮在转圈）
// <number|null> 表示这个变量可以是数字也可以是 null
const runningId = ref<number|null>(null)

// 抽屉（侧边面板）是否可见
const drawerVisible = ref(false)

// 抽屉标题中显示的任务名称
const logTaskName = ref('')

// 抽屉中显示的任务执行日志列表
const taskLogs = ref<any[]>([])

const historyPage = ref(1)
const historyTotal = ref(0)
const historyTaskId = ref<number | null>(null)

// @Ref: docs/sps/plans/20260611_task_20_log_enhancements.md | @Date: 2026-06-11
const fullscreenWrapperRef = ref<HTMLElement | null>(null)
const toggleFullscreen = () => {
  if (!document.fullscreenElement) {
    fullscreenWrapperRef.value?.requestFullscreen().catch(err => {
      ElMessage.warning(`Error attempting to enable fullscreen: ${err.message}`)
    })
  } else {
    document.exitFullscreen()
  }
}

const liveStartTime = ref(0)
const liveDuration = ref(0)
const liveDurationFormatted = computed(() => {
  const totalSeconds = liveDuration.value
  const m = Math.floor(totalSeconds / 60)
  const s = totalSeconds % 60
  return m > 0 ? `${m}m ${s}s` : `${s}s`
})

const calculateDuration = (start: string, end: string) => {
  if (!start || !end) return ''
  const ms = new Date(end).getTime() - new Date(start).getTime()
  if (ms < 0) return ''
  const s = Math.floor(ms / 1000)
  const m = Math.floor(s / 60)
  if (m > 0) return `${m}m ${s % 60}s`
  return `${ms / 1000}s`
}

const downloadLog = (log: any) => {
  const content = log.output || log.error_msg || ''
  const blob = new Blob([content], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `task_${log.task_id}_log_${log.id}.txt`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

const deleteLogRecord = async (logId: number) => {
  try {
    await logAPI.deleteLog(logId)
    ElMessage.success('Log deleted successfully')
    const idx = taskLogs.value.findIndex(l => l.id === logId)
    if (idx !== -1) taskLogs.value.splice(idx, 1)
  } catch (e: any) {
    ElMessage.error(e.response?.data?.message || 'Failed to delete log')
  }
}

// @Ref: docs/sps/decisions/20260611_ui_ux_log_terminal.md | @Date: 2026-06-11
// Live Streaming State
const activeTab = ref('history')
const liveLogs = ref('')
const liveSearch = ref('')
const liveAutoScroll = ref(true)
const liveStatus = ref('STOPPED') // RUNNING, STOPPED, ERROR
let liveStreamTimer: any = null
const liveTerminalRef = ref<HTMLElement | null>(null)

// 自动滚动监听
watch(liveLogs, () => {
  if (liveAutoScroll.value && liveTerminalRef.value) {
    nextTick(() => {
      liveTerminalRef.value!.scrollTop = liveTerminalRef.value!.scrollHeight
    })
  }
})

// 处理手动滚动，判断是否触底
const handleLiveScroll = (e: Event) => {
  const target = e.target as HTMLElement
  // 考虑到浏览器缩放可能有小数点差异，阈值设为 10px
  const isBottom = Math.abs(target.scrollHeight - target.clientHeight - target.scrollTop) < 10
  liveAutoScroll.value = isBottom
}

// 恢复自动滚动
const resumeAutoScroll = () => {
  liveAutoScroll.value = true
  if (liveTerminalRef.value) {
    liveTerminalRef.value.scrollTop = liveTerminalRef.value.scrollHeight
  }
}

// 高亮搜索日志
const highlightedLogs = computed(() => {
  if (!liveSearch.value) return liveLogs.value
  try {
    // Escape special characters in search string
    const safeSearch = liveSearch.value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const regex = new RegExp(`(${safeSearch})`, 'gi')
    return liveLogs.value.replace(regex, '<mark class="log-highlight">$1</mark>')
  } catch(e) {
    return liveLogs.value
  }
})

// 清除流轮询
const clearLiveStream = () => {
  if (liveStreamTimer) {
    clearInterval(liveStreamTimer)
    liveStreamTimer = null
  }
}

// 抽屉关闭时的清理逻辑
const onDrawerClose = () => {
  clearLiveStream()
}

// 获取日志流
const startLiveStream = async (id: number) => {
  liveLogs.value = ''
  liveStatus.value = 'RUNNING'
  liveStartTime.value = Date.now()
  liveDuration.value = 0
  clearLiveStream()
  
  const fetchLogs = async () => {
    try {
      liveDuration.value = Math.floor((Date.now() - liveStartTime.value) / 1000)
      const res: any = await taskAPI.streamLog(id)
      liveLogs.value = typeof res.data === 'string' ? res.data : (res.data?.data?.output || '')
      
      const logRes: any = await taskAPI.getLogs(id, { page: 1, page_size: 1 })
      const latestLog = logRes?.data?.data?.items?.[0]
      if (latestLog && latestLog.status !== 'running') {
        liveStatus.value = latestLog.status === 'success' ? 'STOPPED' : latestLog.status.toUpperCase()
        clearLiveStream()
        loadHistory()
      }
    } catch(e) {
      liveStatus.value = 'ERROR'
      liveLogs.value += '\n\n[System Error] Failed to fetch live logs or connection lost.'
      clearLiveStream()
    }
  }
  
  await fetchLogs()
  liveStreamTimer = setInterval(fetchLogs, 1500)
}

// 杀掉任务
const killLiveTask = async () => {
  if (!historyTaskId.value) return
  try {
    await taskAPI.kill(historyTaskId.value)
    ElMessage.success('Kill signal sent. Process will terminate shortly.')
    // Let polling catch the STOPPED state
  } catch(e: any) {
    ElMessage.error(e.response?.data?.error || 'Failed to kill task')
  }
}

/**
 * typeColor 函数：根据任务类型返回对应的 ElementPlus 标签颜色名。
 * 返回的对象是"映射表"：键是任务类型，值是 ElementPlus 的标签类型名。
 * shell（Shell脚本）-> ''（默认色），http -> 'success'（绿色），
 * cleanup（清理任务）-> 'warning'（橙色），healthcheck（健康检查）-> 'info'（灰色）
 * || '' 的作用：如果类型不在这四种之中，就用空字符串（默认样式）
 */
function typeColor(type: string) { return { shell:'', http:'success', cleanup:'warning', healthcheck:'info' }[type]||'' }

/**
 * rowClass 函数：给表格行动态添加 CSS 类名。
 * 参数解构 { row } 从传入的对象中取出 row（当前行的数据）。
 * 如果任务未启用（row.enabled 为 false），返回 'disabled-row' 类名，
 * 对应 <style> 中的 .disabled-row 样式（降低透明度）。
 */
function rowClass({ row }: any) { return row.enabled ? '' : 'disabled-row' }

/**
 * load 函数：从后端加载任务列表。
 * async 异步函数，因为要发网络请求。
 * 请求参数包括：当前页码、每页条数（20）、搜索关键词、类型筛选。
 * 注意：类型筛选如果不是空字符串，还会传给后端做精确过滤。
 */
async function load() {
  loading.value = true
  try {
    // 拓扑图需要全局完整的依赖关系数据，批量拉取 1000 个以防止 429 限流
    const pageSize = viewMode.value === 'topology' ? 1000 : 20
    const r = await taskAPI.list({ page:page.value, page_size:pageSize, search:search.value })
    // 把后端返回的 items 数组赋值给 tasks（没有则用空数组）
    tasks.value = r.data.data.items || []
    // 把后端返回的总条数赋值给 total（没有则显示 0）
    total.value = r.data.data.total || 0
  }
  finally {
    // 不管加载成功还是失败，都要关闭加载动画
    loading.value = false
  }
}

/**
 * runTask 函数：手动触发执行某个任务。
 */
async function runTask(row: any) {
  runningId.value = row.id             // 标记：这个任务正在被触发
  logTaskName.value = row.name
  historyTaskId.value = row.id
  activeTab.value = 'live'
  drawerVisible.value = true           // 立即弹出面板，展示流

  try {
    await taskAPI.run(row.id)            // 调用 API：POST /tasks/{id}/run
    ElMessage.success('Triggered')      // 弹出绿色成功提示："已触发"
    startLiveStream(row.id)             // 开始轮询日志
  } catch (e: any) {
    ElMessage.error('Failed to trigger')
  } finally {
    runningId.value = null               // 清除运行状态（按钮停止转圈）
    load()                               // 刷新任务列表
  }
}

/**
 * deleteTask 函数：删除一个任务。
 * @param id 要删除的任务 ID
 */
async function deleteTask(id: number) {
  await taskAPI.delete(id)             // 调用 API：DELETE /tasks/{id}
  ElMessage.success('Deleted')         // 弹出成功提示
  load()                               // 刷新列表
}

/**
 * toggleTask 函数：切换任务的启用/禁用状态。
 * @param row 当前行的任务数据
 * @param val 新的启用状态（true = 启用，false = 禁用）
 */
async function toggleTask(row: any, val: boolean) {
  // 调用 API 更新任务：PATCH/PUT /tasks/{id}，只传 enabled 字段
  await taskAPI.update(row.id, { enabled: val })
  // 根据新状态显示不同的提示文字
  ElMessage.success(val ? 'Enabled' : 'Disabled')
  load()  // 刷新列表
}

/**
 * showLogs 函数：打开侧边抽屉，显示某个任务的执行历史。
 * @param row 当前行的任务数据
 */
async function showLogs(row: any) {
  logTaskName.value = row.name      // 记住任务名（抽屉标题用）
  historyTaskId.value = row.id
  historyPage.value = 1
  activeTab.value = 'history'       // 默认展示历史记录
  drawerVisible.value = true        // 打开抽屉
  await loadHistory()
}

async function loadHistory() {
  if (!historyTaskId.value) return
  // 请求该任务的执行日志
  const r = await taskAPI.getLogs(historyTaskId.value, { page: historyPage.value, page_size: 10 })
  taskLogs.value = r.data.data.items || []  // 把日志列表存起来
  historyTotal.value = r.data.data.total || 0
}

/**
 * onMounted(load)：页面加载完成后，立刻执行 load() 函数。
 * 也就是一进这个页面，就自动从后端拉取任务列表数据。
 */
onMounted(() => {
  load()
  fetchDaemonStates()
  daemonTimer = setInterval(fetchDaemonStates, 3000)
})

onUnmounted(() => {
  if (daemonTimer) {
    clearInterval(daemonTimer)
  }
  clearLiveStream()
})
</script>

<style scoped>
/*
  scoped 属性表示这些样式只对当前组件有效，不会影响其他页面。
  :deep() 是 Vue 的"深度选择器"，用于穿透子组件的封装，选中 ElementPlus 表格内部 of 行元素。
*/
:deep(.disabled-row) { opacity: 0.4; }

/* 拓扑视图容器样式，带有嵌入阴影，对冲溢出 */
.topology-container {
  width: 100%;
  overflow: auto;
  border-radius: 8px;
  background: #ffffff;
  border: 1px solid var(--border-color);
  padding: 15px;
}
.topology-wrapper {
  min-width: 100%;
  display: inline-block;
}

/* 连线基本态与荧光流动激活态 */
.neon-line {
  stroke: var(--border-color);
  stroke-width: 1.5px;
  transition: stroke 0.3s, stroke-width 0.3s;
}
.neon-line-active {
  stroke: #10b981;
  stroke-width: 2.5px;
  filter: url(#neon-glow);
  stroke-dasharray: 6, 4;
  animation: strokeDash 1.2s linear infinite;
  transition: all 0.3s;
}
@keyframes strokeDash {
  to {
    stroke-dashoffset: -20;
  }
}

/* 节点背景毛玻璃底板 */
/* @Ref: docs/sps/plans/20260611_ui_ux_pro_max_upgrade.md | @Date: 2026-06-11 */
.glass-node-rect {
  fill: rgba(255, 255, 255, 0.7);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  stroke: var(--border-color);
  stroke-width: 1px;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}
.glass-node-rect:hover, .node-hovered {
  fill: var(--bg-color-page);
  stroke: rgba(16, 185, 129, 0.8);
  filter: drop-shadow(0 0 8px rgba(16, 185, 129, 0.35));
}

/* 节点内元素 */
.node-id {
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  fill: var(--text-secondary);
}
.node-name {
  font-size: 13px;
  fill: var(--text-main);
  font-weight: 500;
}

/* 状态指示点 */
.active-dot {
  fill: #10b981;
  filter: drop-shadow(0 0 3px #10b981);
  animation: pulseGreen 2s infinite;
}
.inactive-dot {
  fill: #64748b;
}
@keyframes pulseGreen {
  0% { opacity: 0.6; }
  50% { opacity: 1; }
  100% { opacity: 0.6; }
}

/* 任务类型标签 */
.type-tag-rect {
  fill: var(--bg-color-page);
  stroke-width: 1px;
}
.type-tag-rect.shell { stroke: rgba(14, 165, 233, 0.5); fill: rgba(14, 165, 233, 0.1); }
.type-tag-rect.http { stroke: rgba(16, 185, 129, 0.5); fill: rgba(16, 185, 129, 0.1); }
.type-tag-rect.cleanup { stroke: rgba(245, 158, 11, 0.5); fill: rgba(245, 158, 11, 0.1); }
.type-tag-rect.healthcheck { stroke: rgba(100, 116, 139, 0.5); fill: rgba(100, 116, 139, 0.1); }

.type-tag-text {
  font-family: var(--font-mono, monospace);
  font-size: 9px;
  fill: var(--text-secondary);
  text-anchor: middle;
}

/* 快捷执行按钮 */
.quick-run-btn {
  opacity: 0.4;
  transition: opacity 0.2s;
}
.quick-run-btn:hover {
  opacity: 1;
}
.run-circle {
  fill: var(--bg-color-page);
  stroke: var(--border-color);
  stroke-width: 1px;
  transition: all 0.2s;
}
.quick-run-btn:hover .run-circle {
  fill: rgba(16, 185, 129, 0.2);
  stroke: #10b981;
}
.run-polygon {
  fill: var(--text-secondary);
  transition: fill 0.2s;
}
.quick-run-btn:hover .run-polygon {
  fill: #10b981;
}
/* 按钮 Loading 旋转 */
.btn-loading {
  animation: rotateBtn 1s linear infinite;
  opacity: 1;
}
.btn-loading .run-circle {
  stroke: #e6a23c;
  stroke-dasharray: 25, 25;
}
.btn-loading .run-polygon {
  display: none;
}
@keyframes rotateBtn {
  from { transform: translate(132px, 34px) rotate(0deg); }
  to { transform: translate(132px, 34px) rotate(360deg); }
}

/* 
  @Ref: docs/sps/decisions/20260611_ui_ux_log_terminal.md 
  Live Terminal Styling 
*/
.drawer-tabs :deep(.el-tabs__content) {
  height: calc(100% - 55px); 
  overflow: visible;
}

.terminal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  background-color: #f8fafc;
  border: 1px solid #e2e8f0;
  border-bottom: none;
  border-radius: 8px 8px 0 0;
  gap: 16px;
}

.terminal-status {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 120px;
}

.status-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background-color: #64748b;
}
.status-dot.running {
  background-color: #10b981;
  box-shadow: 0 0 8px rgba(16, 185, 129, 0.6);
  animation: pulseGreen 1.5s infinite;
}
.status-dot.error {
  background-color: #ef4444;
}

.status-text {
  font-weight: 600;
  font-size: 13px;
  color: #334155;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.terminal-search {
  flex: 1;
  max-width: 300px;
}

.terminal-body {
  flex: 1;
  background-color: #1e293b;
  color: #f8fafc;
  border-radius: 0 0 8px 8px;
  padding: 16px;
  height: 60vh;
  overflow-y: auto;
  position: relative;
  box-shadow: inset 0 2px 10px rgba(0, 0, 0, 0.2);
}

.terminal-content {
  margin: 0;
  font-family: 'Fira Code', var(--font-mono, monospace);
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}

.terminal-empty {
  color: #64748b;
  font-family: 'Fira Code', var(--font-mono, monospace);
  font-size: 13px;
  text-align: center;
  margin-top: 40px;
}

.scroll-resume-btn {
  position: sticky;
  bottom: 20px;
  left: 50%;
  transform: translateX(-50%);
  background-color: rgba(15, 23, 42, 0.8);
  color: #38bdf8;
  padding: 6px 16px;
  border-radius: 20px;
  font-size: 12px;
  cursor: pointer;
  backdrop-filter: blur(4px);
  border: 1px solid rgba(56, 189, 248, 0.3);
  transition: all 0.2s;
  z-index: 10;
  display: inline-block;
  text-align: center;
}
.scroll-resume-btn:hover {
  background-color: rgba(15, 23, 42, 0.95);
  color: #bae6fd;
}

:deep(.log-highlight) {
  background-color: #f59e0b;
  color: #fff;
  padding: 0 2px;
  border-radius: 2px;
}


</style>
