<template>
  <div>
    <div style="display:flex;align-items:center;margin-bottom:16px">
      <el-button text @click="router.push('/groups')"><el-icon><ArrowLeft /></el-icon> Back</el-button>
      <h2 style="margin:0 0 0 10px">{{ isNew ? 'Create Group' : 'Edit Group' }}</h2>
    </div>

    <el-card shadow="hover" style="margin-bottom:20px">
      <el-form :model="form" label-width="120px" style="max-width:1100px">
        <el-form-item label="Name" required>
          <el-input v-model="form.name" placeholder="e.g. daily-backup-pipeline" data-testid="group-form-name" />
        </el-form-item>
        <el-form-item label="Description">
          <el-input v-model="form.description" type="textarea" rows="2" placeholder="Optional description" />
        </el-form-item>
        <el-form-item label="Mode" required>
          <el-radio-group v-model="form.mode" data-testid="group-form-mode">
            <el-radio value="parallel">Parallel — all tasks run at once</el-radio>
            <el-radio value="sequential">Sequential — run one by one in order</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="Cron (optional)">
          <el-input v-model="form.cron_expr" placeholder="0 30 8 * * * — leave empty for manual only" />
          <div style="display:flex;flex-direction:column;gap:0">
          <div style="margin-top:4px;display:flex;gap:4px;flex-wrap:wrap">
            <span v-for="m in cronMacros" :key="m.label"
              style="cursor:pointer;font-size:12px;padding:2px 8px;border:1px solid #555;border-radius:4px;color:#c0c4cc"
              @click="form.cron_expr = m.value" :title="m.label + ': ' + m.value">
              {{ m.label }}
            </span>
          </div>
          <div v-if="cronFields.length > 0" style="margin-top:6px;display:flex;gap:4px">
            <span v-for="(f, i) in cronFields" :key="i"
              :style="{background: cronFieldColors[i],color:'#fff',fontSize:'13px',padding:'3px 8px',borderRadius:'4px'}"
              :title="cronFieldLabels[i]">{{ f }}</span>
          </div>
          <div :style="{fontSize:'13px',color: cronValid ? '#67C23A' : '#F56C6C',marginTop:'10px'}">
            {{ cronHint }}
          </div>
          <div v-if="cronNextRuns.length > 0" style="margin-top:10px;font-size:12px;color:#909399">
            Next:
            <div style="display:flex;flex-wrap:wrap;gap:4px;margin-top:3px">
              <span v-for="(t, i) in cronNextRuns" :key="i"
                style="background:#1d1e1f;padding:2px 8px;border-radius:3px;white-space:nowrap">{{ t }}</span>
            </div>
          </div>
        
          </div></el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="saving" @click="save" data-testid="btn-save-group">{{ isNew ? 'Create' : 'Save' }}</el-button>
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
          <div style="border:1px solid #409EFF;border-radius:4px;min-height:200px;max-height:400px;overflow:auto;padding:8px"
            @dragover.prevent>
            <TransitionGroup name="member-flip">
            <div v-for="(t, idx) in members" :key="t.id"
              :draggable="form.mode==='sequential'"
              @dragstart="onDragStart($event, idx)"
              @dragenter.prevent="onDragEnter($event, idx)"
              @dragleave="onDragLeave($event, idx)"
              @dragover.prevent
              @drop="onDrop($event, idx)"
              @dragend="onDragEnd"
              :style="memberItemStyle(idx)">
              <span style="display:flex;align-items:center;gap:8px">
                <el-tag v-if="form.mode==='sequential'" size="small" :type="dragIdx === idx ? 'danger' : 'warning'" style="min-width:24px;text-align:center">{{ idx + 1 }}</el-tag>
                <span style="font-size:13px">{{ t.name }}</span>
              </span>
              <span style="display:flex;gap:4px">
                <template v-if="form.mode==='sequential'">
                  <el-button size="small" :disabled="idx===0" @click="moveMember(idx, -1)" style="padding:2px 8px;font-size:14px" title="Move up">↑</el-button>
                  <el-button size="small" :disabled="idx===members.length-1" @click="moveMember(idx, 1)" style="padding:2px 8px;font-size:14px" title="Move down">↓</el-button>
                </template>
                <el-button size="small" type="danger" circle @click="removeMember(t, idx)">
                  <el-icon><Close /></el-icon>
                </el-button>
              </span>
            </div>
            </TransitionGroup>
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

// --- cron hint: human-readable explanation + validation ---
function describeField(val: string, unit: string): string {
  if (!val || val === '*') return ''
  if (val.startsWith('*/')) return `每${val.slice(2)}${unit}`
  if (val.includes(',')) return val.split(',').map(v => describeField(v, '')).join('、')
  if (val.includes('-')) { const [a, b] = val.split('-'); return `${a}-${b}${unit}` }
  return `${val}${unit}`
}

const cronHint = computed(() => {
  const expr = form.value.cron_expr?.trim()
  if (!expr) return '未设置 cron — 仅支持手动触发或由上游依赖触发'
  const parts = expr.split(/\s+/).filter(Boolean)
  if (parts.length < 5 || parts.length > 6) return '格式错误：需5或6个字段（秒 分 时 日 月 周）'
  const OK = /^[\d\*\/\-\,\s]+$/.test(expr)
  if (!OK) return '格式错误：只能包含数字、* / - , 和空格'
  const [sec, min, hour, day, month, wday] = parts.length === 6 ? parts : ['0', ...parts]
  const segs: string[] = []
  const hasSec = sec !== '*' && sec !== '0'

  if (hasSec && min === '*' && hour === '*' && day === '*' && month === '*' && wday === '*') {
    segs.push(describeField(sec, '秒').replace(/^(\d+)$/, '每$1秒'))
    return segs.join(' ')
  }
  if (hasSec) segs.push('每分的' + describeField(sec, '秒').replace(/^(\d{1,2})$/, '第$1秒'))
  if (hasSec) segs.push(describeField(sec, '秒'))

  if (min === '*' && hour === '*') segs.push('每分钟')
  else if (min.startsWith('*/') && hour === '*') { segs.push(`每${min.slice(2)}分钟`); return segs.join(' ') }
	  else if (hour === '*' && min !== '*') segs.push(`每小时第${describeField(min, '分')}`)
  else if (min === '0' && hour !== '*') segs.push(`${hour.padStart(2,'0')}:00`)
  else segs.push(`${hour.padStart(2,'0')}:${min.padStart(2,'0')}`)
  if (day !== '*' && wday === '*') segs.push(describeField(day, '号'))
  else if (day === '*' && wday !== '*') segs.push('每' + describeField(wday, '').replace(/^(\d)$/, '周$1').replace(/^0$/, '日'))
  else if (day === '*' && wday === '*') segs.push('每天')
  if (month !== '*') segs.push(describeField(month, '月'))
  return segs.join(' ')
})

const cronValid = computed(() => {
  const expr = form.value.cron_expr?.trim()
  if (!expr) return true
  if (CRON_MACROS[expr]) return true
  const parts = expr.split(/\s+/).filter(Boolean)
  if (parts.length < 5 || parts.length > 6) return false
  return /^[\d\*\/\-\,\s]+$/.test(expr)
})

// --- cron macros, fields, next-runs ---
const CRON_MACROS: Record<string,string> = {
  '@yearly':   '0 0 0 1 1 *',
  '@annually': '0 0 0 1 1 *',
  '@monthly':  '0 0 0 1 * *',
  '@weekly':   '0 0 0 * * 0',
  '@daily':    '0 0 0 * * *',
  '@midnight': '0 0 0 * * *',
  '@hourly':   '0 0 * * * *',
}
const cronMacros = [
  { label:'@every 1s',  value:'*/1 * * * * *' },
  { label:'@every 5s',  value:'*/5 * * * * *' },
  { label:'@every 10s', value:'*/10 * * * * *' },
  { label:'@every 30s', value:'*/30 * * * * *' },
  { label:'@every 1m',  value:'* * * * *' },
  { label:'@every 5m',  value:'0 */5 * * * *' },
  { label:'@every 10m', value:'0 */10 * * * *' },
  { label:'@every 15m', value:'0 */15 * * * *' },
  { label:'@every 30m', value:'0 */30 * * * *' },
  { label:'@hourly',    value:'0 0 * * * *' },
  { label:'@every 2h',  value:'0 0 */2 * * *' },
  { label:'@every 6h',  value:'0 0 */6 * * *' },
  { label:'@every 12h', value:'0 0 */12 * * *' },
  { label:'@daily',     value:'0 0 0 * * *' },
  { label:'@weekly',    value:'0 0 0 * * 0' },
  { label:'@monthly',   value:'0 0 0 1 * *' },
  { label:'@yearly',    value:'0 0 0 1 1 *' },
]
const cronFieldLabels = ['秒','分','时','日','月','周']
const cronFieldColors = ['#E6A23C','#67C23A','#409EFF','#F56C6C','#909399','#E6A23C']
const cronFields = computed(() => {
  let e = form.value.cron_expr?.trim() || ''
  for (const [k, v] of Object.entries(CRON_MACROS)) { if (e === k) e = v }
  const parts = e.split(/\s+/).filter(Boolean)
  return parts.length >= 5 ? (parts.length === 6 ? parts : ['0', ...parts]) : []
})

function parseCronField(f: string, min: number, max: number): number[] {
  f = f.trim()
  if (f === '*') { const r: number[] = []; for (let i = min; i <= max; i++) r.push(i); return r }
  if (f.startsWith('*/')) { const step = parseInt(f.slice(2))||1; const r: number[] = []; for (let i = min; i <= max; i += step) r.push(i); return r }
  if (f.includes(',')) { const r: number[] = []; f.split(',').forEach(p => { const v = parseInt(p); if (!isNaN(v) && v >= min && v <= max) r.push(v) }); return r.sort((a,b)=>a-b) }
  if (f.includes('-')) { const [a,b] = f.split('-').map(Number); const r: number[] = []; if (!isNaN(a) && !isNaN(b)) for (let i = a; i <= b && i <= max; i++) if (i >= min) r.push(i); return r }
  const v = parseInt(f); return !isNaN(v) && v >= min && v <= max ? [v] : []
}

function cronNext(expr: string, count: number = 5): string[] {
  let e = expr.trim()
  for (const [k, v] of Object.entries(CRON_MACROS)) { if (e === k) e = v }
  const parts = e.split(/\s+/).filter(Boolean)
  if (parts.length < 5) return []
  const [secS, minS, hourS, dayS, monS, wdayS] = parts.length === 6 ? parts : ['0', ...parts]
  const secs = parseCronField(secS, 0, 59); const mins = parseCronField(minS, 0, 59)
  const hours = parseCronField(hourS, 0, 23); const days = parseCronField(dayS, 1, 31)
  const mons = parseCronField(monS, 1, 12); const wdays = parseCronField(wdayS, 0, 6)
  if ([secs,mins,hours,days,mons,wdays].some(a => a.length === 0)) return []
  const subMinute = secS !== '*' && secS !== '0'
  const results: Date[] = []; const start = new Date()
  if (subMinute) { start.setSeconds(start.getSeconds() + 1, 0) }
  else { start.setSeconds(0, 0); start.setMinutes(start.getMinutes() + 1) }
  start.setMilliseconds(0)
  const stepMs = subMinute ? 1000 : 60000
  const maxIter = subMinute ? 86400 : 525600
  for (let i = 0; i < maxIter && results.length < count; i++) {
    const d = new Date(start.getTime() + i * stepMs)
    if (!secs.includes(d.getSeconds())) continue
    if (!mins.includes(d.getMinutes())) continue
	      if (!hours.includes(d.getHours())) continue
    if (!mons.includes(d.getMonth() + 1)) continue
    const dayMatch = days.includes(d.getDate()), wdayMatch = wdays.includes(d.getDay())
    if (dayS === '*' && wdayS !== '*') { if (!wdayMatch) continue }
    else if (dayS !== '*' && wdayS === '*') { if (!dayMatch) continue }
    else if (dayS !== '*' && wdayS !== '*') { if (!dayMatch && !wdayMatch) continue }
    results.push(new Date(d))
  }
  return results.map(d => { const pad = (n: number) => String(n).padStart(2, '0'); return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}` })
}

const cronNextRuns = computed(() => { const e = form.value.cron_expr?.trim(); return e ? cronNext(e, 5) : [] })

const members = ref<any[]>([])
const allTasks = ref<any[]>([])

const availableTasks = computed(() => {
  const memberIds = new Set(members.value.map((m: any) => m.id))
  const currentGroupId = isNew.value ? null : Number(route.params.id)
  let tasks = allTasks.value.filter((t: any) => {
    if (memberIds.has(t.id)) return false
    // 排除已绑定其他组的任务（group_id 非空且不是当前组）
    if (t.group_id != null && t.group_id !== currentGroupId) return false
    return true
  })
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

const dragIdx = ref(-1)
	const dragOverIdx = ref(-1)

	function memberItemStyle(idx: number) {
	  const isDragging = dragIdx.value === idx
	  const isOver = dragOverIdx.value === idx && dragIdx.value >= 0 && dragIdx.value !== idx
	  return {
	    display:'flex', justifyContent:'space-between', alignItems:'center',
	    padding:'8px 10px', marginBottom:'4px', borderRadius:'6px',
	    cursor: form.value.mode==='sequential' ? (isDragging ? 'grabbing' : 'grab') : 'default',
	    opacity: isDragging ? 0.5 : 1,
	    background: isDragging ? '#409EFF' : isOver ? '#1a3a5a' : '#1a2a3a',
	    border: isDragging ? '2px dashed #67C23A' : isOver ? '2px solid #409EFF' : '2px solid transparent',
	    transform: isDragging ? 'scale(1.03)' : 'scale(1)',
	    boxShadow: isDragging ? '0 4px 12px rgba(64,158,255,0.4)' : 'none',
	    transition:'transform 0.15s, box-shadow 0.15s, opacity 0.15s, background 0.15s, border 0.15s',
	  }
	}

	function onDragStart(e: DragEvent, idx: number) {
	  dragIdx.value = idx
	  if (e.dataTransfer) {
	    e.dataTransfer.effectAllowed = 'move'
	    e.dataTransfer.setData('text/plain', String(idx))
	  }
	}

	function onDragEnter(_e: DragEvent, idx: number) {
	  if (dragIdx.value < 0 || dragIdx.value === idx) return
	  dragOverIdx.value = idx
	}

	function onDragLeave(_e: DragEvent, idx: number) {
	  if (dragOverIdx.value === idx) dragOverIdx.value = -1
	}

	function onDragEnd() {
	  dragIdx.value = -1
	  dragOverIdx.value = -1
	}

	function onDrop(_e: DragEvent, idx: number) {
	  dragOverIdx.value = -1
	  if (dragIdx.value < 0 || dragIdx.value === idx) return
	  const arr = members.value
	  const item = arr.splice(dragIdx.value, 1)[0]
	  arr.splice(idx, 0, item)
	  dragIdx.value = -1
	}
function moveMember(idx: number, delta: number) {
  const arr = members.value; const newIdx = idx + delta
  if (newIdx < 0 || newIdx >= arr.length) return
  const item = arr.splice(idx, 1)[0]; arr.splice(newIdx, 0, item)
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

<style scoped>
.member-flip-move {
  transition: transform 0.3s ease;
}
.member-flip-leave-active {
  display: none;
}
</style>