import { describe, it, expect, beforeAll, afterEach, afterAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupServer } from 'msw/node'
import { createRouter, createMemoryHistory } from 'vue-router'
import TaskList from '../../src/views/TaskList.vue'
import { handlers } from '../mocks/handlers'

const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/', name: 'TaskList', component: TaskList },
    { path: '/tasks/new', name: 'TaskNew', component: { template: '<div>New Task</div>' } },
    { path: '/tasks/:id', name: 'TaskEdit', component: { template: '<div>Edit Task</div>' } },
  ],
})

const server = setupServer(...handlers)

function mountTaskList() {
  return mount(TaskList, {
    global: {
      plugins: [router],
      stubs: {
        'el-row': { template: '<div class="el-row"><slot /></div>' },
        'el-col': { template: '<div class="el-col"><slot /></div>' },
        'el-card': { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
        'el-icon': { template: '<span class="el-icon"><slot /></span>', props: ['size', 'color'] },
        'el-button': {
          template: '<button @click="$emit(\'click\')"><slot /></button>',
          props: ['text', 'size', 'type', 'circle', 'loading'],
        },
        'el-input': {
          template: '<input />',
          props: ['modelValue', 'placeholder', 'clearable'],
        },
        'el-table': {
          template: '<div class="el-table"><slot /></div>',
          props: ['data', 'stripe', 'loading', 'rowClassName', 'maxHeight'],
        },
        'el-table-column': {
          template: '<div class="el-table-column"><slot :row="{}" /></div>',
          props: ['prop', 'label', 'width', 'minWidth', 'showOverflowTooltip', 'fixed', 'align'],
        },
        'el-tag': { template: '<span class="el-tag"><slot /></span>', props: ['type', 'size'] },
        'el-switch': {
          template: '<div class="el-switch"><slot /></div>',
          props: ['modelValue', 'activeText', 'inactiveText', 'inlinePrompt'],
        },
        'el-popconfirm': {
          template: '<div class="el-popconfirm"><slot name="reference" /><slot /></div>',
          props: ['title'],
        },
        'el-pagination': {
          template: '<div class="el-pagination"><slot /></div>',
          props: ['currentPage', 'total', 'pageSize', 'layout'],
        },
        'el-drawer': {
          template: '<div class="el-drawer"><slot /></div>',
          props: ['modelValue', 'title', 'size', 'direction'],
        },
        'el-timeline': { template: '<div class="el-timeline"><slot /></div>' },
        'el-timeline-item': {
          template: '<div class="el-timeline-item"><slot /></div>',
          props: ['timestamp', 'placement', 'color'],
        },
        'el-select': {
          template: '<div class="el-select"><slot /></div>',
          props: ['modelValue', 'placeholder', 'clearable'],
        },
        'el-option': {
          template: '<div class="el-option"><slot /></div>',
          props: ['label', 'value'],
        },
        'Plus': { template: '<span>+</span>' },
        'Search': { template: '<span>🔍</span>' },
        'Refresh': { template: '<span>↻</span>' },
        'VideoPlay': { template: '<span>▶</span>' },
        'Delete': { template: '<span>✕</span>' },
        'Edit': { template: '<span>✎</span>' },
        'Tickets': { template: '<span>📋</span>' },
      },
    },
  })
}

describe('TaskList.vue', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
  afterEach(() => server.resetHandlers())
  afterAll(() => server.close())

  it('renders New Task button', async () => {
    const wrapper = mountTaskList()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="btn-new-task"]').exists()).toBe(true)
  })

  it('renders search input', async () => {
    const wrapper = mountTaskList()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="task-search"]').exists()).toBe(true)
  })

  it('renders task table', async () => {
    const wrapper = mountTaskList()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="task-table"]').exists()).toBe(true)
  })

  it('renders run and log action buttons', async () => {
    const wrapper = mountTaskList()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="btn-run-task"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="btn-task-logs"]').exists()).toBe(true)
  })
})
