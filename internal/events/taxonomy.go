// Package events defines the centralized event taxonomy for the td sync system.
//
// The event taxonomy provides:
//   - Canonical entity and action types
//   - Backward-compatible normalization of singular/plural entity names
//   - Mapping of legacy td action types to canonical sync action types
//   - Validation of entity+action combinations
//
// # Backward Compatibility
//
// The taxonomy is designed to accept both singular and plural forms of entity type names:
//   - 'issue' and 'issues' both normalize to EntityIssues
//   - 'board' and 'boards' both normalize to EntityBoards
//   - And so on for all entity types
//
// This ensures clients can send events using either old or new naming conventions
// without breaking. The API and sync engine internally normalize to the canonical plural forms.
//
// Legacy action types used by the td action_log are automatically mapped to canonical sync types:
//   - 'handoff' → 'create'
//   - 'add_dependency' → 'create'
//   - 'link_file' → 'create'
//   - 'board_create', 'board_update', 'board_add_issue', 'board_set_position' → 'create'
//   - 'work_session_tag' → 'create'
//   - 'remove_dependency', 'unlink_file', 'board_delete', 'work_session_untag' → 'delete'
//   - 'delete', 'board_unposition', 'board_remove_issue', 'soft_delete' → 'soft_delete'
//   - 'restore' → 'restore'
//   - Issue state transitions ('start', 'review', 'review_approve',
//     'review_changes_requested', 'close_after_review', 'approve', 'reject',
//     'block', 'unblock', 'close', 'reopen', 'board_move_issue') → 'update'
//   - Others default to 'update'
//
// This mapping ensures existing events in the events table with old action/entity types
// can be queried and processed correctly by the sync engine.
package events

import (
	"fmt"
	"strings"
)

// EntityType represents the canonical entity types in the sync system.
type EntityType string

// ActionType represents the canonical action types for events.
type ActionType string

// Canonical entity types
const (
	EntityIssues              EntityType = "issues"
	EntityLogs                EntityType = "logs"
	EntityHandoffs            EntityType = "handoffs"
	EntityComments            EntityType = "comments"
	EntitySessions            EntityType = "sessions"
	EntityBoards              EntityType = "boards"
	EntityBoardIssuePositions EntityType = "board_issue_positions"
	EntityWorkSessions        EntityType = "work_sessions"
	EntityWorkSessionIssues   EntityType = "work_session_issues"
	EntityIssueFiles          EntityType = "issue_files"
	EntityIssueDependencies   EntityType = "issue_dependencies"
	EntityGitSnapshots        EntityType = "git_snapshots"
	EntityIssueSessionHistory EntityType = "issue_session_history"
	EntityIssueReviews        EntityType = "issue_reviews"
	EntityNotes               EntityType = "notes"
)

// Canonical action types
const (
	ActionCreate     ActionType = "create"
	ActionUpdate     ActionType = "update"
	ActionDelete     ActionType = "delete"
	ActionSoftDelete ActionType = "soft_delete"
	ActionRestore    ActionType = "restore"
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
		EntityIssueReviews:        true,
		EntityNotes:               true,
	}
}

// AllActionTypes returns all valid action types.
func AllActionTypes() map[ActionType]bool {
	return map[ActionType]bool{
		ActionCreate:     true,
		ActionUpdate:     true,
		ActionDelete:     true,
		ActionSoftDelete: true,
		ActionRestore:    true,
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
// Handles both singular and plural forms for backward compatibility.
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
	case "issue_review", "issue_reviews":
		return EntityIssueReviews, true
	default:
		return "", false
	}
}

// NormalizeActionType normalizes an action type string to its canonical form.
// Maps td's internal action_log action types to canonical sync event action types.
// Provides backward compatibility for legacy action naming conventions.
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
	case "update", "start", "review", "review_approve", "review_changes_requested",
		"close_after_review", "approve", "reject", "block", "unblock", "close",
		"reopen", "board_move_issue":
		return ActionUpdate
	default:
		return ActionUpdate
	}
}

// EmitEvent normalizes the given entity type and action type strings to their
// canonical forms and validates that the combination is allowed by
// ValidEntityActionCombinations.
//
// It is intentionally side-effect free: callers remain responsible for any
// DB writes / network sends. The helper exists so emit sites can route raw
// strings through a single chokepoint and surface invalid pairings (e.g.
// 'logs' + 'restore', or unknown entity types) at the boundary rather than
// silently dropping them downstream.
//
// Returns the canonical EntityType, the canonical ActionType, and a non-nil
// error when the entity is unknown or the entity+action pair is not in
// ValidEntityActionCombinations.
func EmitEvent(entityType, actionType string) (EntityType, ActionType, error) {
	canonicalEntity, ok := NormalizeEntityType(entityType)
	if !ok {
		return "", "", fmt.Errorf("events: unknown entity type %q", entityType)
	}
	canonicalAction := NormalizeActionType(actionType)
	if !IsValidEntityActionCombination(canonicalEntity, canonicalAction) {
		return canonicalEntity, canonicalAction, fmt.Errorf(
			"events: invalid combination entity=%q action=%q",
			canonicalEntity, canonicalAction,
		)
	}
	return canonicalEntity, canonicalAction, nil
}

// ValidEntityActionCombinations defines which entity types can have which action types.
// Used for validation and semantic checking.
func ValidEntityActionCombinations() map[EntityType]map[ActionType]bool {
	return map[EntityType]map[ActionType]bool{
		EntityIssues: {
			ActionCreate:     true,
			ActionUpdate:     true,
			ActionDelete:     true,
			ActionSoftDelete: true,
			ActionRestore:    true,
		},
		EntityLogs: {
			ActionCreate: true,
			ActionUpdate: true,
		},
		EntityHandoffs: {
			ActionCreate:     true,
			ActionUpdate:     true,
			ActionDelete:     true,
			ActionSoftDelete: true,
		},
		EntityComments: {
			ActionCreate:     true,
			ActionUpdate:     true,
			ActionDelete:     true,
			ActionSoftDelete: true,
		},
		EntitySessions: {
			ActionCreate: true,
			ActionUpdate: true,
		},
		EntityBoards: {
			ActionCreate:     true,
			ActionUpdate:     true,
			ActionDelete:     true,
			ActionSoftDelete: true,
		},
		EntityBoardIssuePositions: {
			ActionCreate: true,
			ActionDelete: true,
			ActionUpdate: true,
		},
		EntityWorkSessions: {
			ActionCreate:     true,
			ActionUpdate:     true,
			ActionDelete:     true,
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
		EntityIssueReviews: {
			// create = new review row; update = supersede_at stamp.
			// Supersede is sent as an UPDATE so peers see superseded_at
			// propagate via the existing partial-update sync path.
			//
			// TODO (Step 2): sync events.go has no per-entity column allowlist
			// for ActionUpdate payloads, so a malicious or buggy peer could
			// theoretically mutate decision / reviewer_session / summary via
			// applyPartialUpdateEvent. When the sync layer gains a column
			// allowlist mechanism, gate issue_reviews to only accept
			// superseded_at updates.
			ActionCreate: true,
			ActionUpdate: true,
		},
		EntityNotes: {
			ActionCreate:     true,
			ActionUpdate:     true,
			ActionDelete:     true,
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
