# td-sync: MVP Testing Spec (Addendum to v3)

Spec for reaching minimum viable sync testing: a test harness that exercises the sync library via direct Go calls, no HTTP layer, no td integration yet.

---

## Goal

Two simulated clients can push and pull events through the sync engine, and both end up with identical database state. Conflicts are resolved last-write-wins with the losing version preserved in the event log's `previous_data` field.

---

## Repository Structure

Everything lives in the td repo. Two binaries, shared sync library.

```
td/
├── cmd/
│   ├── td/                    # existing CLI
│   └── td-sync/               # server binary (phase 1)
│       └── main.go
├── internal/
│   ├── db/                    # existing td database layer
│   ├── sync/                  # sync library (shared by client + server)
│   │   ├── engine.go          # core push/pull logic
│   │   ├── events.go          # event application (create/update/delete → SQL)
│   │   ├── events_test.go
│   │   ├── engine_test.go
│   │   └── testutil.go        # shared test helpers
│   └── ...
├── test/
│   └── syncharness/           # integration test harness
│       ├── harness.go         # orchestrates multi-client scenarios
│       ├── harness_test.go    # test cases
│       ├── fixtures/          # real action logs from td/sidecar (optional)
│       └── dashboard.go       # HTML output for test runs (phase 1b)
```

### Why this layout

- `internal/sync` is the shared library. td imports it for client-side sync. `cmd/td-sync` imports it for server-side event application. Neither can diverge.
- The sync library never imports anything from `internal/db` or `cmd/`. It receives `*sql.Tx` — it doesn't open databases.
- Test harness lives in `test/syncharness` so it can import `internal/sync` (same module) but stays out of the production code paths.

---

## Sync Library Interface

The sync library operates on transactions it's given. It does not manage connections.

```go
package sync

// Event represents a change record moving through the sync system.
type Event struct {
    ClientActionID int64
    DeviceID       string
    SessionID      string
    ActionType     string    // create, update, delete, soft_delete
    EntityType     string    // issue, log, handoff, etc.
    EntityID       string
    Payload        []byte    // JSON: {schema_version, new_data, previous_data}
    ClientTimestamp time.Time
    ServerSeq      int64     // assigned by server, 0 when unsent
}

type PushResult struct {
    Accepted int
    Acks     []Ack
    Rejected []Rejection
}

type Ack struct {
    ClientActionID int64
    ServerSeq      int64
}

type Rejection struct {
    ClientActionID int64
    Reason         string
}

type PullResult struct {
    Events        []Event
    LastServerSeq int64
    HasMore       bool
}

// GetPendingEvents reads unsynced events from the client's action_log.
// Caller provides a read transaction.
func GetPendingEvents(tx *sql.Tx, afterActionID int64) ([]Event, error)

// ApplyRemoteEvents applies pulled events to a local database.
// Caller provides a write transaction. Idempotent — skips already-applied events.
// Returns the highest server_seq applied.
func ApplyRemoteEvents(tx *sql.Tx, events []Event, myDeviceID string) (int64, error)

// InsertServerEvents inserts pushed events into the server's project event log.
// Assigns server_seq (autoincrement). Deduplicates by (device_id, session_id, client_action_id).
// Caller provides a write transaction on the project database.
func InsertServerEvents(tx *sql.Tx, projectID string, events []Event) (PushResult, error)

// GetEventsSince returns events after the given server_seq for a project.
// Caller provides a read transaction on the project database.
// excludeDevice is a non-MVP optimization — without it, clients re-apply own events idempotently.
func GetEventsSince(tx *sql.Tx, afterSeq int64, limit int, excludeDevice string) (PullResult, error)
```

### Event Application Logic

`ApplyRemoteEvents` maps action types to SQL operations using dynamic column mapping — JSON keys in `new_data` map 1:1 to column names. No hardcoded schema knowledge.

```go
func applyEvent(tx *sql.Tx, event Event) error {
    var payload struct {
        SchemaVersion int             `json:"schema_version"`
        NewData       json.RawMessage `json:"new_data"`
        PreviousData  json.RawMessage `json:"previous_data"`
    }
    json.Unmarshal(event.Payload, &payload)

    switch event.ActionType {
    case "create", "update":
        // INSERT OR REPLACE — handles creates, upserts, and update-for-missing-entity
        return upsertEntity(tx, event.EntityType, event.EntityID, payload.NewData)
    case "delete":
        // DELETE — no-op if entity doesn't exist
        return deleteEntity(tx, event.EntityType, event.EntityID)
    case "soft_delete":
        // UPDATE deleted_at — no-op if entity doesn't exist
        return softDeleteEntity(tx, event.EntityType, event.EntityID, event.ClientTimestamp)
    }
    return fmt.Errorf("unknown action_type: %s", event.ActionType)
}

func upsertEntity(tx *sql.Tx, entityType, entityID string, data json.RawMessage) error {
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

Table names validated against a caller-provided allowlist (not hardcoded in the sync library):

```go
var validEntityTypes = map[string]bool{
    "issues": true, "logs": true, "handoffs": true,
    "comments": true, "boards": true, "work_sessions": true,
}
```

### Idempotency Edge Cases

The event stream is authoritative. The sync library always converges toward server state:

| Scenario | Behavior |
|----------|----------|
| `create` for existing entity | Upsert (INSERT OR REPLACE) |
| `update` for missing entity | Insert using `new_data` |
| `delete` for missing entity | No-op |
| `update` for deleted entity | Re-insert using `new_data` |

### Partial Batch Failure

If an individual event fails during `ApplyRemoteEvents`:

- Log at WARN, skip it, continue applying remaining events.
- Return highest successfully applied `server_seq` plus a list of failures.
- Caller commits the transaction — cursor advances past failures.

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

### Conflict Detection

During `ApplyRemoteEvents`, if a remote update targets an entity whose local `updated_at` is newer than the last sync time, log the overwrite:

```go
slog.Warn("sync: overwrite", "entity", event.EntityID, "type", event.EntityType)
```

The `previous_data` in the event payload is the conflict record. No separate table needed — the full history is reconstructable from the event log.

**Accuracy caveat:** `previous_data` reflects what the originating client saw at mutation time. If that client had stale state (hadn't pulled recently), `previous_data` may not match the true prior server state. Acceptable for Phase 1 observability; do not rely on it for undo or merge operations.

---

## Server-Side Database Layout

### Global Database (`/data/server.db`)

One database for the entire server. Contains auth and project registry.

```sql
-- Users, api_keys, projects, memberships, sync_cursors
-- (same schema as auth.db in the v3 spec)
```

### Per-Project Database (`/data/projects/{project_id}/events.db`)

One database per project. Contains only the event log.

```sql
CREATE TABLE events (
    server_seq INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    client_action_id INTEGER NOT NULL,
    action_type TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    payload JSON NOT NULL,
    client_timestamp DATETIME NOT NULL,
    server_timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(device_id, session_id, client_action_id)
);

CREATE INDEX idx_events_entity ON events(entity_type, entity_id);
```

Note: `project_id` is not in the events table — it's implicit from the database file path. The v3 spec had `project_id` in a shared events.db; with per-project databases it's redundant.

### No State Cache in Phase 1

State cache (materialized view) is deferred. No server-side reads of project state needed yet. The event log is the only server-side data store per project.

### Litestream

Runs as a sidecar container (not embedded in the server binary). Replicates `server.db` + all per-project `events.db` files to object storage. Config regenerated and Litestream sent SIGHUP when projects are created/deleted. Not needed for MVP testing — deferred to Phase 1.

---

## Test Harness

### Architecture

The harness simulates N clients syncing through a single server-side event store. No HTTP, no auth — direct Go function calls to `internal/sync`.

```go
type Harness struct {
    // Server side
    ServerDB   *sql.DB                    // server.db (for project registry in future)
    ProjectDBs map[string]*sql.DB         // project_id → events.db

    // Client side
    Clients    map[string]*SimulatedClient // device_id → client
}

type SimulatedClient struct {
    DeviceID          string
    SessionID         string
    DB                *sql.DB    // client's local SQLite (td schema)
    LastPushedAction  int64
    LastPulledSeq     int64
}
```

### Operations

```go
// Create a fresh harness with N clients, each with an empty td-schema database.
func NewHarness(numClients int) *Harness

// Perform a local mutation on a client (insert/update/delete an entity).
// Writes to the entity table AND appends to action_log in one transaction.
func (h *Harness) Mutate(clientID string, actionType string, entityType string, entityID string, data map[string]any) error

// Push a client's pending events to the server's project event store.
func (h *Harness) Push(clientID string, projectID string) (PushResult, error)

// Pull new events from the server and apply to a client's local database.
func (h *Harness) Pull(clientID string, projectID string) (PullResult, error)

// Full sync cycle: push then pull.
func (h *Harness) Sync(clientID string, projectID string) error

// Assert all clients have identical database state (excluding action_log/sync_state).
func (h *Harness) AssertConverged(projectID string) error

// Dump state diff between two clients for debugging.
func (h *Harness) Diff(clientA, clientB string) string
```

### Test Cases (Phase 1 — Minimum Viable)

```go
// 1. Basic: one client creates, other pulls
func TestSingleClientCreate(t *testing.T)
// Client A creates an issue. Push. Client B pulls. Both have the issue.

// 2. Two clients, no conflict
func TestTwoClientsNoConflict(t *testing.T)
// Client A creates issue X. Client B creates issue Y. Both sync. Both have X and Y.

// 3. Update propagation
func TestUpdatePropagation(t *testing.T)
// Client A creates issue. Both sync. Client A updates title. Both sync. Both see new title.

// 4. Delete propagation
func TestDeletePropagation(t *testing.T)
// Client A creates issue. Both sync. Client A deletes it. Both sync. Neither has it.

// 5. Last-write-wins conflict
func TestLastWriteWins(t *testing.T)
// Client A creates issue. Both sync.
// Client A updates title to "A". Client B updates title to "B".
// Client A pushes. Client B pushes. Both pull.
// Both should have title "B" (B pushed last, wins).
// Event log contains both versions via previous_data.

// 6a. Create-delete conflict (delete wins)
func TestCreateDeleteConflict_DeleteLast(t *testing.T)
// Client A creates issue. Both sync.
// Client A updates issue. Client B deletes issue.
// Client A pushes update. Client B pushes delete. Both pull.
// Entity should be deleted (delete arrived last). Update preserved in event log.

// 6b. Create-delete conflict (update wins)
func TestCreateDeleteConflict_UpdateLast(t *testing.T)
// Client A creates issue. Both sync.
// Client A updates issue. Client B deletes issue.
// Client B pushes delete. Client A pushes update. Both pull.
// Entity should exist with Client A's update (update arrived last, re-inserts via new_data).

// 7. Idempotent push
func TestIdempotentPush(t *testing.T)
// Client A pushes the same events twice (simulating retry). Server deduplicates.

// 8. Large batch
func TestLargeBatch(t *testing.T)
// Client A creates 500 issues. Push. Client B pulls. AssertConverged.

// 9. Interleaved sync
func TestInterleavedSync(t *testing.T)
// Alternating mutations and syncs between two clients. Final state converged.

// 10. Multi-entity types
func TestMultiEntityTypes(t *testing.T)
// Issues, logs, handoffs created across clients. All entity types sync correctly.

// 11. Create for existing entity (upsert)
func TestCreateExistingEntity(t *testing.T)
// Client A creates issue. Push. Client B creates same entity ID independently. Client B pulls.
// Client B's local version replaced by server's version via INSERT OR REPLACE.

// 12. Update for missing entity (insert)
func TestUpdateMissingEntity(t *testing.T)
// Client A creates issue, updates it. Push both events.
// Client B pulls only the update event (simulated by skipping create).
// Client B should have the entity from update's new_data.

// 13. Delete for missing entity (no-op)
func TestDeleteMissingEntity(t *testing.T)
// Client A pushes a delete for an entity Client B never had.
// Client B pulls. No error. Entity still absent.

// 14. Update after local delete (re-insert)
func TestUpdateAfterLocalDelete(t *testing.T)
// Client A creates issue. Both sync. Client B deletes locally.
// Client A updates issue. Push. Client B pulls.
// Entity re-appears on Client B with Client A's data.

// 15. Schema version mismatch
func TestSchemaVersionMismatch(t *testing.T)
// Client A sends event with schema_version:2 including an extra field ("priority").
// Client B only knows schema_version:1 (no "priority" column).
// Client B pulls and applies. Unknown field is ignored. Known fields applied correctly.

// 16. Partial batch failure
func TestPartialBatchFailure(t *testing.T)
// Push 10 events where event #5 has a bad entity_type (invalid table).
// Pull on Client B. Events 1-4 and 6-10 applied. Event #5 skipped and logged.
// Cursor advances past all 10.

// 17. Partial payload drops columns (INSERT OR REPLACE safety)
func TestPartialPayloadDropsColumns(t *testing.T)
// Client A creates issue with title="Hello" and status="open". Push. Client B pulls.
// Client A pushes an update event with new_data containing only title="Updated" (no status).
// Client B pulls. Client B's issue should have title="Updated" and status reset to default
// (NULL or column default), NOT the original "open". This validates that INSERT OR REPLACE
// replaces the full row and that callers must always send complete new_data.
```

### Convergence Check

`AssertConverged` compares all entity tables across clients, ignoring sync metadata:

```go
func (h *Harness) AssertConverged(projectID string) error {
    tables := []string{"issues", "logs", "handoffs", "comments", "boards", "work_sessions"}
    for _, table := range tables {
        for i, clientA := range h.clientList() {
            for _, clientB := range h.clientList()[i+1:] {
                rowsA := queryAll(clientA.DB, table)
                rowsB := queryAll(clientB.DB, table)
                if !reflect.DeepEqual(rowsA, rowsB) {
                    return fmt.Errorf("%s diverged between %s and %s:\n%s",
                        table, clientA.DeviceID, clientB.DeviceID,
                        diff(rowsA, rowsB))
                }
            }
        }
    }
    return nil
}
```

### HTML Dashboard (Phase 1b)

After the core tests pass, add a simple HTML report:

- Test name, pass/fail, duration
- For failures: side-by-side database state, event log timeline, conflict details
- Generated as a static HTML file after test runs (`go test -run TestSync -html=report.html`)

Not required for MVP but planned immediately after.

---

## Client-Side Schema Requirements

The test harness needs to create databases with td's schema. The sync library itself doesn't own the schema, but the harness does:

```go
func initClientDB(db *sql.DB) error {
    // Create td's tables: issues, logs, handoffs, comments, boards, work_sessions
    // Create action_log with synced_at column
    // Create sync_state table
    // Use td's actual schema (import from internal/db or duplicate for test isolation)
}
```

Decision: import td's `internal/db.InitSchema()` if it exists as a callable function. If not, extract it. The harness should use the real schema, not a copy that can drift.

**Prerequisite check:** td's existing `action_log` must include `action_type`, `entity_type`, `entity_id`, and a `payload` column with JSON containing `schema_version`, `new_data`, and `previous_data`. If any are missing, a td migration is needed before Phase 0.

---

## Connection Management

The sync library **does not open or manage database connections**. All functions receive `*sql.Tx` from the caller.

- **Test harness**: opens databases directly, creates transactions, passes them in.
- **td client** (future): td's existing connection management creates transactions, passes them in. Sync never competes for the write lock.
- **td-sync server**: opens per-project databases, manages its own connection pool, creates transactions, passes them in.

This means the sync library has zero dependency on any connection management code. It's pure logic operating on transactions.

---

## What's Explicitly Out of Scope

These are not built for MVP testing. They exist in the v3 spec but are deferred:

- HTTP API layer (tested separately later)
- Authentication / authorization
- Device auth flow and verification UI
- Rate limiting
- State cache / materialized views
- Snapshot bootstrap
- Auto-sync triggers (startup, debounce, periodic)
- td CLI commands (`td sync`, `td auth`, etc.)
- Litestream integration
- Docker / deployment
- HTML dashboard (phase 1b, after core tests pass)

---

## Success Criteria

MVP is reached when:

1. All 18 test cases pass (including 6a/6b conflict variants and partial-payload test)
2. `AssertConverged` confirms identical state across clients after every sync scenario
3. Conflict scenarios produce correct last-write-wins results with previous_data preserved in event log
4. Idempotent push deduplication works
5. Idempotency edge cases (upsert, missing entity, no-op delete, re-insert) all handled correctly
6. Partial batch failure skips bad events without blocking sync progress
7. Schema version mismatch handled gracefully (unknown fields ignored)
8. The test suite runs in under 5 seconds (it's all in-process SQLite)
