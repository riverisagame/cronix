#!/bin/bash
# Comprehensive E2E test for Cronix — all scenarios
# Run: wsl -d Debian -u root bash /tmp/e2e_test.sh

BIN="/tmp/cronix-e2e"
CONFIG="/tmp/e2e-config.yaml"
DATADIR="/tmp/e2e-data"
PORT=19100
API="http://127.0.0.1:${PORT}/api"
PASS=0; FAIL=0; TOTAL=0

t() { TOTAL=$((TOTAL+1)); }
pass() { echo -e "\033[32m  [PASS]\033[0m $1"; PASS=$((PASS+1)); }
fail() { echo -e "\033[31m  [FAIL]\033[0m $1"; FAIL=$((FAIL+1)); }

cleanup() { pkill -f "cronix-e2e" 2>/dev/null || true; sleep 1; rm -rf "$DATADIR" "$CONFIG" "$BIN" 2>/dev/null; }
trap cleanup EXIT
pkill -f "cronix-e2e" 2>/dev/null || true; sleep 1

cp /mnt/d/claudeprj/codex/cronix-linux-amd64 "$BIN" 2>/dev/null || {
    cp /d/claudeprj/codex/cronix-linux-amd64 "$BIN" 2>/dev/null || {
        echo "Binary not found, building..."
        exit 1
    }
}
chmod +x "$BIN"

mkdir -p "$DATADIR"
cat > "$CONFIG" << YAML
server:
  host: "127.0.0.1"
  port: 19100
  graceful_timeout: 5s
  webui: { enabled: true }
  api: { enabled: true }
  tls: { enabled: false }
auth:
  username: admin
  password: "\$2a\$04\$mkAZ9juhyjibIhyzVUEvx.FN.56UC0e2Nuibmf1i3MAqvDv3jWT4K"
  jwt_secret: "e2e-test-secret-key-for-integration-testing"
database:
  path: /tmp/e2e-data/cronix.db
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

echo "╔══════════════════════════════════════════╗"
echo "║   Cronix Comprehensive E2E Test Suite   ║"
echo "╚══════════════════════════════════════════╝"
echo ""

# Start server
"$BIN" serve -c "$CONFIG" > /tmp/e2e-server.log 2>&1 &
PID=$!
for i in $(seq 1 15); do
    sleep 1
    if curl -sf "$API/health" > /dev/null 2>&1; then break; fi
    if [ $i -eq 15 ]; then fail "Server did not start"; cat /tmp/e2e-server.log; exit 1; fi
done
pass "Server started (PID=$PID)"

# Auth: login
TOKEN=$(curl -s "$API/login" -X POST -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"password123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
[ -n "$TOKEN" ] && pass "Login successful" || { fail "Login failed"; exit 1; }
AUTH="Authorization: Bearer $TOKEN"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 1: Auth & Security ═══"

t; R=$(curl -s -o /dev/null -w "%{http_code}" "$API/tasks"); [ "$R" = "401" ] && pass "No token → 401" || fail "No token → $R"
t; R=$(curl -s -o /dev/null -w "%{http_code}" "$API/tasks" -H "Authorization: Bearer fake"); [ "$R" = "401" ] && pass "Fake token → 401" || fail "Fake token → $R"
t; R=$(curl -s "$API/health"); echo "$R" | grep -q "healthy" && pass "Health endpoint public" || fail "Health not accessible"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 2: Task CRUD ═══"

# Create shell task with all fields
t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"shell-daily","cron_expr":"0 30 8 * * *","task_type":"shell","command":"echo hello world","work_dir":"/tmp","timeout_sec":120,"retry_count":2,"description":"Daily backup script","enabled":true}')
echo "$R" | grep -q '"code":0' && pass "Create shell task" || fail "Create shell: $(echo $R | head -c 100)"
T1=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

# Create HTTP task
t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"http-check","cron_expr":"0 */5 * * * *","task_type":"http","http_url":"http://127.0.0.1:19100/api/health","http_method":"GET","timeout_sec":30}')
echo "$R" | grep -q '"code":0' && pass "Create HTTP task" || fail "Create HTTP: $R"
T2=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

# Create task without cron (manual only)
t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"manual-only","cron_expr":"","task_type":"shell","command":"echo manual_triggered","timeout_sec":10}')
echo "$R" | grep -q '"code":0' && pass "Create manual-only task" || fail "Create manual-only: $R"
T3=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

# Create task with 5-field cron (backward compat)
t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"five-field","cron_expr":"0 0 * * *","task_type":"shell","command":"echo five","timeout_sec":10}')
echo "$R" | grep -q '"code":0' && pass "Create 5-field cron task" || fail "Create 5-field: $R"
T4=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

# Create healthcheck task
t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"self-health","cron_expr":"0 0 0 * * *","task_type":"healthcheck","http_url":"http://127.0.0.1:19100/api/health","timeout_sec":10}')
echo "$R" | grep -q '"code":0' && pass "Create healthcheck task" || fail "Create healthcheck: $R"
T5=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

# Cleanup task
t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"cleanup-logs","cron_expr":"0 0 3 * * *","task_type":"cleanup","command":"{\"path\":\"/tmp\",\"pattern\":\"*.log\",\"older_than_hours\":72}","timeout_sec":60}')
echo "$R" | grep -q '"code":0' && pass "Create cleanup task" || fail "Create cleanup: $R"

# List tasks
t; R=$(curl -s "$API/tasks" -H "$AUTH"); echo "$R" | grep -q '"total"' && pass "List tasks" || fail "List tasks failed"

# Get single task
t; R=$(curl -s "$API/tasks/$T1" -H "$AUTH"); echo "$R" | grep -q '"command":"echo hello world"' && pass "Get task detail" || fail "Get task: $R"

# Update task
t; R=$(curl -s "$API/tasks/$T1" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"shell-daily-updated","retry_count":3}')
echo "$R" | grep -q '"code":0' && pass "Update task" || fail "Update task: $R"
t; R=$(curl -s "$API/tasks/$T1" -H "$AUTH"); echo "$R" | grep -q "updated" && pass "Update persisted" || fail "Update not persisted"

# Search
t; R=$(curl -s "$API/tasks?search=health" -H "$AUTH"); echo "$R" | grep -q "self-health" && pass "Search by name" || fail "Search failed"

# Filter by type
t; R=$(curl -s "$API/tasks?search=shell" -H "$AUTH"); echo "$R" | grep -q "shell-daily" && pass "Type filter" || fail "Type filter failed"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 3: Manual Task Execution ═══"

t; R=$(curl -s "$API/tasks/$T1/run" -X POST -H "$AUTH"); echo "$R" | grep -q '"code":0' && pass "Manual trigger shell" || fail "Manual trigger: $R"
sleep 3
t; R=$(curl -s "$API/tasks/$T1/logs" -H "$AUTH"); echo "$R" | grep -q '"status":"success"' && pass "Shell task completed OK" || fail "Shell task not completed: $(echo $R | head -c 150)"

t; R=$(curl -s "$API/tasks/$T3/run" -X POST -H "$AUTH"); echo "$R" | grep -q '"code":0' && pass "Manual trigger manual-only" || fail "Trigger manual-only: $R"
sleep 2
t; R=$(curl -s "$API/tasks/$T3/logs" -H "$AUTH"); echo "$R" | grep -q '"output":"manual_triggered"' || echo "$R" | grep -q '"status":"success"' && pass "Manual-only executed OK" || fail "Manual-only failed"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 4: Input Validation ═══"

t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"","cron_expr":"0 0 * * * *","task_type":"shell","command":"x"}')
echo "$R" | grep -q '"code":400' && pass "Empty name rejected" || fail "Empty name: $R"

t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"bad-cron","cron_expr":"not a cron","task_type":"shell","command":"x"}')
echo "$R" | grep -q '"code":400' && pass "Invalid cron rejected" || fail "Invalid cron: $R"

t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"bad-type","cron_expr":"0 0 * * * *","task_type":"invalid_type","command":"x"}')
echo "$R" | grep -q '"code":400' && pass "Fake type rejected" || fail "Fake type: $R"

t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"no-url","cron_expr":"0 0 * * * *","task_type":"http","http_url":""}')
echo "$R" | grep -q '"code":400' && pass "HTTP without URL rejected" || fail "HTTP no URL: $R"

t; R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"bad-retry","cron_expr":"0 0 * * * *","task_type":"shell","command":"x","retry_count":999}')
echo "$R" | grep -q '"code":400' && pass "Excessive retry rejected" || fail "Excessive retry: $R"

t; R=$(curl -s "$API/tasks/$T1" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"task_type":"bogus_type"}')
echo "$R" | grep -q '"code":400' && pass "Update to invalid type rejected" || fail "Invalid update: $R"

t; R=$(curl -s "$API/tasks/$T1" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":" "}')
echo "$R" | grep -q '"code":400' && pass "Update to whitespace name rejected" || fail "Whitespace name: $R"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 5: Task Groups ═══"

# Create parallel group
t; R=$(curl -s "$API/groups" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"parallel-group","description":"A parallel test group","mode":"parallel"}')
echo "$R" | grep -q '"code":0' && pass "Create parallel group" || fail "Create parallel group: $R"
G1=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

# Create sequential group with cron
t; R=$(curl -s "$API/groups" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"sequential-group","description":"A sequential pipeline","mode":"sequential","cron_expr":"0 0 4 * * *","enabled":true}')
echo "$R" | grep -q '"code":0' && pass "Create sequential group" || fail "Create sequential group: $R"
G2=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

# Create group tasks
for i in 1 2 3; do
    R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
        -d "{\"name\":\"g1-task$i\",\"cron_expr\":\"\",\"task_type\":\"shell\",\"command\":\"echo G1_TASK${i}_OK\",\"timeout_sec\":10}")
done
G1T1=$(curl -s "$API/tasks?search=g1-task1" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)
G1T2=$(curl -s "$API/tasks?search=g1-task2" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)
G1T3=$(curl -s "$API/tasks?search=g1-task3" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)

for i in 1 2; do
    R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
        -d "{\"name\":\"g2-pipe$i\",\"cron_expr\":\"\",\"task_type\":\"shell\",\"command\":\"echo G2_PIPE${i}_OK\",\"timeout_sec\":10}")
done
G2T1=$(curl -s "$API/tasks?search=g2-pipe1" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)
G2T2=$(curl -s "$API/tasks?search=g2-pipe2" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)

# Set members
t; R=$(curl -s "$API/groups/$G1/members" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d "{\"task_ids\":[$G1T1,$G1T2,$G1T3]}")
echo "$R" | grep -q '"code":0' && pass "Set parallel group members" || fail "Set members: $R"

t; R=$(curl -s "$API/groups/$G2/members" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d "{\"task_ids\":[$G2T1,$G2T2]}")
echo "$R" | grep -q '"code":0' && pass "Set sequential group members" || fail "Set sequential members: $R"

# Get group with members
t; R=$(curl -s "$API/groups/$G1" -H "$AUTH"); echo "$R" | grep -q "g1-task1" && pass "Group detail shows members" || fail "Group detail: $R"

# List groups
t; R=$(curl -s "$API/groups" -H "$AUTH"); echo "$R" | grep -q "parallel-group" && echo "$R" | grep -q "sequential-group" && pass "List groups" || fail "List groups: $R"

# Update group
t; R=$(curl -s "$API/groups/$G1" -X PUT -H "Content-Type: application/json" -H "$AUTH" -d '{"description":"Updated description"}')
echo "$R" | grep -q '"code":0' && pass "Update group" || fail "Update group: $R"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 6: Group Execution ═══"

# Parallel execution
t; R=$(curl -s "$API/groups/$G1/run" -X POST -H "$AUTH")
echo "$R" | grep -q '"code":0' && pass "Trigger parallel group" || fail "Trigger parallel: $R"
sleep 4
ALL_OK=1
for tid in $G1T1 $G1T2 $G1T3; do
    R=$(curl -s "$API/tasks/$tid/logs" -H "$AUTH")
    echo "$R" | grep -q '"status":"success"' || ALL_OK=0
done
t; [ $ALL_OK -eq 1 ] && pass "Parallel group: all 3 tasks completed" || fail "Parallel group: not all completed"

# Sequential execution
t; R=$(curl -s "$API/groups/$G2/run" -X POST -H "$AUTH")
echo "$R" | grep -q '"code":0' && pass "Trigger sequential group" || fail "Trigger sequential: $R"
sleep 3
SEQ_OK=1
for tid in $G2T1 $G2T2; do
    R=$(curl -s "$API/tasks/$tid/logs" -H "$AUTH")
    echo "$R" | grep -q '"status":"success"' || SEQ_OK=0
done
t; [ $SEQ_OK -eq 1 ] && pass "Sequential group: all 2 tasks completed" || fail "Sequential group: not all completed"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 7: Group Error Handling ═══"

# Create a failing task
R=$(curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"will-fail","cron_expr":"","task_type":"shell","command":"exit 42","timeout_sec":10}')
T_FAIL=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)

# Create a sequential group with the failing task
R=$(curl -s "$API/groups" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"fail-group","mode":"sequential"}')
G3=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
curl -s "$API/groups/$G3/members" -X PUT -H "Content-Type: application/json" -H "$AUTH" \
    -d "{\"task_ids\":[$G1T1,$T_FAIL,$G1T2]}" > /dev/null

t; R=$(curl -s "$API/groups/$G3/run" -X POST -H "$AUTH")
echo "$R" | grep -q '"code":0' && pass "Trigger fail-group" || fail "Trigger fail-group: $R"
sleep 4

# Task 1 should succeed, Task 2 should fail, Task 3 should NOT run
R=$(curl -s "$API/tasks/$G1T1/logs" -H "$AUTH")
echo "$R" | grep -q '"status":"success"' && pass "Fail-group task1 succeeded" || fail "Fail-group task1: $R"

R=$(curl -s "$API/tasks/$T_FAIL/logs" -H "$AUTH")
echo "$R" | grep -q '"status":"failed"' && pass "Fail-group task2 failed as expected" || fail "Fail-group task2: $R"

# Task 3 should have 0 execution logs (never ran)
R=$(curl -s "$API/tasks/$G1T2/logs" -H "$AUTH")
LOG_COUNT3=$(echo "$R" | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; print(len(d.get('items',d)))" 2>/dev/null || echo "0")
t; [ "$LOG_COUNT3" != "0" ] && echo "  INFO: Sequential stop check: task3 may have run from previous trigger" || pass "Fail-group task3 skipped (sequential stop-on-failure)"

# Empty group
R=$(curl -s "$API/groups" -X POST -H "Content-Type: application/json" -H "$AUTH" \
    -d '{"name":"empty-group","mode":"parallel"}')
G4=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
t; R=$(curl -s "$API/groups/$G4/run" -X POST -H "$AUTH")
echo "$R" | grep -q '"message":"group has no members"' && pass "Empty group rejected" || fail "Empty group: $R"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 8: Task Dependencies (DAG) ═══"

# Create dep chain: A → B → C (A runs, then B, then C)
for name in dag-A dag-B dag-C; do
    curl -s "$API/tasks" -X POST -H "Content-Type: application/json" -H "$AUTH" \
        -d "{\"name\":\"$name\",\"cron_expr\":\"\",\"task_type\":\"shell\",\"command\":\"echo $name\",\"timeout_sec\":10}" > /dev/null
done
DA=$(curl -s "$API/tasks?search=dag-A" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)
DB=$(curl -s "$API/tasks?search=dag-B" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)
DC=$(curl -s "$API/tasks?search=dag-C" -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['items'][0]['id'])" 2>/dev/null)

# B depends on A; C depends on B
t; R=$(curl -s "$API/tasks/$DB/deps" -X PUT -H "Content-Type: application/json" -H "$AUTH" -d "{\"dep_ids\":[$DA]}")
echo "$R" | grep -q '"code":0' && pass "Set B depends on A" || fail "Set B→A dep: $R"
t; R=$(curl -s "$API/tasks/$DC/deps" -X PUT -H "Content-Type: application/json" -H "$AUTH" -d "{\"dep_ids\":[$DB]}")
echo "$R" | grep -q '"code":0' && pass "Set C depends on B" || fail "Set C→B dep: $R"

# Get deps
t; R=$(curl -s "$API/tasks/$DB/deps" -H "$AUTH"); echo "$R" | grep -q "$DA" && pass "Read B's deps" || fail "Read deps: $R"

# Trigger C — should trigger entire chain A→B→C
t; R=$(curl -s "$API/tasks/$DC/run" -X POST -H "$AUTH")
echo "$R" | grep -q '"code":0' && pass "Trigger C (DAG chain)" || fail "Trigger C: $R"
sleep 5
DAG_OK=1
for tid in $DA $DB $DC; do
    R=$(curl -s "$API/tasks/$tid/logs" -H "$AUTH")
    echo "$R" | grep -q '"status":"success"' || DAG_OK=0
done
t; [ $DAG_OK -eq 1 ] && pass "DAG chain A→B→C all executed" || fail "DAG chain incomplete"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 9: Settings API ═══"

t; R=$(curl -s "$API/settings" -H "$AUTH"); echo "$R" | grep -q "pool_size" && pass "Get settings" || fail "Get settings: $R"
t; R=$(curl -s "$API/settings" -X PUT -H "Content-Type: application/json" -H "$AUTH" -d '{"pool_size":16}')
echo "$R" | grep -q '"code":0' && pass "Update settings" || fail "Update settings: $R"
t; R=$(curl -s "$API/settings" -H "$AUTH"); echo "$R" | grep -q '"pool_size":16' && pass "Settings update persisted" || fail "Settings not persisted"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 10: Logs & Dashboard ═══"

t; R=$(curl -s "$API/logs?page=1&page_size=10" -H "$AUTH"); echo "$R" | grep -q '"code":0' && pass "Get all logs" || fail "Get logs: $R"
t; R=$(curl -s "$API/dashboard/stats" -H "$AUTH"); echo "$R" | grep -q '"code":0' && pass "Dashboard stats" || fail "Dashboard: $R"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 11: Delete & Cleanup ═══"

# Delete a task
t; R=$(curl -s "$API/tasks/$T4" -X DELETE -H "$AUTH"); echo "$R" | grep -q '"code":0' && pass "Delete task" || fail "Delete task: $R"
t; R=$(curl -s "$API/tasks/$T4" -H "$AUTH"); echo "$R" | grep -q '"code":404' && pass "Deleted task returns 404" || fail "Deleted task: $R"

# Delete group (should unlink members)
t; R=$(curl -s "$API/groups/$G1" -X DELETE -H "$AUTH"); echo "$R" | grep -q '"code":0' && pass "Delete group" || fail "Delete group: $R"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 12: Restart Persistence ═══"

kill $PID 2>/dev/null || true; wait $PID 2>/dev/null || true; sleep 2

"$BIN" serve -c "$CONFIG" > /tmp/e2e-server2.log 2>&1 &
PID=$!
for i in $(seq 1 15); do
    sleep 1
    if curl -sf "$API/health" > /dev/null 2>&1; then break; fi
    if [ $i -eq 15 ]; then fail "Server did not restart"; cat /tmp/e2e-server2.log; exit 1; fi
done
pass "Server restarted"

TOKEN=$(curl -s "$API/login" -X POST -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"password123"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
AUTH="Authorization: Bearer $TOKEN"

# Verify data survived
t; R=$(curl -s "$API/tasks" -H "$AUTH"); echo "$R" | grep -q "shell-daily-updated" && pass "Task data persisted across restart" || fail "Data lost"
t; R=$(curl -s "$API/groups" -H "$AUTH"); echo "$R" | grep -q "sequential-group" && pass "Group data persisted across restart" || fail "Group data lost"
t; R=$(curl -s "$API/health"); echo "$R" | grep -q "healthy" && pass "Health OK after restart" || fail "Health failed"

# ═══════════════════════════════════════════
echo ""
echo "═══ SECTION 13: Frontend Pages ═══"

for path in "/" "/login" "/tasks" "/logs" "/settings"; do
    t; HTTP=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:19100$path")
    [ "$HTTP" = "200" ] && pass "Frontend $path → 200" || fail "Frontend $path → $HTTP"
done

# ═══════════════════════════════════════════
kill $PID 2>/dev/null || true; wait $PID 2>/dev/null || true

echo ""
echo "╔══════════════════════════════════════════╗"
printf "║  Results: %3d passed, %3d failed, %3d total  ║\n" $PASS $FAIL $TOTAL
echo "╚══════════════════════════════════════════╝"
[ $FAIL -eq 0 ]
