package query

import (
	"os"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	return database
}

func createTestIssue(t *testing.T, database *db.DB, id, title string, status models.Status, typ models.Type, priority models.Priority) *models.Issue {
	t.Helper()
	issue := &models.Issue{
		ID:       id,
		Title:    title,
		Status:   status,
		Type:     typ,
		Priority: priority,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}
	return issue
}

func TestExecute(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create test issues
	createTestIssue(t, database, "td-001", "Fix auth bug", models.StatusOpen, models.TypeBug, models.PriorityP1)
	createTestIssue(t, database, "td-002", "Add login feature", models.StatusOpen, models.TypeFeature, models.PriorityP2)
	createTestIssue(t, database, "td-003", "Closed task", models.StatusClosed, models.TypeTask, models.PriorityP3)
	createTestIssue(t, database, "td-004", "In progress bug", models.StatusInProgress, models.TypeBug, models.PriorityP0)

	tests := []struct {
		name      string
		query     string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "empty query returns all",
			query:     "",
			wantCount: 4,
		},
		{
			name:      "status filter",
			query:     "status = open",
			wantCount: 2,
		},
		{
			name:      "type filter",
			query:     "type = bug",
			wantCount: 2,
		},
		{
			name:      "priority filter",
			query:     "priority = P1",
			wantCount: 1,
		},
		{
			name:      "combined AND filter",
			query:     "status = open AND type = bug",
			wantCount: 1,
		},
		{
			name:      "OR filter",
			query:     "status = open OR status = in_progress",
			wantCount: 3,
		},
		{
			name:      "NOT filter",
			query:     "NOT status = closed",
			wantCount: 3,
		},
		{
			name:      "title contains",
			query:     `title ~ "auth"`,
			wantCount: 1,
		},
		{
			name:      "is function",
			query:     "is(open)",
			wantCount: 2,
		},
		{
			name:      "invalid query",
			query:     "status = ",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := Execute(database, tt.query, "ses_test", ExecuteOptions{})
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(results) != tt.wantCount {
				t.Errorf("Execute() returned %d results, want %d", len(results), tt.wantCount)
			}
		})
	}
}

func TestExecuteWithLimit(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create several test issues
	for i := 0; i < 10; i++ {
		createTestIssue(t, database, "td-"+string(rune('A'+i)), "Issue", models.StatusOpen, models.TypeTask, models.PriorityP2)
	}

	results, err := Execute(database, "status = open", "ses_test", ExecuteOptions{Limit: 3})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Execute() returned %d results, want 3", len(results))
	}
}

func TestExecuteWithMaxResults(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create test issues
	for i := 0; i < 5; i++ {
		createTestIssue(t, database, "td-"+string(rune('A'+i)), "Issue", models.StatusOpen, models.TypeTask, models.PriorityP2)
	}

	// MaxResults should cap what's fetched from DB
	results, err := Execute(database, "status = open", "ses_test", ExecuteOptions{MaxResults: 3})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	// Should get at most 3 due to MaxResults limit
	if len(results) > 3 {
		t.Errorf("Execute() returned %d results, want at most 3", len(results))
	}
}

func TestExecuteParentChild(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create parent issue
	parent := &models.Issue{
		ID:       "td-epic",
		Title:    "Epic Task",
		Status:   models.StatusOpen,
		Type:     models.TypeEpic,
		Priority: models.PriorityP1,
	}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("failed to create parent: %v", err)
	}

	// Create child issues
	child1 := &models.Issue{
		ID:       "td-child1",
		Title:    "Child 1",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
		ParentID: "td-epic",
	}
	if err := database.CreateIssue(child1); err != nil {
		t.Fatalf("failed to create child1: %v", err)
	}

	child2 := &models.Issue{
		ID:       "td-child2",
		Title:    "Child 2",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
		ParentID: "td-epic",
	}
	if err := database.CreateIssue(child2); err != nil {
		t.Fatalf("failed to create child2: %v", err)
	}

	// Test child_of function
	results, err := Execute(database, "child_of(td-epic)", "ses_test", ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(results) != 2 {
		t.Errorf("child_of() returned %d results, want 2", len(results))
	}
}

func TestExecuteDescendantOf(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create a hierarchy: epic -> task -> subtask
	epic := &models.Issue{Title: "Epic", Status: models.StatusOpen, Type: models.TypeEpic, Priority: models.PriorityP1}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("failed to create epic: %v", err)
	}

	task := &models.Issue{Title: "Task", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2, ParentID: epic.ID}
	if err := database.CreateIssue(task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	subtask := &models.Issue{Title: "Subtask", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP3, ParentID: task.ID}
	if err := database.CreateIssue(subtask); err != nil {
		t.Fatalf("failed to create subtask: %v", err)
	}

	unrelated := &models.Issue{Title: "Unrelated", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(unrelated); err != nil {
		t.Fatalf("failed to create unrelated: %v", err)
	}

	// Test descendant_of - should find task and subtask (both descend from epic)
	query := "descendant_of(" + epic.ID + ")"
	results, err := Execute(database, query, "ses_test", ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(results) != 2 {
		t.Errorf("descendant_of(%s) returned %d results, want 2 (task and subtask)", epic.ID, len(results))
	}

	// Verify correct issues found
	foundTask := false
	foundSubtask := false
	for _, r := range results {
		if r.ID == task.ID {
			foundTask = true
		}
		if r.ID == subtask.ID {
			foundSubtask = true
		}
	}
	if !foundTask || !foundSubtask {
		t.Errorf("descendant_of didn't find expected issues: foundTask=%v, foundSubtask=%v", foundTask, foundSubtask)
	}
}

func TestExecuteWithLogs(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create test issue
	issue := createTestIssue(t, database, "", "Bug fix", models.StatusOpen, models.TypeBug, models.PriorityP1)

	// Add a log entry
	logEntry := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   "Fixed the authentication bug",
		Type:      models.LogTypeProgress,
	}
	if err := database.AddLog(logEntry); err != nil {
		t.Fatalf("failed to add log: %v", err)
	}

	// Create another issue without matching log
	createTestIssue(t, database, "", "Other task", models.StatusOpen, models.TypeTask, models.PriorityP2)

	// Search for issues with log containing "authentication"
	results, err := Execute(database, `log.message ~ "authentication"`, "ses_test", ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Execute() returned %d results, want 1", len(results))
	}
	if len(results) > 0 && results[0].ID != issue.ID {
		t.Errorf("Expected %s, got %s", issue.ID, results[0].ID)
	}
}

func TestQuickSearch(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	issue1 := createTestIssue(t, database, "", "Fix authentication bug", models.StatusOpen, models.TypeBug, models.PriorityP1)
	createTestIssue(t, database, "", "Add login feature", models.StatusOpen, models.TypeFeature, models.PriorityP2)
	createTestIssue(t, database, "", "Update readme", models.StatusClosed, models.TypeTask, models.PriorityP3)

	t.Run("search by title word", func(t *testing.T) {
		results, err := QuickSearch(database, "auth", "ses_test", 10)
		if err != nil {
			t.Fatalf("QuickSearch() error = %v", err)
		}
		if len(results) != 1 {
			t.Errorf("QuickSearch() returned %d results, want 1", len(results))
		}
	})

	t.Run("search by ID", func(t *testing.T) {
		results, err := QuickSearch(database, issue1.ID, "ses_test", 10)
		if err != nil {
			t.Fatalf("QuickSearch() error = %v", err)
		}
		if len(results) != 1 {
			t.Errorf("QuickSearch() returned %d results, want 1", len(results))
		}
	})

	t.Run("no results", func(t *testing.T) {
		results, err := QuickSearch(database, "nonexistent", "ses_test", 10)
		if err != nil {
			t.Fatalf("QuickSearch() error = %v", err)
		}
		if len(results) != 0 {
			t.Errorf("QuickSearch() returned %d results, want 0", len(results))
		}
	})
}

func TestReworkFunction(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create test issues: all in_progress
	issue1 := createTestIssue(t, database, "td-rework1", "Rejected no resubmit", models.StatusInProgress, models.TypeTask, models.PriorityP2)
	issue2 := createTestIssue(t, database, "td-rework2", "Rejected then resubmitted", models.StatusInProgress, models.TypeTask, models.PriorityP2)
	createTestIssue(t, database, "td-rework3", "Never rejected", models.StatusInProgress, models.TypeTask, models.PriorityP2)
	createTestIssue(t, database, "td-rework4", "Rejected but closed", models.StatusClosed, models.TypeTask, models.PriorityP2)

	// issue1: rejected, no subsequent review (should be detected by rework())
	database.LogAction(&models.ActionLog{
		SessionID:  "ses_reviewer",
		ActionType: models.ActionReject,
		EntityType: "issue",
		EntityID:   issue1.ID,
	})

	// issue2: rejected, then re-submitted (should NOT be detected)
	database.LogAction(&models.ActionLog{
		SessionID:  "ses_reviewer",
		ActionType: models.ActionReject,
		EntityType: "issue",
		EntityID:   issue2.ID,
	})
	database.LogAction(&models.ActionLog{
		SessionID:  "ses_implementer",
		ActionType: models.ActionReview,
		EntityType: "issue",
		EntityID:   issue2.ID,
	})

	// issue3: never rejected (should NOT be detected)
	// issue4: rejected but closed status (should NOT be detected)
	database.LogAction(&models.ActionLog{
		SessionID:  "ses_reviewer",
		ActionType: models.ActionReject,
		EntityType: "issue",
		EntityID:   "td-rework4",
	})

	t.Run("rework() returns rejected in_progress issues", func(t *testing.T) {
		results, err := Execute(database, "rework()", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Execute() returned %d results, want 1", len(results))
		}
		if len(results) > 0 && results[0].ID != issue1.ID {
			t.Errorf("Expected %s, got %s", issue1.ID, results[0].ID)
		}
	})

	t.Run("rework() combined with other conditions", func(t *testing.T) {
		results, err := Execute(database, "rework() AND status = in_progress", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Execute() returned %d results, want 1", len(results))
		}
	})
}

func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()
	os.Exit(code)
}
