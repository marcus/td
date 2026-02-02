# Sync: Agent Onboarding Guide

Current state of td-sync, how it works, how it's tested, and where to go next. Read this before working on any sync feature.

## Status

Sync is **implemented and functional**. Two clients can push/pull events through a self-hosted server, with auto-sync, snapshot bootstrap, conflict detection, and team membership. The tracking epic is **td-bf126c** (`td show td-bf126c`).

What works today:
- Event-based replication (push/pull over HTTPS)
- Last-write-wins conflict resolution with conflict recording
- Auto-sync (on-start, post-mutation debounce, periodic interval)
- Snapshot bootstrap for large projects (configurable threshold, default 100 events)
- Device auth flow (no SMTP dependency)
- Team membership with roles (owner/writer/reader)
- Orphan entity backfill (pre-action-log data gets synthetic create events)
- Soft deletes for board_issue_positions (prevents resurrection on event replay)
- Rate limiting, structured logging, health/metrics endpoints

## Architecture

```
td client (SQLite + sync lib)  ──HTTPS──▶  td-sync server (Go binary)
                                              ├─ server.db (users, keys, projects, members)
                                              └─ /data/projects/{id}/events.db (append-only event log)
```

Key constraint: `internal/sync` never imports `internal/db` or any td-specific package. It receives `*sql.Tx` from callers.

### Code layout

| Path | What |
|---|---|
| `internal/sync/` | Shared sync library (client + server). Event application, backfill, push/pull logic |
| `internal/syncclient/` | HTTP client for td-sync API (auth, push, pull, snapshot, members) |
| `internal/syncconfig/` | Config loading (env vars > config.json > defaults), auth credential storage |
| `internal/api/` | HTTP server: routing, auth, sync endpoints, rate limiting, metrics |
| `internal/serverdb/` | Server-side SQLite (users, API keys, projects, memberships) |
| `cmd/td-sync/` | Server binary entry point |
| `cmd/autosync.go` | Client-side auto-sync (startup, debounce, periodic) |
| `cmd/autosync_push.go` | Push batching (500 events/batch, server max 1000) |
| `cmd/sync.go` | `td sync` command |
| `cmd/sync_project.go` | `td sync-project` (link/unlink/create/list) |
| `cmd/auth.go` | `td auth` (login/logout/status) |
| `test/syncharness/` | Go integration test harness (multi-client simulation) |
| `scripts/e2e/` | Bash e2e test suite |

### Sync library files (`internal/sync/`)

| File | Purpose |
|---|---|
| `types.go` | Core types: `Event`, `PushResult`, `ApplyResult`, `ConflictRecord`, `EntityValidator` |
| `engine.go` | Server-side: `InsertServerEvents`, `GetEventsSince`, timestamp parsing |
| `client.go` | Client-side: `GetPendingEvents` (includes backfill), `ApplyRemoteEvents`, `MarkEventsSynced` |
| `events.go` | Event application: `ApplyEvent` routes to upsert/delete. Dynamic SQL from JSON. Column validation |
| `backfill.go` | Orphan entity detection + synthetic action_log creation. Only runs before first pull |

### Entity types

Synced entities and their action_log aliases:

| Table | action_log type | Aliases checked |
|---|---|---|
| `issues` | `issue` | issue, issues |
| `logs` | `logs` | log, logs |
| `comments` | `comments` | comment, comments |
| `handoffs` | `handoff` | handoff, handoffs |
| `boards` | `boards` | board, boards |
| `work_sessions` | `work_sessions` | work_session, work_sessions |
| `board_issue_positions` | `board_position` | board_position, board_issue_positions (soft_delete for removes) |
| `work_session_issues` | `work_session_issue` | work_session_issue, work_session_issues |
| `issue_dependencies` | `dependency` | dependency, issue_dependencies |
| `issue_files` | `file_link` | file_link, issue_files |

Normalization happens in `normalizeEntityType()` (`client.go`). The server allowlist in `internal/api/sync.go` is broader (includes `git_snapshots`, `issue_session_history`, etc.).

### Event flow

1. Local mutation writes to entity table + `action_log` in same transaction
2. `GetPendingEvents` reads unsynced action_log rows, runs backfill first if needed
3. Events wrapped with `{schema_version, new_data, previous_data}` payload
4. `autoSyncPush` batches into 500-event chunks, POSTs to server
5. Server assigns `server_seq`, stores in project's `events.db`
6. Other clients pull via `after_server_seq`, apply with `INSERT OR REPLACE`

### Backfill

Entities predating action logging have no action_log entries. `BackfillOrphanEntities` (called at top of `GetPendingEvents`) scans all syncable tables for rows with no matching action_log entry and inserts synthetic "create" events. Guard: only runs when `last_pulled_server_seq == 0` (before first pull), since pulled entities also lack action_log entries.

### Conflict resolution

Last-write-wins by server arrival order. Update events use field-level merge: `previous_data` and `new_data` are diffed to apply only changed columns, so concurrent edits to different fields are both preserved. When a remote event overwrites a locally-modified entity (local `updated_at` > `last_sync_at`), a `ConflictRecord` is created with both local and remote data. Stored in `sync_conflicts` table.

## Testing

### Go tests

```bash
go test ./...                           # everything
go test ./internal/sync/...             # sync engine
go test ./internal/api/...              # HTTP layer
go test ./internal/serverdb/...         # server DB
go test ./test/syncharness/...          # multi-client integration (18 scenarios)
go test ./cmd/ -run AutoSyncPush        # push batching
```

The Go harness (`test/syncharness/harness.go`) simulates N clients with real sync functions against in-memory SQLite. Key methods: `Mutate`, `Push`, `Pull`, `Sync`, `PullAll`, `AssertConverged`. See [sync-testing-guide.md](sync-testing-guide.md) for the full scenario list.

### E2E bash tests

```bash
bash scripts/e2e/run-all.sh            # core tests
bash scripts/e2e/run-all.sh --full     # + real-data tests (needs local DBs)
bash scripts/e2e/test_basic_sync.sh    # single test
```

The bash harness (`scripts/e2e/harness.sh`) builds both binaries, starts a server on a random port, creates two isolated clients (alice + bob), authenticates both, and creates a shared project. Provides `td_a`/`td_b` wrappers, `assert_eq`/`assert_ge`/`assert_contains`/`assert_json_field`, `wait_for` polling, and `report`.

| Test | What it covers |
|---|---|
| `test_basic_sync.sh` | Bidirectional manual sync, issue creation + verification |
| `test_autosync_propagation.sh` | Auto-sync with debounce, status updates propagate |
| `test_autosync_on_start_list.sh` | on_start sync, create/review workflow |
| `test_alternating_actions.sh` | Multi-round alternating mutations, DB convergence |
| `test_chaos_sync.sh` | Randomized stress test: ~28 action types, arbitrary content, conflict injection, full convergence |
| `test_sync_real_data.sh` | Real DB seed, push batching, backfill (--full only) |
| `test_sync_real_data_all_projects.sh` | All local project DBs (--full only) |

See [e2e-sync-test-guide.md](../scripts/e2e/e2e-sync-test-guide.md) for writing new e2e tests.

**Extending the chaos test**: When adding new sync features or syncable mutations, add a corresponding `exec_<action>` function in `scripts/e2e/chaos_lib.sh` and register it in the `ACTION_WEIGHTS` array. This ensures new features are exercised under randomized multi-client conditions with conflict injection and convergence verification.

## Related docs

| Doc | Contents |
|---|---|
| [sync-client-guide.md](sync-client-guide.md) | Client setup, auth, commands, auto-sync config, env vars |
| [sync-server-ops-guide.md](sync-server-ops-guide.md) | Server build, deployment, Docker, Litestream backup, config |
| [sync-testing-guide.md](sync-testing-guide.md) | Test architecture, harness API, all 18 integration scenarios |
| [sync-dev-notes.md](sync-dev-notes.md) | Design rationale, architecture decisions |
| [sync-mvp-testing-spec.md](sync-mvp-testing-spec.md) | Original MVP test spec (18 cases, all implemented) |
| [implemented/sync-plan-03-merged.md](implemented/sync-plan-03-merged.md) | Original v3 spec (reference only, implementation is source of truth) |

## Mutation logging contract

Every mutation to a synced entity **must** write to `action_log` with `PreviousData` and `NewData` JSON snapshots, or the change won't sync. The pattern is:

```go
prevData, _ := json.Marshal(issue)   // snapshot before
issue.Status = models.StatusClosed
m.DB.UpdateIssue(issue)
newData, _ := json.Marshal(issue)    // snapshot after
m.DB.LogAction(&models.ActionLog{
    SessionID:    sessionID,
    ActionType:   models.ActionClose,
    EntityType:   "issue",
    EntityID:     issue.ID,
    PreviousData: string(prevData),
    NewData:      string(newData),
})
```

This applies to **all** mutation sites: CLI commands (`cmd/`), TUI monitor actions (`pkg/monitor/actions.go`, `pkg/monitor/form_operations.go`), and cascade helpers (`internal/db/issue_relations.go`). Cascaded child updates (e.g. closing all children of an epic) need individual `LogAction` calls per child — not just for the parent.

If you add a new mutation path (new command, new TUI action, new cascade), verify it writes to `action_log` with both snapshots. Missing `NewData` means the sync engine has nothing to push.

## Known issues

- **Event replay ordering**: "update" events with full JSON snapshots use `INSERT OR REPLACE`, which can re-create rows that were hard-deleted during replay. `board_issue_positions` is fixed — it now uses soft deletes (`deleted_at` column) so replayed updates don't resurrect removed positions. Other entity types can still re-create hard-deleted rows in rare cases; e2e real-data tests use `assert_ge` for this reason.
- **Field-level merge for updates** (fixed in `td-a729f0`): Update events now diff `previous_data` vs `new_data` to apply only changed fields via `UPDATE ... SET`, preserving concurrent edits to different columns. Falls back to full `INSERT OR REPLACE` when `previous_data` is missing or the row doesn't exist locally.
- **Entity type aliases are fragile**: The action_log uses both singular and plural forms inconsistently across the codebase. `normalizeEntityType` and the backfill alias table must stay in sync manually.

## Future directions

Tracked under epic **td-bf126c**. Planned/possible work:

**Near-term (tracked tasks)**:
- Concurrent push stress test (`td-ddefab`)
- Rate limit integration test (`td-3b75aa`)
- HTTP-layer `exclude_client` test (`td-49849f`)
- `td doctor` diagnostics command

**Medium-term**:
- Email-based auth (magic links for collaboration invites)
- Invite flow UX
- Webhook notifications on push

**Long-term / exploratory**:
- **End-to-end encryption**: Encrypt event payloads client-side before push. Server stores opaque blobs, cannot read entity data. Key management via project-scoped symmetric keys shared out-of-band or via public-key exchange. Could be a "pro" tier feature since it requires key escrow/recovery UX and prevents server-side snapshot generation.
- Read-only web view (requires server-side state cache, deferred from Phase 1)
- Conflict detection/resolution UI
- API for third-party integrations
