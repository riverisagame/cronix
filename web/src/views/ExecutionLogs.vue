<!--
  ExecutionLogs.vue -- execution log page with virtual scroll, lazy detail, export.
-->
<template>
  <div>
    <h2 style="margin-top:0;display:flex;align-items:center;gap:12px">
      Execution Logs
      <el-popconfirm title="Delete ALL execution logs?" @confirm="clearAllLogs">
        <template #reference><el-button size="small" type="danger" :loading="clearing">Clear All</el-button></template>
      </el-popconfirm>
      <el-button size="small" @click="exportLogs('csv')" :loading="exporting" :disabled="exporting">Export CSV</el-button>
      <el-button size="small" @click="exportLogs('json')" :loading="exporting" :disabled="exporting">Export JSON</el-button>
    </h2>

    <el-card shadow="hover" v-loading="loading">
      <el-row :gutter="16" style="margin-bottom:16px">
        <el-col :span="6">
          <el-input v-model="filters.task_name" placeholder="Task name..." clearable @keyup.enter="load">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="4">
          <el-select v-model="filters.status" placeholder="Status" clearable @change="load" style="width:100%">
            <el-option label="Success" value="success" />
            <el-option label="Failed" value="failed" />
            <el-option label="Timeout" value="timeout" />
            <el-option label="Running" value="running" />
          </el-select>
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.since" placeholder="Time range" clearable @change="load" style="width:100%">
            <el-option label="Last 1 hour" value="1h" />
            <el-option label="Last 6 hours" value="6h" />
            <el-option label="Last 24 hours" value="24h" />
            <el-option label="Last 7 days" value="168h" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-button type="primary" @click="load"><el-icon><Search /></el-icon> Search</el-button>
          <el-button @click="load"><el-icon><Refresh /></el-icon></el-button>
        </el-col>
      </el-row>

      <el-table-v2
        :columns="columns"
        :data="logs"
        :width="1300"
        :height="600"
        :row-height="40"
        :row-event-handlers="rowEventHandlers"
        fixed
        style="cursor:pointer"
      />

      <div style="margin-top:16px;text-align:right">
        <el-pagination v-model:current-page="page" :total="total" :page-size="20" layout="total,prev,pager,next" @current-change="load" />
      </div>
    </el-card>

    <el-drawer v-model="drawerVisible" title="Execution Detail" size="650px" direction="rtl">
      <div v-if="detailLoading" style="text-align:center;padding:40px;color:#909399" v-loading="true">Loading...</div>
      <template v-else-if="detail">
        <div style="display:flex;gap:10px;margin-bottom:16px">
          <el-tag :type="detail.status==='success'?'success':'danger'">{{ detail.status?.toUpperCase() }}</el-tag>
          <el-tag type="info">{{ detail.trigger_type }}</el-tag>
          <el-tag v-if="detail.exit_code!==null">exit={{ detail.exit_code }}</el-tag>
        </div>
        <el-descriptions :column="2" border size="small" style="margin-bottom:16px">
          <el-descriptions-item label="Task">{{ detail.task_name }}</el-descriptions-item>
          <el-descriptions-item label="Cron">{{ detail.cron_expr||'-' }}</el-descriptions-item>
          <el-descriptions-item label="Start">{{ detail.start_time }}</el-descriptions-item>
          <el-descriptions-item label="Duration">{{ detail.end_time ? duration(detail.start_time,detail.end_time) : 'N/A' }}</el-descriptions-item>
        </el-descriptions>

        <div v-if="detail.output" style="margin-bottom:16px">
          <div style="font-weight:bold;margin-bottom:8px;color:#67C23A">Output</div>
          <pre style="background:#f5f7fa;color:#303133;padding:14px;border-radius:8px;font-size:13px;line-height:1.6;white-space:pre-wrap;word-break:break-all;max-height:300px;overflow:auto;margin:0">{{ outputDisplay }}</pre>
          <el-button v-if="outputTruncated" size="small" text @click="showFullOutput = !showFullOutput" style="margin-top:4px">
            {{ showFullOutput ? 'Collapse' : 'Show all (' + outputLineCount + ' lines)' }}
          </el-button>
        </div>

        <div v-if="detail.error_msg">
          <div style="font-weight:bold;margin-bottom:8px;color:#F56C6C">Error</div>
          <pre style="background:#fef0f0;color:#F56C6C;padding:14px;border-radius:8px;font-size:13px;line-height:1.6;white-space:pre-wrap;word-break:break-all;max-height:300px;overflow:auto;margin:0">{{ detail.error_msg }}</pre>
        </div>
      </template>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, h } from 'vue'
import { ElTableV2, ElTag } from 'element-plus'
import type { Column } from 'element-plus'
import { logAPI } from '../api/index'
import { Search, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

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
const showFullOutput = ref(false)

const outputLineCount = computed(() => detail.value?.output ? detail.value.output.split('\n').length : 0)
const outputTruncated = computed(() => outputLineCount.value > 500)
const outputDisplay = computed(() => {
  if (!detail.value?.output) return ''
  if (!outputTruncated.value || showFullOutput.value) return detail.value.output
  return detail.value.output.split('\n').slice(0, 200).join('\n') + '\n... (truncated)'
})

function formatTime(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

const columns: Column<any>[] = [
  { key: 'id', title: 'ID', width: 60, dataKey: 'id' },
  { key: 'task_name', title: 'Task', width: 140, dataKey: 'task_name' },
  { key: 'group_name', title: 'Group', width: 110, dataKey: 'group_name' },
  {
    key: 'status', title: 'Status', width: 100, dataKey: 'status',
    cellRenderer: ({ cellData }: any) => {
      const val = cellData ?? ''
      const type = val === 'success' ? 'success' : val === 'failed' ? 'danger' : val === 'timeout' ? 'warning' : 'info'
      return h(ElTag, { type, size: 'small' }, () => val.toUpperCase())
    }
  },
  { key: 'trigger_type', title: 'Trigger', width: 80, dataKey: 'trigger_type' },
  {
    key: 'start_time', title: 'Time', width: 160, dataKey: 'start_time',
    cellRenderer: ({ cellData }: any) => h('span', { style: 'font-size:12px' }, formatTime(cellData))
  },
  { key: 'exit_code', title: 'Exit', width: 60, dataKey: 'exit_code' },
  {
    key: 'error_msg', title: 'Preview', width: 220, dataKey: 'error_msg',
    cellRenderer: ({ cellData, rowData }: any) => {
      const output = rowData?.output || ''
      const errMsg = cellData || ''
      if (output) {
        const text = output.length > 80 ? output.substring(0, 80) + '...' : output
        return h('code', { style: 'font-size:12px;color:#303133' }, text)
      }
      if (errMsg) {
        const text = errMsg.length > 80 ? errMsg.substring(0, 80) + '...' : errMsg
        return h('code', { style: 'font-size:12px;color:#F56C6C' }, text)
      }
      return h('span', { style: 'color:#c0c4cc' }, '-')
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
  showFullOutput.value = false
  try {
    const r = await logAPI.getLog(rowData.id)
    detail.value = r.data.data
  } catch {
    // Fallback: use list row data (no output available)
    detail.value = rowData
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

onMounted(load)
</script>
