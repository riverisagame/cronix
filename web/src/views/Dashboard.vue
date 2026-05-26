<!--
  Dashboard.vue -- 仪表盘（首页）页面组件。
  这是用户登录后看到的第一个页面，展示系统的整体运行状况概览。
  包含：统计卡片（任务总数、启用数、今日运行数、失败数）+ 成功率进度环 + 最近执行记录表格。
-->

<template>
  <div>
    <!-- 页面标题：System Dashboard -->
    <h2 style="margin-top:0">System Dashboard</h2>

    <!--
      el-row：ElementPlus 的"行"布局组件，类似于表格的一行。
      :gutter="20" 设置列之间的间距为 20 像素。
      style="margin-bottom:20px" 和下一行之间留 20px 的距离。
    -->
    <el-row :gutter="20" style="margin-bottom:20px">
      <!--
        el-col：ElementPlus 的"列"布局组件。
        :span="6" 表示这一列占整行宽度的 6/24（即四分之一），一行能放 4 个这样的卡片。
        整个行被分成 24 份，span 就是占几份。
      -->
      <el-col :span="6">
        <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
        <el-card shadow="hover" class="glass-card">
          <!-- 卡片内部用 flex 布局：图标在左，文字在右，中间有 16px 间距 -->
          <div style="display:flex;align-items:center;gap:16px">
            <!--
              el-icon：图标组件，:size="28" 设置图标大小 28 像素，color="#409EFF" 蓝色
              Grid 是一个网格图标，象征"总览/全部"
            -->
            <el-icon :size="28" color="#409EFF"><Grid /></el-icon>
            <div>
              <!-- 卡片小标题：Total Tasks（任务总数），小号灰色文字 -->
              <div style="font-size:12px;color:#909399">Total Tasks</div>
              <!--
                卡片数值：stats.total_tasks 是从后端获取的统计数据
                || 0 的意思是：如果 stats.total_tasks 不存在（undefined/null），就显示 0
                这是防止页面在数据还没加载完时显示空白
              -->
              <div style="font-size:28px;font-weight:700;color:#e5e7eb;font-family:var(--cyber-font-mono)">{{ stats.total_tasks || 0 }}</div>
            </div>
          </div>
        </el-card>
      </el-col>

      <!-- 第二张卡片：Enabled（已启用的任务数），绿色图标 -->
      <el-col :span="6">
        <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
        <el-card shadow="hover" class="glass-card">
          <div style="display:flex;align-items:center;gap:16px">
            <!-- CircleCheck 对勾图标，绿色 #10b981 -->
            <el-icon :size="28" color="var(--cyber-green)"><CircleCheck /></el-icon>
            <div>
              <div style="font-size:12px;color:#909399">Enabled</div>
              <div style="font-size:28px;font-weight:700;color:var(--cyber-green);font-family:var(--cyber-font-mono)">{{ stats.enabled_tasks || 0 }}</div>
            </div>
          </div>
        </el-card>
      </el-col>

      <!-- 第三张卡片：Today Runs（今日运行次数），橙色图标 -->
      <el-col :span="6">
        <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
        <el-card shadow="hover" class="glass-card">
          <div style="display:flex;align-items:center;gap:16px">
            <!-- Timer 时钟图标，橙色 #f59e0b -->
            <el-icon :size="28" color="#f59e0b"><Timer /></el-icon>
            <div>
              <div style="font-size:12px;color:#909399">Today Runs</div>
              <div style="font-size:28px;font-weight:700;color:#f59e0b;font-family:var(--cyber-font-mono)">{{ stats.today_total || 0 }}</div>
            </div>
          </div>
        </el-card>
      </el-col>

      <!--
        第四张卡片：Failures（今日失败次数）
        颜色是动态的：如果有失败就用红色，没有失败就用绿色（表示一切正常）
      -->
      <el-col :span="6">
        <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
        <el-card shadow="hover" class="glass-card">
          <div style="display:flex;align-items:center;gap:16px">
            <!--
              WarningFilled 警告图标
              :color="failColor" 动态绑定颜色 -- 有失败时红色，无失败时绿色
            -->
            <el-icon :size="28" :color="failColor"><WarningFilled /></el-icon>
            <div>
              <div style="font-size:12px;color:#909399">Failures</div>
              <!-- 数字颜色也是动态的，和图标颜色保持一致 -->
              <div style="font-size:28px;font-weight:700;font-family:var(--cyber-font-mono)" :style="{color: failColor}">{{ stats.today_failed || 0 }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!--
      第二行：左侧成功率进度环 + 右侧最近执行记录表格
    -->
    <el-row :gutter="20">
      <!-- 左侧：成功率卡片，占 8/24（三分之一） -->
      <el-col :span="8">
        <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
        <el-card shadow="hover" class="glass-card">
          <!--
            #header 是 el-card 的"插槽"（slot），用于自定义卡片的标题区域
            Vue 中的插槽（slot）可以理解为"预留的空位"，让使用者自定义某一块的内容
          -->
          <template #header>
            <!-- 标题区域：左边显示"Success Rate"，右边显示"24H"标签 -->
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span style="font-weight:600">Success Rate</span>
              <!-- el-tag 标签组件，type="info" 灰色信息标签 -->
              <el-tag size="small" type="info">24H</el-tag>
            </div>
          </template>
          <!--
            el-progress：进度条组件。
            type="dashboard" 仪表盘样式（环形进度，像一个速度表）
            :percentage="successRate" 动态绑定百分比数值
            :color="progressColor" 动态绑定颜色（高成功率绿色，低成功率红色）
            :stroke-width="14" 环的粗细（像素）
            :width="180" 整个环的大小（像素）
          -->
          <div style="text-align:center;padding:10px 0">
            <el-progress type="dashboard" :percentage="successRate" :color="progressColor" :stroke-width="14" :width="180">
              <!--
                #default 是 el-progress 的默认插槽，用于自定义进度环中间显示的内容
                { percentage } 从插槽中取出 percentage 数据（当前百分比值）
              -->
              <template #default="{ percentage }">
                <!-- 在进度环中间显示大号百分比数字 -->
                <span style="font-size:28px;font-weight:700;color:#e5e7eb;font-family:var(--cyber-font-mono)">{{ percentage }}%</span>
              </template>
            </el-progress>
          </div>
        </el-card>
      </el-col>

      <!-- 右侧：最近执行记录表格，占 16/24（三分之二） -->
      <el-col :span="16">
        <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
        <el-card shadow="hover" class="glass-card">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span style="font-weight:600">Recent Executions</span>
              <!--
                el-button：文本按钮（text 属性让按钮没有背景色，看起来像链接）
                @click="$router.push('/logs')" 点击后跳转到日志页面
              -->
              <el-button text size="small" type="primary" @click="$router.push('/logs')">View All</el-button>
            </div>
          </template>

          <!--
            el-table：表格组件
            :data="recentLogs" 表格数据来源（一个数组，每条数据就是一行）
            stripe 属性让表格有斑马纹（交替行颜色，更易读）
            size="small" 紧凑尺寸
            max-height="300" 表格最大高度 300px，超出后出现纵向滚动条
          -->
          <el-table :data="recentLogs" stripe size="small" max-height="300">
            <!--
              el-table-column：表格列定义
              prop="task_name" 对应数据中 task_name 字段的值
              label="Task" 列头显示的文字
              width="160" 列宽固定 160 像素
            -->
            <el-table-column prop="task_name" label="Task" width="160" />

            <!-- 状态列 -->
            <el-table-column prop="status" label="Status" width="90">
              <!--
                #default="{ row }" 是列的自定义渲染插槽
                row 是当前行的数据对象
              -->
              <template #default="{ row }">
                <!--
                  el-tag 根据 status 值显示不同颜色：
                  success -> 绿色（success类型），其他 -> 红色（danger类型）
                  .toUpperCase() 把英文转成大写字母显示
                -->
                <el-tag :type="row.status==='success'?'success':'danger'" size="small">{{ row.status?.toUpperCase() }}</el-tag>
              </template>
            </el-table-column>

            <!-- 触发方式列：显示是手动触发(mannual)、定时触发(schedule)还是依赖触发(dependency) -->
            <el-table-column prop="trigger_type" label="Trig" width="70" />

            <!--
              时间列：只显示时分秒部分（substring(11,19) 截取字符串的第 11 到第 18 位）
              比如 "2024-01-15T08:30:00+08:00" -> "08:30:00"
            -->
            <el-table-column label="Time" width="100">
              <template #default="{ row }">
                <span style="font-size:12px;color:#8a8d98;font-family:var(--cyber-font-mono)">{{ row.start_time?.substring(11,19) }}</span>
              </template>
            </el-table-column>

            <!--
              输出预览列：显示输出内容的前 100 个字符
              show-overflow-tooltip 属性：当内容超出列宽时，鼠标悬停显示完整内容的气泡提示
            -->
            <el-table-column label="Output" show-overflow-tooltip>
              <template #default="{ row }">
                <!--
                  <code> 标签表示这是代码/程序输出，用等宽字体显示
                  substring(0, 100) 截取前 100 个字符，超出部分省略
                  如果没有输出内容，显示 '-' 占位符
                -->
                <code style="font-size:12px;color:#a3a6ad;font-family:var(--cyber-font-mono)">{{ row.output?.substring(0, 100) || '-' }}</code>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
/**
 * 从 Vue 框架中导入三个工具：
 *   - ref：创建"响应式数据"（数据变了页面自动更新）
 *   - computed：创建"计算属性"（根据其他数据自动算出新值）
 *   - onMounted：在组件"挂载"到页面后执行回调函数（即页面加载完成后自动运行）
 */
import { ref, computed, onMounted } from 'vue'

// 导入仪表盘和日志的 API 函数
import { dashboardAPI, logAPI } from '../api/index'

// 导入仪表盘页面需要用到的 4 个图标组件
import { Grid, CircleCheck, Timer, WarningFilled } from '@element-plus/icons-vue'

/**
 * stats 是一个响应式变量，存储仪表盘的统计数据。
 * ref<any>({}) 表示创建一个响应式变量，初始值是空对象，类型是 any（可以是任意结构）。
 * 等后端数据返回后，把数据填进去：{ total_tasks: 10, enabled_tasks: 8, today_total: 50, today_failed: 2, ... }
 */
const stats = ref<any>({})

/**
 * recentLogs 存储最近执行的日志记录列表。
 * ref<any[]>([]) 创建一个响应式变量，初始值是空数组。
 * any[] 表示"元素类型可以是任意的数组"。
 */
const recentLogs = ref<any[]>([])

/**
 * successRate 是一个"计算属性"。
 * 它根据 stats 中的数据，自动算出成功率（百分比）。
 * 公式：今日成功次数 / 今日总次数 * 100
 * 如果今日总次数为 0（还没运行过任何任务），则成功率显示 100%（没有失败就是 100%）。
 */
const successRate = computed(() => {
  // 取出今日总运行次数，如果没有就是 0
  const t = stats.value.today_total || 0
  // 如果一次都没运行过，直接返回 100（避免除以 0 的错误）
  if (t === 0) return 100
  // 成功率 = 成功次数 / 总次数 * 100，Math.round() 四舍五入取整
  return Math.round(((stats.value.today_success || 0) / t) * 100)
})

/**
 * failColor 是一个"计算属性"。
 * 它决定失败数字的颜色：如果有失败（>0）显示红色，没有则显示绿色（表示一切正常）。
 */
const failColor = computed(() => (stats.value.today_failed || 0) > 0 ? 'var(--cyber-red)' : 'var(--cyber-green)')

/**
 * progressColor 是一个"计算属性"。
 * 它决定进度环的渐变色，根据成功率高低显示不同颜色：
 *   - >= 95%：蓝色到绿色（非常好）
 *   - >= 80%：蓝色到橙色（还行）
 *   - < 80%：橙色到红色（需要关注）
 * 返回一个包含两个颜色的数组，表示渐变方向。
 */
const progressColor = computed(() => {
  // 拿到当前成功率
  const r = successRate.value
  // 成功率大于等于 95%，显示蓝到绿的渐变
  if (r >= 95) return ['#409EFF', '#67C23A']
  // 成功率大于等于 80%，显示蓝到橙的渐变
  if (r >= 80) return ['#409EFF', '#E6A23C']
  // 成功率低于 80%，显示橙到红的渐变（警告色）
  return ['#E6A23C', '#F56C6C']
})

/**
 * onMounted() 注册一个"页面加载完成后执行"的回调函数。
 * 这个函数是 async 异步的，因为要发网络请求。
 *
 * 页面一加载完，立刻做两件事：
 *   1. 请求仪表盘统计数据
 *   2. 请求最近 8 条执行日志
 *
 * 这两个请求是串行的（先等第一个完成再发第二个），虽然没有并行快，
 * 但这里数据量小，影响不大。try/catch 包裹确保一个请求失败不影响另一个。
 */
onMounted(async () => {
  // 请求一：获取仪表盘统计数据
  // try { ... } catch(e) {} 捕获可能的错误，错误静默忽略（页面保持显示 0）
  try { const s = await dashboardAPI.stats(); stats.value = s.data.data } catch(e) {}

  /**
   * 请求二：获取最近 8 条执行日志
   * logAPI.list({ page: 1, page_size: 8 }) 请求第 1 页，每页 8 条
   * 返回的数据结构：{ data: { data: { items: [...], total: 100 } } }
   * 把 items 数组赋值给 recentLogs，如果没有返回空数组 []
   */
  try { const l = await logAPI.list({ page: 1, page_size: 8 }); recentLogs.value = l.data.data.items || [] } catch(e) {}
})
</script>
