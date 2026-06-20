# Log Storage & Viewing Optimization - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix log storage gaps, optimize SQLite for ~3M-row scale, improve log viewing UX, add CSV/JSON export.

**Architecture:** Backend changes span database init (WAL+indexes), executor cleanup (batch+GroupExecutionLog), service layer (cache, export, ClearAll), handlers (GetLog, ExportLogs, DeleteGroup counts), router (new routes). Frontend: GroupList button + confirm upgrade, ExecutionLogs el-table-v2 + lazy detail + export button.

**Tech Stack:** Go 1.22+, GORM/SQLite, gin, Vue 3, Element Plus 2.8, csv/encoding

---

## File Boundary Map

| File | Change |
|------|--------|
| `internal/database/database.go` | WAL pragmas, 5 CREATE INDEX IF NOT EXISTS |
| `internal/scheduler/executor.go` | cleanupOldLogs: add GroupExecutionLog + batch helper |
| `internal/service/execution_service.go` | stats cache, ClearAllLogs both tables, ExportLogs, Omit("output") in list queries |
| `internal/service/group_service.go` | DeleteGroup returns counts, deletes group logs |
| `internal/handler/log.go` | GetLog handler, ExportLogs handler, ClearAllLogs updated |
| `internal/handler/group.go` | DeleteGroup returns taskCount+logCount |
| `internal/router/router.go` | GET /api/logs/:id, GET /api/logs/export |
| `web/src/api/index.ts` | exportLogs function |
| `web/src/views/ExecutionLogs.vue` | el-table-v2, lazy detail load, export btn, group column |
| `web/src/views/GroupList.vue` | clear-logs button, delete confirm upgrade |

---

## Task 1: Database WAL mode + indexes

**Files:**
- Modify: `internal/database/database.go:128-158`

- [ ] **Step 1: Add WAL pragmas after SetMaxOpenConns**

After line `sqlDB.SetMaxOpenConns(1)` (~line 134), insert:

```go
// WAL mode: better concurrent read/write performance
db.Exec("PRAGMA journal_mode=WAL")
// NORMAL sync is safe in WAL mode, much faster than FULL
db.Exec("PRAGMA synchronous=NORMAL")
```

- [ ] **Step 2: Add CREATE INDEX IF NOT EXISTS after AutoMigrate**

After the AutoMigrate block (~line 158), insert:

```go
// Indexes for log cleanup and query performance
db.Exec("CREATE INDEX IF NOT EXISTS idx_el_created_at ON execution_logs(created_at)")
db.Exec("CREATE INDEX IF NOT EXISTS idx_el_task_start ON execution_logs(task_id, start_time)")
db.Exec("CREATE INDEX IF NOT EXISTS idx_el_status_start ON execution_logs(status, start_time)")
db.Exec("CREATE INDEX IF NOT EXISTS idx_gel_created_at ON group_execution_logs(created_at)")
db.Exec("CREATE INDEX IF NOT EXISTS idx_gel_group_start ON group_execution_logs(group_id, start_time)")
```

- [ ] **Step 3: Build and verify**

```powershell
go build ./internal/database/
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/database/database.go
git commit -m "feat: WAL mode + log table indexes for query/cleanup perf"
```

---

## Task 2: List APIs omit output + GET /api/logs/:id endpoint

**Files:**
- Modify: `internal/service/execution_service.go:24-71`
- Modify: `internal/handler/log.go:134-152`
- Modify: `internal/router/router.go:72-78`

- [ ] **Step 1: Add Omit("output") to GetAllLogs**

In `internal/service/execution_service.go`, line 49, change:
```go
query := s.DB.Model(&model.ExecutionLog{})
```
to:
```go
query := s.DB.Model(&model.ExecutionLog{}).Omit("output")
```

- [ ] **Step 2: Add Omit("output") to GetTaskLogs**

In `internal/service/execution_service.go`, line 27, change:
```go
query := s.DB.Model(&model.ExecutionLog{}).Where("task_id = ?", taskID)
```
to:
```go
query := s.DB.Model(&model.ExecutionLog{}).Omit("output").Where("task_id = ?", taskID)
```

- [ ] **Step 3: Add GetLog handler in log.go**

After the `DeleteLog` function (~line 142), insert:

```go
// GetLog returns a single execution log with full output.
// GET /api/logs/:id
func (h *LogHandler) GetLog(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    var log model.ExecutionLog
    if err := database.DB.First(&log, id).Error; err != nil {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "log not found"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": log})
}
```

Also add `"cronix/internal/database"` and `"cronix/internal/model"` to imports if not already present (check existing imports on line 7-15).

Actually — the handler uses `h.ExecSvc` not `database.DB` directly. Add a service method instead.

In `internal/service/execution_service.go`, after `DeleteLog`:

```go
// GetLog returns a single execution log with full output.
func (s *ExecutionService) GetLog(id uint) (*model.ExecutionLog, error) {
    var log model.ExecutionLog
    if err := s.DB.First(&log, id).Error; err != nil {
        return nil, err
    }
    return &log, nil
}
```

Then handler:

```go
// GetLog returns a single execution log with full output.
func (h *LogHandler) GetLog(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    log, err := h.ExecSvc.GetLog(uint(id))
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "log not found"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": log})
}
```

- [ ] **Step 4: Add route**

In `internal/router/router.go`, after the existing `api.DELETE("/logs/:id", logH.DeleteLog)` line (~line 73), add:

```go
api.GET("/logs/:id", logH.GetLog)                      // 获取单条日志详情（含完整output）
```

- [ ] **Step 5: Build**

```powershell
go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/service/execution_service.go internal/handler/log.go internal/router/router.go
git commit -m "feat: omit output from list APIs, add GET /api/logs/:id for full detail"
```

---

## Task 3: cleanupOldLogs — add GroupExecutionLog + batch delete

**Files:**
- Modify: `internal/scheduler/executor.go:84-117`

- [ ] **Step 1: Add batch cleanup helper**

After the `cleanupOldLogs` function (~line 117), insert:

```go
// deleteOldestBatch deletes excess records in batches of 1000 to avoid
// long transactions and large subquery expansion on big tables.
func (e *Executor) deleteOldestBatch(model interface{}, tableName string, maxRecords int) {
    var count int64
    e.db.Model(model).Count(&count)
    if count <= int64(maxRecords) {
        return
    }
    excess := count - int64(maxRecords)
    batchSize := int64(1000)
    for excess > 0 {
        n := batchSize
        if excess < n {
            n = excess
        }
        result := e.db.Where("id IN (?)",
            e.db.Model(model).Select("id").Order("id ASC").Limit(int(n)),
        ).Delete(model)
        if result.Error != nil {
            log.Warn().Err(result.Error).Str("table", tableName).Msg("batch cleanup failed")
            break
        }
        if result.RowsAffected == 0 {
            break
        }
        excess -= result.RowsAffected
    }
}
```

- [ ] **Step 2: Replace ExecutionLog max_records cleanup with batch helper**

In `cleanupOldLogs()`, replace lines 100-116 (the `if maxRecords > 0` block for ExecutionLog) with:

```go
if maxRecords > 0 {
    e.deleteOldestBatch(&model.ExecutionLog{}, "execution_logs", maxRecords)
}
```

- [ ] **Step 3: Add GroupExecutionLog cleanup block**

After the ExecutionLog cleanup block (both retention + max_records), insert:

```go
// Group execution log cleanup
if retentionDays > 0 {
    cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
    result := e.db.Where("created_at < ?", cutoff).Delete(&model.GroupExecutionLog{})
    if result.Error != nil {
        log.Warn().Err(result.Error).Msg("group log cleanup (retention) failed")
    } else if result.RowsAffected > 0 {
        log.Info().Int64("deleted", result.RowsAffected).Int("retention_days", retentionDays).Msg("group log cleanup (retention)")
    }
}
if maxRecords > 0 {
    e.deleteOldestBatch(&model.GroupExecutionLog{}, "group_execution_logs", maxRecords)
}
```

- [ ] **Step 4: Build**

```powershell
go build ./internal/scheduler/
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/executor.go
git commit -m "fix: add GroupExecutionLog auto-cleanup + batch delete for max_records"
```

---

## Task 4: DeleteGroup — clean orphan logs + return counts

**Files:**
- Modify: `internal/service/group_service.go:60-67`
- Modify: `internal/handler/group.go:73-80`

- [ ] **Step 1: Update DeleteGroup to clean logs and return counts**

Replace the `DeleteGroup` function in `internal/service/group_service.go` (lines 60-67):

```go
func (s *GroupService) DeleteGroup(id uint) (int64, int64, error) {
    // Count tasks before disassociating
    var taskCount int64
    s.DB.Model(&model.Task{}).Where("group_id = ?", id).Count(&taskCount)

    // Count logs before deleting
    var logCount int64
    s.DB.Model(&model.GroupExecutionLog{}).Where("group_id = ?", id).Count(&logCount)

    // Disassociate tasks (keep them, just remove group link)
    s.DB.Model(&model.Task{}).Where("group_id = ?", id).Update("group_id", nil)

    // Delete group execution logs
    s.DB.Where("group_id = ?", id).Delete(&model.GroupExecutionLog{})

    // Delete the group
    if err := s.DB.Delete(&model.TaskGroup{}, id).Error; err != nil {
        return 0, 0, err
    }
    if s.Engine != nil {
        s.Engine.ReloadAll()
    }
    return taskCount, logCount, nil
}
```

- [ ] **Step 2: Update DeleteGroup handler to return counts**

Replace the `DeleteGroup` handler in `internal/handler/group.go` (lines 73-80):

```go
func (h *GroupHandler) DeleteGroup(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    taskCount, logCount, err := h.GroupSvc.DeleteGroup(uint(id))
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{
        "tasks_affected": taskCount,
        "logs_deleted":   logCount,
    }})
}
```

- [ ] **Step 3: Build**

```powershell
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/service/group_service.go internal/handler/group.go
git commit -m "fix: DeleteGroup cleans orphan group logs, returns affected counts"
```

---

## Task 5: ClearAllLogs — both tables + Dashboard stats cache

**Files:**
- Modify: `internal/service/execution_service.go:121-125` (ClearAllLogs)
- Modify: `internal/service/execution_service.go:80-112` (GetDashboardStats)
- Modify: `internal/handler/log.go:113-120` (ClearAllLogs handler)

- [ ] **Step 1: ClearAllLogs clears both tables**

Replace `ClearAllLogs` in `execution_service.go` (lines 121-125):

```go
// ClearAllLogs deletes all execution logs and group execution logs.
func (s *ExecutionService) ClearAllLogs() (int64, int64, error) {
    r1 := s.DB.Where("1 = 1").Delete(&model.ExecutionLog{})
    if r1.Error != nil {
        return 0, 0, r1.Error
    }
    r2 := s.DB.Where("1 = 1").Delete(&model.GroupExecutionLog{})
    return r1.RowsAffected, r2.RowsAffected, r2.Error
}
```

- [ ] **Step 2: Update ClearAllLogs handler**

Replace the `ClearAllLogs` handler in `log.go` (lines 113-120):

```go
// ClearAllLogs deletes all execution logs.
// DELETE /api/logs
func (h *LogHandler) ClearAllLogs(c *gin.Context) {
    taskLogs, groupLogs, err := h.ExecSvc.ClearAllLogs()
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{
        "task_logs_deleted":  taskLogs,
        "group_logs_deleted": groupLogs,
    }})
}
```

- [ ] **Step 3: Add stats cache to ExecutionService**

Add `"sync"` to imports in `execution_service.go`. Add cache struct and field:

After the `ExecutionService` struct (~line 14-16), replace with:

```go
type statsCache struct {
    mu       sync.RWMutex
    data     map[string]interface{}
    expireAt time.Time
}

// ExecutionService is the execution log service layer.
type ExecutionService struct {
    DB    *gorm.DB
    cache *statsCache
}

// NewExecutionService creates a new ExecutionService.
func NewExecutionService(db *gorm.DB) *ExecutionService {
    return &ExecutionService{DB: db, cache: &statsCache{}}
}
```

- [ ] **Step 4: Wrap GetDashboardStats with cache**

Replace `GetDashboardStats` function (lines 80-112) to wrap existing queries in cache:

```go
func (s *ExecutionService) GetDashboardStats() (map[string]interface{}, error) {
    // Check cache (60s TTL)
    s.cache.mu.RLock()
    if s.cache.data != nil && time.Now().Before(s.cache.expireAt) {
        data := s.cache.data
        s.cache.mu.RUnlock()
        return data, nil
    }
    s.cache.mu.RUnlock()

    s.cache.mu.Lock()
    defer s.cache.mu.Unlock()
    // Double-check after acquiring write lock
    if s.cache.data != nil && time.Now().Before(s.cache.expireAt) {
        return s.cache.data, nil
    }

    // ---- existing stats queries (unchanged) ----
    var totalTasks int64
    s.DB.Model(&model.Task{}).Count(&totalTasks)

    var enabledTasks int64
    s.DB.Model(&model.Task{}).Where("enabled = ?", true).Count(&enabledTasks)

    today := time.Now().Truncate(24 * time.Hour)

    var todayTotal int64
    s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ?", today).Count(&todayTotal)

    var todaySuccess int64
    s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ? AND status = ?", today, "success").Count(&todaySuccess)

    var todayFailed int64
    s.DB.Model(&model.ExecutionLog{}).Where("start_time >= ? AND status = ?", today, "failed").Count(&todayFailed)

    stats := map[string]interface{}{
        "total_tasks":   totalTasks,
        "enabled_tasks": enabledTasks,
        "today_total":   todayTotal,
        "today_success": todaySuccess,
        "today_failed":  todayFailed,
    }
    s.cache.data = stats
    s.cache.expireAt = time.Now().Add(60 * time.Second)
    return stats, nil
}
```

- [ ] **Step 5: Update ExecutionService constructor in cmd/root.go**

In `cmd/root.go` line 278, change:
```go
execSvc := &service.ExecutionService{DB: database.DB}
```
to:
```go
execSvc := service.NewExecutionService(database.DB)
```

- [ ] **Step 6: Build**

```powershell
go build ./...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/service/execution_service.go internal/handler/log.go
git commit -m "feat: ClearAllLogs both tables, dashboard stats 60s cache"
```

---

## Task 6: Export API

**Files:**
- Modify: `internal/service/execution_service.go` (add ExportLogs)
- Modify: `internal/handler/log.go` (add ExportLogs handler)
- Modify: `internal/router/router.go` (add export route)

- [ ] **Step 1: Add ExportLogs to service**

In `internal/service/execution_service.go`, add before `CleanOldLogs`:

```go
// ExportLogs returns up to maxRows execution logs matching filters.
// Output column is excluded to keep the response compact.
func (s *ExecutionService) ExportLogs(taskName, status, since string, maxRows int) ([]model.ExecutionLog, error) {
    var logs []model.ExecutionLog
    query := s.DB.Model(&model.ExecutionLog{}).Omit("output")

    if taskName != "" {
        query = query.Where("task_name LIKE ?", "%"+taskName+"%")
    }
    if status != "" {
        query = query.Where("status = ?", status)
    }
    if since != "" {
        if d, err := time.ParseDuration(since); err == nil {
            query = query.Where("start_time > ?", time.Now().Add(-d))
        }
    }
    if err := query.Order("start_time DESC").Limit(maxRows).Find(&logs).Error; err != nil {
        return nil, err
    }
    return logs, nil
}
```

- [ ] **Step 2: Add ExportLogs handler**

In `internal/handler/log.go`, add these imports: `"encoding/csv"`, `"fmt"`, `"time"`. Then add after `ClearGroupLogs`:

```go
// ExportLogs exports execution logs as CSV or JSON.
// GET /api/logs/export?format=csv|json&task_name=&status=&since=&max=100000
func (h *LogHandler) ExportLogs(c *gin.Context) {
    format := c.DefaultQuery("format", "csv")
    maxRows, _ := strconv.Atoi(c.DefaultQuery("max", "100000"))
    if maxRows > 100000 {
        maxRows = 100000
    }
    if maxRows < 1 {
        maxRows = 100000
    }

    taskName := c.Query("task_name")
    status := c.Query("status")
    since := c.Query("since")

    logs, err := h.ExecSvc.ExportLogs(taskName, status, since, maxRows)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }

    date := time.Now().Format("2006-01-02")

    if format == "json" {
        c.Header("Content-Type", "application/json; charset=utf-8")
        c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"cronix-logs-%s.json\"", date))
        c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": logs})
        return
    }

    // Default: CSV
    c.Header("Content-Type", "text/csv; charset=utf-8")
    c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"cronix-logs-%s.csv\"", date))
    w := csv.NewWriter(c.Writer)
    w.Write([]string{"id", "task_name", "status", "trigger_type", "start_time", "end_time", "exit_code", "error_msg", "created_at"})
    for _, l := range logs {
        endTime := ""
        if l.EndTime != nil {
            endTime = l.EndTime.Format("2006-01-02 15:04:05")
        }
        exitCode := ""
        if l.ExitCode != nil {
            exitCode = strconv.Itoa(*l.ExitCode)
        }
        w.Write([]string{
            strconv.FormatUint(uint64(l.ID), 10),
            l.TaskName,
            l.Status,
            l.TriggerType,
            l.StartTime.Format("2006-01-02 15:04:05"),
            endTime,
            exitCode,
            l.ErrorMsg,
            l.CreatedAt.Format("2006-01-02 15:04:05"),
        })
    }
    w.Flush()
}
```

- [ ] **Step 3: Add route**

In `internal/router/router.go`, add before the `// ---- 任务组管理 ----` comment:

```go
api.GET("/logs/export", logH.ExportLogs)                  // 导出日志CSV/JSON
```

Important: this route must be BEFORE `api.GET("/logs/:id", logH.GetLog)` to avoid `:id` matching "export".

- [ ] **Step 4: Build**

```powershell
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/service/execution_service.go internal/handler/log.go internal/router/router.go
git commit -m "feat: add log export API (CSV/JSON, max 100K rows)"
```

---

## Task 7: Frontend — API layer updates

**Files:**
- Modify: `web/src/api/index.ts:105-118`

- [ ] **Step 1: Add export and getLog functions to logAPI**

Replace the `logAPI` block (lines 108-118):

```ts
export const logAPI = {
  list(params: any) { return api.get('/logs', { params }) },
  clearAll() { return api.delete('/logs') },
  deleteLog(id: number) { return api.delete('/logs/' + id) },
  getLog(id: number) { return api.get('/logs/' + id) },
  clearTask(id: number) { return api.delete('/tasks/' + id + '/logs') },
  clearGroup(id: number) { return api.delete('/groups/' + id + '/logs') },
  exportLogs(params: any) { return api.get('/logs/export', { params, responseType: params?.format === 'json' ? 'json' : 'blob' }) },
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```powershell
cd web; npx vue-tsc --noEmit 2>&1 | Select-Object -First 20
```

Expected: no new errors (or only pre-existing ones).

- [ ] **Step 3: Commit**

```bash
git add web/src/api/index.ts
git commit -m "feat: add getLog and exportLogs API functions"
```

---

## Task 8: Frontend — GroupList clear button + delete confirm

**Files:**
- Modify: `web/src/views/GroupList.vue:28-35` (actions column)
- Modify: `web/src/views/GroupList.vue:100-104` (deleteGroup function)

- [ ] **Step 1: Add clear-logs button to Actions column**

In the `<el-table-column label="Actions">` block (line 28-36), add after the log button (line 32):

```vue
<el-popconfirm title="Clear all execution logs for this group?" @confirm="clearGroupLogs(row.id)">
  <template #reference><el-button size="small" type="warning" circle><el-icon><DeleteFilled /></el-icon></el-button></template>
</el-popconfirm>
```

Add `DeleteFilled` to the icon imports on line 72:
```ts
import { Plus, Edit, VideoPlay, Delete, Tickets, DeleteFilled } from '@element-plus/icons-vue'
```

- [ ] **Step 2: Upgrade delete confirmation text**

Replace the existing delete popconfirm (line 33) from:

```vue
<el-popconfirm title="Delete this group?" @confirm="deleteGroup(row.id)">
```

to:

```vue
<el-popconfirm title="Delete this group?" @confirm="deleteGroup(row.id, row.name)">
```

- [ ] **Step 3: Update deleteGroup to show result counts**

Replace the `deleteGroup` function (lines 100-104):

```ts
async function deleteGroup(id: number, name: string) {
  try {
    const r = await groupAPI.delete(id)
    const d = r.data.data
    ElMessage.success(`Deleted '${name}': ${d.tasks_affected} task(s) disassociated, ${d.logs_deleted} log(s) cleared`)
  } catch (e: any) {
    ElMessage.error(e.response?.data?.message || 'Failed')
  }
  load()
}
```

- [ ] **Step 4: Verify TypeScript**

```powershell
cd web; npx vue-tsc --noEmit 2>&1 | Select-Object -First 20
```

Expected: no new errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/views/GroupList.vue
git commit -m "feat: GroupList clear-logs button + delete confirm with counts"
```

---

## Task 9: Frontend — ExecutionLogs revamp (virtual scroll, lazy detail, export, group column)

**Files:**
- Modify: `web/src/views/ExecutionLogs.vue` (substantial rewrite)

- [ ] **Step 1: Replace el-table with el-table-v2, add export button, group column**

Write the full component. Key changes:
- Use `el-table-v2` instead of `el-table` (import `ElTableV2` from `element-plus`)
- Add "Export CSV" and "Export JSON" buttons in header
- Add "Group" column from `task.group_name` or leave blank if no group
- On row click, call `logAPI.getLog(id)` to fetch full output before showing drawer
- Remove `output` preview column (output is no longer in list data)
- Keep existing filter controls (task_name, status, since)

```vue
<!--
  ExecutionLogs.vue -- execution log page with virtual scroll, lazy detail, export.
-->
<template>
  <div>
    <h2 style="margin-top:0;display:flex;align-items:center;gap:12px">
      Execution Logs
      <el-popconfirm title="Delete ALL execution logs?" @confirm="clearAllLogs">
        <template #reference><el-button size="small" type="danger" :loading="clearing">Clear All</el-button></template>
      </el-popconfirm>
      <el-button size="small" @click="exportLogs('csv')" :loading="exporting">Export CSV</el-button>
      <el-button size="small" @click="exportLogs('json')" :loading="exporting">Export JSON</el-button>
    </h2>

    <el-card shadow="hover">
      <el-row :gutter="16" style="margin-bottom:16px">
        <el-col :span="6">
          <el-input v-model="filters.task_name" placeholder="Task name..." clearable @keyup.enter="load">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="4">
          <el-select v-model="filters.status" placeholder="Status" clearable @change="load" style="width:100%">
            <el-option label="Success" value="success" />
            <el-option label="Failed" value="failed" />
            <el-option label="Timeout" value="timeout" />
            <el-option label="Running" value="running" />
          </el-select>
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.since" placeholder="Time range" clearable @change="load" style="width:100%">
            <el-option label="Last 1 hour" value="1h" />
            <el-option label="Last 6 hours" value="6h" />
            <el-option label="Last 24 hours" value="24h" />
            <el-option label="Last 7 days" value="168h" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-button type="primary" @click="load"><el-icon><Search /></el-icon> Search</el-button>
          <el-button @click="load"><el-icon><Refresh /></el-icon></el-button>
        </el-col>
      </el-row>

      <el-auto-resizer>
        <template #default="{ height, width }">
          <el-table-v2
            :columns="columns"
            :data="logs"
            :width="width"
            :height="600"
            :row-height="40"
            fixed
            @row-click="showDetail"
            style="cursor:pointer"
          />
        </template>
      </el-auto-resizer>

      <div style="margin-top:16px;text-align:right">
        <el-pagination v-model:current-page="page" :total="total" :page-size="20" layout="total,prev,pager,next" @current-change="load" />
      </div>
    </el-card>

    <el-drawer v-model="drawerVisible" title="Execution Detail" size="650px" direction="rtl">
      <template v-if="detail">
        <div style="display:flex;gap:10px;margin-bottom:16px">
          <el-tag :type="detail.status==='success'?'success':'danger'">{{ detail.status?.toUpperCase() }}</el-tag>
          <el-tag type="info">{{ detail.trigger_type }}</el-tag>
          <el-tag v-if="detail.exit_code!==null">exit={{ detail.exit_code }}</el-tag>
        </div>
        <el-descriptions :column="2" border size="small" style="margin-bottom:16px">
          <el-descriptions-item label="Task">{{ detail.task_name }}</el-descriptions-item>
          <el-descriptions-item label="Cron">{{ detail.cron_expr||'-' }}</el-descriptions-item>
          <el-descriptions-item label="Start">{{ detail.start_time }}</el-descriptions-item>
          <el-descriptions-item label="Duration">{{ detail.end_time ? duration(detail.start_time,detail.end_time) : 'N/A' }}</el-descriptions-item>
        </el-descriptions>

        <div v-if="detail.output" style="margin-bottom:16px">
          <div style="font-weight:bold;margin-bottom:8px;color:#67C23A">Output</div>
          <pre ref="outputPre" style="background:#f5f7fa;color:#303133;padding:14px;border-radius:8px;font-size:13px;line-height:1.6;white-space:pre-wrap;word-break:break-all;max-height:300px;overflow:auto;margin:0">{{ outputDisplay }}</pre>
          <el-button v-if="outputTruncated" size="small" text @click="showFullOutput = !showFullOutput" style="margin-top:4px">
            {{ showFullOutput ? 'Collapse' : `Show all (${outputLineCount} lines)` }}
          </el-button>
        </div>

        <div v-if="detail.error_msg">
          <div style="font-weight:bold;margin-bottom:8px;color:#F56C6C">Error</div>
          <pre style="background:#fef0f0;color:#F56C6C;padding:14px;border-radius:8px;font-size:13px;line-height:1.6;white-space:pre-wrap;word-break:break-all;max-height:300px;overflow:auto;margin:0">{{ detail.error_msg }}</pre>
        </div>
      </template>
      <div v-else style="text-align:center;padding:40px;color:#909399" v-loading="detailLoading">Loading...</div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { ElTableV2 } from 'element-plus'
import type { Column } from 'element-plus'
import { logAPI } from '../api/index'
import { Search, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

const logs = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const clearing = ref(false)
const exporting = ref(false)

const filters = reactive({ task_name:'', status:'', since:'' })

const drawerVisible = ref(false)
const detail = ref<any>(null)
const detailLoading = ref(false)
const showFullOutput = ref(false)

const outputLineCount = computed(() => detail.value?.output ? detail.value.output.split('\n').length : 0)
const outputTruncated = computed(() => outputLineCount.value > 500)
const outputDisplay = computed(() => {
  if (!detail.value?.output) return ''
  if (!outputTruncated.value || showFullOutput.value) return detail.value.output
  return detail.value.output.split('\n').slice(0, 200).join('\n') + '\n... (truncated)'
})

const columns: Column<any>[] = [
  { key: 'id', title: 'ID', width: 60, dataKey: 'id' },
  { key: 'task_name', title: 'Task', width: 150, dataKey: 'task_name' },
  { key: 'group_name', title: 'Group', width: 120, dataKey: 'group_name' },
  {
    key: 'status', title: 'Status', width: 100, dataKey: 'status',
    cellRenderer: ({ cellData }: any) => {
      const type = cellData === 'success' ? 'success' : cellData === 'failed' ? 'danger' : 'warning'
      return h => h('el-tag', { type, size: 'small' }, () => cellData?.toUpperCase())
    }
  },
  { key: 'trigger_type', title: 'Trigger', width: 80, dataKey: 'trigger_type' },
  { key: 'start_time', title: 'Time', width: 170, dataKey: 'start_time' },
  { key: 'exit_code', title: 'Exit', width: 60, dataKey: 'exit_code' },
  {
    key: 'error_msg', title: 'Preview', width: 200, dataKey: 'error_msg',
    cellRenderer: ({ cellData }: any) => {
      if (!cellData) return h => h('span', { style: 'color:#c0c4cc' }, '-')
      const text = cellData.length > 80 ? cellData.substring(0, 80) + '...' : cellData
      return h => h('code', { style: 'font-size:12px;color:#606266' }, text)
    }
  },
]

function duration(start:string, end:string) {
  const diff = new Date(end).getTime() - new Date(start).getTime()
  if (diff<1000) return diff+'ms'
  if (diff<60000) return (diff/1000).toFixed(2)+'s'
  return (diff/60000).toFixed(1)+'min'
}

async function load() {
  loading.value = true
  try {
    const r = await logAPI.list({
      page: page.value,
      page_size: 20,
      task_name: filters.task_name || undefined,
      status: filters.status || undefined,
      since: filters.since || undefined
    })
    const items = r.data.data.items || []
    // Enrich with group_name from the task relation if available
    logs.value = items
    total.value = r.data.data.total || 0
  }
  finally { loading.value = false }
}

async function clearAllLogs() {
  clearing.value = true
  try {
    const r = await logAPI.clearAll()
    ElMessage.success(`Deleted ${r.data.data.task_logs_deleted} task logs + ${r.data.data.group_logs_deleted} group logs`)
    load()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.message || 'Failed')
  } finally { clearing.value = false }
}

async function showDetail(row: any) {
  drawerVisible.value = true
  detail.value = null
  detailLoading.value = true
  showFullOutput.value = false
  try {
    const r = await logAPI.getLog(row.id)
    detail.value = r.data.data
  } catch {
    // Fallback: use row data (no output)
    detail.value = row
  } finally { detailLoading.value = false }
}

async function exportLogs(format: string) {
  exporting.value = true
  try {
    const r = await logAPI.exportLogs({
      format,
      max: 100000,
      task_name: filters.task_name || undefined,
      status: filters.status || undefined,
      since: filters.since || undefined,
    })
    // Trigger browser download
    const blob = r.data instanceof Blob ? r.data : new Blob([r.data], { type: format === 'json' ? 'application/json' : 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    const date = new Date().toISOString().slice(0, 10)
    a.download = `cronix-logs-${date}.${format}`
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success('Export downloaded')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.message || 'Failed')
  } finally { exporting.value = false }
}

onMounted(load)
</script>
```

Note: `el-auto-resizer` is from `@element-plus/components` or can be replaced with a simple fixed-height wrapper. Element Plus 2.8 exports `ElAutoResizer` from `element-plus`. If unavailable, replace with a direct `:width` and `:height` binding.

- [ ] **Step 2: Verify TypeScript**

```powershell
cd web; npx vue-tsc --noEmit 2>&1 | Select-Object -First 30
```

Expected: may have TS errors from el-table-v2 types. Fix any type mismatches.

- [ ] **Step 3: Build frontend**

```powershell
cd web; npm run build
```

Expected: no build errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/views/ExecutionLogs.vue
git commit -m "feat: ExecutionLogs virtual scroll, lazy detail load, export, group column"
```

---

## Task 10: Integration smoke test + final commit

**Files:** (test script)
- Create: `deploy/smoke_log_optimization.sh`

- [ ] **Step 1: Create smoke test script**

```bash
#!/bin/bash
# Smoke test: log optimization features
BASE="http://localhost:2024/api"
TOKEN=$(curl -s "$BASE/login" -H "Content-Type: application/json" -d '{"username":"admin","password":"admin"}' | jq -r '.data.token')
AUTH="Authorization: Bearer $TOKEN"

echo "=== 1. List logs (should NOT include output field) ==="
curl -s "$BASE/logs?page=1&page_size=2" -H "$AUTH" | jq '.data.items[0] | keys'

echo "=== 2. Get single log (SHOULD include output) ==="
ID=$(curl -s "$BASE/logs?page=1&page_size=1" -H "$AUTH" | jq -r '.data.items[0].id')
curl -s "$BASE/logs/$ID" -H "$AUTH" | jq '.data | {id, has_output: (.output != null)}'

echo "=== 3. Export CSV ==="
curl -s -o /tmp/cronix-export.csv "$BASE/logs/export?format=csv&max=10" -H "$AUTH"
wc -l /tmp/cronix-export.csv

echo "=== 4. Export JSON ==="
curl -s "$BASE/logs/export?format=json&max=5" -H "$AUTH" | jq '.data | length'

echo "=== 5. Dashboard stats (cached) ==="
curl -s "$BASE/dashboard/stats" -H "$AUTH" | jq '.data'

echo "=== 6. Delete group returns counts ==="
# Create a test group with a task, then delete
GROUP_ID=$(curl -s "$BASE/groups" -H "$AUTH" -H "Content-Type: application/json" -d '{"name":"smoke-test-group","mode":"parallel"}' | jq -r '.data.id')
curl -s -X DELETE "$BASE/groups/$GROUP_ID" -H "$AUTH" | jq '.data'

echo "=== ALL CHECKS PASSED ==="
```

- [ ] **Step 2: Run smoke test**

```powershell
bash deploy/smoke_log_optimization.sh
```

Expected: all checks pass.

- [ ] **Step 3: Final commit**

```bash
git add deploy/smoke_log_optimization.sh
git commit -m "test: add smoke test for log optimization features"
```
