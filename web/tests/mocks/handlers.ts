import { http, HttpResponse } from 'msw'

const BASE = '/api'

// Shared sample data
export const sampleTask = {
  id: 1,
  name: 'backup-db',
  cron_expr: '0 30 2 * * *',
  task_type: 'shell',
  command: 'pg_dump mydb',
  enabled: true,
  description: 'Daily database backup',
  group_name: 'maintenance',
  group_id: 1,
  depends_on_ids: [],
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

export const sampleGroup = {
  id: 1,
  name: 'maintenance',
  description: 'Maintenance tasks',
  mode: 'parallel',
  cron_expr: '',
  task_ids: [1, 2],
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

export const sampleLog = {
  id: 1,
  task_id: 1,
  task_name: 'backup-db',
  group_name: '',
  status: 'success',
  trigger_type: 'schedule',
  exit_code: 0,
  output: 'Backup completed successfully',
  error_msg: '',
  start_time: '2026-01-15T08:30:00+08:00',
  end_time: '2026-01-15T08:30:05+08:00',
}

export const sampleDashboardStats = {
  total_tasks: 12,
  enabled_tasks: 10,
  today_total: 48,
  today_success: 45,
  today_failed: 3,
}

export const sampleSettings = {
  pool_size: 32,
  max_concurrency: 16,
  log_retention_days: 30,
  alert_webhook_url: '',
}

function ok<T>(data: T) {
  return HttpResponse.json({ code: 0, data })
}

function paginated<T>(items: T[], total?: number) {
  return HttpResponse.json({ code: 0, data: { items, total: total ?? items.length } })
}

export const handlers = [
  // ---- Auth ----
  http.post(`${BASE}/login`, async ({ request }) => {
    const body = await request.json() as { username: string; password: string }
    if (body.username === 'admin' && body.password === 'admin') {
      return ok({ token: 'mock-jwt-token' })
    }
    return HttpResponse.json({ code: 1, message: 'Invalid credentials' }, { status: 401 })
  }),

  // ---- Tasks ----
  http.get(`${BASE}/tasks`, () => paginated([sampleTask])),
  http.post(`${BASE}/tasks`, () => ok(sampleTask)),
  http.get(`${BASE}/tasks/:id`, () => ok(sampleTask)),
  http.put(`${BASE}/tasks/:id`, () => ok(sampleTask)),
  http.delete(`${BASE}/tasks/:id`, () => ok(null)),
  http.post(`${BASE}/tasks/:id/run`, () => ok({ message: 'Triggered' })),
  http.get(`${BASE}/tasks/:id/logs`, () => paginated([sampleLog])),
  http.get(`${BASE}/tasks/:id/deps`, () => ok([])),
  http.put(`${BASE}/tasks/:id/deps`, () => ok(null)),

  // ---- Groups ----
  http.get(`${BASE}/groups`, () => ok([sampleGroup])),
  http.post(`${BASE}/groups`, () => ok(sampleGroup)),
  http.get(`${BASE}/groups/:id`, () => ok(sampleGroup)),
  http.put(`${BASE}/groups/:id`, () => ok(sampleGroup)),
  http.delete(`${BASE}/groups/:id`, () => ok(null)),
  http.put(`${BASE}/groups/:id/members`, () => ok(null)),
  http.post(`${BASE}/groups/:id/run`, () => ok({ message: 'Group triggered' })),
  http.get(`${BASE}/groups/:id/logs`, () => paginated([sampleLog])),

  // ---- Logs ----
  http.get(`${BASE}/logs/export`, () => new HttpResponse('id,name,status\n1,backup-db,success', {
    headers: { 'Content-Type': 'text/csv' },
  })),
  http.get(`${BASE}/logs`, () => paginated([sampleLog])),
  http.delete(`${BASE}/logs`, () => ok(null)),
  http.delete(`${BASE}/logs/:id`, () => ok(null)),
  http.get(`${BASE}/logs/:id`, () => ok(sampleLog)),
  http.delete(`${BASE}/tasks/:id/logs`, () => ok(null)),
  http.delete(`${BASE}/groups/:id/logs`, () => ok(null)),

  // ---- Dashboard ----
  http.get(`${BASE}/dashboard/stats`, () => ok(sampleDashboardStats)),

  // ---- Settings ----
  http.get(`${BASE}/settings`, () => ok(sampleSettings)),
  http.put(`${BASE}/settings`, () => ok(sampleSettings)),

  // ---- Health ----
  http.get(`${BASE}/health`, () => HttpResponse.json({ status: 'healthy', version: '1.7.0' })),
]
