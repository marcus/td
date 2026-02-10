package events

import "strings"

// EntityType represents the canonical entity types in the sync system.
type EntityType string

// ActionType represents the canonical action types for events.
type ActionType string

// Canonical entity types
const (
	EntityIssues             EntityType = "issues"
	EntityLogs               EntityType = "logs"
	EntityHandoffs           EntityType = "handoffs"
	EntityComments           EntityType = "comments"
	EntitySessions           EntityType = "sessions"
	EntityBoards             EntityType = "boards"
	EntityBoardIssuePositions EntityType = "board_issue_positions"
	EntityWorkSessions       EntityType = "work_sessions"
	EntityWorkSessionIssues  EntityType = "work_session_issues"
	EntityIssueFiles         EntityType = "issue_files"
	EntityIssueDependencies  EntityType = "issue_dependencies"
	EntityGitSnapshots       EntityType = "git_snapshots"
	EntityIssueSessionHistory EntityType = "issue_session_history"
	EntityNotes              EntityType = "notes"
)

// Canonical action types
const (
	ActionCreate    ActionType = "create"
	ActionUpdate    ActionType = "update"
	ActionDelete    ActionType = "delete"
	ActionSoftDelete ActionType = "soft_delete"
	ActionRestore   ActionType = "restore"
)

// AllEntityTypes returns all valid entity types.
func AllEntityTypes() map[EntityType]bool {
	return map[EntityType]bool{
		EntityIssues:              true,
		EntityLogs:                true,
		EntityHandoffs:            true,
		EntityComments:            true,
		EntitySessions:            true,
		EntityBoards:              true,
		EntityBoardIssuePositions: true,
		EntityWorkSessions:        true,
		EntityWorkSessionIssues:   true,
		EntityIssueFiles:          true,
		EntityIssueDependencies:   true,
		EntityGitSnapshots:        true,
		EntityIssueSessionHistory: true,
		EntityNotes:               true,
	}
}

// AllActionTypes returns all valid action types.
func AllActionTypes() map[ActionType]bool {
	return map[ActionType]bool{
		ActionCreate:    true,
		ActionUpdate:    true,
		ActionDelete:    true,
		ActionSoftDelete: true,
		ActionRestore:   true,
	}
}

// IsValidEntityType checks if the given entity type string is valid.
func IsValidEntityType(et string) bool {
	return AllEntityTypes()[EntityType(et)]
}

// IsValidActionType checks if the given action type string is valid.
func IsValidActionType(at string) bool {
	return AllActionTypes()[ActionType(at)]
}

// NormalizeEntityType normalizes an entity type string to its canonical form.
// Returns the canonical entity type and true if valid, or empty string and false if invalid.
// Handles both singular and plural forms.
func NormalizeEntityType(entityType string) (EntityType, bool) {
	switch strings.ToLower(entityType) {
	case "issue", "issues":
		return EntityIssues, true
	case "handoff", "handoffs":
		return EntityHandoffs, true
	case "board", "boards":
		return EntityBoards, true
	case "log", "logs":
		return EntityLogs, true
	case "comment", "comments":
		return EntityComments, true
	case "work_session", "work_sessions":
		return EntityWorkSessions, true
	case "board_position", "board_issue_positions":
		return EntityBoardIssuePositions, true
	case "dependency", "issue_dependencies":
		return EntityIssueDependencies, true
	case "file_link", "issue_files":
		return EntityIssueFiles, true
	case "work_session_issue", "work_session_issues":
		return EntityWorkSessionIssues, true
	case "note", "notes":
		return EntityNotes, true
	case "session", "sessions":
		return EntitySessions, true
	case "git_snapshot", "git_snapshots":
		return EntityGitSnapshots, true
	case "issue_session_history", "issue_session_histories":
		return EntityIssueSessionHistory, true
	default:
		return "", false
	}
}

// NormalizeActionType normalizes an action type string to its canonical form.
// Maps td's internal action_log action types to canonical sync event action types.
func NormalizeActionType(tdAction string) ActionType {
	switch strings.ToLower(tdAction) {
	case "create", "handoff", "add_dependency", "link_file", "board_create",
		 "board_update", "board_add_issue", "board_set_position", "work_session_tag":
		return ActionCreate
	case "remove_dependency", "unlink_file", "board_delete", "work_session_untag":
		return ActionDelete
	case "delete", "board_unposition", "board_remove_issue", "soft_delete":
		return ActionSoftDelete
	case "restore":
		return ActionRestore
	default:
		return ActionUpdate
	}
}

// ValidEntityActionCombinations defines which entity types can have which action types.
// Used for validation and semantic checking.
func ValidEntityActionCombinations() map[EntityType]map[ActionType]bool {
	return map[EntityType]map[ActionType]bool{
		EntityIssues: {
			ActionCreate:    true,
			ActionUpdate:    true,
			ActionDelete:    true,
			ActionSoftDelete: true,
			ActionRestore:   true,
		},
		EntityLogs: {
			ActionCreate: true,
			ActionUpdate: true,
		},
		EntityHandoffs: {
			ActionCreate:    true,
			ActionUpdate:    true,
			ActionDelete:    true,
			ActionSoftDelete: true,
		},
		EntityComments: {
			ActionCreate:    true,
			ActionUpdate:    true,
			ActionDelete:    true,
			ActionSoftDelete: true,
		},
		EntitySessions: {
			ActionCreate: true,
			ActionUpdate: true,
		},
		EntityBoards: {
			ActionCreate:    true,
			ActionUpdate:    true,
			ActionDelete:    true,
			ActionSoftDelete: true,
		},
		EntityBoardIssuePositions: {
			ActionCreate: true,
			ActionDelete: true,
			ActionUpdate: true,
		},
		EntityWorkSessions: {
			ActionCreate:    true,
			ActionUpdate:    true,
			ActionDelete:    true,
			ActionSoftDelete: true,
		},
		EntityWorkSessionIssues: {
			ActionCreate: true,
			ActionDelete: true,
		},
		EntityIssueFiles: {
			ActionCreate: true,
			ActionDelete: true,
		},
		EntityIssueDependencies: {
			ActionCreate: true,
			ActionDelete: true,
		},
		EntityGitSnapshots: {
			ActionCreate: true,
		},
		EntityIssueSessionHistory: {
			ActionCreate: true,
		},
		EntityNotes: {
			ActionCreate:    true,
			ActionUpdate:    true,
			ActionDelete:    true,
			ActionSoftDelete: true,
		},
	}
}

// IsValidEntityActionCombination checks if an entity type can have a given action type.
func IsValidEntityActionCombination(entity EntityType, action ActionType) bool {
	combinations := ValidEntityActionCombinations()
	if actionMap, ok := combinations[entity]; ok {
		return actionMap[action]
	}
	return false
}
