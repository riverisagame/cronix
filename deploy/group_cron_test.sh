#!/bin/bash
# Quick test: verify group cron actually fires
BIN="/tmp/cronix-gc"
CONFIG="/tmp/gc-config.yaml"
DATADIR="/tmp/gc-data"
PORT=19300
API="http://127.0.0.1:${PORT}/api"
PASS=0; FAIL=0
pass() { echo -e "\033[32m  [PASS]\033[0m $1"; PASS=$((PASS+1)); }
fail() { echo -e "\033[31m  [FAIL]\033[0m $1"; FAIL=$((FAIL+1)); }

cleanup() { kill $(pgrep cronix-gc) 2>/dev/null; sleep 1; rm -rf "$DATADIR" "$CONFIG" "$BIN" 2>/dev/null; }
trap cleanup EXIT
kill $(pgrep cronix-gc) 2>/dev/null; sleep 1

cp /mnt/d/claudeprj/codex/cronix-linux-amd64 "$BIN" 2>/dev/null || { cp /d/claudeprj/codex/cronix-linux-amd64 "$BIN"; }
chmod +x "$BIN"
mkdir -p "$DATADIR"

cat > "$CONFIG" << YAML
server: { host: "127.0.0.1", port: 19300, graceful_timeout: 3s, webui: { enabled: true }, api: { enabled: true }, tls: { enabled: false } }
auth: { username: admin, password: "\$2a\$04\$mkAZ9juhyjibIhyzVUEvx.FN.56UC0e2Nuibmf1i3MAqvDv3jWT4K", jwt_secret: "gc-test" }
database: { path: /tmp/gc-data/cronix.db, wal_mode: true }
executor: { pool_size: 4, output_truncate_kb: 64, max_timeout_sec: 3600 }
log: { level: debug, retention_days: 1, max_records: 100 }
circuit_breaker: { failure_threshold: 5, cooldown_seconds: 10 }
notify: { retry: 0, retry_interval: 1s }
YAML

echo "=== Group Cron Verification ==="
echo ""

"$BIN" serve -c "$CONFIG" > /tmp/gc-server.log 2>&1 &
PID=$!
for i in $(seq 1 10); do sleep 1; if curl -sf "$API/health" > /dev/null 2>&1; then break; fi; if [ $i -eq 10 ]; then fail "Server start"; exit 1; fi; done
pass "Server started"

TOKEN=$(curl -s "$API/login" -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"password123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
AUTH="Authorization: Bearer $TOKEN"

# Create tasks without individual cron
curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"gc-task1","cron_expr":"","task_type":"shell","command":"echo GROUP_CRON_OK","timeout_sec":10}' > /dev/null
curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"gc-task2","cron_expr":"","task_type":"shell","command":"echo GROUP_CRON_OK2","timeout_sec":10}' > /dev/null
T1=$(curl -s "$API/tasks?search=gc-task1" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)
T2=$(curl -s "$API/tasks?search=gc-task2" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)
pass "Tasks created (no individual cron)"

# Create group with cron (every 3 seconds - fast for testing)
R=$(curl -s "$API/groups" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d "{\"name\":\"auto-group\",\"mode\":\"parallel\",\"cron_expr\":\"*/3 * * * * *\",\"enabled\":true}")
GID=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
curl -s "$API/groups/$GID/members" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d "{\"task_ids\":[$T1,$T2]}" > /dev/null
pass "Group created with cron '*/3 * * * * *'"

# Wait for group cron to fire (should trigger within ~6-10 seconds)
echo "  Waiting for group cron (every 3s)..."
sleep 12

# Check server log for group trigger evidence
if grep -q "running group" /tmp/gc-server.log 2>/dev/null; then
    pass "Group cron fired (running group in logs)"
else
    fail "Group cron did NOT fire - not found in logs"
fi

# Check if tasks were executed
FOUND=0
for tid in $T1 $T2; do
    R=$(curl -s "$API/tasks/$tid/logs" -H "$AUTH")
    if echo "$R" | grep -q '"status":"success"'; then
        pass "Task $tid executed by group cron"
        FOUND=$((FOUND+1))
    fi
done
[ $FOUND -gt 1 ] && pass "Both tasks executed by group cron ($FOUND executions)" || fail "Not enough executions: $FOUND"

TIMES=$(grep -c "running group" /tmp/gc-server.log 2>/dev/null || echo 0)
echo "  Group cron fired ~$TIMES times"

kill $PID 2>/dev/null; wait $PID 2>/dev/null; sleep 1
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ $FAIL -eq 0 ]
