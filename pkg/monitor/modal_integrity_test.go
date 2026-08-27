package monitor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/atotto/clipboard"
	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

func pasteClipboardWithCtrlV(t *testing.T, m Model, text string) Model {
	t.Helper()
	original, err := clipboard.ReadAll()
	if err != nil {
		t.Skipf("system clipboard unavailable: %v", err)
	}
	if err := clipboard.WriteAll(text); err != nil {
		t.Skipf("system clipboard unavailable: %v", err)
	}
	t.Cleanup(func() { _ = clipboard.WriteAll(original) })

	updated, cmd := m.handleKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("Ctrl+V did not return a clipboard command")
	}

	feed := func(msg tea.Msg) {
		updated, _ = m.Update(msg)
		m = updated.(Model)
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, batchCmd := range batch {
			if batchCmd != nil {
				feed(batchCmd())
			}
		}
	} else {
		feed(msg)
	}
	return m
}

func TestCloseConfirmKeyPathPersistsTypedReason(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{Title: "Close target", Type: models.TypeTask, Status: models.StatusInProgress}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	m := Model{
		Keymap:       newTestKeymap(),
		DB:           database,
		BaseDir:      baseDir,
		SessionID:    "ses-closer",
		Width:        100,
		Height:       40,
		ScrollOffset: map[Panel]int{},
	}
	m = m.openCloseConfirmModal(issue.ID, issue.Title)
	_ = m.CloseConfirmModal.Render(m.Width, m.Height, m.CloseConfirmMouseHandler)

	for _, r := range "duplicate report" {
		next, _ := m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(Model)
		_ = m.CloseConfirmModal.Render(m.Width, m.Height, m.CloseConfirmMouseHandler)
	}
	next, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if got.Status != models.StatusClosed {
		t.Fatalf("status = %s, want closed", got.Status)
	}
	logs, err := database.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("get logs: %v", err)
	}
	found := false
	for _, log := range logs {
		if log.Message == "Closed: duplicate report" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("typed close reason was not persisted; logs = %+v", logs)
	}
}

func TestRecordReviewKeyPathPersistsTypedSummary(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{Title: "Review target", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	m := Model{
		Keymap:       newTestKeymap(),
		DB:           database,
		BaseDir:      baseDir,
		SessionID:    "ses-reviewer",
		Width:        100,
		Height:       40,
		ScrollOffset: map[Panel]int{},
	}
	m = m.openRecordReviewModal(issue.ID, issue.Title)
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)

	for _, r := range "looks fine" {
		next, _ := m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(Model)
		_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	}
	next, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil {
		t.Fatalf("get active review: review=%v err=%v", active, err)
	}
	if active.Summary != "looks fine" {
		t.Fatalf("summary = %q, want looks fine", active.Summary)
	}
}

func TestCloseConfirmCtrlVPastePersistsReason(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{Title: "Paste close", Type: models.TypeTask, Status: models.StatusInProgress}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	m := Model{Keymap: newTestKeymap(), DB: database, BaseDir: baseDir, SessionID: "ses-close", Width: 100, Height: 40}
	m = m.openCloseConfirmModal(issue.ID, issue.Title)
	_ = m.CloseConfirmModal.Render(m.Width, m.Height, m.CloseConfirmMouseHandler)

	m = pasteClipboardWithCtrlV(t, m, "pasted close reason")
	if got := m.CloseConfirmModal.InputValue("reason"); got != "pasted close reason" {
		t.Fatalf("modal reason = %q, want pasted close reason", got)
	}
	updated, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	logs, err := database.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("get logs: %v", err)
	}
	for _, log := range logs {
		if log.Message == "Closed: pasted close reason" {
			return
		}
	}
	t.Fatalf("pasted close reason not persisted: %+v", logs)
}

func TestRecordReviewCtrlVPastePersistsSummary(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{Title: "Paste review", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	m := Model{Keymap: newTestKeymap(), DB: database, BaseDir: baseDir, SessionID: "ses-review", Width: 100, Height: 40}
	m = m.openRecordReviewModal(issue.ID, issue.Title)
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)

	m = pasteClipboardWithCtrlV(t, m, "acceptable pasted review")
	if got := m.RecordReviewModal.InputValue("reason"); got != "acceptable pasted review" {
		t.Fatalf("modal summary = %q, want acceptable pasted review", got)
	}
	updated, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil {
		t.Fatalf("get active review: review=%v err=%v", active, err)
	}
	if active.Summary != "acceptable pasted review" {
		t.Fatalf("summary = %q, want acceptable pasted review", active.Summary)
	}
}

func TestSelfReviewCtrlVPastePersistsReason(t *testing.T) {
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "trusted")
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{Title: "Paste self-review", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	issue.ImplementerSession = "ses-impl"
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("update issue: %v", err)
	}
	if err := database.RecordSessionAction(issue.ID, "ses-impl", models.ActionSessionStarted); err != nil {
		t.Fatalf("record involvement: %v", err)
	}

	m := Model{
		Keymap:       newTestKeymap(),
		DB:           database,
		BaseDir:      baseDir,
		SessionID:    "ses-impl",
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
	_ = m.SelfReviewConfirmModal.Render(m.Width, m.Height, m.SelfReviewConfirmMouseHandler)
	updated, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	_ = m.SelfReviewConfirmModal.Render(m.Width, m.Height, m.SelfReviewConfirmMouseHandler)

	m = pasteClipboardWithCtrlV(t, m, "pasted self-review reason")
	if got := m.SelfReviewConfirmModal.InputValue("reason"); got != "pasted self-review reason" {
		t.Fatalf("modal self-review reason = %q, want pasted self-review reason", got)
	}
	updated, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil {
		t.Fatalf("get active review: review=%v err=%v", active, err)
	}
	if active.Summary != "pasted self-review reason" || !active.SelfReview {
		t.Fatalf("pasted self-review not persisted correctly: %+v", active)
	}
}

func TestRecordReviewTypingCStaysInSummary(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{Title: "Literal c", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	m := Model{Keymap: newTestKeymap(), DB: database, BaseDir: baseDir, SessionID: "ses-review", Width: 100, Height: 40}
	m = m.openRecordReviewModal(issue.ID, issue.Title)
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	for _, r := range "acceptable" {
		updated, _ := m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(Model)
		_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	}
	updated, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil {
		t.Fatalf("get active review: review=%v err=%v", active, err)
	}
	if active.Summary != "acceptable" {
		t.Fatalf("summary = %q, want acceptable", active.Summary)
	}
	if active.Decision != reviewpolicy.DecisionApproved {
		t.Fatalf("decision = %q, want approved", active.Decision)
	}
}

func TestRecordReviewDecisionTogglePreservesModalInputs(t *testing.T) {
	m := Model{Keymap: newTestKeymap(), Width: 100, Height: 40}
	m = m.openRecordReviewModal("td-toggle", "Toggle target")
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	updated, _ := m.Update(tea.PasteMsg{Content: "complete review"})
	m = updated.(Model)

	updated, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	updated, _ = m.Update(tea.PasteMsg{Content: "code reviewer"})
	m = updated.(Model)

	updated, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	updated, _ = m.handleKey(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = updated.(Model)
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)

	if m.RecordReviewDecision != reviewpolicy.DecisionChangesRequested {
		t.Fatalf("decision = %q, want changes_requested", m.RecordReviewDecision)
	}
	if got := m.RecordReviewModal.InputValue("reason"); got != "complete review" {
		t.Fatalf("summary after toggle = %q, want complete review", got)
	}
	if got := m.RecordReviewModal.InputValue("reviewed_by"); got != "code reviewer" {
		t.Fatalf("reviewer after toggle = %q, want code reviewer", got)
	}
	if got := m.RecordReviewModal.FocusedID(); got != "confirm" {
		t.Fatalf("focus after toggle = %q, want confirm", got)
	}
}

func openRecordReviewRacePrompt(t *testing.T, database *db.DB, baseDir string, issue *models.Issue) Model {
	t.Helper()
	m := Model{
		Keymap:       newTestKeymap(),
		DB:           database,
		BaseDir:      baseDir,
		SessionID:    "ses-reviewer",
		Width:        100,
		Height:       40,
		ActivePanel:  PanelTaskList,
		SelectedID:   map[Panel]string{PanelTaskList: issue.ID},
		Cursor:       map[Panel]int{PanelTaskList: 0},
		ScrollOffset: map[Panel]int{},
		TaskListRows: []TaskListRow{{Issue: *issue, Category: CategoryPendingReview}},
	}
	updated, _ := m.recordReviewAction()
	m = updated.(Model)
	if !m.RecordReviewOpen {
		t.Fatal("record-review prompt did not open")
	}
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	_, _ = m.RecordReviewModal.HandleMsg(tea.PasteMsg{Content: "reviewed before submit"})
	return m
}

func TestRecordReviewSubmitRejectsPolicyModeFlip(t *testing.T) {
	baseDir := t.TempDir()
	if err := config.SetFeatureStringFlag(baseDir, features.ReviewPolicyMode, string(reviewpolicy.ModeDelegated)); err != nil {
		t.Fatalf("set delegated mode: %v", err)
	}
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()
	issue := &models.Issue{Title: "Mode flip", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	m := openRecordReviewRacePrompt(t, database, baseDir, issue)

	if err := config.SetFeatureStringFlag(baseDir, features.ReviewPolicyMode, string(reviewpolicy.ModeStrict)); err != nil {
		t.Fatalf("set strict mode: %v", err)
	}
	_, _ = m.executeRecordReview()
	reviews, err := database.ListIssueReviews(issue.ID)
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}
	if len(reviews) != 0 {
		t.Fatalf("strict mode flip still persisted review: %+v", reviews)
	}
}

func TestRecordReviewSubmitRejectsStatusChange(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()
	issue := &models.Issue{Title: "Status change", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	m := openRecordReviewRacePrompt(t, database, baseDir, issue)

	issue.Status = models.StatusInProgress
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("change issue status: %v", err)
	}
	_, _ = m.executeRecordReview()
	reviews, err := database.ListIssueReviews(issue.ID)
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}
	if len(reviews) != 0 {
		t.Fatalf("status change still persisted review: %+v", reviews)
	}
}

func TestRecordReviewSubmitPreservesConcurrentActiveApproval(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()
	issue := &models.Issue{Title: "Concurrent approval", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	m := openRecordReviewRacePrompt(t, database, baseDir, issue)

	reviewID, err := database.CreateIssueReview(db.NewReview{
		IssueID:         issue.ID,
		ReviewerSession: "ses-other-reviewer",
		Decision:        reviewpolicy.DecisionApproved,
		Summary:         "concurrent approval",
	})
	if err != nil {
		t.Fatalf("create concurrent approval: %v", err)
	}
	_, _ = m.executeRecordReview()

	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil {
		t.Fatalf("get active approval: review=%v err=%v", active, err)
	}
	if active.ID != reviewID || active.Summary != "concurrent approval" {
		t.Fatalf("concurrent approval was superseded: %+v", active)
	}
	reviews, err := database.ListIssueReviews(issue.ID)
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}
	if len(reviews) != 1 {
		t.Fatalf("review count = %d, want 1: %+v", len(reviews), reviews)
	}
}

func TestMonitorApprovalSecurityEvents(t *testing.T) {
	tests := []struct {
		name         string
		selfReview   bool
		attributedTo string
		reason       string
		wantReason   string
	}{
		{
			name:       "involved self-review",
			selfReview: true,
			reason:     "reviewed my diff",
			wantReason: "self_review: reviewed my diff",
		},
		{
			name:         "involved attributed review",
			selfReview:   true,
			attributedTo: "code-reviewer sub-agent",
			reason:       "reviewer found no issues",
			wantReason:   "attributed_review by code-reviewer sub-agent: reviewer found no issues",
		},
		{
			name:         "independent attributed review",
			selfReview:   false,
			attributedTo: "code-reviewer sub-agent",
			reason:       "reviewer found no issues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			database, err := db.Initialize(baseDir)
			if err != nil {
				t.Fatalf("db init: %v", err)
			}
			defer func() { _ = database.Close() }()

			issue := &models.Issue{Title: tt.name, Type: models.TypeTask, Status: models.StatusInReview}
			if err := database.CreateIssue(issue); err != nil {
				t.Fatalf("create issue: %v", err)
			}
			m := Model{DB: database, BaseDir: baseDir, SessionID: "ses-recorder", SelectedID: map[Panel]string{}}
			_, _ = m.executeApproveCloseAttributed(issue, tt.selfReview, tt.attributedTo, tt.reason)

			events, err := db.ReadSecurityEvents(baseDir)
			if err != nil {
				t.Fatalf("read security events: %v", err)
			}
			if tt.wantReason == "" {
				if len(events) != 0 {
					t.Fatalf("independent approval wrote security events: %+v", events)
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("security event count = %d, want 1: %+v", len(events), events)
			}
			if events[0].IssueID != issue.ID || events[0].SessionID != m.SessionID {
				t.Fatalf("security event identity = %+v", events[0])
			}
			if events[0].Reason != tt.wantReason {
				t.Fatalf("security event reason = %q, want %q", events[0].Reason, tt.wantReason)
			}
		})
	}
}

func TestRecordReviewSecurityEventUsesRecordOnlyShape(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{Title: "Attributed record-only", Type: models.TypeTask, Status: models.StatusInReview}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "trusted")
	issue.ImplementerSession = "ses-impl"
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("update issue: %v", err)
	}
	if err := database.RecordSessionAction(issue.ID, "ses-impl", models.ActionSessionStarted); err != nil {
		t.Fatalf("record involvement: %v", err)
	}

	m := Model{
		DB:          database,
		BaseDir:     baseDir,
		SessionID:   "ses-impl",
		Width:       100,
		Height:      40,
		ActivePanel: PanelTaskList,
		SelectedID:  map[Panel]string{PanelTaskList: issue.ID},
		TaskListRows: []TaskListRow{
			{Issue: *issue, Category: CategoryPendingReview},
		},
	}
	opened, _ := m.recordReviewAction()
	m = opened.(Model)
	if !m.RecordReviewOpen {
		t.Fatal("involved recorder could not reach the attribution prompt")
	}
	m.RecordReviewDecision = reviewpolicy.DecisionApproved
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	for _, r := range "verified fix" {
		_, _ = m.RecordReviewModal.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	_, _ = m.RecordReviewModal.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	_ = m.RecordReviewModal.Render(m.Width, m.Height, m.RecordReviewMouseHandler)
	for _, r := range "reviewer-2" {
		_, _ = m.RecordReviewModal.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	_, _ = m.executeRecordReview()
	events, err := db.ReadSecurityEvents(baseDir)
	if err != nil {
		t.Fatalf("read security events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("security event count = %d, want 1: %+v", len(events), events)
	}
	want := "record_only attributed_review by reviewer-2: verified fix"
	if events[0].Reason != want {
		t.Fatalf("security event reason = %q, want %q", events[0].Reason, want)
	}
}
