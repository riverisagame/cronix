# Cronix - High Performance Task Scheduler - Design Spec

> **Version:** 1.0 | **Date:** 2026-05-24 | **Status:** Confirmed

## Overview

Cronix is a Go-based high-performance, high-reliability, high-stability scheduled task manager, replacing Linux crontab.
Supports Shell scripts, HTTP requests, cleanup tasks, and health checks. DAG dependency orchestration, Web Dashboard + CLI dual management, single binary deployment.

---

## Tech Stack

| Layer | Choice | Version |
|-------|--------|---------|
| Language | Go | 1.22+ |
| Web Framework | gin-gonic/gin | v1.10+ |
| ORM | gorm.io/gorm + driver/sqlite | v1.25+ |
| Cron Engine | robfig/cron/v3 | v3.0+ |
| CLI Framework | spf13/cobra | v1.8+ |
| Config | spf13/viper | v1.19+ |
| Logger | rs/zerolog | v1.33+ |
| Log Rotation | lumberjack | v2.2+ |
| JWT | golang-jwt/jwt/v5 | v5.2+ |
| Goroutine Pool | panjf2000/ants/v2 | v2.10+ |
| Frontend | Vue 3 + Element Plus + Vite | latest |
| Database | SQLite 3 (WAL mode) | - |

---

## 1. Architecture

### 1.1 Overall Architecture

Single binary cronix:
  Vue3/EP Dashboard (embed.FS) --> Gin API Server :8080
    --> JWT Auth Middleware
    --> Event Bus (channel)
      --> DAG Resolver (topological sort)
        --> Executor (ants pool, panic isolation, timeout)
      --> Notifier (channel, webhook/email)
    --> SQLite (WAL, GORM) + Memory Cache (30s TTL)
  Cobra CLI (passwd, logs, serve)

### 1.2 Event-Driven Flow

cron engine trigger -> buffered chan (cap 1024)
  -> DAG resolver -> topological sort -> submit by layers
    -> L0 (no deps) -> ants pool concurrent
    -> L1 (depends on L0) -> serial after L0 completes
  -> write execution_logs
  -> notify event -> notify chan (async, non-blocking)

### 1.3 Startup Modes

| Mode | webui | api | Description |
|------|-------|-----|-------------|
| Full | true | true | Dashboard + API + Scheduler |
| API-only | false | true | API + Scheduler, manage via curl |
| Headless | false | false | Scheduler only, config-driven tasks |

---

## 2. Data Models

### 2.1 Tasks

| Field | Type | Description |
|-------|------|-------------|
| id | INTEGER PK | Auto increment |
| name | TEXT UNIQUE | Task name |
| cron_expr | TEXT | 6-field second-level cron |
| task_type | TEXT | shell/http/cleanup/healthcheck |
| command | TEXT | Shell command or cleanup JSON config |
| http_method | TEXT | GET/POST/PUT/DELETE |
| http_url | TEXT | Target URL |
| http_headers | TEXT | JSON format headers |
| http_body | TEXT | Request body |
| http_auth_type | TEXT | none/basic/bearer/api_key/oauth2 |
| http_auth_config | TEXT | JSON auth config (sanitized storage) |
| timeout_sec | INTEGER | Timeout seconds, default 300 |
| retry_count | INTEGER | Retry count |
| retry_interval_sec | INTEGER | Retry interval seconds |
| max_concurrent | INTEGER | Max concurrent instances |
| enabled | INTEGER | Enable/disable |
| description | TEXT | Description |
| work_dir | TEXT | Shell working directory |
| created_at | DATETIME | |
| updated_at | DATETIME | |

Index: idx_tasks_enabled ON tasks(enabled)

### 2.2 Task Dependencies

| Field | Type | Description |
|-------|------|-------------|
| id | INTEGER PK | |
| task_id | INTEGER FK | Task ID |
| depends_on_id | INTEGER FK | Depends on task ID |
| UNIQUE(task_id, depends_on_id) | | |
| CHECK(task_id != depends_on_id) | | No self-dependency |

Indexes: idx_deps_task, idx_deps_depends

### 2.3 Execution Logs

| Field | Type | Description |
|-------|------|-------------|
| id | INTEGER PK | |
| task_id | INTEGER FK | Task ID (nullable) |
| task_name | TEXT | Task name (redundant for query) |
| cron_expr | TEXT | Trigger cron expression |
| status | TEXT | running/success/failed/timeout/cancelled |
| trigger_type | TEXT | cron/manual |
| start_time | DATETIME | |
| end_time | DATETIME | |
| exit_code | INTEGER | |
| output | TEXT | Truncated to 64KB |
| error_msg | TEXT | |
| retry_attempt | INTEGER | |
| created_at | DATETIME | |

Indexes: idx_logs_task, idx_logs_status, idx_logs_start

### 2.4 Notify Configs

| Field | Type | Description |
|-------|------|-------------|
| id | INTEGER PK | |
| task_id | INTEGER FK | |
| on_success | INTEGER | Notify on success |
| on_failure | INTEGER | Notify on failure (default 1) |
| notify_type | TEXT | webhook/email |
| webhook_url | TEXT | |
| email_to | TEXT | |
| created_at | DATETIME | |

Index: idx_notify_task

---

## 3. Authentication

- Password: bcrypt hash stored in config auth.password
- CLI: cronix passwd interactive setup
- Token: JWT, 24h expiry
- Middleware: Gin middleware intercepts all /api/* (except /api/login)
- First start: skip auth if no password + WARN log

---

## 4. Execution Types

### Shell
- exec.CommandContext + independent process group (syscall.Setpgid)
- On timeout: kill entire process group, no zombies

### HTTP
- Support GET/POST/PUT/DELETE
- Custom Headers/Body
- Auth: Basic/Bearer/API Key (header or query)/OAuth2 Client Credentials
- OAuth2: auto-fetch token -> memory cache -> refresh 60s before expiry
- Circuit breaker: 5 failures -> 60s cooldown -> half-open probe

### Cleanup
- command stores JSON: {"path":"/tmp/logs","pattern":"*.log","older_than_hours":72}

### Healthcheck
- HTTP GET -> non-2xx -> failure notification

---

## 5. High Availability Design

### High Performance
- robfig/cron timer wheel O(1) lookup
- ants goroutine pool reuses goroutines
- SQLite WAL mode read/write non-blocking
- Memory cache task list 30s TTL
- Async channel notification non-blocking

### High Reliability
- Graceful shutdown: SIGINT -> stop scheduler -> wait 30s -> close DB
- Crash recovery: scan running logs on startup -> mark timeout after 5min
- Idempotent protection: same cron window execution_lock
- Notification retry: 3 times + failure queue

### High Stability/Security
- Panic isolation: recover + stack trace
- Process group isolation: shell independent process group
- Resource limits: context timeout + output truncation + log rotation
- DAG deadlock detection: pre-save + pre-execution dual validation
- Disk protection: >90% usage alert + stop new tasks
- Sensitive sanitization: password/token/secret sanitized in API response

---

## 6. Configuration File

```yaml
server:
  port: 8080
  graceful_timeout: 30s
  webui:
    enabled: true
  api:
    enabled: true

auth:
  username: admin
  password: ""

database:
  path: ./data/cronix.db
  wal_mode: true
  busy_timeout: 5000
  cache_size: 2000

executor:
  pool_size: 32
  output_truncate_kb: 64
  memory_limit_mb: 512

log:
  level: info
  file: ./data/cronix.log
  max_size_mb: 100
  max_backups: 7
  max_age_days: 30
  retention_days: 30
  max_records: 100000

notify:
  retry: 3
  retry_interval: 5s

circuit_breaker:
  failure_threshold: 5
  cooldown_seconds: 60
```

### Headless Mode Task Config

```yaml
tasks:
  - name: backup-db
    cron_expr: "0 0 2 * * *"
    task_type: shell
    command: /opt/scripts/backup.sh
    timeout_sec: 600
    retry_count: 2
    retry_interval_sec: 30
    enabled: true

  - name: health-check
    cron_expr: "0 */5 * * * *"
    task_type: healthcheck
    http_url: https://api.example.com/health
    http_auth_type: bearer
    http_auth_config:
      token: "${HEALTH_TOKEN}"
    notify:
      - on_failure: true
        notify_type: webhook
        webhook_url: https://hooks.example.com/alert
```

---

## 7. Project Structure

```
cronix/
├── main.go
├── go.mod
├── config.yaml
├── embed.go
├── cmd/
│   ├── root.go
│   ├── passwd.go
│   └── logs.go
├── internal/
│   ├── config/config.go
│   ├── model/
│   │   ├── task.go
│   │   ├── task_dep.go
│   │   ├── execution_log.go
│   │   └── notify_config.go
│   ├── database/database.go
│   ├── middleware/auth.go
│   ├── scheduler/
│   │   ├── engine.go
│   │   ├── executor.go
│   │   ├── dag.go
│   │   └── circuit.go
│   ├── handler/
│   │   ├── task.go
│   │   ├── log.go
│   │   ├── dashboard.go
│   │   ├── auth.go
│   │   └── settings.go
│   ├── service/
│   │   ├── task_service.go
│   │   ├── execution_service.go
│   │   └── notify_service.go
│   ├── executor/
│   │   ├── shell.go
│   │   ├── http_exec.go
│   │   ├── cleanup.go
│   │   └── healthcheck.go
│   ├── notify/notifier.go
│   ├── cache/cache.go
│   └── router/router.go
├── web/
│   ├── package.json
│   ├── vite.config.ts
│   └── src/
│       ├── main.ts
│       ├── App.vue
│       ├── router/index.ts
│       ├── api/
│       │   ├── request.ts
│       │   ├── task.ts
│       │   ├── log.ts
│       │   ├── auth.ts
│       │   └── dashboard.ts
│       ├── views/
│       │   ├── Login.vue
│       │   ├── Dashboard.vue
│       │   ├── TaskList.vue
│       │   ├── TaskEdit.vue
│       │   ├── ExecutionLogs.vue
│       │   └── Settings.vue
│       └── components/
│           ├── CronInput.vue
│           ├── DepSelector.vue
│           └── LogViewer.vue
└── data/  (gitignore: cronix.db, cronix.log)
```

---

## 8. API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | /api/login | No | Login, get JWT |
| GET | /api/tasks | Yes | Task list (paginated + search) |
| POST | /api/tasks | Yes | Create task |
| GET | /api/tasks/:id | Yes | Task detail |
| PUT | /api/tasks/:id | Yes | Update task |
| DELETE | /api/tasks/:id | Yes | Delete task |
| POST | /api/tasks/:id/run | Yes | Manual trigger |
| GET | /api/tasks/:id/logs | Yes | Task execution logs |
| GET | /api/tasks/:id/deps | Yes | Get dependencies |
| PUT | /api/tasks/:id/deps | Yes | Update dependencies |
| GET | /api/dashboard/stats | Yes | Dashboard statistics |
| GET | /api/logs | Yes | Global execution logs |
| GET | /api/settings | Yes | Get settings |
| PUT | /api/settings | Yes | Update settings |

---

## 9. CLI Commands

| Command | Description |
|---------|-------------|
| cronix serve | Start service (default) |
| cronix serve -c cfg.yaml | Custom config path |
| cronix passwd | Set/modify WebUI password |
| cronix logs | View recent logs |
| cronix logs --follow | Real-time tail -f |
| cronix logs --task NAME | Filter by task name |
| cronix logs --status failed --since 1h | Filter by status and time |
| cronix run --name backup-db | Manual trigger task |

---

## 10. Frontend Pages

| Page | Route | Features |
|------|-------|----------|
| Login | /login | JWT login form |
| Dashboard | / | Stats cards + recent executions |
| Task List | /tasks | Table + search + toggle + manual run |
| Task Editor | /tasks/:id | Create/edit + deps + notify config |
| Execution Logs | /logs | Global log filtering and viewing |
| Settings | /settings | System configuration |
