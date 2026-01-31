package syncharness

import (
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	tdsync "github.com/marcus/td/internal/sync"
)

const compProj = "proj-comp"

// ─── Board position sync tests ───

func TestBoardPositionCreate_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	boardID := "bd-board1"
	issueID := "td-issue1"
	posID := db.BoardIssuePosID(boardID, issueID)

	// Client A creates a board position
	err := h.Mutate("client-A", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID,
		"issue_id": issueID,
		"position": 1,
	})
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}

	// Push from A, pull on B
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Pull("client-B", compProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify B has the position
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "board_issue_positions", posID)
		if ent == nil {
			t.Fatalf("%s: board position %s not found", cid, posID)
		}
		if pos, ok := ent["position"].(int64); !ok || pos != 1 {
			t.Fatalf("%s: expected position 1, got %v", cid, ent["position"])
		}
	}
}

func TestBoardPositionUpdate_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	boardID := "bd-board2"
	issueID := "td-issue2"
	posID := db.BoardIssuePosID(boardID, issueID)

	// A creates position
	if err := h.Mutate("client-A", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 1,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Sync to B
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Verify B has it
	if h.QueryEntity("client-B", "board_issue_positions", posID) == nil {
		t.Fatal("client-B should have position after initial sync")
	}

	// A updates position
	if err := h.Mutate("client-A", "update", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 5,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Sync again
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify updated position on both
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "board_issue_positions", posID)
		if ent == nil {
			t.Fatalf("%s: position not found", cid)
		}
		if pos, ok := ent["position"].(int64); !ok || pos != 5 {
			t.Fatalf("%s: expected position 5, got %v", cid, ent["position"])
		}
	}
}

func TestBoardPositionDelete_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	boardID := "bd-board3"
	issueID := "td-issue3"
	posID := db.BoardIssuePosID(boardID, issueID)

	// Create, push, pull
	if err := h.Mutate("client-A", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 3,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Verify both have it
	if h.QueryEntity("client-B", "board_issue_positions", posID) == nil {
		t.Fatal("client-B should have position")
	}

	// A deletes
	if err := h.Mutate("client-A", "delete", "board_issue_positions", posID, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Sync
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify deleted on both
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "board_issue_positions", posID)
		if ent != nil {
			t.Fatalf("%s: position should be deleted, got %v", cid, ent)
		}
	}
}

func TestBoardPosition_LastWriteWins(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	boardID := "bd-board4"
	issueID := "td-issue4"
	posID := db.BoardIssuePosID(boardID, issueID)

	// Both clients create the same board position (same deterministic ID)
	if err := h.Mutate("client-A", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 1,
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := h.Mutate("client-B", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 99,
	}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	// A pushes first, B pushes second (B gets higher server_seq)
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Push("client-B", compProj); err != nil {
		t.Fatalf("push B: %v", err)
	}

	// Both PullAll to converge via server ordering
	if _, err := h.PullAll("client-A", compProj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", compProj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(compProj)

	// B pushed last -> higher server_seq -> B's position=99 wins
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "board_issue_positions", posID)
		if ent == nil {
			t.Fatalf("%s: position not found", cid)
		}
		if pos, ok := ent["position"].(int64); !ok || pos != 99 {
			t.Fatalf("%s: expected position 99 (last write wins), got %v", cid, ent["position"])
		}
	}
}

// ─── Dependency sync tests ───

func TestDependencyAdd_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	issueID := "td-dep1"
	dependsOnID := "td-dep2"
	relType := "depends_on"
	depID := db.DependencyID(issueID, dependsOnID, relType)

	// A adds a dependency
	if err := h.Mutate("client-A", "create", "issue_dependencies", depID, map[string]any{
		"issue_id":      issueID,
		"depends_on_id": dependsOnID,
		"relation_type": relType,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Push from A, pull on B
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Pull("client-B", compProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify both have the dependency
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issue_dependencies", depID)
		if ent == nil {
			t.Fatalf("%s: dependency %s not found", cid, depID)
		}
		if iid, _ := ent["issue_id"].(string); iid != issueID {
			t.Fatalf("%s: expected issue_id %q, got %q", cid, issueID, iid)
		}
		if did, _ := ent["depends_on_id"].(string); did != dependsOnID {
			t.Fatalf("%s: expected depends_on_id %q, got %q", cid, dependsOnID, did)
		}
	}
}

func TestDependencyRemove_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	issueID := "td-depR1"
	dependsOnID := "td-depR2"
	relType := "depends_on"
	depID := db.DependencyID(issueID, dependsOnID, relType)

	// Create dependency, sync
	if err := h.Mutate("client-A", "create", "issue_dependencies", depID, map[string]any{
		"issue_id":      issueID,
		"depends_on_id": dependsOnID,
		"relation_type": relType,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Verify B has it
	if h.QueryEntity("client-B", "issue_dependencies", depID) == nil {
		t.Fatal("client-B should have dependency after sync")
	}

	// A removes dependency
	if err := h.Mutate("client-A", "delete", "issue_dependencies", depID, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Sync
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify deleted on both
	for _, cid := range []string{"client-A", "client-B"} {
		if h.QueryEntity(cid, "issue_dependencies", depID) != nil {
			t.Fatalf("%s: dependency should be removed", cid)
		}
	}
}

// ─── File link sync tests ───

func TestFileLink_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	issueID := "td-fl1"
	filePath := "src/main.go"
	fileID := db.IssueFileID(issueID, filePath)

	// A links a file
	if err := h.Mutate("client-A", "create", "issue_files", fileID, map[string]any{
		"issue_id":  issueID,
		"file_path": filePath,
		"role":      "implementation",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Push from A, pull on B
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Pull("client-B", compProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify both have the file link with correct path
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issue_files", fileID)
		if ent == nil {
			t.Fatalf("%s: file link %s not found", cid, fileID)
		}
		fp, _ := ent["file_path"].(string)
		if fp != filePath {
			t.Fatalf("%s: expected file_path %q, got %q", cid, filePath, fp)
		}
		role, _ := ent["role"].(string)
		if role != "implementation" {
			t.Fatalf("%s: expected role 'implementation', got %q", cid, role)
		}
	}
}

func TestFileLinkRemove_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	issueID := "td-flr1"
	filePath := "pkg/utils.go"
	fileID := db.IssueFileID(issueID, filePath)

	// Create file link, sync
	if err := h.Mutate("client-A", "create", "issue_files", fileID, map[string]any{
		"issue_id":  issueID,
		"file_path": filePath,
		"role":      "implementation",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Verify B has it
	if h.QueryEntity("client-B", "issue_files", fileID) == nil {
		t.Fatal("client-B should have file link after sync")
	}

	// A removes file link
	if err := h.Mutate("client-A", "delete", "issue_files", fileID, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Sync
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify deleted on both
	for _, cid := range []string{"client-A", "client-B"} {
		if h.QueryEntity(cid, "issue_files", fileID) != nil {
			t.Fatalf("%s: file link should be removed", cid)
		}
	}
}

func TestFileLink_RepoRelativePath(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	issueID := "td-flp1"
	// Use forward-slash path (repo-relative)
	filePath := "internal/db/schema.go"
	fileID := db.IssueFileID(issueID, filePath)

	// A links a file with forward-slash path
	if err := h.Mutate("client-A", "create", "issue_files", fileID, map[string]any{
		"issue_id":  issueID,
		"file_path": filePath,
		"role":      "implementation",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Push from A, pull on B
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Pull("client-B", compProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	// Verify synced path uses forward slashes
	ent := h.QueryEntity("client-B", "issue_files", fileID)
	if ent == nil {
		t.Fatal("client-B: file link not found after pull")
	}
	fp, _ := ent["file_path"].(string)
	if strings.Contains(fp, "\\") {
		t.Fatalf("file_path should use forward slashes, got %q", fp)
	}
	if fp != filePath {
		t.Fatalf("expected file_path %q, got %q", filePath, fp)
	}

	// Verify the deterministic ID is the same regardless of how we compute it
	expectedID := db.IssueFileID(issueID, filePath)
	if fileID != expectedID {
		t.Fatalf("deterministic IDs should match: %q != %q", fileID, expectedID)
	}
}

// ─── Conflict test ───

func TestBoardPosition_ConflictRecording(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	boardID := "bd-conflict1"
	issueID := "td-conflict1"
	posID := db.BoardIssuePosID(boardID, issueID)

	// A creates position, sync to both
	if err := h.Mutate("client-A", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 1,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Both clients modify the same position concurrently
	if err := h.Mutate("client-A", "update", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 10,
	}); err != nil {
		t.Fatalf("update A: %v", err)
	}
	if err := h.Mutate("client-B", "update", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 20,
	}); err != nil {
		t.Fatalf("update B: %v", err)
	}

	// A pushes first, then B
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Push("client-B", compProj); err != nil {
		t.Fatalf("push B: %v", err)
	}

	// B pulls A's update manually to inspect ApplyResult for conflict recording
	clientB := h.Clients["client-B"]
	serverDB := h.ProjectDBs[compProj]

	serverTx, err := serverDB.Begin()
	if err != nil {
		t.Fatalf("begin server tx: %v", err)
	}
	pullResult, err := tdsync.GetEventsSince(serverTx, clientB.LastPulledSeq, 10000, clientB.DeviceID)
	if err != nil {
		serverTx.Rollback()
		t.Fatalf("get events: %v", err)
	}
	serverTx.Commit()

	if len(pullResult.Events) == 0 {
		t.Fatal("expected events from A's push")
	}

	clientTx, err := clientB.DB.Begin()
	if err != nil {
		t.Fatalf("begin client tx: %v", err)
	}
	farPast := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	applyResult, err := tdsync.ApplyRemoteEvents(clientTx, pullResult.Events, clientB.DeviceID, h.Validator, &farPast)
	if err != nil {
		clientTx.Rollback()
		t.Fatalf("apply: %v", err)
	}
	clientTx.Commit()

	// Update cursor
	if applyResult.LastAppliedSeq > clientB.LastPulledSeq {
		clientB.LastPulledSeq = applyResult.LastAppliedSeq
	}
	if pullResult.LastServerSeq > clientB.LastPulledSeq {
		clientB.LastPulledSeq = pullResult.LastServerSeq
	}

	// Verify conflict was recorded
	if applyResult.Overwrites < 1 {
		t.Fatalf("expected at least 1 overwrite, got %d", applyResult.Overwrites)
	}
	if len(applyResult.Conflicts) < 1 {
		t.Fatalf("expected at least 1 conflict, got %d", len(applyResult.Conflicts))
	}

	// Find the conflict for our entity
	var found bool
	for _, c := range applyResult.Conflicts {
		if c.EntityType == "board_issue_positions" && c.EntityID == posID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("conflict for board_issue_positions/%s not found in %v", posID, applyResult.Conflicts)
	}
}
