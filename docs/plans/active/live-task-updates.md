# Plan: live task updates for every td surface

Status: proposed
Owner repo: `td` (this repo holds the large majority of the work)
Related: sidecar PR #311 `fix(td): pull hosted changes in embedded monitor` — this plan supersedes it
Affected surfaces: `td monitor`, Sidecar's embedded monitor (`internal/plugins/tdmonitor`), td-watch, `td` CLI

## Outcome

A task created or changed anywhere — the hosted browser UI, another machine's CLI, a teammate's device — appears in every open td surface within a second or two, without any surface polling the sync server on a fixed timer, and without any consumer of td re-implementing td's sync gate.

Concretely, when this is done:

- Sidecar's embedded monitor is live because `pkg/monitor` is live, not because Sidecar wired anything.
- `td monitor` gets the same liveness from the same code path, replacing its current 5-minute default tick.
- Local changes made *inside* a monitor (create, approve, close, log) push out as promptly as remote ones come in.
- The sync gate — authenticated, linked, not disabled, not killed by the global override — is resolved in exactly one place.

## Problem

The hosted server already fans out live events over SSE (`GET /v1/projects/{id}/events`), and td-watch consumes them (`td-watch/src/lib/stores/sseDriver.ts`). Nothing in the Go tree subscribes. Every td surface written in Go learns about remote changes only by polling, and only when something remembers to poll:

- `pkg/monitor` refreshes local SQLite on `RefreshInterval` (Sidecar's default, `pollInterval`) but never contacts the sync server itself. It exposes three raw fields — `AutoSyncFunc`, `AutoSyncInterval`, `LastAutoSync` — and leaves the entire policy to whoever embeds it.
- `cmd/monitor.go` fills those fields in, plus runs an independent ticker goroutine because "BubbleTea's `tea.Cmd` dispatch can stall under certain terminal/PTY conditions". The interval is `syncconfig.GetAutoSyncInterval()`, default 5 minutes.
- Sidecar embeds the monitor and fills in nothing, so a Sidecar left open all day never sees a task created in the browser.

That last gap is real and worth fixing. The question this plan answers is where the fix belongs.

## Why this belongs in td, not in Sidecar

Sidecar PR #311 closes the gap from the consumer side: it parses `td --json sync status`, decides for itself whether the gate is open, then shells out to `td sync --pull` on startup and every 60s, with `TD_FEATURE_SYNC_CLI=1` set to get past a feature gate that would otherwise refuse the command. The code is careful — single-flight, context cancellation on project switch, epoch-guarded messages, injectable command for tests. It is the best available implementation of the wrong shape, and four specific things follow from the shape rather than from the care taken:

**1. It reimplements td's gate, and the two copies already disagree.** td has three gate expressions today: `projectSyncConfigured` in `cmd/autosync.go` (configured project autosyncs even with `sync_autosync` unset — td-a4c721), `cmd/monitor.go` (additionally requires `features.SyncAutosync`, and never checks `ProjectID != ""`), and now the PR's JSON parse of `gate`/`configured`/`authenticated`. Three readings of one rule, in two repos. The global kill-switch (`syncconfig.GetGlobalAutosyncOverride`, td-735875) reaches only the copies that ask for it.

**2. It pulls but never pushes, and the monitor is a writing surface.** `td sync --pull` is pull-only by construction. The embedded monitor creates issues, approves, closes, and logs through its forms — `mutatingCommands` in `cmd/autosync.go` lists `"monitor": true` precisely because the standalone monitor's `AutoSyncFunc` is `autoSyncOnce`, which pushes *and* pulls. Under the PR, work done in Sidecar's monitor sits unpushed until some unrelated `td` CLI invocation happens to flush it. The gap the PR closes in one direction opens in the other.

**3. `td sync --pull` can replace `issues.db` underneath a long-lived reader.** `syncCmd`'s `RunE` runs `runBootstrap` — a snapshot download that swaps the database file — whenever `LastPulledServerSeq == 0`, on any invocation that is not `--push`. That is why the PR needs `databaseWasReplaced` and `reopenReplacedDatabase` at all. A background timer that can swap the file out from under Sidecar's long-lived `pkg/monitor` connection is the exact shape flagged in the August 2026 corruption RCA (five `issues.db` corruptions in two days; the mitigation was `journal_mode` WAL → TRUNCATE plus a 5s lock timeout in `internal/db/conn.go`). TRUNCATE journal makes the *writes* safe under contention; it does not make file replacement behind a live handle safe.

**4. Setting `TD_FEATURE_SYNC_CLI=1` from outside is a consumer overriding an owner's gate.** If a linked project's embedder is entitled to sync, td should say so through an API, not be talked past by an environment variable in a child process.

None of this is a criticism of the PR's execution. It is the standing test from the design principles: *the capability belongs to whoever owns the data.* Sidecar owns none of td's sync state, so a Sidecar-side implementation is duplicated policy by definition. The reason it was written there is that td offers no other door — `autoSyncOnce` is unexported in `package cmd`, so an in-process embedder genuinely cannot reach td's own sync. **That missing door is the actual defect, and it is in td.**

## What already exists

Inventory, so the plan below is understood as wiring rather than invention:

| Piece | Location | State |
|---|---|---|
| SSE fan-out endpoint | `internal/api/events_handler.go`, route in `server.go:275` | Done. `RoleReader` auth, 15s pings, `Last-Event-ID` → immediate `refresh` event |
| Per-project SSE hub | `internal/api/sse_hub.go` | Done. 16 event slots + reserved refresh slot, degrades to `refresh` under backpressure |
| Broadcast on REST issue writes | `internal/api/broadcast.go`, `project_routes.go` | Done for the browser write path |
| Broadcast on sync push | — | **Missing.** `handleSyncPush` commits and calls `applyAcceptedEventsToProjectDB` without notifying the hub |
| Browser SSE consumer | `td-watch/src/lib/stores/sseDriver.ts` | Done. Reference for reconnect/visibility behaviour |
| Go SSE consumer | — | **Missing** |
| Pull/apply engine | `internal/sync` (`ApplyRemoteEvents`, `GetPendingEvents`, `MarkEventsSynced`, `ResolvePullOutcome`) | Done and already reused by `internal/api` |
| Push/pull orchestration, retry, backfill | `cmd/autosync.go`, `cmd/sync.go` | Done but **trapped in `package cmd`**, unexported |
| Cheap change probe | `syncclient.SyncStatus` → `{event_count, last_server_seq, last_event_time}` | Done, unused by any poller |
| Embedder hooks | `pkg/monitor` `AutoSyncFunc` / `AutoSyncInterval` / `LastAutoSync` | Present but raw; policy lives in the caller |

Two facts shape the design: the live channel exists and only needs a Go client, and the sync orchestration exists and only needs to be lifted out of `package cmd`.

## Design

Three layers, each independently useful. Layer 1 alone already deletes PR #311's reason to exist.

### Layer 1 — `pkg/tdsync`: one owned, callable sync facade

Move the orchestration out of `package cmd` into an importable package. `package cmd` becomes a caller like everyone else.

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
- **`Once` reuses the caller's `*db.DB` when given one.** `autoSyncOnce` currently calls `db.Open(dir)` for a second handle to a file the monitor already holds. Reusing the monitor's shared pool handle means one long-lived connection per process, which is what the corruption RCA points at as the safe shape.
- **The three gate expressions collapse into `Gate()`.** `cmd/monitor.go`'s divergence (requiring `features.SyncAutosync`, not checking `ProjectID`) is a bug fixed by deletion, not preserved.
- **Nothing about the feature gate changes.** `TD_FEATURE_SYNC_CLI` continues to gate the *user-facing CLI commands*. A library caller with an open gate never touches it, because it is not running a command.

### Layer 2 — `pkg/monitor` syncs itself

The monitor stops asking embedders to supply sync policy and starts owning it, so every embedder is live by construction.

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

- The monitor constructs a `tdsync.Syncer` over its own `*db.DB` handle and runs the ticker on its own goroutine, moving the loop that currently lives in `cmd/monitor.go` — including the comment explaining why it must not depend on `tea.Cmd` dispatch — into the library where every embedder inherits it.
- `AutoSyncFunc` / `AutoSyncInterval` / `LastAutoSync` stay, deprecated, as an explicit escape hatch. An embedder that sets `AutoSyncFunc` suppresses the built-in loop, so nothing existing breaks.
- `Model.Close()` stops the loop. Sidecar's project switch already calls `Stop()` → `Close()`, so cancellation on project switch comes free.
- Pull results that changed local rows trigger the monitor's existing refresh rather than waiting for the next `RefreshInterval` tick.
- **Monitor-originated writes push immediately.** Every mutation made through the monitor — create, update, start, review, approve, reject, close, reopen, block, log, comment — enqueues a push as soon as its local transaction commits, rather than waiting for the tick or for `autoSyncAfterMutation`'s debounce. A monitor mutation is a deliberate human action at a keyboard, so the latency budget is the one the user is watching; the debounce exists to protect against scripted CLI bursts, which is not this case. The push runs on the sync goroutine (never blocking the Bubble Tea loop), and single-flight in `Once` already collapses a rapid series of edits into as few round trips as the network allows. `Result.Pending` reports anything that did not make it out, so the surface can show a "not yet pushed" state instead of silently lying.

After this layer, the whole of Sidecar's required change is: upgrade the td dependency, and delete PR #311.

### Layer 3 — the live channel

Timer-driven sync at any interval is a latency/cost tradeoff with no good setting: 5 minutes is stale, 60 seconds is 1,440 authenticated round trips per day per open surface, per project. Subscribe instead.

**3a. Server: broadcast on the sync-push path.** `handleSyncPush` commits to `events.db` and calls `applyAcceptedEventsToProjectDB`, then returns without touching the hub. Add a broadcast after that apply. Without it, CLI-origin writes are invisible to *every* live client — including td-watch in the browser, which has the same blind spot today. Emit one coalesced `refresh`-style event per push batch rather than one per issue; the hub's own backpressure design already treats `refresh` as the safe degradation.

**3b. Go SSE client in `pkg/tdsync`.**

```go
// Live subscribes to the project's server event stream and calls onChange
// (coalesced) whenever the server reports a change. It reconnects with jittered
// exponential backoff and resumes with Last-Event-ID. It returns immediately if
// the gate is closed. Cancel via ctx.
func (s *Syncer) Live(ctx context.Context, onChange func()) error
```

- `onChange` triggers `Once(ctx)`. Events are coalesced over a short debounce window (~250ms) so a burst of 40 pushed events causes one pull.
- The stream is a *notification*, never a data path. All state still arrives through the existing pull/apply engine, so conflict handling, entity validation, and the notes feature gate are unchanged and untested code paths are not introduced.
- Reconnect ladder: immediate retry, then jittered exponential backoff to a ceiling (~2 min). Every reconnect performs one `Once` first, so a missed-event window closes on reconnect rather than lingering.

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
- **Jitter:** all reconnects and polls are jittered so a fleet of surfaces restarting after a server blip does not arrive in lockstep.

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

## Work sequence

Each phase is shippable and independently valuable.

**Phase 1 — extract `pkg/tdsync`.** Move `autoSyncOnce`, `autoSyncPush`, `autoSyncPull`, `autoSyncApplyPullBatch`, `pushBatchWithRetry`, and the gate predicates out of `package cmd`. `cmd/autosync.go` and `cmd/monitor.go` become thin callers. Collapse the three gate expressions into `Gate()`; `cmd/monitor.go` loses its `features.SyncAutosync` requirement and gains the missing `ProjectID` check. Add the `DB` reuse option.
*Acceptance:* existing `cmd/autosync*_test.go` suite passes unchanged; a new gate table test covers authenticated × configured × kill-switch × explicit-flag; `td sync status --json` reports from `Gate()`.

**Phase 2 — monitor self-wires sync.** Add `EmbeddedOptions.Sync`, run the ticker inside `pkg/monitor`, move `cmd/monitor.go`'s independent goroutine into the library, deprecate the three raw fields, stop the loop in `Close()`.
*Acceptance:* an embedded monitor over a linked fixture project performs a push+pull without the embedder configuring anything; over an unlinked project it makes zero network calls; a mutation made through the monitor enqueues a push on commit rather than on the next tick, and a rapid series of edits collapses to fewer round trips than edits; `Close()` leaves no goroutine (verified under `-race` with a goroutine-leak check); setting `AutoSyncFunc` still suppresses the built-in loop.

**Phase 3 — Sidecar adopts.** Bump the td dependency, close PR #311. Optional: surface the `OnStatus` rung in the plugin's chrome.
*Acceptance:* Sidecar left open on a linked project shows a task created in the browser without any keystroke; a task created in Sidecar's monitor form reaches the server without a separate CLI invocation. Proof run under `scripts/tmux-drive.sh` with both tmux server and state tree isolated (`./scripts/tmux-drive.sh paths` verified first).

**Phase 4 — broadcast on push.** Add the coalesced hub broadcast to `handleSyncPush` after `applyAcceptedEventsToProjectDB`.
*Acceptance:* an integration test asserts an SSE subscriber receives one event for a multi-event push; td-watch in a browser reflects a `td create` from a CLI without a manual refresh. This phase fixes a live-ness gap for the browser independently of anything Go-side.

**Phase 5 — `Live()` and the degradation ladder.** SSE client, coalescing, backoff, `Last-Event-ID` resume, `SyncStatus` probing rung, rung reporting via `OnStatus`. `pkg/monitor` prefers `Live` and falls back automatically.
*Acceptance:* end-to-end latency from a browser edit to a visible row in both `td monitor` and Sidecar, measured, under 2s on a warm connection; killing the server mid-stream shows reconnect with backoff and no missed events after recovery (assert via a seq gap check); a proxy that buffers the stream degrades to the probing rung rather than hanging; a 401 stops the stream and the ticker, fires `OnStatus` exactly once, and issues no further requests until the gate is re-evaluated.

## Changes required in Sidecar

After Phase 2, the entire Sidecar diff is a `go.mod` bump plus deleting PR #311's three files. Optionally, later: render the sync rung in the plugin's chrome from `OnStatus`, and expose `plugins.tdMonitor.sync.disabled` in config for a user who wants a Sidecar that never talks to the network. Both are presentation concerns, which is the correct residue for Sidecar to keep.

## Open questions

1. **Per-machine sync agent.** If several surfaces on one machine each hold an SSE connection and each pull into the same SQLite file, is that acceptable, or does it want a single local agent that other surfaces watch via the filesystem? Deferred until measured — the SQLite lock timeout and single-flight bound the damage, and building the agent now would be anticipating pressure rather than responding to it.
2. **Interval default once live.** With SSE carrying the latency requirement, the timed rung becomes a safety net; `GetAutoSyncInterval`'s 5-minute default is probably right for it, but the probing rung may want its own shorter default.

## Disposition of sidecar PR #311

Close without merging, and reference this plan. The bug it reports is real and its diagnosis is correct — `NewEmbeddedWithOptions` refreshes only local SQLite while standalone `td monitor` separately wires `AutoSyncFunc`. Phase 1 and Phase 2 fix exactly that, in the repo that owns it, for every embedder, in both directions, without a subprocess, without a second gate implementation, and without a background timer that can replace `issues.db` under a live reader.

If the gap needs closing before Phase 2 lands, the interim measure is Phase 1 plus four lines in Sidecar wiring `AutoSyncFunc` to `tdsync.Syncer.Once` — one owned function called in-process, not a shell-out with a copied gate.
