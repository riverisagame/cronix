import { describe, it, expect, beforeAll, afterEach, afterAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupServer } from 'msw/node'
import { createRouter, createMemoryHistory } from 'vue-router'
import GroupEdit from '../../src/views/GroupEdit.vue'
import { handlers } from '../mocks/handlers'

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/groups/:id', name: 'GroupEdit', component: GroupEdit },
      { path: '/groups', name: 'GroupList', component: { template: '<div>Group List</div>' } },
    ],
  })
}

const server = setupServer(...handlers)

const stubs = {
  'el-card': { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
  'el-form': { template: '<form @submit.prevent="void(0)"><slot /></form>' },
  'el-form-item': {
    template: '<div><slot /></div>',
    props: ['label'],
  },
  'el-input': {
    template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" :type="type" :placeholder="placeholder" />',
    props: ['modelValue', 'type', 'placeholder', 'rows', 'size'],
  },
  'el-button': {
    template: '<button :disabled="loading" @click="$emit(\'click\')"><slot /></button>',
    props: ['loading', 'type', 'text', 'size', 'circle'],
  },
  'el-icon': { template: '<span class="el-icon"><slot /></span>', props: ['size', 'color'] },
  'el-radio-group': {
    template: '<div><slot /></div>',
    props: ['modelValue'],
  },
  'el-radio': {
    template: '<label><slot /></label>',
    props: ['value'],
  },
  'el-row': { template: '<div class="el-row"><slot /></div>' },
  'el-col': { template: '<div class="el-col"><slot /></div>', props: ['span', 'offset'] },
  'el-tag': { template: '<span class="el-tag"><slot /></span>', props: ['type', 'size'] },
  'el-select': {
    template: '<div class="el-select"><slot /></div>',
    props: ['modelValue', 'placeholder', 'clearable'],
  },
  'el-option': {
    template: '<div class="el-option"><slot /></div>',
    props: ['label', 'value'],
  },
  'el-divider': { template: '<hr />', props: ['contentPosition'] },
  'ArrowLeft': { template: '<span>&lt;</span>' },
  'Plus': { template: '<span>+</span>' },
  'Close': { template: '<span>✕</span>' },
  'TransitionGroup': { template: '<div><slot /></div>' },
}

describe('GroupEdit.vue', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
  afterEach(() => server.resetHandlers())
  afterAll(() => server.close())

  it('renders empty form in new mode', async () => {
    const router = createTestRouter()
    await router.push('/groups/new')
    await router.isReady()

    const wrapper = mount(GroupEdit, {
      global: {
        plugins: [router],
        stubs,
      },
    })
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="group-form-name"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="group-form-mode"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="btn-save-group"]').exists()).toBe(true)
  })

  it('shows Create Group title in new mode', async () => {
    const router = createTestRouter()
    await router.push('/groups/new')
    await router.isReady()

    const wrapper = mount(GroupEdit, {
      global: {
        plugins: [router],
        stubs,
      },
    })
    await flushPromises()
    await flushPromises()

    expect(wrapper.text()).toContain('Create Group')
  })
})
