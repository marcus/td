package db

import (
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func seedIssueForReviewTests(t *testing.T, database *DB, id string) {
	t.Helper()
	now := time.Now()
	_, err := database.conn.Exec(`
		INSERT INTO issues (id, title, status, type, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, "review-test", "in_review", "task", "P2", now, now)
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
}

func TestCreateIssueReview_ReturnsIDAndPersists(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	seedIssueForReviewTests(t, database, "td-rvtest1")

	id, err := database.CreateIssueReview("td-rvtest1", "ses-reviewer", "approved", "looks good", "ses-orch")
	if err != nil {
		t.Fatalf("CreateIssueReview: %v", err)
	}
	if !strings.HasPrefix(id, "rv-") {
		t.Errorf("review id should use rv- prefix, got %q", id)
	}

	var (
		gotIssueID, gotReviewer, gotDecision, gotSummary, gotRequestedBy string
	)
	err = database.conn.QueryRow(`SELECT issue_id, reviewer_session, decision, summary, requested_by_session FROM issue_reviews WHERE id = ?`, id).
		Scan(&gotIssueID, &gotReviewer, &gotDecision, &gotSummary, &gotRequestedBy)
	if err != nil {
		t.Fatalf("scan inserted row: %v", err)
	}
	if gotIssueID != "td-rvtest1" {
		t.Errorf("issue_id: want td-rvtest1, got %q", gotIssueID)
	}
	if gotReviewer != "ses-reviewer" {
		t.Errorf("reviewer_session: want ses-reviewer, got %q", gotReviewer)
	}
	if gotDecision != "approved" {
		t.Errorf("decision: want approved, got %q", gotDecision)
	}
	if gotSummary != "looks good" {
		t.Errorf("summary: want 'looks good', got %q", gotSummary)
	}
	if gotRequestedBy != "ses-orch" {
		t.Errorf("requested_by_session: want ses-orch, got %q", gotRequestedBy)
	}
}

func TestGetActiveApprovalReview_ReturnsNilWhenNone(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	seedIssueForReviewTests(t, database, "td-rvempty")

	r, err := database.GetActiveApprovalReview("td-rvempty")
	if err != nil {
		t.Fatalf("GetActiveApprovalReview: %v", err)
	}
	if r != nil {
		t.Errorf("expected nil, got %+v", r)
	}
}

func TestGetActiveApprovalReview_IgnoresChangesRequested(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	seedIssueForReviewTests(t, database, "td-rvcr")

	if _, err := database.CreateIssueReview("td-rvcr", "ses-r", "changes_requested", "needs fixes", ""); err != nil {
		t.Fatalf("CreateIssueReview: %v", err)
	}
	got, err := database.GetActiveApprovalReview("td-rvcr")
	if err != nil {
		t.Fatalf("GetActiveApprovalReview: %v", err)
	}
	if got != nil {
		t.Errorf("changes_requested review should not count as active approval; got %+v", got)
	}
}

func TestGetActiveApprovalReview_ReturnsLatestApproval(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	seedIssueForReviewTests(t, database, "td-rvactive")

	if _, err := database.CreateIssueReview("td-rvactive", "ses-a", "approved", "first pass", ""); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	// Sleep to make sure timestamps differ at millisecond resolution.
	time.Sleep(10 * time.Millisecond)
	id2, err := database.CreateIssueReview("td-rvactive", "ses-b", "approved", "second pass", "")
	if err != nil {
		t.Fatalf("second approve: %v", err)
	}

	got, err := database.GetActiveApprovalReview("td-rvactive")
	if err != nil {
		t.Fatalf("GetActiveApprovalReview: %v", err)
	}
	if got == nil {
		t.Fatal("expected active approval, got nil")
	}
	if got.ID != id2 {
		t.Errorf("want latest approval id %q, got %q", id2, got.ID)
	}
	if got.ReviewerSession != "ses-b" {
		t.Errorf("reviewer_session: want ses-b, got %q", got.ReviewerSession)
	}
}

func TestListIssueReviews_ChronologicalOrder(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	seedIssueForReviewTests(t, database, "td-rvhist")

	id1, err := database.CreateIssueReview("td-rvhist", "ses-a", "changes_requested", "first", "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	id2, err := database.CreateIssueReview("td-rvhist", "ses-b", "approved", "second", "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	reviews, err := database.ListIssueReviews("td-rvhist")
	if err != nil {
		t.Fatalf("ListIssueReviews: %v", err)
	}
	if len(reviews) != 2 {
		t.Fatalf("want 2 rows, got %d", len(reviews))
	}
	if reviews[0].ID != id1 || reviews[1].ID != id2 {
		t.Errorf("wrong order: got %q then %q, want %q then %q",
			reviews[0].ID, reviews[1].ID, id1, id2)
	}
}

func TestSupersedeActiveReviews_MarksActive_LeavesSupersededAlone(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	seedIssueForReviewTests(t, database, "td-rvsup")

	id1, err := database.CreateIssueReview("td-rvsup", "ses-a", "approved", "first", "")
	if err != nil {
		t.Fatalf("create 1: %v", err)
	}
	// Supersede the first active review, then add a second.
	if err := database.SupersedeActiveReviews("td-rvsup"); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	id2, err := database.CreateIssueReview("td-rvsup", "ses-b", "approved", "second", "")
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}

	// Currently row1 superseded, row2 active.
	got, err := database.GetActiveApprovalReview("td-rvsup")
	if err != nil {
		t.Fatalf("GetActiveApprovalReview: %v", err)
	}
	if got == nil || got.ID != id2 {
		t.Fatalf("expected active review to be %q, got %+v", id2, got)
	}

	// Capture the supersede timestamp of row1 before supersede #2 runs.
	var firstSupersede time.Time
	if err := database.conn.QueryRow(`SELECT superseded_at FROM issue_reviews WHERE id = ?`, id1).Scan(&firstSupersede); err != nil {
		t.Fatalf("scan first supersede: %v", err)
	}

	if err := database.SupersedeActiveReviews("td-rvsup"); err != nil {
		t.Fatalf("second supersede: %v", err)
	}

	// After second supersede there should be no active approval, and row1
	// supersede timestamp must not have moved.
	got, err = database.GetActiveApprovalReview("td-rvsup")
	if err != nil {
		t.Fatalf("GetActiveApprovalReview (post-supersede): %v", err)
	}
	if got != nil {
		t.Errorf("expected no active approval after supersede, got %+v", got)
	}

	var secondSupersedeCheck time.Time
	if err := database.conn.QueryRow(`SELECT superseded_at FROM issue_reviews WHERE id = ?`, id1).Scan(&secondSupersedeCheck); err != nil {
		t.Fatalf("re-scan first supersede: %v", err)
	}
	if !secondSupersedeCheck.Equal(firstSupersede) {
		t.Errorf("supersede should be idempotent for already-superseded rows; row1 ts moved from %v to %v", firstSupersede, secondSupersedeCheck)
	}
}

// seedInReviewWithApproval prepares a fresh DB with one in_review issue that
// already has a reviewer stamp and an active approval review. Returns the DB
// so the caller can exercise mutation paths and re-query.
func seedInReviewWithApproval(t *testing.T, id string) *DB {
	t.Helper()
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	now := time.Now()
	_, err = database.conn.Exec(`
		INSERT INTO issues (id, title, status, type, priority, created_at, updated_at, reviewer_session, reviewed_at)
		VALUES (?, ?, 'in_review', 'task', 'P2', ?, ?, 'ses-reviewer', ?)
	`, id, "approval-invalidation-test", now, now, now)
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	if _, err := database.CreateIssueReview(id, "ses-reviewer", "approved", "looks good", "ses-orch"); err != nil {
		t.Fatalf("seed approval: %v", err)
	}
	return database
}

// assertApprovalCleared verifies both that no active approval remains and
// that the issue row's reviewer_session/reviewed_at were cleared.
func assertApprovalCleared(t *testing.T, database *DB, issueID string) {
	t.Helper()
	rev, err := database.GetActiveApprovalReview(issueID)
	if err != nil {
		t.Fatalf("GetActiveApprovalReview: %v", err)
	}
	if rev != nil {
		t.Errorf("expected approval superseded, got %+v", rev)
	}

	var reviewer string
	var reviewedAt *time.Time
	if err := database.conn.QueryRow(
		`SELECT reviewer_session, reviewed_at FROM issues WHERE id = ?`, issueID,
	).Scan(&reviewer, &reviewedAt); err != nil {
		t.Fatalf("scan issue row: %v", err)
	}
	if reviewer != "" {
		t.Errorf("reviewer_session should be cleared, got %q", reviewer)
	}
	if reviewedAt != nil {
		t.Errorf("reviewed_at should be cleared, got %v", *reviewedAt)
	}
}

func TestLinkFileLogged_SupersedesActiveApproval(t *testing.T) {
	database := seedInReviewWithApproval(t, "td-invlf1")

	if err := database.LinkFileLogged("td-invlf1", "docs/spec.md", models.FileRoleReference, "", "ses-impl"); err != nil {
		t.Fatalf("LinkFileLogged: %v", err)
	}
	assertApprovalCleared(t, database, "td-invlf1")
}

func TestUnlinkFileLogged_SupersedesActiveApproval(t *testing.T) {
	database := seedInReviewWithApproval(t, "td-invlf2")

	// Link a file up-front so there is something to unlink.
	if err := database.LinkFile("td-invlf2", "docs/spec.md", models.FileRoleReference, ""); err != nil {
		t.Fatalf("LinkFile seed: %v", err)
	}
	// Refresh the approval because the direct LinkFile write does not run
	// through the logged path (it is only used by sync receivers today).
	// Re-seed the approval so we start from a clean "approved" state.
	if err := database.conn.QueryRow(`UPDATE issue_reviews SET superseded_at = NULL WHERE issue_id = ?`, "td-invlf2").Scan(); err != nil && err.Error() != "sql: no rows in result set" {
		// UPDATE returns no rows either way; ignore.
	}
	// Reset reviewer stamp because LinkFile does not touch it.
	if _, err := database.conn.Exec(`UPDATE issues SET reviewer_session = 'ses-reviewer', reviewed_at = ? WHERE id = ?`, time.Now(), "td-invlf2"); err != nil {
		t.Fatalf("reset reviewer stamp: %v", err)
	}

	if err := database.UnlinkFileLogged("td-invlf2", "docs/spec.md", "ses-impl"); err != nil {
		t.Fatalf("UnlinkFileLogged: %v", err)
	}
	assertApprovalCleared(t, database, "td-invlf2")
}

func TestAddDependencyLogged_SupersedesActiveApproval(t *testing.T) {
	database := seedInReviewWithApproval(t, "td-invdep1")
	// Need a target issue for FK if strict FK is enabled — add one.
	now := time.Now()
	_, err := database.conn.Exec(`INSERT INTO issues (id, title, status, type, priority, created_at, updated_at) VALUES ('td-invdep2', 'target', 'open', 'task', 'P2', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("seed target: %v", err)
	}

	if err := database.AddDependencyLogged("td-invdep1", "td-invdep2", "depends_on", "ses-impl"); err != nil {
		t.Fatalf("AddDependencyLogged: %v", err)
	}
	assertApprovalCleared(t, database, "td-invdep1")
}

func TestRemoveDependencyLogged_SupersedesActiveApproval(t *testing.T) {
	database := seedInReviewWithApproval(t, "td-invdep3")
	now := time.Now()
	_, err := database.conn.Exec(`INSERT INTO issues (id, title, status, type, priority, created_at, updated_at) VALUES ('td-invdep4', 'target', 'open', 'task', 'P2', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("seed target: %v", err)
	}

	// Add via the unlogged path so we isolate the Remove path's behavior.
	if err := database.AddDependency("td-invdep3", "td-invdep4", "depends_on"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
	// Reset reviewer stamp (AddDependency does not touch it) so the supersede
	// test starts clean.
	if _, err := database.conn.Exec(`UPDATE issues SET reviewer_session = 'ses-reviewer', reviewed_at = ? WHERE id = ?`, time.Now(), "td-invdep3"); err != nil {
		t.Fatalf("reset reviewer stamp: %v", err)
	}

	if err := database.RemoveDependencyLogged("td-invdep3", "td-invdep4", "ses-impl"); err != nil {
		t.Fatalf("RemoveDependencyLogged: %v", err)
	}
	assertApprovalCleared(t, database, "td-invdep3")
}

func TestTagIssueToWorkSession_SupersedesActiveApproval(t *testing.T) {
	database := seedInReviewWithApproval(t, "td-invws1")

	// Seed a work session the issue can be tagged to.
	ws := &models.WorkSession{Name: "parity-ws", SessionID: "ses-orch"}
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession: %v", err)
	}

	if err := database.TagIssueToWorkSession(ws.ID, "td-invws1", "ses-impl"); err != nil {
		t.Fatalf("TagIssueToWorkSession: %v", err)
	}
	assertApprovalCleared(t, database, "td-invws1")
}

// TestSupersedeApprovalIfLinked_NoOpWhenNoActiveApproval guards against
// needless writes: the helper must short-circuit when there is no active
// approval row, so routine link/unlink on untouched issues doesn't churn
// reviewer_session on every call.
func TestSupersedeApprovalIfLinked_NoOpWhenNoActiveApproval(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Seed an in_review issue WITHOUT any reviews.
	now := time.Now()
	_, err = database.conn.Exec(`
		INSERT INTO issues (id, title, status, type, priority, created_at, updated_at, reviewer_session, reviewed_at)
		VALUES ('td-noop1', 'no-approval', 'in_review', 'task', 'P2', ?, ?, 'ses-r', ?)
	`, now, now, now)
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	database.supersedeApprovalIfLinked("td-noop1")

	// The reviewer_session/reviewed_at should NOT have been nulled out
	// because there was no approval to supersede.
	var reviewer string
	var reviewedAt *time.Time
	if err := database.conn.QueryRow(
		`SELECT reviewer_session, reviewed_at FROM issues WHERE id = ?`, "td-noop1",
	).Scan(&reviewer, &reviewedAt); err != nil {
		t.Fatalf("scan issue row: %v", err)
	}
	if reviewer != "ses-r" {
		t.Errorf("expected reviewer_session untouched, got %q", reviewer)
	}
	if reviewedAt == nil {
		t.Error("expected reviewed_at untouched, got nil")
	}
}
