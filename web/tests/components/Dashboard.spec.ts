import { describe, it, expect, beforeAll, afterEach, afterAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupServer } from 'msw/node'
import { createRouter, createMemoryHistory } from 'vue-router'
import Dashboard from '../../src/views/Dashboard.vue'
import { handlers } from '../mocks/handlers'

const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/', name: 'Dashboard', component: Dashboard },
    { path: '/logs', name: 'Logs', component: { template: '<div>Logs</div>' } },
  ],
})

const server = setupServer(...handlers)

function mountDashboard() {
  return mount(Dashboard, {
    global: {
      plugins: [router],
      stubs: {
        'el-row': { template: '<div class="el-row"><slot /></div>' },
        'el-col': { template: '<div class="el-col"><slot /></div>' },
        'el-card': { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
        'el-icon': { template: '<span class="el-icon"><slot /></span>', props: ['size', 'color'] },
        'el-progress': {
          template: '<div class="el-progress"><slot :percentage="50" /></div>',
          props: ['type', 'percentage', 'color', 'strokeWidth', 'width'],
        },
        'el-table': {
          template: '<div class="el-table"><slot /></div>',
          props: ['data', 'stripe', 'size', 'maxHeight'],
        },
        'el-table-column': {
          template: '<div class="el-table-column"><slot :row="{}" /></div>',
          props: ['prop', 'label', 'width', 'showOverflowTooltip'],
        },
        'el-tag': { template: '<span class="el-tag"><slot /></span>', props: ['type', 'size'] },
        'el-button': {
          template: '<button @click="$emit(\'click\')"><slot /></button>',
          props: ['text', 'size', 'type'],
        },
      },
    },
  })
}

describe('Dashboard.vue', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
  afterEach(() => server.resetHandlers())
  afterAll(() => server.close())

  it('renders 4 stat cards with data-testid', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="stat-total-tasks"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="stat-enabled"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="stat-today-runs"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="stat-failures"]').exists()).toBe(true)
  })

  it('renders success rate section', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="success-rate"]').exists()).toBe(true)
  })

  it('renders recent executions table', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="recent-executions-table"]').exists()).toBe(true)
  })
})
