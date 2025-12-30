# Plan: Multi-User td Database with Turso/libSQL

## Executive Summary

This plan details how to migrate td's SQLite database to Turso (managed) or self-hosted libSQL for multi-user collaboration. **Recommended approach**: Use embedded replicas with a central libSQL server for offline-first, low-latency reads with synced writes.

## Decisions Made

- **Deployment**: Support both Turso Cloud and self-hosted libSQL
- **CGO**: Acceptable (enables embedded replicas)
- **Conflict resolution**: Optimistic locking (reject if `updated_at` changed)

---

## Current Architecture

**Database**: SQLite via `modernc.org/sqlite` driver

- Location: `.todos/issues.db` (project-local)
- WAL mode enabled for concurrent reads
- File-based locking for write serialization across processes
- 12 tables, schema version 5

**Key Files**:

- `internal/db/db.go` - Connection management, CRUD operations
- `internal/db/schema.go` - Schema definition, migrations
- `internal/db/lock*.go` - OS-level file locking (Unix/Windows)

**Current Limitations**:

- Single-machine only (file-based)
- No cross-machine sync
- No multi-user collaboration

---

## Option 1: Turso Cloud (Recommended for Simplicity)

### How It Works

- Turso hosts your primary database in the cloud
- Clients use embedded replicas for local reads (instant, offline-capable)
- Writes go to primary, sync back to replicas

### Go SDK

```go
// Replace modernc.org/sqlite with:
go get github.com/tursodatabase/go-libsql

// Connection with embedded replica
connector, err := libsql.NewEmbeddedReplicaConnector(
    localDBPath,
    tursoURL,
    libsql.WithAuthToken(token),
    libsql.WithSyncInterval(time.Minute),
)
db := sql.OpenDB(connector)
```

### Pricing

| Tier      | Cost      | Storage | Rows Read/mo | Rows Written/mo |
| --------- | --------- | ------- | ------------ | --------------- |
| Free      | $0        | 5GB     | 500M         | 10M             |
| Developer | $4.99/mo  | 9GB     | 2.5B         | 25M             |
| Scaler    | $24.92/mo | 24GB    | 100B         | 100M            |

### Pros

- Zero infrastructure management
- Automatic backups, point-in-time restore
- Global edge distribution
- Built-in sync with embedded replicas

### Cons

- Vendor dependency
- Data leaves your infrastructure
- Ongoing costs (though free tier is generous)
- Requires internet for writes

---

## Option 2: Self-Hosted libSQL Server (Recommended for Control)

### Architecture

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   User A     │     │   User B     │     │   User C     │
│ (embedded    │     │ (embedded    │     │ (embedded    │
│  replica)    │     │  replica)    │     │  replica)    │
└──────┬───────┘     └──────┬───────┘     └──────┬───────┘
       │                    │                    │
       └────────────────────┼────────────────────┘
                            │
                     ┌──────▼───────┐
                     │  libSQL      │
                     │  Server      │
                     │  (sqld)      │
                     └──────────────┘
```

### Docker Deployment

```bash
# Primary server
docker run -d --name td-libsql \
  -p 8080:8080 \
  -p 5001:5001 \
  -v td-data:/var/lib/sqld \
  -e SQLD_NODE=primary \
  -e SQLD_HTTP_AUTH="basic:admin:password" \
  ghcr.io/tursodatabase/libsql-server:latest
```

### Docker Compose

```yaml
services:
  td-libsql:
    image: ghcr.io/tursodatabase/libsql-server:latest
    ports:
      - "8080:8080" # HTTP API
      - "5001:5001" # gRPC (for replicas)
    volumes:
      - td-data:/var/lib/sqld
    environment:
      - SQLD_NODE=primary
      - SQLD_HTTP_AUTH=basic:admin:password
    restart: unless-stopped

volumes:
  td-data:
```

### Pros

- Full data control
- No ongoing cloud costs
- Deploy anywhere (VPS, NAS, Tailscale, etc.)
- Same embedded replica benefits

### Cons

- You manage infrastructure
- Manual backup/restore
- No edge distribution (unless you set up replicas)

---

## Implementation Plan

### Phase 1: Configuration Layer

Add database mode configuration to td:

```go
// internal/config/config.go
type DatabaseConfig struct {
    Mode      string // "local", "turso", "libsql"
    URL       string // Remote URL for turso/libsql
    AuthToken string // Authentication token
    SyncInterval time.Duration // How often to sync (default: 1m)
    LocalPath string // Path for embedded replica
}
```

**Files to modify**:

- Create `internal/config/database.go`
- Modify `internal/db/db.go` - Add connection mode switching

### Phase 2: Driver Abstraction

Create interface for database operations:

```go
// internal/db/driver.go
type Driver interface {
    Open(cfg DatabaseConfig) (*sql.DB, error)
    Sync() error  // For embedded replicas
    Close() error
}

type LocalDriver struct{}   // Current SQLite
type LibSQLDriver struct{}  // Turso/libSQL with embedded replica
```

**Files to create**:

- `internal/db/driver.go` - Interface definition
- `internal/db/driver_local.go` - Current SQLite logic
- `internal/db/driver_libsql.go` - libSQL/Turso connector

### Phase 3: Locking Strategy Changes

Current file-based locking won't work with remote databases. Options:

1. **Trust the server** (recommended): libSQL server handles concurrency
2. **Advisory locking via table**: Create `td_locks` table for coordination
3. **Optimistic concurrency**: Add version columns, retry on conflict

Recommendation: Trust server for most operations, add `updated_at` checks for conflict detection.

**Files to modify**:

- `internal/db/lock.go` - Skip file locking when in remote mode
- `internal/db/db.go` - Add conflict detection on updates

### Phase 4: Sync Management

Add explicit sync controls for embedded replicas:

```go
// internal/db/sync.go
func (db *DB) Sync() error {
    if db.driver.CanSync() {
        return db.driver.Sync()
    }
    return nil
}

// Add to commands that need fresh data
func (db *DB) SyncAndQuery(...) {
    db.Sync()
    // then query
}
```

### Phase 5: CLI Integration

Add configuration commands:

```bash
# Initialize with remote database
td init --mode=libsql --url=http://localhost:8080 --token=xxx

# Or configure existing
td config set database.mode libsql
td config set database.url http://localhost:8080
td config set database.token xxx

# Manual sync
td sync

# Check connection status
td status --db
```

**Files to create/modify**:

- `cmd/config.go` - Add database config subcommands
- `cmd/sync.go` - Manual sync command
- `cmd/init.go` - Add remote mode flags

### Phase 6: Migration Path

For existing users:

```bash
# Export current database
td export --format=sql > backup.sql

# Initialize remote
td init --mode=libsql --url=... --migrate-from=.todos/issues.db
```

---

## Critical Considerations

### 1. Session ID Collisions

Currently, session IDs are generated per-machine. With multiple users:

- Session IDs may collide
- Need to include user/machine identifier

**Solution**: Extend session ID format to include machine fingerprint:

```
ses_XXXXXX -> ses_<machine>_XXXXXX
```

### 2. Conflict Resolution (Optimistic Locking)

When two users modify the same issue simultaneously, use optimistic locking:

```go
// internal/db/db.go - UpdateIssue modification
func (db *DB) UpdateIssue(issue *models.Issue, expectedUpdatedAt time.Time) error {
    return db.withWriteLock(func() error {
        // Check if issue was modified since we read it
        var currentUpdatedAt time.Time
        err := db.conn.QueryRow(
            "SELECT updated_at FROM issues WHERE id = ?", issue.ID,
        ).Scan(&currentUpdatedAt)

        if !currentUpdatedAt.Equal(expectedUpdatedAt) {
            return ErrConflict // New error type
        }

        issue.UpdatedAt = time.Now()
        // ... rest of update
    })
}
```

**Error handling**: When conflict detected, return clear error with current state so user can retry.

### 3. Offline Behavior

Embedded replicas allow reads offline, but writes require connection.

**Options**:

1. Queue writes locally, sync when online (complex)
2. Fail writes when offline (simpler, explicit)

**Recommendation**: Start with option 2, add queuing later if needed.

### 4. Authentication

- Turso uses JWT tokens (managed)
- Self-hosted sqld supports basic auth or JWT

**Recommendation**: Use environment variables for tokens:

```
TD_DATABASE_URL=libsql://...
TD_DATABASE_TOKEN=xxx
```

### 5. Schema Migrations

Current migration system works with SQLite. With remote:

- Migrations must be coordinated
- One user runs migration, others just update schema version

**Solution**: Add migration lock table, first-to-connect runs migrations.

---

## Caveats

### Technical Caveats

1. **go-libsql requires CGO**: The full-featured driver needs C bindings. Use `libsql-client-go` for CGO-free (but no embedded replicas)
2. **Embedded replicas need filesystem**: Won't work in serverless/containers without persistent storage
3. **Sync is not instant**: There's latency between write and replica update
4. **No concurrent writes**: libSQL doesn't support true concurrent writers yet (coming soon per docs)

### Operational Caveats

1. **Backup responsibility**: Self-hosted = you handle backups
2. **Network dependency**: Writes require network (except in pure local mode)
3. **Auth token management**: Tokens need secure storage and rotation
4. **Version compatibility**: td version must match schema version across all users

### Migration Caveats

1. **Breaking change**: Users must re-initialize or migrate existing databases
2. **Session history**: Session IDs from before migration may not map cleanly
3. **Git integration**: `.todos/` is typically gitignored; remote removes this concern but loses git-bisect correlation

---

## Recommended Implementation Order

Since we're supporting both Turso Cloud and self-hosted libSQL:

### Phase 1: Foundation (Week 1)

1. Add `go-libsql` dependency to `go.mod`
2. Create `internal/config/database.go` with config struct
3. Create `internal/db/driver.go` interface
4. Create `internal/db/driver_local.go` (wrap current SQLite logic)

### Phase 2: libSQL Driver (Week 2)

1. Create `internal/db/driver_libsql.go` with embedded replica support
2. Modify `internal/db/db.go` to use driver abstraction
3. Add sync functionality
4. Update locking to skip file locks in remote mode

### Phase 3: Conflict Handling (Week 3)

1. Add `ErrConflict` error type
2. Modify `UpdateIssue` with optimistic locking
3. Update all write operations to handle conflicts
4. Update CLI commands to show conflict errors clearly

### Phase 4: CLI & Config (Week 4)

1. Add `td init --mode=libsql|turso --url=...` flags
2. Add `td config database.mode/url/token` commands
3. Add `td sync` command for manual sync
4. Add `td status --db` for connection status

### Phase 5: Session & Migration (Week 5)

1. Update session ID format for multi-machine
2. Add database migration command
3. Add export/import for data portability
4. Documentation

### Default Behavior

- New installs: Default to local SQLite (backward compatible)
- Existing users: No change until they opt-in
- Remote mode: Opt-in via `td init --mode=libsql` or `td config`

---

## Files to Modify/Create

| File                           | Action | Description                   |
| ------------------------------ | ------ | ----------------------------- |
| `internal/config/database.go`  | Create | Database configuration struct |
| `internal/db/driver.go`        | Create | Driver interface              |
| `internal/db/driver_local.go`  | Create | SQLite driver                 |
| `internal/db/driver_libsql.go` | Create | libSQL driver                 |
| `internal/db/db.go`            | Modify | Use driver abstraction        |
| `internal/db/lock.go`          | Modify | Skip locking for remote       |
| `internal/db/sync.go`          | Create | Sync management               |
| `internal/session/session.go`  | Modify | Machine-aware session IDs     |
| `cmd/init.go`                  | Modify | Add --mode flag               |
| `cmd/config.go`                | Create | Database config commands      |
| `cmd/sync.go`                  | Create | Manual sync command           |
| `go.mod`                       | Modify | Add go-libsql dependency      |

---

## Next Steps

1. Decide: Turso Cloud vs Self-hosted vs Both supported?
2. Decide: CGO dependency acceptable? (affects embedded replicas)
3. Decide: Conflict resolution strategy
4. Implement Phase 1-2 as proof of concept
5. Test with real multi-user scenario
