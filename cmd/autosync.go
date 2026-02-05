package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/session"
	tdsync "github.com/marcus/td/internal/sync"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
)

const autoSyncHTTPTimeout = 5 * time.Second

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

// autoSyncOnce runs a push and optional pull silently.
func autoSyncOnce() {
	if !atomic.CompareAndSwapInt32(&autoSyncInFlight, 0, 1) {
		slog.Debug("autosync: skipped, in flight")
		return
	}
	defer atomic.StoreInt32(&autoSyncInFlight, 0)

	if !AutoSyncEnabled() {
		slog.Debug("autosync: disabled")
		return
	}
	if !syncconfig.IsAuthenticated() {
		slog.Debug("autosync: not authenticated")
		return
	}
	dir := getBaseDir()
	if dir == "" {
		slog.Debug("autosync: no base dir")
		return
	}
	database, err := db.Open(dir)
	if err != nil {
		slog.Debug("autosync: open db", "err", err)
		return
	}
	defer database.Close()

	syncState, err := database.GetSyncState()
	if err != nil {
		slog.Debug("autosync: get sync state", "err", err)
		return
	}
	if syncState == nil {
		slog.Debug("autosync: no sync state")
		return
	}
	if syncState.SyncDisabled {
		slog.Debug("autosync: sync disabled")
		return
	}

	slog.Debug("autosync: starting push+pull")

	deviceID, err := syncconfig.GetDeviceID()
	if err != nil {
		slog.Debug("autosync: device ID unavailable", "err", err)
		return
	}

	serverURL := syncconfig.GetServerURL()
	apiKey := syncconfig.GetAPIKey()
	client := syncclient.New(serverURL, apiKey, deviceID)
	client.HTTP.Timeout = autoSyncHTTPTimeout

	if err := autoSyncPush(database, client, syncState, deviceID); err != nil {
		slog.Debug("autosync: push", "err", err)
	}

	if syncconfig.GetAutoSyncPull() {
		if err := autoSyncPull(database, client, syncState, deviceID); err != nil {
			slog.Debug("autosync: pull", "err", err)
		}
	}
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

	autoSyncOnce()
}

// startupSyncSkipCommands lists commands that should not trigger startup auto-sync.
var startupSyncSkipCommands = map[string]bool{
	"sync": true, "auth": true, "login": true, "version": true, "help": true,
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
			clientTS, _ := time.Parse(time.RFC3339, pe.ClientTimestamp)
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

		conn := database.Conn()
		tx, err := conn.Begin()
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}

		result, err := tdsync.ApplyRemoteEvents(tx, events, deviceID, syncEntityValidator, state.LastSyncAt)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply events: %w", err)
		}

		if err := storeConflicts(tx, result.Conflicts); err != nil {
			tx.Rollback()
			return fmt.Errorf("store conflicts: %w", err)
		}

		if _, err := tx.Exec(`UPDATE sync_state SET last_pulled_server_seq = ?, last_sync_at = CURRENT_TIMESTAMP`, pullResp.LastServerSeq); err != nil {
			tx.Rollback()
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

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		lastSeq = pullResp.LastServerSeq
		slog.Debug("autosync: pulled", "events", len(pullResp.Events))

		if !pullResp.HasMore {
			break
		}
	}
	return nil
}

// autoSyncPush pushes pending events silently. Returns nil if nothing to push.
// Batches events to stay within server limits (pushBatchSize from sync.go).
func autoSyncPush(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID string) error {
	sess, err := session.Get(database)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	conn := database.Conn()
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	events, err := tdsync.GetPendingEvents(tx, deviceID, sess.ID)
	if err != nil {
		return fmt.Errorf("get pending: %w", err)
	}
	events = filterEventsForSync(events, syncEntityValidator)
	if len(events) == 0 {
		return nil
	}

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

		pushResp, err := client.Push(state.ProjectID, pushReq)
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
