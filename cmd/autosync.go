package cmd

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/output"
	syncengine "github.com/marcus/td/internal/sync"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/marcus/td/pkg/tdsync"
)

const (
	autoSyncHTTPTimeout = 5 * time.Second
	// autoSyncPushBudget bounds the total wall-clock spent retrying a push so a
	// sustained outage does not add unbounded tail latency to every mutating
	// command. Transient blips recover within this window; anything longer is
	// left for the next command's startup sync (or the monitor tick) to retry.
	autoSyncPushBudget = 6 * time.Second
	// autoSyncPushBackoff is the initial retry delay; it doubles each attempt.
	autoSyncPushBackoff = 250 * time.Millisecond
	// strandedWarnInterval throttles the "configured but autosync off" warning so
	// it is noticeable without spamming on every mutating command. The check is a
	// single COUNT query, but the message would be noise if printed every time.
	strandedWarnInterval = 3 * time.Minute
)

var (
	lastAutoSyncAt   time.Time
	autoSyncMu       sync.Mutex
	autoSyncInFlight int32 // atomic flag: 1 = sync running

	lastStrandedWarnAt time.Time
	strandedWarnMu     sync.Mutex
)

// mutatingCommands lists commands that modify local data and should trigger auto-sync.
var mutatingCommands = map[string]bool{
	"create":     true,
	"update":     true,
	"delete":     true,
	"restore":    true,
	"start":      true,
	"unstart":    true,
	"close":      true,
	"block":      true,
	"unblock":    true,
	"reopen":     true,
	"review":     true,
	"approve":    true,
	"reject":     true,
	"log":        true,
	"handoff":    true,
	"focus":      true,
	"unfocus":    true,
	"link":       true,
	"unlink":     true,
	"comment":    true,
	"comments":   true,
	"undo":       true,
	"import":     true,
	"init":       true,
	"board":      true,
	"dep":        true,
	"blocked-by": true,
	"depends-on": true,
	"epic":       true,
	"task":       true,
	"ws":         true,
	"monitor":    true,
}

// isMutatingCommand checks if the given command name triggers auto-sync.
func isMutatingCommand(name string) bool {
	return mutatingCommands[name]
}

// AutoSyncEnabled returns true if auto-sync is enabled via config.
func AutoSyncEnabled() bool {
	return syncconfig.GetAutoSyncEnabled()
}

// projectSyncConfigured reports whether the project at baseDir is actually
// configured for sync — i.e. it has a sync_state row with a non-empty ProjectID
// and sync is not disabled, AND credentials are present. This is the default
// gate for autosync (td-a4c721): a configured project autosyncs even when the
// sync_autosync feature flag is unset.
//
// It must be cheap and must never error-out the calling command: any failure
// (no base dir, DB open error, query error) is treated as "not configured" and
// logged at debug level only.
func projectSyncConfigured(baseDir string) bool {
	g := syncGate(baseDir, nil)
	return g.Configured && g.Authenticated
}

// globalKillSwitchOff reports whether a global autosync kill-switch is engaged.
// When true, the autosync gate short-circuits regardless of per-project config
// or the sync_autosync override.
//
// It returns true ONLY when the global autosync override (td-735875) resolves
// to an explicit false — i.e. the user (or a TD_* env var) has explicitly
// killed autosync everywhere. An absent override (nil) or an explicit true does
// NOT engage the kill: an explicit true merely clears the kill and lets the
// per-project gate decide; it never force-enables an unconfigured project.
func globalKillSwitchOff() bool {
	return syncGate("", nil).KillSwitch
}

func syncGate(baseDir string, database *db.DB) tdsync.Gate {
	syncer, _ := tdsync.New(tdsync.Options{BaseDir: baseDir, DB: database})
	return syncer.Gate()
}

// strandedSyncShouldWarn is the pure decision for the "project is configured for
// sync but autosync is gated OFF and changes are piling up" warning. It returns
// (true, pending) only when the project is sync-configured, the autosync gate is
// CLOSED by an explicit OFF (kill-switch engaged or sync_autosync explicitly
// false), and there is at least one pending event that will not be pushed.
//
// It deliberately stays silent for the gate-OPEN case: that path is autosync's
// job and surfaces its own "not yet pushed" warning after a failed push, so
// warning here too would double-warn. Unconfigured projects (no sync_state) are
// the normal majority case and must never warn.
//
// Performance: this runs in PersistentPostRun on EVERY command (including
// read-only ones), so it must avoid DB work on the hot path. It first consults
// only DB-free signals — globalKillSwitchOff() and ResolveExplicit (which read
// env + config.json, no DB) — to decide whether the gate is closed by an
// explicit OFF. When the flag is unset, the gate is either open-and-syncing or
// the project is unconfigured; neither warns, so we return immediately WITHOUT
// opening the DB. Only the genuinely-stranded (explicit-off) path opens the DB,
// and it does so exactly once: a single handle backs both the sync-config check
// and the pending-count query.
//
// It must never error-out the calling command: any failure is treated as "do
// not warn" and logged at debug level.
func strandedSyncShouldWarn(baseDir string) (bool, int64) {
	// Step 1 (no DB): the warning can only fire when the gate is closed by an
	// explicit OFF. If neither the kill-switch nor an explicit-false flag is in
	// play, bail before touching the DB. This is the common/hot path.
	explicitOff := globalKillSwitchOff()
	if !explicitOff {
		if v, explicit := features.ResolveExplicit(baseDir, features.SyncAutosync.Name); explicit && !v {
			explicitOff = true
		}
	}
	if !explicitOff {
		return false, 0
	}

	// Step 2 (one DB open): the gate is closed by an explicit OFF. Now confirm the
	// project is actually configured for sync and count pending events, reusing a
	// single DB handle for both queries.
	if baseDir == "" {
		slog.Debug("stranded-warn: no base dir")
		return false, 0
	}
	if !syncconfig.IsAuthenticated() {
		slog.Debug("stranded-warn: not authenticated")
		return false, 0
	}

	database, err := db.Open(baseDir)
	if err != nil {
		slog.Debug("stranded-warn: open db", "err", err)
		return false, 0
	}
	defer database.Close()

	state, err := database.GetSyncState()
	if err != nil {
		slog.Debug("stranded-warn: get sync state", "err", err)
		return false, 0
	}
	if state == nil || state.ProjectID == "" || state.SyncDisabled {
		// Unconfigured project — the normal majority case, never warn.
		return false, 0
	}

	pending, err := database.CountPendingEvents()
	if err != nil {
		slog.Debug("stranded-warn: count pending", "err", err)
		return false, 0
	}
	if pending <= 0 {
		return false, 0
	}
	return true, pending
}

// warnIfSyncStranded runs the stranded-sync check and emits a throttled warning
// when a sync-configured project has autosync gated off with pending changes.
// The warning is debounced (strandedWarnInterval) so it is not printed on every
// command. Never errors out the caller.
func warnIfSyncStranded(baseDir string) {
	shouldWarn, pending := strandedSyncShouldWarn(baseDir)
	if !shouldWarn {
		return
	}

	strandedWarnMu.Lock()
	if time.Since(lastStrandedWarnAt) < strandedWarnInterval {
		strandedWarnMu.Unlock()
		return
	}
	lastStrandedWarnAt = time.Now()
	strandedWarnMu.Unlock()

	output.Warning("sync: this project is configured for sync but autosync is off — %d change(s) not being pushed. Run 'td sync status'.", pending)
}

// autoSyncOnce runs a push and optional pull silently. It returns the number of
// local events that remain unsynced after the attempt (0 on any early return),
// so callers can surface a "still pending" warning without re-opening the DB.
func autoSyncOnce() int64 {
	if !atomic.CompareAndSwapInt32(&autoSyncInFlight, 0, 1) {
		slog.Debug("autosync: skipped, in flight")
		return 0
	}
	defer atomic.StoreInt32(&autoSyncInFlight, 0)

	dir := getBaseDir()
	syncer, _ := tdsync.New(tdsync.Options{BaseDir: dir})
	result, err := syncer.Once(context.Background())
	if err != nil {
		slog.Debug("autosync", "err", err)
	}
	return result.Pending
}

func countPendingForAutoSync(database *db.DB) int64 {
	pending, err := database.CountPendingEvents()
	if err != nil {
		slog.Debug("autosync: count pending", "err", err)
		return 0
	}
	return pending
}

// autoSyncAfterMutation runs a debounced push+pull after a mutating command.
func autoSyncAfterMutation() {
	debounce := syncconfig.GetAutoSyncDebounce()
	autoSyncMu.Lock()
	if time.Since(lastAutoSyncAt) < debounce {
		autoSyncMu.Unlock()
		return
	}
	lastAutoSyncAt = time.Now()
	autoSyncMu.Unlock()

	// Surface the case where changes were written locally but did not reach the
	// remote (transient error/timeout after retries). Without this the failure
	// is silent (slog.Debug only) and the change sits unsynced until the next
	// command or monitor tick — the main source of "it didn't show up" surprise.
	// autoSyncOnce returns 0 when sync is disabled/unconfigured, so this never
	// warns spuriously.
	if pending := autoSyncOnce(); pending > 0 {
		output.Warning("sync: %d local change(s) not yet pushed to remote (will retry on next td command)", pending)
	}
}

// pushBatchWithRetry pushes one batch, retrying transient failures with
// exponential backoff until the shared deadline. Unauthorized errors are
// terminal and returned immediately. Each attempt's HTTP timeout is clamped to
// the time remaining so a single attempt cannot run far past the budget.
//
// Note that when the server is slow (not fast-failing) the first attempt can
// consume most of the budget, leaving room for at most one short retry — the
// backoff/budget interaction only produces multiple retries against a
// fast-failing server (connection refused, immediate 5xx).
//
// The caller's client.HTTP.Timeout is restored on return so a subsequent pull
// on the same client is not left with a clamped timeout.
func pushBatchWithRetry(client *syncclient.Client, projectID string, req *syncclient.PushRequest, deadline time.Time) (*syncclient.PushResponse, error) {
	return tdsync.PushBatchWithRetry(client, projectID, req, deadline)
}

// startupSyncSkipCommands lists commands that should not trigger startup auto-sync.
var startupSyncSkipCommands = map[string]bool{
	"sync": true, "auth": true, "login": true, "version": true, "help": true, "handoff": true,
}

// autoSyncOnStartup runs a one-time push+pull at process start if configured.
// Does NOT set debounce timestamp — the post-mutation sync in PersistentPostRun
// must still fire for commands that create data after the startup sync.
func autoSyncOnStartup(cmdName string) {
	if !syncconfig.GetAutoSyncOnStart() {
		return
	}
	if startupSyncSkipCommands[cmdName] {
		return
	}

	autoSyncOnce()
}

// autoSyncPull pulls remote events and applies them silently.
func autoSyncPull(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID string) error {
	return tdsync.Pull(database, client, state, deviceID, database.BaseDir())
}

// autoSyncApplyPullBatch applies a batch of pulled events inside a single transaction.
// Extracted from the autoSyncPull loop so that defer tx.Rollback() fires per-batch,
// not accumulated across all loop iterations.
func autoSyncApplyPullBatch(database *db.DB, events []syncengine.Event, deviceID string, lastServerSeq int64, lastSyncAt *time.Time) error {
	return tdsync.ApplyPullBatch(database, events, deviceID, lastServerSeq, lastSyncAt, database.BaseDir())
}

// autoSyncPush pushes pending events silently. Returns nil if nothing to push.
// Batches events to stay within server limits (pushBatchSize from sync.go).
func autoSyncPush(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID string) error {
	return tdsync.Push(database, client, state, deviceID, database.BaseDir())
}

func init() {
	RegisterSyncFeatureHooks(SyncFeatureHooks{
		OnStartup:         autoSyncOnStartup,
		OnAfterMutation:   autoSyncAfterMutation,
		IsMutatingCommand: isMutatingCommand,
	})
}
