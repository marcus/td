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

func TestExecuteEpicByID(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create an epic
	epic := &models.Issue{
		Title:    "Theme Switcher",
		Status:   models.StatusOpen,
		Type:     models.TypeEpic,
		Priority: models.PriorityP1,
	}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("failed to create epic: %v", err)
	}

	// Create tasks under the epic
	task1 := &models.Issue{
		Title:    "Add theme entry type",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(task1); err != nil {
		t.Fatalf("failed to create task1: %v", err)
	}

	task2 := &models.Issue{
		Title:    "Update theme switcher",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(task2); err != nil {
		t.Fatalf("failed to create task2: %v", err)
	}

	// Create a nested subtask (grandchild of epic)
	subtask := &models.Issue{
		Title:    "Subtask under task1",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
		ParentID: task1.ID,
	}
	if err := database.CreateIssue(subtask); err != nil {
		t.Fatalf("failed to create subtask: %v", err)
	}

	// Create an unrelated epic and task
	otherEpic := &models.Issue{
		Title:    "Other Epic",
		Status:   models.StatusOpen,
		Type:     models.TypeEpic,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(otherEpic); err != nil {
		t.Fatalf("failed to create other epic: %v", err)
	}

	unrelatedTask := &models.Issue{
		Title:    "Unrelated task",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
		ParentID: otherEpic.ID,
	}
	if err := database.CreateIssue(unrelatedTask); err != nil {
		t.Fatalf("failed to create unrelated task: %v", err)
	}

	t.Run("epic = id finds direct children and nested descendants", func(t *testing.T) {
		results, err := Execute(database, "epic = "+epic.ID, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// Should find task1, task2 (direct children), and subtask (grandchild via task1)
		if len(results) != 3 {
			t.Errorf("epic = %s returned %d results, want 3", epic.ID, len(results))
			for _, r := range results {
				t.Logf("  got: %s %s", r.ID, r.Title)
			}
		}
		found := map[string]bool{}
		for _, r := range results {
			found[r.ID] = true
		}
		if !found[task1.ID] {
			t.Error("missing task1")
		}
		if !found[task2.ID] {
			t.Error("missing task2")
		}
		if !found[subtask.ID] {
			t.Error("missing subtask (grandchild)")
		}
		if found[epic.ID] {
			t.Error("should not include the epic itself")
		}
		if found[unrelatedTask.ID] {
			t.Error("should not include unrelated task")
		}
	})

	t.Run("epic = id does not match unrelated issues", func(t *testing.T) {
		results, err := Execute(database, "epic = "+otherEpic.ID, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if len(results) != 1 {
			t.Errorf("epic = %s returned %d results, want 1", otherEpic.ID, len(results))
		}
		if len(results) > 0 && results[0].ID != unrelatedTask.ID {
			t.Errorf("expected %s, got %s", unrelatedTask.ID, results[0].ID)
		}
	})
}

func TestExecuteEpicLabels(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create an epic with labels
	epic := &models.Issue{
		Title:    "Parent Epic",
		Status:   models.StatusOpen,
		Type:     models.TypeEpic,
		Priority: models.PriorityP1,
		Labels:   []string{"deferred", "backend"},
	}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("failed to create epic: %v", err)
	}

	// Create a task under the epic
	taskUnderEpic := &models.Issue{
		Title:    "Task under deferred epic",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(taskUnderEpic); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create a standalone task (no epic)
	standaloneTask := &models.Issue{
		Title:    "Standalone task",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(standaloneTask); err != nil {
		t.Fatalf("failed to create standalone task: %v", err)
	}

	// Create another epic without deferred label
	epicNoDeferred := &models.Issue{
		Title:    "Active Epic",
		Status:   models.StatusOpen,
		Type:     models.TypeEpic,
		Priority: models.PriorityP1,
		Labels:   []string{"frontend"},
	}
	if err := database.CreateIssue(epicNoDeferred); err != nil {
		t.Fatalf("failed to create active epic: %v", err)
	}

	// Create task under non-deferred epic
	taskUnderActiveEpic := &models.Issue{
		Title:    "Task under active epic",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
		ParentID: epicNoDeferred.ID,
	}
	if err := database.CreateIssue(taskUnderActiveEpic); err != nil {
		t.Fatalf("failed to create task under active epic: %v", err)
	}

	t.Run("epic.labels matches task under epic with label", func(t *testing.T) {
		results, err := Execute(database, `epic.labels ~ "deferred"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// Should find only taskUnderEpic (the task under the deferred epic)
		if len(results) != 1 {
			t.Errorf("Execute() returned %d results, want 1", len(results))
		}
		if len(results) > 0 && results[0].ID != taskUnderEpic.ID {
			t.Errorf("Expected %s, got %s", taskUnderEpic.ID, results[0].ID)
		}
	})

	t.Run("NOT epic.labels excludes tasks under epic with label", func(t *testing.T) {
		results, err := Execute(database, `NOT epic.labels ~ "deferred"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// Should find: epic, standaloneTask, epicNoDeferred, taskUnderActiveEpic (4 total)
		// Should NOT find: taskUnderEpic
		if len(results) != 4 {
			t.Errorf("Execute() returned %d results, want 4", len(results))
		}
		for _, r := range results {
			if r.ID == taskUnderEpic.ID {
				t.Errorf("NOT epic.labels should not include task under deferred epic")
			}
		}
	})

	t.Run("epic.labels with no matching label", func(t *testing.T) {
		results, err := Execute(database, `epic.labels ~ "nonexistent"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Execute() returned %d results, want 0", len(results))
		}
	})

	t.Run("combined query with epic.labels", func(t *testing.T) {
		results, err := Execute(database, `type = task AND NOT epic.labels ~ "deferred"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// Should find: standaloneTask, taskUnderActiveEpic (2 tasks not under deferred epic)
		if len(results) != 2 {
			t.Errorf("Execute() returned %d results, want 2", len(results))
		}
	})
}

func TestExecuteIsReadyAndHasOpenDeps(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create standalone issue (no dependencies) - should be ready
	standalone := &models.Issue{
		Title:    "Standalone task",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(standalone); err != nil {
		t.Fatalf("failed to create standalone: %v", err)
	}

	// Create a blocker issue (open)
	blocker := &models.Issue{
		Title:    "Blocker task",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
	}
	if err := database.CreateIssue(blocker); err != nil {
		t.Fatalf("failed to create blocker: %v", err)
	}

	// Create issue that depends on blocker (has open deps)
	blockedIssue := &models.Issue{
		Title:    "Blocked task",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(blockedIssue); err != nil {
		t.Fatalf("failed to create blocked issue: %v", err)
	}

	// Add dependency: blockedIssue depends on blocker
	if err := database.AddDependency(blockedIssue.ID, blocker.ID, "depends_on"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// Create closed blocker
	closedBlocker := &models.Issue{
		Title:    "Closed blocker",
		Status:   models.StatusClosed,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(closedBlocker); err != nil {
		t.Fatalf("failed to create closed blocker: %v", err)
	}

	// Create issue with only closed dependencies (should be ready)
	issueWithClosedDeps := &models.Issue{
		Title:    "Task with closed deps",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}
	if err := database.CreateIssue(issueWithClosedDeps); err != nil {
		t.Fatalf("failed to create issue with closed deps: %v", err)
	}
	if err := database.AddDependency(issueWithClosedDeps.ID, closedBlocker.ID, "depends_on"); err != nil {
		t.Fatalf("failed to add closed dependency: %v", err)
	}

	t.Run("is_ready() returns issues with no open deps", func(t *testing.T) {
		results, err := Execute(database, "is_ready()", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// Should find: standalone, blocker, closedBlocker, issueWithClosedDeps (4 total)
		// Should NOT find: blockedIssue (has open dep)
		if len(results) != 4 {
			t.Errorf("is_ready() returned %d results, want 4", len(results))
		}
		for _, r := range results {
			if r.ID == blockedIssue.ID {
				t.Errorf("is_ready() should not include blocked issue")
			}
		}
	})

	t.Run("has_open_deps() returns issues with open deps", func(t *testing.T) {
		results, err := Execute(database, "has_open_deps()", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// Should find only blockedIssue
		if len(results) != 1 {
			t.Errorf("has_open_deps() returned %d results, want 1", len(results))
		}
		if len(results) > 0 && results[0].ID != blockedIssue.ID {
			t.Errorf("Expected %s, got %s", blockedIssue.ID, results[0].ID)
		}
	})

	t.Run("NOT is_ready() equals has_open_deps()", func(t *testing.T) {
		notReadyResults, err := Execute(database, "NOT is_ready()", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		openDepsResults, err := Execute(database, "has_open_deps()", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if len(notReadyResults) != len(openDepsResults) {
			t.Errorf("NOT is_ready() returned %d, has_open_deps() returned %d, should be equal",
				len(notReadyResults), len(openDepsResults))
		}
	})

	t.Run("combined with status filter", func(t *testing.T) {
		results, err := Execute(database, "status = open AND is_ready()", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// Should find: standalone, blocker, issueWithClosedDeps (3 open + ready)
		// NOT: closedBlocker (closed), blockedIssue (has open deps)
		if len(results) != 3 {
			t.Errorf("combined query returned %d results, want 3", len(results))
		}
	})
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

func TestExecuteEpicOR(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Create two epics with children
	epicA := &models.Issue{Title: "Epic A", Status: models.StatusOpen, Type: models.TypeEpic, Priority: models.PriorityP1}
	if err := database.CreateIssue(epicA); err != nil {
		t.Fatal(err)
	}
	epicB := &models.Issue{Title: "Epic B", Status: models.StatusOpen, Type: models.TypeEpic, Priority: models.PriorityP1}
	if err := database.CreateIssue(epicB); err != nil {
		t.Fatal(err)
	}

	taskA1 := &models.Issue{Title: "Task A1", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2, ParentID: epicA.ID}
	if err := database.CreateIssue(taskA1); err != nil {
		t.Fatal(err)
	}
	taskA2 := &models.Issue{Title: "Task A2", Status: models.StatusClosed, Type: models.TypeTask, Priority: models.PriorityP2, ParentID: epicA.ID}
	if err := database.CreateIssue(taskA2); err != nil {
		t.Fatal(err)
	}
	taskB1 := &models.Issue{Title: "Task B1", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2, ParentID: epicB.ID}
	if err := database.CreateIssue(taskB1); err != nil {
		t.Fatal(err)
	}

	// Unrelated issue (no epic parent)
	standalone := &models.Issue{Title: "Standalone", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(standalone); err != nil {
		t.Fatal(err)
	}

	t.Run("epic = X OR epic = Y returns union", func(t *testing.T) {
		q := "epic = " + epicA.ID + " OR epic = " + epicB.ID
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{taskA1.ID: true, taskA2.ID: true, taskB1.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("epic OR with AND status filter", func(t *testing.T) {
		q := "(epic = " + epicA.ID + " OR epic = " + epicB.ID + ") AND status = open"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{taskA1.ID: true, taskB1.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("epic OR status returns union of cross-entity and regular", func(t *testing.T) {
		q := "epic = " + epicA.ID + " OR status = closed"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		// taskA1 and taskA2 from epic, taskA2 also from status=closed (dedup)
		if !got[taskA1.ID] || !got[taskA2.ID] {
			t.Errorf("got %v, should include taskA1 and taskA2", got)
		}
	})

	t.Run("epic = nonexistent returns empty", func(t *testing.T) {
		results, err := Execute(database, "epic = td-nonexistent", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if len(results) != 0 {
			t.Errorf("got %d results, want 0", len(results))
		}
	})

	t.Run("epic = X OR epic = nonexistent returns just X descendants", func(t *testing.T) {
		q := "epic = " + epicA.ID + " OR epic = td-nonexistent"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{taskA1.ID: true, taskA2.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestExecuteDescendantOfOR(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// hierarchy: epicX -> taskX1 -> subtaskX1; epicY -> taskY1
	epicX := &models.Issue{Title: "Epic X", Status: models.StatusOpen, Type: models.TypeEpic, Priority: models.PriorityP1}
	if err := database.CreateIssue(epicX); err != nil {
		t.Fatal(err)
	}
	taskX1 := &models.Issue{Title: "Task X1", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2, ParentID: epicX.ID}
	if err := database.CreateIssue(taskX1); err != nil {
		t.Fatal(err)
	}
	subtaskX1 := &models.Issue{Title: "Subtask X1", Status: models.StatusClosed, Type: models.TypeTask, Priority: models.PriorityP3, ParentID: taskX1.ID}
	if err := database.CreateIssue(subtaskX1); err != nil {
		t.Fatal(err)
	}
	epicY := &models.Issue{Title: "Epic Y", Status: models.StatusOpen, Type: models.TypeEpic, Priority: models.PriorityP1}
	if err := database.CreateIssue(epicY); err != nil {
		t.Fatal(err)
	}
	taskY1 := &models.Issue{Title: "Task Y1", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2, ParentID: epicY.ID}
	if err := database.CreateIssue(taskY1); err != nil {
		t.Fatal(err)
	}
	unrelated := &models.Issue{Title: "Unrelated", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(unrelated); err != nil {
		t.Fatal(err)
	}

	t.Run("descendant_of(X) OR descendant_of(Y) returns union", func(t *testing.T) {
		q := "descendant_of(" + epicX.ID + ") OR descendant_of(" + epicY.ID + ")"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{taskX1.ID: true, subtaskX1.ID: true, taskY1.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("descendant_of(X) AND status = open", func(t *testing.T) {
		q := "descendant_of(" + epicX.ID + ") AND status = open"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{taskX1.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("NOT descendant_of(X) excludes descendants", func(t *testing.T) {
		q := "NOT descendant_of(" + epicX.ID + ")"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		if got[taskX1.ID] || got[subtaskX1.ID] {
			t.Errorf("should not include descendants of epicX, got %v", got)
		}
		if !got[epicX.ID] || !got[epicY.ID] || !got[taskY1.ID] || !got[unrelated.ID] {
			t.Errorf("should include non-descendants, got %v", got)
		}
	})
}

func TestExecuteLogBooleanCombinations(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	issueA := createTestIssue(t, database, "", "Issue A", models.StatusOpen, models.TypeTask, models.PriorityP1)
	issueB := createTestIssue(t, database, "", "Issue B", models.StatusOpen, models.TypeTask, models.PriorityP2)
	issueC := createTestIssue(t, database, "", "Issue C", models.StatusClosed, models.TypeTask, models.PriorityP3)

	// issueA has "deploy" log
	if err := database.AddLog(&models.Log{IssueID: issueA.ID, SessionID: "ses_test", Message: "deployed to staging", Type: models.LogTypeProgress}); err != nil {
		t.Fatal(err)
	}
	// issueB has "rollback" log with blocker type
	if err := database.AddLog(&models.Log{IssueID: issueB.ID, SessionID: "ses_test", Message: "rollback needed", Type: models.LogTypeBlocker}); err != nil {
		t.Fatal(err)
	}
	// issueC has "decision" log
	if err := database.AddLog(&models.Log{IssueID: issueC.ID, SessionID: "ses_test", Message: "decided to proceed", Type: models.LogTypeDecision}); err != nil {
		t.Fatal(err)
	}

	t.Run("log.message OR log.message returns union", func(t *testing.T) {
		results, err := Execute(database, `log.message ~ "deploy" OR log.message ~ "rollback"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueA.ID: true, issueB.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("NOT log.message excludes matches", func(t *testing.T) {
		results, err := Execute(database, `NOT log.message ~ "deploy"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		if got[issueA.ID] {
			t.Errorf("should not include issueA with deploy log, got %v", got)
		}
		if !got[issueB.ID] || !got[issueC.ID] {
			t.Errorf("should include issueB and issueC, got %v", got)
		}
	})

	t.Run("log.type OR log.type", func(t *testing.T) {
		results, err := Execute(database, `log.type = blocker OR log.type = decision`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueB.ID: true, issueC.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("log.message AND status combined", func(t *testing.T) {
		results, err := Execute(database, `log.message ~ "decided" AND status = closed`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueC.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestExecuteCommentCrossEntity(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	issueA := createTestIssue(t, database, "", "Issue A", models.StatusOpen, models.TypeTask, models.PriorityP1)
	issueB := createTestIssue(t, database, "", "Issue B", models.StatusOpen, models.TypeTask, models.PriorityP2)
	issueC := createTestIssue(t, database, "", "Issue C", models.StatusClosed, models.TypeTask, models.PriorityP3)

	if err := database.AddComment(&models.Comment{IssueID: issueA.ID, SessionID: "ses_test", Text: "approved for production"}); err != nil {
		t.Fatal(err)
	}
	if err := database.AddComment(&models.Comment{IssueID: issueB.ID, SessionID: "ses_test", Text: "needs revision"}); err != nil {
		t.Fatal(err)
	}
	// issueC has no comments

	t.Run("comment.text basic match", func(t *testing.T) {
		results, err := Execute(database, `comment.text ~ "approved"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueA.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("comment.text OR comment.text", func(t *testing.T) {
		results, err := Execute(database, `comment.text ~ "approved" OR comment.text ~ "revision"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueA.ID: true, issueB.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("NOT comment.text excludes matches", func(t *testing.T) {
		results, err := Execute(database, `NOT comment.text ~ "approved"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		if got[issueA.ID] {
			t.Errorf("should not include issueA, got %v", got)
		}
		if !got[issueB.ID] || !got[issueC.ID] {
			t.Errorf("should include issueB and issueC, got %v", got)
		}
	})

	t.Run("comment.text AND status combined", func(t *testing.T) {
		results, err := Execute(database, `comment.text ~ "revision" AND status = open`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueB.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestExecuteHandoffCrossEntity(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	issueA := createTestIssue(t, database, "", "Issue A", models.StatusOpen, models.TypeTask, models.PriorityP1)
	issueB := createTestIssue(t, database, "", "Issue B", models.StatusOpen, models.TypeTask, models.PriorityP2)
	issueC := createTestIssue(t, database, "", "Issue C", models.StatusOpen, models.TypeTask, models.PriorityP3)

	if err := database.AddHandoff(&models.Handoff{
		IssueID:   issueA.ID,
		SessionID: "ses_test",
		Done:      []string{"completed database migration"},
		Remaining: []string{"update API endpoints"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.AddHandoff(&models.Handoff{
		IssueID:   issueB.ID,
		SessionID: "ses_test",
		Done:      []string{"wrote unit tests"},
		Remaining: []string{"TODO: fix flaky test"},
	}); err != nil {
		t.Fatal(err)
	}
	// issueC has no handoff

	t.Run("handoff.done basic match", func(t *testing.T) {
		results, err := Execute(database, `handoff.done ~ "database"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueA.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("handoff.remaining OR handoff.done across fields", func(t *testing.T) {
		results, err := Execute(database, `handoff.remaining ~ "TODO" OR handoff.done ~ "database"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueA.ID: true, issueB.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("NOT handoff.remaining excludes matches", func(t *testing.T) {
		results, err := Execute(database, `NOT handoff.remaining ~ "TODO"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		if got[issueB.ID] {
			t.Errorf("should not include issueB with TODO remaining, got %v", got)
		}
		// issueA and issueC should be included (issueA has different remaining, issueC has no handoff)
		if !got[issueA.ID] || !got[issueC.ID] {
			t.Errorf("should include issueA and issueC, got %v", got)
		}
	})
}

func TestExecuteFileCrossEntity(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	issueA := createTestIssue(t, database, "", "Issue A", models.StatusOpen, models.TypeTask, models.PriorityP1)
	issueB := createTestIssue(t, database, "", "Issue B", models.StatusOpen, models.TypeTask, models.PriorityP2)
	issueC := createTestIssue(t, database, "", "Issue C", models.StatusOpen, models.TypeTask, models.PriorityP3)

	if err := database.LinkFile(issueA.ID, "cmd/main.go", models.FileRoleImplementation, "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := database.LinkFile(issueA.ID, "cmd/main_test.go", models.FileRoleTest, "abc124"); err != nil {
		t.Fatal(err)
	}
	if err := database.LinkFile(issueB.ID, "config/app.yaml", models.FileRoleConfig, "def456"); err != nil {
		t.Fatal(err)
	}
	// issueC has no files

	t.Run("file.path basic match", func(t *testing.T) {
		results, err := Execute(database, `file.path ~ "main.go"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		// issueA has both main.go and main_test.go matching "main.go"
		want := map[string]bool{issueA.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("file.role match", func(t *testing.T) {
		results, err := Execute(database, `file.role = test`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueA.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("file.role OR file.role", func(t *testing.T) {
		results, err := Execute(database, `file.role = test OR file.role = config`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueA.ID: true, issueB.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("NOT file.path excludes match", func(t *testing.T) {
		results, err := Execute(database, `NOT file.path ~ "main.go"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		if got[issueA.ID] {
			t.Errorf("should not include issueA, got %v", got)
		}
		if !got[issueB.ID] || !got[issueC.ID] {
			t.Errorf("should include issueB and issueC, got %v", got)
		}
	})
}

func TestExecuteMixedCrossEntityOR(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	issueA := createTestIssue(t, database, "", "Issue A", models.StatusOpen, models.TypeTask, models.PriorityP1)
	issueB := createTestIssue(t, database, "", "Issue B", models.StatusOpen, models.TypeTask, models.PriorityP2)
	createTestIssue(t, database, "", "Issue C", models.StatusOpen, models.TypeTask, models.PriorityP3)

	// issueA has a log
	if err := database.AddLog(&models.Log{IssueID: issueA.ID, SessionID: "ses_test", Message: "deploy complete", Type: models.LogTypeProgress}); err != nil {
		t.Fatal(err)
	}
	// issueB has a comment
	if err := database.AddComment(&models.Comment{IssueID: issueB.ID, SessionID: "ses_test", Text: "looks good"}); err != nil {
		t.Fatal(err)
	}
	// issueC has nothing

	t.Run("log OR comment across different cross-entity types", func(t *testing.T) {
		results, err := Execute(database, `log.message ~ "deploy" OR comment.text ~ "looks good"`, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{issueA.ID: true, issueB.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestExecuteMixedCrossEntityEpicAndDescendant(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	epicA := &models.Issue{Title: "Epic A", Status: models.StatusOpen, Type: models.TypeEpic, Priority: models.PriorityP1}
	if err := database.CreateIssue(epicA); err != nil {
		t.Fatal(err)
	}
	taskA := &models.Issue{Title: "Task A", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2, ParentID: epicA.ID}
	if err := database.CreateIssue(taskA); err != nil {
		t.Fatal(err)
	}

	// Separate parent (not an epic) with a child
	parentB := &models.Issue{Title: "Parent B", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(parentB); err != nil {
		t.Fatal(err)
	}
	taskB := &models.Issue{Title: "Task B", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2, ParentID: parentB.ID}
	if err := database.CreateIssue(taskB); err != nil {
		t.Fatal(err)
	}

	unrelated := &models.Issue{Title: "Unrelated", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(unrelated); err != nil {
		t.Fatal(err)
	}

	t.Run("epic = X OR descendant_of(Y)", func(t *testing.T) {
		q := "epic = " + epicA.ID + " OR descendant_of(" + parentB.ID + ")"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{taskA.ID: true, taskB.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestExecuteComplexNested(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	epicA := &models.Issue{Title: "Epic A", Status: models.StatusOpen, Type: models.TypeEpic, Priority: models.PriorityP1}
	if err := database.CreateIssue(epicA); err != nil {
		t.Fatal(err)
	}
	epicB := &models.Issue{Title: "Epic B", Status: models.StatusOpen, Type: models.TypeEpic, Priority: models.PriorityP1}
	if err := database.CreateIssue(epicB); err != nil {
		t.Fatal(err)
	}
	taskA := &models.Issue{Title: "Task A", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2, ParentID: epicA.ID}
	if err := database.CreateIssue(taskA); err != nil {
		t.Fatal(err)
	}
	taskB := &models.Issue{Title: "Task B", Status: models.StatusClosed, Type: models.TypeTask, Priority: models.PriorityP2, ParentID: epicB.ID}
	if err := database.CreateIssue(taskB); err != nil {
		t.Fatal(err)
	}
	standalone := &models.Issue{Title: "Standalone", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(standalone); err != nil {
		t.Fatal(err)
	}

	// Add logs/comments for mixed tests
	if err := database.AddLog(&models.Log{IssueID: taskA.ID, SessionID: "ses_test", Message: "progress update", Type: models.LogTypeProgress}); err != nil {
		t.Fatal(err)
	}
	if err := database.AddComment(&models.Comment{IssueID: taskB.ID, SessionID: "ses_test", Text: "reviewed and approved"}); err != nil {
		t.Fatal(err)
	}

	t.Run("NOT (epic = X OR epic = Y) excludes both epics descendants", func(t *testing.T) {
		q := "NOT (epic = " + epicA.ID + " OR epic = " + epicB.ID + ")"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		if got[taskA.ID] || got[taskB.ID] {
			t.Errorf("should not include tasks under either epic, got %v", got)
		}
		// Should include epics themselves and standalone
		if !got[epicA.ID] || !got[epicB.ID] || !got[standalone.ID] {
			t.Errorf("should include epics and standalone, got %v", got)
		}
	})

	t.Run("cross-entity OR with AND status", func(t *testing.T) {
		q := `(log.message ~ "progress" OR comment.text ~ "approved") AND status = open`
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		// taskA has log + open, taskB has comment but closed
		want := map[string]bool{taskA.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("(epic = X AND status = open) OR (epic = Y AND status = closed)", func(t *testing.T) {
		q := "(epic = " + epicA.ID + " AND status = open) OR (epic = " + epicB.ID + " AND status = closed)"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{taskA.ID: true, taskB.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestExecuteBlocksOR(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	targetX := createTestIssue(t, database, "", "Target X", models.StatusOpen, models.TypeTask, models.PriorityP1)
	targetY := createTestIssue(t, database, "", "Target Y", models.StatusOpen, models.TypeTask, models.PriorityP1)
	blockerA := createTestIssue(t, database, "", "Blocker A", models.StatusOpen, models.TypeTask, models.PriorityP2)
	blockerB := createTestIssue(t, database, "", "Blocker B", models.StatusOpen, models.TypeTask, models.PriorityP2)
	unrelated := createTestIssue(t, database, "", "Unrelated", models.StatusOpen, models.TypeTask, models.PriorityP3)

	// blockerA blocks targetX; blockerB blocks targetY
	if err := database.AddDependency(targetX.ID, blockerA.ID, "depends_on"); err != nil {
		t.Fatal(err)
	}
	if err := database.AddDependency(targetY.ID, blockerB.ID, "depends_on"); err != nil {
		t.Fatal(err)
	}

	t.Run("blocks(X) OR blocks(Y) returns union", func(t *testing.T) {
		q := "blocks(" + targetX.ID + ") OR blocks(" + targetY.ID + ")"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{blockerA.ID: true, blockerB.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("NOT blocked_by(X) excludes issues blocked by X", func(t *testing.T) {
		q := "NOT blocked_by(" + blockerA.ID + ")"
		results, err := Execute(database, q, "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		if got[targetX.ID] {
			t.Errorf("should not include targetX (blocked by blockerA), got %v", got)
		}
		if !got[blockerA.ID] || !got[blockerB.ID] || !got[targetY.ID] || !got[unrelated.ID] {
			t.Errorf("should include non-blocked issues, got %v", got)
		}
	})
}

func TestExecuteIsReadyOR(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	blocker := createTestIssue(t, database, "", "Blocker", models.StatusOpen, models.TypeTask, models.PriorityP1)
	blocked := createTestIssue(t, database, "", "Blocked", models.StatusOpen, models.TypeTask, models.PriorityP2)
	ready := createTestIssue(t, database, "", "Ready", models.StatusOpen, models.TypeTask, models.PriorityP2)
	closed := createTestIssue(t, database, "", "Closed", models.StatusClosed, models.TypeTask, models.PriorityP3)

	if err := database.AddDependency(blocked.ID, blocker.ID, "depends_on"); err != nil {
		t.Fatal(err)
	}

	t.Run("is_ready() OR status = closed", func(t *testing.T) {
		results, err := Execute(database, "is_ready() OR status = closed", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		// blocker, ready, closed are ready (no open deps); closed also matches status=closed
		// blocked is NOT ready and NOT closed, should be excluded
		if got[blocked.ID] {
			t.Errorf("blocked should not be included, got %v", got)
		}
		if !got[blocker.ID] || !got[ready.ID] || !got[closed.ID] {
			t.Errorf("should include ready and closed issues, got %v", got)
		}
	})

	t.Run("NOT is_ready() AND status = open", func(t *testing.T) {
		results, err := Execute(database, "NOT is_ready() AND status = open", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got := idSet(results)
		want := map[string]bool{blocked.ID: true}
		if !equalSets(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("is_ready() AND NOT has_open_deps() is equivalent", func(t *testing.T) {
		readyResults, err := Execute(database, "is_ready()", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		combinedResults, err := Execute(database, "is_ready() AND NOT has_open_deps()", "ses_test", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		gotReady := idSet(readyResults)
		gotCombined := idSet(combinedResults)
		if !equalSets(gotReady, gotCombined) {
			t.Errorf("is_ready() = %v, is_ready() AND NOT has_open_deps() = %v, should be equal", gotReady, gotCombined)
		}
	})
}

// Helper functions for test assertions

func idSet(issues []models.Issue) map[string]bool {
	m := make(map[string]bool, len(issues))
	for _, i := range issues {
		m[i.ID] = true
	}
	return m
}

func equalSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()
	os.Exit(code)
}
