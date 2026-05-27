import { describe, it, expect, beforeAll, afterEach, afterAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupServer } from 'msw/node'
import { createRouter, createMemoryHistory } from 'vue-router'
import App from '../../src/App.vue'
import { handlers } from '../mocks/handlers'

const DashboardStub = { template: '<div>Dashboard Page</div>' }
const TaskListStub = { template: '<div>Task List Page</div>' }
const LoginStub = { template: '<div>Login Page</div>' }

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'Dashboard', component: DashboardStub },
      { path: '/tasks', name: 'TaskList', component: TaskListStub },
      { path: '/login', name: 'Login', component: LoginStub },
      { path: '/groups', name: 'GroupList', component: { template: '<div>Groups</div>' } },
      { path: '/logs', name: 'ExecutionLogs', component: { template: '<div>Logs</div>' } },
      { path: '/settings', name: 'Settings', component: { template: '<div>Settings</div>' } },
    ],
  })
}

const server = setupServer(...handlers)

const stubs = {
  'el-container': { template: '<div class="el-container"><slot /></div>' },
  'el-aside': { template: '<div class="el-aside"><slot /></div>', props: ['width'] },
  'el-header': { template: '<div class="el-header"><slot /></div>', props: ['height', 'style'] },
  'el-main': { template: '<div class="el-main"><slot /></div>' },
  'el-menu': {
    template: '<div class="el-menu"><slot /></div>',
    props: ['defaultActive', 'router', 'backgroundColor', 'textColor', 'activeTextColor', 'style'],
  },
  'el-menu-item': {
    template: '<div @click="$emit(\'click\')"><slot /></div>',
    props: ['index'],
  },
  'el-icon': { template: '<span class="el-icon"><slot /></span>' },
  'Odometer': { template: '<span>O</span>' },
  'List': { template: '<span>L</span>' },
  'Grid': { template: '<span>G</span>' },
  'Files': { template: '<span>F</span>' },
  'Setting': { template: '<span>S</span>' },
  'SwitchButton': { template: '<span>SB</span>' },
  'router-view': { template: '<div class="router-view"><slot /></div>' },
  'transition': { template: '<div><slot /></div>' },
  'component': { template: '<div><slot /></div>' },
}

describe('App.vue', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
  afterEach(() => server.resetHandlers())
  afterAll(() => server.close())

  it('login page shows no sidebar', async () => {
    localStorage.clear()
    const router = createTestRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(App, {
      global: {
        plugins: [router],
        stubs,
      },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="nav-dashboard"]').exists()).toBe(false)
  })

  it('authenticated layout shows sidebar with nav items', async () => {
    localStorage.setItem('token', 'test-token')
    const router = createTestRouter()
    await router.push('/tasks')
    await router.isReady()

    const wrapper = mount(App, {
      global: {
        plugins: [router],
        stubs,
      },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="nav-dashboard"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-tasks"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-groups"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-logs"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-settings"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-logout"]').exists()).toBe(true)
  })

  it('logout clears token', async () => {
    localStorage.setItem('token', 'test-token')
    const router = createTestRouter()
    await router.push('/tasks')
    await router.isReady()

    const wrapper = mount(App, {
      global: {
        plugins: [router],
        stubs,
      },
    })
    await flushPromises()

    const logoutBtn = wrapper.find('[data-testid="nav-logout"]')
    expect(logoutBtn.exists()).toBe(true)

    await logoutBtn.trigger('click')
    await flushPromises()

    expect(localStorage.getItem('token')).toBeNull()
  })
})
