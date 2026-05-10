package events

import (
	"testing"
)

func TestCompat_EntityTypeCanonicalSetNotShrunk(t *testing.T) {
	// Regression guard: the canonical entity type set must not shrink.
	// If you need to remove an entity type, update this count intentionally.
	const expectedMinEntityTypes = 14

	all := AllEntityTypes()
	if len(all) < expectedMinEntityTypes {
		t.Errorf("canonical entity type count regressed: got %d, want >= %d", len(all), expectedMinEntityTypes)
	}
}

func TestCompat_ActionTypeCanonicalSetNotShrunk(t *testing.T) {
	const expectedMinActionTypes = 5

	all := AllActionTypes()
	if len(all) < expectedMinActionTypes {
		t.Errorf("canonical action type count regressed: got %d, want >= %d", len(all), expectedMinActionTypes)
	}
}

func TestCompat_LegacyEntityAliases(t *testing.T) {
	// Every legacy singular alias must still resolve to the correct canonical type.
	legacyAliases := map[string]EntityType{
		// Singular → canonical plural
		"issue":                   EntityIssues,
		"log":                     EntityLogs,
		"handoff":                 EntityHandoffs,
		"comment":                 EntityComments,
		"session":                 EntitySessions,
		"board":                   EntityBoards,
		"board_position":          EntityBoardIssuePositions,
		"work_session":            EntityWorkSessions,
		"work_session_issue":      EntityWorkSessionIssues,
		"file_link":               EntityIssueFiles,
		"dependency":              EntityIssueDependencies,
		"git_snapshot":            EntityGitSnapshots,
		"issue_session_history":   EntityIssueSessionHistory,
		"issue_session_histories": EntityIssueSessionHistory,
		"note":                    EntityNotes,
		// Canonical forms must also work
		"issues":                EntityIssues,
		"logs":                  EntityLogs,
		"handoffs":              EntityHandoffs,
		"comments":              EntityComments,
		"sessions":              EntitySessions,
		"boards":                EntityBoards,
		"board_issue_positions": EntityBoardIssuePositions,
		"work_sessions":         EntityWorkSessions,
		"work_session_issues":   EntityWorkSessionIssues,
		"issue_files":           EntityIssueFiles,
		"issue_dependencies":    EntityIssueDependencies,
		"git_snapshots":         EntityGitSnapshots,
		"notes":                 EntityNotes,
	}

	for input, expected := range legacyAliases {
		got, ok := NormalizeEntityType(input)
		if !ok {
			t.Errorf("NormalizeEntityType(%q) returned ok=false, expected %q", input, expected)
			continue
		}
		if got != expected {
			t.Errorf("NormalizeEntityType(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestCompat_LegacyActionTypeMapping(t *testing.T) {
	// Every legacy action_log action type must map to the expected canonical type.
	legacyActions := map[string]ActionType{
		// → create
		"create":             ActionCreate,
		"handoff":            ActionCreate,
		"add_dependency":     ActionCreate,
		"link_file":          ActionCreate,
		"board_create":       ActionCreate,
		"board_update":       ActionCreate,
		"board_add_issue":    ActionCreate,
		"board_set_position": ActionCreate,
		"work_session_tag":   ActionCreate,
		// → delete (hard)
		"remove_dependency":  ActionDelete,
		"unlink_file":        ActionDelete,
		"board_delete":       ActionDelete,
		"work_session_untag": ActionDelete,
		// → soft_delete
		"delete":              ActionSoftDelete,
		"board_unposition":    ActionSoftDelete,
		"board_remove_issue":  ActionSoftDelete,
		"soft_delete":         ActionSoftDelete,
		// → restore
		"restore": ActionRestore,
		// → update (default)
		"update":  ActionUpdate,
		"modify":  ActionUpdate,
		"unknown": ActionUpdate,
	}

	for input, expected := range legacyActions {
		got := NormalizeActionType(input)
		if got != expected {
			t.Errorf("NormalizeActionType(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestCompat_CaseInsensitiveEntityNormalization(t *testing.T) {
	// Entity type normalization must be case-insensitive
	cases := []string{"ISSUE", "Issue", "iSSuE", "Issues", "ISSUES"}
	for _, input := range cases {
		got, ok := NormalizeEntityType(input)
		if !ok || got != EntityIssues {
			t.Errorf("NormalizeEntityType(%q) = (%q, %v), want (%q, true)", input, got, ok, EntityIssues)
		}
	}
}

func TestCompat_UnknownEntityTypeRejected(t *testing.T) {
	unknowns := []string{"", "foobar", "issuez", "task", "user", "project"}
	for _, input := range unknowns {
		_, ok := NormalizeEntityType(input)
		if ok {
			t.Errorf("NormalizeEntityType(%q) should return ok=false for unknown type", input)
		}
	}
}

func TestCompat_ValidEntityActionCombinations(t *testing.T) {
	combos := ValidEntityActionCombinations()

	// Every canonical entity type must appear in the combinations map
	for et := range AllEntityTypes() {
		if _, ok := combos[et]; !ok {
			t.Errorf("entity type %q missing from ValidEntityActionCombinations", et)
		}
	}

	// Spot-check critical combinations
	if !IsValidEntityActionCombination(EntityIssues, ActionCreate) {
		t.Error("issues+create should be valid")
	}
	if !IsValidEntityActionCombination(EntityIssues, ActionSoftDelete) {
		t.Error("issues+soft_delete should be valid")
	}
	if !IsValidEntityActionCombination(EntityGitSnapshots, ActionCreate) {
		t.Error("git_snapshots+create should be valid")
	}
	if IsValidEntityActionCombination(EntityGitSnapshots, ActionDelete) {
		t.Error("git_snapshots+delete should NOT be valid")
	}
}
