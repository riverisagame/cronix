import { describe, it, expect, beforeAll, afterEach, afterAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupServer } from 'msw/node'
import { createRouter, createMemoryHistory } from 'vue-router'
import Settings from '../../src/views/Settings.vue'
import { handlers } from '../mocks/handlers'

const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/', name: 'Settings', component: Settings },
  ],
})

const server = setupServer(...handlers)

function mountSettings() {
  return mount(Settings, {
    global: {
      plugins: [router],
      stubs: {
        'el-card': { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
        'el-row': { template: '<div class="el-row"><slot /></div>' },
        'el-col': { template: '<div class="el-col"><slot /></div>', props: ['span'] },
        'el-form-item': {
          template: '<div><slot /></div>',
          props: ['label'],
        },
        'el-input-number': {
          template: '<input type="number" />',
          props: ['modelValue', 'min', 'max', 'step'],
        },
        'el-button': {
          template: '<button :disabled="loading" @click="$emit(\'click\')"><slot /></button>',
          props: ['loading', 'type'],
        },
        'el-alert': {
          template: '<div v-if="title"><slot /></div>',
          props: ['title', 'type', 'closable', 'showIcon'],
        },
        'el-icon': { template: '<span class="el-icon"><slot /></span>' },
      },
    },
  })
}

describe('Settings.vue', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
  afterEach(() => server.resetHandlers())
  afterAll(() => server.close())

  it('renders save button', async () => {
    const wrapper = mountSettings()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="btn-save-settings"]').exists()).toBe(true)
  })
})
