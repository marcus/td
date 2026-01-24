package monitor

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func createTestIssue(t *testing.T, database *db.DB, title string, status models.Status) *models.Issue {
	issue := &models.Issue{
		Title:  title,
		Type:   models.TypeTask,
		Status: status,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("failed to create issue %q: %v", title, err)
	}
	return issue
}

func TestComputeBoardIssueCategories(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer database.Close()

	// Create a blocker issue (open)
	blocker := createTestIssue(t, database, "Blocker issue", models.StatusOpen)

	// Create a blocked issue (open, depends on blocker)
	blocked := createTestIssue(t, database, "Blocked issue", models.StatusOpen)
	if err := database.AddDependency(blocked.ID, blocker.ID, "depends_on"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// Create a ready issue (open, no dependencies)
	ready := createTestIssue(t, database, "Ready issue", models.StatusOpen)

	// Create an explicitly blocked issue
	explicitBlocked := createTestIssue(t, database, "Explicit blocked", models.StatusBlocked)

	// Build BoardIssueViews
	issues := []models.BoardIssueView{
		{Issue: *blocker},
		{Issue: *blocked},
		{Issue: *ready},
		{Issue: *explicitBlocked},
	}

	// Compute categories
	ComputeBoardIssueCategories(database, issues, "test-session", nil)

	// Verify categories
	tests := []struct {
		name     string
		issueID  string
		expected TaskListCategory
	}{
		{"blocker is ready", blocker.ID, CategoryReady},
		{"blocked by dep is blocked", blocked.ID, CategoryBlocked},
		{"ready issue is ready", ready.ID, CategoryReady},
		{"explicit blocked is blocked", explicitBlocked.ID, CategoryBlocked},
	}

	// Build lookup map
	categoryByID := make(map[string]string)
	for _, biv := range issues {
		categoryByID[biv.Issue.ID] = biv.Category
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TaskListCategory(categoryByID[tt.issueID])
			if got != tt.expected {
				t.Errorf("issue %s: got category %q, want %q", tt.issueID, got, tt.expected)
			}
		})
	}
}

func TestGetSortFuncWithPosition(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		sortMode SortMode
		issues   []models.BoardIssueView
		wantIDs  []string // expected order of issue IDs after sort
	}{
		{
			name:     "all positioned - sort by position ASC",
			sortMode: SortByPriority,
			issues: []models.BoardIssueView{
				{Issue: models.Issue{ID: "c", Priority: models.PriorityP0}, Position: 3, HasPosition: true},
				{Issue: models.Issue{ID: "a", Priority: models.PriorityP3}, Position: 1, HasPosition: true},
				{Issue: models.Issue{ID: "b", Priority: models.PriorityP1}, Position: 2, HasPosition: true},
			},
			wantIDs: []string{"a", "b", "c"}, // by position, not priority
		},
		{
			name:     "all unpositioned - sort by priority",
			sortMode: SortByPriority,
			issues: []models.BoardIssueView{
				{Issue: models.Issue{ID: "c", Priority: models.PriorityP3, UpdatedAt: now}},
				{Issue: models.Issue{ID: "a", Priority: models.PriorityP0, UpdatedAt: now}},
				{Issue: models.Issue{ID: "b", Priority: models.PriorityP1, UpdatedAt: now}},
			},
			wantIDs: []string{"a", "b", "c"}, // P0, P1, P3
		},
		{
			name:     "all unpositioned - sort by created desc",
			sortMode: SortByCreatedDesc,
			issues: []models.BoardIssueView{
				{Issue: models.Issue{ID: "a", CreatedAt: now.Add(-2 * time.Hour)}},
				{Issue: models.Issue{ID: "b", CreatedAt: now.Add(-1 * time.Hour)}},
				{Issue: models.Issue{ID: "c", CreatedAt: now}},
			},
			wantIDs: []string{"c", "b", "a"}, // newest first
		},
		{
			name:     "all unpositioned - sort by updated desc",
			sortMode: SortByUpdatedDesc,
			issues: []models.BoardIssueView{
				{Issue: models.Issue{ID: "a", UpdatedAt: now.Add(-2 * time.Hour)}},
				{Issue: models.Issue{ID: "b", UpdatedAt: now.Add(-1 * time.Hour)}},
				{Issue: models.Issue{ID: "c", UpdatedAt: now}},
			},
			wantIDs: []string{"c", "b", "a"}, // newest first
		},
		{
			name:     "mixed - positioned come before unpositioned",
			sortMode: SortByPriority,
			issues: []models.BoardIssueView{
				{Issue: models.Issue{ID: "unpos-p0", Priority: models.PriorityP0, UpdatedAt: now}}, // high priority but unpositioned
				{Issue: models.Issue{ID: "pos-p3", Priority: models.PriorityP3}, Position: 1, HasPosition: true}, // low priority but positioned
				{Issue: models.Issue{ID: "unpos-p1", Priority: models.PriorityP1, UpdatedAt: now}},
			},
			wantIDs: []string{"pos-p3", "unpos-p0", "unpos-p1"}, // positioned first, then by priority
		},
		{
			name:     "mixed positions - stable ordering",
			sortMode: SortByPriority,
			issues: []models.BoardIssueView{
				{Issue: models.Issue{ID: "unpos-1", Priority: models.PriorityP2, UpdatedAt: now}},
				{Issue: models.Issue{ID: "pos-1", Priority: models.PriorityP3}, Position: 1, HasPosition: true},
				{Issue: models.Issue{ID: "pos-2", Priority: models.PriorityP0}, Position: 5, HasPosition: true},
				{Issue: models.Issue{ID: "unpos-2", Priority: models.PriorityP0, UpdatedAt: now}},
			},
			wantIDs: []string{"pos-1", "pos-2", "unpos-2", "unpos-1"}, // positioned by position, unpositioned by priority
		},
		{
			name:     "priority tiebreaker uses updated_at",
			sortMode: SortByPriority,
			issues: []models.BoardIssueView{
				{Issue: models.Issue{ID: "older", Priority: models.PriorityP1, UpdatedAt: now.Add(-1 * time.Hour)}},
				{Issue: models.Issue{ID: "newer", Priority: models.PriorityP1, UpdatedAt: now}},
			},
			wantIDs: []string{"newer", "older"}, // same priority, more recent updated first
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := make([]models.BoardIssueView, len(tt.issues))
			copy(issues, tt.issues)

			sortFunc := getSortFuncWithPosition(tt.sortMode)
			sort.Slice(issues, sortFunc(issues))

			gotIDs := make([]string, len(issues))
			for i, biv := range issues {
				gotIDs[i] = biv.Issue.ID
			}

			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Errorf("got %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}

func TestFilterBoardIssuesByQuery(t *testing.T) {
	issues := []models.BoardIssueView{
		{Issue: models.Issue{ID: "td-abc123", Title: "Fix login bug", Type: models.TypeBug}},
		{Issue: models.Issue{ID: "td-def456", Title: "Add feature", Type: models.TypeFeature}},
		{Issue: models.Issue{ID: "td-ghi789", Title: "Refactor code", Type: models.TypeTask}},
	}

	tests := []struct {
		name    string
		query   string
		wantIDs []string
	}{
		{
			name:    "empty query returns all",
			query:   "",
			wantIDs: []string{"td-abc123", "td-def456", "td-ghi789"},
		},
		{
			name:    "sort clause only returns all",
			query:   "sort:priority",
			wantIDs: []string{"td-abc123", "td-def456", "td-ghi789"},
		},
		{
			name:    "sort clause with minus returns all",
			query:   "sort:-updated",
			wantIDs: []string{"td-abc123", "td-def456", "td-ghi789"},
		},
		{
			name:    "type filter bug",
			query:   "type=bug",
			wantIDs: []string{"td-abc123"},
		},
		{
			name:    "type filter task with sort",
			query:   "sort:priority type=task",
			wantIDs: []string{"td-ghi789"},
		},
		{
			name:    "type filter feature",
			query:   "type=feature",
			wantIDs: []string{"td-def456"},
		},
		{
			name:    "type filter no matches",
			query:   "type=epic",
			wantIDs: []string{},
		},
		{
			name:    "search term filters correctly",
			query:   "login",
			wantIDs: []string{"td-abc123"},
		},
		{
			name:    "search term with sort clause",
			query:   "login sort:priority",
			wantIDs: []string{"td-abc123"},
		},
		{
			name:    "search term with type filter",
			query:   "login type=bug",
			wantIDs: []string{"td-abc123"},
		},
		{
			name:    "search by ID",
			query:   "td-def",
			wantIDs: []string{"td-def456"},
		},
		{
			name:    "no matches",
			query:   "nonexistent",
			wantIDs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid mutation
			issuesCopy := make([]models.BoardIssueView, len(issues))
			copy(issuesCopy, issues)

			result := filterBoardIssuesByQuery(issuesCopy, tt.query)

			gotIDs := make([]string, len(result))
			for i, biv := range result {
				gotIDs[i] = biv.Issue.ID
			}

			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Errorf("filterBoardIssuesByQuery(%q) = %v, want %v", tt.query, gotIDs, tt.wantIDs)
			}
		})
	}
}

func TestComputeBoardIssueCategoriesClosedDepUnblocks(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer database.Close()

	// Create a blocker issue (closed)
	blocker := createTestIssue(t, database, "Blocker issue", models.StatusClosed)

	// Create dependent issue (should be ready since blocker is closed)
	dependent := createTestIssue(t, database, "Dependent issue", models.StatusOpen)
	if err := database.AddDependency(dependent.ID, blocker.ID, "depends_on"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	issues := []models.BoardIssueView{{Issue: *dependent}}

	ComputeBoardIssueCategories(database, issues, "test-session", nil)

	if issues[0].Category != string(CategoryReady) {
		t.Errorf("dependent with closed blocker: got %q, want %q", issues[0].Category, CategoryReady)
	}
}
