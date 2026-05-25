<template>
  <div>
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
      <h2 style="margin:0">Task Groups</h2>
      <el-button type="primary" @click="router.push('/groups/new')">
        <el-icon><Plus /></el-icon> New Group
      </el-button>
    </div>

    <el-card shadow="hover">
      <el-table :data="groups" stripe v-loading="loading">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="Name" min-width="150" />
        <el-table-column prop="mode" label="Mode" width="130">
          <template #default="{ row }">
            <el-tag :type="row.mode==='parallel'?'success':''" size="small">
              {{ row.mode === 'parallel' ? 'Parallel' : 'Sequential' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Cron" width="160">
          <template #default="{ row }">
            <el-tag v-if="row.cron_expr" size="small" type="info">{{ row.cron_expr }}</el-tag>
            <span v-else style="color:#909399;font-size:12px">—</span>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="Description" show-overflow-tooltip min-width="140" />
        <el-table-column label="Actions" width="200" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" @click="router.push('/groups/'+row.id)" circle><el-icon><Edit /></el-icon></el-button>
            <el-button size="small" type="success" @click="runGroup(row)" :loading="runningId===row.id" circle><el-icon><VideoPlay /></el-icon></el-button>
            <el-popconfirm title="Delete this group?" @confirm="deleteGroup(row.id)">
              <template #reference><el-button size="small" type="danger" circle><el-icon><Delete /></el-icon></el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { groupAPI } from '../api/index'
import { Plus, Edit, VideoPlay, Delete } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

const router = useRouter()
const groups = ref<any[]>([])
const loading = ref(false)
const runningId = ref<number|null>(null)

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

async function deleteGroup(id: number) {
  await groupAPI.delete(id)
  ElMessage.success('Deleted')
  load()
}

onMounted(load)
</script>
