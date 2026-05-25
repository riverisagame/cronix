#!/bin/bash
BIN="/tmp/cronix-lc"; cp /mnt/d/claudeprj/codex/cronix-linux-amd64 "$BIN"; chmod +x "$BIN"
mkdir -p /tmp/lc-data
cat > /tmp/lc-config.yaml << 'XYZ'
server: { host: "127.0.0.1", port: 19400, graceful_timeout: 3s, webui: { enabled: true }, api: { enabled: true }, tls: { enabled: false } }
auth: { username: admin, password: "$2a$04$mkAZ9juhyjibIhyzVUEvx.FN.56UC0e2Nuibmf1i3MAqvDv3jWT4K", jwt_secret: "lc-test" }
database: { path: /tmp/lc-data/cronix.db, wal_mode: true }
executor: { pool_size: 4, output_truncate_kb: 64, max_timeout_sec: 3600 }
log: { level: debug, retention_days: 1, max_records: 100 }
circuit_breaker: { failure_threshold: 5, cooldown_seconds: 10 }
notify: { retry: 0, retry_interval: 1s }
XYZ

"$BIN" serve -c /tmp/lc-config.yaml > /tmp/lc.log 2>&1 &
for i in $(seq 1 10); do sleep 1; if curl -sf http://127.0.0.1:19400/api/health > /dev/null 2>&1; then break; fi; done

TOKEN=$(curl -s http://127.0.0.1:19400/api/login -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"password123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
AUTH="Authorization: Bearer $TOKEN"
API="http://127.0.0.1:19400/api"

# Create and run task
curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" -d '{"name":"lc-test","cron_expr":"","task_type":"shell","command":"echo LOG_TEST"}' > /dev/null
TID=$(curl -s "$API/tasks?search=lc-test" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)
curl -s "$API/tasks/$TID/run" -X POST -H "$AUTH" > /dev/null; sleep 2
curl -s "$API/tasks/$TID/run" -X POST -H "$AUTH" > /dev/null; sleep 2

# Verify logs exist
R=$(curl -s "$API/logs" -H "$AUTH")
TOTAL=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['total'])" 2>/dev/null)
[ "$TOTAL" -ge 2 ] && echo "PASS: $TOTAL logs exist" || { echo "FAIL: no logs"; exit 1; }

# Clear task logs
R=$(curl -s "$API/tasks/$TID/logs" -X DELETE -H "$AUTH")
echo "$R" | grep -q '"code":0' && echo "PASS: task logs cleared" || echo "FAIL: $R"

# Verify empty
R=$(curl -s "$API/tasks/$TID/logs" -H "$AUTH")
AFTER=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['total'])" 2>/dev/null)
[ "$AFTER" = "0" ] && echo "PASS: task logs now empty" || echo "FAIL: $AFTER logs remain"

# Clear ALL
R=$(curl -s "$API/logs" -X DELETE -H "$AUTH")
echo "$R" | grep -q '"code":0' && echo "PASS: all logs cleared" || echo "FAIL: $R"

kill $(pgrep cronix-lc) 2>/dev/null
