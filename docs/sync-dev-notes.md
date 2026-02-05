# td-sync: Development Notes

Notes on implementation approach, testing strategy, and open decisions for the sync server.

---

## Build Order

1. **Test harness first** — before writing any sync code, build a harness that can spin up two SQLite databases (client + server), simulate latency, conflicts, schema mismatches, etc. This makes iteration fast and keeps the sync engine debuggable (ideally by AI agents too).

2. **Sync engine** — the core push/pull logic, tested via direct Go API calls (no HTTP yet).

3. **HTTP layer** — built on top of the sync engine, with a separate test suite exercising the HTTP endpoints.

4. **Client integration into td** — the sync client should be a clean library with minimal coupling to the td application. It comes almost directly from what the test harness already exercises.

---

## Test Harness Design

- Each test run creates two databases: one client-side, one server-side.
- The harness includes an action log emitter that can generate events or replay them from an existing database (e.g., real action logs from td or Sidecar).
- Expected result for every test: both databases end up identical. If there was a conflict, both versions are preserved (winner in the main table, loser in the action log).
- The harness should handle both fast and slow sync intervals.
- An **HTML dashboard** shows test run outputs and any conflicts.
- Testing should be bidirectional — not just client→server, but also server→client.

### Using Real Data

Use action logs from td and Sidecar as test fixtures. They contain real schema changes and real data, which is useful for testing replay behavior and schema evolution.

---

## Sync Engine Notes

### Transport

HTTP is simplest — everything is pull-based (POST/GET), no incoming connections needed. No WebSockets required for phase 1.

### Polling Frequency

- Start aggressive (e.g., 1s), then degrade with inactivity (down to 10–15s).
- User should be able to pause sync entirely (built into td).
- The test suite should be agnostic to frequency — handle both fast and slow.

### Conflict Resolution

- Newest version wins (last-write-wins by server arrival order, per the spec).
- The clobbered version **must be preserved** — save it to an action log row or similar. It should be recoverable even if we don't build UI for it now.
- Both the winning and losing versions get synced back, so after sync completes both databases should be identical including the conflict record.

### Action Log as the Key

- The action log ID (server_seq) is the central cursor for sync, not timestamps on entity rows.
- This is more forgiving when tables have inconsistent timestamps.
- UTC timestamps in the action log are important, but the server should be the source of truth for ordering (not client clocks).

### Initial Sync / Bootstrap

- For small projects, replaying all events works fine (even 10k tasks isn't that slow).
- For larger projects, snapshot bootstrap is now implemented: the client downloads a pre-built SQLite database from the server and then pulls only events after the snapshot point. The snapshot threshold is configurable (default 100 events; set `TD_SYNC_SNAPSHOT_THRESHOLD=0` to disable and force event replay).
- The server caches snapshots at `{dataDir}/snapshots/{projectID}/{seq}.db`, rebuilding only when the sequence advances.
- Avoid sending massive HTTP payloads (25MB+) — batch appropriately.

### Connection Code

- Borrow the single-writer connection handling from td to prevent concurrent write issues.

---

## Architecture Layers

Build and test these independently:

1. **Authentication layer** — controls access at the connection level. Two tiers for now: read-write (team) and read-only (e.g., public viewers). Auth token scoped to an ACL. Completely separate from the sync engine.

2. **Sync engine** — core push/pull logic, tested via direct Go calls.

3. **HTTP API** — thin layer on top, with its own test suite.

---

## Client Integration

- The sync client is a library extracted from the test harness.
- Minimal interaction with td's application code.
- td's UI should update reactively after a sync completes.
- Sync should start/stop at the right lifecycle points.

---

## Deployment

- Should be an extremely straightforward, interactive setup script.
- The script gives feedback at every step and is **idempotent** — if something fails, the user fixes it and reruns from the same point.
- User choices are saved as they go through setup.

---

## Auth Tokens

- If a user has an env var with their auth token, it should work across all their databases.
- Auth abstraction layer is separate from sync, so scoping tokens to specific databases can come later.
- Should support tokens for cloud AI agents (e.g., Claude Code in the cloud getting its own token).

---

## Future Consideration

- Could the td-sync server work as a generic sync server for other issue management systems? td itself would only know about its own database; the server handles the rest. Not a priority, but worth keeping the architecture open to it.

---

## Open from These Notes

- Should we store full entity snapshots in the action log on every change? Enables full history replay. Cost: ~2x database size (could compress as blobs). Could store now, build UI later. Worth considering but might be overengineering for phase 1.
- Exact polling degradation curve and configuration.
- How to handle clock skew between client devices (lean toward trusting server timestamps).
