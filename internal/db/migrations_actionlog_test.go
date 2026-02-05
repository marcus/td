package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrationActionLogCompositeIDs(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	insertAction := func(id, entityType, entityID string, newData map[string]any) {
		payload, err := json.Marshal(newData)
		if err != nil {
			t.Fatalf("marshal new_data: %v", err)
		}
		_, err = database.conn.Exec(
			`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp, undone)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
			id, "ses-test", "update", entityType, entityID, string(payload), time.Now(),
		)
		if err != nil {
			t.Fatalf("insert action_log %s: %v", id, err)
		}
	}

	insertAction("al-dep", "dependency", "td-a:td-b", map[string]any{
		"issue_id":      "td-a",
		"depends_on_id": "td-b",
		"relation_type": "depends_on",
	})
	insertAction("al-dep-legacy", "dependency", "td-c:td-d", map[string]any{})

	insertAction("al-board", "board_position", "bd-1:td-a", map[string]any{
		"board_id": "bd-1",
		"issue_id": "td-a",
		"position": 1,
	})
	insertAction("al-board-legacy", "board_position", "bd-2:td-b", map[string]any{})

	absPath := filepath.Join(dir, "src", "main.go")
	insertAction("al-file-in", "file_link", absPath, map[string]any{
		"issue_id":   "td-a",
		"file_path":  absPath,
		"role":       "implementation",
		"linked_sha": "abc123",
		"linked_at":  time.Now().Format(time.RFC3339),
	})
	insertAction("al-file-legacy", "file_link", absPath, map[string]any{
		"issue_id": "td-a",
	})

	outsidePath := filepath.Join(os.TempDir(), "outside.go")
	insertAction("al-file-out", "file_link", outsidePath, map[string]any{
		"issue_id":  "td-a",
		"file_path": outsidePath,
		"role":      "implementation",
	})

	if err := database.setSchemaVersionInternal(19); err != nil {
		t.Fatalf("set schema version: %v", err)
	}

	if _, err := database.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var depType, depID, depData string
	if err := database.conn.QueryRow(`SELECT entity_type, entity_id, new_data FROM action_log WHERE id = ?`, "al-dep").Scan(&depType, &depID, &depData); err != nil {
		t.Fatalf("query dep row: %v", err)
	}
	if depType != "issue_dependencies" {
		t.Fatalf("dep entity_type: got %q", depType)
	}
	expDepID := DependencyID("td-a", "td-b", "depends_on")
	if depID != expDepID {
		t.Fatalf("dep entity_id: got %q want %q", depID, expDepID)
	}
	var depFields map[string]any
	if err := json.Unmarshal([]byte(depData), &depFields); err != nil {
		t.Fatalf("dep new_data parse: %v", err)
	}
	if depFields["id"] != expDepID {
		t.Fatalf("dep new_data id: got %v", depFields["id"])
	}

	if err := database.conn.QueryRow(`SELECT entity_type, entity_id, new_data FROM action_log WHERE id = ?`, "al-dep-legacy").Scan(&depType, &depID, &depData); err != nil {
		t.Fatalf("query dep legacy row: %v", err)
	}
	if depType != "issue_dependencies" {
		t.Fatalf("dep legacy entity_type: got %q", depType)
	}
	expDepLegacyID := DependencyID("td-c", "td-d", "depends_on")
	if depID != expDepLegacyID {
		t.Fatalf("dep legacy entity_id: got %q want %q", depID, expDepLegacyID)
	}

	var boardType, boardID, boardData string
	if err := database.conn.QueryRow(`SELECT entity_type, entity_id, new_data FROM action_log WHERE id = ?`, "al-board").Scan(&boardType, &boardID, &boardData); err != nil {
		t.Fatalf("query board row: %v", err)
	}
	if boardType != "board_issue_positions" {
		t.Fatalf("board entity_type: got %q", boardType)
	}
	expBoardID := BoardIssuePosID("bd-1", "td-a")
	if boardID != expBoardID {
		t.Fatalf("board entity_id: got %q want %q", boardID, expBoardID)
	}
	var boardFields map[string]any
	if err := json.Unmarshal([]byte(boardData), &boardFields); err != nil {
		t.Fatalf("board new_data parse: %v", err)
	}
	if boardFields["id"] != expBoardID {
		t.Fatalf("board new_data id: got %v", boardFields["id"])
	}

	if err := database.conn.QueryRow(`SELECT entity_type, entity_id, new_data FROM action_log WHERE id = ?`, "al-board-legacy").Scan(&boardType, &boardID, &boardData); err != nil {
		t.Fatalf("query board legacy row: %v", err)
	}
	if boardType != "board_issue_positions" {
		t.Fatalf("board legacy entity_type: got %q", boardType)
	}
	expBoardLegacyID := BoardIssuePosID("bd-2", "td-b")
	if boardID != expBoardLegacyID {
		t.Fatalf("board legacy entity_id: got %q want %q", boardID, expBoardLegacyID)
	}

	var fileType, fileID, fileData string
	if err := database.conn.QueryRow(`SELECT entity_type, entity_id, new_data FROM action_log WHERE id = ?`, "al-file-in").Scan(&fileType, &fileID, &fileData); err != nil {
		t.Fatalf("query file row: %v", err)
	}
	if fileType != "issue_files" {
		t.Fatalf("file entity_type: got %q", fileType)
	}
	var fileFields map[string]any
	if err := json.Unmarshal([]byte(fileData), &fileFields); err != nil {
		t.Fatalf("file new_data parse: %v", err)
	}
	relPath := NormalizeFilePathForID(filepath.ToSlash(filepath.Join("src", "main.go")))
	expFileID := IssueFileID("td-a", relPath)
	if fileID != expFileID {
		t.Fatalf("file entity_id: got %q want %q", fileID, expFileID)
	}
	if fileFields["file_path"] != relPath {
		t.Fatalf("file new_data path: got %v want %s", fileFields["file_path"], relPath)
	}
	if fileFields["id"] != expFileID {
		t.Fatalf("file new_data id: got %v", fileFields["id"])
	}

	if err := database.conn.QueryRow(`SELECT entity_type, entity_id, new_data FROM action_log WHERE id = ?`, "al-file-legacy").Scan(&fileType, &fileID, &fileData); err != nil {
		t.Fatalf("query file legacy row: %v", err)
	}
	if fileType != "issue_files" {
		t.Fatalf("file legacy entity_type: got %q", fileType)
	}
	if fileID != expFileID {
		t.Fatalf("file legacy entity_id: got %q want %q", fileID, expFileID)
	}

	var syncedAt string
	if err := database.conn.QueryRow(`SELECT COALESCE(synced_at, '') FROM action_log WHERE id = ?`, "al-file-out").Scan(&syncedAt); err != nil {
		t.Fatalf("query outside file row: %v", err)
	}
	if syncedAt == "" {
		t.Fatalf("expected al-file-out to be marked synced")
	}
}
