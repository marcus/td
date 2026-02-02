package db

import (
	"encoding/json"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestAddDependencyLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create two issues
	issue1 := &models.Issue{Title: "dep parent", Type: models.TypeTask, Priority: models.PriorityP2}
	issue2 := &models.Issue{Title: "dep child", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	err = database.AddDependencyLogged(issue1.ID, issue2.ID, "depends_on", "sess-1")
	if err != nil {
		t.Fatalf("AddDependencyLogged failed: %v", err)
	}

	// Verify dependency row exists
	deps, err := database.GetDependencies(issue1.ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}
	if len(deps) != 1 || deps[0] != issue2.ID {
		t.Errorf("Dependencies: got %v, want [%s]", deps, issue2.ID)
	}

	// Verify action_log entry
	depID := DependencyID(issue1.ID, issue2.ID, "depends_on")
	var actionType, entityType, newData, previousData string
	err = database.conn.QueryRow(
		`SELECT action_type, entity_type, new_data, previous_data FROM action_log WHERE entity_id = ? AND entity_type = 'issue_dependencies'`,
		depID,
	).Scan(&actionType, &entityType, &newData, &previousData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "add_dependency" {
		t.Errorf("action_type: got %s, want add_dependency", actionType)
	}
	if entityType != "issue_dependencies" {
		t.Errorf("entity_type: got %s, want issue_dependencies", entityType)
	}
	if previousData != "" {
		t.Errorf("previous_data should be empty for add, got %s", previousData)
	}

	// Verify NewData contains correct dependency data
	var data map[string]string
	if err := json.Unmarshal([]byte(newData), &data); err != nil {
		t.Fatalf("Unmarshal new_data: %v", err)
	}
	if data["issue_id"] != issue1.ID {
		t.Errorf("new_data issue_id: got %s, want %s", data["issue_id"], issue1.ID)
	}
	if data["depends_on_id"] != issue2.ID {
		t.Errorf("new_data depends_on_id: got %s, want %s", data["depends_on_id"], issue2.ID)
	}
	if data["relation_type"] != "depends_on" {
		t.Errorf("new_data relation_type: got %s, want depends_on", data["relation_type"])
	}
}

func TestRemoveDependencyLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create two issues and add dependency (unlogged)
	issue1 := &models.Issue{Title: "rem dep parent", Type: models.TypeTask, Priority: models.PriorityP2}
	issue2 := &models.Issue{Title: "rem dep child", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.AddDependency(issue1.ID, issue2.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	err = database.RemoveDependencyLogged(issue1.ID, issue2.ID, "sess-2")
	if err != nil {
		t.Fatalf("RemoveDependencyLogged failed: %v", err)
	}

	// Verify dependency removed
	deps, err := database.GetDependencies(issue1.ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("Dependencies should be empty after remove, got %v", deps)
	}

	// Verify action_log entry
	depID := DependencyID(issue1.ID, issue2.ID, "depends_on")
	var actionType, previousData, newData string
	err = database.conn.QueryRow(
		`SELECT action_type, previous_data, new_data FROM action_log WHERE entity_id = ? AND entity_type = 'issue_dependencies'`,
		depID,
	).Scan(&actionType, &previousData, &newData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "remove_dependency" {
		t.Errorf("action_type: got %s, want remove_dependency", actionType)
	}
	if newData != "" {
		t.Errorf("new_data should be empty for remove, got %s", newData)
	}

	// Verify PreviousData contains dependency data
	var data map[string]string
	if err := json.Unmarshal([]byte(previousData), &data); err != nil {
		t.Fatalf("Unmarshal previous_data: %v", err)
	}
	if data["issue_id"] != issue1.ID {
		t.Errorf("previous_data issue_id: got %s, want %s", data["issue_id"], issue1.ID)
	}
	if data["depends_on_id"] != issue2.ID {
		t.Errorf("previous_data depends_on_id: got %s, want %s", data["depends_on_id"], issue2.ID)
	}
}

func TestLinkFileLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "link file test", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	err = database.LinkFileLogged(issue.ID, "main.go", models.FileRoleImplementation, "abc123", "sess-3")
	if err != nil {
		t.Fatalf("LinkFileLogged failed: %v", err)
	}

	// Verify file link exists
	files, err := database.GetLinkedFiles(issue.ID)
	if err != nil {
		t.Fatalf("GetLinkedFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Expected 1 linked file, got %d", len(files))
	}
	if files[0].FilePath != "main.go" {
		t.Errorf("FilePath: got %s, want main.go", files[0].FilePath)
	}

	// Verify action_log entry
	fileID := IssueFileID(issue.ID, "main.go")
	var actionType, entityType, newData, previousData string
	err = database.conn.QueryRow(
		`SELECT action_type, entity_type, new_data, previous_data FROM action_log WHERE entity_id = ? AND entity_type = 'issue_files'`,
		fileID,
	).Scan(&actionType, &entityType, &newData, &previousData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "link_file" {
		t.Errorf("action_type: got %s, want link_file", actionType)
	}
	if entityType != "issue_files" {
		t.Errorf("entity_type: got %s, want issue_files", entityType)
	}
	if previousData != "" {
		t.Errorf("previous_data should be empty for link, got %s", previousData)
	}

	// Verify NewData contains file link data
	var data map[string]string
	if err := json.Unmarshal([]byte(newData), &data); err != nil {
		t.Fatalf("Unmarshal new_data: %v", err)
	}
	if data["issue_id"] != issue.ID {
		t.Errorf("new_data issue_id: got %s, want %s", data["issue_id"], issue.ID)
	}
	if data["file_path"] != "main.go" {
		t.Errorf("new_data file_path: got %s, want main.go", data["file_path"])
	}
	if data["role"] != "implementation" {
		t.Errorf("new_data role: got %s, want implementation", data["role"])
	}
}

func TestUnlinkFileLogged(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "unlink file test", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Link file first (unlogged)
	if err := database.LinkFile(issue.ID, "test.go", models.FileRoleTest, "def456"); err != nil {
		t.Fatalf("LinkFile failed: %v", err)
	}

	err = database.UnlinkFileLogged(issue.ID, "test.go", "sess-4")
	if err != nil {
		t.Fatalf("UnlinkFileLogged failed: %v", err)
	}

	// Verify file link removed
	files, err := database.GetLinkedFiles(issue.ID)
	if err != nil {
		t.Fatalf("GetLinkedFiles failed: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("Expected 0 linked files after unlink, got %d", len(files))
	}

	// Verify action_log entry
	fileID := IssueFileID(issue.ID, "test.go")
	var actionType, previousData, newData string
	err = database.conn.QueryRow(
		`SELECT action_type, previous_data, new_data FROM action_log WHERE entity_id = ? AND entity_type = 'issue_files'`,
		fileID,
	).Scan(&actionType, &previousData, &newData)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}

	if actionType != "unlink_file" {
		t.Errorf("action_type: got %s, want unlink_file", actionType)
	}
	if newData != "" {
		t.Errorf("new_data should be empty for unlink, got %s", newData)
	}

	// Verify PreviousData contains file data
	var data map[string]string
	if err := json.Unmarshal([]byte(previousData), &data); err != nil {
		t.Fatalf("Unmarshal previous_data: %v", err)
	}
	if data["issue_id"] != issue.ID {
		t.Errorf("previous_data issue_id: got %s, want %s", data["issue_id"], issue.ID)
	}
	if data["file_path"] != "test.go" {
		t.Errorf("previous_data file_path: got %s, want test.go", data["file_path"])
	}
}

func TestUnloggedRelationVariants_NoActionLog(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issues for dependency tests
	issue1 := &models.Issue{Title: "unlog dep 1", Type: models.TypeTask, Priority: models.PriorityP2}
	issue2 := &models.Issue{Title: "unlog dep 2", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// AddDependency (unlogged)
	if err := database.AddDependency(issue1.ID, issue2.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	depID := DependencyID(issue1.ID, issue2.ID, "depends_on")
	var count int
	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'issue_dependencies'`,
		depID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("AddDependency (unlogged) created %d action_log entries, want 0", count)
	}

	// RemoveDependency (unlogged)
	if err := database.RemoveDependency(issue1.ID, issue2.ID); err != nil {
		t.Fatalf("RemoveDependency failed: %v", err)
	}

	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'issue_dependencies'`,
		depID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("RemoveDependency (unlogged) created %d action_log entries, want 0", count)
	}

	// LinkFile (unlogged)
	if err := database.LinkFile(issue1.ID, "unlogged.go", models.FileRoleImplementation, "sha1"); err != nil {
		t.Fatalf("LinkFile failed: %v", err)
	}

	fileID := IssueFileID(issue1.ID, "unlogged.go")
	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'issue_files'`,
		fileID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("LinkFile (unlogged) created %d action_log entries, want 0", count)
	}

	// UnlinkFile (unlogged)
	if err := database.UnlinkFile(issue1.ID, "unlogged.go"); err != nil {
		t.Fatalf("UnlinkFile failed: %v", err)
	}

	err = database.conn.QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'issue_files'`,
		fileID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("UnlinkFile (unlogged) created %d action_log entries, want 0", count)
	}
}
