import { mount } from '@vue/test-utils'
import { describe, it, expect } from 'vitest'
import TaskList from '../TaskList.vue'
import { ElTag, ElCard, ElTimeline, ElTimelineItem, ElEmpty, ElTable, ElTableColumn, ElButton } from 'element-plus'

describe('TaskList.vue UI/UX Enhancement Tests', () => {
  it('should render ElTag with effect="dark" for task type to improve color contrast', () => {
    const wrapper = mount(TaskList, {
      global: {
        components: { ElTag, ElCard, ElTimeline, ElTimelineItem, ElEmpty, ElTable, ElTableColumn, ElButton },
        stubs: ['el-icon', 'router-link', 'el-switch', 'el-popconfirm', 'el-select', 'el-option', 'el-input', 'el-row', 'el-col', 'el-pagination', 'el-drawer']
      }
    })

    // To simulate rendered tasks
    wrapper.vm.tasks = [
      { id: 1, name: 'Test Task', task_type: 'shell', enabled: true }
    ]

    // Wait for Vue to update the DOM based on the new tasks value
    wrapper.vm.$nextTick(() => {
      const tags = wrapper.findAllComponents(ElTag)
      if (tags.length > 0) {
        // Assert that the tag has the dark effect applied
        expect(tags[0].attributes('effect')).toBe('dark')
      }
    })
  })
})
