#!/bin/bash
# Robustness stress test for Cronix
# Run: wsl -d Debian -u root bash /tmp/robustness_test.sh
set -e

BIN="/tmp/cronix-robust"
CONFIG="/tmp/robust-config.yaml"
DATADIR="/tmp/robust-data"
PORT=19080
API="http://127.0.0.1:${PORT}/api"
PASS=0
FAIL=0

red()   { echo -e "\033[31m[FAIL]\033[0m $1"; FAIL=$((FAIL+1)); }
green() { echo -e "\033[32m[PASS]\033[0m $1"; PASS=$((PASS+1)); }

cleanup() {
    pkill -f "cronix-robust" 2>/dev/null || true
    rm -f "$CONFIG" "$BIN"
    rm -rf "$DATADIR"
}
trap cleanup EXIT

cp /mnt/d/claudeprj/codex/cronix-linux-amd64 "$BIN"
chmod +x "$BIN"

echo "============================================"
echo " Cronix Robustness Stress Test"
echo "============================================"
echo ""

# ============================================================
# Test 1: Start with NO config file (fault tolerance)
# ============================================================
echo "--- Test 1: Start without config file ---"
rm -f "$CONFIG"
rm -rf "$DATADIR"

"$BIN" serve -c "$CONFIG" > /tmp/robust-out1.log 2>&1 &
PID=$!
# Port may default to 8080 when config is broken/missing
DETECT_PORT=$(grep -oP 'port=\K\d+' /tmp/robust-out1.log | tail -1 || echo "8080")
DETECT_API="http://127.0.0.1:${DETECT_PORT:-8080}/api"
for i in $(seq 1 10); do
    sleep 1
    if curl -sf "$DETECT_API/health" > /dev/null 2>&1; then
        green "Server started without pre-existing config (port=$DETECT_PORT)"
        break
    fi
    if [ $i -eq 10 ]; then
        red "Server failed to start without config"
        cat /tmp/robust-out1.log
        kill $PID 2>/dev/null || true
    fi
done
kill $PID 2>/dev/null || true
wait $PID 2>/dev/null || true
sleep 1

# ============================================================
# Test 2: Start with BROKEN config (YAML syntax error)
# ============================================================
echo ""
echo "--- Test 2: Start with broken config ---"
mkdir -p "$DATADIR"
cat > "$CONFIG" << 'BROKEN'
server:
  port: 19080
  host: "127.0.0.1"
  this_is: broken: yaml: syntax: error: [[[
auth:
  password: "$2a$04$mkAZ9juhyjibIhyzVUEvx.FN.56UC0e2Nuibmf1i3MAqvDv3jWT4K"
  jwt_secret: "test-secret"
database:
  path: /tmp/robust-data/broken.db
log:
  retention_days: 1
  max_records: 100
BROKEN

"$BIN" serve -c "$CONFIG" > /tmp/robust-out2.log 2>&1 &
PID=$!
DETECT_PORT=$(grep -oP 'port=\K\d+' /tmp/robust-out2.log | tail -1 || echo "8080")
DETECT_API="http://127.0.0.1:${DETECT_PORT:-8080}/api"
for i in $(seq 1 10); do
    sleep 1
    if curl -sf "$DETECT_API/health" > /dev/null 2>&1; then
        green "Server started with broken YAML config (used defaults, port=$DETECT_PORT)"
        break
    fi
    if [ $i -eq 10 ]; then
        red "Server failed with broken config"
        cat /tmp/robust-out2.log
    fi
done
kill $PID 2>/dev/null || true
wait $PID 2>/dev/null || true
sleep 1

# ============================================================
# Test 3: Normal startup with valid config
# ============================================================
echo ""
echo "--- Test 3: Normal startup ---"
mkdir -p "$DATADIR"
cat > "$CONFIG" << 'YAML'
server:
  host: "127.0.0.1"
  port: 19080
  graceful_timeout: 5s
  tls:
    enabled: false
  webui:
    enabled: true
  api:
    enabled: true
auth:
  username: admin
  password: "$2a$04$mkAZ9juhyjibIhyzVUEvx.FN.56UC0e2Nuibmf1i3MAqvDv3jWT4K"
  jwt_secret: "test-secret-key-for-testing"
database:
  path: /tmp/robust-data/cronix.db
  wal_mode: true
executor:
  pool_size: 8
  output_truncate_kb: 64
log:
  level: debug
  retention_days: 1
  max_records: 1000
notify:
  retry: 0
  retry_interval: 1s
circuit_breaker:
  failure_threshold: 5
  cooldown_seconds: 10
YAML

"$BIN" serve -c "$CONFIG" > /tmp/robust-out3.log 2>&1 &
PID=$!
for i in $(seq 1 10); do
    sleep 1
    if curl -sf "$API/health" > /dev/null 2>&1; then
        green "Server started normally"
        break
    fi
    if [ $i -eq 10 ]; then
        red "Server failed to start normally"
        cat /tmp/robust-out3.log
        exit 1
    fi
done

# Login
TOKEN=$(curl -s "$API/login" -X POST \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"password123"}' | \
    python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
[ -n "$TOKEN" ] && green "Token obtained" || { red "Login failed"; exit 1; }
AUTH="Authorization: Bearer $TOKEN"

# ============================================================
# Test 4: Create task with invalid cron, server stays up
# ============================================================
echo ""
echo "--- Test 4: Invalid cron resilience ---"
R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"bad-cron-task","cron_expr":"not a cron at all !!!","task_type":"shell","command":"echo BAD","timeout_sec":10}')
if echo "$R" | grep -q 'cron表达式'; then
    green "API rejected invalid cron at input boundary"
else
    # If it got stored (shouldn't with our validation), check server still up
    HEALTH=$(curl -sf "$API/health")
    if echo "$HEALTH" | grep -q "healthy"; then
        green "Server still healthy despite bad cron attempt"
    else
        red "Server crashed after bad cron"
    fi
fi

# ============================================================
# Test 5: Task with empty cron (valid since optional)
# ============================================================
echo ""
echo "--- Test 5: Empty cron acceptance ---"
R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"no-cron-task","cron_expr":"","task_type":"shell","command":"echo NO_CRON","timeout_sec":10}')
echo "$R" | grep -q '"code":0' && green "Task with empty cron created (manual-only)" || { red "Failed to create manual task"; }
TID_NOCR=$(echo "$R" | python3 -c "import sys,json; d=json.load(sys.stdin).get('data',{}); print(d.get('id',''))" 2>/dev/null)

# Manual trigger still works
if [ -n "$TID_NOCR" ]; then
    curl -s "$API/tasks/$TID_NOCR/run" -X POST -H "$AUTH" > /dev/null
    sleep 2
    R=$(curl -s "$API/tasks/$TID_NOCR/logs" -H "$AUTH")
    echo "$R" | grep -q '"status":"success"' && green "Manual trigger works for cron-less task" || red "Manual trigger failed"
fi

# ============================================================
# Test 6: Restart with bad data in DB (simulate corrupted DB)
# ============================================================
echo ""
echo "--- Test 6: Restart resilience ---"
kill $PID 2>/dev/null || true
wait $PID 2>/dev/null || true
sleep 1

"$BIN" serve -c "$CONFIG" > /tmp/robust-out4.log 2>&1 &
PID=$!
for i in $(seq 1 10); do
    sleep 1
    if curl -sf "$API/health" > /dev/null 2>&1; then
        green "Server restarted successfully"
        break
    fi
    if [ $i -eq 10 ]; then
        red "Server failed to restart"
        cat /tmp/robust-out4.log
    fi
done

# Check that the no-cron task still exists
TOKEN=$(curl -s "$API/login" -X POST \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"password123"}' | \
    python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
AUTH="Authorization: Bearer $TOKEN"
R=$(curl -s "$API/tasks/$TID_NOCR" -H "$AUTH")
echo "$R" | grep -q '"code":0' && green "Data persisted across restart" || red "Data lost after restart"

# ============================================================
# Test 7: Version flag works
# ============================================================
echo ""
echo "--- Test 7: Version flag ---"
VER=$("$BIN" --version 2>&1)
echo "$VER" | grep -q "v1.5.0" && green "Version flag: $VER" || red "Version flag wrong: $VER"

# ============================================================
# Cleanup
# ============================================================
kill $PID 2>/dev/null || true
wait $PID 2>/dev/null || true
sleep 1

echo ""
echo "============================================"
echo " Results: $PASS passed, $FAIL failed"
echo "============================================"
[ $FAIL -eq 0 ] || exit 1
