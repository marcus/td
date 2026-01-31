package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/session"
	tdsync "github.com/marcus/td/internal/sync"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
)

// mutatingCommands lists commands that modify local data and should trigger auto-sync.
var mutatingCommands = map[string]bool{
	"create":      true,
	"update":      true,
	"delete":      true,
	"restore":     true,
	"start":       true,
	"unstart":     true,
	"close":       true,
	"block":       true,
	"unblock":     true,
	"reopen":      true,
	"review":      true,
	"approve":     true,
	"reject":      true,
	"log":         true,
	"handoff":     true,
	"focus":       true,
	"unfocus":     true,
	"link":        true,
	"unlink":      true,
	"comment":     true,
	"undo":        true,
	"import":      true,
	"init":        true,
	"board":       true,
	"dep":         true,
	"blocked-by":  true,
	"depends-on":  true,
	"epic":        true,
	"task":        true,
	"ws":          true,
}

// isMutatingCommand checks if the given command name triggers auto-sync.
func isMutatingCommand(name string) bool {
	return mutatingCommands[name]
}

// AutoSyncEnabled returns true if auto-sync is enabled.
// Checks TD_AUTO_SYNC env var, then config. Defaults to true when authenticated.
func AutoSyncEnabled() bool {
	if v := os.Getenv("TD_AUTO_SYNC"); v != "" {
		return v == "1" || v == "true"
	}
	return true // enabled by default
}

// autoSyncAfterMutation runs a quick push after a mutating command completes.
// Runs synchronously but with a short timeout. Errors are logged, not returned.
func autoSyncAfterMutation() {
	if !AutoSyncEnabled() {
		return
	}
	if !syncconfig.IsAuthenticated() {
		return
	}

	dir := getBaseDir()
	if dir == "" {
		return
	}

	database, err := db.Open(dir)
	if err != nil {
		slog.Debug("autosync: open db", "err", err)
		return
	}
	defer database.Close()

	syncState, err := database.GetSyncState()
	if err != nil || syncState == nil {
		return // not linked
	}
	if syncState.SyncDisabled {
		return
	}

	deviceID, err := syncconfig.GetDeviceID()
	if err != nil {
		return
	}

	serverURL := syncconfig.GetServerURL()
	apiKey := syncconfig.GetAPIKey()
	client := syncclient.New(serverURL, apiKey, deviceID)
	client.HTTP.Timeout = 5 * time.Second // short timeout for auto-sync

	// Push only — pull happens on next explicit sync or monitor tick
	if err := autoSyncPush(database, client, syncState, deviceID); err != nil {
		slog.Debug("autosync: push", "err", err)
	}
}

// autoSyncPush pushes pending events silently. Returns nil if nothing to push.
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
	if len(events) == 0 {
		return nil
	}

	pushReq := &syncclient.PushRequest{
		DeviceID:  deviceID,
		SessionID: sess.ID,
	}
	for _, ev := range events {
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
		return fmt.Errorf("push: %w", err)
	}

	acks := make([]tdsync.Ack, 0, len(pushResp.Acks)+len(pushResp.Rejected))
	var maxActionID int64
	for _, a := range pushResp.Acks {
		acks = append(acks, tdsync.Ack{ClientActionID: a.ClientActionID, ServerSeq: a.ServerSeq})
		if a.ClientActionID > maxActionID {
			maxActionID = a.ClientActionID
		}
	}
	for _, r := range pushResp.Rejected {
		if r.Reason == "duplicate" && r.ServerSeq > 0 {
			acks = append(acks, tdsync.Ack{ClientActionID: r.ClientActionID, ServerSeq: r.ServerSeq})
			if r.ClientActionID > maxActionID {
				maxActionID = r.ClientActionID
			}
		}
	}

	if err := tdsync.MarkEventsSynced(tx, acks); err != nil {
		return fmt.Errorf("mark synced: %w", err)
	}

	if maxActionID > 0 {
		if _, err := tx.Exec(`UPDATE sync_state SET last_pushed_action_id = ?, last_sync_at = CURRENT_TIMESTAMP`, maxActionID); err != nil {
			return fmt.Errorf("update state: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	slog.Debug("autosync: pushed", "events", len(acks))
	return nil
}
