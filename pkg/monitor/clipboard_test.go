package monitor

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/marcus/td/internal/models"
)

// TestFormatIssueAsMarkdown tests markdown formatting for individual issues
func TestFormatIssueAsMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		issue    *models.Issue
		contains []string
		notIn    []string
	}{
		{
			name: "basic issue with all fields",
			issue: &models.Issue{
				ID:          "td-123",
				Title:       "Fix login bug",
				Type:        models.TypeBug,
				Priority:    models.PriorityP0,
				Status:      models.StatusOpen,
				Description: "Login fails on Firefox",
				Acceptance:  "Login works on all browsers",
			},
			contains: []string{
				"# Fix login bug",
				"**ID:** `td-123`",
				"**Type:** bug",
				"**Priority:** P0",
				"**Status:** open",
				"## Description",
				"Login fails on Firefox",
				"## Acceptance Criteria",
				"Login works on all browsers",
			},
			notIn: []string{"**Parent:**"},
		},
		{
			name: "issue with parent epic",
			issue: &models.Issue{
				ID:       "td-456",
				Title:    "Auth task",
				Type:     models.TypeTask,
				Priority: models.PriorityP2,
				Status:   models.StatusInProgress,
				ParentID: "td-epic-1",
			},
			contains: []string{
				"# Auth task",
				"**ID:** `td-456`",
				"**Type:** task",
				"**Priority:** P2",
				"**Status:** in_progress",
				"**Parent:** `td-epic-1`",
			},
			notIn: []string{"## Description", "## Acceptance Criteria"},
		},
		{
			name: "issue with minimal fields",
			issue: &models.Issue{
				ID:       "td-789",
				Title:    "Simple task",
				Type:     models.TypeTask,
				Priority: models.PriorityP3,
				Status:   models.StatusOpen,
			},
			contains: []string{
				"# Simple task",
				"**ID:** `td-789`",
				"**Type:** task",
				"**Priority:** P3",
				"**Status:** open",
			},
			notIn: []string{"**Parent:**", "## Description", "## Acceptance Criteria"},
		},
		{
			name: "feature with multiline description",
			issue: &models.Issue{
				ID:          "td-feat-1",
				Title:       "Dark mode",
				Type:        models.TypeFeature,
				Priority:    models.PriorityP0,
				Status:      models.StatusInProgress,
				Description: "Line 1\nLine 2\nLine 3",
				Acceptance:  "Multi-line\nAcceptance\nCriteria",
			},
			contains: []string{
				"# Dark mode",
				"Line 1",
				"Line 2",
				"Line 3",
				"Multi-line",
			},
			notIn: []string{},
		},
		{
			name: "chore with no description or acceptance",
			issue: &models.Issue{
				ID:       "td-chore-1",
				Title:    "Update deps",
				Type:     models.TypeChore,
				Priority: models.PriorityP3,
				Status:   models.StatusClosed,
			},
			contains: []string{
				"# Update deps",
				"**ID:** `td-chore-1`",
				"**Type:** chore",
				"**Priority:** P3",
				"**Status:** closed",
			},
			notIn: []string{"## Description", "## Acceptance Criteria", "**Parent:**"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatIssueAsMarkdown(tt.issue)

			for _, req := range tt.contains {
				if !strings.Contains(result, req) {
					t.Errorf("expected markdown to contain %q, but got:\n%s", req, result)
				}
			}

			for _, notWanted := range tt.notIn {
				if strings.Contains(result, notWanted) {
					t.Errorf("expected markdown NOT to contain %q, but got:\n%s", notWanted, result)
				}
			}
		})
	}
}

// TestFormatEpicAsMarkdown tests markdown formatting for epics with child stories
func TestFormatEpicAsMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		epic     *models.Issue
		children []models.Issue
		contains []string
		notIn    []string
	}{
		{
			name: "epic with multiple child stories",
			epic: &models.Issue{
				ID:          "td-epic-1",
				Title:       "Authentication",
				Type:        models.TypeEpic,
				Priority:    models.PriorityP0,
				Status:      models.StatusInProgress,
				Description: "Build auth system",
				Acceptance:  "All flows work",
			},
			children: []models.Issue{
				{
					ID:          "td-1",
					Title:       "Login page",
					Type:        models.TypeTask,
					Priority:    models.PriorityP1,
					Status:      models.StatusClosed,
					Description: "Main login UI",
					Acceptance:  "Login renders correctly",
				},
				{
					ID:          "td-2",
					Title:       "Password reset",
					Type:        models.TypeTask,
					Priority:    models.PriorityP2,
					Status:      models.StatusInProgress,
					Description: "Reset flow",
				},
				{
					ID:          "td-3",
					Title:       "Two-factor auth",
					Type:        models.TypeTask,
					Priority:    models.PriorityP1,
					Status:      models.StatusOpen,
					Description: "",
				},
			},
			contains: []string{
				"# Epic: Authentication",
				"**ID:** `td-epic-1`",
				"**Priority:** P0",
				"**Status:** in_progress",
				"## Description",
				"Build auth system",
				"## Acceptance Criteria",
				"All flows work",
				"## Tasks",
				"### [x] Login page",
				"**ID:** `td-1`",
				"**Type:** task | **Priority:** P1 | **Status:** closed",
				"#### Description",
				"Main login UI",
				"#### Acceptance Criteria",
				"Login renders correctly",
				"---",
				"### [-] Password reset",
				"**ID:** `td-2`",
				"Reset flow",
				"### [ ] Two-factor auth",
				"**ID:** `td-3`",
			},
			notIn: []string{},
		},
		{
			name: "epic with no child stories",
			epic: &models.Issue{
				ID:       "td-epic-2",
				Title:    "Infrastructure",
				Type:     models.TypeEpic,
				Priority: models.PriorityP2,
				Status:   models.StatusOpen,
			},
			children: []models.Issue{},
			contains: []string{
				"# Epic: Infrastructure",
				"**ID:** `td-epic-2`",
				"**Priority:** P2",
				"**Status:** open",
			},
			notIn: []string{"## Tasks", "## Description", "## Acceptance Criteria"},
		},
		{
			name: "epic with story in review status",
			epic: &models.Issue{
				ID:       "td-epic-3",
				Title:    "Performance",
				Type:     models.TypeEpic,
				Priority: models.PriorityP0,
				Status:   models.StatusInProgress,
			},
			children: []models.Issue{
				{
					ID:     "td-perf-1",
					Title:  "Optimize DB",
					Type:   models.TypeTask,
					Status: models.StatusInReview,
				},
				{
					ID:     "td-perf-2",
					Title:  "Cache layer",
					Type:   models.TypeTask,
					Status: models.StatusBlocked,
				},
			},
			contains: []string{
				"# Epic: Performance",
				"## Tasks",
				"### [~] Optimize DB",
				"**ID:** `td-perf-1`",
				"### [!] Cache layer",
				"**ID:** `td-perf-2`",
			},
			notIn: []string{},
		},
		{
			name: "epic with multiline descriptions in children",
			epic: &models.Issue{
				ID:    "td-epic-4",
				Title: "Testing",
				Type:  models.TypeEpic,
			},
			children: []models.Issue{
				{
					ID:          "td-test-1",
					Title:       "Unit tests",
					Type:        models.TypeTask,
					Status:      models.StatusOpen,
					Description: "Add unit tests\nfor all modules\nwith 80% coverage",
				},
			},
			contains: []string{
				"# Epic: Testing",
				"### [ ] Unit tests",
				"**ID:** `td-test-1`",
				"#### Description",
				"Add unit tests",
				"for all modules",
				"with 80% coverage",
			},
			notIn: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatEpicAsMarkdown(tt.epic, tt.children)

			for _, req := range tt.contains {
				if !strings.Contains(result, req) {
					t.Errorf("expected markdown to contain %q, but got:\n%s", req, result)
				}
			}

			for _, notWanted := range tt.notIn {
				if strings.Contains(result, notWanted) {
					t.Errorf("expected markdown NOT to contain %q, but got:\n%s", notWanted, result)
				}
			}
		})
	}
}

// TestStatusIcon tests status icon formatting for all status types
func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status   models.Status
		expected string
	}{
		{models.StatusOpen, "[ ]"},
		{models.StatusInProgress, "[-]"},
		{models.StatusInReview, "[~]"},
		{models.StatusBlocked, "[!]"},
		{models.StatusClosed, "[x]"},
		{models.Status("unknown"), "[ ]"},
		{models.Status(""), "[ ]"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			result := statusIcon(tt.status)
			if result != tt.expected {
				t.Errorf("statusIcon(%q) = %q, want %q", tt.status, result, tt.expected)
			}
		})
	}
}

// TestFormatIssueAsMarkdownEdgeCases tests edge cases in formatting
func TestFormatIssueAsMarkdownEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		issue    *models.Issue
		validates func(string) error
	}{
		{
			name: "issue with special characters in title",
			issue: &models.Issue{
				ID:    "td-1",
				Title: "Fix [bug] & issue's \"error\"",
				Type:  models.TypeBug,
			},
			validates: func(result string) error {
				if !strings.Contains(result, "Fix [bug] & issue's \"error\"") {
					return fmt.Errorf("expected title with special chars")
				}
				return nil
			},
		},
		{
			name: "issue with empty description but non-empty acceptance",
			issue: &models.Issue{
				ID:          "td-2",
				Title:       "Task",
				Type:        models.TypeTask,
				Description: "",
				Acceptance:  "Must pass tests",
			},
			validates: func(result string) error {
				if !strings.Contains(result, "## Acceptance Criteria") {
					return fmt.Errorf("expected acceptance criteria section")
				}
				return nil
			},
		},
		{
			name: "issue with parent ID",
			issue: &models.Issue{
				ID:       "td-3",
				Title:    "Child",
				ParentID: "td-parent-123",
				Type:     models.TypeTask,
			},
			validates: func(result string) error {
				if !strings.Contains(result, "td-parent-123") {
					return fmt.Errorf("expected parent ID in output")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatIssueAsMarkdown(tt.issue)
			if err := tt.validates(result); err != nil {
				t.Errorf("edge case failed: %v\nGot:\n%s", err, result)
			}
		})
	}
}

// TestFormatEpicAsMarkdownEdgeCases tests edge cases for epic formatting
func TestFormatEpicAsMarkdownEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		epic      *models.Issue
		children  []models.Issue
		validates func(string) error
	}{
		{
			name: "epic with empty children array",
			epic: &models.Issue{
				ID:    "td-epic-1",
				Title: "Epic",
				Type:  models.TypeEpic,
			},
			children: []models.Issue{},
			validates: func(result string) error {
				if strings.Contains(result, "## Tasks") {
					return fmt.Errorf("empty children should not have Tasks section")
				}
				return nil
			},
		},
		{
			name: "epic with child having empty description",
			epic: &models.Issue{
				ID:    "td-epic-2",
				Title: "Epic",
				Type:  models.TypeEpic,
			},
			children: []models.Issue{
				{
					ID:          "td-1",
					Title:       "Task",
					Type:        models.TypeTask,
					Status:      models.StatusOpen,
					Description: "",
				},
			},
			validates: func(result string) error {
				if !strings.Contains(result, "### [ ] Task") {
					return fmt.Errorf("expected task header line in output")
				}
				if !strings.Contains(result, "**ID:** `td-1`") {
					return fmt.Errorf("expected task ID line in output")
				}
				return nil
			},
		},
		{
			name: "epic with multiline description in children",
			epic: &models.Issue{
				ID:    "td-epic-3",
				Title: "Epic",
				Type:  models.TypeEpic,
			},
			children: []models.Issue{
				{
					ID:          "td-1",
					Title:       "Task",
					Status:      models.StatusOpen,
					Description: "Line 1\n\nLine 3",
				},
			},
			validates: func(result string) error {
				if !strings.Contains(result, "Line 1") {
					return fmt.Errorf("expected Line 1 in output")
				}
				if !strings.Contains(result, "Line 3") {
					return fmt.Errorf("expected Line 3 in output")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatEpicAsMarkdown(tt.epic, tt.children)
			if err := tt.validates(result); err != nil {
				t.Errorf("edge case failed: %v\nGot:\n%s", err, result)
			}
		})
	}
}

// TestMarkdownSyntaxValidation verifies correct markdown syntax structure
func TestMarkdownSyntaxValidation(t *testing.T) {
	t.Run("issue markdown syntax", func(t *testing.T) {
		issue := &models.Issue{
			ID:          "td-123",
			Title:       "Example",
			Type:        models.TypeTask,
			Priority:    models.PriorityP0,
			Status:      models.StatusInProgress,
			Description: "This is a test",
			Acceptance:  "Should work",
			ParentID:    "td-parent",
		}

		result := formatIssueAsMarkdown(issue)
		lines := strings.Split(result, "\n")

		if !strings.HasPrefix(lines[0], "# ") {
			t.Errorf("first line should start with '# ', got %q", lines[0])
		}

		hasIDLine := false
		hasTypeLine := false
		hasDescSection := false
		hasAcceptSection := false

		for i, line := range lines {
			if strings.Contains(line, "**ID:**") {
				hasIDLine = true
				if !strings.Contains(line, "`") {
					t.Errorf("line %d should contain backticks around ID: %q", i, line)
				}
			}
			if strings.Contains(line, "**Type:**") {
				hasTypeLine = true
			}
			if strings.HasPrefix(line, "## Description") {
				hasDescSection = true
			}
			if strings.HasPrefix(line, "## Acceptance Criteria") {
				hasAcceptSection = true
			}
		}

		if !hasIDLine || !hasTypeLine || !hasDescSection || !hasAcceptSection {
			t.Error("markdown missing required sections")
		}
	})

	t.Run("epic markdown syntax", func(t *testing.T) {
		epic := &models.Issue{
			ID:          "td-epic-1",
			Title:       "Example Epic",
			Type:        models.TypeEpic,
			Priority:    models.PriorityP0,
			Status:      models.StatusInProgress,
			Description: "Epic description",
			Acceptance:  "Epic acceptance",
		}

		children := []models.Issue{
			{ID: "td-1", Title: "Story 1", Type: models.TypeTask, Status: models.StatusOpen},
			{ID: "td-2", Title: "Story 2", Type: models.TypeTask, Status: models.StatusClosed},
		}

		result := formatEpicAsMarkdown(epic, children)
		lines := strings.Split(result, "\n")

		if !strings.HasPrefix(lines[0], "# Epic:") {
			t.Errorf("first line should start with '# Epic:', got %q", lines[0])
		}

		hasTaskSection := false
		taskHeaderCount := 0

		for _, line := range lines {
			if strings.HasPrefix(line, "## Tasks") {
				hasTaskSection = true
			}
			if strings.HasPrefix(line, "### [") {
				taskHeaderCount++
			}
		}

		if !hasTaskSection {
			t.Error("epic markdown missing Tasks section")
		}
		if taskHeaderCount < 2 {
			t.Errorf("epic markdown should have 2 task headers, found %d", taskHeaderCount)
		}
	})
}

// BenchmarkFormatIssueAsMarkdown benchmarks issue formatting performance
func BenchmarkFormatIssueAsMarkdown(b *testing.B) {
	issue := &models.Issue{
		ID:          "td-123",
		Title:       "Example task with a longer title",
		Type:        models.TypeTask,
		Priority:    models.PriorityP0,
		Status:      models.StatusInProgress,
		Description: "This is a long description\nthat spans multiple lines\nwith details",
		Acceptance:  "Should pass all tests\nand be performant",
		ParentID:    "td-parent-epic",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		formatIssueAsMarkdown(issue)
	}
}

// BenchmarkFormatEpicAsMarkdown benchmarks epic formatting with multiple children
func BenchmarkFormatEpicAsMarkdown(b *testing.B) {
	epic := &models.Issue{
		ID:          "td-epic-1",
		Title:       "Large epic with many stories",
		Type:        models.TypeEpic,
		Priority:    models.PriorityP0,
		Status:      models.StatusInProgress,
		Description: "Epic description",
		Acceptance:  "Epic acceptance",
	}

	children := make([]models.Issue, 20)
	for i := 0; i < 20; i++ {
		children[i] = models.Issue{
			ID:          "td-child-" + strconv.Itoa(i),
			Title:       "Story " + strconv.Itoa(i),
			Status:      models.StatusOpen,
			Description: "Description line 1\nDescription line 2",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		formatEpicAsMarkdown(epic, children)
	}
}

// noopClipboard is a no-op clipboard function for tests
func noopClipboard(_ string) error { return nil }

// TestCopyCurrentIssueToClipboard_FromModal tests copying from an open modal
func TestCopyCurrentIssueToClipboard_FromModal(t *testing.T) {
	issue := &models.Issue{
		ID:          "td-modal-123",
		Title:       "Modal Issue",
		Type:        models.TypeTask,
		Priority:    models.PriorityP1,
		Status:      models.StatusInProgress,
		Description: "Test description",
	}

	m := Model{
		ClipboardFn: noopClipboard,
		ModalStack: []ModalEntry{
			{
				IssueID: issue.ID,
				Issue:   issue,
			},
		},
		Cursor: map[Panel]int{
			PanelTaskList: 0,
		},
		TaskListRows: []TaskListRow{
			{Issue: models.Issue{ID: "td-panel-456"}},
		},
	}

	// Verify modal is open
	if m.CurrentModal() == nil {
		t.Fatal("modal should be open")
	}

	// The method should use modal issue, not panel selection
	result, _ := m.copyCurrentIssueToClipboard()
	m2 := result.(Model)

	// Should have set a status message (success or failure)
	if m2.StatusMessage == "" {
		t.Error("expected StatusMessage to be set")
	}

	// On success, should say "Yanked to clipboard"
	if !m2.StatusIsError && m2.StatusMessage != "Yanked to clipboard" {
		t.Errorf("expected success message, got %q", m2.StatusMessage)
	}
}

// TestCopyCurrentIssueToClipboard_FromPanel tests copying from panel selection
// Note: Panel-based copying requires a DB to look up the full issue.
// This test verifies the logic flow. For full integration, use tests with a real DB.
func TestCopyCurrentIssueToClipboard_FromPanel(t *testing.T) {
	t.Skip("Panel-based copy requires DB - covered by integration tests")
}

// TestCopyCurrentIssueToClipboard_NoSelection tests with no issue selected
func TestCopyCurrentIssueToClipboard_NoSelection(t *testing.T) {
	m := Model{
		ClipboardFn: noopClipboard,
		ModalStack:  []ModalEntry{}, // No modal
		Cursor: map[Panel]int{
			PanelTaskList: 0,
		},
		TaskListRows: []TaskListRow{}, // Empty list
		ActivePanel:  PanelTaskList,
	}

	result, _ := m.copyCurrentIssueToClipboard()
	m2 := result.(Model)

	// Should return without setting status (early return on no selection)
	if m2.StatusMessage != "" {
		t.Errorf("expected no status message for no selection, got %q", m2.StatusMessage)
	}
}

// TestCopyCurrentIssueToClipboard_Epic tests copying an epic with tasks
func TestCopyCurrentIssueToClipboard_Epic(t *testing.T) {
	epicIssue := &models.Issue{
		ID:          "td-epic-001",
		Title:       "Test Epic",
		Type:        models.TypeEpic,
		Priority:    models.PriorityP0,
		Status:      models.StatusInProgress,
		Description: "Epic description",
	}

	epicTasks := []models.Issue{
		{ID: "td-task-1", Title: "Task 1", Status: models.StatusClosed},
		{ID: "td-task-2", Title: "Task 2", Status: models.StatusOpen},
	}

	m := Model{
		ClipboardFn: noopClipboard,
		ModalStack: []ModalEntry{
			{
				IssueID:   epicIssue.ID,
				Issue:     epicIssue,
				EpicTasks: epicTasks,
			},
		},
	}

	result, _ := m.copyCurrentIssueToClipboard()
	m2 := result.(Model)

	// Should have set status message
	if m2.StatusMessage == "" {
		t.Error("expected StatusMessage to be set for epic copy")
	}

	// On success, should say "Yanked to clipboard"
	if !m2.StatusIsError && m2.StatusMessage != "Yanked to clipboard" {
		t.Errorf("expected success message for epic, got %q", m2.StatusMessage)
	}
}

// TestCopyIssueIDToClipboard_FromModal tests copying ID from modal
func TestCopyIssueIDToClipboard_FromModal(t *testing.T) {
	issue := &models.Issue{
		ID:    "td-id-modal-123",
		Title: "Modal Issue",
	}

	m := Model{
		ClipboardFn: noopClipboard,
		ModalStack: []ModalEntry{
			{
				IssueID: issue.ID,
				Issue:   issue,
			},
		},
		Cursor: map[Panel]int{
			PanelTaskList: 0,
		},
		TaskListRows: []TaskListRow{
			{Issue: models.Issue{ID: "td-panel-different"}},
		},
	}

	result, _ := m.copyIssueIDToClipboard()
	m2 := result.(Model)

	// Should have set status message
	if m2.StatusMessage == "" {
		t.Error("expected StatusMessage to be set")
	}

	// On success, should contain the modal's issue ID
	if !m2.StatusIsError && !strings.Contains(m2.StatusMessage, "td-id-modal-123") {
		t.Errorf("expected status to contain modal issue ID, got %q", m2.StatusMessage)
	}
}

// TestCopyIssueIDToClipboard_FromPanel tests copying ID from panel selection
func TestCopyIssueIDToClipboard_FromPanel(t *testing.T) {
	panelIssue := models.Issue{
		ID:    "td-panel-id-456",
		Title: "Panel Issue",
	}

	m := Model{
		ClipboardFn: noopClipboard,
		ModalStack:  []ModalEntry{}, // No modal
		Cursor: map[Panel]int{
			PanelTaskList: 0,
		},
		TaskListRows: []TaskListRow{
			{Issue: panelIssue},
		},
		ActivePanel: PanelTaskList,
	}

	result, _ := m.copyIssueIDToClipboard()
	m2 := result.(Model)

	// Should have set status message with the panel issue ID
	if m2.StatusMessage == "" {
		t.Error("expected StatusMessage to be set")
	}

	// On success, should contain the panel's issue ID
	if !m2.StatusIsError && !strings.Contains(m2.StatusMessage, "td-panel-id-456") {
		t.Errorf("expected status to contain panel issue ID, got %q", m2.StatusMessage)
	}
}

// TestCopyIssueIDToClipboard_NoSelection tests copying ID with no selection
func TestCopyIssueIDToClipboard_NoSelection(t *testing.T) {
	m := Model{
		ClipboardFn: noopClipboard,
		ModalStack:  []ModalEntry{}, // No modal
		Cursor: map[Panel]int{
			PanelTaskList: 0,
		},
		TaskListRows: []TaskListRow{}, // Empty list
		ActivePanel:  PanelTaskList,
	}

	result, _ := m.copyIssueIDToClipboard()
	m2 := result.(Model)

	// Should return without setting status (early return on no selection)
	if m2.StatusMessage != "" {
		t.Errorf("expected no status message for no selection, got %q", m2.StatusMessage)
	}
}

// TestCopyIssueIDToClipboard_PrefersModal tests that modal takes priority
func TestCopyIssueIDToClipboard_PrefersModal(t *testing.T) {
	modalIssue := &models.Issue{
		ID:    "td-modal-priority",
		Title: "Modal Priority Issue",
	}

	panelIssue := models.Issue{
		ID:    "td-panel-ignored",
		Title: "Panel Should Be Ignored",
	}

	m := Model{
		ClipboardFn: noopClipboard,
		ModalStack: []ModalEntry{
			{
				IssueID: modalIssue.ID,
				Issue:   modalIssue,
			},
		},
		Cursor: map[Panel]int{
			PanelTaskList: 0,
		},
		TaskListRows: []TaskListRow{
			{Issue: panelIssue},
		},
		ActivePanel: PanelTaskList,
	}

	result, _ := m.copyIssueIDToClipboard()
	m2 := result.(Model)

	// Should use modal ID, not panel ID
	if !m2.StatusIsError {
		if strings.Contains(m2.StatusMessage, "td-panel-ignored") {
			t.Error("should NOT use panel issue when modal is open")
		}
		if !strings.Contains(m2.StatusMessage, "td-modal-priority") {
			t.Errorf("should use modal issue ID, got %q", m2.StatusMessage)
		}
	}
}
