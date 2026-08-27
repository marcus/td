package monitor

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// newHeldIssue creates an issue in the given status still carrying an
// implementer claim, via the unlogged writer so the fixture is not itself
// normalized by the shared release.
func newHeldIssue(t *testing.T, database *db.DB, title string, status models.Status, holder string) *models.Issue {
	t.Helper()
	issue := &models.Issue{Title: title, Type: models.TypeTask}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	issue.Status = status
	issue.ImplementerSession = holder
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	return got
}

func assertOpenAndUnclaimed(t *testing.T, database *db.DB, id string) {
	t.Helper()
	got, err := database.GetIssue(id)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if got.Status != models.StatusOpen {
		t.Fatalf("status = %s, want open", got.Status)
	}
	if got.ImplementerSession != "" {
		t.Fatalf("implementer = %q, want empty: open issue still holds a claim", got.ImplementerSession)
	}
}

// TestMonitorReopenReleasesClaim covers the TUI reopen action (td-cdbbbe): it
// cleared reviewer_session and closed_at, mirroring the API handler, but left
// the implementer claim on an issue that is now open.
func TestMonitorReopenReleasesClaim(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := newHeldIssue(t, database, "Closed but still held", models.StatusClosed, "ses_holder")

	m := Model{
		DB:          database,
		BaseDir:     baseDir,
		SessionID:   "ses_monitor",
		ActivePanel: PanelTaskList,
		Cursor:      map[Panel]int{PanelTaskList: 0},
		SelectedID:  map[Panel]string{PanelTaskList: issue.ID},
		TaskListRows: []TaskListRow{
			{Issue: *issue, Category: CategoryClosed},
		},
		ModalStack: []ModalEntry{},
	}

	updated, _ := m.reopenIssue()
	m2 := updated.(Model)
	if m2.StatusIsError {
		t.Fatalf("reopen reported an error: %s", m2.StatusMessage)
	}

	assertOpenAndUnclaimed(t, database, issue.ID)
}

// TestMonitorEditFormStatusOpenReleasesClaim covers the TUI edit form's
// status -> open path (td-cdbbbe), the surface nobody thought of when reopen
// and unblock were each fixed at their own call site.
func TestMonitorEditFormStatusOpenReleasesClaim(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := newHeldIssue(t, database, "In progress and held", models.StatusInProgress, "ses_holder")

	m := Model{
		DB:         database,
		BaseDir:    baseDir,
		SessionID:  "ses_monitor",
		Cursor:     map[Panel]int{},
		SelectedID: map[Panel]string{},
		ModalStack: []ModalEntry{},
		FormOpen:   true,
		FormState:  NewFormStateForEdit(issue),
	}
	m.FormState.Status = string(models.StatusOpen)

	updated, _ := m.submitForm()
	m2 := updated.(Model)
	if m2.Err != nil {
		t.Fatalf("submitForm failed: %v", m2.Err)
	}
	if m2.StatusIsError {
		t.Fatalf("submitForm reported an error: %s", m2.StatusMessage)
	}

	assertOpenAndUnclaimed(t, database, issue.ID)
}
