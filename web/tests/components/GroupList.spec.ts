import { describe, it, expect, beforeAll, afterEach, afterAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setupServer } from 'msw/node'
import { createRouter, createMemoryHistory } from 'vue-router'
import { ElTable, ElTableColumn } from 'element-plus'
import GroupList from '../../src/views/GroupList.vue'
import { handlers } from '../mocks/handlers'

const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/', name: 'GroupList', component: GroupList },
    { path: '/groups/new', name: 'GroupNew', component: { template: '<div>New Group</div>' } },
    { path: '/groups/:id', name: 'GroupEdit', component: { template: '<div>Edit Group</div>' } },
  ],
})

const server = setupServer(...handlers)

function mountGroupList() {
  return mount(GroupList, {
    global: {
      plugins: [router],
      components: {
        'el-table': ElTable,
        'el-table-column': ElTableColumn,
      },
      stubs: {
        'el-card': { template: '<div class="el-card"><slot name="header" /><slot /></div>' },
        'el-tag': { template: '<span class="el-tag"><slot /></span>', props: ['type', 'size'] },
        'el-button': {
          template: '<button :disabled="loading" @click="$emit(\'click\')"><slot /></button>',
          props: ['loading', 'type', 'text', 'size', 'circle'],
        },
        'el-icon': { template: '<span class="el-icon"><slot /></span>', props: ['size', 'color'] },
        'el-popconfirm': {
          template: '<div class="el-popconfirm"><slot name="reference" /><slot /></div>',
          props: ['title'],
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
        'Plus': { template: '<span>+</span>' },
        'Edit': { template: '<span>✎</span>' },
        'VideoPlay': { template: '<span>▶</span>' },
        'Tickets': { template: '<span>📋</span>' },
        'Delete': { template: '<span>✕</span>' },
        'DeleteFilled': { template: '<span>✕F</span>' },
      },
    },
  })
}

describe('GroupList.vue', () => {
  beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
  afterEach(() => server.resetHandlers())
  afterAll(() => server.close())

  it('renders group data from API', async () => {
    const wrapper = mountGroupList()
    await flushPromises()
    await flushPromises()

    expect(wrapper.text()).toContain('maintenance')
  })

  it('has new group button', async () => {
    const wrapper = mountGroupList()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="btn-new-group"]').exists()).toBe(true)
  })
})
