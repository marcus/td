package monitor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

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
	defer func() { _ = database.Close() }()

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

	// Confirm the self-review with the reason required by CLI/API parity.
	_ = m2.SelfReviewConfirmModal.Render(100, 40, m2.SelfReviewConfirmMouseHandler)
	_, _ = m2.SelfReviewConfirmModal.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	_ = m2.SelfReviewConfirmModal.Render(100, 40, m2.SelfReviewConfirmMouseHandler)
	for _, r := range "reviewed the implementation" {
		_, _ = m2.SelfReviewConfirmModal.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
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
	defer func() { _ = database.Close() }()

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

// TestSelfReviewModal_TypedAttributionReachesTheReview drives the real key
// path — open the prompt, type a name, press Enter — and asserts the typed
// attribution lands on the recorded review row.
//
// This is a regression test for a silent-data-loss bug found in review.
// openSelfReviewConfirmModal takes its receiver BY VALUE, and the modal builder
// captures &m.SelfReviewConfirmInput on that local copy. Keystrokes reached the
// captured pointer (so the modal rendered the typed text correctly), while
// executeSelfReviewApprove read the live model's field, which was always empty.
// The operator saw the name they typed and got an unattributed self-review —
// exactly the false record this feature exists to prevent.
//
// Asserting on the stored row rather than on any model field is deliberate:
// the bug was that two views of "the input" disagreed, so only the persisted
// outcome is trustworthy evidence.
func TestSelfReviewModal_TypedAttributionReachesTheReview(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "trusted")
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{Title: "Attribution target", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create: %v", err)
	}
	issue.ImplementerSession = "ses-impl"
	_ = database.UpdateIssue(issue)
	_ = database.RecordSessionAction(issue.ID, "ses-impl", models.ActionSessionStarted)

	m := Model{
		Keymap:       newTestKeymap(),
		DB:           database,
		SessionID:    "ses-impl",
		BaseDir:      baseDir,
		Width:        100,
		Height:       40,
		ActivePanel:  PanelTaskList,
		SelectedID:   map[Panel]string{PanelTaskList: issue.ID},
		Cursor:       map[Panel]int{PanelTaskList: 0},
		ScrollOffset: map[Panel]int{},
		TaskListRows: []TaskListRow{{Issue: *issue, Category: CategoryPendingReview}},
	}

	// Approving own work in trusted mode opens the attribution prompt.
	updated, _ := m.approveIssue()
	m = updated.(Model)
	if !m.SelfReviewConfirmOpen {
		t.Fatal("expected the attribution prompt to open for own implementation")
	}

	// The runtime renders once after opening and before accepting input. That
	// first render must discover and focus the input in the same pass.
	_ = m.SelfReviewConfirmModal.Render(m.Width, m.Height, m.SelfReviewConfirmMouseHandler)
	for _, r := range "bob" {
		next, _ := m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(Model)
		_ = m.SelfReviewConfirmModal.Render(m.Width, m.Height, m.SelfReviewConfirmMouseHandler)
	}
	next, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	final, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if final.Status != models.StatusClosed {
		t.Fatalf("status=%v want closed", final.Status)
	}

	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil {
		t.Fatalf("expected an approval review row, got %v (err=%v)", active, err)
	}
	if active.ReviewedBy != "bob" {
		t.Errorf("ReviewedBy = %q, want %q — the typed attribution was dropped", active.ReviewedBy, "bob")
	}
	if !active.SelfReview {
		t.Error("row recorded by the implementer must still be stamped self_review")
	}
}

// TestSelfReviewModal_SelfReviewRequiresAndPersistsReason is the other half:
// leaving attribution blank means "I reviewed it myself", which requires a
// substantive reason just like the CLI and API.
func TestSelfReviewModal_SelfReviewRequiresAndPersistsReason(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "trusted")
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{Title: "Self-review target", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create: %v", err)
	}
	issue.ImplementerSession = "ses-impl"
	_ = database.UpdateIssue(issue)
	_ = database.RecordSessionAction(issue.ID, "ses-impl", models.ActionSessionStarted)

	m := Model{
		Keymap:       newTestKeymap(),
		DB:           database,
		SessionID:    "ses-impl",
		BaseDir:      baseDir,
		Width:        100,
		Height:       40,
		ActivePanel:  PanelTaskList,
		SelectedID:   map[Panel]string{PanelTaskList: issue.ID},
		Cursor:       map[Panel]int{PanelTaskList: 0},
		ScrollOffset: map[Panel]int{},
		TaskListRows: []TaskListRow{{Issue: *issue, Category: CategoryPendingReview}},
	}

	updated, _ := m.approveIssue()
	m = updated.(Model)
	if !m.SelfReviewConfirmOpen {
		t.Fatal("expected the attribution prompt to open")
	}

	_ = m.SelfReviewConfirmModal.Render(m.Width, m.Height, m.SelfReviewConfirmMouseHandler)
	next, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if !m.SelfReviewConfirmOpen {
		t.Fatal("blank self-review must keep the modal open")
	}
	if active, err := database.GetActiveApprovalReview(issue.ID); err != nil || active != nil {
		t.Fatalf("blank self-review wrote a row: active=%v err=%v", active, err)
	}

	next, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(Model)
	_ = m.SelfReviewConfirmModal.Render(m.Width, m.Height, m.SelfReviewConfirmMouseHandler)
	for _, r := range "reviewed my diff" {
		next, _ = m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(Model)
		_ = m.SelfReviewConfirmModal.Render(m.Width, m.Height, m.SelfReviewConfirmMouseHandler)
	}
	next, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil {
		t.Fatalf("expected an approval review row, got %v (err=%v)", active, err)
	}
	if active.ReviewedBy != "" {
		t.Errorf("ReviewedBy = %q, want empty for an unattributed self-review", active.ReviewedBy)
	}
	if !active.SelfReview {
		t.Error("expected self_review to be stamped")
	}
	if active.Summary != "reviewed my diff" {
		t.Errorf("Summary = %q, want reviewed my diff", active.Summary)
	}
}
