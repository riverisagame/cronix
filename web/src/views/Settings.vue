<!--
  Settings.vue -- 系统设置页面组件。
  用户在这里可以调整系统运行参数。
  页面分为三个设置区域：
   1. Executor（执行器）：控制并发执行的任务数和输出截断大小
   2. Log Retention（日志保留）：控制日志保留天数和最大记录数
   3. Circuit Breaker（熔断器）：控制故障熔断的保护参数

  这些设置保存在后端的 config.yaml 文件中，修改后大多即时生效，无需重启服务。
-->

<template>
  <div>
    <!-- 页面标题 -->
    <h2 style="margin-top:0">Settings</h2>

    <!--
      ======== 第一块：执行器设置 ========
      style="margin-bottom:20px" 和下一块之间留间距
    -->
    <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
    <el-card shadow="hover" class="data-card" style="margin-bottom:20px">
      <!--
        #header 插槽自定义卡片标题
        加粗显示 "Executor"
      -->
      <template #header><span style="font-weight:bold">Executor</span></template>
      <!--
        el-row：一行，:gutter="30" 列间距 30px
        el-col：:span="12" 每列占一半宽度
      -->
      <el-row :gutter="30">
        <!-- 左侧：线程池大小 -->
        <el-col :span="12">
          <el-form-item label="Pool Size">
            <!--
              el-input-number：数字输入框
              :min="1" 最少 1 个
              :max="256" 最多 256 个
              这个值表示后端同时能执行多少个任务（goroutine 数量）
            -->
            <el-input-number v-model="form.pool_size" :min="1" :max="256" />
            <!-- 说明文字：小号灰色字体 -->
            <div style="font-size:12px;color:var(--text-secondary);margin-top:4px">Number of goroutines for concurrent execution</div>
          </el-form-item>
        </el-col>

        <!-- 右侧：输出截断大小 -->
        <el-col :span="12">
          <el-form-item label="Output Truncate (KB)">
            <!--
              单位是 KB（千字节），1KB = 1024 字节
              比如设为 64，表示每个任务最多保存 64KB 的输出内容，超出部分截断
            -->
            <el-input-number v-model="form.output_truncate_kb" :min="1" :max="1024" />
            <div style="font-size:12px;color:var(--text-secondary);margin-top:4px">Max output size captured per execution</div>
          </el-form-item>
        </el-col>
      </el-row>
    </el-card>

    <!--
      ======== 第二块：日志保留设置 ========
    -->
    <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
    <el-card shadow="hover" class="data-card" style="margin-bottom:20px">
      <template #header><span style="font-weight:bold">Log Retention</span></template>
      <el-row :gutter="30">
        <!-- 左侧：保留天数 -->
        <el-col :span="12">
          <el-form-item label="Retention (days)">
            <!--
              保留天数：超过这个天数的日志会被自动删除
              最少 1 天，最多 365 天（一年）
            -->
            <el-input-number v-model="form.retention_days" :min="1" :max="365" />
            <div style="font-size:12px;color:var(--text-secondary);margin-top:4px">Auto-delete execution logs older than this</div>
          </el-form-item>
        </el-col>

        <!-- 右侧：最大记录数 -->
        <el-col :span="12">
          <el-form-item label="Max Records">
            <!--
              最大记录数：日志条数的硬性上限
              :min="1000" 最少 1000 条
              :max="10000000" 最多 1000 万条
              :step="10000" 点击增减按钮时每次变化 10000
            -->
            <el-input-number v-model="form.max_records" :min="1000" :max="10000000" :step="10000" />
            <div style="font-size:12px;color:var(--text-secondary);margin-top:4px">Hard cap on execution log row count</div>
          </el-form-item>
        </el-col>
      </el-row>
    </el-card>

    <!--
      ======== 第三块：熔断器设置 ========

      什么是 Circuit Breaker（熔断器）？
        熔断器是一种保护机制。当某个任务连续失败次数超过阈值时，
        系统会自动停止执行该任务一段时间（冷却期），等冷却期结束后再尝试恢复。
        这样可以防止：
        1. 一个出问题的任务不断重复执行，浪费系统资源
        2. 依赖该任务的下游任务也被牵连出问题
        就像家里的电闸：短路时自动跳闸，过一会儿再试着合上。
    -->
    <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
    <el-card shadow="hover" class="data-card">
      <template #header><span style="font-weight:bold">Circuit Breaker</span></template>
      <el-row :gutter="30">
        <!-- 左侧：失败阈值 -->
        <el-col :span="12">
          <el-form-item label="Failure Threshold">
            <!--
              连续失败多少次后触发熔断（打开电路）
              最少 1 次，最多 100 次
              比如设为 5：任务连续失败 5 次后，系统暂停执行它
            -->
            <el-input-number v-model="form.cb_threshold" :min="1" :max="100" />
            <div style="font-size:12px;color:var(--text-secondary);margin-top:4px">Consecutive failures before opening circuit</div>
          </el-form-item>
        </el-col>

        <!-- 右侧：冷却时间 -->
        <el-col :span="12">
          <el-form-item label="Cooldown (seconds)">
            <!--
              熔断后等待多少秒再尝试恢复
              最少 1 秒，最多 3600 秒（1 小时）
              比如设为 60：任务被熔断后，等待 60 秒再尝试重新执行
            -->
            <el-input-number v-model="form.cb_cooldown" :min="1" :max="3600" />
            <div style="font-size:12px;color:var(--text-secondary);margin-top:4px">Wait time before attempting recovery</div>
          </el-form-item>
        </el-col>
      </el-row>
    </el-card>

    <!-- 保存按钮 -->
    <el-button type="primary" @click="save" :loading="saving" style="margin-top:20px" data-testid="btn-save-settings">Save Settings</el-button>

    <!--
      提示信息：告知用户设置保存在 config.yaml 文件中，大部分修改无需重启即可生效
      el-alert：警告提示组件
      type="info" 灰色信息提示样式
      :closable="false" 不允许关闭（用户始终能看到这个说明）
    -->
    <el-alert type="info" :closable="false" style="margin-top:10px" show-icon>
      Settings are saved to config.yaml. Most changes take effect immediately without restart.
    </el-alert>
  </div>
</template>

<script setup lang="ts">
// 导入 Vue 工具
// reactive：创建响应式对象（适合表单这种对象结构的数据）
// onMounted：页面加载后执行
// ref：创建响应式变量（适合布尔、数字等基本类型）
import { reactive, onMounted, ref } from 'vue'

// 导入设置 API 函数
import { settingsAPI } from '../api/index'

// 导入消息提示工具
import { ElMessage } from 'element-plus'

/**
 * form 使用 reactive() 创建响应式表单对象。
 *
 * 为什么用 reactive 而不是 ref？
 *   设置项比较多（6 个字段），用 reactive 包裹一个对象更自然。
 *   访问时不需要 .value，直接 form.pool_size 即可。
 *
 * 各字段默认值：
 *   pool_size: 32           -- 并发执行线程数（同时运行多少个任务）
 *   output_truncate_kb: 64  -- 输出截断大小（KB）
 *   retention_days: 30      -- 日志保留天数
 *   max_records: 100000     -- 最大日志记录数（10 万条）
 *   cb_threshold: 5         -- 熔断器失败阈值（连续失败 5 次触发熔断）
 *   cb_cooldown: 60         -- 熔断器冷却时间（60 秒后尝试恢复）
 */
const form = reactive({
  pool_size: 32,
  output_truncate_kb: 64,
  retention_days: 30,
  max_records: 100000,
  cb_threshold: 5,
  cb_cooldown: 60
})

// saving：是否正在保存中（true 时按钮转圈）
const saving = ref(false)

/**
 * onMounted：页面加载完成后执行。
 * 从后端获取当前设置值，填入表单。
 */
onMounted(async () => {
  try {
    // 调用 settingsAPI.get() 获取当前设置
    const r = await settingsAPI.get()
    // 从返回数据中取出设置对象
    const d = r.data.data
    /**
     * Object.assign(form, d) 把后端返回的数据合并到 form 对象里。
     *
     * Object.assign() 是 JavaScript 内置函数：
     *   它把第二个对象（d）的所有属性复制到第一个对象（form）上。
     *   如果有同名属性，d 的值会覆盖 form 的默认值。
     *   如果 d 为 null/undefined，不执行合并（form 保持默认值）。
     *
     * 和 TaskEdit 中 { ...form, ...data } 的展开语法效果类似，
     * 但 Object.assign 是直接修改原对象，不创建新对象。
     */
    if (d) Object.assign(form, d)
  } catch (e: any) {
    // 加载失败，弹出错误提示
    ElMessage.error('Failed to load settings')
  }
})

/**
 * save 函数：保存设置到后端。
 */
async function save() {
  // 开始保存，按钮转圈
  saving.value = true
  try {
    /**
     * { ...form } 使用展开运算符把 reactive 对象转成普通对象。
     * （虽然这里直接传 form 也能工作，但转成普通对象是更安全的做法）
     * 然后调用 settingsAPI.update() 发送 PUT 请求到后端。
     */
    await settingsAPI.update({ ...form })
    // 保存成功，弹出绿色提示
    ElMessage.success('Settings saved')
  } catch (e: any) {
    // 保存失败，显示错误信息
    ElMessage.error(e.response?.data?.message || 'Failed to save')
  } finally {
    // 不管成功失败，按钮停止转圈
    saving.value = false
  }
}
</script>
