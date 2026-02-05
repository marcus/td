package syncharness

import (
	"testing"

	"github.com/marcus/td/internal/db"
)

const softDelProj = "proj-soft-del"

// TestBoardPositionSoftDelete_SyncConvergence verifies that soft-deleting a
// board_issue_position on one client propagates correctly to another client
// and prevents resurrection when a stale create arrives after a delete.
func TestBoardPositionSoftDelete_SyncConvergence(t *testing.T) {
	h := NewHarness(t, 2, softDelProj)

	boardID := "bd-sdboard"
	issueID := "td-sdissue"
	posID := db.BoardIssuePosID(boardID, issueID)

	// Client A creates a board position
	if err := h.Mutate("client-A", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 65536,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Sync A->server->B so both have the position
	if err := h.Sync("client-A", softDelProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", softDelProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Verify both have the position
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "board_issue_positions", posID)
		if ent == nil {
			t.Fatalf("%s: position missing after initial sync", cid)
		}
	}

	// Client A soft-deletes the position
	if err := h.Mutate("client-A", "soft_delete", "board_issue_positions", posID, nil); err != nil {
		t.Fatalf("soft_delete: %v", err)
	}

	// Sync A->server->B
	if err := h.Sync("client-A", softDelProj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", softDelProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	// Both should have deleted_at set (row exists but soft-deleted)
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntityRaw(cid, "board_issue_positions", posID)
		if ent == nil {
			t.Fatalf("%s: row should still exist (soft-deleted)", cid)
		}
		if ent["deleted_at"] == nil {
			t.Fatalf("%s: deleted_at should be set after soft delete", cid)
		}
	}

	h.AssertConverged(softDelProj)
}

// TestBoardPositionSoftDelete_NoResurrection verifies that a stale create
// event does not resurrect a soft-deleted position. The soft_delete event
// with a higher server_seq should win.
func TestBoardPositionSoftDelete_NoResurrection(t *testing.T) {
	h := NewHarness(t, 2, softDelProj)

	boardID := "bd-resurrect"
	issueID := "td-resiss"
	posID := db.BoardIssuePosID(boardID, issueID)

	// Client A creates position and syncs to B
	if err := h.Mutate("client-A", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 65536,
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := h.Sync("client-A", softDelProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", softDelProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Client A soft-deletes the position (offline from B's perspective)
	if err := h.Mutate("client-A", "soft_delete", "board_issue_positions", posID, nil); err != nil {
		t.Fatalf("soft_delete A: %v", err)
	}

	// Client B (unaware of delete) re-creates the same position
	if err := h.Mutate("client-B", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 131072,
	}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Push A first (soft_delete gets lower server_seq), then push B (create gets higher server_seq)
	if _, err := h.Push("client-A", softDelProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Push("client-B", softDelProj); err != nil {
		t.Fatalf("push B: %v", err)
	}

	// Pull all on both clients to reach convergence
	if _, err := h.PullAll("client-A", softDelProj); err != nil {
		t.Fatalf("pull A: %v", err)
	}
	if _, err := h.PullAll("client-B", softDelProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	// Both should converge -- the create (higher server_seq) wins over soft_delete
	h.AssertConverged(softDelProj)

	// The row should exist with the create's position value (B's create had higher seq)
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "board_issue_positions", posID)
		if ent == nil {
			t.Fatalf("%s: position should exist (create won)", cid)
		}
	}
}

// TestBoardDeleteCascadesPositionSync verifies that when one client deletes
// a board, the cascaded soft-deletes of board_issue_positions are properly
// synced to other clients.
func TestBoardDeleteCascadesPositionSync(t *testing.T) {
	h := NewHarness(t, 2, softDelProj)

	boardID := "bd-cascade"
	issueID1 := "td-casc1"
	issueID2 := "td-casc2"
	posID1 := db.BoardIssuePosID(boardID, issueID1)
	posID2 := db.BoardIssuePosID(boardID, issueID2)

	// Client A creates a board
	if err := h.Mutate("client-A", "create", "boards", boardID, map[string]any{
		"name": "Cascade board",
	}); err != nil {
		t.Fatalf("create board: %v", err)
	}

	// Client A creates 2 issues
	if err := h.Mutate("client-A", "create", "issues", issueID1, map[string]any{
		"title": "Issue 1", "status": "open",
	}); err != nil {
		t.Fatalf("create issue 1: %v", err)
	}
	if err := h.Mutate("client-A", "create", "issues", issueID2, map[string]any{
		"title": "Issue 2", "status": "open",
	}); err != nil {
		t.Fatalf("create issue 2: %v", err)
	}

	// Client A positions both issues on the board
	if err := h.Mutate("client-A", "create", "board_issue_positions", posID1, map[string]any{
		"board_id": boardID, "issue_id": issueID1, "position": 65536,
	}); err != nil {
		t.Fatalf("create pos 1: %v", err)
	}
	if err := h.Mutate("client-A", "create", "board_issue_positions", posID2, map[string]any{
		"board_id": boardID, "issue_id": issueID2, "position": 131072,
	}); err != nil {
		t.Fatalf("create pos 2: %v", err)
	}

	// Sync: A pushes, B pulls — verify B has the positions
	if err := h.Sync("client-A", softDelProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", softDelProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	for _, cid := range []string{"client-A", "client-B"} {
		for _, pid := range []string{posID1, posID2} {
			ent := h.QueryEntity(cid, "board_issue_positions", pid)
			if ent == nil {
				t.Fatalf("%s: position %s missing after initial sync", cid, pid)
			}
			if ent["deleted_at"] != nil {
				t.Fatalf("%s: position %s should not be soft-deleted yet", cid, pid)
			}
		}
	}

	// Client A deletes the board — cascade: soft-delete both positions, then
	// hard-delete board. Mirrors DeleteBoardLogged which logs "board_unposition"
	// for each position and "board_delete" for the board.
	// Use soft_delete for positions (mapActionType("soft_delete") = "soft_delete").
	if err := h.Mutate("client-A", "soft_delete", "board_issue_positions", posID1, nil); err != nil {
		t.Fatalf("soft_delete pos 1: %v", err)
	}
	if err := h.Mutate("client-A", "soft_delete", "board_issue_positions", posID2, nil); err != nil {
		t.Fatalf("soft_delete pos 2: %v", err)
	}
	// For the board, use Mutate "delete" (local hard DELETE), then patch the
	// action_log entry to "board_delete" so mapActionType produces "delete"
	// on remote (boards have no deleted_at column, so soft_delete would fail).
	if err := h.Mutate("client-A", "delete", "boards", boardID, nil); err != nil {
		t.Fatalf("delete board: %v", err)
	}
	// Patch the action_log: change "delete" → "board_delete" for the board entry
	// so the sync engine maps it to hard delete on the remote client.
	_, err := h.Clients["client-A"].DB.Exec(
		`UPDATE action_log SET action_type = 'board_delete' WHERE entity_type = 'boards' AND entity_id = ? AND action_type = 'delete'`,
		boardID,
	)
	if err != nil {
		t.Fatalf("patch action_log for board_delete: %v", err)
	}

	// A pushes, B pulls
	if _, err := h.Push("client-A", softDelProj); err != nil {
		t.Fatalf("push A2: %v", err)
	}
	if _, err := h.Pull("client-B", softDelProj); err != nil {
		t.Fatalf("pull B2: %v", err)
	}

	// Assert converged
	h.AssertConverged(softDelProj)

	// Verify B has both positions soft-deleted (row exists with deleted_at set)
	for _, cid := range []string{"client-A", "client-B"} {
		for _, pid := range []string{posID1, posID2} {
			ent := h.QueryEntityRaw(cid, "board_issue_positions", pid)
			if ent == nil {
				t.Fatalf("%s: position %s row should still exist (soft-deleted)", cid, pid)
			}
			if ent["deleted_at"] == nil {
				t.Fatalf("%s: position %s deleted_at should be set after board delete cascade", cid, pid)
			}
		}
	}

	// Verify B has the board deleted
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "boards", boardID)
		if ent != nil {
			t.Fatalf("%s: board should be deleted, got %v", cid, ent)
		}
	}

	// Count active (non-soft-deleted) positions for the board on B
	var activeCount int
	err = h.Clients["client-B"].DB.QueryRow(
		`SELECT COUNT(*) FROM board_issue_positions WHERE board_id = ? AND deleted_at IS NULL`,
		boardID,
	).Scan(&activeCount)
	if err != nil {
		t.Fatalf("count active positions: %v", err)
	}
	if activeCount != 0 {
		t.Fatalf("client-B: expected 0 active positions for deleted board, got %d", activeCount)
	}
}

// TestBoardPositionSoftDelete_ThenReposition verifies that re-adding a
// position after soft delete clears deleted_at properly.
func TestBoardPositionSoftDelete_ThenReposition(t *testing.T) {
	h := NewHarness(t, 2, softDelProj)

	boardID := "bd-repos"
	issueID := "td-reposiss"
	posID := db.BoardIssuePosID(boardID, issueID)

	// Client A creates position
	if err := h.Mutate("client-A", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 65536,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Sync to B
	if err := h.Sync("client-A", softDelProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", softDelProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Client A soft-deletes, then re-creates with new position
	if err := h.Mutate("client-A", "soft_delete", "board_issue_positions", posID, nil); err != nil {
		t.Fatalf("soft_delete: %v", err)
	}
	if err := h.Mutate("client-A", "create", "board_issue_positions", posID, map[string]any{
		"board_id": boardID, "issue_id": issueID, "position": 131072,
	}); err != nil {
		t.Fatalf("re-create: %v", err)
	}

	// Sync both
	if err := h.Sync("client-A", softDelProj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", softDelProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	// Both should have the new position with deleted_at cleared
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "board_issue_positions", posID)
		if ent == nil {
			t.Fatalf("%s: position should exist after re-create", cid)
		}
		if ent["deleted_at"] != nil {
			t.Fatalf("%s: deleted_at should be nil after re-create, got %v", cid, ent["deleted_at"])
		}
		if pos, ok := ent["position"].(int64); !ok || pos != 131072 {
			t.Fatalf("%s: expected position 131072, got %v", cid, ent["position"])
		}
	}

	h.AssertConverged(softDelProj)
}
