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
│ sync lib    │     │ sync lib    │     │ sync lib    │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           │ HTTPS
                           ▼
                 ┌───────────────────┐
                 │   td-sync server  │
                 │   (Go binary)     │
                 │   + sync lib      │
                 └─────────┬─────────┘
                           │
              ┌────────────┴────────────┐
              ▼                         ▼
       ┌─────────────┐     ┌──────────────────────┐
       │  server.db  │     │  Per-Project DBs      │
       │  (SQLite)   │     │  /data/projects/      │
       │  users,     │     │    {id}/events.db     │
       │  api_keys,  │     │    {id}/events.db     │
       │  projects,  │     │    ...                │
       │  members    │     └──────────────────────┘
       └─────────────┘                │
              │                       │
              └───────────┬───────────┘
                          ▼
                   ┌─────────────┐
                   │ Litestream  │
                   │ (sidecar)   │
                   │ → S3/R2/B2  │
                   └─────────────┘
```

### Components

| Component | Technology | Purpose |
|-----------|------------|---------|
| Sync server | Go (single binary) | HTTP API for auth and sync |
| Sync library | Go (`internal/sync`) | Shared event application logic, used by client and server |
| Server database | SQLite (`server.db`) | Users, API keys, projects, memberships |
| Project databases | SQLite (one per project) | Append-only event log |
| Backup | Litestream (sidecar) | Continuous WAL replication to object storage |

---

## Database Schemas

### server.db

Single database for the entire server. Contains auth, project registry, and membership data. All authorization checks resolve here before touching any project database.

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

### Per-Project events.db

Located at: `/data/projects/{project_id}/events.db`

One database per project. `project_id` is implicit from the file path and not stored in the events table.

```sql
-- Events (append-only log of all changes)
CREATE TABLE events (
    server_seq INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL,          -- originating device
    session_id TEXT NOT NULL,         -- originating session (for idempotency)
    client_action_id INTEGER NOT NULL,-- action_log.id from client
    action_type TEXT NOT NULL,        -- create, update, delete, etc.
    entity_type TEXT NOT NULL,        -- issue, log, handoff, etc.
    entity_id TEXT NOT NULL,
    payload JSON NOT NULL,            -- {schema_version, previous_data, new_data}
    client_timestamp DATETIME NOT NULL,
    server_timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(device_id, session_id, client_action_id)
);

CREATE INDEX idx_events_entity ON events(entity_type, entity_id);
```

### State Cache (per project) — Deferred

~~Located at: `/data/projects/{project_id}/state.db`~~

**Deferred to Phase 4 (Read-Only Web).** State cache adds a second source of truth with rebuild/race-condition complexity. Not needed until server-side reads are required. In Phase 1, the event log is the only server-side store per project.

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
ALTER TABLE action_log ADD COLUMN server_seq INTEGER;  -- populated from push ack
```

**Prerequisite:** td's existing `action_log` must include `action_type`, `entity_type`, `entity_id`, and a `payload` column containing JSON with `schema_version`, `new_data`, and `previous_data`. If any of these are missing, a td migration is required before Phase 0. The test harness imports td's `internal/db.InitSchema()` to create real td databases — the code is the source of truth for the schema, not this spec.

### Event generation

- Every local mutation MUST produce exactly one event in `action_log`.
- Events MUST be appended within the same transaction as the mutation.
- Events MUST include enough data to apply the change on another replica.
- Event payloads MUST include a `schema_version` field.
- **`new_data` MUST contain the full entity state**, not a partial diff. `INSERT OR REPLACE` deletes the existing row and re-inserts, so any columns omitted from `new_data` are reset to defaults. Partial payloads silently drop data.

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

The server treats payloads as opaque for sync purposes.

### Schema Compatibility Rules

- **Ignore unknown fields.** If a payload contains fields the client doesn't recognize, skip them during apply.
- **Use zero-values for missing fields.** If the client expects a field not in the payload, use the column default (NULL, empty string, 0).
- **Never remove fields from payloads.** Fields can be added across schema versions but never removed. A `schema_version: 3` payload is a superset of `schema_version: 2`.
- **The server never interprets payloads.** It stores and forwards them. Compatibility is purely a client concern.

Clients don't need to be on the same version to sync. An older client won't see new fields; a newer client fills defaults for fields an older client didn't send.

### Sync triggers

Auto-sync is configured via `sync.auto` in `~/.config/td/config.json`:

| Trigger | Config field | Default | Behavior |
|---------|-------------|---------|----------|
| Startup | `auto.on_start` | `true` | Push+pull on command start (skipped for sync/auth/login/version/help) |
| Post-mutation | `auto.debounce` | `"3s"` | Push+pull after mutating commands, rate-limited |
| Periodic | `auto.interval` | `"5m"` | Push+pull at interval (TUI monitor) |
| Manual | n/a | n/a | `td sync` command |

Auto-sync requires: `auto.enabled=true` + authenticated + project linked. All auto-sync operations are silent (`slog.Debug` only) with 5s HTTP timeout. Set `auto.pull=false` for push-only.

Sync is opt-out per project (`sync_disabled`) and globally (`auto.enabled`).

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
| `create` | INSERT OR REPLACE using `new_data` |
| `update` | INSERT OR REPLACE using `new_data` |
| `delete` | DELETE by entity_id (no-op if missing) |
| `soft_delete` | UPDATE SET deleted_at = timestamp (no-op if missing) |

Entity types match td tables: `issue`, `log`, `handoff`, `comment`, `board`, `work_session`, etc.

### Event-to-SQL Mapping

The sync library maps JSON payloads to SQL dynamically — no hardcoded schema knowledge. JSON keys in `new_data` map 1:1 to column names. `INSERT OR REPLACE` handles both creates and upserts.

```go
func insertEntity(tx *sql.Tx, entityType, entityID string, data json.RawMessage) error {
    var fields map[string]any
    json.Unmarshal(data, &fields)
    fields["id"] = entityID
    cols, vals, placeholders := buildInsert(fields)
    _, err := tx.Exec(
        fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)",
            entityType, cols, placeholders),
        vals...,
    )
    return err
}
```

Table names are validated against a caller-provided allowlist to prevent SQL injection:

```go
var validEntityTypes = map[string]bool{
    "issues": true, "logs": true, "handoffs": true,
    "comments": true, "boards": true, "work_sessions": true,
}
```

The allowlist is owned by the caller (td or the test harness), not the sync library. The library accepts a validation function.

### Idempotency Edge Cases

The event stream is authoritative. The sync library always converges toward the server's state:

| Scenario | Behavior | Rationale |
|----------|----------|-----------|
| `create` for existing entity | Upsert (INSERT OR REPLACE) | May exist from partial sync or create-create race. |
| `update` for missing entity | Insert using `new_data` | Entity created in an event we missed. Payload has full data. |
| `delete` for missing entity | No-op | Already gone or never existed. Desired state is "not present." |
| `update` for deleted entity | Re-insert using `new_data` | Server says entity should exist. Server wins. |

### Partial Batch Failure

When applying pulled events, if an individual event fails:

- Log the error at WARN level with full context (event type, entity ID, error).
- **Skip it and continue** applying remaining events.
- Return the highest successfully applied `server_seq` plus a list of failed events.
- The caller commits the transaction, advancing the cursor past failures.

Failed events are preserved in the server's event log for debugging. Rolling back the entire batch would block the client permanently on a single bad event.

```go
type ApplyResult struct {
    LastAppliedSeq int64
    Applied        int
    Failed         []FailedEvent
}

type FailedEvent struct {
    ServerSeq int64
    Error     error
}
```

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

**`previous_data` accuracy caveat:** `previous_data` is captured at mutation time on the originating client. If that client had stale local state (e.g., hadn't pulled recently), its `previous_data` reflects what *it* saw, not necessarily the true prior server state. This is acceptable for Phase 1 observability and debugging, but `previous_data` should not be treated as a reliable conflict history for undo or merge operations.

**UX note:** Silent last-write-wins is fine for solo users. For multi-user, users must know their local edits can be lost; document clearly and consider console warning on next sync.

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

### First User Bootstrap

When `AllowSignup` is `true` (default for new installations):

1. Client calls `POST /v1/auth/login/start` with `{ email: "user@example.com" }`.
2. Server creates a pending auth request. If no user exists with that email, one is created on successful verification.
3. Server returns `device_code`, `user_code`, `verification_uri`.
4. User visits the verification page (served by the sync server — a simple HTML form, no JS framework).
5. Server verifies the code, creates the user if new, generates an API key.
6. Client polls and receives the API key.

The device code flow does not verify email ownership in Phase 1 — the user just types their email and a code. True email verification is Phase 2 (collaboration invites).

When `AllowSignup` is `false`:

- `POST /v1/auth/login/start` returns an error if the email isn't in `server.db`.
- An admin adds users via CLI: `td-sync admin add-user --email user@example.com`

---

## Sync Flow

```
1. PUSH local changes
   ├── Query: SELECT * FROM action_log WHERE synced_at IS NULL ORDER BY id
   ├── POST /projects/:id/sync/push
   └── For each ack: UPDATE action_log SET synced_at = now(), server_seq = {ack.server_seq} WHERE id = {ack.client_action_id}

2. PULL remote changes
   ├── GET /projects/:id/sync/pull?after_server_seq={n}  (exclude_client is non-MVP)
   ├── For each event:
   │   └── Apply to local database (see Event Application)
   └── UPDATE sync_state SET last_pulled_server_seq = {new_seq}
```

### Initial Sync (New Client)

For small projects (below the snapshot threshold), the client replays all events:
```
GET /projects/:id/sync/pull?after_server_seq=0
Apply all events to empty local database
```

For larger projects, the client auto-bootstraps from a snapshot. See the snapshot endpoint below and the [Sync Client Guide](sync-client-guide.md#snapshot-bootstrap) for details.

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
         exclude_client=string     # omit own events (optional, non-MVP optimization — clients re-apply own events idempotently without it)
  Response: {
    events: Event[],
    last_server_seq: integer,
    has_more: boolean
  }

GET /v1/projects/:id/sync/snapshot
  # Download a pre-built SQLite database for fast bootstrap
  # The snapshot contains the full td schema with all migrations applied,
  # plus all entity data up to the snapshot sequence number.
  # Server caches snapshots at {dataDir}/snapshots/{projectID}/{seq}.db
  Response: SQLite database file (application/x-sqlite3)
  Headers:
    X-Snapshot-Seq: integer  # server_seq at snapshot time

GET /v1/projects/:id/sync/status
  Response: {
    event_count: integer,
    last_event_at: datetime
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
    "auto": {
      "enabled": true,
      "on_start": true,
      "debounce": "3s",
      "interval": "5m",
      "pull": true
    }
  }
}
```

| Field | Default | Description |
|---|---|---|
| `auto.enabled` | `true` | Master switch for all auto-sync |
| `auto.on_start` | `true` | Push+pull on command startup |
| `auto.debounce` | `"3s"` | Minimum interval between post-mutation syncs |
| `auto.interval` | `"5m"` | Periodic push+pull interval (TUI monitor) |
| `auto.pull` | `true` | Include pull in auto-sync; `false` for push-only |

Environment variables (override config file):

```bash
TD_SYNC_URL="https://api.td.dev"        # Server URL
TD_AUTH_KEY="td_live_xxxxxxxx"           # API key (overrides auth.json)
TD_SYNC_ENABLED="true"                  # Enable/disable sync
TD_SYNC_AUTO="true"                     # Enable/disable auto-sync
TD_SYNC_AUTO_START="true"               # Enable/disable startup sync
TD_SYNC_AUTO_DEBOUNCE="3s"              # Debounce duration
TD_SYNC_AUTO_INTERVAL="5m"              # Periodic sync interval
TD_SYNC_AUTO_PULL="true"                # Include pull in auto-sync
```

---

## Server Implementation

### Project Structure

Server binary and sync library live in the td repo. Single module, two binaries.

```
td/
├── cmd/
│   ├── td/                        # existing CLI binary
│   └── td-sync/                   # server binary
│       └── main.go
├── internal/
│   ├── db/                        # existing td database layer
│   ├── sync/                      # shared sync library
│   │   ├── engine.go              # core push/pull logic
│   │   ├── events.go              # event application (create/update/delete → SQL)
│   │   ├── events_test.go
│   │   ├── engine_test.go
│   │   └── testutil.go
│   ├── api/                       # HTTP layer (server only)
│   │   ├── auth.go
│   │   ├── projects.go
│   │   ├── sync.go
│   │   └── middleware.go
│   └── models/
│       └── models.go
├── test/
│   └── syncharness/               # integration test harness
│       ├── harness.go
│       ├── harness_test.go
│       └── fixtures/
├── migrations/
│   ├── global/
│   └── project/
├── Dockerfile
├── docker-compose.yml
├── .env.example
└── docs/
    └── sync-mvp-testing-spec.md   # detailed MVP testing spec
```

**Key constraint:** `internal/sync` never imports from `internal/db`, `cmd/`, or any td-specific package. It receives `*sql.Tx` from callers — it does not open or manage database connections. This keeps the sync library testable in isolation and prevents it from competing with td's single-writer connection management.

### Configuration

```go
type Config struct {
    // Server
    Host          string        `env:"HOST" default:"0.0.0.0"`
    Port          int           `env:"PORT" default:"8080"`

    // Database
    ServerDBPath   string       `env:"SERVER_DB_PATH" default:"/data/server.db"`
    ProjectDataDir string       `env:"PROJECT_DATA_DIR" default:"/data/projects"`

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

### Push Handler

Server-side push uses the shared sync library. The server opens the project's events.db, starts a transaction, and passes it to `sync.InsertServerEvents`:

```go
func (s *Server) handlePush(projectID string, events []sync.Event) (sync.PushResult, error) {
    db := s.getProjectDB(projectID)
    tx, _ := db.Begin()
    defer tx.Rollback()

    result, err := sync.InsertServerEvents(tx, projectID, events)
    if err != nil {
        return result, err
    }

    tx.Commit()
    return result, nil
}
```

No state cache updates in Phase 1. The event log is the only write target.

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

Litestream runs as a sidecar container, replicating `server.db` and all per-project `events.db` files to object storage. When new projects are created, regenerate the Litestream config and send SIGHUP to reload.

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

Static config for `server.db`. Per-project databases require config regeneration + SIGHUP when projects are created/deleted.

```yaml
dbs:
  - path: /data/server.db
    replicas:
      - url: s3://your-bucket/td-backup/server.db

  # Per-project entries added dynamically:
  # - path: /data/projects/{project_id}/events.db
  #   replicas:
  #     - url: s3://your-bucket/td-backup/projects/{project_id}/events.db
```

### Quick Start (Self-Host)

```bash
# Clone
git clone https://github.com/marcus/td
cd td

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

### Phase 0: Test Harness & Sync Library
- [ ] Shared sync library (`internal/sync`) with event application logic
- [ ] Test harness with simulated multi-client scenarios (direct Go calls, no HTTP)
- [ ] Core test cases: create, update, delete, conflict, idempotency, large batch
- [ ] Convergence validation (all clients identical after sync)
- [ ] See [sync-mvp-testing-spec.md](sync-mvp-testing-spec.md) for detailed spec

### Phase 1: Core Sync
- [ ] Server binary (`cmd/td-sync`): HTTP layer on top of sync library
- [ ] Server database: device auth, projects, memberships
- [ ] Per-project databases: event storage
- [ ] Device auth with self-hosted verification page
- [ ] Litestream sidecar with dynamic config regeneration
- [ ] Client integration: `td auth login`, `td sync`, `td project link/create`
- [ ] Observability: structured logging, basic metrics

### Phase 2: Polish
- [x] Snapshot bootstrap (auto-triggers on first sync when event count >= threshold)
- [ ] `td doctor` diagnostics
- [ ] Rate limiting
- [ ] Email-based auth (magic links for collaboration invites)
- [x] Auto-sync triggers (startup, debounce, periodic)

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
3. **Soft delete retention**: 30 days? 90 days? Configurable?
4. **Undo across sync**: Should `td undo` emit an event, or only work locally?

## Resolved Decisions

1. **Database layout**: One server.db for auth/registry + one events.db per project. Memberships stay in server.db — auth resolves before any project db is opened.
2. **State cache**: Deferred to Phase 4. Event log is the only server-side store in Phase 1.
3. **Snapshot bootstrap**: Implemented. Client auto-bootstraps from server snapshot when `last_pulled_server_seq == 0` and server event count >= configurable threshold (default 100). Server builds snapshots with full schema + migrations and caches them at `{dataDir}/snapshots/{projectID}/{seq}.db`. Threshold configurable via `TD_SYNC_SNAPSHOT_THRESHOLD` env or `config.json` `sync.snapshot_threshold`.
4. **Repository structure**: Single repo (td). Server binary at `cmd/td-sync`, shared sync library at `internal/sync`.
5. **Sync library connection management**: Library receives `*sql.Tx` from callers, never opens databases. Prevents write contention with td's single-writer model.
6. **Litestream**: Sidecar container (not embedded). Config regenerated + SIGHUP on project create/delete.
7. **Device auth verification UI**: Self-hosted HTML page served by the sync server. No third-party dependency.
8. **Conflict preservation**: `previous_data` in event payload is the conflict record. No separate conflict table — event log is the history.
9. **Schema compatibility**: Ignore unknown fields, zero-value missing fields, never remove fields. Clients don't need matching versions.
10. **Event-to-SQL mapping**: Dynamic column mapping from JSON keys. No hardcoded schema in the sync library. Table names validated via caller-provided allowlist.
11. **Idempotency**: Event stream is authoritative. Upsert on create/update, no-op on delete-if-missing, re-insert on update-after-delete.
12. **Partial batch failure**: Skip failed events, log them, continue applying. Don't roll back the batch.
13. **First user bootstrap**: Auto-create users on successful device auth when `AllowSignup` is true. CLI admin command when false.
