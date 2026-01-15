# Remote Sync Options for td - Research Analysis

## Current Architecture Summary

**Database**: SQLite with WAL mode, stored at `.todos/issues.db`
**Language**: Go with pure-Go SQLite (`modernc.org/sqlite`)
**Core Models**: Issue, Log, Handoff, WorkSession, Board, ActionLog
**Existing Features**:

- File-based write locking for multi-process safety
- Session management with agent fingerprinting
- No existing export/import/sync functionality

---

## Sync Options Analysis

### Option 1: Turso/libSQL (RECOMMENDED - Lowest Complexity)

**What it is**: Drop-in SQLite replacement with built-in cloud sync

**Pros**:

- Near-identical SQLite API - minimal code changes
- Go SDK available (`github.com/tursodatabase/go-libsql`)
- Push/pull sync model (sync on demand, not continuous)
- Embedded replicas work offline
- Free tier: 9GB storage, 500 DBs

**Cons**:

- Requires Turso cloud account (or self-host)
- No true peer-to-peer (hub-and-spoke model)
- Rust rewrite in progress (API may evolve)

**Implementation Effort**: Low

- Replace `modernc.org/sqlite` with `go-libsql`
- Add `td sync push` / `td sync pull` commands
- Store remote URL + token in config

**Files to modify**: `internal/db/db.go`, add `cmd/sync.go`

---

### Option 2: rqlite (BEST for Future Web UI)

**What it is**: Distributed SQLite using Raft consensus, written in Go

**Pros**:

- Go-native (perfect match for td)
- Built-in HTTP/JSON API (enables web UI trivially)
- Strong consistency via Raft
- 10 years mature, battle-tested
- CDC (Change Data Capture) in v9.0
- gorqlite client library available

**Cons**:

- Requires running rqlite server(s)
- Not suited for high-write workloads
- More ops overhead than Turso

**Implementation Effort**: Medium

- Add rqlite client (`gorqlite`)
- Either: connect to rqlite instead of local SQLite
- Or: hybrid mode with local + remote sync

**Files to modify**: `internal/db/db.go`, new `internal/db/rqlite.go`, add `cmd/sync.go`

---

### Option 3: CR-SQLite (BEST for True P2P)

**What it is**: CRDT extension enabling multi-writer SQLite merge

**Pros**:

- True peer-to-peer (no central server required)
- Automatic conflict resolution
- Can work with any transport (git, S3, custom)
- Open source, no vendor lock-in

**Cons**:

- 2.5x slower inserts
- Requires schema changes (UUID PKs, CRDT columns)
- More complex to reason about
- C extension (may complicate Go integration)

**Implementation Effort**: High

- Schema migration to CRDT-friendly format
- Integrate cr-sqlite extension
- Build sync transport layer

---

### Option 4: Git-Based Sync (Simplest MVP)

**What it is**: Serialize DB to JSON/YAML, commit to git

**Pros**:

- Zero infrastructure
- Leverages existing git workflows
- Human-readable history
- Works offline naturally

**Cons**:

- Manual conflict resolution via git
- Not real-time
- Scales poorly with large datasets
- Merge conflicts likely on concurrent edits

**Implementation Effort**: Low-Medium

- Add `td export --json` / `td import --json`
- Users commit `.todos/` or exported file to git

---

### Option 5: Litestream + S3 (Backup Only)

**What it is**: Continuous WAL streaming to object storage

**Pros**:

- Simple disaster recovery
- Point-in-time restore
- No code changes needed

**Cons**:

- One-way replication only
- Not multi-user sync
- Requires S3/compatible storage

**Implementation Effort**: None (ops-only)

---

## Recommendation Matrix

| Requirement       | Turso | rqlite | CR-SQLite | Git-sync |
| ----------------- | ----- | ------ | --------- | -------- |
| Multi-user sync   | ✓     | ✓      | ✓         | ✓\*      |
| Offline support   | ✓     | ✗      | ✓         | ✓        |
| Low complexity    | ✓✓    | ✓      | ✗         | ✓        |
| Web UI path       | ✓     | ✓✓     | ✓         | ✗        |
| No vendor lock-in | ✗     | ✓      | ✓         | ✓        |
| True P2P          | ✗     | ✗      | ✓         | ✓        |

\*Git-sync requires manual merge resolution

---

## Final Recommendation: Turso

Given your requirements (offline nice-to-have, minimal ops, web UI possible later):

**Why Turso**:

1. **Minimal code changes** - Same SQLite API, just swap driver
2. **Zero ops** - Managed cloud service, no servers to run
3. **Explicit sync** - `td sync push/pull` matches CLI philosophy
4. **Offline works** - Embedded replicas function without network
5. **Web UI path** - Can build on top of Turso's HTTP API later
6. **Free tier** - 9GB storage, 500 DBs covers most teams

**What changes in td**:

- Replace `modernc.org/sqlite` with `github.com/tursodatabase/go-libsql`
- Add config for remote DB URL + auth token
- Add `td sync` command with `push`/`pull` subcommands
- Add `td sync init` to set up remote connection

**Estimated scope**: ~200-400 lines of new code

---

## Alternative Paths

**rqlite** - Choose if you later want:

- Built-in HTTP API for web UI
- Self-hosted infrastructure
- Strong consistency guarantees

**Git-based export** - Choose if you want:

- Zero external dependencies
- Human-readable sync format
- Accept manual conflict resolution

---

## Sources

- [Turso](https://turso.tech/) - Databases Everywhere
- [Turso Sync](https://turso.tech/blog/introducing-databases-anywhere-with-turso-sync) - Push/pull sync
- [rqlite](https://github.com/rqlite/rqlite) - Distributed SQLite
- [rqlite 9.0 CDC](https://philipotoole.com/rqlite-9-0-real-time-change-data-capture-for-distributed-sqlite/)
- [CR-SQLite](https://github.com/vlcn-io/cr-sqlite) - CRDT SQLite extension
- [Litestream](https://litestream.io/) - SQLite streaming replication
- [SQLite Sync Comparison 2025](https://onidel.com/blog/sqlite-replication-vps-2025)
