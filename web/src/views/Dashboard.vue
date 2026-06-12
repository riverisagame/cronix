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
        <el-card shadow="hover" class="data-card" data-testid="stat-total-tasks">
          <!-- 卡片内部用 flex 布局：图标在左，文字在右，中间有 16px 间距 -->
          <div style="display:flex;align-items:center;gap:16px">
            <!--
              el-icon：图标组件，:size="28" 设置图标大小 28 像素，color="#409EFF" 蓝色
              Grid 是一个网格图标，象征"总览/全部"
            -->
            <el-icon :size="28" color="#409EFF"><Grid /></el-icon>
            <div>
              <!-- 卡片小标题：Total Tasks（任务总数），小号灰色文字 -->
              <div style="font-size:12px;color:var(--text-secondary)">Total Tasks</div>
              <div style="font-size:36px;font-weight:700;color:var(--text-main);font-family:var(--font-mono)">{{ stats.total_tasks || 0 }}</div>
            </div>
          </div>
        </el-card>
      </el-col>

      <!-- 第二张卡片：Enabled（已启用的任务数），绿色图标 -->
      <el-col :span="6">
        <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
        <el-card shadow="hover" class="data-card" data-testid="stat-enabled">
          <div style="display:flex;align-items:center;gap:16px">
            <!-- CircleCheck 对勾图标，绿色 #10b981 -->
            <el-icon :size="28" color="var(--success-color)"><CircleCheck /></el-icon>
            <div>
              <div style="font-size:12px;color:var(--text-secondary)">Enabled</div>
              <div style="font-size:36px;font-weight:700;color:var(--success-color);font-family:var(--font-mono)">{{ stats.enabled_tasks || 0 }}</div>
            </div>
          </div>
        </el-card>
      </el-col>

      <!-- 第三张卡片：Today Runs（今日运行次数），橙色图标 -->
      <el-col :span="6">
        <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
        <el-card shadow="hover" class="data-card" data-testid="stat-today-runs">
          <div style="display:flex;align-items:center;gap:16px">
            <!-- Timer 时钟图标，橙色 #f59e0b -->
            <el-icon :size="28" color="#f59e0b"><Timer /></el-icon>
            <div>
              <div style="font-size:12px;color:var(--text-secondary)">Today Runs</div>
              <div style="font-size:36px;font-weight:700;color:#f59e0b;font-family:var(--font-mono)">{{ stats.today_total || 0 }}</div>
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
        <el-card shadow="hover" class="data-card" data-testid="stat-failures">
          <div style="display:flex;align-items:center;gap:16px">
            <!--
              WarningFilled 警告图标
              :color="failColor" 动态绑定颜色 -- 有失败时红色，无失败时绿色
            -->
            <el-icon :size="28" :color="failColor"><WarningFilled /></el-icon>
            <div>
              <div style="font-size:12px;color:var(--text-secondary)">Failures</div>
              <!-- 数字颜色也是动态的，和图标颜色保持一致 -->
              <div style="font-size:36px;font-weight:700;font-family:var(--font-mono)" :style="{color: failColor}">{{ stats.today_failed || 0 }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- Daemon Status Overview -->
    <el-row :gutter="20" style="margin-bottom:20px" v-if="daemonStats.total > 0">
      <el-col :span="6">
        <el-card shadow="hover" class="data-card">
          <div style="display:flex;align-items:center;gap:16px">
            <el-icon :size="28" color="#10b981"><VideoPlay /></el-icon>
            <div>
              <div style="font-size:12px;color:var(--text-secondary)">Daemon Running</div>
              <div style="font-size:36px;font-weight:700;color:#10b981;font-family:var(--font-mono)">{{ daemonStats.running }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="data-card">
          <div style="display:flex;align-items:center;gap:16px">
            <el-icon :size="28" color="#64748b"><VideoPause /></el-icon>
            <div>
              <div style="font-size:12px;color:var(--text-secondary)">Daemon Stopped</div>
              <div style="font-size:36px;font-weight:700;color:#64748b;font-family:var(--font-mono)">{{ daemonStats.stopped }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="data-card">
          <div style="display:flex;align-items:center;gap:16px">
            <el-icon :size="28" color="var(--warning-color)"><WarningFilled /></el-icon>
            <div>
              <div style="font-size:12px;color:var(--text-secondary)">Daemon Backoff</div>
              <div style="font-size:36px;font-weight:700;color:var(--warning-color);font-family:var(--font-mono)">{{ daemonStats.backoff }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="data-card">
          <div style="display:flex;align-items:center;gap:16px">
            <el-icon :size="28" color="var(--danger-color)"><CircleCloseFilled /></el-icon>
            <div>
              <div style="font-size:12px;color:var(--text-secondary)">Daemon Fatal</div>
              <div style="font-size:36px;font-weight:700;color:var(--danger-color);font-family:var(--font-mono)">{{ daemonStats.fatal }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
      第二行：左侧成功率进度环 + 右侧最近执行记录表格
    -->
    <el-row :gutter="20">
      <!-- 左侧：成功率卡片，占 8/24（三分之一） -->
      <el-col :span="8">
        <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
        <el-card shadow="hover" class="data-card" data-testid="success-rate">
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
                <span style="font-size:36px;font-weight:700;color:var(--text-main);font-family:var(--font-mono)">{{ percentage }}%</span>
              </template>
            </el-progress>
          </div>
        </el-card>
      </el-col>

      <!-- 右侧：最近执行记录表格，占 16/24（三分之二） -->
      <el-col :span="16">
        <!-- @Ref: docs/sps/plans/20260527_ui_ux_refinement_plan.md | @Date: 2026-05-27 -->
        <el-card shadow="hover" class="data-card" data-testid="recent-executions-table">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span style="font-weight:600">Recent Executions</span>
              <!--
                el-button：文本按钮（text 属性让按钮没有背景色，看起来像链接）
                @click="$router.push('/logs')" 点击后跳转到日志页面
              -->
              <el-button text type="primary" @click="$router.push('/logs')">View All</el-button>
            </div>
          </template>

          <!--
            el-table：表格组件
            :data="recentLogs" 表格数据来源（一个数组，每条数据就是一行）
            stripe 属性让表格有斑马纹（交替行颜色，更易读）
            size="small" 紧凑尺寸
            max-height="300" 表格最大高度 300px，超出后出现纵向滚动条
          -->
          <el-table :data="recentLogs" stripe max-height="400">
            <!--
              el-table-column：表格列定义
              prop="task_name" 对应数据中 task_name 字段的值
              label="Task" 列头显示的文字
              width="160" 列宽固定 160 像素
            -->
            <el-table-column prop="task_name" label="Task" width="160" />

            <!-- 状态列 -->
            <el-table-column prop="status" label="Status" width="90">
              <template #default="{ row }">
                <el-tag 
                  :type="row.status === 'success' ? 'success' : row.status === 'timeout' ? 'warning' : row.status === 'failed' ? 'danger' : 'info'" 
                  :class="{'tag-running-vibrant': row.status === 'running'}">
                  {{ row.status?.toUpperCase() }}
                </el-tag>
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
                <span style="font-size:12px;color:var(--text-secondary);font-family:var(--font-mono)">{{ row.start_time?.substring(11,19) }}</span>
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
                <code v-if="row.output" style="font-size:12px;color:var(--text-secondary);font-family:var(--font-mono)">{{ row.output?.substring(0, 100) }}{{ row.output?.length > 100 ? '...' : '' }}</code>
                <span v-else style="color:var(--text-secondary)">-</span>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>
	<!-- 第三行：性能指标图表 -->
	<!-- @Ref: docs/sps/plans/20260605_metrics_plan.md | @Date: 2026-06-05 -->
	<el-row :gutter="20" style="margin-top:20px">
	  <el-col :span="12">
		<el-card shadow="hover" class="data-card">
		  <template #header>
			<span style="font-weight:600">Throughput (Tasks/min)</span>
		  </template>
		  <div style="height:300px">
			<v-chart class="chart" :option="throughputOption" autoresize />
		  </div>
		</el-card>
	  </el-col>
	  <el-col :span="12">
		<el-card shadow="hover" class="data-card">
		  <template #header>
			<span style="font-weight:600">Latency (P95 / P99 ms)</span>
		  </template>
		  <div style="height:300px">
			<v-chart class="chart" :option="latencyOption" autoresize />
		  </div>
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
import { ref, computed, onMounted, onUnmounted } from 'vue'

import { dashboardAPI, logAPI, daemonAPI } from '../api/index'

import { Grid, CircleCheck, Timer, WarningFilled, VideoPlay, VideoPause, CircleCloseFilled } from '@element-plus/icons-vue'

// 导入 ECharts
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart, BarChart } from 'echarts/charts'
import {
  TitleComponent,
  TooltipComponent,
  LegendComponent,
  GridComponent
} from 'echarts/components'
import VChart from 'vue-echarts'

use([
  CanvasRenderer,
  LineChart,
  BarChart,
  TitleComponent,
  TooltipComponent,
  LegendComponent,
  GridComponent
])

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

const daemonStats = ref({ total: 0, running: 0, stopped: 0, backoff: 0, fatal: 0 })

const fetchDaemonStats = async () => {
  try {
    const res: any = await daemonAPI.getAllStates()
    const states = res?.data?.data || {}
    const counts = { total: 0, running: 0, stopped: 0, backoff: 0, fatal: 0 }
    for (const k of Object.keys(states)) {
      const s = states[k]?.status || ''
      counts.total++
      if (s === 'RUNNING' || s === 'STARTING') counts.running++
      else if (s === 'BACKOFF') counts.backoff++
      else if (s === 'FATAL') counts.fatal++
      else counts.stopped++
    }
    daemonStats.value = counts
  } catch(e) {}
}
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
const failColor = computed(() => (stats.value.today_failed || 0) > 0 ? 'var(--error-color)' : 'var(--success-color)')

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
 * 这两个请求是串行的（先等第一个完成再发第二个）
 */
onMounted(async () => {
  try { const s = await dashboardAPI.stats(); stats.value = s.data.data } catch(e) {}
  try { const l = await logAPI.list({ page: 1, page_size: 8 }); recentLogs.value = l.data.data.items || [] } catch(e) {}
  await refreshMetrics()
  await fetchDaemonStats()
  metricsTimer = setInterval(refreshMetrics, 15000)
  daemonTimer = setInterval(fetchDaemonStats, 5000)
})
let metricsTimer: any = null
let daemonTimer: any = null
onUnmounted(() => {
  if (metricsTimer) clearInterval(metricsTimer)
  if (daemonTimer) clearInterval(daemonTimer)
})

const throughputOption = ref<any>({})
const latencyOption = ref<any>({})

const refreshMetrics = async () => {
  try {
    const res = await dashboardAPI.metrics()
    const data = res.data.data

    // Throughput Chart (Bar Chart for Success vs Failed)
    throughputOption.value = {
      tooltip: { 
        trigger: 'axis', 
        axisPointer: { type: 'shadow' },
        backgroundColor: '#171717',
        borderColor: '#3F3F46',
        textStyle: { color: '#F4F4F5' },
        padding: [12, 16]
      },
      legend: { data: ['Success', 'Failed'], textStyle: { color: '#D4D4D8' }, top: 0 },
      grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
      xAxis: { 
        type: 'category', 
        data: data.minute_labels, 
        axisLabel: { color: '#A1A1AA' },
        axisLine: { lineStyle: { color: '#3F3F46' } }
      },
      yAxis: { 
        type: 'value', 
        splitLine: { lineStyle: { color: '#3F3F46', type: 'dashed', opacity: 0.4 } }, 
        axisLabel: { color: '#A1A1AA' } 
      },
      series: [
        { 
          name: 'Success', 
          type: 'bar', 
          stack: 'total', 
          data: data.minute_success, 
          barMaxWidth: 32,
          itemStyle: { 
            color: '#10B981', // Emerald 500
            borderRadius: [2, 2, 0, 0]
          } 
        },
        { 
          name: 'Failed', 
          type: 'bar', 
          stack: 'total', 
          data: data.minute_failed, 
          barMaxWidth: 32,
          itemStyle: { 
            color: '#EF4444', // Red 500
            borderRadius: [2, 2, 0, 0]
          } 
        }
      ]
    }

    // Latency Chart (Line Chart for P95 and P99)
    latencyOption.value = {
      tooltip: { 
        trigger: 'axis',
        backgroundColor: '#171717',
        borderColor: '#3F3F46',
        textStyle: { color: '#F4F4F5' },
        padding: [12, 16]
      },
      legend: { data: ['P95', 'P99'], textStyle: { color: '#D4D4D8' }, top: 0 },
      grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
      xAxis: { 
        type: 'category', 
        boundaryGap: false, 
        data: data.minute_labels, 
        axisLabel: { color: '#A1A1AA' },
        axisLine: { lineStyle: { color: '#3F3F46' } }
      },
      yAxis: { 
        type: 'value', 
        name: 'ms', 
        splitLine: { lineStyle: { color: '#3F3F46', type: 'dashed', opacity: 0.4 } }, 
        axisLabel: { color: '#A1A1AA' }, 
        nameTextStyle: { color: '#A1A1AA', padding: [0, 25, 0, 0] } 
      },
      series: [
        { 
          name: 'P95', 
          type: 'line', 
          smooth: 0.4, 
          data: data.minute_p95, 
          symbol: 'circle',
          symbolSize: 6,
          showSymbol: false,
          itemStyle: { color: '#3B82F6' }, // Blue 500
          lineStyle: { width: 3, color: '#3B82F6' },
          areaStyle: { 
            color: {
              type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
              colorStops: [{ offset: 0, color: 'rgba(59, 130, 246, 0.4)' }, { offset: 1, color: 'rgba(59, 130, 246, 0.0)' }]
            }
          } 
        },
        { 
          name: 'P99', 
          type: 'line', 
          smooth: 0.4, 
          data: data.minute_p99, 
          symbol: 'circle',
          symbolSize: 6,
          showSymbol: false,
          itemStyle: { color: '#8B5CF6' }, // Violet 500
          lineStyle: { width: 3, color: '#8B5CF6' },
          areaStyle: { 
            color: {
              type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
              colorStops: [{ offset: 0, color: 'rgba(139, 92, 246, 0.4)' }, { offset: 1, color: 'rgba(139, 92, 246, 0.0)' }]
            }
          } 
        }
      ]
    }
  } catch (e) {
    console.error('Failed to load metrics:', e)
  }
}
</script>

<style scoped>
.chart {
  width: 100%;
  height: 100%;
}
</style>
