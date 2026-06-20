# Log Storage & Viewing Optimization - Design Spec

> **Version:** 1.0 | **Date:** 2026-05-26 | **Status:** Confirmed

## Overview

Fix log storage gaps (orphan cleanup, missing indexes), optimize SQLite for ~3M row scale (WAL mode, batch cleanup, query cache), improve log viewing UX (API slim-down, virtual scroll, lazy output load), and add CSV/JSON export.

Target scale: ~300 tasks, ~100K logs/day, 30-day retention = ~3M rows.

---

## 1. Bug Fixes

### 1.1 Group log auto-cleanup missing

**Problem:** `cleanupOldLogs()` in `scheduler/executor.go` only targets `model.ExecutionLog{}`. `GroupExecutionLog` grows unbounded.

**Fix:** Add matching retention + max_records cleanup for `GroupExecutionLog` in the same function.

### 1.2 Orphan group logs after group deletion

**Problem:** `DeleteGroup()` in `service/group_service.go` sets `group_id = nil` on member tasks but leaves `group_execution_logs` rows orphaned.

**Fix:** Append `s.DB.Where("group_id = ?", id).Delete(&model.GroupExecutionLog{})` before deleting the group.

### 1.3 "Clear All Logs" skips group logs

**Problem:** `ClearAllLogs()` only clears `execution_logs`.

**Fix:** Clear both `execution_logs` and `group_execution_logs`, return sum of deleted counts.

---

## 2. Storage Optimization

### 2.1 WAL mode

```go
db.Exec("PRAGMA journal_mode=WAL")
db.Exec("PRAGMA synchronous=NORMAL")
```

Kept `SetMaxOpenConns(1)` — WAL still benefits single-connection write speed.

### 2.2 New indexes

Run after AutoMigrate via `db.Exec("CREATE INDEX IF NOT EXISTS ...")`:

| Table | Index | Columns | Purpose |
|-------|-------|---------|---------|
| execution_logs | idx_el_created_at | (created_at) | Retention cleanup by days |
| execution_logs | idx_el_task_start | (task_id, start_time) | Per-task log queries |
| execution_logs | idx_el_status_start | (status, start_time) | Status filter + time sort |
| group_execution_logs | idx_gel_created_at | (created_at) | Retention cleanup by days |
| group_execution_logs | idx_gel_group_start | (group_id, start_time) | Per-group log queries |

### 2.3 Batch cleanup for max_records

Replace single subquery DELETE with batch loop (1000 rows per iteration) to avoid long transactions and subquery expansion on large tables.

---

## 3. Frontend Buttons & Confirmations

### 3.1 GroupList: add clear-logs button

Add a delete/trash icon button to the Actions column of GroupList table. Calls `logAPI.clearGroup(id)` with `el-popconfirm`.

### 3.2 ExecutionLogs: add Group column

Display the group name each task belongs to (from `task.group_id` join). No group-level filter on this page — group log viewing stays in GroupList drawer.

### 3.3 Delete group confirmation upgrade

Change popconfirm text from "Delete this group?" to:

> "Delete group 'X'? It has N task(s) and M execution log(s). Tasks will be kept; logs will be cleared."

Backend `DeleteGroup` returns `{tasks_affected, logs_deleted}` in response.

---

## 4. Dashboard Cache

### 4.1 60s TTL in-memory cache

`ExecutionService` holds a `sync.RWMutex`-protected cache struct:

```go
type statsCache struct {
    mu       sync.RWMutex
    data     map[string]interface{}
    expireAt time.Time
}
```

- `GetDashboardStats()` returns cached data within TTL; queries DB on miss.
- Cache is invalidated actively after each task execution log write (in executor).

---

## 5. Log Viewing Optimization

### 5.1 List API excludes output

`GetAllLogs` and `GetTaskLogs` SELECT all columns including `output` (up to 64KB per row). 20 rows = 1.28MB JSON.

**Fix:** GORM `Select` to exclude `output` column in list queries. Preview column in table shows the `error_msg` abbreviated instead, or just drop the preview column.

### 5.2 Lazy-load output on detail open

Add `GET /api/logs/:id` endpoint returning the full log record (including `output` and `error_msg`). Currently this route does not exist — the detail drawer reads from list row data which already includes output. After 5.1 strips output from list API, the drawer must fetch this endpoint on open.

Handler: `logH.GetLog` — simple `SELECT * FROM execution_logs WHERE id = ?`.

### 5.3 Virtual scroll table

Replace `el-table` with `el-table-v2` (from `@element-plus/components` or `element-plus` experimental export). Virtual scroll renders only visible rows, avoiding DOM bloat even with large datasets.

Fallback: if el-table-v2 integration is problematic, keep el-table with strict `max-height` + pagination (current 20/page is already fine for DOM). The real win is 5.1.

### 5.4 Large output fold

In execution detail drawer: if `output` exceeds 500 lines, show first 200 with a "Show all (N lines)" toggle button. Use `v-show` to avoid re-rendering.

---

## 6. Log Export

### 6.1 Export API

```
GET /api/logs/export?format=csv|json&task_name=&status=&since=&group_id=&max=100000
```

- `format`: `csv` (default) or `json`
- `max`: cap at 100,000 rows
- Same filter params as `GET /api/logs`
- Response: `Content-Type: text/csv; charset=utf-8` with `Content-Disposition: attachment; filename="cronix-logs-{date}.csv"`

### 6.2 CSV streaming

Go `encoding/csv` writer writes to `c.Writer` directly (buffered via `bufio.Writer`), one row at a time from DB cursor. Never loads full result set into memory.

CSV columns: `id, task_name, group_name, status, trigger_type, start_time, end_time, duration_ms, exit_code, output_truncated, error_msg`

### 6.3 Frontend export button

Button in ExecutionLogs page header: "Export CSV" / "Export JSON". Passes current filter state to export API. Shows download progress or just triggers browser download.

---

## 7. Files Changed

| File | Change |
|------|--------|
| `internal/database/database.go` | WAL pragmas, index creation |
| `internal/scheduler/executor.go` | cleanupOldLogs: add GroupExecutionLog cleanup, batch mode |
| `internal/service/execution_service.go` | ClearAllLogs both tables, stats cache, export method |
| `internal/service/group_service.go` | DeleteGroup: also delete group logs, return counts |
| `internal/handler/log.go` | Add ExportLogs handler, GetLog handler |
| `internal/handler/group.go` | DeleteGroup response with counts |
| `internal/router/router.go` | Add export route, GET /api/logs/:id route |
| `web/src/views/ExecutionLogs.vue` | Virtual scroll, Group column, export button, lazy output |
| `web/src/views/GroupList.vue` | Clear-logs button, delete confirmation text |
| `web/src/views/GroupEdit.vue` | — (no change) |
| `web/src/api/index.ts` | Add export API function |

---

## 8. Migration Notes

- Index creation uses `IF NOT EXISTS` — safe to run on existing DB.
- WAL mode is set at connection open — existing databases auto-upgrade on next start.
- No schema changes (no new columns/tables) — zero downtime migration.
- `el-table-v2` may require installing `@element-plus/components` or using the built-in export from `element-plus` v2.4+. If unavailable, keep `el-table` + pagination.
