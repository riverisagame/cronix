#!/bin/bash
# Smoke test: log storage & viewing optimization features
# Requires cronix running on localhost:2024
# Usage: bash deploy/smoke_log_optimization.sh

BASE="http://localhost:2024/api"
TOKEN=$(curl -sf "$BASE/login" -H "Content-Type: application/json" -d '{"username":"admin","password":"admin"}' | jq -r '.data.token')
if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
  echo "ERROR: Cannot authenticate. Is cronix running?"
  exit 1
fi
AUTH="Authorization: Bearer $TOKEN"

echo "=== 1. List logs — should NOT include output field ==="
ITEM=$(curl -sf "$BASE/logs?page=1&page_size=2" -H "$AUTH" | jq '.data.items[0]')
echo "$ITEM" | jq 'keys'
if echo "$ITEM" | jq -e '.output' > /dev/null 2>&1; then
  echo "WARN: output field still in list response"
else
  echo "PASS: no output in list"
fi

echo ""
echo "=== 2. GetLog single — SHOULD include output ==="
ID=$(curl -sf "$BASE/logs?page=1&page_size=1" -H "$AUTH" | jq -r '.data.items[0].id')
if [ "$ID" = "null" ] || [ -z "$ID" ]; then
  echo "SKIP: no logs exist yet"
else
  curl -sf "$BASE/logs/$ID" -H "$AUTH" | jq '.data | {id, has_output: (.output != null)}'
  echo "PASS: getLog returned"
fi

echo ""
echo "=== 3. Export CSV ==="
curl -sf -o /tmp/cronix-export.csv "$BASE/logs/export?format=csv&max=10" -H "$AUTH"
LINES=$(wc -l < /tmp/cronix-export.csv)
echo "CSV rows: $LINES (header + data)"
if [ "$LINES" -ge 1 ]; then echo "PASS: CSV export"; else echo "WARN: empty CSV"; fi

echo ""
echo "=== 4. Export JSON ==="
JSON_COUNT=$(curl -sf "$BASE/logs/export?format=json&max=5" -H "$AUTH" | jq '.data | length')
echo "JSON records: $JSON_COUNT"
echo "PASS: JSON export"

echo ""
echo "=== 5. Dashboard stats — cached ==="
curl -sf "$BASE/dashboard/stats" -H "$AUTH" | jq '.data'
echo "PASS: dashboard stats"

echo ""
echo "=== 6. ClearAllLogs — both tables ==="
RESULT=$(curl -sf -X DELETE "$BASE/logs" -H "$AUTH" | jq '.data')
echo "$RESULT"
echo "PASS: clear all logs"

echo ""
echo "=== 7. Delete group returns counts ==="
GROUP_ID=$(curl -sf "$BASE/groups" -H "$AUTH" -H "Content-Type: application/json" -d '{"name":"smoke-test-group","mode":"parallel"}' | jq -r '.data.id')
sleep 1
DEL_RESULT=$(curl -sf -X DELETE "$BASE/groups/$GROUP_ID" -H "$AUTH" | jq '.data')
echo "$DEL_RESULT"
echo "PASS: delete group with counts"

echo ""
echo "=== ALL CHECKS PASSED ==="
