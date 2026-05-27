import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest'
import { setupServer } from 'msw/node'
import { handlers } from '../mocks/handlers'
import { authAPI, taskAPI, logAPI, dashboardAPI, settingsAPI, groupAPI } from '../../src/api/index'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

describe('authAPI', () => {
  it('login sends POST with username and password', async () => {
    const res = await authAPI.login('admin', 'admin')
    expect(res.data.code).toBe(0)
    expect(res.data.data.token).toBe('mock-jwt-token')
  })
})

describe('taskAPI', () => {
  it('list sends GET with query params', async () => {
    const res = await taskAPI.list({ page: 1, page_size: 20, search: 'backup' })
    expect(res.data.code).toBe(0)
    expect(res.data.data.items).toHaveLength(1)
  })

  it('create sends POST with task data', async () => {
    const res = await taskAPI.create({ name: 'new-task', cron_expr: '* * * * * *', task_type: 'shell', command: 'echo hi' })
    expect(res.data.code).toBe(0)
    expect(res.data.data.id).toBe(1)
  })

  it('get sends GET with id in path', async () => {
    const res = await taskAPI.get(1)
    expect(res.data.code).toBe(0)
  })

  it('update sends PUT with id and data', async () => {
    const res = await taskAPI.update(1, { name: 'updated' })
    expect(res.data.code).toBe(0)
  })

  it('delete sends DELETE with id', async () => {
    const res = await taskAPI.delete(1)
    expect(res.data.code).toBe(0)
  })

  it('run sends POST to run endpoint', async () => {
    const res = await taskAPI.run(1)
    expect(res.data.code).toBe(0)
  })

  it('getLogs sends GET with pagination', async () => {
    const res = await taskAPI.getLogs(1, { page: 1, page_size: 50 })
    expect(res.data.code).toBe(0)
  })

  it('getDeps sends GET to deps endpoint', async () => {
    const res = await taskAPI.getDeps(1)
    expect(res.data.code).toBe(0)
  })

  it('updateDeps sends PUT with dep_ids', async () => {
    const res = await taskAPI.updateDeps(1, [2, 3])
    expect(res.data.code).toBe(0)
  })
})

describe('logAPI', () => {
  it('list fetches logs', async () => {
    const res = await logAPI.list({ page: 1, page_size: 20 })
    expect(res.data.code).toBe(0)
  })

  it('clearAll sends DELETE to /logs', async () => {
    const res = await logAPI.clearAll()
    expect(res.data.code).toBe(0)
  })

  it('deleteLog sends DELETE with id', async () => {
    const res = await logAPI.deleteLog(1)
    expect(res.data.code).toBe(0)
  })

  it('getLog fetches single log', async () => {
    const res = await logAPI.getLog(1)
    expect(res.data.code).toBe(0)
  })

  it('clearTask sends DELETE to task logs', async () => {
    const res = await logAPI.clearTask(1)
    expect(res.data.code).toBe(0)
  })

  it('clearGroup sends DELETE to group logs', async () => {
    const res = await logAPI.clearGroup(1)
    expect(res.data.code).toBe(0)
  })

  it('exportLogs with json returns json responseType', async () => {
    const res = await logAPI.exportLogs({ format: 'json' })
    expect(res.data).toBeDefined()
  })
})

describe('dashboardAPI', () => {
  it('stats fetches dashboard data', async () => {
    const res = await dashboardAPI.stats()
    expect(res.data.code).toBe(0)
    expect(res.data.data.total_tasks).toBe(12)
  })
})

describe('settingsAPI', () => {
  it('get fetches settings', async () => {
    const res = await settingsAPI.get()
    expect(res.data.code).toBe(0)
    expect(res.data.data.pool_size).toBe(32)
  })

  it('update sends PUT with settings data', async () => {
    const res = await settingsAPI.update({ pool_size: 64 })
    expect(res.data.code).toBe(0)
  })
})

describe('groupAPI', () => {
  it('list fetches groups', async () => {
    const res = await groupAPI.list()
    expect(res.data.code).toBe(0)
    expect(res.data.data).toHaveLength(1)
  })

  it('create sends POST with group data', async () => {
    const res = await groupAPI.create({ name: 'new-group', mode: 'parallel' })
    expect(res.data.code).toBe(0)
  })

  it('get fetches single group', async () => {
    const res = await groupAPI.get(1)
    expect(res.data.code).toBe(0)
  })

  it('update sends PUT with id and data', async () => {
    const res = await groupAPI.update(1, { name: 'updated-group' })
    expect(res.data.code).toBe(0)
  })

  it('delete sends DELETE with id', async () => {
    const res = await groupAPI.delete(1)
    expect(res.data.code).toBe(0)
  })

  it('setMembers sends PUT with task_ids', async () => {
    const res = await groupAPI.setMembers(1, [1, 2, 3])
    expect(res.data.code).toBe(0)
  })

  it('run sends POST to group run endpoint', async () => {
    const res = await groupAPI.run(1)
    expect(res.data.code).toBe(0)
  })

  it('getLogs fetches group logs', async () => {
    const res = await groupAPI.getLogs(1, { page: 1 })
    expect(res.data.code).toBe(0)
  })
})
