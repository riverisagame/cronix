# Cronix

High-performance task scheduler — crontab replacement with Web Dashboard.

Supports Shell, HTTP, Cleanup, and Healthcheck task types with DAG-based dependencies.

## Quick Start

```bash
# 1. Set admin password
./cronix passwd -c config.yaml

# 2. Start server
./cronix serve -c config.yaml

# 3. Open browser
# http://localhost:8080
```

## Docker

```bash
docker build -t cronix .
docker run -d -p 8080:8080 \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/config.yaml:/app/config.yaml \
  cronix
```

## TLS (Production)

```yaml
# config.yaml
server:
  tls:
    enabled: true
    cert_file: /etc/ssl/cert.pem
    key_file: /etc/ssl/key.pem
```

## CLI

| Command | Description |
|---------|-------------|
| `cronix serve` | Start the server |
| `cronix passwd` | Set admin password |
| `cronix logs` | View log management |

## API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/health` | No | Health check |
| POST | `/api/login` | No | Login, returns JWT |
| GET | `/api/tasks` | Yes | List tasks |
| POST | `/api/tasks` | Yes | Create task |
| GET | `/api/tasks/:id` | Yes | Get task |
| PUT | `/api/tasks/:id` | Yes | Update task |
| DELETE | `/api/tasks/:id` | Yes | Delete task |
| POST | `/api/tasks/:id/run` | Yes | Manual trigger |
| GET | `/api/tasks/:id/logs` | Yes | Task execution logs |
| GET | `/api/tasks/:id/deps` | Yes | Task dependencies |
| PUT | `/api/tasks/:id/deps` | Yes | Update dependencies |
| GET | `/api/logs` | Yes | All execution logs |
| GET | `/api/dashboard/stats` | Yes | Dashboard statistics |
| GET | `/api/settings` | Yes | Read settings |
| PUT | `/api/settings` | Yes | Update settings |

## Task Types

| Type | Description | Config Fields |
|------|-------------|---------------|
| `shell` | Shell command | `command`, `work_dir` |
| `http` | HTTP request | `http_method`, `http_url`, `http_auth_type` |
| `cleanup` | File cleanup | `command` (JSON config) |
| `healthcheck` | HTTP health check | `http_url` |

## Configuration

```yaml
server:
  port: 8080
  tls:
    enabled: false
  webui:
    enabled: true
  api:
    enabled: true

auth:
  username: admin
  password: ""          # Set via: cronix passwd
  jwt_secret: ""        # Auto-generated on first start

executor:
  pool_size: 32
  output_truncate_kb: 64
  memory_limit_mb: 512
  cpu_quota: 50           # Max CPU usage quota (%)
  enable_cgroups: false   # Enable cgroups isolation limit
  nice_value: 19          # Nice value (CPU scheduling priority, 19 is lowest)
  ionice_class: 3         # I/O scheduling class (3 is Idle)

log:
  retention_days: 30
  max_records: 100000
  max_logs_per_task: 1000 # Max DB logs per task (prevent log pollution)
  file_max_size_mb: 50    # Max disk task log size
  file_max_backups: 5     # Max disk log backups
  file_max_age_days: 30   # Max days to keep disk logs
  min_free_disk_space_percent: 10 # Min free disk space percentage (safety valve)
  min_free_disk_space_gb: 10      # Min free disk space GB

circuit_breaker:
  failure_threshold: 5
  cooldown_seconds: 60
```

## Architecture

```
cmd/           CLI commands (Cobra)
internal/
  config/      YAML config (Viper + hot-reload)
  database/    SQLite via GORM
  handler/     HTTP handlers (Gin)
  middleware/  JWT auth + rate limiting
  model/       GORM models
  executor/    Task executors (shell/http/cleanup/healthcheck)
  scheduler/   Cron engine (robfig/cron) + DAG + ants pool
  circuit/     Circuit breaker for HTTP tasks
  service/     Business logic layer
  router/      Gin router + SPA serving
web/           Vue 3 + Element Plus frontend (Vite)
```

## Deployment (Linux Production)

### 方案 1: Systemd + Nginx（推荐）

最轻量、最稳定的生产方案。systemd 负责进程守护和崩溃自愈，Nginx 负责 TLS 和反向代理。

```bash
# 方式 A: curl 一键安装（推荐）
curl -fsSL https://raw.githubusercontent.com/riverisagame/cronix/master/deploy/install.sh | sudo bash

# 方式 B: 克隆后本地安装
sudo bash deploy/install.sh

# 设置密码
sudo -u cronix /opt/cronix/cronix passwd -c /opt/cronix/config.yaml

# 启动
sudo systemctl start cronix
sudo systemctl enable cronix

# 日常运维
sudo systemctl status cronix          # 查看状态
sudo journalctl -u cronix -f          # 实时日志
sudo systemctl restart cronix         # 重启
```

配合反向代理 + TLS（二选一）：

**选项 A: 宝塔面板（推荐新手）**

1. **创建网站** → 左侧「网站」→「添加站点」，域名填 `cronix.example.com`，PHP 选「纯静态」
2. **反向代理** → 网站设置 →「反向代理」→「添加反向代理」:
   ```
   代理名称: cronix
   目标URL:  http://127.0.0.1:8080
   发送域名: $host
   ```
3. **SSL 证书** → 网站设置 →「SSL」→「Let's Encrypt」→ 申请并开启「强制 HTTPS」
4. **静态资源缓存** → 网站设置 →「配置文件」，在 `location /` 前插入:
   ```nginx
   location /assets/ {
       proxy_pass http://127.0.0.1:8080;
       expires 30d;
       add_header Cache-Control "public, immutable";
   }
   ```

**选项 B: 纯 Nginx（无面板）**

```bash
sudo apt install nginx certbot python3-certbot-nginx
sudo certbot --nginx -d cronix.example.com
sudo cp deploy/nginx-cronix.conf /etc/nginx/sites-available/cronix
sudo ln -s /etc/nginx/sites-available/cronix /etc/nginx/sites-enabled/
sudo systemctl reload nginx
```

配置日志轮转：

```bash
sudo cp deploy/cronix-logrotate /etc/logrotate.d/cronix
```

### 方案 2: Docker Compose

```bash
cd deploy && docker compose up -d
```

### 方案 3: 裸二进制

```bash
./cronix passwd -c config.yaml
nohup ./cronix serve -c config.yaml &>/var/log/cronix.log &
```

### 方案 4: K8s / 自建服务器

```bash
# 交叉编译
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o cronix-linux .
# 上传到服务器 scp cronix-linux config.yaml user@server:/opt/cronix/
```

## Build

```bash
# Frontend
cd web && npm install && npm run build

# Backend
go build -o cronix .

# Linux cross-compile
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o cronix-linux .
```

## Security

- Password: bcrypt hashed, stored in config.yaml
- JWT: HS256 with auto-generated 256-bit random key
- Rate limiting: 5 login attempts per IP per minute
- Input validation: task name required, page_size capped
- Auth required on all `/api/*` routes (except `/api/health`, `/api/login`)

## Production Checklist

- [x] JWT secret auto-generated and persisted
- [x] Password required before server starts
- [x] TLS support configurable
- [x] Login rate limiting (5/min/IP)
- [x] Task retry with configurable count and interval
- [x] Atomic cron reload with rollback
- [x] Thread-safe config access (RWMutex)
- [x] Input validation (name required, page_size max)
- [x] Health check endpoint
- [x] Docker multi-stage build
- [x] Graceful shutdown
- [x] Panic recovery in all goroutines
- [x] Circuit breaker for HTTP tasks
- [x] Execution log cleanup (retention + max records)
- [x] DAG cycle detection with rollback
- [x] Linux CPU/IO scheduling prioritization (nice/ionice) & cgroups limits fallback
- [x] Streaming disk logging with rotation & gzip compression & expiration
- [x] Disk space safety valve protection
- [x] Task-level database log quota isolation
