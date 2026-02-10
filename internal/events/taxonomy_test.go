package events

import (
	"testing"
)

func TestNormalizeEntityType(t *testing.T) {
	tests := []struct {
		input    string
		expected EntityType
		valid    bool
	}{
		// Issues
		{"issue", EntityIssues, true},
		{"issues", EntityIssues, true},
		{"ISSUE", EntityIssues, true},
		{"Issues", EntityIssues, true},

		// Handoffs
		{"handoff", EntityHandoffs, true},
		{"handoffs", EntityHandoffs, true},

		// Boards
		{"board", EntityBoards, true},
		{"boards", EntityBoards, true},

		// Logs
		{"log", EntityLogs, true},
		{"logs", EntityLogs, true},

		// Comments
		{"comment", EntityComments, true},
		{"comments", EntityComments, true},

		// Work sessions
		{"work_session", EntityWorkSessions, true},
		{"work_sessions", EntityWorkSessions, true},

		// Board issue positions
		{"board_position", EntityBoardIssuePositions, true},
		{"board_issue_positions", EntityBoardIssuePositions, true},

		// Dependencies
		{"dependency", EntityIssueDependencies, true},
		{"issue_dependencies", EntityIssueDependencies, true},

		// Issue files
		{"file_link", EntityIssueFiles, true},
		{"issue_files", EntityIssueFiles, true},

		// Work session issues
		{"work_session_issue", EntityWorkSessionIssues, true},
		{"work_session_issues", EntityWorkSessionIssues, true},

		// Notes
		{"note", EntityNotes, true},
		{"notes", EntityNotes, true},

		// Sessions
		{"session", EntitySessions, true},
		{"sessions", EntitySessions, true},

		// Git snapshots
		{"git_snapshot", EntityGitSnapshots, true},
		{"git_snapshots", EntityGitSnapshots, true},

		// Issue session history
		{"issue_session_history", EntityIssueSessionHistory, true},
		{"issue_session_histories", EntityIssueSessionHistory, true},

		// Invalid
		{"invalid", "", false},
		{"unknown", "", false},
		{"", "", false},
	}

	for _, test := range tests {
		result, valid := NormalizeEntityType(test.input)
		if valid != test.valid {
			t.Errorf("NormalizeEntityType(%q): expected valid=%v, got %v", test.input, test.valid, valid)
		}
		if valid && result != test.expected {
			t.Errorf("NormalizeEntityType(%q): expected %q, got %q", test.input, test.expected, result)
		}
	}
}

func TestNormalizeActionType(t *testing.T) {
	tests := []struct {
		input    string
		expected ActionType
	}{
		// Create variations
		{"create", ActionCreate},
		{"handoff", ActionCreate},
		{"add_dependency", ActionCreate},
		{"link_file", ActionCreate},
		{"board_create", ActionCreate},
		{"board_update", ActionCreate},
		{"board_add_issue", ActionCreate},
		{"board_set_position", ActionCreate},
		{"work_session_tag", ActionCreate},

		// Delete variations
		{"remove_dependency", ActionDelete},
		{"unlink_file", ActionDelete},
		{"board_delete", ActionDelete},
		{"work_session_untag", ActionDelete},

		// Soft delete variations
		{"delete", ActionSoftDelete},
		{"board_unposition", ActionSoftDelete},
		{"board_remove_issue", ActionSoftDelete},
		{"soft_delete", ActionSoftDelete},

		// Restore
		{"restore", ActionRestore},

		// Default to update
		{"update", ActionUpdate},
		{"modify", ActionUpdate},
		{"unknown", ActionUpdate},
		{"", ActionUpdate},
	}

	for _, test := range tests {
		result := NormalizeActionType(test.input)
		if result != test.expected {
			t.Errorf("NormalizeActionType(%q): expected %q, got %q", test.input, test.expected, result)
		}
	}
}

func TestIsValidEntityType(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"issues", true},
		{"logs", true},
		{"handoffs", true},
		{"comments", true},
		{"sessions", true},
		{"boards", true},
		{"board_issue_positions", true},
		{"work_sessions", true},
		{"work_session_issues", true},
		{"issue_files", true},
		{"issue_dependencies", true},
		{"git_snapshots", true},
		{"issue_session_history", true},
		{"notes", true},
		{"invalid", false},
		{"", false},
	}

	for _, test := range tests {
		result := IsValidEntityType(test.input)
		if result != test.expected {
			t.Errorf("IsValidEntityType(%q): expected %v, got %v", test.input, test.expected, result)
		}
	}
}

func TestIsValidActionType(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"create", true},
		{"update", true},
		{"delete", true},
		{"soft_delete", true},
		{"restore", true},
		{"invalid", false},
		{"", false},
	}

	for _, test := range tests {
		result := IsValidActionType(test.input)
		if result != test.expected {
			t.Errorf("IsValidActionType(%q): expected %v, got %v", test.input, test.expected, result)
		}
	}
}

func TestIsValidEntityActionCombination(t *testing.T) {
	tests := []struct {
		entity   EntityType
		action   ActionType
		expected bool
	}{
		// Issues should support all actions
		{EntityIssues, ActionCreate, true},
		{EntityIssues, ActionUpdate, true},
		{EntityIssues, ActionDelete, true},
		{EntityIssues, ActionSoftDelete, true},
		{EntityIssues, ActionRestore, true},

		// Logs only support create and update
		{EntityLogs, ActionCreate, true},
		{EntityLogs, ActionUpdate, true},
		{EntityLogs, ActionDelete, false},
		{EntityLogs, ActionSoftDelete, false},
		{EntityLogs, ActionRestore, false},

		// Handoffs support create, update, delete, soft_delete
		{EntityHandoffs, ActionCreate, true},
		{EntityHandoffs, ActionUpdate, true},
		{EntityHandoffs, ActionDelete, true},
		{EntityHandoffs, ActionSoftDelete, true},
		{EntityHandoffs, ActionRestore, false},

		// Work session issues only support create and delete
		{EntityWorkSessionIssues, ActionCreate, true},
		{EntityWorkSessionIssues, ActionDelete, true},
		{EntityWorkSessionIssues, ActionUpdate, false},
		{EntityWorkSessionIssues, ActionSoftDelete, false},

		// Issue files only support create and delete
		{EntityIssueFiles, ActionCreate, true},
		{EntityIssueFiles, ActionDelete, true},
		{EntityIssueFiles, ActionUpdate, false},

		// Git snapshots only support create
		{EntityGitSnapshots, ActionCreate, true},
		{EntityGitSnapshots, ActionUpdate, false},
		{EntityGitSnapshots, ActionDelete, false},
	}

	for _, test := range tests {
		result := IsValidEntityActionCombination(test.entity, test.action)
		if result != test.expected {
			t.Errorf("IsValidEntityActionCombination(%q, %q): expected %v, got %v", test.entity, test.action, test.expected, result)
		}
	}
}

func TestAllEntityTypes(t *testing.T) {
	types := AllEntityTypes()
	expected := 14 // Number of entity types defined

	if len(types) != expected {
		t.Errorf("AllEntityTypes(): expected %d types, got %d", expected, len(types))
	}

	// Verify all constants are present
	requiredTypes := []EntityType{
		EntityIssues, EntityLogs, EntityHandoffs, EntityComments,
		EntitySessions, EntityBoards, EntityBoardIssuePositions,
		EntityWorkSessions, EntityWorkSessionIssues, EntityIssueFiles,
		EntityIssueDependencies, EntityGitSnapshots, EntityIssueSessionHistory,
		EntityNotes,
	}

	for _, et := range requiredTypes {
		if !types[et] {
			t.Errorf("AllEntityTypes(): missing entity type %q", et)
		}
	}
}

func TestAllActionTypes(t *testing.T) {
	types := AllActionTypes()
	expected := 5 // Number of action types defined

	if len(types) != expected {
		t.Errorf("AllActionTypes(): expected %d types, got %d", expected, len(types))
	}

	// Verify all constants are present
	requiredTypes := []ActionType{
		ActionCreate, ActionUpdate, ActionDelete, ActionSoftDelete, ActionRestore,
	}

	for _, at := range requiredTypes {
		if !types[at] {
			t.Errorf("AllActionTypes(): missing action type %q", at)
		}
	}
}

func TestValidEntityActionCombinations(t *testing.T) {
	combinations := ValidEntityActionCombinations()

	// Verify all entity types are in the combinations map
	allTypes := AllEntityTypes()
	for et := range allTypes {
		if _, ok := combinations[et]; !ok {
			t.Errorf("ValidEntityActionCombinations(): missing entity type %q", et)
		}
	}

	// Verify all combinations use valid action types
	validActions := AllActionTypes()
	for entity, actionMap := range combinations {
		for action := range actionMap {
			if !validActions[action] {
				t.Errorf("ValidEntityActionCombinations(): entity %q has invalid action %q", entity, action)
			}
		}
	}
}
