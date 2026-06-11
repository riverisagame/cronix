<template>
  <div ref="wrapperRef" class="fullscreen-wrapper" style="height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color);">
    <div class="terminal-header">
      <div class="terminal-status">
        <span class="status-dot" :class="normalizedStatus.toLowerCase()"></span>
        <span class="status-text" v-if="mode === 'live'">
          <!-- Live mode: explicitly show STOPPED if not running -->
          <template v-if="normalizedStatus === 'RUNNING'">RUNNING</template>
          <template v-else>
            <span data-testid="live-status-stopped" style="color: var(--el-color-danger)">STOPPED</span>
          </template>
        </span>
        <span class="status-text" v-else>
          {{ normalizedStatus }}
        </span>
        <span class="status-time" v-if="duration" style="margin-left: 12px; font-size: 12px; color: #a8b2c1; font-family: var(--font-mono)">
          Elapsed: {{ duration }}
        </span>
      </div>
      
      <el-input v-model="searchQuery" placeholder="Search logs..." class="terminal-search" clearable size="small" data-testid="live-search">
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>

      <el-button size="small" type="primary" plain @click="toggleFullscreen" data-testid="btn-fullscreen">
        <el-icon><FullScreen /></el-icon> Fullscreen
      </el-button>

      <el-popconfirm v-if="mode === 'live'" title="Are you sure to kill this task?" confirm-button-type="danger" @confirm="emit('kill')" :disabled="normalizedStatus !== 'RUNNING'">
        <template #reference>
          <el-button type="danger" size="small" :disabled="normalizedStatus !== 'RUNNING'" data-testid="btn-kill-task">
            <el-icon><VideoPause /></el-icon> Kill Task
          </el-button>
        </template>
      </el-popconfirm>
      
      <!-- In history mode, show download buttons -->
      <el-button-group v-if="mode === 'history'">
        <el-button size="small" type="primary" plain @click="emit('download', 'csv')">Export CSV</el-button>
        <el-button size="small" type="primary" plain @click="emit('download', 'json')">Export JSON</el-button>
      </el-button-group>
    </div>

    <div 
      class="terminal-body" 
      ref="terminalRef" 
      @scroll="handleScroll"
    >
      <pre v-if="logs" class="terminal-content" v-html="highlightedLogs"></pre>
      <div v-else class="terminal-empty">Waiting for execution logs...</div>
      
      <!-- Auto-scroll indicator/button -->
      <div class="scroll-resume-btn" v-show="!autoScroll && mode === 'live'" @click="resumeAutoScroll">
        Resume auto-scroll
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { Search, FullScreen, VideoPause } from '@element-plus/icons-vue'

const props = defineProps<{
  mode: 'live' | 'history'
  status?: string
  logs?: string
  duration?: string
}>()

const emit = defineEmits(['kill', 'download'])

const wrapperRef = ref<HTMLElement | null>(null)
const terminalRef = ref<HTMLElement | null>(null)
const searchQuery = ref('')
const autoScroll = ref(true)

const normalizedStatus = computed(() => {
  if (!props.status) return 'STOPPED'
  const s = props.status.toUpperCase()
  if (s === 'SUCCESS') return 'SUCCESS'
  if (s === 'FAILED') return 'ERROR'
  if (s === 'RUNNING') return 'RUNNING'
  return 'STOPPED'
})

// Highlight logs based on search query
const highlightedLogs = computed(() => {
  if (!props.logs) return ''
  if (!searchQuery.value) {
    // Basic escape
    return props.logs
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
  }
  const escapedLog = props.logs
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
  
  const searchRegex = new RegExp(`(${searchQuery.value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi')
  return escapedLog.replace(searchRegex, '<span class="log-highlight">$1</span>')
})

const handleScroll = (e: Event) => {
  if (props.mode !== 'live') return
  const el = e.target as HTMLElement
  if (!el) return
  const isAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50
  if (!isAtBottom) {
    autoScroll.value = false
  } else {
    autoScroll.value = true
  }
}

const resumeAutoScroll = () => {
  autoScroll.value = true
  scrollToBottom()
}

const scrollToBottom = () => {
  if (terminalRef.value) {
    terminalRef.value.scrollTop = terminalRef.value.scrollHeight
  }
}

watch(() => props.logs, () => {
  if (props.mode === 'live' && autoScroll.value) {
    nextTick(() => {
      scrollToBottom()
    })
  }
})

onMounted(() => {
  if (props.mode === 'live') {
    scrollToBottom()
  }
})

// Fullscreen API Logic
const toggleFullscreen = async () => {
  if (!wrapperRef.value) return
  if (!document.fullscreenElement) {
    try {
      await wrapperRef.value.requestFullscreen()
    } catch (err) {
      console.warn('Error attempting to enable fullscreen:', err)
    }
  } else {
    if (document.exitFullscreen) {
      await document.exitFullscreen()
    }
  }
}

</script>

<style scoped>
.fullscreen-wrapper {
  transition: all 0.2s ease;
}

.terminal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  background-color: #f8fafc;
  border: 1px solid #e2e8f0;
  border-bottom: none;
  border-radius: 8px 8px 0 0;
  gap: 16px;
}

.terminal-status {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 120px;
}

.status-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background-color: #64748b;
}
.status-dot.running {
  background-color: #10b981;
  box-shadow: 0 0 8px rgba(16, 185, 129, 0.6);
  animation: pulseGreen 1.5s infinite;
}
.status-dot.error, .status-dot.failed {
  background-color: #ef4444;
}
.status-dot.success {
  background-color: #10b981;
}
.status-dot.stopped {
  background-color: #64748b;
}

@keyframes pulseGreen {
  0% { opacity: 0.6; }
  50% { opacity: 1; }
  100% { opacity: 0.6; }
}

.status-text {
  font-weight: 600;
  font-size: 13px;
  color: #334155;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.terminal-search {
  flex: 1;
  max-width: 300px;
}

.terminal-body {
  flex: 1;
  background-color: #1e293b;
  color: #f8fafc;
  border-radius: 0 0 8px 8px;
  padding: 16px;
  height: 60vh;
  overflow-y: auto;
  position: relative;
  box-shadow: inset 0 2px 10px rgba(0, 0, 0, 0.2);
}

.terminal-content {
  margin: 0;
  font-family: 'Fira Code', var(--font-mono, monospace);
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}

.terminal-empty {
  color: #64748b;
  font-family: 'Fira Code', var(--font-mono, monospace);
  font-size: 13px;
  text-align: center;
  margin-top: 40px;
}

.scroll-resume-btn {
  position: sticky;
  bottom: 20px;
  left: 50%;
  transform: translateX(-50%);
  background-color: rgba(15, 23, 42, 0.8);
  color: #38bdf8;
  padding: 6px 16px;
  border-radius: 20px;
  font-size: 12px;
  cursor: pointer;
  backdrop-filter: blur(4px);
  border: 1px solid rgba(56, 189, 248, 0.3);
  transition: all 0.2s;
  z-index: 10;
  display: inline-block;
  text-align: center;
}
.scroll-resume-btn:hover {
  background-color: rgba(15, 23, 42, 0.95);
  color: #bae6fd;
}

:deep(.log-highlight) {
  background-color: #f59e0b;
  color: #fff;
  padding: 0 2px;
  border-radius: 2px;
}
</style>
