package db

import (
	"testing"

	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

// TestReadyToCloseByFilter_DelegatedMode exercises the composer used by the
// CLI + monitor + snapshot-query-source to populate the "ready to close"
// bucket. This is the composer-level parity test the Step 1c reviewer asked
// for: asserting the shared SQL composer selects exactly the rows a
// delegated-mode caller expects so monitor / CLI / API cannot drift.
func TestReadyToCloseByFilter_DelegatedMode(t *testing.T) {
	database, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer database.Close()

	// Fixture: issue A — in_review, active approval, caller is requester.
	a := &models.Issue{Title: "A: ready to close (requester)", Type: models.TypeTask, Status: models.StatusOpen}
	if err := database.CreateIssue(a); err != nil {
		t.Fatalf("create A: %v", err)
	}
	a.Status = models.StatusInReview
	a.ImplementerSession = "ses-impl"
	a.ReviewRequestedBySession = "ses-closer"
	_ = database.UpdateIssue(a)
	if _, err := database.CreateIssueReview(a.ID, "ses-reviewer", reviewpolicy.DecisionApproved, "", "ses-closer"); err != nil {
		t.Fatalf("create review for A: %v", err)
	}

	// Fixture: issue B — in_review, active approval, but caller has no role.
	b := &models.Issue{Title: "B: approved, caller has no role", Type: models.TypeTask, Status: models.StatusOpen}
	if err := database.CreateIssue(b); err != nil {
		t.Fatalf("create B: %v", err)
	}
	b.Status = models.StatusInReview
	b.ImplementerSession = "ses-impl"
	_ = database.UpdateIssue(b)
	if _, err := database.CreateIssueReview(b.ID, "ses-reviewer", reviewpolicy.DecisionApproved, "", ""); err != nil {
		t.Fatalf("create review for B: %v", err)
	}

	// Fixture: issue C — in_review, NO active approval. Not ready to close.
	c := &models.Issue{Title: "C: in_review, no approval", Type: models.TypeTask, Status: models.StatusOpen}
	if err := database.CreateIssue(c); err != nil {
		t.Fatalf("create C: %v", err)
	}
	c.Status = models.StatusInReview
	c.ImplementerSession = "ses-impl"
	c.ReviewRequestedBySession = "ses-closer"
	_ = database.UpdateIssue(c)

	// Fixture: issue D — closed. Must not appear in in_review bucket.
	d := &models.Issue{Title: "D: already closed", Type: models.TypeTask, Status: models.StatusOpen}
	if err := database.CreateIssue(d); err != nil {
		t.Fatalf("create D: %v", err)
	}
	d.Status = models.StatusClosed
	_ = database.UpdateIssue(d)

	results, err := database.ListIssues(ListIssuesOptions{
		ReadyToCloseBy:   "ses-closer",
		ReviewPolicyMode: "delegated",
	})
	if err != nil {
		t.Fatalf("list ready_to_close: %v", err)
	}

	found := map[string]bool{}
	for _, iss := range results {
		found[iss.ID] = true
	}
	if !found[a.ID] {
		t.Errorf("expected issue A in ready-to-close bucket (requester role)")
	}
	if found[b.ID] {
		t.Errorf("issue B should NOT be in ready-to-close bucket (session has no role)")
	}
	if found[c.ID] {
		t.Errorf("issue C should NOT be in ready-to-close bucket (no active approval)")
	}
	if found[d.ID] {
		t.Errorf("issue D should NOT be in ready-to-close bucket (already closed)")
	}
}

// TestReadyToCloseByFilter_StrictIsEmpty asserts the composer returns no rows
// under strict mode — strict has no close-after-review flow, so the ready-to-
// close bucket is always empty.
func TestReadyToCloseByFilter_StrictIsEmpty(t *testing.T) {
	database, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer database.Close()

	// Seed: in_review issue with an active approval + caller as requester
	// (would match in delegated mode, must NOT match in strict).
	iss := &models.Issue{Title: "x", Type: models.TypeTask, Status: models.StatusOpen}
	if err := database.CreateIssue(iss); err != nil {
		t.Fatalf("create: %v", err)
	}
	iss.Status = models.StatusInReview
	iss.ReviewRequestedBySession = "ses-closer"
	_ = database.UpdateIssue(iss)
	_, _ = database.CreateIssueReview(iss.ID, "ses-reviewer", reviewpolicy.DecisionApproved, "", "ses-closer")

	results, err := database.ListIssues(ListIssuesOptions{
		ReadyToCloseBy:   "ses-closer",
		ReviewPolicyMode: "strict",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("strict mode returned %d ready-to-close rows, want 0", len(results))
	}
}

func TestReviewableByFilter_DelegatedIgnoresNonImplementationHistory(t *testing.T) {
	database, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:              "Delegated reviewer can review again",
		Type:               models.TypeTask,
		Status:             models.StatusInReview,
		ImplementerSession: "ses-impl",
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("persist review fields: %v", err)
	}
	if err := database.RecordSessionAction(issue.ID, "ses-reviewer", models.ActionSessionReviewApproved); err != nil {
		t.Fatalf("record review history: %v", err)
	}

	delegated, err := database.ListIssues(ListIssuesOptions{
		ReviewableBy:     "ses-reviewer",
		ReviewPolicyMode: "delegated",
	})
	if err != nil {
		t.Fatalf("delegated list: %v", err)
	}
	if len(delegated) != 1 || delegated[0].ID != issue.ID {
		t.Fatalf("delegated reviewable = %v, want only %s", idsForTest(delegated), issue.ID)
	}

	balanced, err := database.ListIssues(ListIssuesOptions{
		ReviewableBy:     "ses-reviewer",
		ReviewPolicyMode: "balanced",
	})
	if err != nil {
		t.Fatalf("balanced list: %v", err)
	}
	if len(balanced) != 0 {
		t.Fatalf("balanced reviewable = %v, want empty because prior non-creator history still blocks", idsForTest(balanced))
	}

	if err := database.RecordSessionAction(issue.ID, "ses-reviewer", models.ActionSessionStarted); err != nil {
		t.Fatalf("record implementation history: %v", err)
	}
	delegated, err = database.ListIssues(ListIssuesOptions{
		ReviewableBy:     "ses-reviewer",
		ReviewPolicyMode: "delegated",
	})
	if err != nil {
		t.Fatalf("delegated list after impl history: %v", err)
	}
	if len(delegated) != 0 {
		t.Fatalf("delegated reviewable after implementation history = %v, want empty", idsForTest(delegated))
	}
}

func idsForTest(issues []models.Issue) []string {
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	return ids
}
