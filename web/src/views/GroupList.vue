<template>
  <div>
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
      <h2 style="margin:0">Task Groups</h2>
      <el-button type="primary" @click="router.push('/groups/new')" data-testid="btn-new-group">
        <el-icon><Plus /></el-icon> New Group
      </el-button>
    </div>

    <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
    <el-card shadow="hover" class="data-card">
      <el-table :data="groups" stripe v-loading="loading">
        <el-table-column label="ID" width="60">
          <template #default="{ row }">
            <span style="font-family:var(--font-mono);font-size:12px;color:var(--text-main)">{{ row.id }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="Name" min-width="150" />
        <el-table-column prop="mode" label="Mode" width="130">
          <template #default="{ row }">
            <el-tag :type="row.mode==='parallel'?'success':''">
              {{ row.mode === 'parallel' ? 'Parallel' : 'Sequential' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Cron" width="160">
          <template #default="{ row }">
            <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
            <el-tag v-if="row.cron_expr" type="info" style="font-family:var(--font-mono)">{{ row.cron_expr }}</el-tag>
            <span v-else style="color:var(--text-secondary);font-size:12px">—</span>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="Description" show-overflow-tooltip min-width="140" />
        <el-table-column label="Actions" width="240" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" @click="router.push('/groups/'+row.id)" circle><el-icon><Edit /></el-icon></el-button>
            <el-button type="success" @click="runGroup(row)" :loading="runningId===row.id" circle><el-icon><VideoPlay /></el-icon></el-button>
            <el-button @click="showLogs(row)" circle><el-icon><Tickets /></el-icon></el-button>
            <el-popconfirm title="Clear all execution logs for this group?" @confirm="clearGroupLogs(row.id)">
              <template #reference><el-button type="warning" circle><el-icon><DeleteFilled /></el-icon></el-button></template>
            </el-popconfirm>
            <el-popconfirm :title="'Delete group \'' + row.name + '\'? Tasks will be kept, logs cleared.'" @confirm="deleteGroup(row.id, row.name)">
              <template #reference><el-button type="danger" circle><el-icon><Delete /></el-icon></el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- Group execution logs drawer -->
    <el-drawer v-model="drawerVisible" :title="'History: ' + logGroupName" size="66%" direction="rtl">
      <div v-if="groupLogs.length===0" style="text-align:center;padding:40px;color:var(--text-secondary)">No executions yet</div>
      <el-timeline v-else>
        <el-timeline-item v-for="log in groupLogs" :key="log.id" :timestamp="log.start_time" placement="top"
          :color="log.status==='success'?'#67C23A':log.status==='failed'?'#F56C6C':log.status==='partial'?'#E6A23C':'#909399'">
          <el-card shadow="hover" class="data-card">
            <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:4px">
              <div style="display:flex;align-items:center;gap:8px">
                <el-tag :type="log.status==='success'?'success':log.status==='failed'?'danger':'warning'">{{ log.status }}</el-tag>
                <el-tag type="info">{{ log.trigger_type }}</el-tag>
                <span style="font-size:12px;color:var(--text-secondary)">OK:{{ log.success_count }} FAIL:{{ log.failed_count }}/{{ log.member_count }}</span>
              </div>
            </div>
            <LogViewer
              v-if="log.error_msg"
              mode="history"
              :status="log.status"
              :logs="log.error_msg"
            />
          </el-card>
        </el-timeline-item>
      </el-timeline>
      <div v-if="groupLogs.length > 0" style="margin-top:16px;text-align:right">
        <el-pagination v-model:current-page="historyPage" :total="historyTotal" :page-size="10" layout="total,prev,pager,next" @current-change="loadHistory" />
      </div>
      <div style="margin-top:12px;text-align:center">
        <el-popconfirm title="Clear all execution logs for this group?" @confirm="clearGroupLogs(logGroupId)">
          <template #reference><el-button type="danger" :loading="clearingLogs">Clear Group Logs</el-button></template>
        </el-popconfirm>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { groupAPI, logAPI } from '../api/index'
import { Plus, Edit, VideoPlay, Delete, Tickets, DeleteFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import LogViewer from '../components/LogViewer.vue'

const router = useRouter()
const groups = ref<any[]>([])
const loading = ref(false)
const runningId = ref<number|null>(null)
const drawerVisible = ref(false)
const logGroupName = ref('')
const logGroupId = ref(0)
const groupLogs = ref<any[]>([])
const clearingLogs = ref(false)

const historyPage = ref(1)
const historyTotal = ref(0)

async function load() {
  loading.value = true
  try {
    const r = await groupAPI.list()
    groups.value = r.data.data || []
  } finally { loading.value = false }
}

async function runGroup(row: any) {
  runningId.value = row.id
  try { await groupAPI.run(row.id); ElMessage.success('Group triggered') }
  catch(e: any) { ElMessage.error(e.response?.data?.message || 'Failed') }
  finally { runningId.value = null; load() }
}

async function deleteGroup(id: number, name: string) {
  try {
    const r = await groupAPI.delete(id)
    const d = r.data.data
    ElMessage.success(`Deleted '${name}': ${d.tasks_affected} task(s) disassociated, ${d.logs_deleted} log(s) cleared`)
  } catch (e: any) {
    ElMessage.error(e.response?.data?.message || 'Failed')
  }
  load()
}

async function showLogs(row: any) {
  logGroupName.value = row.name; logGroupId.value = row.id; drawerVisible.value = true
  historyPage.value = 1
  await loadHistory()
}

async function loadHistory() {
  if (!logGroupId.value) return
  try {
    const r = await groupAPI.getLogs(logGroupId.value, { page: historyPage.value, page_size: 10 })
    groupLogs.value = r.data.data.items || []
    historyTotal.value = r.data.data.total || 0
  } catch { groupLogs.value = []; historyTotal.value = 0 }
}

async function clearGroupLogs(id: number) {
  clearingLogs.value = true
  try {
    await logAPI.clearGroup(id)
    ElMessage.success('Cleared'); groupLogs.value = []; load()
  } catch(e: any) { ElMessage.error('Failed') }
  finally { clearingLogs.value = false }
}

onMounted(load)
</script>
