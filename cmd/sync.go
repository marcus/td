package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	tdsync "github.com/marcus/td/internal/sync"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/spf13/cobra"
)

// baseSyncableEntities are always eligible for sync.
//
// issue_reviews is registered at the same time the table is introduced so
// that recorded approvals (and supersede events, delivered as UPDATE events
// that carry superseded_at) cross machines from the first release. The sync
// engine applies UPDATEs for any registered table via the partial-update
// path (see internal/sync/events.go), so the superseded_at stamp propagates
// without a dedicated sync mechanism.
var baseSyncableEntities = map[string]bool{
	"issues":                true,
	"logs":                  true,
	"comments":              true,
	"handoffs":              true,
	"boards":                true,
	"work_sessions":         true,
	"board_issue_positions": true,
	"issue_dependencies":    true,
	"issue_files":           true,
	"work_session_issues":   true,
	"issue_reviews":         true,
}

const syncNotesEntity = "notes"

// syncEntityValidator validates inbound and outbound sync entities.
// Notes sync is explicitly feature-gated to keep rollout opt-in.
var syncEntityValidator tdsync.EntityValidator = func(entityType string) bool {
	if baseSyncableEntities[entityType] {
		return true
	}
	if entityType == syncNotesEntity {
		return features.IsEnabled(getBaseDir(), features.SyncNotes.Name)
	}
	return false
}

// errBootstrapNotNeeded signals that the server event count is below the snapshot threshold.
var errBootstrapNotNeeded = errors.New("bootstrap not needed")

var syncCmd = &cobra.Command{
	Use:     "sync",
	Short:   "Sync local data with remote server",
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		pushOnly, _ := cmd.Flags().GetBool("push")
		pullOnly, _ := cmd.Flags().GetBool("pull")
		statusOnly, _ := cmd.Flags().GetBool("status")

		if !syncconfig.IsAuthenticated() {
			output.Error("not logged in (run: td auth login)")
			return fmt.Errorf("not authenticated")
		}

		baseDir := getBaseDir()
		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("open database: %v", err)
			return err
		}
		defer database.Close()

		syncState, err := database.GetSyncState()
		if err != nil {
			output.Error("get sync state: %v", err)
			return err
		}
		if syncState == nil {
			output.Error("project not linked (run: td sync-project link <id>)")
			return fmt.Errorf("not linked")
		}

		deviceID, err := syncconfig.GetDeviceID()
		if err != nil {
			output.Error("get device id: %v", err)
			return err
		}

		serverURL := syncconfig.GetServerURL()
		apiKey := syncconfig.GetAPIKey()
		client := syncclient.New(serverURL, apiKey, deviceID)

		if statusOnly {
			return runSyncStatus(database, client, syncState)
		}

		// Never push or apply events from a damaged database. In particular,
		// orphan backfill interprets corrupt table pages as real entities and can
		// otherwise upload that garbage before the first write reports SQLITE_CORRUPT.
		if err := database.QuickCheck(); err != nil {
			output.Error("local database is corrupt; sync aborted before making changes: %v", err)
			return err
		}

		// Try snapshot bootstrap on first sync
		bootstrapped := false
		if !pushOnly && syncState.LastPulledServerSeq == 0 {
			newDB, err := runBootstrap(database, client, syncState)
			if newDB != nil {
				database = newDB // old DB already closed by runBootstrap
			}
			if err == nil {
				bootstrapped = true
				syncState, err = database.GetSyncState()
				if err != nil {
					output.Error("get sync state after bootstrap: %v", err)
					return err
				}
			} else if !errors.Is(err, errBootstrapNotNeeded) {
				output.Warning("bootstrap failed, falling back to normal pull: %v", err)
			}
		}

		if !pullOnly {
			if err := runPush(database, client, syncState, deviceID); err != nil {
				return err
			}
		}

		if !pushOnly && !bootstrapped {
			if err := runPull(database, client, syncState, deviceID); err != nil {
				return err
			}
		}

		return nil
	},
}

func runSyncStatus(database *db.DB, client *syncclient.Client, state *db.SyncState) error {
	pending, err := database.CountPendingEvents()
	if err != nil {
		output.Error("count pending: %v", err)
		return err
	}

	fmt.Printf("Project:     %s\n", state.ProjectID)
	fmt.Printf("Last pushed: action %d\n", state.LastPushedActionID)
	fmt.Printf("Last pulled: seq %d\n", state.LastPulledServerSeq)
	fmt.Printf("Pending:     %d events\n", pending)
	if state.LastSyncAt != nil {
		fmt.Printf("Last sync:   %s\n", state.LastSyncAt.Format(time.RFC3339))
	}

	serverStatus, err := client.SyncStatus(state.ProjectID)
	if err != nil {
		if errors.Is(err, syncclient.ErrUnauthorized) {
			output.Warning("unauthorized - re-login may be needed")
			return nil
		}
		output.Error("server status: %v", err)
		return err
	}

	fmt.Printf("\nServer:\n")
	fmt.Printf("  Events:    %d\n", serverStatus.EventCount)
	fmt.Printf("  Last seq:  %d\n", serverStatus.LastServerSeq)
	if serverStatus.LastEventTime != "" {
		fmt.Printf("  Last event: %s\n", serverStatus.LastEventTime)
	}
	return nil
}

func runBootstrap(database *db.DB, client *syncclient.Client, state *db.SyncState) (*db.DB, error) {
	threshold := syncconfig.GetSnapshotThreshold()
	if threshold <= 0 {
		return nil, errBootstrapNotNeeded
	}

	// Check for pending local changes before overwriting DB
	pendingCount, err := database.CountPendingEvents()
	if err == nil && pendingCount > 0 {
		output.Warning("bootstrap skipped: local changes pending push")
		return nil, errBootstrapNotNeeded
	}

	serverStatus, err := client.SyncStatus(state.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("check server status: %w", err)
	}

	if serverStatus.EventCount < int64(threshold) {
		return nil, errBootstrapNotNeeded
	}

	output.Info("bootstrapping from snapshot (server has %d events)...", serverStatus.EventCount)

	snapshot, err := client.GetSnapshot(state.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("download snapshot: %w", err)
	}
	if snapshot == nil {
		// Server returned 404 — no snapshot available, fall through
		return nil, errBootstrapNotNeeded
	}

	// Validate SQLite header
	if len(snapshot.Data) < 16 || string(snapshot.Data[:16]) != "SQLite format 3\x00" {
		return nil, fmt.Errorf("invalid snapshot: not a SQLite database")
	}

	dbPath := filepath.Join(database.BaseDir(), ".todos", "issues.db")
	backupPath := dbPath + ".pre-snapshot-backup"
	baseDir := database.BaseDir()

	// Stage and fully validate the replacement before touching the live DB.
	// A header check alone accepts snapshots with damaged b-trees or indexes.
	staged, err := os.CreateTemp(filepath.Dir(dbPath), ".issues.snapshot-*.db")
	if err != nil {
		return nil, fmt.Errorf("stage snapshot: %w", err)
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if _, err := staged.Write(snapshot.Data); err != nil {
		staged.Close()
		return nil, fmt.Errorf("stage snapshot: %w", err)
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return nil, fmt.Errorf("flush staged snapshot: %w", err)
	}
	if err := staged.Close(); err != nil {
		return nil, fmt.Errorf("close staged snapshot: %w", err)
	}
	if err := os.Chmod(stagedPath, 0o644); err != nil {
		return nil, fmt.Errorf("set staged snapshot permissions: %w", err)
	}
	if err := validateSQLiteFile(stagedPath); err != nil {
		return nil, fmt.Errorf("invalid snapshot: %w", err)
	}

	err = db.WithMaintenanceLock(baseDir, func() error {
		// Merge every committed WAL frame while no other cooperating td writer
		// can enter. A busy result means another connection still has SQLite
		// pinned; abort without touching the live generation.
		if err := checkpointSQLiteForReplacement(database); err != nil {
			return fmt.Errorf("prepare database replacement: %w", err)
		}
		if err := database.Close(); err != nil {
			return fmt.Errorf("close database for replacement: %w", err)
		}

		if err := copyFile(dbPath, backupPath); err != nil {
			return fmt.Errorf("backup db: %w", err)
		}

		// The last SQLite connection removes clean WAL/SHM sidecars on close. If
		// either still exists, another connection may still reference the old
		// inode/generation. Do not delete them and rename underneath that process;
		// fail closed and let the user stop it before retrying.
		if err := requireSQLiteSidecarsAbsent(dbPath); err != nil {
			return err
		}

		if err := os.Rename(stagedPath, dbPath); err != nil {
			return fmt.Errorf("install snapshot: %w", err)
		}
		return nil
	})
	if err != nil {
		// The original file remains installed for every failure before Rename.
		reopened, reopenErr := db.Open(baseDir)
		if reopenErr != nil {
			return nil, fmt.Errorf("replacement failed (%w) and reopen failed: %v", err, reopenErr)
		}
		return reopened, err
	}

	// Open only after releasing the maintenance lock: db.Open runs migrations,
	// which acquire the same non-reentrant cross-process lock.
	reopened, err := db.Open(baseDir)
	if err != nil {
		restored, restoreErr := restoreBootstrapBackup(baseDir, dbPath, backupPath)
		if restoreErr != nil {
			return nil, fmt.Errorf("reopen failed (%w) and restore failed: %v", err, restoreErr)
		}
		return restored, fmt.Errorf("reopen after bootstrap: %w", err)
	}

	// Use INSERT OR REPLACE since the snapshot may not contain sync_state.
	_, err = reopened.Conn().Exec(
		`INSERT OR REPLACE INTO sync_state (project_id, last_pulled_server_seq, last_pushed_action_id, last_sync_at, sync_disabled)
		 VALUES (?, ?, 0, CURRENT_TIMESTAMP, 0)`,
		state.ProjectID, snapshot.SnapshotSeq,
	)
	if err != nil {
		_ = reopened.Close()
		restored, restoreErr := restoreBootstrapBackup(baseDir, dbPath, backupPath)
		if restoreErr != nil {
			return nil, fmt.Errorf("sync_state update failed (%w) and restore failed: %v", err, restoreErr)
		}
		return restored, fmt.Errorf("update sync_state: %w", err)
	}

	fmt.Printf("Bootstrap complete (seq %d).\n", snapshot.SnapshotSeq)
	return reopened, nil
}

func validateSQLiteFile(path string) error {
	conn, err := db.OpenSQLite(path, db.OpenOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	database := db.NewWithConn(conn, filepath.Dir(path))
	defer database.Close()
	return database.QuickCheck()
}

func checkpointSQLiteForReplacement(database *db.DB) error {
	var busy, logFrames, checkpointedFrames int
	if err := database.Conn().QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(
		&busy, &logFrames, &checkpointedFrames,
	); err != nil {
		return fmt.Errorf("checkpoint WAL: %w", err)
	}
	if busy != 0 || checkpointedFrames < logFrames {
		return fmt.Errorf("database is busy (WAL frames=%d, checkpointed=%d); retry after other td processes exit",
			logFrames, checkpointedFrames)
	}
	return nil
}

func requireSQLiteSidecarsAbsent(dbPath string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		path := dbPath + suffix
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("refusing database replacement while %s exists; stop other td processes and retry", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
	}
	return nil
}

func restoreBootstrapBackup(baseDir, dbPath, backupPath string) (*db.DB, error) {
	if err := db.WithMaintenanceLock(baseDir, func() error {
		// Do not restore underneath a process that opened the new generation
		// during the unlocked db.Open/update window. Preserve both files and
		// surface the error for manual recovery instead.
		if err := requireSQLiteSidecarsAbsent(dbPath); err != nil {
			return err
		}
		if err := os.Rename(backupPath, dbPath); err != nil {
			return fmt.Errorf("restore backup: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	// As in installation, open after releasing the non-reentrant lock.
	reopened, err := db.Open(baseDir)
	if err != nil {
		return nil, fmt.Errorf("reopen restored backup: %w", err)
	}
	return reopened, nil
}

// copyFile copies src to dst, creating dst if it doesn't exist.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to back up
		}
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

const pushBatchSize = 500

func filterEventsForSync(events []tdsync.Event, validator tdsync.EntityValidator) []tdsync.Event {
	if validator == nil {
		return events
	}

	filtered := events[:0]
	for _, event := range events {
		if validator(event.EntityType) {
			filtered = append(filtered, event)
			continue
		}
		slog.Debug("sync: skipping feature-gated entity", "entity_type", event.EntityType, "entity_id", event.EntityID)
	}

	return filtered
}

func runPush(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID string) error {
	sess, err := session.GetOrCreate(database)
	if err != nil {
		output.Error("get session: %v", err)
		return err
	}

	conn := database.Conn()
	tx, err := conn.Begin()
	if err != nil {
		output.Error("begin tx: %v", err)
		return err
	}
	defer tx.Rollback()

	events, err := tdsync.GetPendingEvents(tx, deviceID, sess.ID)
	if err != nil {
		output.Error("get pending events: %v", err)
		return err
	}
	events = filterEventsForSync(events, syncEntityValidator)

	if len(events) == 0 {
		fmt.Println("Nothing to push.")
		return nil
	}

	var allAcks []tdsync.Ack
	var maxActionID int64
	totalAccepted := 0
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
				output.Error("unauthorized - re-login may be needed")
			} else {
				output.Error("push: %v", err)
			}
			return err
		}

		totalAccepted += pushResp.Accepted

		for _, a := range pushResp.Acks {
			allAcks = append(allAcks, tdsync.Ack{
				ClientActionID: a.ClientActionID,
				ServerSeq:      a.ServerSeq,
			})
			if a.ClientActionID > maxActionID {
				maxActionID = a.ClientActionID
			}
		}
		// Treat duplicate rejections as idempotent success — mark them synced too
		for _, r := range pushResp.Rejected {
			if r.Reason == "duplicate" && r.ServerSeq > 0 {
				allAcks = append(allAcks, tdsync.Ack{
					ClientActionID: r.ClientActionID,
					ServerSeq:      r.ServerSeq,
				})
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
		output.Error("mark synced: %v", err)
		return err
	}

	// Update sync_state within the same transaction to avoid race
	if maxActionID > 0 {
		if _, err := tx.Exec(`UPDATE sync_state SET last_pushed_action_id = ?, last_sync_at = CURRENT_TIMESTAMP`, maxActionID); err != nil {
			output.Error("update sync state: %v", err)
			return err
		}
	}

	if err := db.RecordSyncHistoryTx(tx, allHistoryEntries); err != nil {
		slog.Debug("sync: record push history", "err", err)
	}

	if err := tx.Commit(); err != nil {
		output.Error("commit: %v", err)
		return err
	}

	fmt.Printf("Pushed %d events.\n", totalAccepted)
	return nil
}

func runPull(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID string) error {
	lastSeq := state.LastPulledServerSeq
	totalPulled := 0
	totalApplied := 0
	totalOverwrites := 0
	totalSkipped := 0
	var allConflicts []tdsync.ConflictRecord

	for {
		// NOTE: intentionally pass empty exclude_client for batch pull.
		// Unlike autoSyncPull (incremental), batch pull needs all events
		// including own to maintain convergence via server_seq ordering.
		pullResp, err := client.Pull(state.ProjectID, lastSeq, 1000, "")
		if err != nil {
			if errors.Is(err, syncclient.ErrUnauthorized) {
				output.Error("unauthorized - re-login may be needed")
			} else {
				output.Error("pull: %v", err)
			}
			return err
		}

		if len(pullResp.Events) == 0 {
			break
		}

		// Convert pull events to sync events
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

		conn := database.Conn()
		tx, err := conn.Begin()
		if err != nil {
			output.Error("begin tx: %v", err)
			return err
		}

		result, err := tdsync.ApplyRemoteEvents(tx, events, deviceID, syncEntityValidator, state.LastSyncAt)
		if err != nil {
			tx.Rollback()
			output.Error("apply events: %v", err)
			return err
		}
		if err := failedRemoteEventsError(result); err != nil {
			tx.Rollback()
			output.Error("apply events: %v", err)
			return err
		}

		// Store conflict records
		if err := storeConflicts(tx, result.Conflicts); err != nil {
			tx.Rollback()
			output.Error("store conflicts: %v", err)
			return err
		}

		// Record deliberate drops and quarantined events with the same commit
		// that advances the cursor past them.
		skipped := resolveApplyOutcome(result)
		if err := db.RecordSkippedEventsTx(tx, skipped); err != nil {
			tx.Rollback()
			output.Error("record skipped events: %v", err)
			return err
		}
		totalSkipped += len(skipped)

		// Update sync_state within the same transaction to avoid race
		if _, err := tx.Exec(`UPDATE sync_state SET last_pulled_server_seq = ?, last_sync_at = CURRENT_TIMESTAMP`, pullResp.LastServerSeq); err != nil {
			tx.Rollback()
			output.Error("update sync state: %v", err)
			return err
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
			slog.Debug("sync: record pull history", "err", err)
		}

		if err := tx.Commit(); err != nil {
			output.Error("commit: %v", err)
			return err
		}

		totalPulled += len(pullResp.Events)
		totalApplied += result.Applied
		totalOverwrites += result.Overwrites
		allConflicts = append(allConflicts, result.Conflicts...)
		lastSeq = pullResp.LastServerSeq

		if !pullResp.HasMore {
			break
		}
	}

	if totalPulled == 0 {
		fmt.Println("Nothing to pull.")
	} else {
		fmt.Printf("Pulled %d events (%d applied).\n", totalPulled, totalApplied)
		if totalSkipped > 0 {
			output.Warning("%d remote event(s) skipped and recorded; see `td sync status`", totalSkipped)
		}
		if totalOverwrites > 0 {
			output.Warning("%d local records overwritten by remote changes:", totalOverwrites)
			maxShow := 10
			for i, c := range allConflicts {
				if i >= maxShow {
					fmt.Printf("  ... and %d more\n", len(allConflicts)-maxShow)
					break
				}
				fmt.Printf("  %s/%s (seq %d)\n", c.EntityType, c.EntityID, c.ServerSeq)
			}
		}
	}
	return nil
}

// failedRemoteEventsError reports the failures that must abort a pull batch.
// Thin wrapper over tdsync.ResolvePullOutcome, which owns the rule.
func failedRemoteEventsError(result tdsync.ApplyResult) error {
	return tdsync.ResolvePullOutcome(result).Abort
}

// resolveApplyOutcome returns the events to durably record as skipped: the
// deliberate drops plus any permanently-failed events being quarantined.
//
// Quarantining advances the cursor past an event that can never apply, which is
// what keeps the rest of the stream flowing. Nothing is discarded — every entry
// lands in sync_skipped_events with its error and server_seq and shows up in
// `td sync status`.
func resolveApplyOutcome(result tdsync.ApplyResult) []db.SkippedSyncEvent {
	outcome := tdsync.ResolvePullOutcome(result)
	var out []db.SkippedSyncEvent
	for _, s := range outcome.Record {
		if s.Reason == tdsync.SkipReasonQuarantined {
			slog.Warn("sync: quarantined unappliable remote event",
				"seq", s.ServerSeq, "entity", s.EntityType+"/"+s.EntityID, "err", s.Detail)
		}
		out = append(out, db.SkippedSyncEvent{
			ServerSeq:  s.ServerSeq,
			DeviceID:   s.DeviceID,
			ActionType: s.ActionType,
			EntityType: s.EntityType,
			EntityID:   s.EntityID,
			Reason:     s.Reason,
			Error:      s.Detail,
			Payload:    string(s.Payload),
		})
	}
	return out
}

// storeConflicts inserts conflict records into the sync_conflicts table.
func storeConflicts(tx *sql.Tx, conflicts []tdsync.ConflictRecord) error {
	if len(conflicts) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`INSERT INTO sync_conflicts (entity_type, entity_id, server_seq, local_data, remote_data, overwritten_at)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare conflict insert: %w", err)
	}
	defer stmt.Close()

	for _, c := range conflicts {
		localJSON := "null"
		if c.LocalData != nil {
			localJSON = string(c.LocalData)
		}
		remoteJSON := "null"
		if c.RemoteData != nil {
			remoteJSON = string(c.RemoteData)
		}
		if _, err := stmt.Exec(c.EntityType, c.EntityID, c.ServerSeq, localJSON, remoteJSON, c.OverwrittenAt); err != nil {
			return fmt.Errorf("insert conflict %s/%s: %w", c.EntityType, c.EntityID, err)
		}
	}
	return nil
}

// syncEnableCmd / syncDisableCmd flip the GLOBAL autosync master switch in
// ~/.config/td/config.json (sync.autosync). They are registered UNGATED (as
// subcommands of syncCmd) so a user can always turn sync back on even when the
// SyncCLI feature is otherwise off — the parent `td sync` being feature-gated
// would otherwise strand a user who had disabled sync.
var syncEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Clear the global autosync kill-switch (sync.autosync=true in config.json)",
	Long: `Sets the global autosync master switch to true in ~/.config/td/config.json.

This clears any global kill-switch so per-project sync configuration decides
whether autosync runs. It does NOT force-enable sync on an unconfigured
project. Works regardless of shell-init semantics (unlike TD_* env vars).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := syncconfig.SetGlobalAutosyncOverride(true); err != nil {
			output.Error("write config: %v", err)
			return err
		}
		output.Success("global autosync enabled (sync.autosync=true)")
		if v := syncconfig.GetGlobalAutosyncOverride(); v == nil || !*v {
			output.Warning("a TD_* env var is overriding config.json; unset it to take effect")
		}
		return nil
	},
}

var syncDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Engage the global autosync kill-switch (sync.autosync=false in config.json)",
	Long: `Sets the global autosync master switch to false in ~/.config/td/config.json.

This is a shell-independent kill-switch: every td process reads config.json, so
autosync is suppressed everywhere regardless of which shell-init files are
sourced. Re-enable with: td sync enable.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := syncconfig.SetGlobalAutosyncOverride(false); err != nil {
			output.Error("write config: %v", err)
			return err
		}
		output.Success("global autosync disabled (sync.autosync=false)")
		if v := syncconfig.GetGlobalAutosyncOverride(); v == nil || *v {
			output.Warning("a TD_* env var is overriding config.json; unset it to take effect")
		}
		return nil
	},
}

// syncAlwaysOnCmd is a minimal `td sync` parent used ONLY when the full,
// feature-gated syncCmd is not registered (SyncCLI off). It exists so the
// global kill-switch subcommands `enable`/`disable` remain reachable — a user
// who disabled sync must always be able to turn it back on without hand-editing
// config.json. (td-78b482 will ungate status/doctor separately under the same
// always-on surface.)
var syncAlwaysOnCmd = &cobra.Command{
	Use:     "sync",
	Short:   "Sync local data with remote server",
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// wireSyncCommands attaches the always-reachable sync subcommands (enable,
// disable and the read-only status diagnostic) to exactly one `td sync` parent
// and returns that parent for registration on the root command.
//
// enable/disable and status must be reachable regardless of the SyncCLI gate:
// a user who disabled sync has to be able to turn it back on, and `status` is
// the first diagnostic to reach for when sync looks stuck. They go on the full
// syncCmd when SyncCLI is on, else on the minimal always-on parent — exactly
// one, never both, since double-registration would leave a command with the
// wrong parent. (td-78b482)
//
// The gate is a parameter rather than a call to features.IsEnabledForProcess so
// that both branches are testable in a single process. init() runs against the
// ambient env before any test does, so a test that only inspects the resulting
// command tree can only ever observe whichever branch the developer's shell
// happened to select. (td-6fda71)
func wireSyncCommands(syncCLIEnabled bool, full, alwaysOn *cobra.Command, subs ...*cobra.Command) *cobra.Command {
	parent := alwaysOn
	if syncCLIEnabled {
		parent = full
	}
	for _, sub := range subs {
		parent.AddCommand(sub)
	}
	return parent
}

func init() {
	syncCmd.Flags().Bool("push", false, "Push only")
	syncCmd.Flags().Bool("pull", false, "Pull only")
	syncCmd.Flags().Bool("status", false, "Show sync status only")

	parent := wireSyncCommands(
		features.IsEnabledForProcess(features.SyncCLI.Name),
		syncCmd, syncAlwaysOnCmd,
		syncEnableCmd, syncDisableCmd, syncStatusCmd,
	)
	rootCmd.AddCommand(parent)
}
