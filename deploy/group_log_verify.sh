#!/bin/bash
BIN="/tmp/cronix-gl"; cp /mnt/d/claudeprj/codex/cronix-linux-amd64 "$BIN"; chmod +x "$BIN"
rm -rf /tmp/gl-data; mkdir -p /tmp/gl-data
cat > /tmp/gl-config.yaml << 'XYZ'
server: { host: "127.0.0.1", port: 19500, graceful_timeout: 3s, webui: { enabled: true }, api: { enabled: true }, tls: { enabled: false } }
auth: { username: admin, password: "$2a$04$mkAZ9juhyjibIhyzVUEvx.FN.56UC0e2Nuibmf1i3MAqvDv3jWT4K", jwt_secret: "gl-test" }
database: { path: /tmp/gl-data/cronix.db, wal_mode: true }
executor: { pool_size: 4, output_truncate_kb: 64, max_timeout_sec: 3600 }
log: { level: debug, retention_days: 1, max_records: 100 }
circuit_breaker: { failure_threshold: 5, cooldown_seconds: 10 }
notify: { retry: 0, retry_interval: 1s }
XYZ
kill $(pgrep cronix-gl) 2>/dev/null; sleep 1

"$BIN" serve -c /tmp/gl-config.yaml > /tmp/gl.log 2>&1 &
for i in $(seq 1 10); do sleep 1; if curl -sf http://127.0.0.1:19500/api/health > /dev/null 2>&1; then break; fi; done

TOKEN=$(curl -s http://127.0.0.1:19500/api/login -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"password123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
AUTH="Authorization: Bearer $TOKEN"
API=http://127.0.0.1:19500/api

curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" -d '{"name":"gl-t1","cron_expr":"","task_type":"shell","command":"echo OK","timeout_sec":10}' > /dev/null
curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" -d '{"name":"gl-t2","cron_expr":"","task_type":"shell","command":"echo OK2","timeout_sec":10}' > /dev/null
T1=$(curl -s "$API/tasks?search=gl-t1" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)
T2=$(curl -s "$API/tasks?search=gl-t2" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)

R=$(curl -s "$API/groups" -X POST -H "Content-Type: application/json" -H "$AUTH" -d '{"name":"gl-group","mode":"parallel"}')
GID=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
curl -s "$API/groups/$GID/members" -X PUT -H "Content-Type: application/json" -H "$AUTH" -d "{\"task_ids\":[$T1,$T2]}" > /dev/null
curl -s "$API/groups/$GID/run" -X POST -H "$AUTH" > /dev/null
sleep 3

R=$(curl -s "$API/groups/$GID/logs" -H "$AUTH")
TOTAL=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['total'])" 2>/dev/null)
STATUS=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['status'])" 2>/dev/null)
OK=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['success_count'])" 2>/dev/null)

[ "$TOTAL" -ge 1 ] && echo "PASS: $TOTAL group log recorded" || echo "FAIL: no logs"
[ "$STATUS" = "success" ] && echo "PASS: status=$STATUS" || echo "FAIL: status=$STATUS"
[ "$OK" = "2" ] && echo "PASS: success_count=$OK/2" || echo "FAIL: success_count=$OK"

kill $(pgrep cronix-gl) 2>/dev/null
