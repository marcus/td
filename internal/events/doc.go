// Package events defines the centralized event taxonomy for the td sync
// system, standardizing entity and action types across the codebase.
//
// It provides canonical definitions for entity types (issues, boards,
// comments, etc.) and action types (create, update, delete, soft_delete,
// restore), along with normalization functions that handle both singular
// and plural entity name variants for backward compatibility. The package
// also includes legacy action type mapping from old td action_log naming
// conventions and validation functions to ensure only valid entity-action
// combinations are permitted.
package events
