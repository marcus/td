# td-sync: Specification

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
| Sync server | Go | HTTP API for auth and sync |
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
    client_id TEXT NOT NULL,          -- unique per device/session
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
    client_id TEXT NOT NULL,          -- originating client
    client_action_id INTEGER NOT NULL,-- action_log.id from client
    action_type TEXT NOT NULL,        -- create, update, delete, etc.
    entity_type TEXT NOT NULL,        -- issue, log, handoff, etc.
    entity_id TEXT NOT NULL,
    payload JSON NOT NULL,            -- {previous_data, new_data}
    client_timestamp DATETIME NOT NULL,
    server_timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(project_id, client_id, client_action_id)
);

CREATE INDEX idx_events_project_id ON events(project_id, id);
CREATE INDEX idx_events_project_entity ON events(project_id, entity_type, entity_id);
```

### State Cache (per project)

Located at: `/data/projects/{project_id}/state.db`

Schema: **Identical to td client schema** (issues, logs, handoffs, etc.)

Purpose: Materialized view for read-only web access and fast client bootstrap.

---

## API

Base URL: `https://api.td.dev/v1` (hosted) or `https://your-server/v1` (self-hosted)

### Authentication

All endpoints except `/auth/register` and `/auth/login` require:
```
Authorization: Bearer td_live_xxxxxxxx
```

Key format: `td_live_` + 32 random alphanumeric characters

### Endpoints

#### Auth

```
POST /auth/register
  Request:  { email: string }
  Response: { user_id: string, message: "Check email for verification" }
  
POST /auth/verify
  Request:  { token: string }  # from email link
  Response: { api_key: string, expires_at: datetime }

POST /auth/login
  Request:  { email: string }
  Response: { message: "Check email for login link" }
  
POST /auth/token
  # Exchange email token for API key
  Request:  { token: string }
  Response: { api_key: string, expires_at: datetime }

POST /auth/keys
  # Create additional API key
  Request:  { name?: string, scopes?: string[], expires_in?: duration }
  Response: { api_key: string, key_id: string, expires_at: datetime }

DELETE /auth/keys/:key_id
  Response: { deleted: true }

GET /auth/me
  Response: { user_id, email, created_at }
```

#### Projects

```
POST /projects
  Request:  { name: string, description?: string }
  Response: { project: Project }

GET /projects
  Response: { projects: Project[] }

GET /projects/:id
  Response: { project: Project, membership: Membership }

PATCH /projects/:id
  Request:  { name?: string, description?: string }
  Response: { project: Project }

DELETE /projects/:id
  # Soft delete; owner only
  Response: { deleted: true }
```

#### Memberships

```
POST /projects/:id/members
  # Invite user; owner only
  Request:  { email: string, role: "writer" | "reader" }
  Response: { membership: Membership, invited: boolean }

GET /projects/:id/members
  Response: { members: Membership[] }

PATCH /projects/:id/members/:user_id
  # Change role; owner only
  Request:  { role: "writer" | "reader" }
  Response: { membership: Membership }

DELETE /projects/:id/members/:user_id
  # Remove member; owner only, cannot remove self
  Response: { removed: true }
```

#### Sync

```
POST /projects/:id/sync/push
  # Upload local events
  Request: {
    client_id: string,
    events: [{
      client_action_id: integer,
      action_type: string,
      entity_type: string,
      entity_id: string,
      payload: object,
      client_timestamp: datetime
    }]
  }
  Response: {
    accepted: integer[],      # client_action_ids successfully stored
    rejected: [{              # duplicates or errors
      client_action_id: integer,
      reason: string
    }],
    server_event_id: integer  # highest event id after insert
  }

GET /projects/:id/sync/pull
  # Download events since cursor
  Query: since=integer        # last known event id (0 for full sync)
         limit=integer        # max events (default 1000, max 10000)
         exclude_client=string # omit own events (optional)
  Response: {
    events: Event[],
    last_event_id: integer,
    has_more: boolean
  }

GET /projects/:id/sync/snapshot
  # Download full state.db for fast bootstrap
  Response: SQLite database file (application/x-sqlite3)
  Headers:
    X-Snapshot-Event-Id: integer  # event id at snapshot time

GET /projects/:id/sync/status
  Response: {
    event_count: integer,
    last_event_at: datetime,
    snapshot_available: boolean,
    snapshot_event_id: integer
  }
```

#### Read-Only Views (Future)

```
GET /projects/:id/view/issues
  Query: status, type, priority, limit, offset
  Response: { issues: Issue[], total: integer }

GET /projects/:id/view/issues/:issue_id
  Response: { issue: Issue, logs: Log[], handoffs: Handoff[] }

GET /projects/:id/view/boards
  Response: { boards: Board[] }
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

## Sync Protocol

### Client State

New table in td's `.todos/db.sqlite`:

```sql
CREATE TABLE sync_state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Keys:
--   client_id: unique identifier for this client (generated on first sync)
--   project_id: linked remote project id (null if not linked)
--   last_event_id: last synced server event id
--   last_sync_at: timestamp of last successful sync
```

New column in `action_log`:

```sql
ALTER TABLE action_log ADD COLUMN synced_at DATETIME;
```

### Sync Flow

```
1. PUSH local changes
   ├── Query: SELECT * FROM action_log WHERE synced_at IS NULL ORDER BY id
   ├── POST /projects/:id/sync/push
   └── UPDATE action_log SET synced_at = now() WHERE id IN (accepted)

2. PULL remote changes
   ├── GET /projects/:id/sync/pull?since={last_event_id}&exclude_client={client_id}
   ├── For each event:
   │   └── Apply to local database (see Event Application)
   └── UPDATE sync_state SET value = {new_last_event_id} WHERE key = 'last_event_id'
```

### Event Application

Events map to local database operations:

| action_type | Operation |
|-------------|-----------|
| `create` | INSERT using `new_data` |
| `update` | UPDATE using `new_data` |
| `delete` | DELETE by entity_id |
| `soft_delete` | UPDATE SET deleted_at = timestamp |

Entity types match td tables: `issue`, `log`, `handoff`, `comment`, `board`, `work_session`, etc.

### Conflict Resolution

**Strategy: Last-write-wins by server arrival order**

Events are applied in `server_event_id` order. If two clients edit the same entity:

```
Client A (offline): Edit issue title → "Fix bug"
Client B (online):  Edit issue title → "Fix the bug"

Server receives B first → event_id 100
Server receives A second → event_id 101

All clients apply in order:
  100: title = "Fix the bug"
  101: title = "Fix bug"

Final state: title = "Fix bug" (A's edit wins because it arrived later)
```

For most task management operations, this is acceptable. Future enhancement: detect conflicts via `previous_data` comparison and surface to user.

### Initial Sync (New Client)

Option A: **Event replay** (simple, works for small projects)
```
GET /projects/:id/sync/pull?since=0
Apply all events to empty local database
```

Option B: **Snapshot bootstrap** (fast, for larger projects)
```
GET /projects/:id/sync/snapshot
  → Save as local .todos/db.sqlite
GET /projects/:id/sync/pull?since={X-Snapshot-Event-Id}
  → Apply incremental events
```

Threshold: Use snapshot if event_count > 1000.

---

## Client Changes

### New Commands

```bash
# Authentication
td auth login [--server URL]     # Interactive login flow
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
    "auto_sync": false
  }
}
```

Environment variables (override config file):

```bash
TD_SYNC_URL="https://api.td.dev"    # Server URL
TD_SYNC_KEY="td_live_xxxxxxxx"      # API key
TD_SYNC_ENABLED="true"              # Enable/disable sync
TD_SYNC_AUTO="false"                # Auto-sync on every command (future)
```

### Auth Storage

File: `~/.config/td/auth.json`

```json
{
  "api_key": "td_live_xxxxxxxx",
  "user_id": "01HXK...",
  "email": "user@example.com",
  "server_url": "https://api.td.dev",
  "expires_at": "2025-12-31T23:59:59Z"
}
```

Permissions: `0600` (user read/write only)

---

## Server Implementation

### Project Structure

```
td-server/
├── cmd/
│   └── td-server/
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
    
    // Email (for auth)
    SMTPHost      string        `env:"SMTP_HOST"`
    SMTPPort      int           `env:"SMTP_PORT" default:"587"`
    SMTPUser      string        `env:"SMTP_USER"`
    SMTPPass      string        `env:"SMTP_PASS"`
    SMTPFrom      string        `env:"SMTP_FROM"`
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
            log.Warn("state cache apply failed", "event", event.ID, "err", err)
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

## Self-Hosting

### Docker Compose

```yaml
version: "3.8"

services:
  td-server:
    image: ghcr.io/marcus/td-server:latest
    environment:
      - AUTH_SECRET=${AUTH_SECRET}
      - SMTP_HOST=${SMTP_HOST}
      - SMTP_USER=${SMTP_USER}
      - SMTP_PASS=${SMTP_PASS}
      - SMTP_FROM=${SMTP_FROM}
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
git clone https://github.com/marcus/td-server
cd td-server

# Configure
cp .env.example .env
# Edit .env: set AUTH_SECRET, SMTP credentials

# Run
docker-compose up -d

# In any td project
td auth login --server https://your-server:8080
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

---

## Roadmap

### Phase 1: Core Sync
- [ ] Server: auth, projects, memberships, events
- [ ] Client: auth commands, sync command
- [ ] Self-host: Docker image, docs

### Phase 2: Polish
- [ ] Snapshot bootstrap
- [ ] `td doctor` diagnostics
- [ ] Rate limiting
- [ ] Email verification

### Phase 3: Collaboration
- [ ] Invite flow (email)
- [ ] Role management

### Phase 4: Read-Only Web
- [ ] Public project pages
- [ ] Private view (auth required)
- [ ] Embed widget

### Phase 5: Future
- [ ] Auto-sync mode
- [ ] Conflict detection/UI
- [ ] Webhooks
- [ ] API for integrations

---

## Open Questions

1. **Email provider**: Resend? Postmark? Self-hosted SMTP?

2. **Key expiry**: 1 year default? Never expire unless revoked?

3. **Free tier limits**: Max projects? Max events? Max collaborators?

4. **Snapshot frequency**: On-demand only? Periodic background job?

5. **Soft delete retention**: 30 days? 90 days? Configurable?

6. **Undo across sync**: Should `td undo` emit an event, or only work locally?
