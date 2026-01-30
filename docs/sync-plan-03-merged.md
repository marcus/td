# td-sync: Specification (v3 — Merged)

## Overview

Sync server for td task databases. Enables multi-device and multi-user collaboration while preserving local-first SQLite architecture.

### Goals

- Minimal changes to td client codebase
- Local SQLite remains primary; sync is additive
- Self-hostable with identical functionality
- Free or near-free to operate at small scale
- Manageable by a solo developer

### Non-Goals

- Real-time collaboration (near-real-time is sufficient)
- Offline conflict resolution UI (last-write-wins acceptable)
- Mobile clients (CLI/TUI only)
- Replacing local SQLite with a remote database
- Strong global serializability across concurrent writers

### Terminology

| Term | Definition |
|------|-----------|
| Client | td CLI/TUI running on a device |
| Server | Cloud service providing auth, authorization, and sync APIs |
| Project | A single td database instance (a local SQLite file) representing a task space |
| Event | An immutable change record representing a mutation to project state |
| Action Log | Client-side append-only table that records events |
| Device | A specific client installation instance |
| Session | A sequence of operations performed by a device (used for idempotency) |
| server_seq | Monotonic per-project ordering assigned by the server |

---

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│ td client A │     │ td client B │     │ td client C │
│ (SQLite)    │     │ (SQLite)    │     │ (SQLite)    │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           │
                           ▼
                 ┌───────────────────┐
                 │   td-sync server  │
                 │   (Go)            │
                 └─────────┬─────────┘
                           │
              ┌────────────┴────────────┐
              ▼                         ▼
       ┌─────────────┐          ┌─────────────┐
       │  auth.db    │          │  events.db  │
       │  (SQLite)   │          │  (SQLite)   │
       └─────────────┘          └─────────────┘
              │                         │
              └────────────┬────────────┘
                           ▼
                    ┌─────────────┐
                    │ Litestream  │
                    │ → S3/R2/B2  │
                    └─────────────┘
```

### Components

| Component | Technology | Purpose |
|-----------|------------|---------|
| Sync server | Go (single binary) | HTTP API for auth and sync |
| Auth database | SQLite | Users, API keys, projects, memberships |
| Events database | SQLite | Append-only event log per project |
| State cache | SQLite per project | Materialized view for web/API reads |
| Backup | Litestream | Continuous replication to object storage |

---

## Database Schemas

### auth.db

```sql
-- Users
CREATE TABLE users (
    id TEXT PRIMARY KEY,              -- ulid
    email TEXT UNIQUE NOT NULL,
    email_verified_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- API Keys
CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,              -- ulid
    user_id TEXT NOT NULL REFERENCES users(id),
    key_hash TEXT NOT NULL,           -- sha256(key)
    key_prefix TEXT NOT NULL,         -- first 8 chars for identification
    name TEXT DEFAULT '',             -- user-provided label
    scopes TEXT DEFAULT 'sync',       -- comma-separated: sync,admin
    expires_at DATETIME,
    last_used_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(key_hash)
);

-- Projects
CREATE TABLE projects (
    id TEXT PRIMARY KEY,              -- ulid
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

-- Project Memberships
CREATE TABLE memberships (
    project_id TEXT NOT NULL REFERENCES projects(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    role TEXT NOT NULL,               -- owner, writer, reader
    invited_by TEXT REFERENCES users(id),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (project_id, user_id)
);

-- Sync Cursors (tracks each client's sync position)
CREATE TABLE sync_cursors (
    project_id TEXT NOT NULL REFERENCES projects(id),
    client_id TEXT NOT NULL,          -- unique per device
    last_event_id BIGINT DEFAULT 0,
    last_sync_at DATETIME,
    PRIMARY KEY (project_id, client_id)
);

CREATE INDEX idx_api_keys_user ON api_keys(user_id);
CREATE INDEX idx_api_keys_prefix ON api_keys(key_prefix);
CREATE INDEX idx_memberships_user ON memberships(user_id);
```

### events.db

```sql
-- Events (append-only log of all changes)
CREATE TABLE events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL,
    device_id TEXT NOT NULL,          -- originating device
    session_id TEXT NOT NULL,         -- originating session (for idempotency)
    client_action_id INTEGER NOT NULL,-- action_log.id from client
    action_type TEXT NOT NULL,        -- create, update, delete, etc.
    entity_type TEXT NOT NULL,        -- issue, log, handoff, etc.
    entity_id TEXT NOT NULL,
    payload JSON NOT NULL,            -- {schema_version, previous_data, new_data}
    client_timestamp DATETIME NOT NULL,
    server_timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, device_id, session_id, client_action_id)
);

CREATE INDEX idx_events_project_id ON events(project_id, id);
CREATE INDEX idx_events_project_entity ON events(project_id, entity_type, entity_id);
```

### State Cache (per project)

Located at: `/data/projects/{project_id}/state.db`

Schema: **Identical to td client schema** (issues, logs, handoffs, etc.)

Purpose: Materialized view for read-only web access and fast client bootstrap.

---

## Client Specification

### Identity

- `device_id`: stable UUID generated on first run, stored locally.
- `session_id`: UUID generated per process start or per sync cycle.
- `project_id`: stable UUID stored in the local DB.

### Required local tables

New table in td's `.todos/db.sqlite`:

```sql
CREATE TABLE sync_state (
    project_id TEXT PRIMARY KEY,
    last_pushed_action_id INTEGER DEFAULT 0,
    last_pulled_server_seq INTEGER DEFAULT 0,
    last_sync_at DATETIME,
    sync_disabled INTEGER DEFAULT 0
);
```

New columns in `action_log`:

```sql
ALTER TABLE action_log ADD COLUMN synced_at DATETIME;
```

### Event generation

- Every local mutation MUST produce exactly one event in `action_log`.
- Events MUST be appended within the same transaction as the mutation.
- Events MUST include enough data to apply the change on another replica.
- Event payloads MUST include a `schema_version` field.

### Event payload format

```json
{
  "schema_version": 1,
  "action_type": "update",
  "entity_type": "issue",
  "entity_id": "01HXK...",
  "new_data": { "title": "Fix bug", "status": "open" },
  "previous_data": { "title": "Old title", "status": "open" }
}
```

The server treats payloads as opaque for sync purposes. Payloads are interpreted only for state cache materialization and future publishing.

### Sync triggers

- **On startup**: attempt sync for all projects if authenticated.
- **On debounce after local writes**: sync after a short delay (e.g., 2s).
- **Periodically**: jittered interval (30–120s) while td is running.
- **Manual**: `td sync` command.
- Sync is opt-out per project (`sync_disabled`) and globally.

### Push protocol

Client sends events where `action_log.id > last_pushed_action_id`.

Client MUST include:
- `project_id`
- `device_id`
- `session_id`
- `client_action_id` (the local `action_log.id`)
- `payload` (JSON including `schema_version`)
- `client_timestamp`

Client MUST retry transient failures with exponential backoff.

### Pull protocol

Client requests:
- `project_id`
- `after_server_seq` = `last_pulled_server_seq`
- `limit`

Client applies returned events in ascending `server_seq` order.

### Applying remote events

- Remote event application MUST be deterministic and idempotent.
- Client MUST store the highest applied `server_seq` in `sync_state`.
- Client MUST ignore events already applied.

Events map to local database operations:

| action_type | Operation |
|-------------|-----------|
| `create` | INSERT using `new_data` |
| `update` | UPDATE using `new_data` |
| `delete` | DELETE by entity_id |
| `soft_delete` | UPDATE SET deleted_at = timestamp |

Entity types match td tables: `issue`, `log`, `handoff`, `comment`, `board`, `work_session`, etc.

### Conflict resolution

**Strategy: Last-write-wins by server arrival order.**

Events are applied in `server_seq` order. If two clients edit the same entity, the last event to arrive at the server wins.

When a remote event overwrites a locally-modified entity (i.e., the entity's `updated_at` is after `last_sync_at`), log a warning:

```go
if localUpdatedAt.After(lastSyncAt) {
    slog.Warn("sync overwrite", "entity", entityID, "type", entityType)
}
```

This provides observability into conflict frequency with minimal code. A full conflict recording table will be added in a later phase if multi-user collaboration reveals the need.

### Authentication UX

Phase 1 uses a device authorization flow (no SMTP required):

1. Client calls `POST /v1/auth/login/start`
2. Server returns `device_code`, `user_code`, `verification_uri`
3. User visits `verification_uri` and enters `user_code`
4. Client polls `POST /v1/auth/login/poll` with `device_code`
5. On success, server returns `api_key`

Credentials stored at `~/.config/td/auth.json` with `0600` permissions:

```json
{
  "api_key": "td_live_xxxxxxxx",
  "user_id": "01HXK...",
  "email": "user@example.com",
  "server_url": "https://api.td.dev",
  "device_id": "uuid",
  "expires_at": "2025-12-31T23:59:59Z"
}
```

If the server returns 401/403:
- Client enters unauthenticated state for sync
- Prompts user to re-authenticate via `td auth login`

Email-based auth (magic links) deferred to phase 2 for collaboration invites.

---

## Sync Flow

```
1. PUSH local changes
   ├── Query: SELECT * FROM action_log WHERE synced_at IS NULL ORDER BY id
   ├── POST /projects/:id/sync/push
   └── UPDATE action_log SET synced_at = now() WHERE id IN (accepted)

2. PULL remote changes
   ├── GET /projects/:id/sync/pull?after_server_seq={n}&exclude_client={device_id}
   ├── For each event:
   │   └── Apply to local database (see Event Application)
   └── UPDATE sync_state SET last_pulled_server_seq = {new_seq}
```

### Initial Sync (New Client)

Option A: **Event replay** (simple, works for small projects)
```
GET /projects/:id/sync/pull?after_server_seq=0
Apply all events to empty local database
```

Option B: **Snapshot bootstrap** (fast, for larger projects)
```
GET /projects/:id/sync/snapshot
  → Save as local .todos/db.sqlite
GET /projects/:id/sync/pull?after_server_seq={X-Snapshot-Event-Id}
  → Apply incremental events
```

Threshold: Use snapshot if event_count > 1000.

---

## API

Base URL: `https://api.td.dev/v1` (hosted) or `https://your-server/v1` (self-hosted)

### Authentication

All endpoints except auth flow endpoints require:
```
Authorization: Bearer td_live_xxxxxxxx
```

Key format: `td_live_` + 32 random alphanumeric characters from `crypto/rand`.

### Endpoints

#### Auth

```
POST /v1/auth/login/start
  Response: { device_code, user_code, verification_uri, expires_in, interval }

POST /v1/auth/login/poll
  Request:  { device_code: string }
  Response: { api_key: string, user_id: string, expires_at: datetime }
           OR { status: "pending" }

POST /v1/auth/keys/revoke
  # Revokes the calling key
  Response: { revoked: true }

POST /v1/auth/keys
  # Create additional API key
  Request:  { name?: string, scopes?: string[], expires_in?: duration }
  Response: { api_key: string, key_id: string, expires_at: datetime }

DELETE /v1/auth/keys/:key_id
  Response: { deleted: true }

GET /v1/auth/me
  Response: { user_id, email, created_at }
```

#### Projects

```
POST /v1/projects
  Request:  { name: string, description?: string }
  Response: { project: Project }

GET /v1/projects
  Response: { projects: Project[] }

GET /v1/projects/:id
  Response: { project: Project, membership: Membership }

PATCH /v1/projects/:id
  Request:  { name?: string, description?: string }
  Response: { project: Project }

DELETE /v1/projects/:id
  # Soft delete; owner only
  Response: { deleted: true }
```

#### Memberships

```
POST /v1/projects/:id/members
  # Invite user; owner only
  Request:  { email: string, role: "writer" | "reader" }
  Response: { membership: Membership, invited: boolean }

GET /v1/projects/:id/members
  Response: { members: Membership[] }

PATCH /v1/projects/:id/members/:user_id
  # Change role; owner only
  Request:  { role: "writer" | "reader" }
  Response: { membership: Membership }

DELETE /v1/projects/:id/members/:user_id
  # Remove member; owner only, cannot remove self
  Response: { removed: true }
```

#### Sync

```
POST /v1/projects/:id/sync/push
  Request: {
    device_id: string,
    session_id: string,
    events: [{
      client_action_id: integer,
      action_type: string,
      entity_type: string,
      entity_id: string,
      payload: object,       # must include schema_version
      client_timestamp: datetime
    }]
  }
  Response: {
    accepted: integer,
    last_server_seq: integer,
    acks: [{ client_action_id: integer, server_seq: integer }],
    rejected: [{
      client_action_id: integer,
      reason: string
    }]
  }

GET /v1/projects/:id/sync/pull
  Query: after_server_seq=integer  # last known seq (0 for full sync)
         limit=integer             # max events (default 1000, max 10000)
         exclude_client=string     # omit own events (optional)
  Response: {
    events: Event[],
    last_server_seq: integer,
    has_more: boolean
  }

GET /v1/projects/:id/sync/snapshot
  # Download full state.db for fast bootstrap
  Response: SQLite database file (application/x-sqlite3)
  Headers:
    X-Snapshot-Event-Id: integer  # event id at snapshot time

GET /v1/projects/:id/sync/status
  Response: {
    event_count: integer,
    last_event_at: datetime,
    snapshot_available: boolean,
    snapshot_event_id: integer
  }
```

### Error Responses

```json
{
  "error": {
    "code": "invalid_api_key",
    "message": "API key expired or invalid"
  }
}
```

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `invalid_api_key` | 401 | Missing, expired, or invalid key |
| `insufficient_scope` | 403 | Key lacks required scope |
| `not_found` | 404 | Resource doesn't exist |
| `forbidden` | 403 | User lacks permission |
| `invalid_request` | 400 | Malformed request body |
| `conflict` | 409 | Duplicate or constraint violation |
| `rate_limited` | 429 | Too many requests |

---

## Client Changes

### New Commands

```bash
# Authentication (device auth flow)
td auth login [--server URL]     # Start device auth flow
td auth logout                   # Clear local credentials
td auth status                   # Show current auth state

# Project linking
td project link <project_id>     # Link local .todos to remote project
td project unlink                # Remove remote link
td project create <name>         # Create remote project and link
td project list                  # List accessible remote projects

# Sync
td sync                          # Push and pull changes
td sync --push                   # Push only
td sync --pull                   # Pull only
td sync status                   # Show sync state

# Diagnostics
td doctor                        # Check connectivity, auth, version
```

### Configuration

File: `~/.config/td/config.json`

```json
{
  "sync": {
    "url": "https://api.td.dev",
    "enabled": true,
    "auto_sync": true
  }
}
```

Environment variables (override config file):

```bash
TD_SYNC_URL="https://api.td.dev"    # Server URL
TD_AUTH_KEY="td_live_xxxxxxxx"       # API key (overrides auth.json)
TD_SYNC_ENABLED="true"              # Enable/disable sync
TD_SYNC_AUTO="true"                 # Auto-sync (startup, periodic, debounce)
```

---

## Server Implementation

### Project Structure

```
td-sync/
├── cmd/
│   └── td-sync/
│       └── main.go
├── internal/
│   ├── api/
│   │   ├── auth.go
│   │   ├── projects.go
│   │   ├── sync.go
│   │   └── middleware.go
│   ├── db/
│   │   ├── auth.go
│   │   └── events.go
│   ├── models/
│   │   └── models.go
│   └── state/
│       └── materialized.go      # State cache management
├── migrations/
│   ├── auth/
│   └── events/
├── Dockerfile
├── docker-compose.yml
├── .env.example
└── README.md
```

### Configuration

```go
type Config struct {
    // Server
    Host          string        `env:"HOST" default:"0.0.0.0"`
    Port          int           `env:"PORT" default:"8080"`

    // Database
    AuthDBPath    string        `env:"AUTH_DB_PATH" default:"/data/auth.db"`
    EventsDBPath  string        `env:"EVENTS_DB_PATH" default:"/data/events.db"`
    StateCachePath string       `env:"STATE_CACHE_PATH" default:"/data/projects"`

    // Auth
    AuthSecret    string        `env:"AUTH_SECRET" required:"true"`
    KeyExpiry     time.Duration `env:"KEY_EXPIRY" default:"8760h"`  // 1 year

    // Features
    AllowSignup   bool          `env:"ALLOW_SIGNUP" default:"true"`
    MaxProjects   int           `env:"MAX_PROJECTS" default:"-1"`   // -1 = unlimited

    // Limits
    MaxEventPayloadBytes int    `env:"MAX_EVENT_PAYLOAD" default:"65536"`  // 64KB
    MaxPushBatchSize     int    `env:"MAX_PUSH_BATCH" default:"1000"`
}
```

### State Cache Updates

On event insert, apply to project's state.db:

```go
func (s *Server) handlePush(projectID string, events []Event) error {
    // 1. Insert into events.db
    eventIDs, err := s.eventsDB.InsertEvents(projectID, events)

    // 2. Apply to state cache
    stateDB := s.stateCache.Get(projectID)
    for _, event := range events {
        if err := applyEvent(stateDB, event); err != nil {
            // Log but don't fail; state can be rebuilt
            slog.Warn("state cache apply failed", "event", event.ID, "err", err)
        }
    }

    return nil
}
```

State cache rebuild (on corruption or new project):

```go
func (s *Server) rebuildStateCache(projectID string) error {
    events, err := s.eventsDB.GetAllEvents(projectID)
    stateDB := sqlite.OpenNew(s.stateCachePath(projectID))
    initSchema(stateDB)  // td's schema
    for _, event := range events {
        applyEvent(stateDB, event)
    }
    return nil
}
```

---

## Security

### API Key Handling

- Keys generated server-side: `td_live_` + 32 chars from `crypto/rand`
- Stored hashed: `sha256(key)`
- Transmitted: HTTPS only, `Authorization: Bearer` header
- Client storage: `~/.config/td/auth.json` with `0600` permissions

### Rate Limiting

| Endpoint | Limit |
|----------|-------|
| `/auth/*` | 10/minute per IP |
| `/sync/push` | 60/minute per API key |
| `/sync/pull` | 120/minute per API key |
| All others | 300/minute per API key |

### Authorization Matrix

| Action | Owner | Writer | Reader |
|--------|-------|--------|--------|
| Delete project | ✓ | | |
| Manage members | ✓ | | |
| Push events | ✓ | ✓ | |
| Pull events | ✓ | ✓ | ✓ |
| View project | ✓ | ✓ | ✓ |
| Create project | any authenticated user |

### Limits and Quotas

- Maximum event payload size: 64KB
- Maximum push batch size: 1,000 events
- Rate limiting per key and per IP

---

## Observability

- Structured logs with request ID, project ID, user ID (when known)
- Metrics:
  - Request count and error rates
  - Push/pull volume (events per interval)
  - Event lag per project
  - DB latency
- Sync overwrite warnings logged at WARN level for future conflict analysis

---

## Self-Hosting

### Docker Compose

```yaml
version: "3.8"

services:
  td-sync:
    image: ghcr.io/marcus/td-sync:latest
    environment:
      - AUTH_SECRET=${AUTH_SECRET}
    volumes:
      - td-data:/data
    ports:
      - "8080:8080"
    restart: unless-stopped

  litestream:
    image: litestream/litestream:latest
    volumes:
      - td-data:/data
      - ./litestream.yml:/etc/litestream.yml
    command: replicate
    restart: unless-stopped

volumes:
  td-data:
```

### Litestream Config

```yaml
dbs:
  - path: /data/auth.db
    replicas:
      - url: s3://your-bucket/td-backup/auth.db

  - path: /data/events.db
    replicas:
      - url: s3://your-bucket/td-backup/events.db
```

### Quick Start (Self-Host)

```bash
# Clone
git clone https://github.com/marcus/td-sync
cd td-sync

# Configure
cp .env.example .env
# Edit .env: set AUTH_SECRET

# Run
docker-compose up -d

# In any td project
td auth login --server https://your-server:8080
```

---

## Compatibility and Versioning

- API versioned under `/v1`
- Event payload `schema_version` included in each payload
- Client and server must accept unknown fields

---

## Roadmap

### Phase 1: Core Sync
- [ ] Server: device auth, projects, memberships, events
- [ ] Client: auth commands, sync command, auto-sync triggers
- [ ] Self-host: Docker image, Litestream, docs
- [ ] Observability: structured logging, basic metrics

### Phase 2: Polish
- [ ] Snapshot bootstrap
- [ ] `td doctor` diagnostics
- [ ] Rate limiting
- [ ] Email-based auth (magic links for collaboration invites)

### Phase 3: Collaboration
- [ ] Invite flow (email)
- [ ] Role management
- [ ] Conflict recording table (if overwrite logs show meaningful frequency)

### Phase 4: Read-Only Web
- [ ] Public project pages
- [ ] Private view (auth required)
- [ ] Embed widget

### Phase 5: Future
- [ ] Auto-sync mode refinements
- [ ] Conflict detection/resolution UI
- [ ] Webhooks
- [ ] API for integrations

---

## Open Questions

1. **Key expiry**: 1 year default? Never expire unless revoked?
2. **Free tier limits**: Max projects? Max events? Max collaborators?
3. **Snapshot frequency**: On-demand only? Periodic background job?
4. **Soft delete retention**: 30 days? 90 days? Configurable?
5. **Undo across sync**: Should `td undo` emit an event, or only work locally?
6. **Device auth verification UI**: Hosted web page? Or third-party OAuth provider?
