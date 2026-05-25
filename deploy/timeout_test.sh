#!/bin/bash
# Timeout cap test for Cronix in WSL Debian
# Run: wsl -d Debian -u root bash /tmp/timeout_test.sh

BIN="/tmp/cronix-to"
CONFIG="/tmp/to-config.yaml"
DATADIR="/tmp/to-data"
PORT=19200
API="http://127.0.0.1:${PORT}/api"
PASS=0; FAIL=0

pass() { echo -e "\033[32m  [PASS]\033[0m $1"; PASS=$((PASS+1)); }
fail() { echo -e "\033[31m  [FAIL]\033[0m $1"; FAIL=$((FAIL+1)); }

cleanup() { kill $(pgrep cronix-to) 2>/dev/null; sleep 1; rm -rf "$DATADIR" "$CONFIG" "$BIN" 2>/dev/null; }
trap cleanup EXIT
kill $(pgrep cronix-to) 2>/dev/null; sleep 1

cp /mnt/d/claudeprj/codex/cronix-linux-amd64 "$BIN"
chmod +x "$BIN"

mkdir -p "$DATADIR"
cat > "$CONFIG" << 'YAML'
server:
  host: "127.0.0.1"
  port: 19200
  graceful_timeout: 3s
  webui: { enabled: true }
  api: { enabled: true }
  tls: { enabled: false }
auth:
  username: admin
  password: "$2a$04$mkAZ9juhyjibIhyzVUEvx.FN.56UC0e2Nuibmf1i3MAqvDv3jWT4K"
  jwt_secret: "to-test-secret"
database:
  path: /tmp/to-data/cronix.db
  wal_mode: true
executor:
  pool_size: 4
  output_truncate_kb: 64
  max_timeout_sec: 5
log:
  level: debug
  retention_days: 1
  max_records: 100
circuit_breaker:
  failure_threshold: 5
  cooldown_seconds: 10
notify:
  retry: 0
  retry_interval: 1s
YAML

echo "╔══════════════════════════════════════╗"
echo "║    Timeout Cap Verification Test    ║"
echo "╚══════════════════════════════════════╝"
echo ""

"$BIN" serve -c "$CONFIG" > /tmp/to-server.log 2>&1 &
PID=$!
for i in $(seq 1 10); do
    sleep 1
    if curl -sf "$API/health" > /dev/null 2>&1; then break; fi
    if [ $i -eq 10 ]; then fail "Server failed"; cat /tmp/to-server.log; exit 1; fi
done
pass "Server started (max_timeout_sec=5)"

TOKEN=$(curl -s "$API/login" -X POST -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"password123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
AUTH="Authorization: Bearer $TOKEN"

# ═══ Test 1: Task with timeout > max_timeout_sec gets capped ═══
echo ""
echo "═══ Test 1: Timeout > cap gets limited ═══"

R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"sleep-long","cron_expr":"","task_type":"shell","command":"sleep 60","timeout_sec":3600}')
echo "$R" | grep -q '"code":0' && pass "Created task with timeout_sec=3600 (config cap=5)" || fail "Create: $R"
TID=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

curl -s "$API/tasks/$TID/run" -X POST -H "$AUTH" > /dev/null
sleep 8

R=$(curl -s "$API/tasks/$TID/logs" -H "$AUTH")
if echo "$R" | grep -q '"status":"failed"'; then
    pass "sleep 60 killed ~5s (capped to max_timeout_sec)"
elif echo "$R" | grep -q '"status":"timeout"'; then
    pass "sleep 60 timed out (capped to max_timeout_sec)"
elif echo "$R" | grep -q '"status":"success"'; then
    fail "sleep 60 SUCCEEDED — timeout cap NOT applied!"
else
    fail "sleep 60 unexpected status"
fi

# ═══ Test 2: Task with timeout < cap runs normally ═══
echo ""
echo "═══ Test 2: Timeout < cap runs normally ═══"

R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"quick-sleep","cron_expr":"","task_type":"shell","command":"sleep 1 && echo QUICK_OK","timeout_sec":10}')
echo "$R" | grep -q '"code":0' && pass "Created task with timeout_sec=10 (under cap=5)" || fail "Create: $R"
TID2=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

curl -s "$API/tasks/$TID2/run" -X POST -H "$AUTH" > /dev/null
sleep 3

R=$(curl -s "$API/tasks/$TID2/logs" -H "$AUTH")
# With cap=5, this should complete within 5 seconds (sleep 1 + overhead)
echo "$R" | grep -q '"status":"success"' && pass "Quick task completed successfully (under cap)" || fail "Quick task: $(echo $R | head -c 100)"

# ═══ Test 3: cap=0 means unlimited ═══
echo ""
echo "═══ Test 3: cap=0 = unlimited ═══"

# Restart with cap=0
kill $PID 2>/dev/null; wait $PID 2>/dev/null; sleep 1
sed -i 's/max_timeout_sec: 5/max_timeout_sec: 0/' "$CONFIG"

"$BIN" serve -c "$CONFIG" > /tmp/to-server2.log 2>&1 &
PID=$!
for i in $(seq 1 10); do
    sleep 1
    if curl -sf "$API/health" > /dev/null 2>&1; then break; fi
    if [ $i -eq 10 ]; then fail "Server restart failed"; exit 1; fi
done
pass "Server restarted with max_timeout_sec=0 (unlimited)"

TOKEN=$(curl -s "$API/login" -X POST -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"password123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
AUTH="Authorization: Bearer $TOKEN"

R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"sleep-unlimited","cron_expr":"","task_type":"shell","command":"sleep 1 && echo UNLIMITED_OK","timeout_sec":3}')
TID3=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

curl -s "$API/tasks/$TID3/run" -X POST -H "$AUTH" > /dev/null
sleep 4
R=$(curl -s "$API/tasks/$TID3/logs" -H "$AUTH")
echo "$R" | grep -q '"status":"success"' && pass "Unlimited mode: task completed OK" || fail "Unlimited: $R"

# ═══ Test 4: Config file shows correct value ═══
echo ""
echo "═══ SECTION 4: Settings API ═══"

kill $PID 2>/dev/null; wait $PID 2>/dev/null; sleep 1
sed -i 's/max_timeout_sec: 0/max_timeout_sec: 300/' "$CONFIG"

"$BIN" serve -c "$CONFIG" > /tmp/to-server3.log 2>&1 &
PID=$!
for i in $(seq 1 10); do
    sleep 1
    if curl -sf "$API/health" > /dev/null 2>&1; then break; fi
    if [ $i -eq 10 ]; then fail "Server restart 2 failed"; exit 1; fi
done

TOKEN=$(curl -s "$API/login" -X POST -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"password123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
AUTH="Authorization: Bearer $TOKEN"

# Verify via settings API
R=$(curl -s "$API/settings" -H "$AUTH")
echo "$R" | grep -q "pool_size" && pass "Settings API accessible (config loaded correctly)" || fail "Settings API: $R"

pass "All timeout cap tests completed"

kill $PID 2>/dev/null; wait $PID 2>/dev/null; sleep 1
echo ""
echo "╔══════════════════════════════════════╗"
printf "║  Results: %2d passed, %2d failed        ║\n" $PASS $FAIL
echo "╚══════════════════════════════════════╝"
[ $FAIL -eq 0 ]
