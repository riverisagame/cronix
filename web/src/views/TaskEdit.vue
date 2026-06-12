<!--
  TaskEdit.vue -- 任务编辑/创建页面组件。
  这是一个"表单页面"，用于创建新任务或编辑已有任务。
  根据路由参数 :id 的值判断模式：
  - :id 是 "new" -> 新建模式（表单为空，提交时调用创建 API）
  - :id 是数字（如 "5"） -> 编辑模式（从后端加载已有数据，提交时调用更新 API）

  任务有四种类型，不同类型的表单字段不同：
  1. Shell：执行 Shell 命令（有 command 和 work_dir 字段）
  2. HTTP：发送 HTTP 请求（有 http_method、http_url、http_auth_type 字段）
  3. Cleanup：清理文件任务（通过 JSON 配置）
  4. Healthcheck：健康检查（和 HTTP 类似，但没有 HTTP 方法选项）
-->

<template>
  <div>
    <!-- 页面头部：返回按钮 + 标题 -->
    <div style="display:flex;align-items:center;margin-bottom:16px">
      <!--
        返回按钮：text 属性让按钮没有背景色（看起来像纯文字链接）
        @click="router.push('/tasks')" 点击后回到任务列表页
      -->
      <el-button text @click="router.push('/tasks')"><el-icon><ArrowLeft /></el-icon> Back</el-button>
      <!--
        标题根据 isNew 的值动态变化：
        isNew 为 true 时显示 "Create Task"，否则显示 "Edit Task"
      -->
      <h2 style="margin:0 0 0 10px">{{ isNew ? 'Create Task' : 'Edit Task' }}</h2>
    </div>

    <el-card shadow="hover" class="data-card">
      <!--
        el-form：表单组件
        :model="form" 把表单数据绑定到 form 响应式对象上
        label-width="160px" 标签宽度
      -->
      <el-form :model="form" label-width="160px" size="large" style="max-width:100%">
        <!--
          el-divider：分割线组件
          content-position="left" 文字靠左显示
        -->
        <el-divider content-position="left">Basic</el-divider>

        <!-- 任务名称（必填项，用 required 属性在标签前显示红色星号） -->
        <el-form-item label="Task Name" required>
          <!-- v-model="form.name" 双向绑定到 form 对象的 name 属性 -->
          <el-input v-model="form.name" placeholder="e.g. backup-database" data-testid="task-form-name" />
        </el-form-item>

        <el-form-item label="Run Mode">
          <el-radio-group v-model="form.run_mode">
            <el-radio-button value="cron">Cron Schedule</el-radio-button>
            <el-radio-button value="daemon">Daemon Service</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <!-- 仅 cron 模式显示 Cron 表达式（daemon 的调度字段在 Daemon Configuration 区） -->
        <el-form-item label="Cron Expression" v-if="form.run_mode === 'cron'">
          <el-input v-model="form.cron_expr" placeholder="0 30 8 * * *（留空由任务组触发或手动执行）" data-testid="task-form-cron"
            @input="onCronInput" />
          <!-- 快捷宏 -->
          <div style="margin-top:10px;display:flex;gap:4px;flex-wrap:wrap">
            <span v-for="m in cronMacros" :key="m.label"
              class="macro-tag"
              style="cursor:pointer;font-size:12px;padding:2px 8px;border:1px solid var(--border-color);border-radius:4px;color:var(--text-secondary)"
              @click="applyMacro(m.value)" :title="m.label + ': ' + m.value">
              {{ m.label }}
            </span>
          </div>
          <!-- 字段高亮 -->
          <div v-if="cronFields.length > 0" style="margin-top:10px;display:flex;gap:4px">
            <span v-for="(f, i) in cronFields" :key="i"
              :style="{background: cronFieldColors[i],color:'#fff',fontSize:'13px',padding:'3px 8px',borderRadius:'4px'}"
              :title="cronFieldLabels[i]">{{ f }}</span>
          </div>
          <!-- 可读说明 -->
          <div :style="{fontSize:'13px',color: cronValid ? '#67C23A' : '#F56C6C',marginTop:'10px'}">
            {{ cronHint }}
          </div>
          <!-- 下次执行 -->
          <div v-if="cronNextRuns.length > 0" style="margin-top:10px;font-size:12px;color:var(--text-secondary)">
            Next:
            <div style="display:flex;flex-wrap:wrap;gap:4px;margin-top:3px">
              <span v-for="(t, i) in cronNextRuns" :key="i"
                style="background:var(--bg-color-page);padding:2px 8px;border-radius:3px;white-space:nowrap;color:var(--text-main)">{{ t }}</span>
            </div>
          </div>
        </el-form-item>

        <!-- ======== Daemon Configuration ======== -->
        <template v-if="form.run_mode === 'daemon'">
          <el-divider content-position="left">Daemon Configuration</el-divider>

          <el-form-item label="Restart Policy">
            <el-select v-model="form.restart_policy" style="width:200px">
              <el-option label="Always" value="always" />
              <el-option label="On Failure" value="on-failure" />
              <el-option label="Never" value="never" />
            </el-select>
          </el-form-item>

          <el-row :gutter="20">
            <el-col :span="12">
              <el-form-item label="Max Restart Attempts">
                <el-input-number v-model="form.max_restart_attempts" :min="0" :max="100" />
                <div style="font-size:12px;color:var(--text-secondary);margin-top:2px">0 = unlimited</div>
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="Restart Delay(s)">
                <el-input-number v-model="form.restart_delay_sec" :min="0" :max="86400" />
                <div style="font-size:12px;color:var(--text-secondary);margin-top:2px">0 = auto backoff (1s–60s)</div>
              </el-form-item>
            </el-col>
          </el-row>

          <el-form-item label="Scheduled Restart(s)">
            <el-input-number v-model="form.scheduled_restart_sec" :min="0" :max="86400" />
            <span style="margin-left:10px;font-size:12px;color:var(--text-secondary)">0 = never, >0 = force restart every N seconds</span>
          </el-form-item>
        </template>
        <el-form-item label="Task Type">
          <el-radio-group v-model="form.task_type" data-testid="task-form-type">
            <el-radio-button value="shell">Shell</el-radio-button>
            <el-radio-button value="http">HTTP</el-radio-button>
            <el-radio-button value="cleanup">Cleanup</el-radio-button>
            <el-radio-button value="healthcheck">Health Check</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <!-- 所属任务组（可选） -->
        <el-form-item label="Group">
          <el-select v-model="form.group_id" placeholder="None" clearable style="width:250px">
            <el-option v-for="g in groupList" :key="g.id" :label="g.name + ' (' + g.mode + ')'" :value="g.id" />
          </el-select>
        </el-form-item>

        <!-- 描述（非必填），type="textarea" 多行文本输入框 -->
        <el-form-item label="Description">
          <el-input v-model="form.description" type="textarea" rows="2" placeholder="Optional description" />
        </el-form-item>

        <!--
          ======== Shell 类型的专属字段 ========
          v-if="form.task_type==='shell'" 条件渲染：
          只有当任务类型选择为 "shell" 时，这一块才显示出来
        -->
        <template v-if="form.task_type==='shell'">
          <el-divider content-position="left">Shell Command</el-divider>

          <!-- 要执行的 Shell 命令（必填），多行文本框 -->
          <el-form-item label="Command" required>
            <el-input v-model="form.command" type="textarea" rows="25" placeholder="echo hello" data-testid="task-form-command" style="font-family: monospace; font-size: 14px; width: 100%;" />
          </el-form-item>

          <!-- 工作目录（命令在哪个文件夹下执行），非必填 -->
          <el-form-item label="Work Directory">
            <el-input v-model="form.work_dir" placeholder="/opt/scripts" />
          </el-form-item>

          <!-- 以哪个用户执行（需配置 sudoers），非必填 -->
          <el-form-item label="Run As User">
            <el-input v-model="form.run_as" placeholder="Default: root" />
          </el-form-item>
        </template>

        <!--
          ======== HTTP 和 Healthcheck 类型的专属字段 ========
          v-if 条件：task_type 是 'http' 或者 'healthcheck' 时显示
        -->
        <template v-if="form.task_type==='http'||form.task_type==='healthcheck'">
          <!-- 分割线标题也动态变化：HTTP 显示 "HTTP Request"，Healthcheck 显示 "Health Check" -->
          <el-divider content-position="left">{{ form.task_type==='http'?'HTTP Request':'Health Check' }}</el-divider>

          <!-- HTTP 方法选择（只有 http 类型需要，healthcheck 不需要） -->
          <el-form-item v-if="form.task_type==='http'" label="Method">
            <!-- 下拉选择：GET（获取数据）或 POST（提交数据） -->
            <el-select v-model="form.http_method" style="width:120px">
              <el-option label="GET" value="GET" /><el-option label="POST" value="POST" />
            </el-select>
          </el-form-item>

          <!-- URL 地址（必填） -->
          <el-form-item label="URL" required>
            <el-input v-model="form.http_url" placeholder="https://..." />
          </el-form-item>

          <!-- 认证方式（只有 http 类型需要） -->
          <el-form-item v-if="form.task_type==='http'" label="Auth">
            <el-select v-model="form.http_auth_type" style="width:150px">
              <el-option label="None" value="none" />    <!-- 不需要认证 -->
              <el-option label="Basic" value="basic" />   <!-- 用户名 + 密码认证 -->
              <el-option label="Bearer" value="bearer" /> <!-- Token 认证 -->
            </el-select>
          </el-form-item>
        </template>

        <!--
          ======== Cleanup 类型的专属字段 ========
          清理任务的配置是一个 JSON 格式的字符串
        -->
        <template v-if="form.task_type==='cleanup'">
          <el-divider content-position="left">Cleanup Config (JSON)</el-divider>
          <el-form-item label="Config">
            <!--
              注意：cleanup 的配置存在 form.command 字段里（复用字段）
              placeholder 显示一个 JSON 示例：清理 /tmp 目录下超过 72 小时的 .log 文件
            -->
            <el-input v-model="form.command" type="textarea" rows="10" placeholder='{"path":"/tmp","pattern":"*.log","older_than_hours":72}' style="font-family: monospace; font-size: 14px;" />
          </el-form-item>
        </template>

        <!--
          ======== 报警与通知配置 ========
        -->
        <el-divider content-position="left">Alert & Notification</el-divider>
        <el-form-item label="Webhook URL">
          <el-input v-model="notifyForm.webhook_url" placeholder="https://..." clearable />
          <div style="font-size:12px;color:var(--text-secondary);margin-top:4px;line-height:1.4;word-break:break-all;">
            支持企业微信、钉钉、飞书等机器人的 Webhook 地址。<strong>留空则不发送通知</strong>。<br/>
            默认推送 JSON 数据格式：<code style="background:var(--bg-color-page);padding:2px 4px;border-radius:3px;">{"task": "任务名称", "status": "success/failed", "timestamp": "通知时间"}</code>
          </div>
        </el-form-item>
        <el-form-item label="Notify On">
          <el-checkbox v-model="notifyForm.on_failure" label="Failure" />
          <el-checkbox v-model="notifyForm.on_success" label="Success" />
        </el-form-item>

        <!--
          ======== 高级设置（所有类型通用） ========
        -->
        <el-divider content-position="left">Advanced</el-divider>
        <!-- 用 el-row 把两个设置项并排显示 -->
        <el-row :gutter="20">
          <el-col :span="12" v-if="form.run_mode !== 'daemon'">
            <el-form-item label="Timeout(s)" title="Daemon 任务不受此限制">
              <el-input-number v-model="form.timeout_sec" :min="1" :max="3600" style="width:100%" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="Retry Count">
              <el-input-number v-model="form.retry_count" :min="0" :max="10" style="width:100%" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="20">
          <el-col :span="12">
            <el-form-item label="Retry Interval(s)">
              <el-input-number v-model="form.retry_interval_sec" :min="0" :max="3600" style="width:100%" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="Max Concurrent">
              <el-input-number v-model="form.max_concurrent" :min="1" :max="100" style="width:100%" />
            </el-form-item>
          </el-col>
        </el-row>

        <!-- 启用开关 -->
        <el-form-item label="Enabled">
          <!-- el-switch 开关组件：v-model 绑定到 form.enabled -->
          <el-switch v-model="form.enabled" />
        </el-form-item>

        <!-- 任务依赖（此任务必须等所选任务成功后才能执行） -->
        <el-form-item label="Depends On">
          <el-select v-model="form.dep_ids" multiple placeholder="Select tasks this depends on..." style="width:100%">
            <el-option v-for="t in availableDepTasks" :key="t.id" :label="t.name + ' (#' + t.id + ')'" :value="t.id" />
          </el-select>
          <div style="font-size:11px;color:var(--text-secondary);margin-top:2px">所选任务必须先成功执行，此任务才会运行</div>
        </el-form-item>

        <!-- 操作按钮 -->
        <el-form-item>
          <!--
            保存按钮：文字根据模式变化（新建显示"Create Task"，编辑显示"Save Changes"）
            :loading="saving" 保存时按钮转圈
            @click="save" 点击触发保存逻辑
          -->
          <el-button type="primary" @click="save" :loading="saving" data-testid="btn-save-task">{{ isNew?'Create Task':'Save Changes' }}</el-button>
          <!-- 取消按钮：直接返回任务列表 -->
          <el-button @click="router.push('/tasks')">Cancel</el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
// 导入 Vue 工具
import { ref, computed, onMounted } from 'vue'
// 导入路由工具
import { useRoute, useRouter } from 'vue-router'
// 导入任务 API
import { taskAPI, groupAPI } from '../api/index'
// 导入返回箭头图标
import { ArrowLeft } from '@element-plus/icons-vue'
// 导入消息提示工具
import { ElMessage } from 'element-plus'

// useRoute() 获取当前路由信息，route.params.id 就是 URL 中 /tasks/:id 的 :id 部分
const route = useRoute()
// useRouter() 获取路由跳转工具
const router = useRouter()

/**
 * isNew 计算属性：判断当前是"新建任务"还是"编辑已有任务"。
 * 如果路由参数 id 的值是字符串 'new'，就是新建模式。
 * 如果是数字（如 '5'），就是编辑模式。
 */
const isNew = computed(() => route.params.id === 'new')

// saving 表示"是否正在保存中"（true 时保存按钮转圈，防止重复提交）
const saving = ref(false)

/**
 * form 是表单数据对象，包含任务的所有可编辑字段。
 * ref<any>({...}) 创建一个响应式对象，初始值如下：
 *   name: 任务名称（空）
 *   cron_expr: Cron 调度表达式（空）
 *   task_type: 任务类型，默认 'shell'
 *   command: Shell 命令 / Cleanup 的 JSON 配置（空）
 *   http_method: HTTP 请求方法，默认 'GET'
const form = ref<any>({ name:'', run_mode:'cron', restart_policy:'always', max_restart_attempts:10, restart_delay_sec:0, scheduled_restart_sec:0, cron_expr:'', task_type:'shell', command:'', http_method:'GET', http_url:'', http_auth_type:'none', work_dir:'', run_as:'root', group_id: null, dep_ids: [], timeout_sec:300, retry_count:0, retry_interval_sec:10, max_concurrent:1, enabled:true, description:'' })
 *   work_dir: Shell 命令的工作目录（空）
 *   timeout_sec: 超时时间，默认 300 秒（5 分钟）
 *   retry_count: 失败重试次数，默认 0
const form = ref<any>({ name:'', run_mode:'cron', restart_policy:'always', max_restart_attempts:10, restart_delay_sec:0, cron_expr:'', task_type:'shell', command:'', http_method:'GET', http_url:'', http_auth_type:'none', work_dir:'', run_as:'root', group_id: null, dep_ids: [], timeout_sec:300, retry_count:0, retry_interval_sec:10, max_concurrent:1, enabled:true, description:'' })
 *   enabled: 是否启用，默认 true（启用）
 *   description: 任务描述（空）
 */
const form = ref<any>({ name:'', run_mode:'cron', restart_policy:'always', max_restart_attempts:10, cron_expr:'', task_type:'shell', command:'', http_method:'GET', http_url:'', http_auth_type:'none', work_dir:'', run_as:'root', group_id: null, dep_ids: [], timeout_sec:300, retry_count:0, retry_interval_sec:10, max_concurrent:1, enabled:true, description:'' })
const notifyForm = ref<any>({ webhook_url: '', on_failure: true, on_success: false })
const groupList = ref<any[]>([])
const availableDepTasks = ref<any[]>([])

// --- cronHint: 将 cron 表达式翻译为人话 ---
const WEEKDAY_NAMES = ['日', '一', '二', '三', '四', '五', '六']

function describeField(val: string, unit: string): string {
  if (!val || val === '*') return ''
  if (val.startsWith('*/')) return `每${val.slice(2)}${unit}`
  if (val.includes(',')) {
    const parts = val.split(',').map(v => describeField(v, ''))
    return parts.join('、')
  }
  if (val.includes('-')) {
    const [a, b] = val.split('-')
    return `${a}-${b}${unit}`
  }
  return `${val}${unit}`
}

const cronHint = computed(() => {
  const expr = form.value.cron_expr?.trim()
  if (!expr) return '输入 cron 表达式后将显示可读说明'
  const parts = expr.split(/\s+/)
  if (parts.length < 5) return '格式：秒 分 时 日 月 星期'
  const [sec, min, hour, day, month, wday] = parts.length === 6 ? parts : ['0', ...parts]
  const segs: string[] = []
  const hasSec = sec !== '*' && sec !== '0'

  // 秒级
  if (hasSec && min === '*' && hour === '*' && day === '*' && month === '*' && wday === '*') {
    segs.push(describeField(sec, '秒').replace(/^(\d+)$/, '每$1秒'))
    return segs.join(' ')
  }
  if (hasSec) segs.push('每分的' + describeField(sec, '秒').replace(/^(\d{1,2})$/, '第$1秒'))

  // 时分
  if (min === '*' && hour === '*') segs.push('每分钟')
  else if (min.startsWith('*/') && hour === '*') { segs.push(`每${min.slice(2)}分钟`); return segs.join(' ') }
	  else if (hour === '*' && min !== '*') segs.push(`每小时第${describeField(min, '分')}`)
  else if (hour.startsWith('*/') && min === '0') segs.push(`每${hour.slice(2)}小时`)
	  else if (min === '0' && hour !== '*') segs.push(`${hour}:00`)
  else segs.push(`${hour.padStart(2, '0')}:${min.padStart(2, '0')}`)

  // 日期 vs 星期
  if (day !== '*' && wday === '*') segs.push(describeField(day, '号'))
  else if (day === '*' && wday !== '*') segs.push('每' + describeField(wday, '').replace(/^(\d)$/, '周$1').replace(/^0/, '日'))
  else if (day === '*' && wday === '*') segs.push('每天')

  if (month !== '*') segs.push(describeField(month, '月'))
  return segs.join(' ')
})

// --- cronValid: 校验 cron 格式 ---
const cronValid = computed(() => {
  const expr = form.value.cron_expr?.trim()
  if (!expr) return true
  if (CRON_MACROS[expr]) return true
  const parts = expr.split(/\s+/).filter(Boolean)
  return parts.length >= 5 && parts.length <= 6 && /^[\d\*\/\-\,\s]+$/.test(expr)
})

// --- cron 快捷宏 ---
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
function applyMacro(val: string) { form.value.cron_expr = val }
function onCronInput() { /* reactive update handles everything */ }

// --- cron 字段高亮 ---
const cronFieldLabels = ['秒','分','时','日','月','周']
const cronFieldColors = ['#E6A23C','#67C23A','#409EFF','#F56C6C','#909399','#E6A23C']
const cronFields = computed(() => {
  let e = form.value.cron_expr?.trim() || ''
  for (const [k, v] of Object.entries(CRON_MACROS)) { if (e === k) e = v }
  const parts = e.split(/\s+/).filter(Boolean)
  return parts.length >= 5 ? (parts.length === 6 ? parts : ['0', ...parts]) : []
})

// --- 下次执行时间计算（基础版，覆盖常用模式） ---
function parseCronField(f: string, min: number, max: number): number[] {
  f = f.trim()
  if (f === '*') {
    const r: number[] = []; for (let i = min; i <= max; i++) r.push(i); return r
  }
  if (f.startsWith('*/')) {
    const step = parseInt(f.slice(2)) || 1
    const r: number[] = []; for (let i = min; i <= max; i += step) r.push(i); return r
  }
  if (f.includes(',')) {
    const r: number[] = []
    f.split(',').forEach(p => { const v = parseInt(p); if (!isNaN(v) && v >= min && v <= max) r.push(v) })
    return r.sort((a,b)=>a-b)
  }
  if (f.includes('-')) {
    const [a,b] = f.split('-').map(Number); const r: number[] = []
    if (!isNaN(a) && !isNaN(b)) for (let i = a; i <= b && i <= max; i++) if (i >= min) r.push(i)
    return r
  }
  const v = parseInt(f); return !isNaN(v) && v >= min && v <= max ? [v] : []
}

function cronNext(expr: string, count: number = 5): string[] {
  let e = expr.trim()
  for (const [k, v] of Object.entries(CRON_MACROS)) { if (e === k) e = v }
  const parts = e.split(/\s+/).filter(Boolean)
  if (parts.length < 5) return []
  const [secS, minS, hourS, dayS, monS, wdayS] = parts.length === 6 ? parts : ['0', ...parts]
  const secs = parseCronField(secS, 0, 59)
  const mins = parseCronField(minS, 0, 59)
  const hours = parseCronField(hourS, 0, 23)
  const days = parseCronField(dayS, 1, 31)
  const mons = parseCronField(monS, 1, 12)
  const wdays = parseCronField(wdayS, 0, 6)
  if ([secs,mins,hours,days,mons,wdays].some(a => a.length === 0)) return []

  // Detect sub-minute: iterate by seconds if seconds field is non-trivial
  const subMinute = secS !== '*' && secS !== '0'
  const results: Date[] = []
  // Start from next time slot to avoid matching the past
  const start = new Date()
  if (subMinute) {
    start.setSeconds(start.getSeconds() + 1, 0) // next full second
  } else {
    start.setSeconds(0, 0); start.setMinutes(start.getMinutes() + 1) // next full minute
  }
  start.setMilliseconds(0)
  const stepMs = subMinute ? 1000 : 60000
  const maxIter = subMinute ? 86400 : 525600 // 1 day or 1 year
  for (let i = 0; i < maxIter && results.length < count; i++) {
    const d = new Date(start.getTime() + i * stepMs)
    if (!secs.includes(d.getSeconds())) continue
    if (!mins.includes(d.getMinutes())) continue
    if (!hours.includes(d.getHours())) continue
    if (!mons.includes(d.getMonth() + 1)) continue
    const dayMatch = days.includes(d.getDate())
    const wdayMatch = wdays.includes(d.getDay())
    if (dayS === '*' && wdayS !== '*') { if (!wdayMatch) continue }
    else if (dayS !== '*' && wdayS === '*') { if (!dayMatch) continue }
    else if (dayS !== '*' && wdayS !== '*') { if (!dayMatch && !wdayMatch) continue }
    results.push(new Date(d))
  }
  return results.map(d => {
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  })
}

const cronNextRuns = computed(() => {
  const expr = form.value.cron_expr?.trim()
  return expr ? cronNext(expr, 5) : []
})

const depTaskOptions = computed(() => {
  return availableDepTasks.value.filter((t: any) => t.id !== Number(route.params.id))
})
onMounted(async () => {
  // Load group list for the selector
  try { const r = await groupAPI.list(); groupList.value = r.data.data || [] } catch { /* ignore */ }
  // Load all tasks for dependency selector
  try { const r = await taskAPI.list({ page:1, page_size:200 }); availableDepTasks.value = r.data.data.items || [] } catch { /* ignore */ }
  // 如果不是新建模式（即编辑已有任务）
  if (!isNew.value) {
    try {
      const r = await taskAPI.get(Number(route.params.id))
      const d = r.data.data
      form.value = { ...form.value, ...d }
      // Load existing deps
      try {
        const dr = await taskAPI.getDeps(Number(route.params.id))
        const deps = dr.data.data || []
        form.value.dep_ids = deps.map((dep: any) => dep.depends_on_id || dep.id)
      } catch { /* deps load failed, ignore */ }
      // Load notify config
      try {
        const nr = await taskAPI.getNotify(Number(route.params.id))
        if (nr.data.data && nr.data.data.id) {
          notifyForm.value = {
            webhook_url: nr.data.data.webhook_url || '',
            on_failure: nr.data.data.on_failure,
            on_success: nr.data.data.on_success
          }
        }
      } catch { /* ignore */ }
    }
    catch {
      ElMessage.error('Load failed')
      router.push('/tasks')
    }
  }
})

/**
 * save 函数：保存任务（新建或更新）。
 * async 异步函数。
 *
 * 逻辑：
 *   如果是新建模式 -> 调用 taskAPI.create(form.value) 创建任务
 *   如果是编辑模式 -> 调用 taskAPI.update(任务ID, form.value) 更新任务
 *   保存成功后跳回任务列表页
 *   保存失败则弹出错误提示
 */
async function save() {
  // 开始保存，按钮转圈
  saving.value = true
  try {
    const { dep_ids, id, group_name, created_at, updated_at, sort_order, ...taskData } = form.value
    if (isNew.value) {
      // 新建模式：调用创建 API
      const r = await taskAPI.create(taskData)
      const newId = r.data.data.id
      // Save dependencies if any
      if (dep_ids && dep_ids.length > 0) {
        await taskAPI.updateDeps(newId, dep_ids)
      }
      // Save notify
      await taskAPI.updateNotify(newId, notifyForm.value)
      ElMessage.success('Created')
    } else {
      // 编辑模式：把 :id 参数转成数字，调用更新 API
      await taskAPI.update(Number(route.params.id), taskData)
      // Save dependencies
      await taskAPI.updateDeps(Number(route.params.id), dep_ids || [])
      // Save notify
      await taskAPI.updateNotify(Number(route.params.id), notifyForm.value)
      ElMessage.success('Saved')
    }
    // 保存成功，返回任务列表页
    router.push('/tasks')
  } catch(e: any) {
    // 保存失败：显示后端返回的错误信息，如果没有就用默认的 'Failed'
    ElMessage.error(e.response?.data?.message || 'Failed')
  } finally {
    // 无论成功失败，最后都要让按钮停止转圈
    saving.value = false
  }
}
</script>

<style scoped>


/* 宏卡片高亮 */
.macro-tag {
  transition: all 0.2s ease;
}
.macro-tag:hover {
  border-color: var(--primary-color) !important;
  color: var(--primary-color) !important;
  background: rgba(64, 158, 255, 0.05) !important;
}
</style>
