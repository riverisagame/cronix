# Cronix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build production-grade Go task scheduler (Cronix) with Vue3/Element Plus dashboard, JWT auth, DAG dependencies, 4 executor types, single-binary deployment.

**Architecture:** Event-driven - cron engine triggers via buffered channel -> DAG resolver (topological sort) -> ants goroutine pool executes -> async notifier. Gin API with JWT middleware. Vue3 SPA via go:embed. SQLite WAL mode.

**Tech Stack:** Go 1.22+, Gin, GORM/SQLite, robfig/cron, cobra, viper, zerolog, ants, Vue3/ElementPlus/Vite, golang-jwt

---

## File Boundary Map

| File | Responsibility |
|------|---------------|
| main.go | Entry point, cobra root |
| embed.go | go:embed web/dist |
| config.yaml | Default config |
| cmd/root.go | cronix serve |
| cmd/passwd.go | cronix passwd |
| cmd/logs.go | cronix logs |
| internal/config/config.go | Viper loading + hot-reload |
| internal/model/task.go | Task GORM model |
| internal/model/task_dep.go | TaskDep model |
| internal/model/execution_log.go | ExecutionLog model |
| internal/model/notify_config.go | NotifyConfig model |
| internal/database/database.go | GORM init + WAL + migrate |
| internal/middleware/auth.go | JWT middleware |
| internal/scheduler/engine.go | cron wrapper + event bus |
| internal/scheduler/dag.go | DAG + topological sort |
| internal/scheduler/executor.go | DAG + ants pool coordinator |
| internal/scheduler/circuit.go | HTTP circuit breaker |
| internal/executor/shell.go | Shell executor |
| internal/executor/http_exec.go | HTTP executor with auth |
| internal/executor/cleanup.go | File cleanup executor |
| internal/executor/healthcheck.go | Health check executor |
| internal/service/task_service.go | Task CRUD logic |
| internal/service/execution_service.go | Execution + manual trigger |
| internal/service/notify_service.go | Notification dispatch |
| internal/notify/notifier.go | Webhook + email sender |
| internal/cache/cache.go | In-memory TTL cache |
| internal/handler/task.go | Task CRUD handlers |
| internal/handler/log.go | Execution log handlers |
| internal/handler/dashboard.go | Dashboard stats |
| internal/handler/auth.go | Login handler |
| internal/handler/settings.go | Settings handler |
| internal/router/router.go | Gin routes + embed static |
| web/ | Vue3 + Element Plus SPA |

---

## Task 1: Project Scaffold and Go Module

**Files:** go.mod, main.go, .gitignore

- [ ] Init Go module: go mod init cronix
- [ ] Create .gitignore (data/, web/dist/, web/node_modules/, *.exe, *.log)
- [ ] Create main.go skeleton with Execute() stub, os.Exit on error
- [ ] go build -o cronix.exe .

---

## Task 2: Configuration System

**Files:** internal/config/config.go, config.yaml

- [ ] go get github.com/spf13/viper
- [ ] Create config.yaml with all sections (server, auth, database, executor, log, notify, circuit_breaker)
- [ ] Write config.go: Config structs with mapstructure tags, Load() function using viper.ReadInConfig + Unmarshal + WatchConfig
- [ ] go build ./internal/config/



## Task 3: Database Layer

**Files:** internal/model/task.go, task_dep.go, execution_log.go, notify_config.go, internal/database/database.go

- [ ] go get gorm.io/gorm gorm.io/driver/sqlite
- [ ] Write Task model: ID, Name(unique), CronExpr, TaskType(shell/http/cleanup/healthcheck), Command, HTTPMethod, HTTPURL, HTTPHeaders(json), HTTPBody, HTTPAuthType, HTTPAuthConfig(json tag:-), TimeoutSec(300), RetryCount, RetryIntervalSec(10), MaxConcurrent(1), Enabled(bool), Description, WorkDir, timestamps. TableName() tasks.
- [ ] Write TaskDep model: ID, TaskID, DependsOnID. TableName() task_deps.
- [ ] Write ExecutionLog model: ID, TaskID(*uint nullable), TaskName, CronExpr, Status(running/success/failed/timeout/cancelled), TriggerType(cron/manual), StartTime, EndTime(*time.Time), ExitCode(*int), Output, ErrorMsg, RetryAttempt, CreatedAt. TableName() execution_logs.
- [ ] Write NotifyConfig model: ID, TaskID, OnSuccess, OnFailure(true), NotifyType(webhook/email), WebhookURL, EmailTo, CreatedAt. TableName() notify_configs.
- [ ] Write database.go: Init(dbPath) creates dir, gorm.Open with WAL&busy_timeout params, SetMaxOpenConns(1), AutoMigrate all 4 models.
- [ ] go build ./internal/...

---

## Task 4: DAG Engine

**Files:** internal/scheduler/dag.go, internal/scheduler/dag_test.go

- [ ] Write NewDAG(): returns DAG with nodes []uint, edges map[uint][]uint, inDegree map[uint]int
- [ ] AddNode(id): adds node to maps
- [ ] AddEdge(from, to uint) error: adds edge to[from], increments inDegree[to], runs hasCycle(), rolls back on cycle
- [ ] hasCycle() bool: Kahn algorithm with inDegree copy + queue
- [ ] TopologicalSort() [][]uint: Kahn returning layers, each layer = nodes with inDegree 0 at that round
- [ ] Write tests: TestDAGNoCycle (3 nodes, edges 1->2, 1->3, verify layers), TestDAGCycleDetection (1->2, 2->3, 3->1, expect error)
- [ ] go test ./internal/scheduler/ -v -run TestDAG -> PASS

---

## Task 5: Circuit Breaker

**Files:** internal/scheduler/circuit.go

- [ ] Write CircuitBreaker struct: mu Mutex, state(CircuitClosed/Open/HalfOpen), failures int, lastFailure time, threshold int, cooldown Duration
- [ ] NewCircuitBreaker(threshold, cooldownSec)
- [ ] Allow() bool: Closed=true, Open checks cooldown->HalfOpen true else false, HalfOpen=true
- [ ] RecordSuccess(): failures=0, state=Closed
- [ ] RecordFailure(): failures++, if >=threshold state=Open
- [ ] go build ./internal/scheduler/

---

## Task 6: Executor Types

**Files:** internal/executor/shell.go, http_exec.go, cleanup.go, healthcheck.go

- [ ] go get github.com/panjf2000/ants/v2
- [ ] Write ExecuteShell(ctx, command, workDir, timeoutSec): exec.CommandContext with sh -c, Setpgid=true, capture stdout/stderr, on DeadlineExceeded kill process group (-pid), return ShellResult{Output, ExitCode, Error}
- [ ] Write ExecuteHTTP(ctx, method, url, headers, body, authType, authConfig, timeout, cbThreshold, cbCooldown): circuit breaker check -> build http.Request with context -> apply auth (basic/bearer/api_key(header|query)/oauth2) -> execute -> 5xx=recordFailure else recordSuccess. OAuth2: fetch token, cache with 60s expiry buffer.
- [ ] Write HTTPAuthConfig struct (Username, Password, Token, HeaderName, APIKey, KeyIn, TokenURL, ClientID, ClientSecret, Scopes)
- [ ] Write applyHTTPAuth(req, authType, authConfig): switch on authType, set Authorization header or query param
- [ ] Write getOAuthToken(cfg): POST client_credentials, cache in sync.Map, return cached if not near expiry
- [ ] Write ExecuteCleanup(ctx, configJSON): parse CleanupConfig{Path, Pattern, OlderThanHours}, filepath.Glob, os.Remove files older than cutoff
- [ ] Write ExecuteHealthCheck(ctx, url, timeoutSec): HTTP GET, return status, error if non-2xx
- [ ] go build ./internal/executor/

---



## Task 7: Scheduler Engine + Event Bus

**Files:** internal/scheduler/engine.go, internal/scheduler/executor.go

- [ ] go get github.com/robfig/cron/v3
- [ ] Write Engine struct: cron *cron.Cron(cron.WithSeconds), db, triggerCh(chan uint, cap 1024), mu, entryMap(map[uint]cron.EntryID)
- [ ] NewEngine(db): creates cron, triggerCh, entryMap
- [ ] TriggerChan() <-chan uint: returns triggerCh
- [ ] Start(): e.cron.Start()
- [ ] Stop() context.Context: e.cron.Stop()
- [ ] ReloadAll(): remove all entries, query enabled tasks, AddFunc(task.CronExpr, func(){triggerCh<-taskID}), store entryMap
- [ ] Write Executor struct: db, pool(*ants.Pool), cfg, engine
- [ ] NewExecutor(db, cfg, engine): ants.NewPool(cfg.PoolSize or CPU*4)
- [ ] Run(ctx): select loop ctx.Done / engine.TriggerChan -> handleTrigger
- [ ] handleTrigger(taskID): fetch all enabled tasks, buildDAG, TopologicalSort, for each layer: WaitGroup + pool.Submit with panic recover, executeTask
- [ ] buildDAG(tasks): NewDAG, AddNode each task, query TaskDep, AddEdge(DependsOnID, TaskID)
- [ ] executeTask(taskID): fetch task, create ExecutionLog(running), defer save result, switch task.TaskType call appropriate executor
- [ ] Shutdown(): pool.Release()
- [ ] go mod tidy && go build ./...

---

## Task 8: Task Service Layer

**Files:** internal/service/task_service.go, internal/service/execution_service.go

- [ ] Write TaskService struct{db *gorm.DB}
- [ ] ListTasks(page, pageSize int, search string): query with Where name LIKE, Order created_at DESC, Offset/Limit, return ([]Task, total int64)
- [ ] GetTask(id): First by id, Preload related deps and notifies
- [ ] CreateTask(task *Task): validate name unique, cron_expr parseable, Save, return task
- [ ] UpdateTask(id, task): First by id, Updates, reload scheduler engine
- [ ] DeleteTask(id): delete task (cascade deps/logs/notifies), reload scheduler
- [ ] ToggleTask(id): toggle Enabled, reload scheduler
- [ ] RunTaskNow(id): create ExecutionLog with trigger_type=manual, submit to executor
- [ ] GetTaskDeps(id): query TaskDep where task_id
- [ ] UpdateTaskDeps(id, depIDs []uint): delete existing, create new, validate no cycles via DAG.AddEdge
- [ ] Write ExecutionService struct{db *gorm.DB}
- [ ] GetTaskLogs(taskID, page, pageSize, status): query execution_logs filtered
- [ ] GetAllLogs(page, pageSize, taskName, status, since): query with filters
- [ ] GetDashboardStats(): count tasks, today executions, success/failure counts
- [ ] CleanOldLogs(retentionDays): delete where created_at < cutoff, keep max maxRecords
- [ ] go build ./internal/service/

---

## Task 9: Auth Middleware + Login Handler

**Files:** internal/middleware/auth.go, internal/handler/auth.go

- [ ] go get golang.org/x/crypto/bcrypt github.com/golang-jwt/jwt/v5
- [ ] Write AuthMiddleware(cfg): returns gin.HandlerFunc, skip if auth.password empty, parse Bearer token, jwt.Parse with HMAC secret(sha256 of password), set userID in context
- [ ] Write POST /api/login handler: bind JSON{username,password}, bcrypt.CompareHashAndPassword, generate JWT(24h expiry, sub=username), return {token, expires_at}
- [ ] Write sanitizeAuthConfig(task): mask password/token/secret fields in HTTPAuthConfig before returning in API
- [ ] go build ./internal/middleware/ ./internal/handler/

---



## Task 10: API Handlers

**Files:** internal/handler/task.go, log.go, dashboard.go, settings.go

- [ ] go get github.com/gin-gonic/gin
- [ ] Write task handlers: GET /api/tasks (ListTasks paginated), POST /api/tasks (CreateTask), GET /api/tasks/:id (GetTask + sanitize), PUT /api/tasks/:id (UpdateTask), DELETE /api/tasks/:id (DeleteTask), POST /api/tasks/:id/run (RunTaskNow), GET /api/tasks/:id/logs (GetTaskLogs), GET /api/tasks/:id/deps (GetTaskDeps), PUT /api/tasks/:id/deps (UpdateTaskDeps)
- [ ] Write log handler: GET /api/logs (GetAllLogs with query params page, page_size, task_name, status, since)
- [ ] Write dashboard handler: GET /api/dashboard/stats (GetDashboardStats)
- [ ] Write settings handler: GET /api/settings (return config), PUT /api/settings (update config + persist to yaml)
- [ ] All handlers use gin.Context, return JSON {code, message, data}
- [ ] go build ./internal/handler/

---

## Task 11: Router + Embed Static Files

**Files:** internal/router/router.go, embed.go

- [ ] Write SetupRouter(cfg, handlers, authMW): gin.New with Recovery+Logger middleware
- [ ] Public routes: POST /api/login, health check
- [ ] Protected group (/api): use authMW, register all CRUD routes from handlers
- [ ] If cfg.Server.WebUI.Enabled: embed web/dist, serve / as static, NoRoute to index.html for SPA history mode
- [ ] If !cfg.Server.API.Enabled: skip API routes entirely
- [ ] Write embed.go: package main, //go:embed web/dist/*, var webFS embed.FS
- [ ] go build ./internal/router/

---

## Task 12: CLI Commands (Cobra)

**Files:** cmd/root.go, cmd/passwd.go, cmd/logs.go, modify main.go

- [ ] go get github.com/spf13/cobra
- [ ] Write root.go: var rootCmd = cobra.Command{Use: cronix, Short: ...}, Execute() calls rootCmd.Execute()
- [ ] Add serve subcommand: loads config, inits DB, creates scheduler+executor, starts Gin, handles SIGINT/SIGTERM graceful shutdown(30s timeout, stop scheduler, wait executor, close DB)
- [ ] Add serve flags: -c/--config (default config.yaml)
- [ ] Write passwd.go: cronix passwd subcommand, prompt username+password, bcrypt hash, write to config.yaml auth.password
- [ ] Write logs.go: cronix logs subcommand with flags --follow, --task, --status, --since, --last. Opens DB read-only, queries execution_logs, for --follow tails with polling or DB change detection. Uses zerolog console writer for colored output.
- [ ] Modify main.go: import cmd, call cmd.Execute()
- [ ] go build -o cronix.exe .

---

## Task 13: Notifier + Cache

**Files:** internal/notify/notifier.go, internal/service/notify_service.go, internal/cache/cache.go

- [ ] Write Notifier struct{notifyCh chan NotifyEvent}
- [ ] NotifyEvent: TaskName, Status, Config(NotifyConfig)
- [ ] Start(ctx): consume notifyCh, for each event: if webhook: http.Post(webhookURL, JSON payload{task,status,time}), if email: SMTP send (configurable)
- [ ] Retry logic: 3 attempts with interval
- [ ] Write NotifyService: Enqueue(task, status, config), write to notifyCh
- [ ] Write Cache[T any] struct{mu, data, ttl, lastUpdate}
- [ ] Get(key): check TTL, return value+bool
- [ ] Set(key, value): store with timestamp
- [ ] go build ./internal/notify/ ./internal/cache/

---

## Task 14: Wire executors into Executor.runTaskByType

- [ ] In executor.go runTaskByType: switch task.TaskType, call appropriate executor
- [ ] shell: ExecuteShell with command, workDir, timeoutSec
- [ ] http: ExecuteHTTP with method, url, headers, body, authType, authConfig, timeout, cbThreshold, cbCooldown
- [ ] cleanup: ExecuteCleanup with command (JSON config)
- [ ] healthcheck: ExecuteHealthCheck with http_url, timeout
- [ ] For each: update execLog fields (Status, ExitCode, Output truncated to cfg.OutputTruncateKB, ErrorMsg, EndTime), db.Save
- [ ] Wire NotifyService: after save, Enqueue notify events
- [ ] go build ./...

---



## Task 15: Frontend Scaffold (Vue3 + Element Plus + Vite)

**Files:** web/package.json, vite.config.ts, index.html, src/main.ts, App.vue, router/index.ts, api/request.ts, api/auth.ts

- [ ] Create web/package.json: vue3, vue-router, element-plus, @element-plus/icons-vue, axios, vite, @vitejs/plugin-vue, typescript
- [ ] Create vite.config.ts: vue plugin, server proxy /api to http://localhost:8080, build outDir dist
- [ ] Create index.html: standard Vite template, mount #app
- [ ] Create src/main.ts: createApp, use ElementPlus, use router, mount
- [ ] Create src/App.vue: el-container with sidebar(el-menu: Dashboard/Tasks/Logs/Settings) + router-view
- [ ] Create src/router/index.ts: routes / -> Dashboard, /login -> Login, /tasks -> TaskList, /tasks/:id -> TaskEdit, /logs -> ExecutionLogs, /settings -> Settings
- [ ] Create src/api/request.ts: axios instance with baseURL /api, request interceptor(add Bearer token from localStorage), response interceptor(401 -> redirect /login)
- [ ] Create src/api/auth.ts: login(username, password) POST /api/login
- [ ] npm install && npm run dev (verify Vite starts)

---

## Task 16: Frontend Pages

**Files:** web/src/views/Login.vue, Dashboard.vue, TaskList.vue, TaskEdit.vue, ExecutionLogs.vue, Settings.vue. web/src/components/CronInput.vue, DepSelector.vue, LogViewer.vue. web/src/api/task.ts, log.ts, dashboard.ts

- [ ] Create api/task.ts: getTasks, createTask, getTask, updateTask, deleteTask, runTask, getTaskLogs, getTaskDeps, updateTaskDeps
- [ ] Create api/log.ts: getLogs(params)
- [ ] Create api/dashboard.ts: getStats()
- [ ] Create Login.vue: el-card + el-form(username/password) + el-button submit, call auth.login, store token, router.push /
- [ ] Create Dashboard.vue: el-row with el-statistic cards(total tasks, today runs, success, failed), el-table of recent executions, auto-refresh 30s
- [ ] Create TaskList.vue: el-input search + el-button create + el-table(tasks) with columns(name, cron, type, enabled switch, status, actions: edit/delete/run), pagination
- [ ] Create TaskEdit.vue: el-form with all task fields, tabs: Basic(name,cron,type,desc) / Command(shell command or http fields conditional on type) / Advanced(timeout,retry,max_concurrent,work_dir) / Dependencies(multi-select transfer) / Notifications(on_success,on_failure,webhook_url,email_to). CronInput component: 6-field input with human-readable preview. DepSelector: el-transfer with available tasks. Submit save/update.
- [ ] Create ExecutionLogs.vue: el-input task_name filter + el-select status filter + el-date-picker time range + el-table(logs) with expandable output row, auto-refresh button
- [ ] Create Settings.vue: el-form with config sections(executor pool_size, output_truncate, log retention/max_records, notify retry/interval, circuit_breaker threshold/cooldown), save updates config
- [ ] Create CronInput.vue: 6 el-input-number fields (sec min hour day month weekday), computed humanReadable string
- [ ] Create DepSelector.vue: el-transfer with all available tasks as options, v-model selected deps
- [ ] Create LogViewer.vue: pre/code block with monospace, collapsible for long output, status tag color
- [ ] npm run build (verify dist/ created)

---

## Task 17: Integration Build

**Files:** modify embed.go, final wiring

- [ ] Ensure embed.go: //go:embed web/dist var WebDist embed.FS
- [ ] In router.go: use http.FS for embed, strip web/dist prefix
- [ ] Configure zerolog: console writer for stdout(colored) + lumberjack file writer(JSON) + MultiLevelWriter
- [ ] Add startup log: print config summary, mode(full/api-only/headless), port
- [ ] In root.go serve: add memory limit debug.SetMemoryLimit, disk usage check goroutine
- [ ] Add zerolog recovery in Gin middleware: log panics with stack trace
- [ ] go mod tidy && go build -ldflags='-s -w' -o cronix.exe .

---

## Task 18: End-to-End Validation

- [ ] Start server: ./cronix.exe serve
- [ ] Test login: curl POST /api/login
- [ ] Test CRUD: curl POST/GET/PUT/DELETE /api/tasks
- [ ] Create shell task: echo test, manual run, verify log
- [ ] Create HTTP task: GET httpbin.org/get, manual run
- [ ] Create DAG: task B depends on A, manual run A, verify B triggers
- [ ] Test headless mode: set webui.enabled=false, api.enabled=false, verify no port open
- [ ] Test CLI: cronix passwd, cronix logs --last 10
- [ ] Build frontend and test Dashboard in browser: login, create task, view logs

---

## Task 19: (Optional) Polish + README

- [ ] Write README.md: features, quick start, config reference, API docs
- [ ] Add Makefile: build, dev, clean targets
- [ ] Add Dockerfile: multi-stage build (go + node -> alpine binary)

