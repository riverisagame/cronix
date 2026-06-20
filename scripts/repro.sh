#!/bin/bash
set -e

cd /mnt/d/claudeprj/codex

echo "Cleaning up old DB..."
rm -rf data/cronix.db

echo "Starting server briefly to initialize DB..."
export CRONIX_TEST_MODE=1
timeout 3 ./cronix_linux serve || true

echo "Inserting daemon task..."
sqlite3 ./data/cronix.db <<EOF
INSERT INTO tasks (created_at, updated_at, name, task_type, run_mode, cron_expr, command, enabled, max_restart_attempts, restart_delay_sec) 
VALUES (datetime('now'), datetime('now'), 'test_daemon', 'shell', 'daemon', '', 'sleep 100', 1, 3, 1);
EOF

echo "Inserting orphaned running log..."
sqlite3 ./data/cronix.db <<EOF
INSERT INTO execution_logs (created_at, task_id, task_name, status, trigger_type, start_time)
VALUES (datetime('now'), 1, 'test_daemon', 'running', 'daemon', datetime('now'));
EOF

echo "Running server to reproduce issue (10s)..."
# redirect stderr and stdout to log file
timeout 15 ./cronix_linux serve > repro.log 2>&1 || true

echo "Grepping logs for evidence:"
grep -E 'daemon monitor|跳过本次触发' repro.log || true
