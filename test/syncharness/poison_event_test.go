package syncharness

import (
	"errors"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	tdsync "github.com/marcus/td/internal/sync"
)

const poisonProj = "proj-poison"

// TestPoisonEvent_DoesNotWedgeTheStream is the deterministic regression test for
// td-8fe2bc: a remote event that can never apply used to halt a peer's sync
// permanently.
//
// It reproduces the real mechanism without the chaos harness. The peer replays
// its OWN events from a cursor suffix: it created a board, positioned an issue
// on it, then deleted the board. Its local state is already the end state, so
// replaying the position create tries to insert a child whose ON DELETE CASCADE
// parent is gone. Before the fix the FK violation rolled the batch back with the
// cursor preserved, so every later pull replayed the identical failure and
// nothing behind it ever applied again.
//
// The assertion is forward progress, not merely "no error": the cursor must
// advance and events sequenced AFTER the poison one must land.
func TestPoisonEvent_DoesNotWedgeTheStream(t *testing.T) {
	h := NewHarness(t, 2, poisonProj)
	h.EnableBoardPositionFKs()

	const (
		boardID = "bd-poison"
		issueID = "td-poison"
	)
	posID := db.BoardIssuePosID(boardID, issueID)

	// Parent rows first, so the position create is valid when it is authored.
	if err := h.Mutate("client-A", "create", "issues", issueID, map[string]any{
		"title": "poison issue", "status": "open",
	}); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := h.Mutate("client-A", "create", "boards", boardID, map[string]any{
		"name": "poison board",
	}); err != nil {
		t.Fatalf("create board: %v", err)
	}

	// Push and pull the parents back, so A's cursor sits PAST the board create.
	// That is what makes the later replay a suffix: the board create will not
	// come round again to re-supply the parent.
	if _, err := h.Push("client-A", poisonProj); err != nil {
		t.Fatalf("push A parents: %v", err)
	}
	if _, err := h.PullAll("client-A", poisonProj); err != nil {
		t.Fatalf("pull A parents: %v", err)
	}

	if err := h.Mutate("client-A", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 65536,
	}); err != nil {
		t.Fatalf("create position: %v", err)
	}

	// A hard-deletes the board. ON DELETE CASCADE takes the position row with
	// it, but the position's action_log entry survives and is still pending
	// push — so A is about to publish a create it can no longer apply itself.
	// This is the poison, and it is exactly the shape the chaos run produced.
	if _, err := h.Clients["client-A"].DB.Exec(`DELETE FROM boards WHERE id = ?`, boardID); err != nil {
		t.Fatalf("delete board locally: %v", err)
	}

	// Push WITHOUT pulling: the position create lands on the server ahead of
	// A's cursor — the event A will replay back at itself.
	if _, err := h.Push("client-A", poisonProj); err != nil {
		t.Fatalf("push A position: %v", err)
	}

	// An event sequenced strictly AFTER the poison one. If the stream wedges,
	// this never arrives — that is the defect under test.
	if err := h.Mutate("client-B", "create", "issues", "td-behind", map[string]any{
		"title": "behind the poison event", "status": "open",
	}); err != nil {
		t.Fatalf("create trailing issue: %v", err)
	}
	if _, err := h.Push("client-B", poisonProj); err != nil {
		t.Fatalf("push B: %v", err)
	}

	cursorBefore := h.Clients["client-A"].LastPulledSeq

	// A pulls with PullAll — the batch-pull path, which deliberately includes
	// the client's own events (cmd/sync.go runPull does the same). The batch
	// contains A's own poison create plus the later event.
	if _, err := h.PullAll("client-A", poisonProj); err != nil {
		t.Fatalf("pull must not fail on an unappliable event: %v", err)
	}
	// Capture before the second pull overwrites it.
	skippedOnPoisonPull := h.LastSkipped

	// 1. The cursor advanced past the poison event.
	if got := h.Clients["client-A"].LastPulledSeq; got <= cursorBefore {
		t.Fatalf("cursor did not advance: before=%d after=%d (stream is wedged)", cursorBefore, got)
	}

	// 2. The event behind the poison one actually applied.
	if ent := h.QueryEntity("client-A", "issues", "td-behind"); ent == nil {
		t.Fatal("event sequenced after the poison event did not apply — stream still blocked")
	}

	// 3. A second pull still makes progress rather than replaying the failure.
	if _, err := h.PullAll("client-A", poisonProj); err != nil {
		t.Fatalf("second pull failed: %v", err)
	}

	// 4. The skip was recorded, not silently swallowed.
	assertSkipRecorded(t, skippedOnPoisonPull, "board_issue_positions", posID)

	// 5. The orphaned row stayed absent. Resurrecting a child whose cascade
	//    parent is gone would be the divergence this fix exists to prevent.
	if ent := h.QueryEntityRaw("client-A", "board_issue_positions", posID); ent != nil {
		t.Fatalf("orphaned position was resurrected: %v", ent)
	}
	if ent := h.QueryEntityRaw("client-A", "boards", boardID); ent != nil {
		t.Fatalf("deleted board was resurrected: %v", ent)
	}
}

// TestPoisonEvent_TransientFailureStillRetries is the other side of the
// quarantine rule (td-8fe2bc AC5). A transient failure must NOT be quarantined:
// the event is valid and a retry can succeed, so the batch must roll back with
// the cursor preserved, exactly as before the fix.
//
// This pins the classifier rather than the pull plumbing, because that is where
// the two cases are actually told apart.
func TestPoisonEvent_TransientFailureStillRetries(t *testing.T) {
	permanent := []error{
		errors.New("upsert board_issue_positions/bip_x: constraint failed: FOREIGN KEY constraint failed (787)"),
		errors.New("upsert issues/td-x: UNIQUE constraint failed: issues.id"),
		errors.New(`invalid entity type: "widgets"`),
		errors.New(`unknown action type: "frobnicate"`),
		errors.New(`empty entity ID for "create" event`),
		errors.New("upsert issues/td-x: unmarshal payload: unexpected end of JSON input"),
		errors.New("no such column: nonexistent"),
		&tdsync.OrphanedParentError{
			EntityType: "board_issue_positions", EntityID: "bip_x",
			Column: "board_id", ParentTable: "boards", ParentID: "bd-x",
		},
	}
	for _, err := range permanent {
		if !tdsync.IsPermanentApplyError(err) {
			t.Errorf("expected PERMANENT (quarantine), got transient: %v", err)
		}
	}

	transient := []error{
		errors.New("database is locked"),
		errors.New("disk I/O error"),
		errors.New("database or disk is full"),
		errors.New("context deadline exceeded"),
		errors.New("sql: connection is already closed"),
		errors.New("interrupted"),
		// Unrecognised errors default to transient: a peer stalls loudly
		// rather than silently skipping an event we could not classify.
		errors.New("something nobody has seen before"),
		nil,
	}
	for _, err := range transient {
		if tdsync.IsPermanentApplyError(err) {
			t.Errorf("expected TRANSIENT (retry), got permanent: %v", err)
		}
	}

	// A constraint error aborted by a lock must read as transient: retrying is
	// always the safe reading when both markers are present.
	both := errors.New("constraint failed: database is locked")
	if tdsync.IsPermanentApplyError(both) {
		t.Error("transient marker must win over permanent when both appear")
	}

	// And the batch decision must follow the classification.
	transientBatch := tdsync.ApplyResult{Failed: []tdsync.FailedEvent{
		{ServerSeq: 9, Error: errors.New("database is locked")},
	}}
	outcome := tdsync.ResolvePullOutcome(transientBatch)
	if outcome.Abort == nil {
		t.Error("transient failure must abort the batch so it can be retried")
	}
	if len(outcome.Record) != 0 {
		t.Errorf("transient failure must not be quarantined, got %d records", len(outcome.Record))
	}

	permanentBatch := tdsync.ApplyResult{Failed: []tdsync.FailedEvent{
		{ServerSeq: 9, EntityType: "issues", EntityID: "td-x",
			Error: errors.New("constraint failed: FOREIGN KEY constraint failed")},
	}}
	outcome = tdsync.ResolvePullOutcome(permanentBatch)
	if outcome.Abort != nil {
		t.Errorf("permanent failure must not abort the batch: %v", outcome.Abort)
	}
	if len(outcome.Record) != 1 {
		t.Fatalf("permanent failure must be quarantined, got %+v", outcome.Record)
	}
	if outcome.Record[0].Reason != tdsync.SkipReasonQuarantined {
		t.Errorf("expected reason %q, got %q", tdsync.SkipReasonQuarantined, outcome.Record[0].Reason)
	}
	if outcome.Record[0].Detail == "" {
		t.Error("quarantined event must carry its error")
	}
}

// TestPoisonEvent_MissingParentDropsOnlyTheChild verifies the causality half of
// the fix: an orphaned create is dropped, and only it. Its siblings in the same
// batch still apply, so a single bad edge cannot take healthy rows with it.
func TestPoisonEvent_MissingParentDropsOnlyTheChild(t *testing.T) {
	h := NewHarness(t, 2, poisonProj+"-sibling")
	h.EnableBoardPositionFKs()

	const (
		goodBoard = "bd-good"
		issueID   = "td-sib"
	)
	goodPos := db.BoardIssuePosID(goodBoard, issueID)

	for _, m := range []struct {
		action, entity, id string
		data               map[string]any
	}{
		{"create", "issues", issueID, map[string]any{"title": "sib", "status": "open"}},
		{"create", "boards", goodBoard, map[string]any{"name": "good board"}},
		{"create", "board_issue_positions", goodPos, map[string]any{
			"board_id": goodBoard, "issue_id": issueID, "position": 65536}},
	} {
		if err := h.Mutate("client-A", m.action, m.entity, m.id, m.data); err != nil {
			t.Fatalf("%s %s: %v", m.action, m.entity, err)
		}
	}

	// A position whose board was never created anywhere: permanently orphaned.
	orphanPos := db.BoardIssuePosID("bd-never-existed", issueID)
	if _, err := h.Clients["client-A"].DB.Exec(
		`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp)
		 VALUES ('al-orphan', 'session-A-0001', 'board_set_position', 'board_issue_positions', ?, '{}', ?, datetime('now'))`,
		orphanPos,
		`{"id":"`+orphanPos+`","board_id":"bd-never-existed","issue_id":"`+issueID+`","position":65536}`,
	); err != nil {
		t.Fatalf("stage orphan event: %v", err)
	}

	if err := h.Sync("client-A", poisonProj+"-sibling"); err != nil {
		t.Fatalf("sync A: %v", err)
	}
	if err := h.Sync("client-B", poisonProj+"-sibling"); err != nil {
		t.Fatalf("sync B must not wedge on the orphan: %v", err)
	}

	// The healthy sibling landed on B.
	if ent := h.QueryEntity("client-B", "board_issue_positions", goodPos); ent == nil {
		t.Error("valid position did not apply — a dropped orphan took a healthy row with it")
	}
	// The orphan did not.
	if ent := h.QueryEntityRaw("client-B", "board_issue_positions", orphanPos); ent != nil {
		t.Errorf("orphaned position applied despite missing board: %v", ent)
	}
}

func assertSkipRecorded(t *testing.T, skipped []tdsync.SkippedEvent, entityType, entityID string) {
	t.Helper()
	for _, s := range skipped {
		if s.EntityType == entityType && s.EntityID == entityID {
			if s.Reason == "" {
				t.Error("skipped event recorded without a reason")
			}
			if s.Detail == "" {
				t.Error("skipped event recorded without an error detail")
			}
			if s.ServerSeq == 0 {
				t.Error("skipped event recorded without a server_seq")
			}
			return
		}
	}
	var got []string
	for _, s := range skipped {
		got = append(got, s.EntityType+"/"+s.EntityID+" ("+s.Reason+")")
	}
	t.Fatalf("no skip recorded for %s/%s; recorded: [%s] — a dropped event must never be silent",
		entityType, entityID, strings.Join(got, ", "))
}
