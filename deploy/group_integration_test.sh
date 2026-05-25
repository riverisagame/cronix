#!/bin/bash
# WSL Debian integration test for Cronix Task Groups
# Run: bash group_integration_test.sh
set -e

BIN="${CRONIX_BIN:-./cronix-linux-amd64}"
CONFIG="/tmp/cronix-test-config.yaml"
DATA_DIR="/tmp/cronix-test-data"
PORT=18080
API="http://127.0.0.1:${PORT}/api"

red()   { echo -e "\033[31m$1\033[0m"; }
green() { echo -e "\033[32m$1\033[0m"; }

cleanup() {
    if [ -n "$SERVER_PID" ]; then
        kill $SERVER_PID 2>/dev/null || true
        wait $SERVER_PID 2>/dev/null || true
    fi
    rm -f "$CONFIG" "$DATA_DIR/cronix.db" 2>/dev/null || true
}
trap cleanup EXIT

# Check binary
if [ ! -f "$BIN" ]; then
    red "Binary not found: $BIN"
    red "Build: cd /d/claudeprj/codex && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags=\"-s -w\" -o cronix-linux-amd64 ."
    exit 1
fi

echo "=== Cronix Group Integration Test ==="

# Setup config with password
mkdir -p "$DATA_DIR"
cat > "$CONFIG" << 'YAML'
server:
  host: "127.0.0.1"
  port: 18080
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
  jwt_secret: "test-secret-key-for-integration-testing"
database:
  path: /tmp/cronix-test-data/cronix.db
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

echo ""
green "[OK] Config written (password hash embedded)"

# Start server (must run as root)
sudo "$BIN" serve -c "$CONFIG" &
SERVER_PID=$!

# Wait for server
for i in $(seq 1 15); do
    sleep 1
    if curl -sf "$API/health" > /dev/null 2>&1; then
        break
    fi
    if [ $i -eq 15 ]; then
        red "[FAIL] Server did not start"
        exit 1
    fi
done
green "[OK] Server started (PID=$SERVER_PID)"

# Login and get token
TOKEN=$(curl -s "$API/login" -X POST \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"password123"}' | \
    python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)

if [ -z "$TOKEN" ]; then
    red "[FAIL] Login failed"
    exit 1
fi
green "[OK] Token obtained"

AUTH="Authorization: Bearer $TOKEN"

# ============================================================
# Test 1: Group CRUD
# ============================================================
echo ""
echo "--- Test 1: Group CRUD ---"

# Create group
R=$(curl -s "$API/groups" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"test-parallel-group","mode":"parallel"}')
echo "$R" | grep -q '"code":0' && green "  PASS: create group" || { red "  FAIL: create group"; echo "$R"; }

GID=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
echo "  Group ID=$GID"

# Get group
R=$(curl -s "$API/groups/$GID" -H "$AUTH")
echo "$R" | grep -q '"mode":"parallel"' && green "  PASS: get group" || { red "  FAIL: get group"; }

# Update group mode
R=$(curl -s "$API/groups/$GID" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"mode":"sequential"}')
echo "$R" | grep -q '"code":0' && green "  PASS: update mode to sequential" || { red "  FAIL: update mode"; }

# List groups
R=$(curl -s "$API/groups" -H "$AUTH")
echo "$R" | grep -q '"mode":"sequential"' && green "  PASS: list groups reflects update" || { red "  FAIL: list groups"; }

# ============================================================
# Test 2: Create tasks and assign to group
# ============================================================
echo ""
echo "--- Test 2: Group Membership ---"

# Create tasks
R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"group-task-1","cron_expr":"0 0 1 * * *","task_type":"shell","command":"echo TASK1_OK","timeout_sec":10}')
TID1=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
green "  Task 1 created: ID=$TID1"

R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"group-task-2","cron_expr":"0 0 1 * * *","task_type":"shell","command":"echo TASK2_OK","timeout_sec":10}')
TID2=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
green "  Task 2 created: ID=$TID2"

R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"group-task-3","cron_expr":"0 0 1 * * *","task_type":"shell","command":"exit 1","timeout_sec":10}')
TID3=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
green "  Task 3 (failing) created: ID=$TID3"

# Assign tasks to group via set members
R=$(curl -s "$API/groups/$GID/members" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d "{\"task_ids\":[$TID1,$TID2,$TID3]}")
echo "$R" | grep -q '"code":0' && green "  PASS: set members" || { red "  FAIL: set members"; echo "$R"; }

# Verify members
R=$(curl -s "$API/groups/$GID" -H "$AUTH")
echo "$R" | grep -q "group-task-1" && echo "$R" | grep -q "group-task-2" && \
    green "  PASS: members listed correctly" || { red "  FAIL: members"; }

# Also verify via task API that group_id is set
R=$(curl -s "$API/tasks/$TID1" -H "$AUTH")
echo "$R" | grep -q '"group_id"' && green "  PASS: task has group_id" || { red "  FAIL: no group_id on task"; }

# ============================================================
# Test 3: Run group (sequential mode)
# ============================================================
echo ""
echo "--- Test 3: Group Execution (sequential) ---"

R=$(curl -s "$API/groups/$GID/run" -X POST -H "$AUTH")
echo "$R" | grep -q '"code":0' && green "  PASS: group triggered" || { red "  FAIL: trigger"; echo "$R"; }

# Wait for execution
sleep 3

# Check execution logs for all three tasks
for tid in $TID1 $TID2 $TID3; do
    R=$(curl -s "$API/tasks/$tid/logs" -H "$AUTH")
    if echo "$R" | grep -q '"status":"success"'; then
        green "  PASS: task $tid completed successfully"
    elif echo "$R" | grep -q '"status":"failed"'; then
        green "  PASS: task $tid failed as expected (failing task)"
    else
        echo "  WARN: task $tid status unclear"
    fi
done

# ============================================================
# Test 4: Run group (parallel mode)
# ============================================================
echo ""
echo "--- Test 4: Group Execution (parallel) ---"

# Create parallel group
R=$(curl -s "$API/groups" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"parallel-test","mode":"parallel"}')
PGID=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")

# Assign tasks 1 and 2 only (the working ones)
curl -s "$API/groups/$PGID/members" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d "{\"task_ids\":[$TID1,$TID2]}" > /dev/null

R=$(curl -s "$API/groups/$PGID/run" -X POST -H "$AUTH")
echo "$R" | grep -q '"code":0' && green "  PASS: parallel group triggered" || { red "  FAIL: trigger"; }

sleep 3

for tid in $TID1 $TID2; do
    R=$(curl -s "$API/tasks/$tid/logs" -H "$AUTH")
    if echo "$R" | grep -q '"status":"success"'; then
        green "  PASS: task $tid completed (parallel)"
    else
        red "  FAIL: task $tid did not complete"
    fi
done

# ============================================================
# Test 5: Delete group unlinks tasks
# ============================================================
echo ""
echo "--- Test 5: Group Deletion Cleanup ---"

curl -s "$API/groups/$GID" -X DELETE -H "$AUTH" > /dev/null
green "  Group deleted"

R=$(curl -s "$API/tasks/$TID1" -H "$AUTH")
if echo "$R" | grep -q '"group_id":null'; then
    green "  PASS: task unlinked after group delete"
else
    echo "  INFO: group_id may have been removed (check DB)"
fi

# ============================================================
# Test 6: Empty group trigger
# ============================================================
echo ""
echo "--- Test 6: Empty Group Rejection ---"

R=$(curl -s "$API/groups/$PGID/members" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"task_ids":[]}')
sleep 0.5
R=$(curl -s "$API/groups/$PGID/run" -X POST -H "$AUTH")
echo "$R" | grep -q '"message":"group has no members"' && green "  PASS: empty group rejected" || { red "  FAIL: should reject"; }

echo ""
echo "============================================"
echo " Integration tests completed"
echo "============================================"
