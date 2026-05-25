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
      <el-button type="primary" @click="router.push('/tasks/new')">
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
          <el-input v-model="search" placeholder="Search by name..." clearable @clear="load" @keyup.enter="load">
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
          <el-select v-model="filterType" placeholder="All Types" clearable @change="load" style="width:100%">
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
          <el-button @click="load"><el-icon><Refresh /></el-icon> Refresh</el-button>
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
      <el-table :data="tasks" stripe v-loading="loading" :row-class-name="rowClass">
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
          Cron 表达式列
          cron_expr 是定时任务的调度表达式，比如 "0 30 8 * * *" 表示每天 8:30
        -->
        <el-table-column prop="cron_expr" label="Cron" width="140">
          <template #default="{ row }">
            <!-- 用 el-tag 标签显示 Cron 表达式，type="info" 灰色标签 -->
            <el-tag size="small" type="info">{{ row.cron_expr }}</el-tag>
          </template>
        </el-table-column>

        <!-- 任务类型列 -->
        <el-table-column label="Type" width="110">
          <template #default="{ row }">
            <!--
              :type="typeColor(row.task_type)" 调用 typeColor 函数，
              根据任务类型返回对应的 ElementPlus 标签颜色名
            -->
            <el-tag :type="typeColor(row.task_type)" size="small">{{ row.task_type }}</el-tag>
          </template>
        </el-table-column>

        <!-- 所属任务组列 -->
        <el-table-column label="Group" width="130">
          <template #default="{ row }">
            <el-tag v-if="row.group_id" size="small" type="warning">G{{ row.group_id }}</el-tag>
            <span v-else style="color:#909399;font-size:12px">—</span>
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
            <el-button size="small" type="primary" @click="router.push('/tasks/'+row.id)" circle><el-icon><Edit /></el-icon></el-button>

            <!--
              手动执行按钮：圆形绿色按钮
              :loading="runningId===row.id" 只有正在执行的那一行按钮显示加载动画
              @click="runTask(row)" 点击时触发手动执行
            -->
            <el-button size="small" type="success" @click="runTask(row)" :loading="runningId===row.id" circle><el-icon><VideoPlay /></el-icon></el-button>

            <!--
              查看日志按钮：圆形默认按钮
              @click="showLogs(row)" 打开侧边抽屉显示该任务的执行历史
            -->
            <el-button size="small" @click="showLogs(row)" circle><el-icon><Tickets /></el-icon></el-button>

            <!--
              删除按钮：使用 el-popconfirm 包裹，点击后弹出确认气泡
              title="Delete this task?" 确认气泡里显示的文字
              @confirm="deleteTask(row.id)" 用户点击"确认"后才真正删除
            -->
            <el-popconfirm title="Delete this task?" @confirm="deleteTask(row.id)">
              <!--
                #reference 插槽：定义触发弹出框的元素
                这里是红色圆形删除按钮
              -->
              <template #reference><el-button size="small" type="danger" circle><el-icon><Delete /></el-icon></el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <!--
        分页组件
        v-model:current-page="page" 双向绑定当前页码
        :total="total" 数据总条数（后端返回的）
        :page-size="20" 每页显示 20 条
        layout="total,prev,pager,next" 布局：显示"共X条"、上一页、页码、下一页
        @current-change="load" 页码变化时重新加载数据
      -->
      <div style="margin-top:16px;text-align:right">
        <el-pagination v-model:current-page="page" :total="total" :page-size="20" layout="total,prev,pager,next" @current-change="load" />
      </div>
    </el-card>

    <!--
      el-drawer：侧边抽屉组件（从右侧滑出的面板）
      v-model="drawerVisible" 绑定显示/隐藏状态
      :title="'History: '+logTaskName" 动态标题（显示任务名称）
      size="700px" 抽屉宽度 700 像素
      direction="rtl" 从右侧滑出（Right To Left）
    -->
    <el-drawer v-model="drawerVisible" :title="'History: '+logTaskName" size="700px" direction="rtl">
      <!--
        如果该任务的执行日志为空，显示提示信息
      -->
      <div v-if="taskLogs.length===0" style="text-align:center;padding:40px;color:#909399">
        <p>No executions yet</p>
      </div>

      <!--
        时间线组件（el-timeline）：按时间顺序展示执行历史
        v-for="log in taskLogs" 循环渲染每条日志
        :key="log.id" 给每条数据一个唯一标识（Vue 用来优化渲染性能）
        :timestamp="log.start_time" 时间线节点旁边显示的时间
        placement="top" 时间标签显示在节点上方
        :color 动态设置节点颜色：成功绿色，失败红色，其他橙色
      -->
      <el-timeline v-else>
        <el-timeline-item v-for="log in taskLogs" :key="log.id" :timestamp="log.start_time" placement="top"
          :color="log.status==='success'?'#67C23A':log.status==='failed'?'#F56C6C':'#E6A23C'">
          <!-- 每条日志用卡片包裹 -->
          <el-card shadow="hover">
            <!-- 标签行：状态标签 + 触发方式标签 + 退出码 -->
            <div style="display:flex;align-items:center;gap:10px;margin-bottom:8px">
              <!-- 状态标签：成功绿色、失败红色 -->
              <el-tag :type="log.status==='success'?'success':'danger'" size="small">{{ log.status?.toUpperCase() }}</el-tag>
              <!-- 触发方式标签：灰色信息标签 -->
              <el-tag size="small" type="info">{{ log.trigger_type }}</el-tag>
              <!--
                退出码（exit_code）：程序运行结束时的返回值，0 表示正常，非 0 表示异常
                v-if="log.exit_code!==null" 如果退出码不为 null 才显示
                !== null 表示"有值就显示"，null 代表还没有结束（任务还在运行中）
              -->
              <span v-if="log.exit_code!==null" style="font-size:12px;color:#909399">exit={{ log.exit_code }}</span>
            </div>
            <!--
              程序输出内容（pre 标签保留原始格式，包括空格和换行）
              背景浅灰，深色文字，圆角边框
              white-space:pre-wrap 保留换行和空格，但允许自动换行
              word-break:break-all 长单词/长字符串自动截断换行（防止撑破容器）
              max-height:200px 最高 200px，超过出现滚动条
            -->
            <pre v-if="log.output" style="background:#f5f7fa;color:#303133;padding:10px;border-radius:6px;font-size:12px;white-space:pre-wrap;word-break:break-all;max-height:200px;overflow:auto;margin:0">{{ log.output }}</pre>
            <!--
              错误信息（如果存在）
              背景浅红色，红色文字，样式和输出内容类似但颜色不同，以示区分
            -->
            <pre v-if="log.error_msg" style="background:#fef0f0;color:#F56C6C;padding:10px;border-radius:6px;font-size:12px;white-space:pre-wrap;word-break:break-all;max-height:150px;overflow:auto;margin:0;margin-top:8px">{{ log.error_msg }}</pre>
          </el-card>
        </el-timeline-item>
      </el-timeline>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
// 导入 Vue 的响应式工具
import { ref, onMounted } from 'vue'
// 导入路由工具
import { useRouter } from 'vue-router'
// 导入任务 API 函数
import { taskAPI } from '../api/index'
// 导入图标组件
import { Plus, Search, Refresh, VideoPlay, Delete, Edit, Tickets } from '@element-plus/icons-vue'
// ElMessage 是 ElementPlus 的消息提示工具（用来在页面上方弹出"操作成功"等提示）
import { ElMessage } from 'element-plus'

// 获取路由跳转工具
const router = useRouter()

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
  // 开始加载，显示表格加载动画
  loading.value = true
  try {
    // 调用 taskAPI.list 发起 GET 请求
    const r = await taskAPI.list({ page:page.value, page_size:20, search:search.value })
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
 * @param row 当前行的任务数据
 * 1. 记录正在运行的任务 ID（让按钮转圈）
 * 2. 调用后端 API 触发执行
 * 3. 弹出"已触发"提示
 * 4. 清除运行状态
 * 5. 刷新列表
 */
async function runTask(row: any) {
  runningId.value = row.id             // 标记：这个任务正在被触发
  await taskAPI.run(row.id)            // 调用 API：POST /tasks/{id}/run
  ElMessage.success('Triggered')      // 弹出绿色成功提示："已触发"
  runningId.value = null               // 清除运行状态（按钮停止转圈）
  load()                               // 刷新任务列表
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
  drawerVisible.value = true        // 打开抽屉
  // 请求该任务的执行日志（第 1 页，每页 50 条）
  const r = await taskAPI.getLogs(row.id, { page:1, page_size:50 })
  taskLogs.value = r.data.data.items || []  // 把日志列表存起来
}

/**
 * onMounted(load)：页面加载完成后，立刻执行 load() 函数。
 * 也就是一进这个页面，就自动从后端拉取任务列表数据。
 */
onMounted(load)
</script>

<style scoped>
/*
  scoped 属性表示这些样式只对当前组件有效，不会影响其他页面。
  :deep() 是 Vue 的"深度选择器"，用于穿透子组件的封装，选中 ElementPlus 表格内部的行元素。
  .disabled-row 把禁用的任务行透明度设为 0.4（看起来暗淡模糊，一眼就知道是停用的任务）。
*/
:deep(.disabled-row) { opacity: 0.4; }
</style>
