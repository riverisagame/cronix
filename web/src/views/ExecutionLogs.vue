<!--
  ExecutionLogs.vue -- 执行日志页面组件。
  这个页面展示所有任务的执行记录（全局视图），用户可以：
   - 按任务名称、执行状态、时间范围筛选日志
   - 查看每条日志的详细信息（点击行展开侧边抽屉）
   - 在抽屉中看到完整的输出内容和错误信息
-->

<template>
  <div>
    <!-- 页面标题 -->
    <h2 style="margin-top:0;display:flex;align-items:center;gap:12px">
      Execution Logs
      <el-popconfirm title="Delete ALL execution logs?" @confirm="clearAllLogs">
        <template #reference><el-button size="small" type="danger" :loading="clearing">Clear All</el-button></template>
      </el-popconfirm>
    </h2>

    <el-card shadow="hover">
      <!--
        筛选条件行：任务名称搜索 + 状态筛选 + 时间范围 + 搜索/刷新按钮
      -->
      <el-row :gutter="16" style="margin-bottom:16px">
        <!-- 任务名称搜索框，占 6/24 -->
        <el-col :span="6">
          <el-input v-model="filters.task_name" placeholder="Task name..." clearable @keyup.enter="load">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>

        <!-- 状态筛选下拉框，占 4/24 -->
        <el-col :span="4">
          <el-select v-model="filters.status" placeholder="Status" clearable @change="load" style="width:100%">
            <el-option label="Success" value="success" />
            <el-option label="Failed" value="failed" />
            <el-option label="Timeout" value="timeout" />
            <el-option label="Running" value="running" />
          </el-select>
        </el-col>

        <!-- 时间范围筛选，占 6/24 -->
        <el-col :span="6">
          <el-select v-model="filters.since" placeholder="Time range" clearable @change="load" style="width:100%">
            <el-option label="Last 1 hour" value="1h" />
            <el-option label="Last 6 hours" value="6h" />
            <el-option label="Last 24 hours" value="24h" />
            <el-option label="Last 7 days" value="168h" />
          </el-select>
        </el-col>

        <!-- 搜索和刷新按钮，占 4/24 -->
        <el-col :span="4">
          <el-button type="primary" @click="load"><el-icon><Search /></el-icon> Search</el-button>
          <el-button @click="load"><el-icon><Refresh /></el-icon></el-button>
        </el-col>
      </el-row>

      <!--
        日志数据表格
        max-height="600" 表格最大高度 600px，超出后纵向滚动（表头固定）
        @row-click="showDetail" 点击行时触发 showDetail 函数，打开详情抽屉
        style="cursor:pointer" 鼠标移到行上时变成手形指针（暗示可以点击）
      -->
      <el-table :data="logs" stripe v-loading="loading" max-height="600" @row-click="showDetail" style="cursor:pointer">
        <!-- ID 列 -->
        <el-table-column prop="id" label="ID" width="60" />

        <!-- 任务名称列 -->
        <el-table-column prop="task_name" label="Task" min-width="140" />

        <!-- 状态列：不同状态显示不同颜色的标签 -->
        <el-table-column label="Status" width="100">
          <template #default="{ row }">
            <!--
              三重判断：success -> 绿色(success)，failed -> 红色(danger)，其他 -> 橙色(warning)
            -->
            <el-tag :type="row.status==='success'?'success':row.status==='failed'?'danger':'warning'" size="small">
              {{ row.status?.toUpperCase() }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 触发方式列：schedule(定时)、mannual(手动)、dependency(依赖触发) -->
        <el-table-column prop="trigger_type" label="Trigger" width="80" />

        <!-- 开始时间列 -->
        <el-table-column prop="start_time" label="Time" width="170" />

        <!--
          执行耗时列
          如果任务已结束（有 end_time），显示计算出的时长
          如果还在运行中（没有 end_time），显示 "running" 标签
        -->
        <el-table-column label="Duration" width="100">
          <template #default="{ row }">
            <!--
              如果 end_time 存在（任务已结束），调用 duration 函数计算耗时
            -->
            <span v-if="row.end_time" style="font-size:12px;color:#909399">{{ duration(row.start_time, row.end_time) }}</span>
            <!-- 如果 end_time 不存在（还在运行），显示一个橙色 warning 标签 -->
            <el-tag v-else size="small" type="warning">running</el-tag>
          </template>
        </el-table-column>

        <!-- 退出码列：exit_code 是程序结束时的返回值 -->
        <el-table-column prop="exit_code" label="Exit" width="60" />

        <!--
          输出预览列：显示输出内容的前 80 个字符
          如果内容超过 80 个字符，后面加上 "..." 省略号
          如果没有输出内容，显示灰色 "-" 占位符
        -->
        <el-table-column label="Preview" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <code v-if="row.output" style="font-size:12px;color:#606266">
              {{ row.output?.substring(0,80) }}{{ row.output.length>80?'...':'' }}
            </code>
            <span v-else style="color:#c0c4cc">-</span>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页组件（和 TaskList 中的用法一样） -->
      <div style="margin-top:16px;text-align:right">
        <el-pagination v-model:current-page="page" :total="total" :page-size="20" layout="total,prev,pager,next" @current-change="load" />
      </div>
    </el-card>

    <!--
      详情抽屉：点击某条日志后从右侧滑出，显示该次执行的详细信息
      size="650px" 宽度 650 像素
      direction="rtl" 从右侧滑入
    -->
    <el-drawer v-model="drawerVisible" title="Execution Detail" size="650px" direction="rtl">
      <!--
        v-if="detail"：只有选中了某条日志（detail 不为空）才渲染详情内容
        防止在数据还没加载时访问 detail.xxx 导致报错
      -->
      <template v-if="detail">
        <!-- 标签行：状态 + 触发方式 + 退出码 -->
        <div style="display:flex;gap:10px;margin-bottom:16px">
          <el-tag :type="detail.status==='success'?'success':'danger'">{{ detail.status?.toUpperCase() }}</el-tag>
          <el-tag type="info">{{ detail.trigger_type }}</el-tag>
          <el-tag v-if="detail.exit_code!==null">exit={{ detail.exit_code }}</el-tag>
        </div>

        <!--
          el-descriptions：描述列表组件，用于展示"标签-值"对
          :column="2" 一行两列
          border 显示边框
          size="small" 紧凑尺寸
        -->
        <el-descriptions :column="2" border size="small" style="margin-bottom:16px">
          <el-descriptions-item label="Task">{{ detail.task_name }}</el-descriptions-item>
          <el-descriptions-item label="Cron">{{ detail.cron_expr||'-' }}</el-descriptions-item>
          <el-descriptions-item label="Start">{{ detail.start_time }}</el-descriptions-item>
          <!-- 耗时：如果任务已结束显示计算时长，否则显示 N/A -->
          <el-descriptions-item label="Duration">{{ detail.end_time ? duration(detail.start_time,detail.end_time) : 'N/A' }}</el-descriptions-item>
        </el-descriptions>

        <!--
          程序输出内容块（正常输出）
          绿色标题，浅灰背景代码块
        -->
        <div v-if="detail.output" style="margin-bottom:16px">
          <div style="font-weight:bold;margin-bottom:8px;color:#67C23A">Output</div>
          <pre style="background:#f5f7fa;color:#303133;padding:14px;border-radius:8px;font-size:13px;line-height:1.6;white-space:pre-wrap;word-break:break-all;max-height:450px;overflow:auto;margin:0">{{ detail.output }}</pre>
        </div>

        <!--
          错误信息块（异常输出）
          红色标题，浅红背景代码块
        -->
        <div v-if="detail.error_msg">
          <div style="font-weight:bold;margin-bottom:8px;color:#F56C6C">Error</div>
          <pre style="background:#fef0f0;color:#F56C6C;padding:14px;border-radius:8px;font-size:13px;line-height:1.6;white-space:pre-wrap;word-break:break-all;max-height:300px;overflow:auto;margin:0">{{ detail.error_msg }}</pre>
        </div>
      </template>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
// 导入 Vue 工具
// ref：创建响应式数据
// reactive：创建"响应式对象"（和 ref 类似，但用于对象类型更方便）
// onMounted：页面加载后执行
import { ref, reactive, onMounted } from 'vue'

// 导入日志 API
import { logAPI } from '../api/index'

// 导入图标
import { Search, Refresh } from '@element-plus/icons-vue'

// ---- 响应式数据 ----

// 日志列表数据
const logs = ref<any[]>([])

// 日志总条数（用于分页）
const total = ref(0)

// 当前页码
const page = ref(1)

// 表格加载状态
const loading = ref(false)
const clearing = ref(false)

/**
 * filters 使用 reactive() 创建（而不是 ref()）。
 *
 * reactive 和 ref 的区别：
 *   - ref 适合基本类型（字符串、数字、布尔），访问时需要 .value
 *   - reactive 适合对象类型，访问时不需要 .value，直接 filters.task_name 即可
 *
 * 两者都是响应式的：数据变了，页面自动更新。
 *
 * filters 包含三个筛选字段：
 *   - task_name：按任务名称模糊搜索
 *   - status：按执行状态筛选（success/failed/timeout/running）
 *   - since：按时间范围筛选（1h/6h/24h/168h）
 */
const filters = reactive({ task_name:'', status:'', since:'' })

// 抽屉可见性
const drawerVisible = ref(false)

// 当前查看的日志详情（点击某条日志后赋值）
const detail = ref<any>(null)

/**
 * duration 函数：计算并格式化执行耗时。
 * @param start 开始时间字符串，如 "2024-01-15T08:30:00.000Z"
 * @param end 结束时间字符串
 * @returns 格式化后的耗时字符串，如 "1.5s" 或 "2.3min"
 *
 * 计算逻辑：
 *   1. new Date() 把时间字符串转成日期对象
 *   2. .getTime() 获取时间戳（从 1970-01-01 到现在的毫秒数）
 *   3. 两个时间戳相减 = 时间差（毫秒）
 *   4. 根据差值大小，选择合适的单位显示：
 *      - 小于 1 秒：显示毫秒（ms）
 *      - 小于 1 分钟：显示秒（s），保留两位小数
 *      - 大于等于 1 分钟：显示分钟（min），保留一位小数
 */
function duration(start:string, end:string) {
  // 计算两个时间的毫秒差
  const diff = new Date(end).getTime() - new Date(start).getTime()
  // 如果小于 1000 毫秒（1 秒），显示毫秒
  if (diff<1000) return diff+'ms'
  // 如果小于 60000 毫秒（60 秒 = 1 分钟），转成秒显示
  if (diff<60000) return (diff/1000).toFixed(2)+'s'
  // 大于等于 1 分钟，转成分钟显示
  // toFixed(1) 保留一位小数
  return (diff/60000).toFixed(1)+'min'
}

/**
 * load 函数：从后端加载执行日志列表。
 * 请求参数包括：页码、每页条数（20）、任务名称筛选、状态筛选、时间范围筛选。
 *
 * 注意：|| undefined 的作用 --
 *   如果筛选值为空字符串 ''，后端可能把空字符串当成有效值来查询，导致查不到数据。
 *   所以用 || undefined 把空字符串转成 undefined（未定义），
 *   这样 axios 在发送请求时就会自动忽略这个参数，后端也就不会用它来筛选。
 */
async function load() {
  // 开始加载，表格显示加载动画
  loading.value = true
  try {
    // 调用日志列表 API，传入筛选参数
    const r = await logAPI.list({
      page: page.value,
      page_size: 20,
      task_name: filters.task_name || undefined,   // 空字符串 -> undefined（不传这个参数）
      status: filters.status || undefined,          // 同上
      since: filters.since || undefined             // 同上
    })
    // 把后端返回的数据存起来
    logs.value = r.data.data.items || []
    total.value = r.data.data.total || 0
  }
  finally {
    // 关闭加载动画
    loading.value = false
  }
}

async function clearAllLogs() {
  clearing.value = true
  try {
    const r = await logAPI.clearAll()
    ElMessage.success(`Deleted ${r.data.data.deleted} log entries`)
    load()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.message || 'Failed')
  } finally { clearing.value = false }
}

/**
 * showDetail 函数：点击表格行时触发。
 * @param row 被点击行的数据
 * 把该行数据赋值给 detail，然后打开抽屉。
 */
function showDetail(row: any) {
  detail.value = row            // 记住当前查看的日志数据
  drawerVisible.value = true    // 打开抽屉
}

// 页面加载完成后立即执行 load 函数，从后端获取日志列表
onMounted(load)
</script>
