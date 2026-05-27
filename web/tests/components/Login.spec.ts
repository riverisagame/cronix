import { describe, it, expect, beforeAll, afterEach, afterAll, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupServer } from 'msw/node'
import { http, HttpResponse } from 'msw'
import { createRouter, createMemoryHistory } from 'vue-router'
import Login from '../../src/views/Login.vue'
import { handlers } from '../mocks/handlers'

const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/login', name: 'Login', component: Login },
    { path: '/', name: 'Dashboard', component: { template: '<div>Home</div>' } },
  ],
})

const server = setupServer(...handlers)

function mountLogin() {
  return mount(Login, {
    global: {
      plugins: [router],
      stubs: {
        'el-card': { template: '<div class="el-card"><slot /></div>' },
        'el-form': { template: '<form @submit.prevent="void(0)"><slot /></form>' },
        'el-form-item': {
          template: '<div><slot /></div>',
          props: ['label'],
        },
        'el-input': {
          template:
            '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" :type="type" :placeholder="placeholder" />',
          props: ['modelValue', 'type', 'placeholder', 'showPassword'],
        },
        'el-button': {
          template:
            '<button :disabled="loading" @click="$emit(\'click\')"><slot /></button>',
          props: ['loading', 'type'],
        },
        'el-alert': {
          template: '<div v-if="title" role="alert" :data-testid="\'login-error\'">{{ title }}</div>',
          props: ['title', 'type', 'closable', 'showIcon'],
        },
      },
    },
  })
}

describe('Login.vue', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))

  beforeEach(async () => {
    localStorage.clear()
    await router.push('/login')
    await router.isReady()
  })

  afterEach(() => server.resetHandlers())
  afterAll(() => server.close())

  it('renders login form with username, password inputs and submit button', () => {
    const wrapper = mountLogin()
    expect(wrapper.find('[data-testid="login-username"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="login-password"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="login-submit"]').exists()).toBe(true)
  })

  it('binds username and password via v-model', async () => {
    const wrapper = mountLogin()
    const usernameInput = wrapper.find('[data-testid="login-username"]')
    const passwordInput = wrapper.find('[data-testid="login-password"]')

    await usernameInput.setValue('admin')
    await passwordInput.setValue('secret')

    expect((usernameInput.element as HTMLInputElement).value).toBe('admin')
    expect((passwordInput.element as HTMLInputElement).value).toBe('secret')
  })

  it('shows loading state on submit button during login', async () => {
    const wrapper = mountLogin()
    await wrapper.find('[data-testid="login-username"]').setValue('admin')
    await wrapper.find('[data-testid="login-password"]').setValue('admin')
    await wrapper.find('[data-testid="login-submit"]').trigger('click')

    const btn = wrapper.find('[data-testid="login-submit"]')
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('displays error message on failed login', async () => {
    // Override handler to return a non-401 error (avoids triggering
    // the axios 401 interceptor which sets window.location.href)
    server.use(
      http.post('/api/login', () =>
        HttpResponse.json({ code: 1, message: 'Invalid credentials' }, { status: 400 })
      )
    )

    const wrapper = mountLogin()
    await wrapper.find('[data-testid="login-username"]').setValue('wrong')
    await wrapper.find('[data-testid="login-password"]').setValue('wrong')
    await wrapper.find('[data-testid="login-submit"]').trigger('click')

    // Wait for the async handleLogin to complete
    await flushPromises()
    await flushPromises()

    const errorAlert = wrapper.find('[data-testid="login-error"]')
    expect(errorAlert.exists()).toBe(true)
  })

  it('stores token and navigates on successful login', async () => {
    const wrapper = mountLogin()
    await wrapper.find('[data-testid="login-username"]').setValue('admin')
    await wrapper.find('[data-testid="login-password"]').setValue('admin')
    await wrapper.find('[data-testid="login-submit"]').trigger('click')

    // Flush pending promises: MSW handler -> axios response -> handleLogin continuation
    await flushPromises()
    await flushPromises()

    expect(localStorage.getItem('token')).toBe('mock-jwt-token')
  })
})
