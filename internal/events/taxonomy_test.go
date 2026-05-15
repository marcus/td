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
	expected := 15 // Number of entity types defined

	if len(types) != expected {
		t.Errorf("AllEntityTypes(): expected %d types, got %d", expected, len(types))
	}

	// Verify all constants are present
	requiredTypes := []EntityType{
		EntityIssues, EntityLogs, EntityHandoffs, EntityComments,
		EntitySessions, EntityBoards, EntityBoardIssuePositions,
		EntityWorkSessions, EntityWorkSessionIssues, EntityIssueFiles,
		EntityIssueDependencies, EntityGitSnapshots, EntityIssueSessionHistory,
		EntityIssueReviews, EntityNotes,
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

func TestNormalizeActionType_LegacyVerbsExhaustive(t *testing.T) {
	// Every legacy action_log verb listed in the package doc plus the
	// issue state transitions discovered during the audit. This test exists
	// so future edits to NormalizeActionType cannot silently change the
	// legacy → canonical mapping.
	tests := []struct {
		legacy   string
		expected ActionType
	}{
		// Documented create-aliases
		{"create", ActionCreate},
		{"handoff", ActionCreate},
		{"add_dependency", ActionCreate},
		{"link_file", ActionCreate},
		{"board_create", ActionCreate},
		{"board_update", ActionCreate},
		{"board_add_issue", ActionCreate},
		{"board_set_position", ActionCreate},
		{"work_session_tag", ActionCreate},

		// Documented delete-aliases
		{"remove_dependency", ActionDelete},
		{"unlink_file", ActionDelete},
		{"board_delete", ActionDelete},
		{"work_session_untag", ActionDelete},

		// Documented soft_delete-aliases
		{"delete", ActionSoftDelete},
		{"board_unposition", ActionSoftDelete},
		{"board_remove_issue", ActionSoftDelete},
		{"soft_delete", ActionSoftDelete},

		// Restore
		{"restore", ActionRestore},

		// Issue state transitions (explicit update-aliases)
		{"update", ActionUpdate},
		{"start", ActionUpdate},
		{"review", ActionUpdate},
		{"review_approve", ActionUpdate},
		{"review_changes_requested", ActionUpdate},
		{"close_after_review", ActionUpdate},
		{"approve", ActionUpdate},
		{"reject", ActionUpdate},
		{"block", ActionUpdate},
		{"unblock", ActionUpdate},
		{"close", ActionUpdate},
		{"reopen", ActionUpdate},
		{"board_move_issue", ActionUpdate},

		// Case-insensitive
		{"HANDOFF", ActionCreate},
		{"Board_Delete", ActionDelete},

		// Unknown defaults to update (preserve forward-compat)
		{"totally_unknown_verb", ActionUpdate},
	}

	for _, test := range tests {
		got := NormalizeActionType(test.legacy)
		if got != test.expected {
			t.Errorf("NormalizeActionType(%q) = %q; want %q", test.legacy, got, test.expected)
		}
	}
}

func TestNormalizeEntityType_RoundTripCanonical(t *testing.T) {
	// Every normalized output must itself be in AllEntityTypes() — i.e.
	// the normalizer cannot emit a string that the validator rejects.
	canon := AllEntityTypes()
	for raw := range canon {
		got, ok := NormalizeEntityType(string(raw))
		if !ok {
			t.Errorf("NormalizeEntityType(%q): canonical form rejected by normalizer", raw)
			continue
		}
		if !canon[got] {
			t.Errorf("NormalizeEntityType(%q) = %q; not in AllEntityTypes()", raw, got)
		}
	}
}

func TestNormalizeEntityType_UnknownNegative(t *testing.T) {
	negatives := []string{"users", "foo", "issue_review_v2", "boards_", " "}
	for _, n := range negatives {
		if _, ok := NormalizeEntityType(n); ok {
			t.Errorf("NormalizeEntityType(%q): expected invalid, got valid", n)
		}
	}
}

func TestEmitEvent(t *testing.T) {
	tests := []struct {
		name       string
		entity     string
		action     string
		wantEntity EntityType
		wantAction ActionType
		wantErr    bool
	}{
		// Happy path: canonical inputs
		{"canonical_issue_create", "issues", "create", EntityIssues, ActionCreate, false},
		{"canonical_issue_update", "issues", "update", EntityIssues, ActionUpdate, false},

		// Singular legacy entity + legacy action
		{"legacy_handoff_verb", "handoffs", "handoff", EntityHandoffs, ActionCreate, false},
		{"legacy_dep_create", "dependency", "add_dependency", EntityIssueDependencies, ActionCreate, false},
		{"legacy_dep_remove", "dependency", "remove_dependency", EntityIssueDependencies, ActionDelete, false},
		{"legacy_file_link", "file_link", "link_file", EntityIssueFiles, ActionCreate, false},
		{"legacy_board_create", "board", "board_create", EntityBoards, ActionCreate, false},

		// Issue state transitions on issues
		{"issue_close", "issue", "close", EntityIssues, ActionUpdate, false},
		{"issue_review_approve", "issue", "review_approve", EntityIssues, ActionUpdate, false},

		// Restore on issues is valid
		{"issue_restore", "issues", "restore", EntityIssues, ActionRestore, false},

		// Invalid combination: logs cannot be restored
		{"logs_restore_invalid", "logs", "restore", EntityLogs, ActionRestore, true},
		// Invalid combination: git_snapshots cannot be updated
		{"git_snapshots_update_invalid", "git_snapshots", "update", EntityGitSnapshots, ActionUpdate, true},
		// Invalid combination: work_session_issues cannot be updated
		{"wsi_update_invalid", "work_session_issues", "update", EntityWorkSessionIssues, ActionUpdate, true},

		// Unknown entity is rejected outright
		{"unknown_entity", "users", "create", "", "", true},
		{"empty_entity", "", "create", "", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotEntity, gotAction, err := EmitEvent(tc.entity, tc.action)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("EmitEvent(%q, %q): expected error, got nil (entity=%q action=%q)",
						tc.entity, tc.action, gotEntity, gotAction)
				}
				if gotEntity != tc.wantEntity {
					t.Errorf("EmitEvent(%q, %q): entity = %q; want %q",
						tc.entity, tc.action, gotEntity, tc.wantEntity)
				}
				if gotAction != tc.wantAction {
					t.Errorf("EmitEvent(%q, %q): action = %q; want %q",
						tc.entity, tc.action, gotAction, tc.wantAction)
				}
				return
			}
			if err != nil {
				t.Fatalf("EmitEvent(%q, %q): unexpected error: %v", tc.entity, tc.action, err)
			}
			if gotEntity != tc.wantEntity {
				t.Errorf("EmitEvent(%q, %q): entity = %q; want %q", tc.entity, tc.action, gotEntity, tc.wantEntity)
			}
			if gotAction != tc.wantAction {
				t.Errorf("EmitEvent(%q, %q): action = %q; want %q", tc.entity, tc.action, gotAction, tc.wantAction)
			}
		})
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
