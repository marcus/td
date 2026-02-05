package db

import (
	"encoding/json"
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
