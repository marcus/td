package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/marcus/td/internal/models"
)

// marshalDependency returns a JSON representation of a dependency row for action_log storage.
func marshalDependency(id, issueID, dependsOnID, relationType string) string {
	data, _ := json.Marshal(map[string]string{
		"id": id, "issue_id": issueID, "depends_on_id": dependsOnID, "relation_type": relationType,
	})
	return string(data)
}

// AddDependencyLogged adds a dependency and logs the action atomically within a single withWriteLock call.
func (db *DB) AddDependencyLogged(issueID, dependsOnID, relationType, sessionID string) error {
	return db.withWriteLock(func() error {
		depID := DependencyID(issueID, dependsOnID, relationType)
		_, err := db.conn.Exec(`
			INSERT OR REPLACE INTO issue_dependencies (id, issue_id, depends_on_id, relation_type)
			VALUES (?, ?, ?, ?)
		`, depID, issueID, dependsOnID, relationType)
		if err != nil {
			return err
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		now := time.Now()
		newData := marshalDependency(depID, issueID, dependsOnID, relationType)
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionAddDep), "issue_dependencies", depID, "", newData, now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// RemoveDependencyLogged removes a dependency and logs the action atomically within a single withWriteLock call.
func (db *DB) RemoveDependencyLogged(issueID, dependsOnID, sessionID string) error {
	return db.withWriteLock(func() error {
		// Capture row data before deletion
		depID := DependencyID(issueID, dependsOnID, "depends_on")
		previousData := marshalDependency(depID, issueID, dependsOnID, "depends_on")

		_, err := db.conn.Exec(`DELETE FROM issue_dependencies WHERE issue_id = ? AND depends_on_id = ?`, issueID, dependsOnID)
		if err != nil {
			return err
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		now := time.Now()
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionRemoveDep), "issue_dependencies", depID, previousData, "", now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}
