// Package tdsync owns td's steady-state sync policy and orchestration.
//
// It is safe for long-lived in-process consumers: Once never bootstraps or
// replaces issues.db, and callers may supply the database handle they already
// own.
package tdsync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/session"
	syncengine "github.com/marcus/td/internal/sync"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
)

const (
	httpTimeout   = 5 * time.Second
	pushBudget    = 6 * time.Second
	pushBackoff   = 250 * time.Millisecond
	pushBatchSize = 500
)

// Options configures one project's Syncer. DB is optional; when present, the
// caller retains ownership and Syncer never closes it.
type Options struct {
	BaseDir string
	DB      *db.DB
	Logger  *slog.Logger
}

// Gate is the complete steady-state sync decision for a project.
type Gate struct {
	Open          bool   `json:"open"`
	Authenticated bool   `json:"authenticated"`
	Configured    bool   `json:"configured"`
	KillSwitch    bool   `json:"kill_switch"`
	Reason        string `json:"reason"`
	Source        string `json:"source"`
}

// Result summarizes one push-and-pull round trip.
type Result struct {
	Pushed    int   `json:"pushed"`
	Pulled    int   `json:"pulled"`
	Pending   int64 `json:"pending"`
	Conflicts int   `json:"conflicts"`
}

type inFlightCall struct {
	done   chan struct{}
	result Result
	err    error
}

// Syncer owns one project's steady-state sync lifecycle.
type Syncer struct {
	baseDir string
	db      *db.DB
	logger  *slog.Logger

	mu       sync.Mutex
	inFlight *inFlightCall
}

// New constructs a Syncer without opening a database or doing network work.
func New(opts Options) (*Syncer, error) {
	baseDir := opts.BaseDir
	if baseDir == "" && opts.DB != nil {
		baseDir = opts.DB.BaseDir()
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Syncer{baseDir: baseDir, db: opts.DB, logger: logger}, nil
}

// Gate resolves the one sync gate used by commands and in-process consumers.
// It never returns an error; unavailable state closes the gate with a reason.
func (s *Syncer) Gate() Gate {
	database, closeDB, err := s.database()
	if closeDB && database != nil {
		defer database.Close()
	}
	return s.gate(database, err)
}

func (s *Syncer) gate(database *db.DB, dbErr error) Gate {
	g := Gate{Authenticated: syncconfig.IsAuthenticated(), Source: "derived-per-project"}
	if override := syncconfig.GetGlobalAutosyncOverride(); override != nil && !*override {
		g.KillSwitch = true
		g.Source = "global-kill-switch"
	}
	if v, source := features.Resolve(s.baseDir, features.SyncAutosync.Name); source != "default" && !g.KillSwitch {
		g.Source = "explicit-" + source
		if !v && !g.KillSwitch {
			g.Reason = "autosync explicitly disabled"
		}
	}

	if database != nil {
		state, err := database.GetSyncState()
		if err == nil && state != nil && state.ProjectID != "" && !state.SyncDisabled {
			g.Configured = true
		} else if err != nil && dbErr == nil {
			dbErr = err
		}
	}

	switch {
	case g.KillSwitch:
		g.Reason = "global autosync kill-switch is enabled"
	case !g.Authenticated:
		g.Reason = "not authenticated"
	case g.Reason != "":
		// Preserve the explicit-disable reason.
	case !syncconfig.GetAutoSyncEnabled():
		g.Reason = "autosync disabled"
	case s.baseDir == "" && s.db == nil:
		g.Reason = "no project directory resolved"
	case dbErr != nil:
		g.Reason = fmt.Sprintf("database unavailable: %v", dbErr)
	case !g.Configured:
		g.Reason = "project is not linked or sync is disabled"
	default:
		g.Open = true
		g.Reason = "sync enabled"
	}
	return g
}

func (s *Syncer) database() (*db.DB, bool, error) {
	if s.db != nil {
		return s.db, false, nil
	}
	if s.baseDir == "" {
		return nil, false, nil
	}
	database, err := db.Open(s.baseDir)
	return database, err == nil, err
}

// Once runs one push-and-optional-pull round trip. Concurrent callers share
// the same in-flight result. It never performs snapshot bootstrap or replaces
// the database file.
func (s *Syncer) Once(ctx context.Context) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if call := s.inFlight; call != nil {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-call.done:
			return call.result, call.err
		}
	}
	call := &inFlightCall{done: make(chan struct{})}
	s.inFlight = call
	s.mu.Unlock()

	call.result, call.err = s.once(ctx)
	s.mu.Lock()
	s.inFlight = nil
	close(call.done)
	s.mu.Unlock()
	return call.result, call.err
}

func (s *Syncer) once(ctx context.Context) (Result, error) {
	var result Result
	database, closeDB, err := s.database()
	if err != nil {
		return result, err
	}
	if closeDB {
		defer database.Close()
	}
	gate := s.gate(database, nil)
	if !gate.Open {
		s.logger.Debug("sync skipped", "reason", gate.Reason)
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := database.QuickCheck(); err != nil {
		result.Pending = countPending(database, s.logger)
		return result, fmt.Errorf("local database integrity check failed: %w", err)
	}

	state, err := database.GetSyncState()
	if err != nil {
		return result, fmt.Errorf("get sync state: %w", err)
	}
	deviceID, err := syncconfig.GetDeviceID()
	if err != nil {
		return result, fmt.Errorf("get device id: %w", err)
	}
	client := syncclient.New(syncconfig.GetServerURL(), syncconfig.GetAPIKey(), deviceID)
	client.HTTP.Timeout = httpTimeout
	client.HTTP.Transport = &contextTransport{ctx: ctx, base: http.DefaultTransport}

	result.Pushed, err = push(ctx, database, client, state, deviceID, s.baseDir, s.logger)
	if err != nil {
		result.Pending = countPending(database, s.logger)
		return result, err
	}
	if syncconfig.GetAutoSyncPull() {
		state, err = database.GetSyncState()
		if err != nil || state == nil {
			return result, fmt.Errorf("reload sync state: %w", err)
		}
		result.Pulled, result.Conflicts, err = pull(database, client, state, deviceID, s.baseDir, s.logger)
		if err != nil {
			result.Pending = countPending(database, s.logger)
			return result, err
		}
	}
	result.Pending = countPending(database, s.logger)
	return result, nil
}

type contextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t *contextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(req.WithContext(t.ctx))
}

func countPending(database *db.DB, logger *slog.Logger) int64 {
	pending, err := database.CountPendingEvents()
	if err != nil {
		logger.Debug("sync: count pending", "err", err)
		return 0
	}
	return pending
}

func entityValidator(baseDir string) syncengine.EntityValidator {
	base := map[string]bool{
		"issues": true, "logs": true, "comments": true, "handoffs": true,
		"boards": true, "work_sessions": true, "board_issue_positions": true,
		"issue_dependencies": true, "issue_files": true,
		"work_session_issues": true, "issue_reviews": true,
	}
	return func(entityType string) bool {
		return base[entityType] || entityType == "notes" && features.IsEnabled(baseDir, features.SyncNotes.Name)
	}
}

func filterEvents(events []syncengine.Event, validator syncengine.EntityValidator, logger *slog.Logger) []syncengine.Event {
	filtered := events[:0]
	for _, event := range events {
		if validator == nil || validator(event.EntityType) {
			filtered = append(filtered, event)
			continue
		}
		logger.Debug("sync: skipping feature-gated entity", "entity_type", event.EntityType, "entity_id", event.EntityID)
	}
	return filtered
}

func push(ctx context.Context, database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID, baseDir string, logger *slog.Logger) (int, error) {
	sess, err := session.GetOrCreate(database)
	if err != nil {
		return 0, fmt.Errorf("get session: %w", err)
	}
	tx, err := database.Conn().Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	events, err := syncengine.GetPendingEvents(tx, deviceID, sess.ID)
	if err != nil {
		return 0, fmt.Errorf("get pending: %w", err)
	}
	events = filterEvents(events, entityValidator(baseDir), logger)
	if len(events) == 0 {
		return 0, nil
	}

	deadline := time.Now().Add(pushBudget)
	var allAcks []syncengine.Ack
	var maxActionID int64
	var history []db.SyncHistoryEntry
	for i := 0; i < len(events); i += pushBatchSize {
		end := i + pushBatchSize
		if end > len(events) {
			end = len(events)
		}
		batch := events[i:end]
		req := &syncclient.PushRequest{DeviceID: deviceID, SessionID: sess.ID}
		for _, event := range batch {
			req.Events = append(req.Events, syncclient.EventInput{
				ClientActionID: event.ClientActionID, ActionType: event.ActionType,
				EntityType: event.EntityType, EntityID: event.EntityID, Payload: event.Payload,
				ClientTimestamp: event.ClientTimestamp.Format(time.RFC3339),
			})
		}
		resp, err := pushBatchWithRetry(ctx, client, state.ProjectID, req, deadline)
		if err != nil {
			if errors.Is(err, syncclient.ErrUnauthorized) {
				return len(allAcks), syncclient.ErrUnauthorized
			}
			return len(allAcks), fmt.Errorf("push batch %d/%d: %w", i/pushBatchSize+1, (len(events)+pushBatchSize-1)/pushBatchSize, err)
		}
		ackMap := make(map[int64]int64)
		for _, ack := range resp.Acks {
			allAcks = append(allAcks, syncengine.Ack{ClientActionID: ack.ClientActionID, ServerSeq: ack.ServerSeq})
			ackMap[ack.ClientActionID] = ack.ServerSeq
			if ack.ClientActionID > maxActionID {
				maxActionID = ack.ClientActionID
			}
		}
		for _, rejected := range resp.Rejected {
			if rejected.Reason == "duplicate" && rejected.ServerSeq > 0 {
				allAcks = append(allAcks, syncengine.Ack{ClientActionID: rejected.ClientActionID, ServerSeq: rejected.ServerSeq})
				if rejected.ClientActionID > maxActionID {
					maxActionID = rejected.ClientActionID
				}
			}
		}
		for _, event := range batch {
			if seq, ok := ackMap[event.ClientActionID]; ok {
				history = append(history, db.SyncHistoryEntry{Direction: "push", ActionType: event.ActionType, EntityType: event.EntityType, EntityID: event.EntityID, ServerSeq: seq, DeviceID: deviceID, Timestamp: time.Now()})
			}
		}
	}
	if err := syncengine.MarkEventsSynced(tx, allAcks); err != nil {
		return len(allAcks), fmt.Errorf("mark synced: %w", err)
	}
	if maxActionID > 0 {
		if _, err := tx.Exec(`UPDATE sync_state SET last_pushed_action_id = ?, last_sync_at = CURRENT_TIMESTAMP`, maxActionID); err != nil {
			return len(allAcks), fmt.Errorf("update state: %w", err)
		}
	}
	if err := db.RecordSyncHistoryTx(tx, history); err != nil {
		logger.Debug("sync: record push history", "err", err)
	}
	if err := db.PruneSyncHistory(tx, 500); err != nil {
		logger.Debug("sync: prune history", "err", err)
	}
	if err := tx.Commit(); err != nil {
		return len(allAcks), fmt.Errorf("commit: %w", err)
	}
	logger.Debug("sync: pushed", "events", len(allAcks))
	return len(allAcks), nil
}

func pushBatchWithRetry(ctx context.Context, client *syncclient.Client, projectID string, req *syncclient.PushRequest, deadline time.Time) (*syncclient.PushResponse, error) {
	originalTimeout := client.HTTP.Timeout
	defer func() { client.HTTP.Timeout = originalTimeout }()
	backoff := pushBackoff
	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if attempt > 0 {
				return nil, lastErr
			}
			remaining = httpTimeout
		}
		client.HTTP.Timeout = httpTimeout
		if remaining < httpTimeout {
			client.HTTP.Timeout = remaining
		}
		resp, err := client.Push(projectID, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if errors.Is(err, syncclient.ErrUnauthorized) {
			return nil, err
		}
		if time.Until(deadline) < backoff+pushBackoff {
			return nil, lastErr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

func pull(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID, baseDir string, logger *slog.Logger) (int, int, error) {
	lastSeq := state.LastPulledServerSeq
	totalPulled, totalConflicts := 0, 0
	for {
		resp, err := client.Pull(state.ProjectID, lastSeq, 1000, deviceID)
		if err != nil {
			return totalPulled, totalConflicts, fmt.Errorf("pull: %w", err)
		}
		if len(resp.Events) == 0 {
			break
		}
		events := make([]syncengine.Event, len(resp.Events))
		for i, event := range resp.Events {
			clientTS, err := time.Parse(time.RFC3339Nano, event.ClientTimestamp)
			if err != nil {
				clientTS, _ = time.Parse(time.RFC3339, event.ClientTimestamp)
			}
			events[i] = syncengine.Event{ServerSeq: event.ServerSeq, DeviceID: event.DeviceID, SessionID: event.SessionID, ClientActionID: event.ClientActionID, ActionType: event.ActionType, EntityType: event.EntityType, EntityID: event.EntityID, Payload: event.Payload, ClientTimestamp: clientTS}
		}
		conflicts, err := applyPullBatch(database, events, deviceID, resp.LastServerSeq, state.LastSyncAt, baseDir, logger)
		if err != nil {
			return totalPulled, totalConflicts, err
		}
		totalPulled += len(events)
		totalConflicts += conflicts
		lastSeq = resp.LastServerSeq
		logger.Debug("sync: pulled", "events", len(events))
		if !resp.HasMore {
			break
		}
	}
	return totalPulled, totalConflicts, nil
}

func applyPullBatch(database *db.DB, events []syncengine.Event, deviceID string, lastServerSeq int64, lastSyncAt *time.Time, baseDir string, logger *slog.Logger) (int, error) {
	tx, err := database.Conn().Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := syncengine.ApplyRemoteEvents(tx, events, deviceID, entityValidator(baseDir), lastSyncAt)
	if err != nil {
		return 0, fmt.Errorf("apply events: %w", err)
	}
	if err := syncengine.ResolvePullOutcome(result).Abort; err != nil {
		return 0, fmt.Errorf("apply events: %w", err)
	}
	if err := storeConflicts(tx, result.Conflicts); err != nil {
		return 0, fmt.Errorf("store conflicts: %w", err)
	}
	if err := db.RecordSkippedEventsTx(tx, resolveApplyOutcome(result, logger)); err != nil {
		return 0, fmt.Errorf("record skipped events: %w", err)
	}
	if _, err := tx.Exec(`UPDATE sync_state SET last_pulled_server_seq = ?, last_sync_at = CURRENT_TIMESTAMP`, lastServerSeq); err != nil {
		return 0, fmt.Errorf("update sync state: %w", err)
	}
	history := make([]db.SyncHistoryEntry, 0, len(events))
	for _, event := range events {
		history = append(history, db.SyncHistoryEntry{Direction: "pull", ActionType: event.ActionType, EntityType: event.EntityType, EntityID: event.EntityID, ServerSeq: event.ServerSeq, DeviceID: event.DeviceID, Timestamp: time.Now()})
	}
	if err := db.RecordSyncHistoryTx(tx, history); err != nil {
		logger.Debug("sync: record pull history", "err", err)
	}
	return len(result.Conflicts), tx.Commit()
}

func resolveApplyOutcome(result syncengine.ApplyResult, logger *slog.Logger) []db.SkippedSyncEvent {
	var skipped []db.SkippedSyncEvent
	for _, event := range syncengine.ResolvePullOutcome(result).Record {
		if event.Reason == syncengine.SkipReasonQuarantined {
			logger.Warn("sync: quarantined unappliable remote event", "seq", event.ServerSeq, "entity", event.EntityType+"/"+event.EntityID, "err", event.Detail)
		}
		skipped = append(skipped, db.SkippedSyncEvent{ServerSeq: event.ServerSeq, DeviceID: event.DeviceID, ActionType: event.ActionType, EntityType: event.EntityType, EntityID: event.EntityID, Reason: event.Reason, Error: event.Detail, Payload: string(event.Payload)})
	}
	return skipped
}

func storeConflicts(tx *sql.Tx, conflicts []syncengine.ConflictRecord) error {
	if len(conflicts) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`INSERT INTO sync_conflicts (entity_type, entity_id, server_seq, local_data, remote_data, overwritten_at) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, conflict := range conflicts {
		local, remote := "null", "null"
		if conflict.LocalData != nil {
			local = string(conflict.LocalData)
		}
		if conflict.RemoteData != nil {
			remote = string(conflict.RemoteData)
		}
		if _, err := stmt.Exec(conflict.EntityType, conflict.EntityID, conflict.ServerSeq, local, remote, conflict.OverwrittenAt); err != nil {
			return err
		}
	}
	return nil
}

// Push is retained for the explicit command's focused compatibility tests.
// New consumers should use Syncer.Once.
func Push(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID, baseDir string) error {
	_, err := push(context.Background(), database, client, state, deviceID, baseDir, slog.Default())
	return err
}

// Pull is retained for focused command compatibility tests.
func Pull(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID, baseDir string) error {
	_, _, err := pull(database, client, state, deviceID, baseDir, slog.Default())
	return err
}

// ApplyPullBatch is retained for focused command compatibility tests.
func ApplyPullBatch(database *db.DB, events []syncengine.Event, deviceID string, lastServerSeq int64, lastSyncAt *time.Time, baseDir string) error {
	_, err := applyPullBatch(database, events, deviceID, lastServerSeq, lastSyncAt, baseDir, slog.Default())
	return err
}

// PushBatchWithRetry is retained for focused command compatibility tests.
func PushBatchWithRetry(client *syncclient.Client, projectID string, req *syncclient.PushRequest, deadline time.Time) (*syncclient.PushResponse, error) {
	return pushBatchWithRetry(context.Background(), client, projectID, req, deadline)
}
