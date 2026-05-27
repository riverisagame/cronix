import { describe, it, expect, beforeAll, afterEach, afterAll, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupServer } from 'msw/node'
import { createRouter, createMemoryHistory } from 'vue-router'
import TaskEdit from '../../src/views/TaskEdit.vue'
import { handlers } from '../mocks/handlers'

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/tasks/:id', name: 'TaskEdit', component: TaskEdit },
      { path: '/tasks', name: 'TaskList', component: { template: '<div>Task List</div>' } },
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
    props: ['modelValue', 'type', 'placeholder', 'showPassword', 'rows'],
  },
  'el-button': {
    template: '<button :disabled="loading" @click="$emit(\'click\')"><slot /></button>',
    props: ['loading', 'type', 'text', 'size', 'circle'],
  },
  'el-icon': { template: '<span class="el-icon"><slot /></span>', props: ['size', 'color'] },
  'el-divider': { template: '<hr />', props: ['contentPosition'] },
  'el-radio-group': {
    template: '<div><slot /></div>',
    props: ['modelValue'],
  },
  'el-radio-button': {
    template: '<label><slot /></label>',
    props: ['value'],
  },
  'el-select': {
    template: '<div class="el-select"><slot /></div>',
    props: ['modelValue', 'placeholder', 'clearable', 'multiple', 'style'],
  },
  'el-option': {
    template: '<div class="el-option"><slot /></div>',
    props: ['label', 'value'],
  },
  'el-switch': {
    template: '<div class="el-switch"><slot /></div>',
    props: ['modelValue'],
  },
  'el-input-number': {
    template: '<input type="number" />',
    props: ['modelValue', 'min', 'max', 'step'],
  },
  'ArrowLeft': { template: '<span>&lt;</span>' },
}

describe('TaskEdit.vue', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
  afterEach(() => server.resetHandlers())
  afterAll(() => server.close())

  it('renders form in new mode with correct fields and save button', async () => {
    const router = createTestRouter()
    await router.push('/tasks/new')
    await router.isReady()

    const wrapper = mount(TaskEdit, {
      global: {
        plugins: [router],
        stubs,
      },
    })
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="task-form-name"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="task-form-command"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="task-form-cron"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="task-form-type"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="btn-save-task"]').exists()).toBe(true)
  })

  it('shows Create Task title in new mode', async () => {
    const router = createTestRouter()
    await router.push('/tasks/new')
    await router.isReady()

    const wrapper = mount(TaskEdit, {
      global: {
        plugins: [router],
        stubs,
      },
    })
    await flushPromises()
    await flushPromises()

    expect(wrapper.text()).toContain('Create Task')
  })

  it('shows Edit Task title in edit mode', async () => {
    const router = createTestRouter()
    await router.push('/tasks/1')
    await router.isReady()

    const wrapper = mount(TaskEdit, {
      global: {
        plugins: [router],
        stubs,
      },
    })
    await flushPromises()
    await flushPromises()

    expect(wrapper.text()).toContain('Edit Task')
  })
})
