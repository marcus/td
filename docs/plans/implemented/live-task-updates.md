# Plan: live task updates for every td surface

Status: implemented
Owner repo: `td` (this repo holds the large majority of the work)
Related: closed Sidecar PR #311 `fix(td): pull hosted changes in embedded monitor` — superseded by the td-owned implementation
Affected surfaces: `td monitor`, Sidecar's embedded monitor (`internal/plugins/tdmonitor`), td-watch, `td` CLI

## Outcome

A task created or changed anywhere — the hosted browser UI, another machine's CLI, a teammate's device — appears in every open td surface within a second or two on a healthy live connection, without fixed-timer polling on the normal path and without any consumer of td re-implementing td's sync gate. Fixed-interval probes and full syncs remain as degradation fallbacks.

Implemented behavior:

- Sidecar's embedded monitor is live because `pkg/monitor` is live, not because Sidecar wired anything.
- `td monitor` gets the same liveness from the same code path instead of depending on a 5-minute full-sync tick.
- Local syncable changes made *inside* a monitor — issues, reviews, approvals, closes, notes, boards, and positions — enqueue a prompt push after their logical transaction succeeds.
- The sync gate — authenticated, linked, not disabled, not killed by the global override — is resolved in exactly one place.

## Original defect

Before this implementation, the hosted server fanned out live events over SSE (`GET /v1/projects/{id}/events`) and td-watch consumed them (`td-watch/src/lib/stores/sseDriver.ts`), but nothing in the Go tree subscribed. Go surfaces learned about remote changes only when a caller remembered to poll:

- `pkg/monitor` refreshed local SQLite on `RefreshInterval` but left server sync policy in three raw embedder fields: `AutoSyncFunc`, `AutoSyncInterval`, and `LastAutoSync`.
- `cmd/monitor.go` supplied those fields and ran a second ticker outside Bubble Tea at `syncconfig.GetAutoSyncInterval()`, default 5 minutes.
- Sidecar supplied no sync callback, so an open embedded monitor never saw a browser-created task until another process pulled it.

The ownership defect was the missing in-process td sync facade, not the absence of Sidecar polling.

## Why this belongs in td, not in Sidecar

Sidecar PR #311 proposed closing the gap from the consumer side: parse `td --json sync status`, decide whether the gate was open, then shell out to `td sync --pull` on startup and every 60s with `TD_FEATURE_SYNC_CLI=1`. The code was careful — single-flight, context cancellation on project switch, epoch-guarded messages, and an injectable command for tests — but four problems followed from that ownership shape:

**1. It reimplemented td's gate.** The pre-implementation code already had divergent expressions in `cmd/autosync.go` and `cmd/monitor.go`; the PR would have added a third JSON-derived reading in another repo. The shared `pkg/tdsync.Gate` now replaces those copies.

**2. It pulled but never pushed, while the monitor is a writing surface.** `td sync --pull` is pull-only. Under the proposed PR, work done in Sidecar's monitor would have remained unpushed until another td invocation flushed it.

**3. `td sync --pull` could replace `issues.db` underneath a long-lived reader.** The explicit sync command may bootstrap by swapping the database when no server sequence has been pulled. A background subprocess using that command would have required Sidecar's inode-replacement recovery and recreated the unsafe long-lived-reader shape. `pkg/tdsync.Once` never bootstraps or replaces the file.

**4. Setting `TD_FEATURE_SYNC_CLI=1` from outside made a consumer override the owner's command gate.** An entitled embedder now calls td's in-process API directly instead.

This follows the ownership test from the design principles: *the capability belongs to whoever owns the data.* Sidecar owns none of td's sync state. `pkg/tdsync` is the missing in-process door, and Sidecar remains a projection.

## Current implementation inventory

The implementation is split across these owned seams:

| Piece | Location | State |
|---|---|---|
| SSE fan-out endpoint | `internal/api/events_handler.go`, route in `server.go:275` | Done. `RoleReader` auth, 15s pings, `Last-Event-ID` → immediate `refresh` event |
| Per-project SSE hub | `internal/api/sse_hub.go` | Done. 16 event slots + reserved refresh slot, degrades to `refresh` under backpressure |
| Broadcast on REST issue writes | `internal/api/broadcast.go`, `project_routes.go` | Done for the browser write path |
| Broadcast on sync push | `internal/api/broadcast.go`, `sync.go` | Done. One project refresh after a successful non-empty accepted/apply batch |
| Browser SSE consumer | `td-watch/src/lib/stores/sseDriver.ts` | Done. Reference for reconnect/visibility behaviour |
| Go SSE consumer | `internal/syncclient`, `pkg/tdsync/live.go` | Done. Streaming parser, body-liveness watchdog, resume hint, reconnect and fallbacks |
| Pull/apply engine | `internal/sync` (`ApplyRemoteEvents`, `GetPendingEvents`, `MarkEventsSynced`, `ResolvePullOutcome`) | Done and already reused by `internal/api` |
| Push/pull orchestration and gate | `pkg/tdsync` | Done. Importable shared-handle facade; commands are thin callers |
| Cheap change probe | `syncclient.SyncStatus` → `{event_count, last_server_seq, last_event_time}` | Done and used by the probing rung |
| Monitor ownership | `pkg/monitor/sync_runtime.go`, `EmbeddedOptions.Sync` | Done. Zero-configuration live default; deprecated raw callback remains an override |

The live channel is a notification path only; all data still moves through the existing event push/pull/apply engine.

## Design

Three layers, each independently useful. Layer 1 alone already deletes PR #311's reason to exist.

### Layer 1 — `pkg/tdsync`: one owned, callable sync facade

The orchestration lives in an importable package. `package cmd` is a caller like every other surface.

```go
package tdsync // pkg/tdsync — named tdsync so importers need no alias against std sync

// Syncer owns one project's sync lifecycle. Safe for concurrent use; overlapping
// calls collapse to a single in-flight round trip.
type Syncer struct{ /* ... */ }

type Options struct {
    BaseDir string
    DB      *db.DB   // optional: reuse an existing handle instead of opening a second one
    Logger  *slog.Logger
}

func New(opts Options) (*Syncer, error)

// Gate resolves whether this project may sync right now, and why not if it may
// not. This is the ONE gate. cmd/autosync.go, cmd/monitor.go, pkg/monitor and
// `td sync status --json` all report from this function.
func (s *Syncer) Gate() Gate

type Gate struct {
    Open          bool
    Authenticated bool
    Configured    bool   // linked, ProjectID != "", not SyncDisabled
    KillSwitch    bool   // global autosync override explicitly false
    Reason        string // human-readable, for status output and UI hints
}

// Once runs one push+pull round trip. It is single-flight, respects the gate,
// honours ctx for cancellation, and NEVER bootstraps or replaces the database.
func (s *Syncer) Once(ctx context.Context) (Result, error)

type Result struct {
    Pushed, Pulled int
    Pending        int64 // local events still unpushed
    Conflicts      int
}
```

Settled decisions for this layer:

- **`Once` never bootstraps.** Snapshot bootstrap stays an explicit operation on the `td sync` command path. A background sync must never swap the file a live embedder is reading. This removes the whole `databaseWasReplaced` concern from the steady-state path.
- **`Once` reuses the caller's `*db.DB` when given one.** The monitor supplies its shared pool handle, avoiding a second connection owned by steady-state sync.
- **The gate is resolved by `Gate()`.** Authentication, project linkage/disablement, precedence-aware feature settings, the legacy setting, and the global kill switch are evaluated once.
- **Nothing about the feature gate changes.** `TD_FEATURE_SYNC_CLI` continues to gate the *user-facing CLI commands*. A library caller with an open gate never touches it, because it is not running a command.

### Layer 2 — `pkg/monitor` syncs itself

The monitor owns sync policy, so every embedder is live by construction.

```go
type EmbeddedOptions struct {
    // ... existing fields ...

    // Sync controls background sync. Zero value means "sync when the project's
    // gate is open" — an embedder gets liveness by doing nothing. Set Disabled
    // to opt out.
    Sync SyncOptions
}

type SyncOptions struct {
    Disabled bool
    Interval time.Duration // 0 = syncconfig.GetAutoSyncInterval()
    Logger   *slog.Logger
    OnStatus func(SyncStatus) // optional: live/degraded/error, for host chrome
}
```

- The monitor constructs a `tdsync.Syncer` over its own `*db.DB` handle and starts one pointer-owned live supervisor after the first-frame boundary. The supervisor owns SSE, probes, timed fallback, prompt mutation wakes, and cancellation independently of Bubble Tea command dispatch.
- `AutoSyncFunc` / `AutoSyncInterval` / `LastAutoSync` stay, deprecated, as an explicit escape hatch. An embedder that sets `AutoSyncFunc` suppresses the built-in loop, so nothing existing breaks.
- `Model.Close()` stops the loop. Sidecar's project switch already calls `Stop()` → `Close()`, so cancellation on project switch comes free.
- Pull results that changed local rows trigger the monitor's existing refresh rather than waiting for the next `RefreshInterval` tick.
- **Monitor-originated writes push promptly.** Each successful logical issue, review, note, board, position, or link mutation enqueues a sync wake after its transaction and related side effects commit. The push runs on the sync goroutine, and a capacity-one wake retains one follow-up pass without queueing one request per edit. `Result.Pending` reports local events that remain unpushed.

The pre-implementation expectation was a Sidecar dependency bump and deletion of PR #311's files. The PR never landed, so the implemented source adoption required no Sidecar plugin diff; the published module pin moves with the td release.

### Layer 3 — the live channel

Timer-driven sync at any interval is a latency/cost tradeoff with no good setting: 5 minutes is stale, 60 seconds is 1,440 authenticated round trips per day per open surface, per project. Subscribe instead.

**3a. Server: broadcast on the sync-push path.** After `handleSyncPush` commits accepted events and applies them to the project database, it emits one project-scoped `refresh` event for the batch. The hub's backpressure design also degrades overloaded subscribers to a reserved refresh event.

**3b. Go SSE client in `pkg/tdsync`.**

```go
// Live owns the project's live sync ladder. It subscribes to the server event
// stream, performs coalesced Once calls, reconnects with jittered exponential
// backoff, and uses Last-Event-ID as a refresh hint. onChange runs after pulled
// data changes the local database so the caller can refresh its projection.
// It returns immediately if the gate is closed. Cancel via ctx.
func (s *Syncer) Live(ctx context.Context, onChange func()) error
```

- `Live` owns `Once(ctx)` and coalesces events over a fixed short window (~250ms), so a burst of 40 pushed events causes one pull while retaining one follow-up pass for a change arriving during the in-flight sync.
- The stream is a *notification*, never a data path. All state still arrives through the existing pull/apply engine, so conflict handling, entity validation, and the notes feature gate are unchanged and untested code paths are not introduced.
- Reconnect ladder: immediate retry, then jittered exponential backoff to a ceiling (~2 min). Every reconnect performs one `Once` first, so a missed-event window closes on reconnect rather than lingering. `Last-Event-ID` requests an immediate server refresh; it is not treated as a replay cursor or sequence proof.

**3c. Degradation ladder.** The surface reports which rung it is on via `OnStatus`, and the fallbacks are what makes this safe to ship:

| Rung | Condition | Behaviour | Latency |
|---|---|---|---|
| Live | SSE connected | pull on event | ~1s |
| Probing | SSE unavailable (proxy strips streams, old server) | poll `syncclient.SyncStatus`; pull only when `last_server_seq` advanced | interval, one cheap request |
| Timed | `SyncStatus` unavailable | full `Once` on `GetAutoSyncInterval()` — today's behaviour | interval |
| Off | gate closed | nothing; zero network | — |
| Expired | server returned 401 | stop; report `Reason: "credential expired"` once | — |

Rung 2 matters on its own: a status probe returning three integers is far cheaper than a pull, so even the non-SSE path stops doing real sync work against an idle project.

**3d. Credential expiry is terminal, not transient.** A Sidecar open for days outlives an API key. A 401 from the stream or from `Once` drops straight to the Expired rung: the SSE loop exits, the ticker stops, and `OnStatus` fires once so the surface can prompt a re-login. It is never retried with backoff — a dead credential does not recover on its own, and reconnecting against it burns the server's auth rate limit and buries the one signal the user needs to act on. `syncclient.ErrUnauthorized` already exists and is already treated as terminal by `pushBatchWithRetry`; this extends the same rule to the stream and the poll. Re-authentication re-opens the gate, and the next `Gate()` evaluation (on the surface's own retry, or on the next project switch or restart) resumes normally.

## Performance budget

The stated goal is near-real-time updates, and the standing constraint is that neither `td monitor` nor Sidecar may pay for it in startup latency or steady-state cost.

- **Startup: no sync work before the first frame.** `Syncer` construction is cheap (reads config, reuses the caller's DB handle). The first `Once` and the SSE dial happen on the background goroutine after the model is built. This preserves Sidecar's `td-9c7bf2` rule that `Init()`/`Start()` stay free of network and filesystem work — and is a rung better than PR #311, which fires a `td` subprocess (process spawn + second DB open + full HTTP round trip) during monitor adoption, on machines where an endpoint security agent taxes every spawn.
- **Steady state, idle project, live rung:** one idle HTTP connection, a 15s server ping, zero DB work. Compare with PR #311's 1,440 `td` process spawns per day per project.
- **Steady state, idle project, probing rung:** one small authenticated GET per interval, no transaction, no writes.
- **On remote change:** one pull, one transaction, one monitor refresh. Coalesced, so bursts cost one round trip.
- **On local change:** one push per monitor mutation, bounded by single-flight — a burst of edits made faster than the round trip collapses into the next in-flight push rather than queueing one request each. Worst case is one push per human keystroke-completed action, which is the correct cost for an interactive surface.
- **DB contention:** one long-lived connection per process, reusing the monitor's handle. No second in-process handle (unlike `autoSyncOnce`), no short-lived writer processes racing a long-lived reader (unlike PR #311) — which is the pattern the corruption RCA names. TRUNCATE journal means a writer takes an exclusive lock, so writes stay short and batched, and the pull applies per-batch inside one transaction as `autoSyncApplyPullBatch` already does.
- **Multiple surfaces on one machine:** N surfaces on the same project means N SSE connections and N pullers against one SQLite file. Contention is bounded by the 5s lock timeout, and single-flight is per-process. If this proves to be real pressure, the answer is a per-machine sync agent — deliberately out of scope here; noted as an open question, not designed for speculatively.
- **Jitter:** reconnect backoff is jittered so surfaces recovering from a server blip do not all redial in lockstep. Probe and timed-fallback cadence use the configured fixed interval.

## Ownership boundaries

| Concern | Owner | Consumers get |
|---|---|---|
| Is this project allowed to sync? | `pkg/tdsync.Gate` | a struct, not a JSON parse |
| Push/pull/conflict/backfill | `internal/sync` + `pkg/tdsync` | `Once(ctx)` |
| Snapshot bootstrap / DB replacement | `td sync` command only | never happens behind a live handle |
| Sync cadence and liveness | `pkg/monitor` | correct default, opt-out available |
| Server change notification | `internal/api` SSE hub | one endpoint, two clients (Go + browser) |
| Where the monitor is rendered, focus, theme, project switch | Sidecar | unchanged |

Sidecar's job is the projection, which is what it should own. td's job is knowing when its data changed, which is what it should own.

## Implemented phases and evidence

All five phases are implemented and independently reviewed under epic `td-ad905b`:

1. `pkg/tdsync` owns the shared gate and steady-state `Once` orchestration, reuses caller-owned database handles, and never bootstraps or replaces a live database. Crossed feature/legacy overrides are covered so the highest-precedence setting wins without bypassing authentication, linkage, project disablement, or the global kill switch.
2. `pkg/monitor` owns one pointer-backed background runtime across Bubble Tea model copies. It starts after the first-frame boundary, pushes successful monitor mutations promptly, refreshes through Bubble Tea after pulls, retains one follow-up wake, and cancels and waits before releasing the database. Legacy `AutoSyncFunc` remains an explicit override.
3. Sidecar inherits the zero-configuration monitor behavior with no plugin polling, copied gate, or sync subprocess. Its existing stop, project-switch, stale-build, and database-reopen paths close the model correctly.
4. A successful non-empty sync push broadcasts one project-scoped SSE refresh after the accepted events are applied; rejected, duplicate-only, failed-apply, and cross-project paths do not emit a false refresh.
5. `Live` owns SSE, coalescing, gap-closing reconnect passes, status probing, timed fallback, rung reporting, and terminal credential expiry. Body-liveness detection catches buffering proxies. A 401 is bound to the exact server/key/project generation used by the request, so a late failure cannot expire newly rotated credentials.

Verification on td commit `371c28a`:

- `make test` passed across all packages, including `cmd`, `internal/api`, `pkg/monitor`, `pkg/tdsync`, and `test/e2e`.
- Focused lifecycle, live-ladder, gate, auth-rotation, API broadcast, and monitor tests passed under the race detector where concurrency is material.
- Sidecar commit `c76590bc` built against td `371c28a`; `go test ./internal/plugins/tdmonitor ./internal/app` and `go build ./...` passed.
- In an isolated Sidecar/tmux/state harness, a browser-equivalent REST create appeared without a keypress in 675 ms, and a Sidecar monitor-form create reached the server without a CLI sync in 757 ms.

## Sidecar adoption

No Sidecar plugin code changed. PR #311 never landed, so there was no consumer-side polling code to delete. Local source workspaces inherit the td implementation automatically. Sidecar's published dependency pin should move from td v0.63.0 to the release containing these commits when that td release is published; committing an unpublished or local-only module version would make the Sidecar build unreproducible.

Optional presentation work remains separate: Sidecar may render the sync rung from `OnStatus` or expose a host-level opt-out. Neither is required for liveness or ownership parity.

## Deferred pressure

A per-machine sync agent remains deliberately deferred. Multiple open surfaces currently hold independent SSE connections and per-process single-flight syncers against the shared SQLite file. The existing lock timeout, short transactions, and fallback cadence are sufficient for the measured local use case; a local agent should be introduced only if real contention or connection counts justify it.

The existing configured autosync interval remains the probe/timed safety-net cadence. Live SSE carries the normal latency requirement, and the timed rung periodically retries live capability so a temporary or old-server downgrade is not permanent.

## Disposition of Sidecar PR #311

PR #311 is closed without merge. Its bug report and diagnosis were valid, but the replacement belongs to td and now serves standalone and embedded monitors in both directions without a subprocess, copied gate, second database handle, or background database replacement.
