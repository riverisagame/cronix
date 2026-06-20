#!/bin/bash
cd /mnt/d/claudeprj/codex
./cronix serve &
CRONIX_PID=$!
sleep 2
cd web
export PLAYWRIGHT_BASE_URL=http://localhost:8080
npx playwright test tests/e2e/specs/tasks.spec.ts tests/e2e/specs/logs.spec.ts
kill $CRONIX_PID
