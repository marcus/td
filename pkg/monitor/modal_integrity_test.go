package monitor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

func TestCloseConfirmKeyPathPersistsTypedReason(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	defer database.Close()

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
	defer database.Close()

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
			defer database.Close()

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
	defer database.Close()

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
