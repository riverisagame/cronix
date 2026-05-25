#!/bin/bash
# Verify all cron macro shortcuts work correctly in WSL Debian
BIN="/tmp/cronix-mv"; cp /mnt/d/claudeprj/codex/cronix-linux-amd64 "$BIN"; chmod +x "$BIN"
rm -rf /tmp/mv-data; mkdir -p /tmp/mv-data
cat > /tmp/mv-config.yaml << 'XYZ'
server: { host: "127.0.0.1", port: 19600, graceful_timeout: 3s, webui: { enabled: true }, api: { enabled: true }, tls: { enabled: false } }
auth: { username: admin, password: "$2a$04$mkAZ9juhyjibIhyzVUEvx.FN.56UC0e2Nuibmf1i3MAqvDv3jWT4K", jwt_secret: "mv-test" }
database: { path: /tmp/mv-data/cronix.db, wal_mode: true }
executor: { pool_size: 4, output_truncate_kb: 64, max_timeout_sec: 3600 }
log: { level: info, retention_days: 1, max_records: 100 }
circuit_breaker: { failure_threshold: 5, cooldown_seconds: 10 }
notify: { retry: 0, retry_interval: 1s }
XYZ
kill $(pgrep cronix-mv) 2>/dev/null; sleep 1

"$BIN" serve -c /tmp/mv-config.yaml > /tmp/mv.log 2>&1 &
for i in $(seq 1 10); do sleep 1; if curl -sf http://127.0.0.1:19600/api/health > /dev/null 2>&1; then break; fi; done

TOKEN=$(curl -s http://127.0.0.1:19600/api/login -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"password123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
AUTH="Authorization: Bearer $TOKEN"
API=http://127.0.0.1:19600/api

PASS=0; FAIL=0
check() {
  local label="$1" expr="$2"
  # Create task with this cron
  R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
      -d "{\"name\":\"test-$label\",\"cron_expr\":\"$expr\",\"task_type\":\"shell\",\"command\":\"echo OK\",\"timeout_sec\":10}")
  if echo "$R" | grep -q '"code":0'; then
    echo "  PASS: $label ($expr)"
    PASS=$((PASS+1))
  else
    MSG=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message','unknown'))" 2>/dev/null || echo "parse_error")
    echo "  FAIL: $label ($expr) — $MSG"
    FAIL=$((FAIL+1))
  fi
}

echo "=== Cron Macro Verification ==="
echo ""

check "@every 10s" "*/10 * * * * *"
check "@every 30s" "*/30 * * * * *"
check "@hourly" "0 0 * * * *"
check "@daily" "0 0 0 * * *"
check "@weekly" "0 0 0 * * 0"
check "@monthly" "0 0 0 1 * *"
check "@every 5m" "0 */5 * * * *"
check "@every 15m" "0 */15 * * * *"
check "@every 30m" "0 */30 * * * *"

# Verify server didn't crash from loading these
sleep 2
R=$(curl -sf http://127.0.0.1:19600/api/health 2>/dev/null)
[ -n "$R" ] && echo "" && echo "PASS: Server still healthy after loading all macros" && PASS=$((PASS+1)) || { echo "FAIL: Server crashed!"; FAIL=$((FAIL+1)); }

# Check engine loaded them without errors
if grep -qi "跳过\|无效\|error\|fail" /tmp/mv.log 2>/dev/null; then
  echo "WARN: Some errors in server log:"
  grep -i "跳过\|无效\|error" /tmp/mv.log | head -5
else
  echo "PASS: No errors in server log"
  PASS=$((PASS+1))
fi

echo ""
echo "Results: $PASS passed, $FAIL failed"
kill $(pgrep cronix-mv) 2>/dev/null
[ $FAIL -eq 0 ]
