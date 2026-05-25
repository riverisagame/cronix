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

    <el-card shadow="hover">
      <!--
        el-form：表单组件
        :model="form" 把表单数据绑定到 form 响应式对象上
        label-width="140px" 所有标签宽度统一 140 像素（对齐美观）
        style="max-width:800px" 表单最大宽度 800 像素（太宽了不好看）
      -->
      <el-form :model="form" label-width="140px" style="max-width:800px">
        <!--
          el-divider：分割线组件
          content-position="left" 文字靠左显示
        -->
        <el-divider content-position="left">Basic</el-divider>

        <!-- 任务名称（必填项，用 required 属性在标签前显示红色星号） -->
        <el-form-item label="Task Name" required>
          <!-- v-model="form.name" 双向绑定到 form 对象的 name 属性 -->
          <el-input v-model="form.name" placeholder="e.g. backup-database" />
        </el-form-item>

        <!--
          Cron 表达式（必填项）
          Cron 是一种时间调度语法：5 个字段分别表示分、时、日、月、星期
          例如 "0 30 8 * * *" 表示每天上午 8:30 执行
          星号 * 表示"每一个"（每天、每月等）
        -->
        <el-form-item label="Cron Expression" required>
          <el-input v-model="form.cron_expr" placeholder="0 30 8 * * *" />
          <div style="font-size:12px;color:#909399;margin-top:4px">
            {{ cronHint }}
          </div>
        </el-form-item>

        <!--
          任务类型：单选按钮组
          el-radio-group 包裹多个 el-radio-button
          v-model="form.task_type" 绑定的值变化时，选中按钮自动更新
        -->
        <el-form-item label="Task Type">
          <el-radio-group v-model="form.task_type">
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
            <el-input v-model="form.command" type="textarea" rows="3" placeholder="echo hello" />
          </el-form-item>

          <!-- 工作目录（命令在哪个文件夹下执行），非必填 -->
          <el-form-item label="Work Directory">
            <el-input v-model="form.work_dir" placeholder="/opt/scripts" />
          </el-form-item>

          <!-- 以哪个用户执行（需配置 sudoers），非必填 -->
          <el-form-item label="Run As User">
            <el-input v-model="form.run_as" placeholder="Leave empty to run as cronix" />
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
            <el-input v-model="form.command" type="textarea" rows="3" placeholder='{"path":"/tmp","pattern":"*.log","older_than_hours":72}' />
          </el-form-item>
        </template>

        <!--
          ======== 高级设置（所有类型通用） ========
        -->
        <el-divider content-position="left">Advanced</el-divider>
        <!-- 用 el-row 把两个设置项并排显示 -->
        <el-row :gutter="20">
          <!-- 超时时间（秒）：使用数字输入框 el-input-number -->
          <el-col :span="8">
            <el-form-item label="Timeout(s)">
              <!--
                el-input-number：数字输入框，带有增减按钮
                :min="1" 最小值 1 秒
                :max="3600" 最大值 3600 秒（1 小时）
              -->
              <el-input-number v-model="form.timeout_sec" :min="1" :max="3600" />
            </el-form-item>
          </el-col>
          <!-- 重试次数：失败后自动重试的次数 -->
          <el-col :span="8">
            <el-form-item label="Retry Count">
              <!-- 最小 0 次（不重试），最大 10 次 -->
              <el-input-number v-model="form.retry_count" :min="0" :max="10" />
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
          <div style="font-size:11px;color:#909399;margin-top:2px">所选任务必须先成功执行，此任务才会运行</div>
        </el-form-item>

        <!-- 操作按钮 -->
        <el-form-item>
          <!--
            保存按钮：文字根据模式变化（新建显示"Create Task"，编辑显示"Save Changes"）
            :loading="saving" 保存时按钮转圈
            @click="save" 点击触发保存逻辑
          -->
          <el-button type="primary" @click="save" :loading="saving">{{ isNew?'Create Task':'Save Changes' }}</el-button>
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
 *   http_url: HTTP 请求的 URL 地址（空）
 *   http_auth_type: HTTP 认证方式，默认 'none'
 *   work_dir: Shell 命令的工作目录（空）
 *   timeout_sec: 超时时间，默认 300 秒（5 分钟）
 *   retry_count: 失败重试次数，默认 0
 *   retry_interval_sec: 重试间隔秒数，默认 10
 *   max_concurrent: 最大并发执行数，默认 1
 *   enabled: 是否启用，默认 true（启用）
 *   description: 任务描述（空）
 */
const form = ref<any>({ name:'', cron_expr:'', task_type:'shell', command:'', http_method:'GET', http_url:'', http_auth_type:'none', work_dir:'', run_as:'', group_id: null, dep_ids: [], timeout_sec:300, retry_count:0, retry_interval_sec:10, max_concurrent:1, enabled:true, description:'' })
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
  const [, min, hour, day, month, wday] = parts
  const segs: string[] = []

  // 时间
  if (min === '*' && hour === '*') segs.push('每分钟')
  else if (hour === '*' && min !== '*') segs.push(`每小时第${min}分`)
  else if (min === '0' && hour !== '*') segs.push(`${hour}:00`)
  else segs.push(`${hour.padStart(2, '0')}:${min.padStart(2, '0')}`)

  // 日期 vs 星期
  if (day !== '*' && wday === '*') segs.push(describeField(day, '号'))
  else if (day === '*' && wday !== '*') segs.push('每' + describeField(wday, '').replace(/^(\d)$/, '周$1').replace(/^0/, '日'))
  else if (day === '*' && wday === '*') segs.push('每天')

  if (month !== '*') segs.push(describeField(month, '月'))
  return segs.join(' ')
})

/**
 * onMounted：页面加载完成后执行。
 * 如果是编辑模式（isNew 为 false），从后端加载已有任务的数据填入表单。
 */
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
    const { dep_ids, ...taskData } = form.value
    if (isNew.value) {
      // 新建模式：调用创建 API
      const r = await taskAPI.create(taskData)
      const newId = r.data.data.id
      // Save dependencies if any
      if (dep_ids && dep_ids.length > 0) {
        await taskAPI.updateDeps(newId, dep_ids)
      }
      ElMessage.success('Created')
    } else {
      // 编辑模式：把 :id 参数转成数字，调用更新 API
      await taskAPI.update(Number(route.params.id), taskData)
      // Save dependencies
      await taskAPI.updateDeps(Number(route.params.id), dep_ids || [])
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
