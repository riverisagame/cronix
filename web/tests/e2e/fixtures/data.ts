export const TEST_TASK = {
  name: 'e2e-test-task',
  cron_expr: '0 0 1 * * *',
  task_type: 'shell',
  command: 'echo "e2e test"',
}

export const TEST_GROUP = {
  name: 'e2e-test-group',
  mode: 'parallel',
  description: 'E2E test group',
}
