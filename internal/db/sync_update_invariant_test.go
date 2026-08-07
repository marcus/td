package db

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

// ============================================================================
// The sync-event invariant, update edition
//
// TestEverySyncableRowGetsASyncEvent (sync_event_invariant_test.go) asserts
// every ROW has a create event. That guard is blind to the other half of the
// same defect class: a column UPDATE applied straight to the issues table with
// no matching action_log entry. The row has its create event, so the row-level
// invariant passes — and the mutated column still exists on the writing client
// and nowhere else, permanently, for exactly the same reason (the sync engine
// derives events only from action_log, and both BackfillOrphanEntities and
// BackfillStaleIssues bail out once last_pulled_server_seq > 0).
//
// That is how td-018ee1's sibling escaped: db.supersedeApprovalIfLinked cleared
// issues.reviewer_session / reviewed_at with a bare
//
//	UPDATE issues SET reviewer_session = '', reviewed_at = NULL WHERE id = ?
//
// after a dependency / file-link / work-session-tag mutation. TestChaosSync
// caught it as `issues match — common set diverges`, with the reviewing client
// holding a reviewer_session its peer had cleared.
//
// So the guard here is global and works in payload space: replay the issue's
// action_log entries the way internal/sync/events.go applies them, and assert
// the reconstruction equals the issue as it is serialized today. Any write that
// changes an issue column without a faithful action_log payload — no entry at
// all, or an entry whose payload omits the column — fails this test.
// ============================================================================

// issueFieldsSkippedByReconstruction are compared loosely or not at all.
//
//   - created_at / updated_at: bookkeeping timestamps whose SQLite round-trip
//     precision is not worth asserting on; a write that touches only these
//     cannot diverge a peer's view of the work.
//   - reviewed_at / closed_at / deleted_at: compared for PRESENCE only (see
//     comparePresenceOnly) for the same round-trip reason. Presence is the part
//     that carries meaning and the part the bug above got wrong.
var issueFieldsSkippedByReconstruction = map[string]bool{
	"created_at": true,
	"updated_at": true,
}

var issueFieldsComparedByPresence = map[string]bool{
	"reviewed_at": true,
	"closed_at":   true,
	"deleted_at":  true,
}

// unwrapIssueActionPayload mirrors internal/sync/events.go unwrapIssuePayload:
// review-undo events nest the issue under an "issue" key.
func unwrapIssueActionPayload(fields map[string]any) map[string]any {
	if inner, ok := fields["issue"]; ok {
		if m, ok := inner.(map[string]any); ok {
			return m
		}
	}
	return fields
}

// reconstructIssueFromActionLog replays every non-undone action_log entry for
// one issue using the same rules internal/sync/events.go applies on the
// receiving side:
//
//   - "create"-shaped events replace the whole state with new_data
//   - "update"-shaped events with usable previous_data apply only the diffed
//     fields (applyPartialUpdateEvent), and otherwise replace the whole state
//   - a nil in the diff means the field was dropped, i.e. set to NULL
//
// Ordering is by rowid, which is what GetPendingEvents uses.
func reconstructIssueFromActionLog(t *testing.T, database *DB, issueID string) (map[string]any, bool) {
	t.Helper()

	rows, err := database.conn.Query(`
		SELECT action_type, COALESCE(previous_data, ''), COALESCE(new_data, '')
		FROM action_log
		WHERE entity_id = ? AND entity_type IN ('issue', 'issues') AND undone = 0
		ORDER BY rowid ASC`, issueID)
	if err != nil {
		t.Fatalf("query action_log for %s: %v", issueID, err)
	}
	defer rows.Close()

	var state map[string]any
	seen := false
	for rows.Next() {
		var actionType, prevRaw, newRaw string
		if err := rows.Scan(&actionType, &prevRaw, &newRaw); err != nil {
			t.Fatalf("scan action_log for %s: %v", issueID, err)
		}
		if newRaw == "" || newRaw == "{}" {
			// delete / soft_delete / restore carry no field payload; the
			// receiving side handles them structurally.
			continue
		}
		var newFields map[string]any
		if err := json.Unmarshal([]byte(newRaw), &newFields); err != nil {
			continue
		}
		newFields = unwrapIssueActionPayload(newFields)
		seen = true

		var prevFields map[string]any
		usePartial := prevRaw != "" && prevRaw != "{}" && actionType != "create"
		if usePartial {
			if err := json.Unmarshal([]byte(prevRaw), &prevFields); err != nil {
				usePartial = false
			} else {
				prevFields = unwrapIssueActionPayload(prevFields)
			}
		}

		if state == nil || !usePartial {
			state = make(map[string]any, len(newFields))
			for k, v := range newFields {
				state[k] = v
			}
			continue
		}

		for k, v := range newFields {
			if k == "id" {
				continue
			}
			if old, existed := prevFields[k]; !existed || !reflect.DeepEqual(old, v) {
				state[k] = v
			}
		}
		for k := range prevFields {
			if k == "id" {
				continue
			}
			if _, still := newFields[k]; !still {
				delete(state, k)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate action_log for %s: %v", issueID, err)
	}
	return state, seen
}

// currentIssuePayload serializes the issue exactly as the logged write path
// would (marshalIssue), so the comparison happens in payload space and does not
// have to model column-vs-JSON representation differences.
func currentIssuePayload(t *testing.T, database *DB, issueID string) map[string]any {
	t.Helper()
	issue, err := database.GetIssue(issueID)
	if err != nil {
		t.Fatalf("GetIssue %s: %v", issueID, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(marshalIssue(issue)), &m); err != nil {
		t.Fatalf("unmarshal current issue %s: %v", issueID, err)
	}
	return m
}

// assertActionLogReconstructsEveryIssue is the invariant assertion itself.
func assertActionLogReconstructsEveryIssue(t *testing.T, database *DB) {
	t.Helper()

	rows, err := database.conn.Query(`SELECT id FROM issues WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan issue id: %v", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate issues: %v", err)
	}

	for _, id := range ids {
		replayed, seen := reconstructIssueFromActionLog(t, database, id)
		if !seen {
			// No create event at all — that is the row-level invariant's
			// business (TestEverySyncableRowGetsASyncEvent), not this one.
			continue
		}
		current := currentIssuePayload(t, database, id)

		keys := make(map[string]bool, len(current)+len(replayed))
		for k := range current {
			keys[k] = true
		}
		for k := range replayed {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)

		for _, k := range sorted {
			if issueFieldsSkippedByReconstruction[k] {
				continue
			}
			want, wantOK := current[k]
			got, gotOK := replayed[k]

			if issueFieldsComparedByPresence[k] {
				if wantOK != gotOK {
					t.Errorf("issue %s: field %q presence diverges — this client has it %s, "+
						"but replaying its action_log gives it %s, so no peer will ever agree",
						id, k, presence(wantOK), presence(gotOK))
				}
				continue
			}

			if wantOK != gotOK || !reflect.DeepEqual(want, got) {
				t.Errorf("issue %s: field %q is %#v on this client but %#v after replaying its "+
					"action_log — the change was written without a faithful action_log payload, "+
					"so it exists here and nowhere else, permanently",
					id, k, want, got)
			}
		}
	}
}

func presence(ok bool) string {
	if ok {
		return "set"
	}
	return "unset"
}

// TestActionLogReconstructsEveryIssue drives a broad stream of ordinary local
// mutations and then asserts the update-side invariant for every issue.
//
// It deliberately uses the *logged* API variants: the unlogged twins exist for
// the sync receiver applying remote events, where a local action_log entry
// would be wrong.
func TestActionLogReconstructsEveryIssue(t *testing.T) {
	database := claimTestDB(t)
	const session = "ses_update_invariant"

	mk := func(title string, typ models.Type) *models.Issue {
		t.Helper()
		iss := &models.Issue{Title: title, Type: typ, Status: models.StatusOpen, Priority: models.PriorityP2}
		if err := database.CreateIssueLogged(iss, session); err != nil {
			t.Fatalf("CreateIssueLogged %s: %v", title, err)
		}
		got, err := database.GetIssue(iss.ID)
		if err != nil {
			t.Fatalf("GetIssue %s: %v", iss.ID, err)
		}
		return got
	}

	// --- ordinary field edits ----------------------------------------------
	edited := mk("update invariant edited", models.TypeTask)
	edited.Title = "update invariant edited (renamed)"
	edited.Description = "a description added later"
	edited.Priority = models.PriorityP1
	edited.Points = 3
	edited.Labels = []string{"alpha", "beta"}
	edited.Acceptance = "it converges"
	edited.Sprint = "s-1"
	edited.CreatedBranch = "feature/update-invariant"
	if err := database.UpdateIssueLogged(edited, session, models.ActionUpdate); err != nil {
		t.Fatalf("edit issue: %v", err)
	}

	// --- the full start / review / approve lifecycle ------------------------
	reviewed := mk("update invariant reviewed", models.TypeTask)
	reviewed.Status = models.StatusInProgress
	reviewed.ImplementerSession = "ses_impl"
	if err := database.UpdateIssueLogged(reviewed, session, models.ActionStart); err != nil {
		t.Fatalf("start reviewed: %v", err)
	}
	reviewed, err := database.GetIssue(reviewed.ID)
	if err != nil {
		t.Fatalf("GetIssue reviewed: %v", err)
	}
	reviewed.Status = models.StatusInReview
	reviewed.ReviewRequestedBySession = "ses_impl"
	if err := database.UpdateIssueLogged(reviewed, session, models.ActionReview); err != nil {
		t.Fatalf("request review: %v", err)
	}
	reviewed, err = database.GetIssue(reviewed.ID)
	if err != nil {
		t.Fatalf("GetIssue reviewed: %v", err)
	}
	reviewed.ReviewerSession = "ses_reviewer"
	now := time.Now()
	reviewed.ReviewedAt = &now
	if _, err := database.CreateIssueReviewAndUpdateIssueLogged(
		NewReview{
			IssueID:            reviewed.ID,
			ReviewerSession:    "ses_reviewer",
			Decision:           "approved",
			Summary:            "looks right",
			RequestedBySession: "ses_impl",
		},
		reviewed, models.StatusInReview, "ses_reviewer", models.ActionApprove,
	); err != nil {
		t.Fatalf("record approval: %v", err)
	}

	// Guard: the approval stamp must actually be on the row, or the assertions
	// below would pass without exercising the clearing paths at all.
	if got, err := database.GetIssue(reviewed.ID); err != nil {
		t.Fatalf("GetIssue reviewed: %v", err)
	} else if got.ReviewerSession != "ses_reviewer" {
		t.Fatalf("reviewer_session was not stamped (%q); the scenario below tests nothing",
			got.ReviewerSession)
	}
	if rv, err := database.GetActiveApprovalReview(reviewed.ID); err != nil || rv == nil {
		t.Fatalf("no active approval review on %s (err=%v); the clearing paths below "+
			"are no-ops and this test would pass vacuously", reviewed.ID, err)
	}

	// --- the side-table mutations that invalidate that approval -------------
	// Each of these calls db.supersedeApprovalIfLinked, which clears
	// issues.reviewer_session / reviewed_at.
	blocker := mk("update invariant blocker", models.TypeTask)
	if err := database.AddDependencyLogged(reviewed.ID, blocker.ID, "depends_on", session); err != nil {
		t.Fatalf("AddDependencyLogged: %v", err)
	}

	// The same clearing path reached through a file link.
	linked := reviewApprovedIssue(t, database, "update invariant linked", session)
	if err := database.LinkFileLogged(linked.ID, "internal/db/reviews.go",
		models.FileRoleImplementation, "", session); err != nil {
		t.Fatalf("LinkFileLogged: %v", err)
	}

	// And through a work-session tag.
	tagged := reviewApprovedIssue(t, database, "update invariant tagged", session)
	ws := &models.WorkSession{Name: "update invariant work session", SessionID: session}
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession: %v", err)
	}
	if err := database.TagIssueToWorkSession(ws.ID, tagged.ID, session); err != nil {
		t.Fatalf("TagIssueToWorkSession: %v", err)
	}

	// Guard: the clearing really happened, so the invariant below is not
	// asserting over an untouched row.
	for _, id := range []string{reviewed.ID, linked.ID, tagged.ID} {
		got, err := database.GetIssue(id)
		if err != nil {
			t.Fatalf("GetIssue %s: %v", id, err)
		}
		if got.ReviewerSession != "" {
			t.Fatalf("issue %s still carries reviewer_session %q — the approval "+
				"invalidation path did not run, so this test proves nothing",
				id, got.ReviewerSession)
		}
	}

	// --- claim release, close, reopen ---------------------------------------
	claimed := mk("update invariant claimed", models.TypeTask)
	claimed.Status = models.StatusInProgress
	claimed.ImplementerSession = "ses_dead"
	if err := database.UpdateIssueLogged(claimed, "ses_dead", models.ActionStart); err != nil {
		t.Fatalf("claim issue: %v", err)
	}
	if _, err := database.ReleaseClaims(
		[]ClaimRelease{{IssueID: claimed.ID, LogMessage: "released by update invariant"}},
		session); err != nil {
		t.Fatalf("ReleaseClaims: %v", err)
	}

	assertActionLogReconstructsEveryIssue(t, database)
}

// reviewApprovedIssue creates an issue, drives it to in_review and records an
// active approval on it, returning the reloaded row.
func reviewApprovedIssue(t *testing.T, database *DB, title, session string) *models.Issue {
	t.Helper()

	iss := &models.Issue{Title: title, Type: models.TypeTask, Status: models.StatusOpen, Priority: models.PriorityP2}
	if err := database.CreateIssueLogged(iss, session); err != nil {
		t.Fatalf("CreateIssueLogged %s: %v", title, err)
	}
	loaded, err := database.GetIssue(iss.ID)
	if err != nil {
		t.Fatalf("GetIssue %s: %v", iss.ID, err)
	}
	loaded.Status = models.StatusInReview
	loaded.ImplementerSession = "ses_impl"
	loaded.ReviewRequestedBySession = "ses_impl"
	if err := database.UpdateIssueLogged(loaded, session, models.ActionReview); err != nil {
		t.Fatalf("request review %s: %v", iss.ID, err)
	}
	loaded, err = database.GetIssue(iss.ID)
	if err != nil {
		t.Fatalf("GetIssue %s: %v", iss.ID, err)
	}
	now := time.Now()
	loaded.ReviewerSession = "ses_reviewer"
	loaded.ReviewedAt = &now
	if _, err := database.CreateIssueReviewAndUpdateIssueLogged(
		NewReview{
			IssueID:            iss.ID,
			ReviewerSession:    "ses_reviewer",
			Decision:           "approved",
			RequestedBySession: "ses_impl",
		},
		loaded, models.StatusInReview, "ses_reviewer", models.ActionApprove,
	); err != nil {
		t.Fatalf("record approval %s: %v", iss.ID, err)
	}
	got, err := database.GetIssue(iss.ID)
	if err != nil {
		t.Fatalf("GetIssue %s: %v", iss.ID, err)
	}
	return got
}
