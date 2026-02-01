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

// ─── Board duplicate name sync test ───

func TestBoardDuplicateName_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	boardIDA := "bd-dupA"
	boardIDB := "bd-dupB"
	boardName := "My Board"

	// Client A creates a board with name "My Board"
	err := h.Mutate("client-A", "create", "boards", boardIDA, map[string]any{
		"name":       boardName,
		"query":      "",
		"is_builtin": 0,
		"view_mode":  "swimlanes",
	})
	if err != nil {
		t.Fatalf("mutate A: %v", err)
	}

	// Client B creates a different board with the same name
	err = h.Mutate("client-B", "create", "boards", boardIDB, map[string]any{
		"name":       boardName,
		"query":      "",
		"is_builtin": 0,
		"view_mode":  "swimlanes",
	})
	if err != nil {
		t.Fatalf("mutate B: %v", err)
	}

	// Both push
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Push("client-B", compProj); err != nil {
		t.Fatalf("push B: %v", err)
	}

	// Both PullAll to converge
	if _, err := h.PullAll("client-A", compProj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", compProj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(compProj)

	// Both boards must still exist on both clients (no silent deletion)
	for _, cid := range []string{"client-A", "client-B"} {
		entA := h.QueryEntity(cid, "boards", boardIDA)
		if entA == nil {
			t.Fatalf("%s: board %s was silently deleted", cid, boardIDA)
		}
		entB := h.QueryEntity(cid, "boards", boardIDB)
		if entB == nil {
			t.Fatalf("%s: board %s was silently deleted", cid, boardIDB)
		}

		// Verify both have the same name
		nameA, _ := entA["name"].(string)
		nameB, _ := entB["name"].(string)
		if nameA != boardName {
			t.Fatalf("%s: board A name = %q, want %q", cid, nameA, boardName)
		}
		if nameB != boardName {
			t.Fatalf("%s: board B name = %q, want %q", cid, nameB, boardName)
		}
	}

	// Verify total board count is 2 on both clients
	for _, cid := range []string{"client-A", "client-B"} {
		count := h.CountEntities(cid, "boards")
		if count != 2 {
			t.Fatalf("%s: expected 2 boards, got %d", cid, count)
		}
	}
}

// ─── Work session issue sync tests ───

// ─── Issue file convergence tests ───

func TestIssueFileLink_BothClientsLinkSameFile(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	issueID := "td-ifl-both1"
	filePath := "cmd/root.go"
	fileID := db.IssueFileID(issueID, filePath)

	// Both clients link the identical file to the same issue concurrently
	for _, cid := range []string{"client-A", "client-B"} {
		if err := h.Mutate(cid, "create", "issue_files", fileID, map[string]any{
			"issue_id":  issueID,
			"file_path": filePath,
			"role":      "implementation",
		}); err != nil {
			t.Fatalf("create on %s: %v", cid, err)
		}
	}

	// A pushes first, B pushes second
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Push("client-B", compProj); err != nil {
		t.Fatalf("push B: %v", err)
	}

	// Both PullAll to converge
	if _, err := h.PullAll("client-A", compProj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", compProj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify exactly one row exists on both clients (no duplicates, no silent deletion)
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issue_files", fileID)
		if ent == nil {
			t.Fatalf("%s: file link %s was silently deleted", cid, fileID)
		}
		fp, _ := ent["file_path"].(string)
		if fp != filePath {
			t.Fatalf("%s: expected file_path %q, got %q", cid, filePath, fp)
		}
		count := h.CountEntities(cid, "issue_files")
		if count != 1 {
			t.Fatalf("%s: expected 1 issue_files row, got %d", cid, count)
		}
	}
}

func TestIssueFileLink_PathNormalization(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	issueID := "td-ifl-norm1"
	// NormalizeFilePathForID uses filepath.Clean + filepath.ToSlash.
	// filepath.Clean collapses ".." segments, so these two paths produce the same ID:
	cleanPath := "internal/db/schema.go"
	dirtyPath := "internal/db/../db/schema.go"

	// Both should produce the same deterministic ID after path cleaning
	idClean := db.IssueFileID(issueID, cleanPath)
	idDirty := db.IssueFileID(issueID, dirtyPath)
	if idClean != idDirty {
		t.Fatalf("IssueFileID should normalize paths: clean=%q dirty=%q", idClean, idDirty)
	}

	fileID := idClean

	// Client A links using clean path
	if err := h.Mutate("client-A", "create", "issue_files", fileID, map[string]any{
		"issue_id":  issueID,
		"file_path": cleanPath,
		"role":      "implementation",
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}

	// Client B links using dirty path (same deterministic ID after normalization)
	if err := h.Mutate("client-B", "create", "issue_files", fileID, map[string]any{
		"issue_id":  issueID,
		"file_path": dirtyPath,
		"role":      "implementation",
	}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Both push, then PullAll
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Push("client-B", compProj); err != nil {
		t.Fatalf("push B: %v", err)
	}
	if _, err := h.PullAll("client-A", compProj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", compProj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(compProj)

	// Both clients should have exactly one row for this file link
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issue_files", fileID)
		if ent == nil {
			t.Fatalf("%s: file link not found", cid)
		}
		count := h.CountEntities(cid, "issue_files")
		if count != 1 {
			t.Fatalf("%s: expected 1 issue_files row, got %d", cid, count)
		}
	}
}

func TestIssueFileLink_ConcurrentLinkUnlink(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	issueID := "td-ifl-lu1"
	filePath := "pkg/monitor/tui.go"
	fileID := db.IssueFileID(issueID, filePath)

	// Both clients start with the file link
	for _, cid := range []string{"client-A", "client-B"} {
		if err := h.Mutate(cid, "create", "issue_files", fileID, map[string]any{
			"issue_id":  issueID,
			"file_path": filePath,
			"role":      "implementation",
		}); err != nil {
			t.Fatalf("create on %s: %v", cid, err)
		}
	}

	// Sync so both have the link
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Client A re-links (update with new role), client B unlinks concurrently
	if err := h.Mutate("client-A", "update", "issue_files", fileID, map[string]any{
		"issue_id":  issueID,
		"file_path": filePath,
		"role":      "test",
	}); err != nil {
		t.Fatalf("update A: %v", err)
	}
	if err := h.Mutate("client-B", "delete", "issue_files", fileID, nil); err != nil {
		t.Fatalf("delete B: %v", err)
	}

	// A pushes first, B pushes second (B's delete gets higher server_seq => wins)
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Push("client-B", compProj); err != nil {
		t.Fatalf("push B: %v", err)
	}

	// Both PullAll to converge
	if _, err := h.PullAll("client-A", compProj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", compProj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(compProj)

	// B pushed last (delete) => last-write-wins => file link should be gone
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issue_files", fileID)
		if ent != nil {
			t.Fatalf("%s: file link should be deleted (last-write-wins), got %v", cid, ent)
		}
	}
}

func TestWorkSessionIssueTag_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	wsID := "ws-wsi1"
	issueID := "td-wsi1"
	wsiID := db.WsiID(wsID, issueID)

	// Pre-create work session and issue on both clients
	for _, cid := range []string{"client-A", "client-B"} {
		if err := h.Mutate(cid, "create", "work_sessions", wsID, map[string]any{
			"name": "Test WS", "session_id": "ses-test",
		}); err != nil {
			t.Fatalf("create ws on %s: %v", cid, err)
		}
		if err := h.Mutate(cid, "create", "issues", issueID, map[string]any{
			"title": "Test Issue", "status": "open", "type": "task", "priority": "P2",
		}); err != nil {
			t.Fatalf("create issue on %s: %v", cid, err)
		}
	}

	// Client A tags issue to work session
	if err := h.Mutate("client-A", "create", "work_session_issues", wsiID, map[string]any{
		"work_session_id": wsID,
		"issue_id":        issueID,
	}); err != nil {
		t.Fatalf("tag: %v", err)
	}

	// Push from A, pull on B
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Pull("client-B", compProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify B has the tag
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "work_session_issues", wsiID)
		if ent == nil {
			t.Fatalf("%s: work_session_issue %s not found", cid, wsiID)
		}
		if ws, _ := ent["work_session_id"].(string); ws != wsID {
			t.Fatalf("%s: expected work_session_id %q, got %q", cid, wsID, ws)
		}
		if iid, _ := ent["issue_id"].(string); iid != issueID {
			t.Fatalf("%s: expected issue_id %q, got %q", cid, issueID, iid)
		}
	}
}

func TestWorkSessionIssueUntag_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	wsID := "ws-wsi2"
	issueID := "td-wsi2"
	wsiID := db.WsiID(wsID, issueID)

	// Pre-create work session and issue on both clients
	for _, cid := range []string{"client-A", "client-B"} {
		if err := h.Mutate(cid, "create", "work_sessions", wsID, map[string]any{
			"name": "Test WS", "session_id": "ses-test",
		}); err != nil {
			t.Fatalf("create ws on %s: %v", cid, err)
		}
		if err := h.Mutate(cid, "create", "issues", issueID, map[string]any{
			"title": "Test Issue", "status": "open", "type": "task", "priority": "P2",
		}); err != nil {
			t.Fatalf("create issue on %s: %v", cid, err)
		}
	}

	// Both clients have the tag
	for _, cid := range []string{"client-A", "client-B"} {
		if err := h.Mutate(cid, "create", "work_session_issues", wsiID, map[string]any{
			"work_session_id": wsID,
			"issue_id":        issueID,
		}); err != nil {
			t.Fatalf("tag on %s: %v", cid, err)
		}
	}

	// Client A untags
	if err := h.Mutate("client-A", "delete", "work_session_issues", wsiID, nil); err != nil {
		t.Fatalf("untag: %v", err)
	}

	// Sync
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify deleted on both
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "work_session_issues", wsiID)
		if ent != nil {
			t.Fatalf("%s: work_session_issue should be deleted, got %v", cid, ent)
		}
	}
}

// ─── Board query field sync tests ───

func TestBoardQueryUpdate_Sync(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	boardID := "bd-query1"

	// Client A creates a board with empty query
	err := h.Mutate("client-A", "create", "boards", boardID, map[string]any{
		"name":       "Query Board",
		"query":      "",
		"is_builtin": 0,
		"view_mode":  "swimlanes",
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}

	// Client A updates the board query
	err = h.Mutate("client-A", "update", "boards", boardID, map[string]any{
		"name":       "Query Board",
		"query":      "status:open priority:high",
		"is_builtin": 0,
		"view_mode":  "swimlanes",
	})
	if err != nil {
		t.Fatalf("update board query: %v", err)
	}

	// Push A's events (both create and update) to server
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}

	// Client B pulls — should get both events and end up with updated query
	if _, err := h.Pull("client-B", compProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	h.AssertConverged(compProj)

	// Verify both clients have the updated query
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "boards", boardID)
		if ent == nil {
			t.Fatalf("%s: board %s not found", cid, boardID)
		}
		q, _ := ent["query"].(string)
		if q != "status:open priority:high" {
			t.Fatalf("%s: expected query %q, got %q", cid, "status:open priority:high", q)
		}
	}
}

func TestBoardQueryUpdate_OutOfOrder(t *testing.T) {
	// Tests that board_update (mapped to "create" action) uses INSERT OR REPLACE,
	// so even if the update event is processed, the board data is correct.
	h := NewHarness(t, 2, compProj)

	boardID := "bd-query2"

	// Client A creates board and updates query
	err := h.Mutate("client-A", "create", "boards", boardID, map[string]any{
		"name":       "OOO Board",
		"query":      "",
		"is_builtin": 0,
		"view_mode":  "swimlanes",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = h.Mutate("client-A", "update", "boards", boardID, map[string]any{
		"name":       "OOO Board",
		"query":      "type:bug",
		"is_builtin": 0,
		"view_mode":  "swimlanes",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	// Push both events from A
	if _, err := h.Push("client-A", compProj); err != nil {
		t.Fatalf("push A: %v", err)
	}

	// B pulls both events — board should have updated query
	if _, err := h.Pull("client-B", compProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	h.AssertConverged(compProj)

	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "boards", boardID)
		if ent == nil {
			t.Fatalf("%s: board not found", cid)
		}
		q, _ := ent["query"].(string)
		if q != "type:bug" {
			t.Fatalf("%s: expected query %q, got %q", cid, "type:bug", q)
		}
	}
}

func TestBoardQueryUpdate_ConcurrentEdits_LWW(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	boardID := "bd-query3"

	// Both clients create the same board
	for _, cid := range []string{"client-A", "client-B"} {
		if err := h.Mutate(cid, "create", "boards", boardID, map[string]any{
			"name":       "LWW Board",
			"query":      "",
			"is_builtin": 0,
			"view_mode":  "swimlanes",
		}); err != nil {
			t.Fatalf("create on %s: %v", cid, err)
		}
	}

	// Sync so both have the board
	if err := h.Sync("client-A", compProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", compProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// A sets query to "status:open", B sets query to "priority:high" concurrently
	if err := h.Mutate("client-A", "update", "boards", boardID, map[string]any{
		"name":       "LWW Board",
		"query":      "status:open",
		"is_builtin": 0,
		"view_mode":  "swimlanes",
	}); err != nil {
		t.Fatalf("update A: %v", err)
	}
	if err := h.Mutate("client-B", "update", "boards", boardID, map[string]any{
		"name":       "LWW Board",
		"query":      "priority:high",
		"is_builtin": 0,
		"view_mode":  "swimlanes",
	}); err != nil {
		t.Fatalf("update B: %v", err)
	}

	// A pushes first, B pushes second (B gets higher server_seq => wins LWW)
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

	// B pushed last => higher server_seq => B's query "priority:high" wins
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "boards", boardID)
		if ent == nil {
			t.Fatalf("%s: board not found", cid)
		}
		q, _ := ent["query"].(string)
		if q != "priority:high" {
			t.Fatalf("%s: expected query %q (last-write-wins), got %q", cid, "priority:high", q)
		}
	}
}

func TestWorkSessionIssue_LastWriteWins(t *testing.T) {
	h := NewHarness(t, 2, compProj)

	wsID := "ws-wsi3"
	issueID := "td-wsi3"
	wsiID := db.WsiID(wsID, issueID)

	// Pre-create work session and issue on both clients
	for _, cid := range []string{"client-A", "client-B"} {
		if err := h.Mutate(cid, "create", "work_sessions", wsID, map[string]any{
			"name": "Test WS", "session_id": "ses-test",
		}); err != nil {
			t.Fatalf("create ws on %s: %v", cid, err)
		}
		if err := h.Mutate(cid, "create", "issues", issueID, map[string]any{
			"title": "Test Issue", "status": "open", "type": "task", "priority": "P2",
		}); err != nil {
			t.Fatalf("create issue on %s: %v", cid, err)
		}
	}

	// Both clients tag the same combo (same deterministic ID)
	if err := h.Mutate("client-A", "create", "work_session_issues", wsiID, map[string]any{
		"work_session_id": wsID,
		"issue_id":        issueID,
	}); err != nil {
		t.Fatalf("tag A: %v", err)
	}
	if err := h.Mutate("client-B", "create", "work_session_issues", wsiID, map[string]any{
		"work_session_id": wsID,
		"issue_id":        issueID,
	}); err != nil {
		t.Fatalf("tag B: %v", err)
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

	// Both should have the tag (last write wins = B's create, which is also a create)
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "work_session_issues", wsiID)
		if ent == nil {
			t.Fatalf("%s: work_session_issue not found", cid)
		}
		if ws, _ := ent["work_session_id"].(string); ws != wsID {
			t.Fatalf("%s: expected work_session_id %q, got %q", cid, wsID, ws)
		}
	}
}
