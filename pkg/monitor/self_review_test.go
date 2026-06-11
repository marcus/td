package monitor

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

// newSelfReviewTestModel builds a minimal Model wired to a real DB, focused on
// the task-list panel with a single selected issue, for exercising the
// trusted-mode self-review approve flow.
func newSelfReviewTestModel(database *db.DB, baseDir, sessionID, issueID string) Model {
	m := Model{
		DB:          database,
		BaseDir:     baseDir,
		SessionID:   sessionID,
		ActivePanel: PanelTaskList,
		Cursor:      map[Panel]int{PanelTaskList: 0},
		SelectedID:  map[Panel]string{},
		TaskListRows: []TaskListRow{
			{Issue: models.Issue{ID: issueID}, Category: CategoryPendingReview},
		},
		ModalStack: []ModalEntry{},
	}
	m.SelectedID[PanelTaskList] = issueID
	return m
}

// TestTrustedSelfReviewApproveOpensConfirmModal asserts that, in trusted mode,
// an implementer approving the in_review issue they implemented does NOT close
// the issue immediately — instead the self-review confirm modal opens. On
// confirm, the approval is recorded with self_review=true and the issue closes.
func TestTrustedSelfReviewApproveOpensConfirmModal(t *testing.T) {
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "trusted")
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer database.Close()

	const session = "ses-self"

	// Create an in_review issue implemented by the current session, so the
	// session has implementation history and is the implementer-of-record.
	issue := &models.Issue{Title: "self-reviewed", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	issue.ImplementerSession = session
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("update issue: %v", err)
	}
	// Record an implementation action so WasSessionImplementationInvolved is true.
	_ = database.RecordSessionAction(issue.ID, session, models.ActionSessionStarted)

	m := newSelfReviewTestModel(database, baseDir, session, issue.ID)

	// Approve: should open the self-review confirm modal, NOT close.
	updated, _ := m.approveIssue()
	m2 := updated.(Model)
	if !m2.SelfReviewConfirmOpen {
		t.Fatalf("expected self-review confirm modal to open for implementer in trusted mode")
	}
	if m2.SelfReviewConfirmIssueID != issue.ID {
		t.Fatalf("confirm modal issue = %q, want %q", m2.SelfReviewConfirmIssueID, issue.ID)
	}
	// Issue must still be in_review (not closed yet).
	got, _ := database.GetIssue(issue.ID)
	if got.Status != models.StatusInReview {
		t.Fatalf("issue closed before confirm; status = %v", got.Status)
	}

	// Confirm the self-review.
	confirmed, _ := m2.executeSelfReviewApprove()
	m3 := confirmed.(Model)
	if m3.SelfReviewConfirmOpen {
		t.Fatalf("confirm modal should be closed after confirming")
	}

	// Issue should now be closed.
	got, _ = database.GetIssue(issue.ID)
	if got.Status != models.StatusClosed {
		t.Fatalf("issue not closed after self-review confirm; status = %v", got.Status)
	}

	// The recorded issue_reviews row must carry the self_review audit bit.
	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil {
		t.Fatalf("get active approval: %v", err)
	}
	if active == nil {
		t.Fatalf("expected a recorded approval review")
	}
	if !active.SelfReview {
		t.Fatalf("recorded approval should have self_review=true")
	}
	if active.Decision != reviewpolicy.DecisionApproved {
		t.Fatalf("recorded review decision = %q, want approved", active.Decision)
	}
}

// TestTrustedNonImplementerApproveSkipsSelfReviewPrompt asserts that a
// non-implementer (independent) session approving in trusted mode proceeds
// without the self-review prompt and records self_review=false, matching
// delegated behavior.
func TestTrustedNonImplementerApproveSkipsSelfReviewPrompt(t *testing.T) {
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "trusted")
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer database.Close()

	// Issue implemented by someone else; the approver is a fresh, independent
	// session with no implementation history.
	issue := &models.Issue{Title: "reviewed by other", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	issue.ImplementerSession = "ses-impl"
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("update issue: %v", err)
	}
	_ = database.RecordSessionAction(issue.ID, "ses-impl", models.ActionSessionStarted)

	m := newSelfReviewTestModel(database, baseDir, "ses-fresh", issue.ID)

	updated, _ := m.approveIssue()
	m2 := updated.(Model)
	if m2.SelfReviewConfirmOpen {
		t.Fatalf("non-implementer must not trigger the self-review prompt")
	}

	got, _ := database.GetIssue(issue.ID)
	if got.Status != models.StatusClosed {
		t.Fatalf("non-implementer approve should close immediately; status = %v", got.Status)
	}

	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil {
		t.Fatalf("get active approval: %v", err)
	}
	if active == nil {
		t.Fatalf("expected a recorded approval review")
	}
	if active.SelfReview {
		t.Fatalf("non-implementer approval must record self_review=false")
	}
}
