<!--
  ExecutionLogs.vue -- execution log page with virtual scroll, lazy detail, export.
-->
<template>
  <div>
    <h2 style="margin-top:0;display:flex;align-items:center;gap:12px">
      Execution Logs
      <el-popconfirm title="Delete ALL execution logs?" @confirm="clearAllLogs">
        <template #reference><el-button type="danger" :loading="clearing">Clear All</el-button></template>
      </el-popconfirm>
      <el-button @click="exportLogs('csv')" :loading="exporting" :disabled="exporting" data-testid="btn-export-csv">Export CSV</el-button>
      <el-button @click="exportLogs('json')" :loading="exporting" :disabled="exporting" data-testid="btn-export-json">Export JSON</el-button>
    </h2>

    <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
    <el-card shadow="hover" class="data-card" v-loading="loading">
      <el-row :gutter="16" style="margin-bottom:16px">
        <el-col :span="6">
          <el-input v-model="filters.task_name" placeholder="Task name..." clearable @keyup.enter="load" size="large">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="4">
          <el-select v-model="filters.status" placeholder="Status" clearable @change="load" style="width:100%" data-testid="log-status-filter" size="large">
            <el-option label="Success" value="success" />
            <el-option label="Failed" value="failed" />
            <el-option label="Timeout" value="timeout" />
            <el-option label="Running" value="running" />
          </el-select>
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.since" placeholder="Time range" clearable @change="load" style="width:100%" data-testid="log-time-filter" size="large">
            <el-option label="Last 1 hour" value="1h" />
            <el-option label="Last 6 hours" value="6h" />
            <el-option label="Last 24 hours" value="24h" />
            <el-option label="Last 7 days" value="168h" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-button type="primary" size="large" @click="load"><el-icon><Search /></el-icon> Search</el-button>
          <el-button size="large" @click="load"><el-icon><Refresh /></el-icon></el-button>
        </el-col>
      </el-row>

      <el-auto-resizer>
        <template #default="{ width }">
          <el-table-v2
            :columns="columns"
            :data="logs"
            :width="width"
            :height="600"
            :row-height="56"
            :row-event-handlers="rowEventHandlers"
            style="cursor:pointer"
          />
        </template>
      </el-auto-resizer>

      <div style="margin-top:16px;text-align:right">
        <el-pagination v-model:current-page="page" :total="total" :page-size="20" layout="total,prev,pager,next" @current-change="load" />
      </div>
    </el-card>

    <el-drawer v-model="drawerVisible" title="Execution Detail" size="80%" direction="rtl">
      <div v-if="detailLoading" style="text-align:center;padding:40px;color:var(--text-secondary)" v-loading="true">Loading...</div>
      <template v-else-if="detail">
        <div style="display:flex;gap:10px;margin-bottom:16px">
          <el-tag :type="detail.status==='success'?'success':'danger'">{{ detail.status?.toUpperCase() }}</el-tag>
          <el-tag type="info">{{ detail.trigger_type }}</el-tag>
          <el-tag v-if="detail.exit_code!==null">exit={{ detail.exit_code }}</el-tag>
        </div>
        <el-descriptions :column="2" border style="margin-bottom:16px">
          <el-descriptions-item label="Task">{{ detail.task_name }}</el-descriptions-item>
          <el-descriptions-item label="Cron">{{ detail.cron_expr||'-' }}</el-descriptions-item>
          <el-descriptions-item label="Start">{{ detail.start_time }}</el-descriptions-item>
          <el-descriptions-item label="Duration">{{ detail.end_time ? duration(detail.start_time,detail.end_time) : 'N/A' }}</el-descriptions-item>
        </el-descriptions>

        <LogViewer
          :mode="detail.status === 'running' ? 'live' : 'history'"
          :status="detail.status"
          :logs="(liveOutput || detail.output || '') + (detail.error_msg ? '\n' + detail.error_msg : '')"
          :duration="detail.end_time ? duration(detail.start_time, detail.end_time) : ''"
          :taskId="detail.task_id"
        />
      </template>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, h } from 'vue'
import { ElTableV2, ElTag, ElAutoResizer } from 'element-plus'
import type { Column } from 'element-plus'
import { logAPI, taskAPI } from '../api/index'
import { ElMessage } from 'element-plus'
import LogViewer from '../components/LogViewer.vue'

const logs = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const clearing = ref(false)
const exporting = ref(false)

const filters = reactive({ task_name:'', status:'', since:'' })

const drawerVisible = ref(false)
const detail = ref<any>(null)
const detailLoading = ref(false)
const liveOutput = ref('')  // 运行中任务的实时流输出
function formatTime(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

const columns: Column<any>[] = [
  /* @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 */
  {
    key: 'id', title: 'ID', width: 80, dataKey: 'id',
    cellRenderer: ({ cellData }: any) => h('span', { style: 'font-family:var(--font-mono);font-size:12px;color:var(--text-main)' }, cellData)
  },
  { key: 'task_name', title: 'Task', width: 160, flexGrow: 1, dataKey: 'task_name' },
  { key: 'group_name', title: 'Group', width: 130, flexGrow: 1, dataKey: 'group_name' },
  {
    key: 'status', title: 'Status', width: 100, dataKey: 'status',
    cellRenderer: ({ cellData: val }) => {
      if (val === 'running') {
        return h(ElTag, { class: 'tag-running-vibrant', type: 'info' }, () => val.toUpperCase())
      }
      const type = val === 'success' ? 'success' : val === 'failed' ? 'danger' : val === 'timeout' ? 'warning' : 'info'
      return h(ElTag, { type }, () => val ? val.toUpperCase() : '')
    }
  },
  { key: 'trigger_type', title: 'Trigger', width: 90, dataKey: 'trigger_type' },
  {
    key: 'start_time', title: 'Time', width: 180, flexGrow: 1, dataKey: 'start_time',
    cellRenderer: ({ cellData }: any) => h('span', { style: 'font-family:var(--font-mono);font-size:12px;color:var(--text-secondary)' }, formatTime(cellData))
  },
  {
    key: 'exit_code', title: 'Exit', width: 60, dataKey: 'exit_code',
    cellRenderer: ({ cellData }: any) => h('span', { style: 'font-family:var(--font-mono);font-size:12px;color:var(--text-main)' }, cellData !== null ? String(cellData) : '-')
  },
  {
    key: 'error_msg', title: 'Preview', width: 300, flexGrow: 4, dataKey: 'error_msg',
    cellRenderer: ({ cellData, rowData }: any) => {
      const output = rowData?.output || ''
      const errMsg = cellData || ''
      if (output) {
        const text = output.length > 80 ? output.substring(0, 80) + '...' : output
        return h('code', { style: 'font-family:var(--font-mono);font-size:12px;color:var(--text-main)' }, text)
      }
      if (errMsg) {
        const text = errMsg.length > 80 ? errMsg.substring(0, 80) + '...' : errMsg
        return h('code', { style: 'font-family:var(--font-mono);font-size:12px;color:var(--error-color)' }, text)
      }
      return h('span', { style: 'color:var(--text-secondary)' }, '-')
    }
  },
]

const rowEventHandlers = {
  onClick({ rowData }: any) {
    showDetail(rowData)
  }
}

function duration(start:string, end:string) {
  const diff = new Date(end).getTime() - new Date(start).getTime()
  if (diff<1000) return diff+'ms'
  if (diff<60000) return (diff/1000).toFixed(2)+'s'
  return (diff/60000).toFixed(1)+'min'
}

async function load() {
  loading.value = true
  try {
    const r = await logAPI.list({
      page: page.value,
      page_size: 20,
      task_name: filters.task_name || undefined,
      status: filters.status || undefined,
      since: filters.since || undefined
    })
    logs.value = r.data.data.items || []
    total.value = r.data.data.total || 0
  }
  finally { loading.value = false }
}

async function clearAllLogs() {
  clearing.value = true
  try {
    const r = await logAPI.clearAll()
    ElMessage.success(`Deleted ${r.data.data.task_logs_deleted} task logs + ${r.data.data.group_logs_deleted} group logs`)
    load()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.message || 'Failed')
  } finally { clearing.value = false }
}

async function showDetail(rowData: any) {
  drawerVisible.value = true
  detail.value = null
  detailLoading.value = true
  liveOutput.value = ''
  try {
    const r = await logAPI.getLog(rowData.id)
    detail.value = r.data.data
    // 运行中的任务：拉取实时磁盘日志（DB 中 output 此时为空）
    if (detail.value?.status === 'running' && detail.value?.task_id) {
      try {
        const sr: any = await taskAPI.streamLog(detail.value.task_id, { offset: 0 })
        const payload = sr.data?.data || {}
        liveOutput.value = typeof sr.data === 'string' ? sr.data : (payload.content || '')
      } catch {}
    }
  } finally { detailLoading.value = false }
}

async function exportLogs(format: string) {
  exporting.value = true
  try {
    const r = await logAPI.exportLogs({
      format,
      max: 100000,
      task_name: filters.task_name || undefined,
      status: filters.status || undefined,
      since: filters.since || undefined,
    })
    const blob = r.data instanceof Blob ? r.data : new Blob([JSON.stringify(r.data)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    const date = new Date().toISOString().slice(0, 10)
    a.download = `cronix-logs-${date}.${format === 'json' ? 'json' : 'csv'}`
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success('Export downloaded')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.message || 'Failed')
  } finally { exporting.value = false }
}

const downloadSingleLog = (log: any, format: string) => {
  const content = (log.output || '') + (log.error_msg ? '\n' + log.error_msg : '')
  if (!content) return
  const blob = new Blob([content], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `task_${log.task_name}_log_${log.id}.txt`
  a.click()
  URL.revokeObjectURL(url)
}

onMounted(load)
</script>
