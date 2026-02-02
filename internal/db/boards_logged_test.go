package db

import (
	"encoding/json"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestCreateBoardLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	board, err := database.CreateBoardLogged("test board", "status:open", "sess-1")
	if err != nil {
		t.Fatalf("CreateBoardLogged failed: %v", err)
	}

	if board.ID == "" {
		t.Fatal("Board ID not set")
	}

	// Verify board row exists
	got, err := database.GetBoard(board.ID)
	if err != nil {
		t.Fatalf("GetBoard failed: %v", err)
	}
	if got.Name != "test board" {
		t.Errorf("Name: got %s, want test board", got.Name)
	}
	if got.Query != "status:open" {
		t.Errorf("Query: got %s, want status:open", got.Query)
	}

	// Verify action_log entry
	var actionType, entityType, entityID, newData, previousData string
	err = database.conn.QueryRow(
		`SELECT action_type, entity_type, entity_id, new_data, previous_data FROM action_log WHERE entity_id = ? AND entity_type = 'board'`,
		board.ID,
	).Scan(&actionType, &entityType, &entityID, &newData, &previousData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "board_create" {
		t.Errorf("action_type: got %s, want board_create", actionType)
	}
	if entityType != "board" {
		t.Errorf("entity_type: got %s, want board", entityType)
	}
	if previousData != "" {
		t.Errorf("previous_data should be empty for create, got %s", previousData)
	}

	// Verify NewData contains correct board data
	var logged models.Board
	if err := json.Unmarshal([]byte(newData), &logged); err != nil {
		t.Fatalf("Unmarshal new_data: %v", err)
	}
	if logged.Name != "test board" {
		t.Errorf("new_data name: got %s, want test board", logged.Name)
	}
	if logged.ID != board.ID {
		t.Errorf("new_data id: got %s, want %s", logged.ID, board.ID)
	}
}

func TestUpdateBoardLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create board first (unlogged)
	board, err := database.CreateBoard("original", "")
	if err != nil {
		t.Fatalf("CreateBoard failed: %v", err)
	}

	// Modify and update with logging
	board.Name = "updated"
	board.Query = "status:closed"
	err = database.UpdateBoardLogged(board, "sess-2")
	if err != nil {
		t.Fatalf("UpdateBoardLogged failed: %v", err)
	}

	// Verify the update applied
	got, err := database.GetBoard(board.ID)
	if err != nil {
		t.Fatalf("GetBoard failed: %v", err)
	}
	if got.Name != "updated" {
		t.Errorf("Name: got %s, want updated", got.Name)
	}

	// Verify action_log entry
	var actionType, previousData, newData string
	err = database.conn.QueryRow(
		`SELECT action_type, previous_data, new_data FROM action_log WHERE entity_id = ? AND entity_type = 'board'`,
		board.ID,
	).Scan(&actionType, &previousData, &newData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "board_update" {
		t.Errorf("action_type: got %s, want board_update", actionType)
	}

	// PreviousData should have the old name
	var prev models.Board
	if err := json.Unmarshal([]byte(previousData), &prev); err != nil {
		t.Fatalf("Unmarshal previous_data: %v", err)
	}
	if prev.Name != "original" {
		t.Errorf("previous_data name: got %s, want original", prev.Name)
	}

	// NewData should have the new name
	var newBoard models.Board
	if err := json.Unmarshal([]byte(newData), &newBoard); err != nil {
		t.Fatalf("Unmarshal new_data: %v", err)
	}
	if newBoard.Name != "updated" {
		t.Errorf("new_data name: got %s, want updated", newBoard.Name)
	}
}

func TestDeleteBoardLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create board first (unlogged)
	board, err := database.CreateBoard("to delete", "")
	if err != nil {
		t.Fatalf("CreateBoard failed: %v", err)
	}

	err = database.DeleteBoardLogged(board.ID, "sess-3")
	if err != nil {
		t.Fatalf("DeleteBoardLogged failed: %v", err)
	}

	// Verify board is deleted
	_, err = database.GetBoard(board.ID)
	if err == nil {
		t.Fatal("Expected error for deleted board, got nil")
	}

	// Verify action_log entry
	var actionType, previousData, newData string
	err = database.conn.QueryRow(
		`SELECT action_type, previous_data, new_data FROM action_log WHERE entity_id = ? AND entity_type = 'board'`,
		board.ID,
	).Scan(&actionType, &previousData, &newData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "board_delete" {
		t.Errorf("action_type: got %s, want board_delete", actionType)
	}
	if newData != "" {
		t.Errorf("new_data should be empty for delete, got %s", newData)
	}

	// PreviousData should have the pre-delete state
	var prev models.Board
	if err := json.Unmarshal([]byte(previousData), &prev); err != nil {
		t.Fatalf("Unmarshal previous_data: %v", err)
	}
	if prev.Name != "to delete" {
		t.Errorf("previous_data name: got %s, want to delete", prev.Name)
	}
}

func TestSetIssuePositionLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create board and issue
	board, err := database.CreateBoard("pos board", "")
	if err != nil {
		t.Fatalf("CreateBoard failed: %v", err)
	}
	issue := &models.Issue{Title: "pos test", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	err = database.SetIssuePositionLogged(board.ID, issue.ID, 100, "sess-4")
	if err != nil {
		t.Fatalf("SetIssuePositionLogged failed: %v", err)
	}

	// Verify position was set
	pos, err := database.GetIssuePosition(board.ID, issue.ID)
	if err != nil {
		t.Fatalf("GetIssuePosition failed: %v", err)
	}
	if pos != 100 {
		t.Errorf("Position: got %d, want 100", pos)
	}

	// Verify action_log entry
	bipID := BoardIssuePosID(board.ID, issue.ID)
	var actionType, entityType, newData string
	err = database.conn.QueryRow(
		`SELECT action_type, entity_type, new_data FROM action_log WHERE entity_id = ? AND entity_type = 'board_issue_positions'`,
		bipID,
	).Scan(&actionType, &entityType, &newData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "board_set_position" {
		t.Errorf("action_type: got %s, want board_set_position", actionType)
	}
	if entityType != "board_issue_positions" {
		t.Errorf("entity_type: got %s, want board_issue_positions", entityType)
	}

	// Verify NewData contains position info
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(newData), &data); err != nil {
		t.Fatalf("Unmarshal new_data: %v", err)
	}
	if data["board_id"] != board.ID {
		t.Errorf("new_data board_id: got %v, want %s", data["board_id"], board.ID)
	}
	if data["issue_id"] != issue.ID {
		t.Errorf("new_data issue_id: got %v, want %s", data["issue_id"], issue.ID)
	}
	if int(data["position"].(float64)) != 100 {
		t.Errorf("new_data position: got %v, want 100", data["position"])
	}
}

func TestRemoveIssuePositionLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create board, issue, and set position (unlogged)
	board, err := database.CreateBoard("unpos board", "")
	if err != nil {
		t.Fatalf("CreateBoard failed: %v", err)
	}
	issue := &models.Issue{Title: "unpos test", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.SetIssuePosition(board.ID, issue.ID, 200); err != nil {
		t.Fatalf("SetIssuePosition failed: %v", err)
	}

	err = database.RemoveIssuePositionLogged(board.ID, issue.ID, "sess-5")
	if err != nil {
		t.Fatalf("RemoveIssuePositionLogged failed: %v", err)
	}

	// Verify position removed (soft-deleted)
	pos, err := database.GetIssuePosition(board.ID, issue.ID)
	if err != nil {
		t.Fatalf("GetIssuePosition failed: %v", err)
	}
	if pos != 0 {
		t.Errorf("Position should be 0 after remove, got %d", pos)
	}

	// Verify action_log entry
	bipID := BoardIssuePosID(board.ID, issue.ID)
	var actionType, entityType, previousData, newData string
	err = database.conn.QueryRow(
		`SELECT action_type, entity_type, previous_data, new_data FROM action_log WHERE entity_id = ? AND entity_type = 'board_issue_positions'`,
		bipID,
	).Scan(&actionType, &entityType, &previousData, &newData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "board_unposition" {
		t.Errorf("action_type: got %s, want board_unposition", actionType)
	}
	if newData != "" {
		t.Errorf("new_data should be empty for unposition, got %s", newData)
	}

	// Verify PreviousData contains position info
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(previousData), &data); err != nil {
		t.Fatalf("Unmarshal previous_data: %v", err)
	}
	if int(data["position"].(float64)) != 200 {
		t.Errorf("previous_data position: got %v, want 200", data["position"])
	}
}

func TestUnloggedBoardVariants_NoActionLog(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// CreateBoard (unlogged)
	board, err := database.CreateBoard("unlogged board", "")
	if err != nil {
		t.Fatalf("CreateBoard failed: %v", err)
	}

	var count int
	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'board'`,
		board.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("CreateBoard (unlogged) created %d action_log entries, want 0", count)
	}

	// UpdateBoard (unlogged)
	board.Name = "updated unlogged"
	if err := database.UpdateBoard(board); err != nil {
		t.Fatalf("UpdateBoard failed: %v", err)
	}

	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'board'`,
		board.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("UpdateBoard (unlogged) created %d action_log entries, want 0", count)
	}

	// SetIssuePosition (unlogged)
	issue := &models.Issue{Title: "pos bypass", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.SetIssuePosition(board.ID, issue.ID, 50); err != nil {
		t.Fatalf("SetIssuePosition failed: %v", err)
	}

	bipID := BoardIssuePosID(board.ID, issue.ID)
	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'board_issue_positions'`,
		bipID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("SetIssuePosition (unlogged) created %d action_log entries, want 0", count)
	}

	// RemoveIssuePosition (unlogged)
	if err := database.RemoveIssuePosition(board.ID, issue.ID); err != nil {
		t.Fatalf("RemoveIssuePosition failed: %v", err)
	}

	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'board_issue_positions'`,
		bipID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("RemoveIssuePosition (unlogged) created %d action_log entries, want 0", count)
	}

	// DeleteBoard (unlogged)
	if err := database.DeleteBoard(board.ID); err != nil {
		t.Fatalf("DeleteBoard failed: %v", err)
	}

	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'board'`,
		board.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("DeleteBoard (unlogged) created %d action_log entries, want 0", count)
	}
}
