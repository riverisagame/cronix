import { describe, it, expect, beforeAll, afterEach, afterAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupServer } from 'msw/node'
import { createRouter, createMemoryHistory } from 'vue-router'
import ExecutionLogs from '../../src/views/ExecutionLogs.vue'
import { handlers } from '../mocks/handlers'

const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/', name: 'ExecutionLogs', component: ExecutionLogs },
  ],
})

const server = setupServer(...handlers)

function mountExecutionLogs() {
  return mount(ExecutionLogs, {
    global: {
      plugins: [router],
      stubs: {
        'el-card': { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
        'el-row': { template: '<div class="el-row"><slot /></div>' },
        'el-col': { template: '<div class="el-col"><slot /></div>', props: ['span'] },
        'el-input': {
          template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" :placeholder="placeholder" />',
          props: ['modelValue', 'placeholder', 'clearable'],
        },
        'el-select': {
          template: '<div class="el-select"><slot /></div>',
          props: ['modelValue', 'placeholder', 'clearable', 'style'],
        },
        'el-option': {
          template: '<div class="el-option"><slot /></div>',
          props: ['label', 'value'],
        },
        'el-button': {
          template: '<button :disabled="disabled || loading" @click="$emit(\'click\')"><slot /></button>',
          props: ['loading', 'disabled', 'type', 'text', 'size', 'circle'],
        },
        'el-icon': { template: '<span class="el-icon"><slot /></span>', props: ['size', 'color'] },
        'el-pagination': {
          template: '<div class="el-pagination"><slot /></div>',
          props: ['currentPage', 'total', 'pageSize', 'layout'],
        },
        'el-table-v2': {
          template: '<div class="el-table-v2"><slot /></div>',
          props: ['columns', 'data', 'width', 'height', 'rowHeight', 'rowEventHandlers', 'fixed'],
        },
        'el-tag': { template: '<span class="el-tag"><slot /></span>', props: ['type', 'size'] },
        'el-drawer': {
          template: '<div class="el-drawer"><slot /></div>',
          props: ['modelValue', 'title', 'size', 'direction'],
        },
        'el-descriptions': { template: '<div class="el-descriptions"><slot /></div>', props: ['column', 'border', 'size'] },
        'el-descriptions-item': {
          template: '<div class="el-descriptions-item"><slot /></div>',
          props: ['label'],
        },
        'el-popconfirm': {
          template: '<div class="el-popconfirm"><slot name="reference" /><slot /></div>',
          props: ['title'],
        },
        'Search': { template: '<span>🔍</span>' },
        'Refresh': { template: '<span>↻</span>' },
      },
    },
  })
}

describe('ExecutionLogs.vue', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
  afterEach(() => server.resetHandlers())
  afterAll(() => server.close())

  it('renders filter selects', async () => {
    const wrapper = mountExecutionLogs()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="log-status-filter"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="log-time-filter"]').exists()).toBe(true)
  })

  it('renders export CSV and JSON buttons', async () => {
    const wrapper = mountExecutionLogs()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="btn-export-csv"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="btn-export-json"]').exists()).toBe(true)
  })
})
