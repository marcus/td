package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	tdsync "github.com/marcus/td/internal/sync"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
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
)

var (
	lastAutoSyncAt   time.Time
	autoSyncMu       sync.Mutex
	autoSyncInFlight int32 // atomic flag: 1 = sync running
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

// autoSyncOnce runs a push and optional pull silently. It returns the number of
// local events that remain unsynced after the attempt (0 on any early return),
// so callers can surface a "still pending" warning without re-opening the DB.
func autoSyncOnce() int64 {
	if !atomic.CompareAndSwapInt32(&autoSyncInFlight, 0, 1) {
		slog.Debug("autosync: skipped, in flight")
		return 0
	}
	defer atomic.StoreInt32(&autoSyncInFlight, 0)

	if !AutoSyncEnabled() {
		slog.Debug("autosync: disabled")
		return 0
	}
	if !syncconfig.IsAuthenticated() {
		slog.Debug("autosync: not authenticated")
		return 0
	}
	dir := getBaseDir()
	if dir == "" {
		slog.Debug("autosync: no base dir")
		return 0
	}
	database, err := db.Open(dir)
	if err != nil {
		slog.Debug("autosync: open db", "err", err)
		return 0
	}
	defer database.Close()

	syncState, err := database.GetSyncState()
	if err != nil {
		slog.Debug("autosync: get sync state", "err", err)
		return 0
	}
	if syncState == nil {
		slog.Debug("autosync: no sync state")
		return 0
	}
	if syncState.SyncDisabled {
		slog.Debug("autosync: sync disabled")
		return 0
	}

	slog.Debug("autosync: starting push+pull")

	deviceID, err := syncconfig.GetDeviceID()
	if err != nil {
		slog.Debug("autosync: device ID unavailable", "err", err)
		return 0
	}

	serverURL := syncconfig.GetServerURL()
	apiKey := syncconfig.GetAPIKey()
	client := syncclient.New(serverURL, apiKey, deviceID)
	client.HTTP.Timeout = autoSyncHTTPTimeout

	if err := autoSyncPush(database, client, syncState, deviceID); err != nil {
		slog.Debug("autosync: push", "err", err)
		return countPendingForAutoSync(database)
	}

	if syncconfig.GetAutoSyncPull() {
		// Reload syncState after push — push updates last_sync_at in the DB
		// but the in-memory struct is stale. ApplyRemoteEvents uses LastSyncAt
		// for conflict detection, so a stale value causes false conflicts.
		syncState, err = database.GetSyncState()
		if err != nil || syncState == nil {
			slog.Debug("autosync: reload sync state", "err", err)
			return 0
		}
		if err := autoSyncPull(database, client, syncState, deviceID); err != nil {
			slog.Debug("autosync: pull", "err", err)
		}
	}

	// Report what still hasn't reached the remote. Non-zero means the push
	// failed (transiently) and the change is stranded until the next attempt.
	return countPendingForAutoSync(database)
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
		output.WarningErr("sync: %d local change(s) not yet pushed to remote (will retry on next td command)", pending)
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
	origTimeout := client.HTTP.Timeout
	defer func() { client.HTTP.Timeout = origTimeout }()

	backoff := autoSyncPushBackoff
	var lastErr error
	for attempt := 0; ; attempt++ {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			// Out of budget. Guarantee at least one attempt; otherwise give up.
			if attempt > 0 {
				return nil, lastErr
			}
			remaining = autoSyncHTTPTimeout
		}
		if remaining < autoSyncHTTPTimeout {
			client.HTTP.Timeout = remaining
		} else {
			client.HTTP.Timeout = autoSyncHTTPTimeout
		}

		resp, err := client.Push(projectID, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if errors.Is(err, syncclient.ErrUnauthorized) {
			return nil, err
		}

		// Stop if there is no room left for a backoff plus a minimal attempt.
		if time.Until(deadline) < backoff+autoSyncPushBackoff {
			return nil, lastErr
		}
		time.Sleep(backoff)
		backoff *= 2
	}
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
	lastSeq := state.LastPulledServerSeq

	for {
		pullResp, err := client.Pull(state.ProjectID, lastSeq, 1000, deviceID)
		if err != nil {
			return fmt.Errorf("pull: %w", err)
		}
		if len(pullResp.Events) == 0 {
			break
		}

		events := make([]tdsync.Event, len(pullResp.Events))
		for i, pe := range pullResp.Events {
			clientTS, err := time.Parse(time.RFC3339Nano, pe.ClientTimestamp)
			if err != nil {
				clientTS, _ = time.Parse(time.RFC3339, pe.ClientTimestamp)
			}
			events[i] = tdsync.Event{
				ServerSeq:       pe.ServerSeq,
				DeviceID:        pe.DeviceID,
				SessionID:       pe.SessionID,
				ClientActionID:  pe.ClientActionID,
				ActionType:      pe.ActionType,
				EntityType:      pe.EntityType,
				EntityID:        pe.EntityID,
				Payload:         pe.Payload,
				ClientTimestamp: clientTS,
			}
		}

		if err := autoSyncApplyPullBatch(database, events, deviceID, pullResp.LastServerSeq, state.LastSyncAt); err != nil {
			return err
		}

		lastSeq = pullResp.LastServerSeq
		slog.Debug("autosync: pulled", "events", len(pullResp.Events))

		if !pullResp.HasMore {
			break
		}
	}
	return nil
}

// autoSyncApplyPullBatch applies a batch of pulled events inside a single transaction.
// Extracted from the autoSyncPull loop so that defer tx.Rollback() fires per-batch,
// not accumulated across all loop iterations.
func autoSyncApplyPullBatch(database *db.DB, events []tdsync.Event, deviceID string, lastServerSeq int64, lastSyncAt *time.Time) error {
	conn := database.Conn()
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tdsync.ApplyRemoteEvents(tx, events, deviceID, syncEntityValidator, lastSyncAt)
	if err != nil {
		return fmt.Errorf("apply events: %w", err)
	}

	if err := storeConflicts(tx, result.Conflicts); err != nil {
		return fmt.Errorf("store conflicts: %w", err)
	}

	if _, err := tx.Exec(`UPDATE sync_state SET last_pulled_server_seq = ?, last_sync_at = CURRENT_TIMESTAMP`, lastServerSeq); err != nil {
		return fmt.Errorf("update sync state: %w", err)
	}

	// Record sync history
	var historyEntries []db.SyncHistoryEntry
	for _, ev := range events {
		historyEntries = append(historyEntries, db.SyncHistoryEntry{
			Direction:  "pull",
			ActionType: ev.ActionType,
			EntityType: ev.EntityType,
			EntityID:   ev.EntityID,
			ServerSeq:  ev.ServerSeq,
			DeviceID:   ev.DeviceID,
			Timestamp:  time.Now(),
		})
	}
	if err := db.RecordSyncHistoryTx(tx, historyEntries); err != nil {
		slog.Debug("autosync: record pull history", "err", err)
	}

	return tx.Commit()
}

// autoSyncPush pushes pending events silently. Returns nil if nothing to push.
// Batches events to stay within server limits (pushBatchSize from sync.go).
func autoSyncPush(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID string) error {
	sess, err := session.GetOrCreate(database)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	conn := database.Conn()
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	events, err := tdsync.GetPendingEvents(tx, deviceID, sess.ID)
	if err != nil {
		return fmt.Errorf("get pending: %w", err)
	}
	events = filterEventsForSync(events, syncEntityValidator)
	if len(events) == 0 {
		return nil
	}

	// Bound the total time spent retrying across all batches.
	deadline := time.Now().Add(autoSyncPushBudget)

	var allAcks []tdsync.Ack
	var maxActionID int64
	var allHistoryEntries []db.SyncHistoryEntry

	// Push in batches to stay within server limits
	for i := 0; i < len(events); i += pushBatchSize {
		end := i + pushBatchSize
		if end > len(events) {
			end = len(events)
		}
		batch := events[i:end]

		pushReq := &syncclient.PushRequest{
			DeviceID:  deviceID,
			SessionID: sess.ID,
		}
		for _, ev := range batch {
			pushReq.Events = append(pushReq.Events, syncclient.EventInput{
				ClientActionID:  ev.ClientActionID,
				ActionType:      ev.ActionType,
				EntityType:      ev.EntityType,
				EntityID:        ev.EntityID,
				Payload:         ev.Payload,
				ClientTimestamp: ev.ClientTimestamp.Format(time.RFC3339),
			})
		}

		pushResp, err := pushBatchWithRetry(client, state.ProjectID, pushReq, deadline)
		if err != nil {
			if errors.Is(err, syncclient.ErrUnauthorized) {
				return fmt.Errorf("unauthorized")
			}
			return fmt.Errorf("push batch %d/%d: %w", i/pushBatchSize+1, (len(events)+pushBatchSize-1)/pushBatchSize, err)
		}

		for _, a := range pushResp.Acks {
			allAcks = append(allAcks, tdsync.Ack{ClientActionID: a.ClientActionID, ServerSeq: a.ServerSeq})
			if a.ClientActionID > maxActionID {
				maxActionID = a.ClientActionID
			}
		}
		for _, r := range pushResp.Rejected {
			if r.Reason == "duplicate" && r.ServerSeq > 0 {
				allAcks = append(allAcks, tdsync.Ack{ClientActionID: r.ClientActionID, ServerSeq: r.ServerSeq})
				if r.ClientActionID > maxActionID {
					maxActionID = r.ClientActionID
				}
			}
		}

		// Build history entries for this batch
		ackMap := make(map[int64]int64)
		for _, a := range pushResp.Acks {
			ackMap[a.ClientActionID] = a.ServerSeq
		}
		for _, ev := range batch {
			if seq, ok := ackMap[ev.ClientActionID]; ok {
				allHistoryEntries = append(allHistoryEntries, db.SyncHistoryEntry{
					Direction:  "push",
					ActionType: ev.ActionType,
					EntityType: ev.EntityType,
					EntityID:   ev.EntityID,
					ServerSeq:  seq,
					DeviceID:   deviceID,
					Timestamp:  time.Now(),
				})
			}
		}
	}

	if err := tdsync.MarkEventsSynced(tx, allAcks); err != nil {
		return fmt.Errorf("mark synced: %w", err)
	}

	if maxActionID > 0 {
		if _, err := tx.Exec(`UPDATE sync_state SET last_pushed_action_id = ?, last_sync_at = CURRENT_TIMESTAMP`, maxActionID); err != nil {
			return fmt.Errorf("update state: %w", err)
		}
	}

	if err := db.RecordSyncHistoryTx(tx, allHistoryEntries); err != nil {
		slog.Debug("autosync: record push history", "err", err)
	}
	if err := db.PruneSyncHistory(tx, 500); err != nil {
		slog.Debug("autosync: prune history", "err", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	slog.Debug("autosync: pushed", "events", len(allAcks))
	return nil
}

func init() {
	RegisterSyncFeatureHooks(SyncFeatureHooks{
		OnStartup:         autoSyncOnStartup,
		OnAfterMutation:   autoSyncAfterMutation,
		IsMutatingCommand: isMutatingCommand,
	})
}
