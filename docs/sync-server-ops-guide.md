# Sync Server Operator's Guide

This guide covers running, deploying, monitoring, and maintaining the `td-sync` server.

## Architecture

```
Client (td CLI)
  │
  ▼
┌──────────────────────────────────────────┐
│  td-sync HTTP server (:8080)             │
│  ├─ Middleware: recovery, request ID,    │
│  │    logging, metrics, rate limiting    │
│  ├─ Auth: device flow + API keys         │
│  ├─ Sync: push/pull event replication    │
│  └─ Projects: CRUD + membership          │
├──────────────────────────────────────────┤
│  ServerDB (SQLite)  │  Per-project DBs   │
│  users, keys, auth  │  event logs        │
├──────────────────────────────────────────┤
│  Litestream (continuous replication)     │
│  └─ File or S3 replica                   │
└──────────────────────────────────────────┘
```

The server is a single Go binary (`td-sync`) backed by SQLite. One database stores server metadata (users, API keys, projects, memberships). Each project gets its own SQLite database for event logs. All databases use WAL mode.

## Local Development

### Build and run

```bash
# Build the server
go build -o td-sync ./cmd/td-sync

# Run with defaults (listens on :8080, stores data in ./data/)
./td-sync

# Run with custom config
SYNC_LISTEN_ADDR=:9090 \
SYNC_SERVER_DB_PATH=./mydata/server.db \
SYNC_PROJECT_DATA_DIR=./mydata/projects \
SYNC_LOG_FORMAT=text \
SYNC_LOG_LEVEL=debug \
./td-sync
```

### Verify it's running

```bash
curl http://localhost:8080/healthz
# {"status":"ok"}
```

### Test the full auth + sync flow locally

```bash
# 1. Start server
./td-sync &

# 2. Login from td CLI (uses localhost:8080 by default)
td auth login
# Enter email, then open the verification URL in browser and enter the code

# 3. Create a remote project
td sync-project create "my-project"
# Note the project ID

# 4. Link local project to remote
td sync-project link <project-id>

# 5. Push local changes
td sync --push

# 6. Pull remote changes
td sync --pull

# 7. Check status
td sync --status
```

### Run tests

```bash
go test ./...
go test ./internal/api/...         # Server tests
go test ./internal/sync/...        # Sync engine tests
go test ./internal/serverdb/...    # Server DB tests
```

## Configuration

All config is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `SYNC_LISTEN_ADDR` | `:8080` | Address to bind |
| `SYNC_SERVER_DB_PATH` | `./data/server.db` | Server metadata DB path |
| `SYNC_PROJECT_DATA_DIR` | `./data/projects` | Directory for per-project event DBs |
| `SYNC_SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown timeout |
| `SYNC_ALLOW_SIGNUP` | `true` | Allow new user registration via device auth |
| `SYNC_BASE_URL` | `http://localhost:8080` | Public URL for device auth verification links |
| `SYNC_LOG_FORMAT` | `json` | Log format: `json` or `text` |
| `SYNC_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

## Docker Deployment

### Quick start

```bash
cd deploy
cp .env.example .env
# Edit .env as needed
docker compose up -d
```

### What happens on startup

The Docker entrypoint (`deploy/entrypoint.sh`) does two things:

1. **Restore from backup** -- if a Litestream replica exists (e.g., after deploying to a new host), it restores `server.db` before the server starts.
2. **Start with replication** -- Litestream wraps the `td-sync` process, continuously replicating the database to the configured replica target.

### Docker Compose config

```yaml
services:
  td-sync:
    build:
      context: ..
      dockerfile: deploy/Dockerfile
    ports:
      - "${SYNC_LISTEN_PORT:-8080}:8080"
    environment:
      - SYNC_LISTEN_ADDR=:8080
      - SYNC_SERVER_DB_PATH=/data/server.db
      - SYNC_PROJECT_DATA_DIR=/data/projects
      - SYNC_ALLOW_SIGNUP=${SYNC_ALLOW_SIGNUP:-true}
      - SYNC_BASE_URL=${SYNC_BASE_URL:-http://localhost:8080}
      - SYNC_SHUTDOWN_TIMEOUT=${SYNC_SHUTDOWN_TIMEOUT:-30s}
    volumes:
      - td-data:/data
      - td-backups:/backups
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
```

### Volumes

- **`td-data`** -- Server DB and per-project event databases. This is the primary data.
- **`td-backups`** -- Litestream file-based replicas. Used for local backup by default.

### Environment file

Copy `deploy/.env.example` to `deploy/.env`:

```bash
SYNC_LISTEN_PORT=8080
SYNC_ALLOW_SIGNUP=true
SYNC_BASE_URL=https://sync.example.com   # Set to your public URL
SYNC_SHUTDOWN_TIMEOUT=30s
```

## Production Deployment

This section walks through deploying td-sync to a VPS with SSL and nginx.

### Prerequisites

- A VPS running Ubuntu 20.04+ (or similar Linux distro)
- A domain name with DNS access
- SSH access to the VPS as root (or sudo user)
- Docker and Docker Compose on the VPS

### 1. DNS Setup

Create an A record pointing your subdomain to your VPS IP:

```bash
# Example using DigitalOcean CLI
doctl compute domain records create example.com \
  --record-type A \
  --record-name sync \
  --record-data <VPS_IP> \
  --record-ttl 300

# Verify (may take a few minutes to propagate)
dig sync.example.com +short
```

For other DNS providers, add an A record through their web interface.

### 2. Install Dependencies (on VPS)

```bash
ssh root@<VPS_IP>

# Install Docker
curl -fsSL https://get.docker.com | sh
systemctl enable docker

# Install certbot and nginx
apt update && apt install -y certbot python3-certbot-nginx nginx
```

### 3. SSL Certificate

Stop any services using port 80, then get a certificate:

```bash
# Stop nginx/apache if running
systemctl stop nginx
systemctl stop apache2 2>/dev/null

# Get certificate (replace with your domain and email)
certbot certonly --standalone \
  -d sync.example.com \
  --non-interactive \
  --agree-tos \
  --email you@example.com

# Enable auto-renewal
systemctl enable --now certbot.timer
```

### 4. nginx Configuration

Create `/etc/nginx/sites-available/td-sync`:

```nginx
server {
    listen 80;
    server_name sync.example.com;

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 301 https://$server_name$request_uri;
    }
}

server {
    listen 443 ssl http2;
    server_name sync.example.com;

    ssl_certificate /etc/letsencrypt/live/sync.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sync.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    access_log /var/log/nginx/td-sync.access.log;
    error_log /var/log/nginx/td-sync.error.log;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
        client_max_body_size 12M;
    }
}
```

Enable and start nginx:

```bash
ln -sf /etc/nginx/sites-available/td-sync /etc/nginx/sites-enabled/
nginx -t && systemctl start nginx
```

### 5. Deploy the Application

Use the deploy script (see [Multi-Environment Deployment](#multi-environment-deployment) for details):

```bash
# 1. Copy and configure the environment template
cp deploy/envs/.env.staging.example deploy/envs/.env.staging

# 2. Edit with your values
#    - DEPLOY_HOST: Your VPS IP or hostname
#    - DEPLOY_USER: SSH user (e.g., root or deploy)
#    - DEPLOY_PATH: Remote path (e.g., /opt/td-sync)
#    - SYNC_BASE_URL: Your public URL
vim deploy/envs/.env.staging

# 3. Deploy
./deploy/deploy.sh staging --build
```

The deploy script handles rsync, environment config, and docker compose automatically.

### 6. Firewall Configuration

```bash
ufw allow 22/tcp   # SSH
ufw allow 80/tcp   # HTTP (for SSL renewal)
ufw allow 443/tcp  # HTTPS
ufw --force enable
ufw status
```

### 7. Verification

Run these checks to confirm the deployment:

```bash
# DNS resolves correctly
dig sync.example.com +short

# Health check returns OK
curl -s https://sync.example.com/healthz
# Expected: {"status":"ok"}

# Auth verification page loads
curl -s -o /dev/null -w "%{http_code}" https://sync.example.com/auth/verify
# Expected: 200

# Container is healthy
docker compose ps
# Expected: STATUS shows "healthy"
```

Test the full auth flow from a client machine:

```bash
td config set sync.url https://sync.example.com
td auth login
```

### 8. Post-Deployment Security

After creating your initial user account, disable public signups by editing your local env file:

```bash
# Edit deploy/envs/.env.staging (or .env.prod)
# Change: SYNC_ALLOW_SIGNUP=true -> SYNC_ALLOW_SIGNUP=false

# Re-deploy
./deploy/deploy.sh staging
```

### Updating the Server

To deploy updates:

```bash
./deploy/deploy.sh staging

# Or force a rebuild:
./deploy/deploy.sh staging --build
```

### Viewing Logs

```bash
# Quick status check (container status + recent logs + health)
./deploy/deploy.sh staging --status

# Tail container logs after deploy
./deploy/deploy.sh staging --logs

# On VPS directly
ssh root@<VPS_IP>
cd /opt/td-sync/deploy && docker compose logs -f td-sync

# nginx logs
tail -f /var/log/nginx/td-sync.access.log
tail -f /var/log/nginx/td-sync.error.log
```

## Multi-Environment Deployment

The deploy system supports dev, staging, and production environments with a unified deployment script.

### Quick Start

```bash
# 1. Copy the environment template
cp deploy/envs/.env.prod.example deploy/envs/.env.prod

# 2. Fill in your values (DEPLOY_HOST, S3 credentials, etc.)
vim deploy/envs/.env.prod

# 3. Deploy
./deploy/deploy.sh prod
```

### Environments

| Environment | Purpose | Runs On | S3 Backup |
|-------------|---------|---------|-----------|
| dev | Local development | localhost | No |
| staging | Pre-production testing | VPS | Optional |
| prod | Production | VPS | Required |

### Deploy Script

```bash
./deploy.sh <env> [options]

Environments: dev, staging, prod

Options:
  --build      Force Docker rebuild
  --logs       Tail logs after deployment
  --dry-run    Validate config only
  --status     Check deployment status
  --stop       Stop the deployment
```

### Environment Configuration

Each environment has a template in `deploy/envs/`:

- `.env.dev.example` - Local development (relaxed rate limits, debug logging)
- `.env.staging.example` - Staging VPS (production-like settings)
- `.env.prod.example` - Production VPS (strict settings, S3 backup required)

Copy the template to `.env.<env>` and fill in your values. The actual `.env` files are gitignored to protect secrets.

### Required Variables

**All environments:**
- `SYNC_BASE_URL` - Public URL of the server

**Remote environments (staging, prod):**
- `DEPLOY_HOST` - VPS hostname or IP
- `DEPLOY_USER` - SSH user for deployment
- `DEPLOY_PATH` - Remote path (e.g., `/opt/td-sync`)

**Production only:**
- `LITESTREAM_S3_BUCKET` - S3 bucket for backups
- `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` - AWS credentials

### Adding a New Environment

See `deploy/envs/README.md` for instructions on creating additional environments.

## Backup and Recovery

### Default: file-based replica

Litestream continuously replicates `server.db` to `/backups/server.db` inside the container. The `td-backups` volume persists this across container restarts.

### S3-compatible storage

To replicate to S3 (or any S3-compatible store like MinIO, R2, etc.):

1. Edit `deploy/litestream.yml` -- uncomment the S3 replica section
2. Set environment variables in `.env`:

```bash
LITESTREAM_S3_BUCKET=my-td-backups
LITESTREAM_S3_ENDPOINT=https://s3.us-east-1.amazonaws.com
AWS_DEFAULT_REGION=us-east-1
AWS_ACCESS_KEY_ID=AKIA...
AWS_SECRET_ACCESS_KEY=...
```

3. Restart the container.

### Manual restore

If you need to restore from a replica onto a fresh host:

```bash
# Litestream restore from S3
litestream restore -config /etc/litestream.yml /data/server.db

# Or copy the file replica directly
cp /backups/server.db /data/server.db
```

The entrypoint does this automatically on first boot (`restore -if-replica-exists`).

### What's backed up

Only `server.db` (users, keys, projects, memberships) is replicated by Litestream. Per-project event databases under `/data/projects/` are not replicated by default. For production, consider:

- Adding each project DB to `litestream.yml`
- Volume-level snapshots
- Periodic `sqlite3 .backup` cron jobs

## Observability

### Health check

```bash
curl http://localhost:8080/healthz
# {"status":"ok"}    -- server and DB accessible
# 500 response       -- DB unreachable
```

Docker Compose runs this every 30s with 3 retries.

### Metrics

```bash
curl http://localhost:8080/metricz
```

Returns JSON with atomic counters:

```json
{
  "uptime_seconds": 3600,
  "requests": 15420,
  "server_errors": 2,
  "client_errors": 45,
  "push_events_accepted": 8930,
  "pull_requests": 6200
}
```

These are simple counters, not histograms. They reset on restart. Useful for basic dashboards and alerting.

### Structured logging

Every request is logged with:

- **Request ID** -- 16-byte hex, passed through the request lifecycle
- **Method and path**
- **Status code**
- **Duration**
- **User ID** (if authenticated)
- **Project ID** (if applicable)

JSON format (default) for machine parsing:

```json
{"level":"INFO","msg":"request","request_id":"a1b2c3...","method":"POST","path":"/v1/projects/abc/sync/push","status":200,"duration_ms":12,"user_id":"u-123"}
```

Text format for local dev:

```
SYNC_LOG_FORMAT=text SYNC_LOG_LEVEL=debug ./td-sync
```

### What to alert on

| Condition | How to detect |
|---|---|
| Server down | Health check fails (`/healthz` returns non-200) |
| High error rate | `server_errors` counter increasing rapidly |
| Push failures | Monitor `push_events_accepted` growth stalling |
| Auth issues | `client_errors` spike (401/403 responses) |
| Rate limiting | 429 status codes in logs |

## Rate Limiting

Fixed-window (1-minute) rate limits are applied per key:

| Endpoint pattern | Limit | Keyed by |
|---|---|---|
| `/auth/*`, `/v1/auth/*` | 10/min | Client IP |
| `/v1/projects/*/sync/push` | 60/min | API key ID |
| `/v1/projects/*/sync/pull` | 120/min | API key ID |
| All other authenticated routes | 300/min | API key ID |

When a limit is hit, the server returns `429 Too Many Requests`. Stale rate limit buckets are cleaned up every 5 minutes.

## API Endpoints Reference

### Public

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Health check |
| `GET` | `/metricz` | Metrics snapshot |
| `POST` | `/v1/auth/login/start` | Start device auth |
| `POST` | `/v1/auth/login/poll` | Poll for auth completion |
| `GET` | `/auth/verify` | Verification page (HTML) |
| `POST` | `/auth/verify` | Submit verification code |

### Authenticated (Bearer token)

| Method | Path | Role | Description |
|---|---|---|---|
| `POST` | `/v1/projects` | any | Create project |
| `GET` | `/v1/projects` | any | List user's projects |
| `GET` | `/v1/projects/{id}` | reader+ | Get project |
| `PATCH` | `/v1/projects/{id}` | writer+ | Update project |
| `DELETE` | `/v1/projects/{id}` | owner | Delete project |
| `POST` | `/v1/projects/{id}/members` | owner | Add member |
| `GET` | `/v1/projects/{id}/members` | reader+ | List members |
| `PATCH` | `/v1/projects/{id}/members/{uid}` | owner | Update role |
| `DELETE` | `/v1/projects/{id}/members/{uid}` | owner | Remove member |
| `POST` | `/v1/projects/{id}/sync/push` | writer+ | Push events |
| `GET` | `/v1/projects/{id}/sync/pull` | reader+ | Pull events |
| `GET` | `/v1/projects/{id}/sync/status` | reader+ | Sync status |

### Roles

- **owner** -- full control, can manage members and delete project
- **writer** -- can push and pull events
- **reader** -- read-only, can only pull events

## Database Details

### Server DB schema

Tables: `users`, `auth_requests`, `api_keys`, `projects`, `memberships`

SQLite pragmas: WAL mode, `synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=ON`

### Per-project event DBs

Each project gets `<PROJECT_DATA_DIR>/<project-id>/events.db` with a single `events` table:

- `server_seq` -- auto-increment primary key (global ordering)
- `device_id`, `session_id`, `client_action_id` -- client provenance
- `action_type` -- `create`, `update`, `delete`, `soft_delete`
- `entity_type`, `entity_id` -- what was changed
- `payload` -- full JSON snapshot
- `client_timestamp`, `server_timestamp`
- Unique constraint on `(device_id, session_id, client_action_id)` prevents duplicate pushes

### Expired auth cleanup

A background goroutine runs every 5 minutes, deleting auth requests older than their TTL (15 minutes by default).

## Shutdown Behavior

On `SIGINT` or `SIGTERM`:

1. Stop accepting new connections
2. Wait up to `SYNC_SHUTDOWN_TIMEOUT` for in-flight requests
3. Close server DB and all project DB connections
4. WAL checkpoint (TRUNCATE) on shutdown for clean state
5. Litestream flushes final replication before exit

## Security Considerations

- Set `SYNC_ALLOW_SIGNUP=false` after initial user registration to lock down the server
- API keys expire after 1 year by default
- Device auth codes use a human-friendly charset (no 0/1/I/L/O) and expire after 15 minutes
- Request bodies capped at 10 MB
- All database inputs are parameterized (no SQL injection)
- Auth endpoints are rate-limited by IP to prevent brute force
