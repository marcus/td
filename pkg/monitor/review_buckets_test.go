package monitor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

// TestCategorizeInReviewIssue_DelegatedBuckets verifies the Step-3 split
// between the four buckets under delegated and trusted. The Test drives
// categorizeInReviewIssue directly (no DB) so the classification logic can be
// exercised without seeding issue_reviews.
func TestCategorizeInReviewIssue_DelegatedBuckets(t *testing.T) {
	baseIssue := &models.Issue{
		ID:                 "td-x",
		Status:             models.StatusInReview,
		ImplementerSession: "ses-impl",
		CreatorSession:     "ses-creator",
	}

	tests := []struct {
		name                 string
		sessionID            string
		hasImplHistory       bool
		wasAnyInvolved       bool
		hasActiveApproval    bool
		reviewerSession      string
		reviewRequestSession string
		minor                bool
		mode                 reviewpolicy.Mode
		want                 TaskListCategory
	}{
		{
			name:      "delegated: uninvolved → reviewable",
			sessionID: "ses-reviewer", mode: reviewpolicy.ModeDelegated,
			want: CategoryReviewable,
		},
		{
			name:      "delegated: implementer → pending_review",
			sessionID: "ses-impl", mode: reviewpolicy.ModeDelegated,
			hasImplHistory: true, wasAnyInvolved: true,
			want: CategoryPendingReview,
		},
		{
			name:      "delegated: has active approval + session is review requester → ready to close",
			sessionID: "ses-requester", mode: reviewpolicy.ModeDelegated,
			hasActiveApproval:    true,
			reviewRequestSession: "ses-requester",
			want:                 CategoryReadyToClose,
		},
		{
			name:      "delegated: has active approval + unrelated session → ready to close",
			sessionID: "ses-bystander", mode: reviewpolicy.ModeDelegated,
			hasActiveApproval: true,
			want:              CategoryReadyToClose,
		},
		// Trusted (the default) joined the four-bucket split when --record-only
		// was opened to it. Every trusted row below must match its delegated
		// twin: the mode is threaded into the reviewpolicy inputs, so any
		// divergence between these pairs means the threading regressed.
		{
			name:      "trusted: uninvolved → reviewable",
			sessionID: "ses-reviewer", mode: reviewpolicy.ModeTrusted,
			want: CategoryReviewable,
		},
		{
			name:      "trusted: implementer, no approval → pending_review",
			sessionID: "ses-impl", mode: reviewpolicy.ModeTrusted,
			hasImplHistory: true, wasAnyInvolved: true,
			want: CategoryPendingReview,
		},
		{
			name:      "trusted: implementer + active approval → ready to close",
			sessionID: "ses-impl", mode: reviewpolicy.ModeTrusted,
			hasImplHistory: true, wasAnyInvolved: true, hasActiveApproval: true,
			want: CategoryReadyToClose,
		},
		{
			name:      "trusted: bystander + active approval → ready to close",
			sessionID: "ses-bystander", mode: reviewpolicy.ModeTrusted,
			hasActiveApproval: true,
			want:              CategoryReadyToClose,
		},
		{
			// Implementation history without being the implementer-of-record
			// still can't review under trusted without --self-review.
			name:      "trusted: impl history, not implementer-of-record → pending_review",
			sessionID: "ses-helper", mode: reviewpolicy.ModeTrusted,
			hasImplHistory: true, wasAnyInvolved: true,
			want: CategoryPendingReview,
		},
		{
			// Minor bypasses review in every mode, so even the implementer is
			// an eligible reviewer.
			name:      "trusted: minor + implementer → reviewable",
			sessionID: "ses-impl", mode: reviewpolicy.ModeTrusted,
			hasImplHistory: true, wasAnyInvolved: true, minor: true,
			want: CategoryReviewable,
		},
		{
			name:      "strict: uninvolved → reviewable (legacy behavior)",
			sessionID: "ses-reviewer", mode: reviewpolicy.ModeStrict,
			want: CategoryReviewable,
		},
		{
			name:      "strict: implementer → pending_review (legacy behavior)",
			sessionID: "ses-impl", mode: reviewpolicy.ModeStrict,
			hasImplHistory: true, wasAnyInvolved: true,
			want: CategoryPendingReview,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			issue := *baseIssue
			issue.ReviewerSession = tc.reviewerSession
			issue.ReviewRequestedBySession = tc.reviewRequestSession
			issue.Minor = tc.minor
			got := categorizeInReviewIssue(&issue, tc.sessionID, tc.mode,
				tc.hasImplHistory, tc.wasAnyInvolved, tc.hasActiveApproval)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestApproveIssueDirectReviewerClose verifies the monitor's approve action
// records a review row and stamps reviewer+closed-by for the direct Mode-A
// path.
func TestApproveIssueDirectReviewerClose(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer database.Close()

	// Seed an in_review issue implemented by someone else.
	issue := &models.Issue{
		Title:  "Review target",
		Type:   models.TypeTask,
		Status: models.StatusInReview,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create: %v", err)
	}
	issue.ImplementerSession = "ses-impl"
	_ = database.UpdateIssue(issue)
	_ = database.RecordSessionAction(issue.ID, "ses-impl", models.ActionSessionStarted)

	m := Model{
		DB:           database,
		SessionID:    "ses-reviewer",
		BaseDir:      baseDir,
		ActivePanel:  PanelTaskList,
		SelectedID:   map[Panel]string{PanelTaskList: issue.ID},
		Cursor:       map[Panel]int{PanelTaskList: 0},
		ScrollOffset: map[Panel]int{},
		TaskListRows: []TaskListRow{{Issue: *issue, Category: CategoryReviewable}},
	}

	_, _ = m.approveIssue()

	final, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if final.Status != models.StatusClosed {
		t.Fatalf("status=%v want closed", final.Status)
	}
	if final.ClosedBySession != "ses-reviewer" || final.ReviewerSession != "ses-reviewer" {
		t.Fatalf("expected reviewer+closed-by stamped, got %+v", final)
	}
	reviews, _ := database.ListIssueReviews(issue.ID)
	if len(reviews) == 0 {
		t.Fatalf("expected issue_reviews row written for direct reviewer-close")
	}
}

// TestRecordReviewChangesRequestedToggle verifies that the 'c' toggle
// wires through to executeRecordReview so the modal's advertised toggle
// actually does something. Before Fix 2, RecordReviewDecision was
// initialised to "approved" and never mutated, making
// ActionReviewChangesRequested dead code from the TUI.
//
// The test simulates the runtime flow: open the modal, flip the decision
// as the 'c' handler would, submit with a reason, then assert:
//   - an issue_reviews row with decision='changes_requested' exists
//   - the issue's reviewer_session / reviewed_at remain UNSET (changes
//     requested must not masquerade as a real approval)
//   - action_log carries ActionReviewChangesRequested
func TestRecordReviewChangesRequestedToggle(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer database.Close()

	// executeRecordReview itself doesn't gate on mode (the modal-open path
	// does), but turn on delegated mode anyway so any future tightening stays
	// green.
	if err := config.SetFeatureStringFlag(baseDir, features.ReviewPolicyMode, string(reviewpolicy.ModeDelegated)); err != nil {
		t.Fatalf("set delegated: %v", err)
	}

	issue := &models.Issue{Title: "Review target", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create: %v", err)
	}
	issue.ImplementerSession = "ses-impl"
	_ = database.UpdateIssue(issue)
	_ = database.RecordSessionAction(issue.ID, "ses-impl", models.ActionSessionStarted)

	m := Model{DB: database, SessionID: "ses-reviewer", BaseDir: baseDir, Width: 100, Height: 40}
	m = m.openRecordReviewModal(issue.ID, issue.Title)
	m.RecordReviewDecision = reviewpolicy.DecisionChangesRequested
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	for _, r := range "please tighten the error message" {
		_, _ = m.RecordReviewModal.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	_, _ = m.executeRecordReview()

	reviews, err := database.ListIssueReviews(issue.ID)
	if err != nil || len(reviews) != 1 {
		t.Fatalf("expected 1 review row, got %d err=%v", len(reviews), err)
	}
	if reviews[0].Decision != reviewpolicy.DecisionChangesRequested {
		t.Fatalf("decision=%q want changes_requested", reviews[0].Decision)
	}

	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Critical: changes_requested must NOT stamp reviewer_session or
	// reviewed_at. If it did, downstream closers would see a fake approval.
	if got.ReviewerSession != "" {
		t.Errorf("reviewer_session=%q, must remain empty for changes_requested", got.ReviewerSession)
	}
	if got.ReviewedAt != nil {
		t.Errorf("reviewed_at=%v, must remain nil for changes_requested", got.ReviewedAt)
	}

	actions, err := database.GetRecentActionsAll(10)
	if err != nil {
		t.Fatalf("get actions: %v", err)
	}
	foundChangesRequested := false
	for _, a := range actions {
		if a.EntityID == issue.ID && a.ActionType == models.ActionReviewChangesRequested {
			foundChangesRequested = true
			break
		}
	}
	if !foundChangesRequested {
		t.Errorf("expected ActionReviewChangesRequested in action_log for %s", issue.ID)
	}
}

func TestRecordReviewLaterIssueEventFailureLeavesMonitorStateRetryable(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Atomic record review", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().Exec(`
		CREATE TRIGGER fail_issue_action
		BEFORE INSERT ON action_log
		WHEN NEW.entity_type = 'issue' AND NEW.action_type = 'review_changes_requested'
		BEGIN
			SELECT RAISE(ABORT, 'injected issue_reviews action failure');
		END;
	`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	m := Model{
		Keymap:    newTestKeymap(),
		DB:        database,
		SessionID: "ses-second-reviewer",
		BaseDir:   baseDir,
		Width:     100,
		Height:    40,
	}
	m = m.openRecordReviewModal(issue.ID, issue.Title)
	m.RecordReviewDecision = reviewpolicy.DecisionChangesRequested
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	updatedModel, _ := m.Update(tea.PasteMsg{Content: "needs another pass"})
	m = updatedModel.(Model)
	updated, _ := m.executeRecordReview()
	gotModel := updated.(Model)
	if !gotModel.StatusIsError {
		t.Fatal("monitor did not surface review sync failure")
	}

	unchanged, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != models.StatusInReview {
		t.Fatalf("issue status = %s, want in_review", unchanged.Status)
	}
	reviews, err := database.ListIssueReviews(issue.ID)
	if err != nil || len(reviews) != 0 {
		t.Fatalf("reviews changed: count=%d err=%v", len(reviews), err)
	}
	_, _ = database.Conn().Exec(`DROP TRIGGER fail_issue_action`)
	_, _ = gotModel.executeRecordReview()
	reviews, err = database.ListIssueReviews(issue.ID)
	if err != nil || len(reviews) != 1 {
		t.Fatalf("retry record review did not persist: count=%d err=%v", len(reviews), err)
	}
}

func TestApproveCloseLaterIssueEventFailureDoesNotCloseAndCanRetry(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Atomic approve", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().Exec(`
		CREATE TRIGGER fail_issue_action
		BEFORE INSERT ON action_log
		WHEN NEW.entity_type = 'issue' AND NEW.action_type = 'approve'
		BEGIN
			SELECT RAISE(ABORT, 'injected issue_reviews action failure');
		END;
	`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	m := newSelfReviewTestModel(database, baseDir, "ses-reviewer", issue.ID)
	updated, _ := m.executeApproveClose(issue, false)
	gotModel := updated.(Model)
	if !gotModel.StatusIsError {
		t.Fatal("monitor did not surface review sync failure")
	}
	unchanged, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != models.StatusInReview {
		t.Fatalf("issue status = %s, want in_review", unchanged.Status)
	}
	reviews, err := database.ListIssueReviews(issue.ID)
	if err != nil || len(reviews) != 0 {
		t.Fatalf("review row committed without event: count=%d err=%v", len(reviews), err)
	}
	_, _ = database.Conn().Exec(`DROP TRIGGER fail_issue_action`)
	_, _ = gotModel.executeApproveClose(issue, false)
	closed, _ := database.GetIssue(issue.ID)
	if closed.Status != models.StatusClosed {
		t.Fatalf("retry status = %s, want closed", closed.Status)
	}
}

// TestApproveIssueParentCascadeStampsClosedBy is the Step-3 regression guard
// reviewers flagged: approving an epic must stamp closed_by_session on each
// descendant AND write an issue_reviews row tagged approved_by_parent_cascade.
func TestApproveIssueParentCascadeStampsClosedBy(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer database.Close()

	epic := &models.Issue{Title: "Epic", Type: models.TypeEpic, Status: models.StatusInReview}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("create epic: %v", err)
	}
	epic.ImplementerSession = "ses-impl"
	_ = database.UpdateIssue(epic)
	_ = database.RecordSessionAction(epic.ID, "ses-impl", models.ActionSessionStarted)

	var childIDs []string
	for i := 0; i < 3; i++ {
		child := &models.Issue{
			Title:    "Child",
			Type:     models.TypeTask,
			Status:   models.StatusInReview,
			ParentID: epic.ID,
		}
		if err := database.CreateIssue(child); err != nil {
			t.Fatalf("create child: %v", err)
		}
		child.ImplementerSession = "ses-impl"
		_ = database.UpdateIssue(child)
		_ = database.RecordSessionAction(child.ID, "ses-impl", models.ActionSessionStarted)
		childIDs = append(childIDs, child.ID)
	}

	m := Model{
		DB:           database,
		SessionID:    "ses-reviewer",
		BaseDir:      baseDir,
		ActivePanel:  PanelTaskList,
		SelectedID:   map[Panel]string{PanelTaskList: epic.ID},
		Cursor:       map[Panel]int{PanelTaskList: 0},
		ScrollOffset: map[Panel]int{},
		TaskListRows: []TaskListRow{{Issue: *epic, Category: CategoryReviewable}},
	}
	_, _ = m.approveIssue()

	for _, id := range childIDs {
		final, err := database.GetIssue(id)
		if err != nil {
			t.Fatalf("get child %s: %v", id, err)
		}
		if final.Status != models.StatusClosed {
			t.Errorf("child %s status=%v want closed", id, final.Status)
		}
		if final.ClosedBySession != "ses-reviewer" {
			t.Errorf("child %s closed_by_session=%q want ses-reviewer", id, final.ClosedBySession)
		}
		revs, _ := database.ListIssueReviews(id)
		hasCascade := false
		for _, r := range revs {
			if r.Decision == "approved_by_parent_cascade" {
				hasCascade = true
				break
			}
		}
		if !hasCascade {
			t.Errorf("child %s missing approved_by_parent_cascade review row", id)
		}
	}
}

// TestMonitorApproveDecision_Attribution covers the monitor's attribution
// prompt at the decision layer: a typed name lets an implementer approve, an
// empty submit falls through to the self-review acknowledgement, and neither
// path grants anything in a mode that exists to enforce independence.
func TestMonitorApproveDecision_Attribution(t *testing.T) {
	issue := &models.Issue{
		ID:                 "td-mon1",
		Status:             models.StatusInReview,
		ImplementerSession: "ses-impl",
		CreatorSession:     "ses-creator",
	}

	// Typed name -> allowed, stamped as an involved recorder with attribution.
	got := monitorApproveDecision(monitorApproveInputs{
		Mode:                     reviewpolicy.ModeTrusted,
		Issue:                    issue,
		SessionID:                "ses-impl",
		HasImplementationHistory: true,
		WasAnyInvolved:           true,
		AttributedTo:             "code-reviewer sub-agent",
	})
	if !got.Allowed {
		t.Fatalf("attributed approve should be allowed: %s", got.RejectionMessage)
	}
	if !got.SelfReview || got.AttributedTo != "code-reviewer sub-agent" {
		t.Errorf("decision = %+v, want SelfReview with the attribution echoed", got)
	}

	// Empty submit -> the self-review acknowledgement path, which the modal
	// sets instead of AttributedTo.
	got = monitorApproveDecision(monitorApproveInputs{
		Mode:                     reviewpolicy.ModeTrusted,
		Issue:                    issue,
		SessionID:                "ses-impl",
		HasImplementationHistory: true,
		WasAnyInvolved:           true,
		SelfReviewAcknowledged:   true,
	})
	if !got.Allowed || got.AttributedTo != "" {
		t.Errorf("empty submit should be a plain self-review: %+v", got)
	}

	// Neither acknowledgement -> rejected, with the teaching message.
	got = monitorApproveDecision(monitorApproveInputs{
		Mode:                     reviewpolicy.ModeTrusted,
		Issue:                    issue,
		SessionID:                "ses-impl",
		HasImplementationHistory: true,
		WasAnyInvolved:           true,
	})
	if got.Allowed {
		t.Error("implementer with no attestation must not approve from the monitor")
	}

	// Delegated ignores the attribution entirely.
	got = monitorApproveDecision(monitorApproveInputs{
		Mode:                     reviewpolicy.ModeDelegated,
		Issue:                    issue,
		SessionID:                "ses-impl",
		HasImplementationHistory: true,
		WasAnyInvolved:           true,
		AttributedTo:             "code-reviewer sub-agent",
	})
	if got.Allowed {
		t.Error("delegated mode must not let attribution grant the implementer an approval")
	}
}
