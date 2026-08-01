package db

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestCreateIssueLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:       "Logged create test",
		Description: "Test description",
		Type:        models.TypeTask,
		Priority:    models.PriorityP1,
	}

	err = database.CreateIssueLogged(issue, "sess-1")
	if err != nil {
		t.Fatalf("CreateIssueLogged failed: %v", err)
	}

	if issue.ID == "" {
		t.Fatal("Issue ID not set")
	}

	// Verify issue was created
	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if got.Title != "Logged create test" {
		t.Errorf("Title mismatch: got %s, want %s", got.Title, "Logged create test")
	}

	// Verify action_log entry
	var actionType, entityType, entityID, newData, previousData string
	err = database.conn.QueryRow(
		`SELECT action_type, entity_type, entity_id, new_data, previous_data FROM action_log WHERE entity_id = ? AND entity_type = 'issue'`,
		issue.ID,
	).Scan(&actionType, &entityType, &entityID, &newData, &previousData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "create" {
		t.Errorf("action_type: got %s, want create", actionType)
	}
	if entityType != "issue" {
		t.Errorf("entity_type: got %s, want issue", entityType)
	}
	if previousData != "" {
		t.Errorf("previous_data should be empty for create, got %s", previousData)
	}

	// Verify NewData contains correct issue data
	var logged models.Issue
	if err := json.Unmarshal([]byte(newData), &logged); err != nil {
		t.Fatalf("Unmarshal new_data: %v", err)
	}
	if logged.Title != "Logged create test" {
		t.Errorf("new_data title: got %s, want %s", logged.Title, "Logged create test")
	}
	if logged.ID != issue.ID {
		t.Errorf("new_data id: got %s, want %s", logged.ID, issue.ID)
	}
}

func TestUpdateIssueLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue first (unlogged)
	issue := &models.Issue{
		Title:       "Before update",
		Description: "Original desc",
		Type:        models.TypeTask,
		Priority:    models.PriorityP2,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Modify and update with logging
	issue.Title = "After update"
	issue.Description = "Updated desc"
	issue.Priority = models.PriorityP0
	err = database.UpdateIssueLogged(issue, "sess-2", models.ActionUpdate)
	if err != nil {
		t.Fatalf("UpdateIssueLogged failed: %v", err)
	}

	// Verify the update applied
	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if got.Title != "After update" {
		t.Errorf("Title: got %s, want After update", got.Title)
	}
	if got.Priority != models.PriorityP0 {
		t.Errorf("Priority: got %s, want P0", got.Priority)
	}

	// Verify action_log entry
	var actionType, previousData, newData string
	err = database.conn.QueryRow(
		`SELECT action_type, previous_data, new_data FROM action_log WHERE entity_id = ? AND entity_type = 'issue'`,
		issue.ID,
	).Scan(&actionType, &previousData, &newData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "update" {
		t.Errorf("action_type: got %s, want update", actionType)
	}

	// PreviousData should have the old title
	var prev models.Issue
	if err := json.Unmarshal([]byte(previousData), &prev); err != nil {
		t.Fatalf("Unmarshal previous_data: %v", err)
	}
	if prev.Title != "Before update" {
		t.Errorf("previous_data title: got %s, want Before update", prev.Title)
	}
	if prev.Priority != models.PriorityP2 {
		t.Errorf("previous_data priority: got %s, want P2", prev.Priority)
	}

	// NewData should have the new title
	var newIssue models.Issue
	if err := json.Unmarshal([]byte(newData), &newIssue); err != nil {
		t.Fatalf("Unmarshal new_data: %v", err)
	}
	if newIssue.Title != "After update" {
		t.Errorf("new_data title: got %s, want After update", newIssue.Title)
	}
	if newIssue.Priority != models.PriorityP0 {
		t.Errorf("new_data priority: got %s, want P0", newIssue.Priority)
	}
}

func TestUpdateIssueLoggedIfStatusDetectsStaleTransition(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:    "Concurrent review target",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	staleCopy, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	current, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue current failed: %v", err)
	}
	current.Status = models.StatusInProgress
	if err := database.UpdateIssue(current); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	staleCopy.Status = models.StatusInReview
	err = database.UpdateIssueLoggedIfStatus(staleCopy, models.StatusOpen, "sess-reviewer", models.ActionReview)
	if err == nil {
		t.Fatal("expected stale status error")
	}

	var staleErr *StaleIssueStatusError
	if !errors.As(err, &staleErr) {
		t.Fatalf("expected StaleIssueStatusError, got %T", err)
	}
	if staleErr.Expected != models.StatusOpen {
		t.Fatalf("expected stale error expected=%s, got %s", models.StatusOpen, staleErr.Expected)
	}
	if staleErr.Actual != models.StatusInProgress {
		t.Fatalf("expected stale error actual=%s, got %s", models.StatusInProgress, staleErr.Actual)
	}
	t.Log(staleErr.Error())

	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after stale update failed: %v", err)
	}
	if got.Status != models.StatusInProgress {
		t.Fatalf("status = %s, want %s", got.Status, models.StatusInProgress)
	}

	var actions int
	if err := database.conn.QueryRow(`SELECT COUNT(*) FROM action_log WHERE entity_id = ?`, issue.ID).Scan(&actions); err != nil {
		t.Fatalf("count action_log failed: %v", err)
	}
	if actions != 0 {
		t.Fatalf("expected no action log entry for stale transition, got %d", actions)
	}
}

// TestUpdateIssueLoggedRejectsConcurrentWrite reproduces the scenario from
// the td-e38551 report: two sessions load the same issue, one writes first,
// and the second's stale full-row write must not silently revert it.
func TestUpdateIssueLoggedRejectsConcurrentWrite(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:    "Original title",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	staleCopy, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	current, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue current failed: %v", err)
	}
	current.Title = "Updated by session A"
	if err := database.UpdateIssueLogged(current, "sess-a", models.ActionUpdate); err != nil {
		t.Fatalf("UpdateIssueLogged (session A) failed: %v", err)
	}

	// staleCopy still carries the pre-session-A UpdatedAt: this mirrors a
	// sweep or background writer that loaded the issue before session A's
	// write landed, then tries to write its own (unrelated) field change.
	staleCopy.Labels = []string{"swept"}
	err = database.UpdateIssueLogged(staleCopy, "sess-sweep", models.ActionUpdate)
	if err == nil {
		t.Fatal("expected stale update to be rejected")
	}
	var staleErr *StaleIssueUpdateError
	if !errors.As(err, &staleErr) {
		t.Fatalf("expected StaleIssueUpdateError, got %T: %v", err, err)
	}

	// Session A's write must survive: the rejected sweep must not have
	// reverted the title.
	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after rejected update failed: %v", err)
	}
	if got.Title != "Updated by session A" {
		t.Fatalf("title = %q, want %q (session A's write was reverted)", got.Title, "Updated by session A")
	}

	var actions int
	if err := database.conn.QueryRow(`SELECT COUNT(*) FROM action_log WHERE entity_id = ?`, issue.ID).Scan(&actions); err != nil {
		t.Fatalf("count action_log failed: %v", err)
	}
	if actions != 1 {
		t.Fatalf("expected only session A's action logged, got %d", actions)
	}
}

// TestUpdateIssueLoggedAllowsUnloadedSnapshot pins the intentional exemption
// in the staleness guard: an issue that was never loaded from the DB carries a
// zero UpdatedAt and is written unguarded. `td system import --force`
// (cmd/system.go) depends on this — it builds an Issue from parsed markdown
// and overwrites the existing row by ID.
func TestUpdateIssueLoggedAllowsUnloadedSnapshot(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:    "Original title",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// A freshly constructed Issue carrying only an ID: no UpdatedAt, so
	// there is no loaded snapshot for the guard to compare against.
	imported := &models.Issue{
		ID:       issue.ID,
		Title:    "Imported title",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if !imported.UpdatedAt.IsZero() {
		t.Fatal("test premise broken: constructed issue should have a zero UpdatedAt")
	}
	if err := database.UpdateIssueLogged(imported, "sess-import", models.ActionUpdate); err != nil {
		t.Fatalf("UpdateIssueLogged with an unloaded snapshot failed: %v", err)
	}

	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if got.Title != "Imported title" {
		t.Fatalf("title = %q, want %q (unloaded-snapshot write did not apply)", got.Title, "Imported title")
	}
}

// TestUpdateIssueLoggedUnconditionalBypassesStaleGuard verifies undo's write
// path is unaffected by the staleness guard: it must be able to restore an
// older snapshot even though the persisted updated_at has since moved on.
func TestUpdateIssueLoggedUnconditionalBypassesStaleGuard(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:    "Before update",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	preUpdate, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	current, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue current failed: %v", err)
	}
	current.Title = "After update"
	if err := database.UpdateIssueLogged(current, "sess-a", models.ActionUpdate); err != nil {
		t.Fatalf("UpdateIssueLogged failed: %v", err)
	}

	// preUpdate still carries the pre-write UpdatedAt, exactly as undo's
	// PreviousData snapshot would.
	if err := database.UpdateIssueLoggedUnconditional(preUpdate, "sess-undo", models.ActionUpdate); err != nil {
		t.Fatalf("UpdateIssueLoggedUnconditional failed: %v", err)
	}

	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after undo failed: %v", err)
	}
	if got.Title != "Before update" {
		t.Fatalf("title = %q, want %q (undo did not apply)", got.Title, "Before update")
	}
}

func TestDeleteIssueLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:    "To be deleted",
		Type:     models.TypeTask,
		Priority: models.PriorityP3,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	err = database.DeleteIssueLogged(issue.ID, "sess-3")
	if err != nil {
		t.Fatalf("DeleteIssueLogged failed: %v", err)
	}

	// Verify soft delete
	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if got.DeletedAt == nil {
		t.Error("DeletedAt should be set after soft delete")
	}

	// Verify action_log entry
	var actionType, previousData, newData string
	err = database.conn.QueryRow(
		`SELECT action_type, previous_data, new_data FROM action_log WHERE entity_id = ? AND entity_type = 'issue'`,
		issue.ID,
	).Scan(&actionType, &previousData, &newData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "delete" {
		t.Errorf("action_type: got %s, want delete", actionType)
	}
	if newData != "" {
		t.Errorf("new_data should be empty for delete, got %s", newData)
	}

	// PreviousData should have the pre-delete state
	var prev models.Issue
	if err := json.Unmarshal([]byte(previousData), &prev); err != nil {
		t.Fatalf("Unmarshal previous_data: %v", err)
	}
	if prev.Title != "To be deleted" {
		t.Errorf("previous_data title: got %s, want To be deleted", prev.Title)
	}
	if prev.DeletedAt != nil {
		t.Error("previous_data should not have DeletedAt set")
	}
}

func TestUpdateIssueLogged_NotFound(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		ID:    "td-nonexistent",
		Title: "Does not exist",
	}
	err = database.UpdateIssueLogged(issue, "sess-4", models.ActionUpdate)
	if err == nil {
		t.Fatal("Expected error for non-existent issue, got nil")
	}
}

func TestDeleteIssueLogged_NotFound(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	err = database.DeleteIssueLogged("td-nonexistent", "sess-5")
	if err == nil {
		t.Fatal("Expected error for non-existent issue, got nil")
	}
}

func TestUnloggedVariants_NoActionLog(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue with unlogged variant
	issue := &models.Issue{
		Title:    "Unlogged create",
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify no action_log entry for the create
	var count int
	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'issue'`,
		issue.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("CreateIssue (unlogged) created %d action_log entries, want 0", count)
	}

	// Update with unlogged variant
	issue.Title = "Unlogged update"
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Verify still no action_log entries
	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'issue'`,
		issue.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("UpdateIssue (unlogged) created %d action_log entries, want 0", count)
	}
}
