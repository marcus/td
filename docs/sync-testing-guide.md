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

# E2E chaos oracle (black-box, builds real binaries, starts real server)
go test -v -count=1 -timeout 5m -run TestSmoke ./test/e2e/

# Full chaos run
go test -v -count=1 -timeout 30m -run TestChaosSync ./test/e2e/ \
    -args -chaos.actions=500 -chaos.seed=42

# Individual conflict scenarios
go test -v -count=1 -timeout 5m -run TestPartitionRecovery ./test/e2e/
```

## Test Architecture

Tests are layered from low-level to high-level:

```
test/e2e/                  ← Black-box chaos oracle (builds real binaries)
  │  Spawns td + td-sync, authenticates actors, runs CLI commands
  │
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

The `test/syncharness/` and lower layers use in-memory SQLite for isolation and speed. The `test/e2e/` layer builds real binaries, starts a real server on a random port, and exercises the full CLI-to-server path.

## Test Harness (syncharness)

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

## E2E Chaos Oracle (`test/e2e/`)

The chaos oracle is a black-box test layer that builds real `td` and `td-sync` binaries, starts a real sync server, authenticates multiple actors via the device auth flow, and drives randomized mutations through the CLI. It then verifies sync correctness using 8 independent oracle checks.

### How It Works

1. **Setup**: Builds binaries, starts server on random port, creates project, authenticates 2-3 actors (alice, bob, optionally carol)
2. **Chaos loop**: Weighted random action selection (43 action types), configurable sync timing, conflict injection
3. **Final sync**: Round-robin convergence sync with rate-limit retry
4. **Verification**: 8 correctness oracle checks run against actor databases

### Test Functions

| Test | What it does |
|---|---|
| `TestSmoke` | 10-action quick run, convergence + idempotency checks |
| `TestChaosSync` | Full configurable chaos run with all 8 verifications |
| `TestPartitionRecovery` | 50+ mutations per side without sync, then converge |
| `TestUndoSync` | Create, sync, undo, sync, verify convergence |
| `TestMultiFieldCollision` | Concurrent updates to different fields on same issue |
| `TestRapidCreateDelete` | Create 10, delete all, restore 5, delete 3 — convergence at each stage |
| `TestCascadeConflict` | Parent cascade vs independent child close |
| `TestDependencyCycle` | A->B->C->A cycle detection |
| `TestThunderingHerd` | Concurrent goroutine sync (both actors sync simultaneously) |
| `TestBurstNoSync` | 20 sequential mutations without sync, then converge |
| `TestServerRestart` | Create, sync, kill server, offline mutations, restart, converge |

### CLI Flags

All flags are passed after `-args`:

```bash
go test -run TestChaosSync ./test/e2e/ -args [FLAGS]
```

| Flag | Default | Description |
|---|---|---|
| `-chaos.seed` | 0 (time-based) | PRNG seed for reproducibility |
| `-chaos.actions` | 100 | Total actions to perform |
| `-chaos.duration` | 0 | Seconds to run (overrides actions when >0) |
| `-chaos.actors` | 2 | Number of actors: 2 or 3 |
| `-chaos.verbose` | false | Per-action output |
| `-chaos.sync-mode` | adaptive | Sync strategy: adaptive, aggressive, random |
| `-chaos.conflict-rate` | 20 | Percentage of conflict rounds |
| `-chaos.json-report` | "" | Write JSON report to file |

### Example Runs

```bash
# Quick CI smoke test
go test -v -count=1 -timeout 5m -run TestSmoke ./test/e2e/

# Standard chaos run
go test -v -count=1 -timeout 30m -run TestChaosSync ./test/e2e/ \
    -args -chaos.actions=500 -chaos.seed=42

# Three-actor test
go test -v -count=1 -timeout 30m -run TestChaosSync ./test/e2e/ \
    -args -chaos.actions=100 -chaos.actors=3

# Aggressive sync (sync every 1-3 actions)
go test -v -count=1 -timeout 10m -run TestChaosSync ./test/e2e/ \
    -args -chaos.actions=200 -chaos.sync-mode=aggressive

# Time-based with JSON report
go test -v -count=1 -timeout 10m -run TestChaosSync ./test/e2e/ \
    -args -chaos.duration=60 -chaos.json-report=report.json

# Reproducible failure investigation
go test -v -count=1 -timeout 30m -run TestChaosSync ./test/e2e/ \
    -args -chaos.seed=1770081563082117000 -chaos.verbose
```

### Correctness Verifications

The oracle runs these checks after the chaos loop completes:

| Verification | What it checks |
|---|---|
| **Entity convergence** | All synced tables (issues, comments, logs, handoffs, deps, boards, positions, files, work sessions) match across actors |
| **Action log convergence** | For every common server_seq, the (entity_type, action_type, entity_id) tuple matches |
| **Monotonic sequence** | server_seq is strictly increasing with no duplicates per actor |
| **Causal ordering** | Create events precede updates/deletes for same entity; start precedes review |
| **Idempotency** | SHA-256 hash of all table dumps is stable across N additional sync rounds |
| **Event counts** | Synced event counts and type distributions match (warnings for expected init divergence) |
| **Read-your-writes** | Each actor can `td show` every non-deleted issue they created |
| **Field-level merge** | Concurrent updates to different fields on same issue both survive sync |

### Action Coverage

The chaos engine executes 43 weighted-random action types:

- **CRUD**: create, update, update_append, delete, restore, update_bulk
- **Status**: start, unstart, review, approve, reject, close, reopen, block, unblock
- **Bulk**: bulk_start, bulk_review, bulk_close
- **Content**: comment, log_progress, log_blocker, log_decision, log_hypothesis, log_result
- **Dependencies**: dep_add, dep_rm
- **Boards**: board_create, board_edit, board_move, board_unposition, board_delete, board_view_mode
- **Handoffs**: handoff
- **File links**: link, unlink
- **Work sessions**: ws_start, ws_tag, ws_untag, ws_end, ws_handoff
- **Parent-child**: create_child, cascade_handoff, cascade_review

15% of generated content uses edge-case strings (emoji, CJK, RTL Arabic, SQL injection attempts, XSS payloads, long strings, format specifiers, embedded JSON, etc.).

### JSON Report

When `-chaos.json-report=path` is set, the test writes a structured report:

```json
{
  "seed": 42,
  "actions": 500,
  "duration_ms": 45000,
  "actors": 2,
  "results": {
    "total": 500,
    "ok": 380,
    "expected_fail": 45,
    "unexpected_fail": 0,
    "skipped": 75
  },
  "per_action": { "create": {"ok": 60, ...}, ... },
  "verifications": [
    {"name": "issues match", "passed": true},
    {"name": "monotonic server_seq", "passed": true, "details": "450 events, range [1..620]"}
  ],
  "sync_stats": {"count": 85},
  "pass": true
}
```

### File Layout

```
test/e2e/
├── harness.go          # Build binaries, start server, auth, project setup
├── harness_test.go     # Basic create-sync-verify test
├── engine.go           # ChaosEngine: state tracking, action execution
├── engine_test.go      # Engine smoke test + weight distribution
├── actions.go          # 43 action executors
├── selection.go        # Weighted random action selection
├── random.go           # Edge-case content generators
├── history.go          # Thread-safe operation history (JSON/text reports)
├── history_test.go     # History concurrency + serialization tests
├── verify.go           # 8 correctness oracle verifications
├── verify_test.go      # Verification unit tests
├── conflicts.go        # 8 advanced conflict scenarios
├── conflicts_test.go   # Scenario test runners
├── restart.go          # Server restart resilience scenario
├── restart_test.go     # Restart test runner
├── chaos_test.go       # TestSmoke + TestChaosSync (main entry points)
└── report.go           # JSON report generation
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

### E2E Chaos Oracle (`test/e2e/`)

See the [E2E Chaos Oracle](#e2e-chaos-oracle-teste2e) section above for full details. In summary:

- 2 main test entry points (TestSmoke, TestChaosSync)
- 9 targeted conflict/resilience scenarios
- 43 weighted-random action types with edge-case content injection
- 8 independent correctness verifications
- Reproducible via PRNG seed
- JSON report output for CI

### Bash E2E Tests (`scripts/e2e/`)

Comprehensive bash-based test suite with 13 specialized test scripts:

**Core Infrastructure:**
- `harness.sh` — Build binaries, start server, auth, project setup, server lifecycle, late-joiner setup
- `chaos_lib.sh` — 44 action executors, weighted selection, state tracking, verification functions

**Main Chaos Test:**
- `test_chaos_sync.sh` — Configurable randomized 2-3 actor mutations with convergence verification, mid-test checks, conflict injection

**Scenario Tests:**
- `test_network_partition.sh` — Client offline, batch accumulation, reconnect sync
- `test_late_join.sh` — New client joins after 50+ issues exist, full history transfer
- `test_server_restart.sh` — Server crash/restart resilience, data durability
- `test_create_delete_recreate.sh` — Tombstone vs new-entity disambiguation
- `test_parent_delete_cascade.sh` — Orphan handling when parent deleted
- `test_large_payload.sh` — 10K-50K descriptions, 50-200 comments, 20-80 dependencies
- `test_event_ordering.sh` — Causal ordering verification (creates before updates, parents before children)

**Other Tests:**
- `test_basic_sync.sh` — Simple bidirectional sync
- `test_alternating_actions.sh` — Interleaved actor mutations
- `test_autosync_*.sh` — Auto-sync behavior tests
- `test_monitor_autosync.sh` — TUI monitor + sync integration

**Regression Infrastructure:**
- `run_regression_seeds.sh` — Run known seeds from `regression_seeds.json`
- `regression_seeds.json` — Database of reproducible test seeds for CI

The bash tests provide fast iteration, easy debugging, and comprehensive scenario coverage. See `scripts/e2e/e2e-sync-test-guide.md` for full documentation.

## Planned Tests (Phase 3.5)

The following gaps are tracked as tasks under the sync epic (`td-bf126c`):

- **Phase 3.5a** (`td-ddefab`) -- Concurrent push test, crash-recovery test
- **Phase 3.5b** (`td-3b75aa`) -- Rate limit integration test, long-session pagination test (5000+ events)
- **Phase 3.5c** (`td-49849f`) -- HTTP-layer `exclude_client` test
