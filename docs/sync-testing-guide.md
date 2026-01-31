# Sync Testing Guide

## Running Tests

```bash
# All tests
go test ./...

# Sync engine (event application, push/pull logic)
go test ./internal/sync/...

# Server API (HTTP handlers, auth, push/pull endpoints)
go test ./internal/api/...

# Server database (users, keys, projects, memberships)
go test ./internal/serverdb/...

# Multi-client integration (convergence, conflicts, edge cases)
go test ./test/syncharness/...

# Verbose output
go test -v ./test/syncharness/...
```

## Test Architecture

Tests are layered from low-level to high-level:

```
test/syncharness/          ← Multi-client integration (18 scenarios)
  │  Uses real sync functions, simulates N devices
  │
internal/api/              ← HTTP integration (server_test.go)
  │  Full request/response cycle via httptest
  │
internal/sync/             ← Sync engine unit tests
  ├─ events_test.go        ← Entity upsert/delete, validation, overwrites (18 tests)
  ├─ client_test.go        ← GetPendingEvents, ApplyRemoteEvents, conflicts (11 tests)
  └─ engine_test.go        ← Server insert, dedup, pagination, device exclusion (7 tests)
  │
internal/serverdb/         ← Database unit tests (30+ tests)
     Users, API keys, projects, memberships, role enforcement
```

All tests use in-memory SQLite for isolation and speed.

## Test Harness

The harness in `test/syncharness/harness.go` simulates multiple devices syncing through a shared server database. It uses the real sync functions (not mocks), so it validates the actual code paths.

### Setup

```go
func TestMyScenario(t *testing.T) {
    h := NewHarness(t, 2, "proj-1")  // 2 clients, 1 project
    // ...
}
```

This creates:
- One server-side event database
- N client databases, each with the local schema (issues, logs, comments, etc.)
- Each client gets a unique device ID

### Core Operations

```go
// Record a local change (writes to client's action_log)
h.Mutate("client-A", "create", "issues", "i1", map[string]any{
    "id": "i1", "title": "Bug", "status": "open",
})

// Push pending events to server
h.Push("client-A", "proj-1")

// Pull remote events and apply locally
h.Pull("client-B", "proj-1")

// Push then pull (convenience)
h.Sync("client-A", "proj-1")

// Pull including own device's events (useful for convergence checks)
h.PullAll("client-A", "proj-1")
```

### Assertions

```go
// Verify all clients have identical entity data
// Excludes timestamp columns (created_at, updated_at, etc.)
h.AssertConverged("proj-1")

// Query a specific entity
row := h.QueryEntity("client-A", "issues", "i1")

// Count entities
n := h.CountEntities("client-B", "issues")
```

### Supported Entity Types

The harness schema includes: `issues`, `logs`, `comments`, `handoffs`, `boards`, `work_sessions`.

### Example: Conflict Scenario

```go
func TestConflict(t *testing.T) {
    h := NewHarness(t, 2, "proj-1")

    // Both clients create the same entity independently
    h.Mutate("client-A", "create", "issues", "i1", map[string]any{
        "id": "i1", "title": "Version A", "status": "open",
    })
    h.Mutate("client-B", "create", "issues", "i1", map[string]any{
        "id": "i1", "title": "Version B", "status": "open",
    })

    // A pushes first, then B pushes (B gets higher server_seq)
    h.Push("client-A", "proj-1")
    h.Push("client-B", "proj-1")

    // Both pull all events — last-write-wins (B's version)
    h.PullAll("client-A", "proj-1")
    h.PullAll("client-B", "proj-1")

    h.AssertConverged("proj-1")
}
```

## What's Tested

### Sync Engine (`internal/sync/`)

**Event application** (`events_test.go`):
- Create, update, delete, soft_delete operations
- Overwrite detection (returns old data when replacing existing row)
- Validation: rejects nil payload, empty entity ID, malformed JSON
- SQL injection prevention via column name validation
- INSERT OR REPLACE behavior (partial payloads reset unspecified columns to DEFAULT)

**Client-side sync** (`client_test.go`):
- `GetPendingEvents` reads unsynced action_log entries, skips undone/synced rows
- Action type mapping (td actions like `start`, `close` → sync types `create`, `update`, `delete`)
- `ApplyRemoteEvents` batch application with conflict tracking
- Conflict records contain both local and remote data as JSON
- `MarkEventsSynced` updates action_log with server_seq and synced_at

**Server-side storage** (`engine_test.go`):
- Batch insert with sequential server_seq assignment
- Deduplication via `(device_id, session_id, client_action_id)` unique constraint
- Pagination with `limit` and `HasMore` flag
- Device exclusion filter (`exclude_client`)
- Validation rejection (empty device_id, etc.)

### Server API (`internal/api/`)

- Auth enforcement (401 without Bearer token)
- Push: accepts events, returns acks with server_seqs
- Pull: pagination, device exclusion
- Project CRUD and membership
- Role enforcement (owner vs writer vs reader permissions)
- Health endpoint

### Server Database (`internal/serverdb/`)

- Full CRUD for users, API keys, projects, memberships
- Role-based authorization (writer can push, reader cannot)
- Last-owner protection (can't remove sole owner)
- Member removal revokes access
- API key expiry checks

### Multi-Client Integration (`test/syncharness/`)

| Test | Scenario |
|---|---|
| SingleClientCreate | A creates, B pulls |
| TwoClientsNoConflict | A and B create different issues |
| UpdatePropagation | Create + update + pull |
| DeletePropagation | Create + delete + pull |
| LastWriteWins | A and B update same issue; later push wins |
| CreateDeleteConflict (x2) | Delete vs update ordering determines outcome |
| IdempotentPush | Double-push → second has 0 accepted |
| LargeBatch | 500 issues, full convergence |
| InterleavedSync | Mixed creates/updates/deletes across clients |
| MultiEntityTypes | Issues, logs, comments, boards, work_sessions |
| CreateExistingEntity | Same entity ID created independently on two clients |
| UpdateMissingEntity | Update for entity never pulled → upserts |
| DeleteMissingEntity | Delete for unknown entity → no-op |
| UpdateAfterLocalDelete | Remote update resurrects locally deleted entity |
| SchemaVersionMismatch | Unknown payload fields ignored |
| PartialBatchFailure | Bad events skipped, good ones applied |
| PartialPayloadDropsColumns | Partial update resets omitted columns to DEFAULT |

## Planned Tests (Phase 3.5)

The following gaps are tracked as tasks under the sync epic (`td-bf126c`):

- **Phase 3.5a** (`td-ddefab`) -- Concurrent push test, crash-recovery test
- **Phase 3.5b** (`td-3b75aa`) -- Rate limit integration test, long-session pagination test (5000+ events)
- **Phase 3.5c** (`td-49849f`) -- HTTP-layer `exclude_client` test
