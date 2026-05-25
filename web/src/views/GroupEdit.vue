<template>
  <div>
    <div style="display:flex;align-items:center;margin-bottom:16px">
      <el-button text @click="router.push('/groups')"><el-icon><ArrowLeft /></el-icon> Back</el-button>
      <h2 style="margin:0 0 0 10px">{{ isNew ? 'Create Group' : 'Edit Group' }}</h2>
    </div>

    <el-card shadow="hover" style="margin-bottom:20px">
      <el-form :model="form" label-width="120px" style="max-width:700px">
        <el-form-item label="Name" required>
          <el-input v-model="form.name" placeholder="e.g. daily-backup-pipeline" />
        </el-form-item>
        <el-form-item label="Description">
          <el-input v-model="form.description" type="textarea" rows="2" placeholder="Optional description" />
        </el-form-item>
        <el-form-item label="Mode" required>
          <el-radio-group v-model="form.mode">
            <el-radio value="parallel">Parallel — all tasks run at once</el-radio>
            <el-radio value="sequential">Sequential — run one by one in order</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="Cron (optional)">
          <el-input v-model="form.cron_expr" placeholder="0 30 8 * * * — leave empty for manual only" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="saving" @click="save">{{ isNew ? 'Create' : 'Save' }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- Member management (only for existing groups) -->
    <el-card v-if="!isNew" shadow="hover">
      <template #header><span style="font-weight:bold">Members</span></template>

      <el-row :gutter="20">
        <!-- Available tasks -->
        <el-col :span="11">
          <h4 style="margin-top:0">Available Tasks</h4>
          <el-input v-model="taskSearch" placeholder="Search..." size="small" style="margin-bottom:8px" />
          <div style="border:1px solid #333;border-radius:4px;min-height:200px;max-height:400px;overflow:auto;padding:8px">
            <div v-for="t in availableTasks" :key="t.id"
              style="display:flex;justify-content:space-between;align-items:center;padding:6px 8px;margin-bottom:4px;background:#1d1e1f;border-radius:4px;cursor:pointer"
              @click="addMember(t)">
              <span style="font-size:13px">{{ t.name }} <el-tag size="small" type="info" style="margin-left:4px">{{ t.task_type }}</el-tag></span>
              <el-icon><Plus /></el-icon>
            </div>
            <div v-if="availableTasks.length===0" style="text-align:center;color:#909399;padding:20px">No tasks available</div>
          </div>
        </el-col>

        <!-- Group members -->
        <el-col :span="11" :offset="2">
          <h4 style="margin-top:0">Group Members ({{ members.length }})</h4>
          <div v-if="form.mode==='sequential'" style="font-size:12px;color:#909399;margin-bottom:8px">
            Drag to reorder — tasks execute from top to bottom
          </div>
          <div style="border:1px solid #409EFF;border-radius:4px;min-height:200px;max-height:400px;overflow:auto;padding:8px">
            <div v-for="(t, idx) in members" :key="t.id"
              style="display:flex;justify-content:space-between;align-items:center;padding:6px 8px;margin-bottom:4px;background:#1a2a3a;border-radius:4px">
              <span>
                <el-tag v-if="form.mode==='sequential'" size="small" type="warning" style="margin-right:6px">{{ idx + 1 }}</el-tag>
                <span style="font-size:13px">{{ t.name }}</span>
              </span>
              <el-button size="small" type="danger" circle @click="removeMember(t, idx)">
                <el-icon><Close /></el-icon>
              </el-button>
            </div>
            <div v-if="members.length===0" style="text-align:center;color:#909399;padding:20px">
              Click tasks on the left to add them
            </div>
          </div>
        </el-col>
      </el-row>

      <div style="margin-top:12px">
        <el-button type="primary" size="small" :loading="savingMembers" @click="saveMembers">Save Members</el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { groupAPI, taskAPI } from '../api/index'
import { ArrowLeft, Plus, Close } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

const route = useRoute()
const router = useRouter()
const isNew = computed(() => route.params.id === 'new')
const saving = ref(false)
const savingMembers = ref(false)
const taskSearch = ref('')

const form = ref<any>({ name: '', description: '', mode: 'parallel', cron_expr: '' })
const members = ref<any[]>([])
const allTasks = ref<any[]>([])

const availableTasks = computed(() => {
  const memberIds = new Set(members.value.map((m: any) => m.id))
  let tasks = allTasks.value.filter((t: any) => !memberIds.has(t.id))
  if (taskSearch.value) {
    const q = taskSearch.value.toLowerCase()
    tasks = tasks.filter((t: any) => t.name.toLowerCase().includes(q))
  }
  return tasks
})

function addMember(t: any) {
  members.value.push(t)
}

function removeMember(_t: any, idx: number) {
  members.value.splice(idx, 1)
}

async function loadAllTasks() {
  const r = await taskAPI.list({ page: 1, page_size: 200 })
  allTasks.value = r.data.data.items || []
}

onMounted(async () => {
  await loadAllTasks()
  if (!isNew.value) {
    const r = await groupAPI.get(Number(route.params.id))
    const d = r.data.data
    form.value = { ...form.value, ...d.group }
    members.value = d.members || []
  }
})

async function save() {
  saving.value = true
  try {
    if (isNew.value) {
      const r = await groupAPI.create(form.value)
      ElMessage.success('Created')
      router.push('/groups/' + r.data.data.id)
    } else {
      await groupAPI.update(Number(route.params.id), form.value)
      ElMessage.success('Saved')
    }
  } catch (e: any) {
    ElMessage.error(e.response?.data?.message || 'Failed')
  } finally { saving.value = false }
}

async function saveMembers() {
  savingMembers.value = true
  try {
    const ids = members.value.map((m: any) => m.id)
    await groupAPI.setMembers(Number(route.params.id), ids)
    ElMessage.success('Members saved')
    // Reload to get the updated list
    const r = await groupAPI.get(Number(route.params.id))
    members.value = r.data.data.members || []
    await loadAllTasks()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.message || 'Failed')
  } finally { savingMembers.value = false }
}
</script>
