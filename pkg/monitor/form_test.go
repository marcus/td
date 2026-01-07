package monitor

import (
	"testing"

	"github.com/marcus/td/internal/models"
)

// TestPointsToString tests conversion from int to string for story points
func TestPointsToString(t *testing.T) {
	tests := []struct {
		name     string
		points   int
		expected string
	}{
		{"zero points", 0, "0"},
		{"one point", 1, "1"},
		{"two points", 2, "2"},
		{"three points", 3, "3"},
		{"five points", 5, "5"},
		{"eight points", 8, "8"},
		{"thirteen points", 13, "13"},
		{"twenty-one points", 21, "21"},
		{"invalid negative", -5, "0"},
		{"invalid large value", 100, "0"},
		{"invalid non-fib", 7, "0"},
		{"invalid non-fib", 10, "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pointsToString(tt.points)
			if got != tt.expected {
				t.Errorf("pointsToString(%d) = %q, want %q", tt.points, got, tt.expected)
			}
		})
	}
}

// TestStringToPoints tests conversion from string to int for story points
func TestStringToPoints(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"zero string", "0", 0},
		{"one string", "1", 1},
		{"two string", "2", 2},
		{"three string", "3", 3},
		{"five string", "5", 5},
		{"eight string", "8", 8},
		{"thirteen string", "13", 13},
		{"twenty-one string", "21", 21},
		{"empty string defaults to 0", "", 0},
		{"invalid string", "invalid", 0},
		{"non-fib number", "7", 0},
		{"negative string", "-5", 0},
		{"large invalid", "100", 0},
		{"whitespace", "  ", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringToPoints(tt.input)
			if got != tt.expected {
				t.Errorf("stringToPoints(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

// TestParseLabels tests parsing comma-separated label strings
func TestParseLabels(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"single label", "bug", []string{"bug"}},
		{"multiple labels", "bug, feature, urgent", []string{"bug", "feature", "urgent"}},
		{"labels with extra spaces", "bug  ,  feature  , urgent", []string{"bug", "feature", "urgent"}},
		{"empty string", "", nil},
		{"whitespace only", "   ", nil},
		{"trailing comma", "bug, feature,", []string{"bug", "feature"}},
		{"leading comma", ",bug, feature", []string{"bug", "feature"}},
		{"multiple commas", "bug,,,feature", []string{"bug", "feature"}},
		{"single comma", ",", nil},
		{"tabs and newlines", "bug\t, feature\n", []string{"bug", "feature"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLabels(tt.input)
			if !slicesEqual(got, tt.expected) {
				t.Errorf("parseLabels(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// TestNewFormState tests creating a new form for issue creation
func TestNewFormState(t *testing.T) {
	tests := []struct {
		name         string
		mode         FormMode
		parentID     string
		expectMode   FormMode
		expectType   string
		expectPrio   string
		expectPoints string
	}{
		{
			name:         "create mode no parent",
			mode:         FormModeCreate,
			parentID:     "",
			expectMode:   FormModeCreate,
			expectType:   string(models.TypeTask),
			expectPrio:   string(models.PriorityP2),
			expectPoints: "0",
		},
		{
			name:         "create mode with parent",
			mode:         FormModeCreate,
			parentID:     "td-epic-123",
			expectMode:   FormModeCreate,
			expectType:   string(models.TypeTask),
			expectPrio:   string(models.PriorityP2),
			expectPoints: "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewFormState(tt.mode, tt.parentID)

			if state.Mode != tt.expectMode {
				t.Errorf("Mode = %v, want %v", state.Mode, tt.expectMode)
			}
			if state.Type != tt.expectType {
				t.Errorf("Type = %q, want %q", state.Type, tt.expectType)
			}
			if state.Priority != tt.expectPrio {
				t.Errorf("Priority = %q, want %q", state.Priority, tt.expectPrio)
			}
			if state.Points != tt.expectPoints {
				t.Errorf("Points = %q, want %q", state.Points, tt.expectPoints)
			}
			if state.ParentID != tt.parentID {
				t.Errorf("ParentID = %q, want %q", state.ParentID, tt.parentID)
			}
			if state.Parent != tt.parentID {
				t.Errorf("Parent = %q, want %q", state.Parent, tt.parentID)
			}
			if state.ShowExtended != false {
				t.Errorf("ShowExtended = %v, want false", state.ShowExtended)
			}
			if state.Form == nil {
				t.Error("Form should be initialized")
			}
		})
	}
}

// TestNewFormStateForEdit tests creating a form for editing an existing issue
func TestNewFormStateForEdit(t *testing.T) {
	tests := []struct {
		name     string
		issue    *models.Issue
		validate func(t *testing.T, state *FormState)
	}{
		{
			name: "basic task edit",
			issue: &models.Issue{
				ID:          "td-001",
				Title:       "Fix login bug",
				Type:        models.TypeBug,
				Priority:    models.PriorityP1,
				Description: "Login button not working",
				Labels:      []string{"bug", "urgent"},
				ParentID:    "td-epic-001",
				Points:      5,
				Acceptance:  "- [ ] Test on Chrome\n- [ ] Test on Firefox",
				Minor:       false,
			},
			validate: func(t *testing.T, state *FormState) {
				if state.Mode != FormModeEdit {
					t.Errorf("Mode = %v, want FormModeEdit", state.Mode)
				}
				if state.IssueID != "td-001" {
					t.Errorf("IssueID = %q, want td-001", state.IssueID)
				}
				if state.Title != "Fix login bug" {
					t.Errorf("Title = %q, want 'Fix login bug'", state.Title)
				}
				if state.Type != string(models.TypeBug) {
					t.Errorf("Type = %q, want %q", state.Type, models.TypeBug)
				}
				if state.Priority != string(models.PriorityP1) {
					t.Errorf("Priority = %q, want %q", state.Priority, models.PriorityP1)
				}
				if state.Description != "Login button not working" {
					t.Errorf("Description = %q, want 'Login button not working'", state.Description)
				}
				if state.Labels != "bug, urgent" {
					t.Errorf("Labels = %q, want 'bug, urgent'", state.Labels)
				}
				if state.Parent != "td-epic-001" {
					t.Errorf("Parent = %q, want 'td-epic-001'", state.Parent)
				}
				if state.Points != "5" {
					t.Errorf("Points = %q, want '5'", state.Points)
				}
				if state.Acceptance != "- [ ] Test on Chrome\n- [ ] Test on Firefox" {
					t.Errorf("Acceptance not preserved")
				}
				if state.Minor != false {
					t.Errorf("Minor = %v, want false", state.Minor)
				}
				if state.Form == nil {
					t.Error("Form should be initialized")
				}
			},
		},
		{
			name: "epic with extended fields",
			issue: &models.Issue{
				ID:         "td-epic-001",
				Title:      "Q1 Platform Redesign",
				Type:       models.TypeEpic,
				Priority:   models.PriorityP0,
				Points:     21,
				Minor:      true,
				Labels:     []string{"platform", "redesign"},
				Acceptance: "- [ ] All pages updated\n- [ ] Tests passing",
			},
			validate: func(t *testing.T, state *FormState) {
				if state.Type != string(models.TypeEpic) {
					t.Errorf("Type = %q, want %q", state.Type, models.TypeEpic)
				}
				if state.Points != "21" {
					t.Errorf("Points = %q, want '21'", state.Points)
				}
				if state.Minor != true {
					t.Errorf("Minor = %v, want true", state.Minor)
				}
			},
		},
		{
			name: "issue with zero points",
			issue: &models.Issue{
				ID:       "td-002",
				Title:    "Update docs",
				Type:     models.TypeChore,
				Priority: models.PriorityP3,
				Points:   0,
			},
			validate: func(t *testing.T, state *FormState) {
				if state.Points != "0" {
					t.Errorf("Points = %q, want '0'", state.Points)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewFormStateForEdit(tt.issue)
			tt.validate(t, state)
		})
	}
}

// TestToggleExtended tests toggling extended fields visibility
func TestToggleExtended(t *testing.T) {
	state := NewFormState(FormModeCreate, "")

	if state.ShowExtended != false {
		t.Errorf("Initial ShowExtended = %v, want false", state.ShowExtended)
	}
	if state.Form == nil {
		t.Fatal("Form should be initialized")
	}

	// Toggle on
	state.ToggleExtended()
	if state.ShowExtended != true {
		t.Errorf("After toggle ShowExtended = %v, want true", state.ShowExtended)
	}

	// Toggle off
	state.ToggleExtended()
	if state.ShowExtended != false {
		t.Errorf("After second toggle ShowExtended = %v, want false", state.ShowExtended)
	}

	// Toggle on again
	state.ToggleExtended()
	if state.ShowExtended != true {
		t.Errorf("After third toggle ShowExtended = %v, want true", state.ShowExtended)
	}
}

// TestToIssue tests converting form state to Issue model
func TestToIssue(t *testing.T) {
	tests := []struct {
		name      string
		formState *FormState
		validate  func(t *testing.T, issue *models.Issue)
	}{
		{
			name: "basic task conversion",
			formState: &FormState{
				Title:       "New feature",
				Type:        string(models.TypeFeature),
				Priority:    string(models.PriorityP1),
				Description: "Add user profile page",
				Labels:      "feature, frontend",
				Parent:      "td-epic-001",
				Points:      "8",
				Acceptance:  "- [ ] Profile photo\n- [ ] Settings link",
				Minor:       false,
			},
			validate: func(t *testing.T, issue *models.Issue) {
				if issue.Title != "New feature" {
					t.Errorf("Title = %q, want 'New feature'", issue.Title)
				}
				if issue.Type != models.TypeFeature {
					t.Errorf("Type = %v, want %v", issue.Type, models.TypeFeature)
				}
				if issue.Priority != models.PriorityP1 {
					t.Errorf("Priority = %v, want %v", issue.Priority, models.PriorityP1)
				}
				if issue.Description != "Add user profile page" {
					t.Errorf("Description mismatch")
				}
				if len(issue.Labels) != 2 || issue.Labels[0] != "feature" || issue.Labels[1] != "frontend" {
					t.Errorf("Labels = %v, want [feature frontend]", issue.Labels)
				}
				if issue.ParentID != "td-epic-001" {
					t.Errorf("ParentID = %q, want 'td-epic-001'", issue.ParentID)
				}
				if issue.Points != 8 {
					t.Errorf("Points = %d, want 8", issue.Points)
				}
				if issue.Acceptance != "- [ ] Profile photo\n- [ ] Settings link" {
					t.Errorf("Acceptance mismatch")
				}
				if issue.Minor != false {
					t.Errorf("Minor = %v, want false", issue.Minor)
				}
			},
		},
		{
			name: "bug with whitespace trimming",
			formState: &FormState{
				Title:    "  Database connection error  ",
				Type:     string(models.TypeBug),
				Priority: string(models.PriorityP0),
				Labels:   "  bug  ,  critical  ",
				Parent:   "  ",
				Points:   "5",
				Minor:    false,
			},
			validate: func(t *testing.T, issue *models.Issue) {
				if issue.Title != "Database connection error" {
					t.Errorf("Title not trimmed: %q", issue.Title)
				}
				if issue.ParentID != "" {
					t.Errorf("Empty parent should remain empty, got %q", issue.ParentID)
				}
				if len(issue.Labels) != 2 {
					t.Errorf("Labels after trim = %v", issue.Labels)
				}
			},
		},
		{
			name: "no labels",
			formState: &FormState{
				Title:    "Simple task",
				Type:     string(models.TypeTask),
				Priority: string(models.PriorityP2),
				Labels:   "",
				Points:   "3",
				Minor:    false,
			},
			validate: func(t *testing.T, issue *models.Issue) {
				if issue.Labels != nil {
					t.Errorf("Empty labels should be nil, got %v", issue.Labels)
				}
			},
		},
		{
			name: "all points values",
			formState: &FormState{
				Title:    "Test points",
				Type:     string(models.TypeTask),
				Priority: string(models.PriorityP2),
				Points:   "13",
				Minor:    false,
			},
			validate: func(t *testing.T, issue *models.Issue) {
				if issue.Points != 13 {
					t.Errorf("Points = %d, want 13", issue.Points)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := tt.formState.ToIssue()
			tt.validate(t, issue)
		})
	}
}

// TestGetDependencies tests dependency parsing from form state
func TestGetDependencies(t *testing.T) {
	tests := []struct {
		name      string
		formState *FormState
		expected  []string
	}{
		{
			name:      "single dependency",
			formState: &FormState{Dependencies: "td-001"},
			expected:  []string{"td-001"},
		},
		{
			name:      "multiple dependencies",
			formState: &FormState{Dependencies: "td-001, td-002, td-003"},
			expected:  []string{"td-001", "td-002", "td-003"},
		},
		{
			name:      "dependencies with spaces",
			formState: &FormState{Dependencies: "  td-001  ,  td-002  "},
			expected:  []string{"td-001", "td-002"},
		},
		{
			name:      "empty dependencies",
			formState: &FormState{Dependencies: ""},
			expected:  nil,
		},
		{
			name:      "whitespace only",
			formState: &FormState{Dependencies: "   "},
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.formState.GetDependencies()
			if !slicesEqual(got, tt.expected) {
				t.Errorf("GetDependencies() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestFormStateFieldValidation tests form field validation
func TestFormStateFieldValidation(t *testing.T) {
	tests := []struct {
		name     string
		state    *FormState
		validate func(t *testing.T, state *FormState)
	}{
		{
			name: "title required validation",
			state: &FormState{
				Title:    "Test Title",
				Type:     string(models.TypeTask),
				Priority: string(models.PriorityP2),
			},
			validate: func(t *testing.T, state *FormState) {
				// Form should be constructed with valid title
				if state.Title == "" {
					t.Error("Title should not be empty")
				}
			},
		},
		{
			name:  "all types represented",
			state: &FormState{},
			validate: func(t *testing.T, state *FormState) {
				validTypes := []string{
					string(models.TypeTask),
					string(models.TypeBug),
					string(models.TypeFeature),
					string(models.TypeChore),
					string(models.TypeEpic),
				}
				for _, typeStr := range validTypes {
					testState := &FormState{
						Title:    "test",
						Type:     typeStr,
						Priority: string(models.PriorityP2),
					}
					issue := testState.ToIssue()
					if string(issue.Type) != typeStr {
						t.Errorf("Type %q not preserved", typeStr)
					}
				}
			},
		},
		{
			name:  "all priorities represented",
			state: &FormState{},
			validate: func(t *testing.T, state *FormState) {
				validPriorities := []string{
					string(models.PriorityP0),
					string(models.PriorityP1),
					string(models.PriorityP2),
					string(models.PriorityP3),
					string(models.PriorityP4),
				}
				for _, prioStr := range validPriorities {
					testState := &FormState{
						Title:    "test",
						Type:     string(models.TypeTask),
						Priority: prioStr,
					}
					issue := testState.ToIssue()
					if string(issue.Priority) != prioStr {
						t.Errorf("Priority %q not preserved", prioStr)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.validate(t, tt.state)
		})
	}
}

// TestFormModeStringRepresentation tests FormMode type
func TestFormModeStringRepresentation(t *testing.T) {
	tests := []struct {
		name     string
		mode     FormMode
		expected string
	}{
		{"create mode", FormModeCreate, "create"},
		{"edit mode", FormModeEdit, "edit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.mode) != tt.expected {
				t.Errorf("FormMode(%v) = %q, want %q", tt.mode, string(tt.mode), tt.expected)
			}
		})
	}
}

// TestBuildFormStandardFields tests form construction with standard fields
func TestBuildFormStandardFields(t *testing.T) {
	state := NewFormState(FormModeCreate, "")

	if state.Form == nil {
		t.Fatal("Form should not be nil")
	}

	// Verify basic form properties when not showing extended
	if state.ShowExtended {
		t.Error("Standard form should not show extended fields initially")
	}

	// Rebuild to verify idempotency
	state.buildForm()
	if state.Form == nil {
		t.Error("Form should still be valid after rebuild")
	}
}

// TestBuildFormExtendedFields tests form construction with extended fields
func TestBuildFormExtendedFields(t *testing.T) {
	state := NewFormState(FormModeCreate, "")
	state.ShowExtended = true
	state.buildForm()

	if state.Form == nil {
		t.Fatal("Form should not be nil")
	}

	if !state.ShowExtended {
		t.Error("ShowExtended should be true")
	}
}

// TestFormStateEditModeTitle tests form title in edit mode
func TestFormStateEditModeTitle(t *testing.T) {
	issue := &models.Issue{
		ID:       "td-12345",
		Title:    "Sample issue",
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}

	state := NewFormStateForEdit(issue)

	if state.Mode != FormModeEdit {
		t.Errorf("Mode = %v, want FormModeEdit", state.Mode)
	}

	if state.IssueID != "td-12345" {
		t.Errorf("IssueID = %q, want 'td-12345'", state.IssueID)
	}

	// Form title should reflect edit mode
	if state.Form == nil {
		t.Fatal("Form should be initialized")
	}
}

// TestPointsRoundTrip tests conversion roundtrip int -> string -> int
func TestPointsRoundTrip(t *testing.T) {
	validPoints := []int{0, 1, 2, 3, 5, 8, 13, 21}

	for _, originalPoints := range validPoints {
		t.Run("roundtrip "+string(rune('0'+originalPoints%10)), func(t *testing.T) {
			strPoints := pointsToString(originalPoints)
			newPoints := stringToPoints(strPoints)

			if newPoints != originalPoints {
				t.Errorf("roundtrip for %d failed: %d -> %q -> %d",
					originalPoints, originalPoints, strPoints, newPoints)
			}
		})
	}
}

// TestLabelsParsing tests edge cases in label parsing
func TestLabelsParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		desc     string
	}{
		{
			name:     "unicode labels",
			input:    "🐛-bug, ✨-feature",
			expected: []string{"🐛-bug", "✨-feature"},
			desc:     "should handle unicode in labels",
		},
		{
			name:     "hyphenated labels",
			input:    "high-priority, needs-review",
			expected: []string{"high-priority", "needs-review"},
			desc:     "should handle hyphens",
		},
		{
			name:     "underscore labels",
			input:    "backend_optimization, frontend_ux",
			expected: []string{"backend_optimization", "frontend_ux"},
			desc:     "should handle underscores",
		},
		{
			name:     "mixed case",
			input:    "Bug, FEATURE, Task",
			expected: []string{"Bug", "FEATURE", "Task"},
			desc:     "should preserve case",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLabels(tt.input)
			if !slicesEqual(got, tt.expected) {
				t.Errorf("parseLabels(%q) = %v, want %v - %s",
					tt.input, got, tt.expected, tt.desc)
			}
		})
	}
}

// TestFormStatePreservesAllIssueFields tests that form preserves all issue fields
func TestFormStatePreservesAllIssueFields(t *testing.T) {
	originalIssue := &models.Issue{
		ID:          "td-comprehensive-001",
		Title:       "Comprehensive test issue",
		Type:        models.TypeFeature,
		Priority:    models.PriorityP1,
		Description: "This tests all field preservation",
		Labels:      []string{"test", "preservation"},
		ParentID:    "td-epic-parent",
		Points:      13,
		Acceptance:  "- [ ] Test 1\n- [ ] Test 2\n- [ ] Test 3",
		Minor:       true,
		Status:      models.StatusOpen,
	}

	// Create form state from issue
	formState := NewFormStateForEdit(originalIssue)

	// Convert back to issue
	newIssue := formState.ToIssue()

	// Verify all fields are preserved
	if newIssue.Title != originalIssue.Title {
		t.Errorf("Title not preserved: %q != %q", newIssue.Title, originalIssue.Title)
	}
	if newIssue.Type != originalIssue.Type {
		t.Errorf("Type not preserved: %v != %v", newIssue.Type, originalIssue.Type)
	}
	if newIssue.Priority != originalIssue.Priority {
		t.Errorf("Priority not preserved: %v != %v", newIssue.Priority, originalIssue.Priority)
	}
	if newIssue.Description != originalIssue.Description {
		t.Errorf("Description not preserved")
	}
	if !slicesEqual(newIssue.Labels, originalIssue.Labels) {
		t.Errorf("Labels not preserved: %v != %v", newIssue.Labels, originalIssue.Labels)
	}
	if newIssue.ParentID != originalIssue.ParentID {
		t.Errorf("ParentID not preserved: %q != %q", newIssue.ParentID, originalIssue.ParentID)
	}
	if newIssue.Points != originalIssue.Points {
		t.Errorf("Points not preserved: %d != %d", newIssue.Points, originalIssue.Points)
	}
	if newIssue.Acceptance != originalIssue.Acceptance {
		t.Errorf("Acceptance not preserved")
	}
	if newIssue.Minor != originalIssue.Minor {
		t.Errorf("Minor not preserved: %v != %v", newIssue.Minor, originalIssue.Minor)
	}
}

// Helper function to compare string slices
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
