package api

import (
	"errors"
	"fmt"
	"log/slog"

	tdsync "github.com/marcus/td/internal/sync"
)

// applyAcceptedEventsToProjectDB applies the given accepted events (those that
// InsertServerEvents acked, paired with their assigned server_seq) to the
// project-live DB for projectID. The full event slice that was passed to
// InsertServerEvents and its PushResult are required so we can pair each
// accepted event with its server_seq.
//
// Behavior:
//   - Acquires a project.db handle from the pool (initializing if needed) and
//     releases on return.
//   - Reads the current applied_events cursor and skips any events with
//     server_seq <= cursor (idempotent — supports retried pushes and protects
//     against double-apply when bootstrap replay just covered these seqs).
//   - Wraps replay + cursor update in a single project.db transaction so a
//     mid-batch failure leaves the cursor untouched and the next push retries.
//   - On any error, returns it so the caller can surface a 500 to the client;
//     the client is then expected to retry the push, which will be deduped by
//     events.db (UNIQUE on device_id, session_id, client_action_id) and reach
//     this path again with the same server_seqs.
//
// This is the inbound /v1/sync/push companion to ProjectLivePool's bootstrap
// replay — same machinery (tdsync.ApplyRemoteEvents), same cursor table.
// See plan §7.2 (td-watch-rich-ui-plan.md) for context.
func applyAcceptedEventsToProjectDB(
	pool *ProjectLivePool,
	projectID string,
	pushedEvents []tdsync.Event,
	pushResult tdsync.PushResult,
) error {
	if pool == nil {
		return errors.New("sync_apply: nil project_live_pool")
	}
	if pushResult.Accepted == 0 {
		return nil
	}

	// Build a server_seq lookup keyed by client_action_id (unique within a
	// single push request because the request carries one device_id +
	// session_id, and (device, session, client_action_id) is the dedupe key).
	seqByCAID := make(map[int64]int64, len(pushResult.Acks))
	for _, ack := range pushResult.Acks {
		seqByCAID[ack.ClientActionID] = ack.ServerSeq
	}

	// Pair pushed events with their assigned server_seq, in server_seq order
	// so ApplyRemoteEvents sees a monotonic stream.
	accepted := make([]tdsync.Event, 0, pushResult.Accepted)
	for _, ev := range pushedEvents {
		seq, ok := seqByCAID[ev.ClientActionID]
		if !ok {
			continue // rejected / duplicate — already in events.db, no apply needed
		}
		ev.ServerSeq = seq
		accepted = append(accepted, ev)
	}
	if len(accepted) == 0 {
		return nil
	}
	// Sort by server_seq ascending. InsertServerEvents assigns AUTOINCREMENT
	// in slice order so this is already sorted, but we sort defensively.
	for i := 1; i < len(accepted); i++ {
		for j := i; j > 0 && accepted[j-1].ServerSeq > accepted[j].ServerSeq; j-- {
			accepted[j-1], accepted[j] = accepted[j], accepted[j-1]
		}
	}

	db, err := pool.Acquire(projectID)
	if err != nil {
		return fmt.Errorf("acquire project.db: %w", err)
	}
	defer pool.Release(projectID)

	tx, err := db.Conn().Begin()
	if err != nil {
		return fmt.Errorf("begin project.db tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Read current cursor inside the tx so concurrent pushes see consistent
	// state. SQLite serializes writers, so two pushes targeting the same
	// project.db take turns here — no double-apply.
	var cursor int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(server_seq), 0) FROM applied_events`).Scan(&cursor); err != nil {
		return fmt.Errorf("read applied cursor: %w", err)
	}

	// Skip events already covered by a prior apply (idempotent path: catches
	// the case where bootstrap replay or a previous push retry already wrote
	// these rows).
	toApply := accepted[:0:len(accepted)]
	for _, ev := range accepted {
		if ev.ServerSeq > cursor {
			toApply = append(toApply, ev)
		}
	}
	if len(toApply) == 0 {
		// Nothing new — cursor already past these seqs. Leave as-is.
		return nil
	}

	validator := func(t string) bool { return isValidEntityType(t) }
	// lastSyncAt nil: server-side replay, no client-conflict semantics needed.
	if _, err := tdsync.ApplyRemoteEvents(tx, toApply, "", validator, nil); err != nil {
		return fmt.Errorf("apply remote events: %w", err)
	}

	newCursor := toApply[len(toApply)-1].ServerSeq
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO applied_events(server_seq, applied_at) VALUES (?, datetime('now'))`,
		newCursor,
	); err != nil {
		return fmt.Errorf("update applied cursor: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit project.db tx: %w", err)
	}

	slog.Debug("applied push to project.db",
		"project", projectID,
		"events", len(toApply),
		"cursor_from", cursor,
		"cursor_to", newCursor,
	)
	return nil
}
