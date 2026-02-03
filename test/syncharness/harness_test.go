package syncharness

import (
	"fmt"
	"sync"
	"testing"
	"time"

	tdsync "github.com/marcus/td/internal/sync"
)

const proj = "proj-1"

func TestSingleClientCreate(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// Client A creates an issue
	err := h.Mutate("client-A", "create", "issues", "td-001", map[string]any{
		"title":  "Fix bug",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}

	// Client A pushes
	_, err = h.Push("client-A", proj)
	if err != nil {
		t.Fatalf("push: %v", err)
	}

	// Client B pulls
	_, err = h.Pull("client-B", proj)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}

	// Assert convergence
	h.AssertConverged(proj)

	// Verify both have td-001 with title "Fix bug"
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-001")
		if ent == nil {
			t.Fatalf("%s: td-001 not found", cid)
		}
		title, _ := ent["title"].(string)
		if title != "Fix bug" {
			t.Fatalf("%s: expected title 'Fix bug', got %q", cid, title)
		}
	}
}

func TestTwoClientsNoConflict(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// Client A creates issue X
	err := h.Mutate("client-A", "create", "issues", "td-X", map[string]any{
		"title":  "Issue X",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("mutate A: %v", err)
	}

	// Client B creates issue Y
	err = h.Mutate("client-B", "create", "issues", "td-Y", map[string]any{
		"title":  "Issue Y",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("mutate B: %v", err)
	}

	// Both sync
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B: %v", err)
	}
	// A needs another pull to get B's data
	if _, err := h.Pull("client-A", proj); err != nil {
		t.Fatalf("pull A: %v", err)
	}

	h.AssertConverged(proj)

	// Both should have X and Y
	for _, cid := range []string{"client-A", "client-B"} {
		if h.CountEntities(cid, "issues") != 2 {
			t.Fatalf("%s: expected 2 issues, got %d", cid, h.CountEntities(cid, "issues"))
		}
		x := h.QueryEntity(cid, "issues", "td-X")
		y := h.QueryEntity(cid, "issues", "td-Y")
		if x == nil || y == nil {
			t.Fatalf("%s: missing issue X or Y", cid)
		}
	}
}

func TestUpdatePropagation(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// Client A creates issue
	err := h.Mutate("client-A", "create", "issues", "td-U1", map[string]any{
		"title":  "Original",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Both sync
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Verify B has the issue
	ent := h.QueryEntity("client-B", "issues", "td-U1")
	if ent == nil {
		t.Fatal("client-B should have td-U1 after sync")
	}

	// Client A updates the title
	err = h.Mutate("client-A", "update", "issues", "td-U1", map[string]any{
		"title":  "New title",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	// Both sync again
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	h.AssertConverged(proj)

	// Both should see "New title"
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-U1")
		if ent == nil {
			t.Fatalf("%s: td-U1 not found", cid)
		}
		title, _ := ent["title"].(string)
		if title != "New title" {
			t.Fatalf("%s: expected 'New title', got %q", cid, title)
		}
	}
}

func TestDeletePropagation(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// Client A creates issue
	err := h.Mutate("client-A", "create", "issues", "td-D1", map[string]any{
		"title":  "To delete",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Both sync
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Verify both have it
	if h.QueryEntity("client-B", "issues", "td-D1") == nil {
		t.Fatal("client-B should have td-D1")
	}

	// Client A deletes the issue
	err = h.Mutate("client-A", "delete", "issues", "td-D1", nil)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Both sync
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	h.AssertConverged(proj)

	// Neither should have it
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-D1")
		if ent != nil {
			t.Fatalf("%s: td-D1 should be deleted but found: %v", cid, ent)
		}
	}
}

func TestLastWriteWins(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// Client A creates issue
	err := h.Mutate("client-A", "create", "issues", "td-LWW", map[string]any{
		"title":  "Original",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Both sync so B has the issue
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Both update concurrently (no sync between)
	err = h.Mutate("client-A", "update", "issues", "td-LWW", map[string]any{
		"title":  "A",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("update A: %v", err)
	}
	err = h.Mutate("client-B", "update", "issues", "td-LWW", map[string]any{
		"title":  "B",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("update B: %v", err)
	}

	// A pushes first, then B pushes — B gets higher server_seq
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Push("client-B", proj); err != nil {
		t.Fatalf("push B: %v", err)
	}

	// Both pull ALL events (including own) to converge via server ordering.
	// Events are applied in server_seq order; B pushed last so its event
	// has the highest seq and is applied last on both clients.
	if _, err := h.PullAll("client-A", proj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", proj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(proj)

	// B pushed last → higher server_seq → applied last → "B" wins
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-LWW")
		if ent == nil {
			t.Fatalf("%s: td-LWW not found", cid)
		}
		title, _ := ent["title"].(string)
		if title != "B" {
			t.Fatalf("%s: expected title 'B' (last write wins), got %q", cid, title)
		}
	}
}

// ─── Test 6a: Delete arrives last → entity deleted ───

func TestCreateDeleteConflict_DeleteLast(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates issue
	if err := h.Mutate("client-A", "create", "issues", "td-CD1", map[string]any{
		"title": "Will be deleted", "status": "open",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Both sync so B has it
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B: %v", err)
	}

	// A updates, B deletes (no sync between)
	if err := h.Mutate("client-A", "update", "issues", "td-CD1", map[string]any{
		"title": "Updated by A", "status": "open",
	}); err != nil {
		t.Fatalf("update A: %v", err)
	}
	if err := h.Mutate("client-B", "delete", "issues", "td-CD1", nil); err != nil {
		t.Fatalf("delete B: %v", err)
	}

	// A pushes update first, B pushes delete second (higher server_seq)
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Push("client-B", proj); err != nil {
		t.Fatalf("push B: %v", err)
	}

	// Both PullAll for convergence
	if _, err := h.PullAll("client-A", proj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", proj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(proj)

	// Delete arrived last → entity should be gone
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-CD1")
		if ent != nil {
			t.Fatalf("%s: td-CD1 should be deleted, got %v", cid, ent)
		}
	}
}

// ─── Test 6b: Delete wins — update does not resurrect deleted entity ───

func TestCreateDeleteConflict_UpdateLast(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates issue
	if err := h.Mutate("client-A", "create", "issues", "td-CD2", map[string]any{
		"title": "Will stay deleted", "status": "open",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Both sync
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B: %v", err)
	}

	// B deletes, A updates (no sync between)
	if err := h.Mutate("client-B", "delete", "issues", "td-CD2", nil); err != nil {
		t.Fatalf("delete B: %v", err)
	}
	if err := h.Mutate("client-A", "update", "issues", "td-CD2", map[string]any{
		"title": "Resurrected by A", "status": "in_progress",
	}); err != nil {
		t.Fatalf("update A: %v", err)
	}

	// B pushes delete first, A pushes update second (higher server_seq)
	if _, err := h.Push("client-B", proj); err != nil {
		t.Fatalf("push B: %v", err)
	}
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A: %v", err)
	}

	// Both PullAll for convergence
	if _, err := h.PullAll("client-A", proj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", proj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(proj)

	// Delete wins — update does not resurrect a deleted entity
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-CD2")
		if ent != nil {
			t.Fatalf("%s: td-CD2 should be deleted (delete wins over update), got %v", cid, ent)
		}
	}
}

// ─── Test 7: Idempotent push — no double-send after MarkEventsSynced ───

func TestIdempotentPush(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates 3 issues
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("td-IP%d", i)
		if err := h.Mutate("client-A", "create", "issues", id, map[string]any{
			"title": fmt.Sprintf("Issue %d", i), "status": "open",
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	// First push — should accept 3
	res1, err := h.Push("client-A", proj)
	if err != nil {
		t.Fatalf("push 1: %v", err)
	}
	if res1.Accepted != 3 {
		t.Fatalf("push 1: expected 3 accepted, got %d", res1.Accepted)
	}

	// Second push — no pending events (already synced)
	res2, err := h.Push("client-A", proj)
	if err != nil {
		t.Fatalf("push 2: %v", err)
	}
	if res2.Accepted != 0 {
		t.Fatalf("push 2: expected 0 accepted (no pending), got %d", res2.Accepted)
	}
}

// ─── Test 8: Large batch — 500 issues ───

func TestLargeBatch(t *testing.T) {
	h := NewHarness(t, 2, proj)

	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("td-LB%04d", i)
		if err := h.Mutate("client-A", "create", "issues", id, map[string]any{
			"title": fmt.Sprintf("Batch issue %d", i), "status": "open",
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push: %v", err)
	}
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull: %v", err)
	}

	h.AssertConverged(proj)

	countA := h.CountEntities("client-A", "issues")
	countB := h.CountEntities("client-B", "issues")
	if countA != 500 || countB != 500 {
		t.Fatalf("expected 500 issues each, got A=%d B=%d", countA, countB)
	}
}

// ─── Test 9: Interleaved sync ───

func TestInterleavedSync(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates issue-1, syncs
	if err := h.Mutate("client-A", "create", "issues", "td-IL1", map[string]any{
		"title": "Issue 1", "status": "open",
	}); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}

	// B creates issue-2, syncs (also pulls issue-1)
	if err := h.Mutate("client-B", "create", "issues", "td-IL2", map[string]any{
		"title": "Issue 2", "status": "open",
	}); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// A pulls to get issue-2, then updates issue-2 title
	if _, err := h.Pull("client-A", proj); err != nil {
		t.Fatalf("pull A: %v", err)
	}
	if err := h.Mutate("client-A", "update", "issues", "td-IL2", map[string]any{
		"title": "Issue 2 updated by A", "status": "open",
	}); err != nil {
		t.Fatalf("update IL2: %v", err)
	}
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}

	// B pulls to get A's update, then updates issue-1 status
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B: %v", err)
	}
	if err := h.Mutate("client-B", "update", "issues", "td-IL1", map[string]any{
		"title": "Issue 1", "status": "in_progress",
	}); err != nil {
		t.Fatalf("update IL1: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	// A creates issue-3, B deletes issue-2, both sync
	if err := h.Mutate("client-A", "create", "issues", "td-IL3", map[string]any{
		"title": "Issue 3", "status": "open",
	}); err != nil {
		t.Fatalf("create 3: %v", err)
	}
	if err := h.Mutate("client-B", "soft_delete", "issues", "td-IL2", nil); err != nil {
		t.Fatalf("delete IL2: %v", err)
	}

	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A3: %v", err)
	}
	if _, err := h.Push("client-B", proj); err != nil {
		t.Fatalf("push B3: %v", err)
	}

	// Both PullAll for convergence
	if _, err := h.PullAll("client-A", proj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", proj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(proj)

	// issue-1 should exist with in_progress, issue-2 deleted, issue-3 exists
	for _, cid := range []string{"client-A", "client-B"} {
		e1 := h.QueryEntity(cid, "issues", "td-IL1")
		if e1 == nil {
			t.Fatalf("%s: td-IL1 should exist", cid)
		}
		if s, _ := e1["status"].(string); s != "in_progress" {
			t.Fatalf("%s: td-IL1 status expected 'in_progress', got %q", cid, s)
		}
		e2 := h.QueryEntityRaw(cid, "issues", "td-IL2")
		if e2 == nil {
			t.Fatalf("%s: td-IL2 should exist (soft-deleted)", cid)
		}
		if e2["deleted_at"] == nil {
			t.Fatalf("%s: td-IL2 should be soft-deleted (deleted_at should be set)", cid)
		}
		if h.QueryEntity(cid, "issues", "td-IL3") == nil {
			t.Fatalf("%s: td-IL3 should exist", cid)
		}
	}
}

// ─── Test 10: Multi-entity types ───

func TestMultiEntityTypes(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates: issue, log, comment
	if err := h.Mutate("client-A", "create", "issues", "td-ME1", map[string]any{
		"title": "Multi-entity issue", "status": "open",
	}); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := h.Mutate("client-A", "create", "logs", "log-ME1", map[string]any{
		"issue_id": "td-ME1", "session_id": "sess-1", "message": "Started work",
	}); err != nil {
		t.Fatalf("create log: %v", err)
	}
	if err := h.Mutate("client-A", "create", "comments", "cmt-ME1", map[string]any{
		"issue_id": "td-ME1", "session_id": "sess-1", "text": "First comment",
	}); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	// B creates: board, work_session
	if err := h.Mutate("client-B", "create", "boards", "brd-ME1", map[string]any{
		"name": "Sprint board",
	}); err != nil {
		t.Fatalf("create board: %v", err)
	}
	if err := h.Mutate("client-B", "create", "work_sessions", "ws-ME1", map[string]any{
		"name": "Morning session", "session_id": "sess-2",
	}); err != nil {
		t.Fatalf("create work_session: %v", err)
	}

	// Both sync
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B: %v", err)
	}
	// A pulls B's data
	if _, err := h.Pull("client-A", proj); err != nil {
		t.Fatalf("pull A: %v", err)
	}

	h.AssertConverged(proj)

	// Verify all entity types present on both clients
	for _, cid := range []string{"client-A", "client-B"} {
		if h.CountEntities(cid, "issues") != 1 {
			t.Fatalf("%s: expected 1 issue", cid)
		}
		if h.CountEntities(cid, "logs") != 1 {
			t.Fatalf("%s: expected 1 log", cid)
		}
		if h.CountEntities(cid, "comments") != 1 {
			t.Fatalf("%s: expected 1 comment", cid)
		}
		if h.CountEntities(cid, "boards") != 1 {
			t.Fatalf("%s: expected 1 board", cid)
		}
		if h.CountEntities(cid, "work_sessions") != 1 {
			t.Fatalf("%s: expected 1 work_session", cid)
		}
	}
}

// ─── Test 11: Create existing entity — server version wins via upsert ───

func TestCreateExistingEntity(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates td-DUP with "A version"
	if err := h.Mutate("client-A", "create", "issues", "td-DUP", map[string]any{
		"title": "A version", "status": "open",
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A: %v", err)
	}

	// B independently creates same ID with "B version" (no pull)
	if err := h.Mutate("client-B", "create", "issues", "td-DUP", map[string]any{
		"title": "B version", "status": "open",
	}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	// B pulls A's create — server event upserts over B's local
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	ent := h.QueryEntity("client-B", "issues", "td-DUP")
	if ent == nil {
		t.Fatal("client-B: td-DUP should exist")
	}
	title, _ := ent["title"].(string)
	if title != "A version" {
		t.Fatalf("client-B: expected 'A version', got %q", title)
	}
}

// ─── Test 12: Update for missing entity is a no-op ───

func TestUpdateMissingEntity(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates issue then updates title
	if err := h.Mutate("client-A", "create", "issues", "td-UM1", map[string]any{
		"title": "Original", "status": "open",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.Mutate("client-A", "update", "issues", "td-UM1", map[string]any{
		"title": "Updated", "status": "in_progress",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Push both events
	res, err := h.Push("client-A", proj)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if res.Accepted < 2 {
		t.Fatalf("expected at least 2 accepted, got %d", res.Accepted)
	}

	// Advance B's cursor past the create event (skip seq 1, only pull from seq 2+)
	// The create event has the lowest server_seq
	h.Clients["client-B"].LastPulledSeq = res.Acks[0].ServerSeq

	// B pulls — only gets the update event
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	// Update without prior create is a no-op — entity should not exist
	ent := h.QueryEntity("client-B", "issues", "td-UM1")
	if ent != nil {
		t.Fatalf("client-B: td-UM1 should not exist (update without prior create is no-op), got %v", ent)
	}
}

// ─── Test 13: Delete for entity that never existed locally ───

func TestDeleteMissingEntity(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates and deletes — push both
	if err := h.Mutate("client-A", "create", "issues", "td-DM1", map[string]any{
		"title": "Ghost", "status": "open",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.Mutate("client-A", "delete", "issues", "td-DM1", nil); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push: %v", err)
	}

	// Advance B past the create, so B only sees the delete
	// Get server events to find seqs
	h.Clients["client-B"].LastPulledSeq = 1 // skip create (seq 1)

	// B pulls — gets delete for entity it never had
	_, err := h.Pull("client-B", proj)
	if err != nil {
		t.Fatalf("pull B: %v (should be no error for deleting missing entity)", err)
	}

	// Entity should still be absent
	ent := h.QueryEntity("client-B", "issues", "td-DM1")
	if ent != nil {
		t.Fatal("client-B: td-DM1 should not exist")
	}
}

// ─── Test 14: Update after local delete — delete wins ───

func TestUpdateAfterLocalDelete(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates issue, both sync
	if err := h.Mutate("client-A", "create", "issues", "td-ULD1", map[string]any{
		"title": "Original", "status": "open",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", proj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// B deletes locally (does NOT push)
	if err := h.Mutate("client-B", "delete", "issues", "td-ULD1", nil); err != nil {
		t.Fatalf("local delete B: %v", err)
	}
	// Verify B deleted locally
	if h.QueryEntity("client-B", "issues", "td-ULD1") != nil {
		t.Fatal("client-B: should have deleted td-ULD1 locally")
	}

	// A updates issue, pushes
	if err := h.Mutate("client-A", "update", "issues", "td-ULD1", map[string]any{
		"title": "Updated by A", "status": "in_progress",
	}); err != nil {
		t.Fatalf("update A: %v", err)
	}
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A: %v", err)
	}

	// B pulls — update does not resurrect the locally-deleted entity
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	ent := h.QueryEntity("client-B", "issues", "td-ULD1")
	if ent != nil {
		t.Fatalf("client-B: td-ULD1 should stay deleted (delete wins over remote update), got %v", ent)
	}
}

// ─── Test 15: Schema version mismatch — unknown fields ignored ───

func TestSchemaVersionMismatch(t *testing.T) {
	h := NewHarness(t, 2, proj)

	serverDB := h.ProjectDBs[proj]

	// Construct a raw event with unknown field in new_data
	payload := `{"schema_version":2,"new_data":{"title":"Alien","status":"open","custom_xyz":"should be ignored"},"previous_data":{}}`
	ev := tdsync.Event{
		ClientActionID:  999,
		DeviceID:        "device-phantom",
		SessionID:       "session-phantom",
		ActionType:      "create",
		EntityType:      "issues",
		EntityID:        "td-SV1",
		Payload:         []byte(payload),
		ClientTimestamp:  time.Now(),
	}

	// Insert directly into server
	serverTx, err := serverDB.Begin()
	if err != nil {
		t.Fatalf("begin server tx: %v", err)
	}
	_, err = tdsync.InsertServerEvents(serverTx, []tdsync.Event{ev})
	if err != nil {
		serverTx.Rollback()
		t.Fatalf("insert server events: %v", err)
	}
	if err := serverTx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// B pulls — unknown field silently dropped, entity created successfully
	_, err = h.Pull("client-B", proj)
	if err != nil {
		t.Fatalf("pull B: %v", err)
	}

	// Entity should exist with known fields; unknown custom_xyz was silently dropped
	ent := h.QueryEntity("client-B", "issues", "td-SV1")
	if ent == nil {
		t.Fatal("client-B: td-SV1 should exist after pull with unknown fields dropped")
	}
	title, _ := ent["title"].(string)
	if title != "Alien" {
		t.Fatalf("client-B: expected title 'Alien', got %q", title)
	}
	status, _ := ent["status"].(string)
	if status != "open" {
		t.Fatalf("client-B: expected status 'open', got %q", status)
	}
}

// ─── Test 16: Partial batch failure — bad entity_type ───

func TestPartialBatchFailure(t *testing.T) {
	h := NewHarness(t, 2, proj)

	serverDB := h.ProjectDBs[proj]

	// Build 7 events: 3 good, 1 bad entity_type, 3 more good
	var events []tdsync.Event
	now := time.Now()

	for i := 1; i <= 3; i++ {
		payload := fmt.Sprintf(`{"schema_version":1,"new_data":{"title":"Good %d","status":"open"},"previous_data":{}}`, i)
		events = append(events, tdsync.Event{
			ClientActionID:  int64(i),
			DeviceID:        "device-batch",
			SessionID:       "session-batch",
			ActionType:      "create",
			EntityType:      "issues",
			EntityID:        fmt.Sprintf("td-PB%d", i),
			Payload:         []byte(payload),
			ClientTimestamp:  now,
		})
	}

	// Bad event — nonexistent_table
	events = append(events, tdsync.Event{
		ClientActionID:  4,
		DeviceID:        "device-batch",
		SessionID:       "session-batch",
		ActionType:      "create",
		EntityType:      "nonexistent_table",
		EntityID:        "td-BAD",
		Payload:         []byte(`{"schema_version":1,"new_data":{"name":"bad"},"previous_data":{}}`),
		ClientTimestamp:  now,
	})

	for i := 5; i <= 7; i++ {
		payload := fmt.Sprintf(`{"schema_version":1,"new_data":{"title":"Good %d","status":"open"},"previous_data":{}}`, i)
		events = append(events, tdsync.Event{
			ClientActionID:  int64(i),
			DeviceID:        "device-batch",
			SessionID:       "session-batch",
			ActionType:      "create",
			EntityType:      "issues",
			EntityID:        fmt.Sprintf("td-PB%d", i),
			Payload:         []byte(payload),
			ClientTimestamp:  now,
		})
	}

	// Insert all into server
	serverTx, err := serverDB.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, err = tdsync.InsertServerEvents(serverTx, events)
	if err != nil {
		serverTx.Rollback()
		t.Fatalf("insert: %v", err)
	}
	if err := serverTx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// B pulls — ApplyRemoteEvents handles bad entity gracefully
	_, err = h.Pull("client-B", proj)
	if err != nil {
		t.Fatalf("pull B: %v", err)
	}

	// 6 good issues should be applied
	count := h.CountEntities("client-B", "issues")
	if count != 6 {
		t.Fatalf("expected 6 issues, got %d", count)
	}

	// Cursor should have advanced past all 7 events
	// (LastPulledSeq should be >= 7)
	if h.Clients["client-B"].LastPulledSeq < 7 {
		t.Fatalf("cursor should advance past all events, got %d", h.Clients["client-B"].LastPulledSeq)
	}
}

// ─── Test 17: Partial payload drops columns (INSERT OR REPLACE behavior) ───

func TestPartialPayloadDropsColumns(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates issue with title + status + priority
	if err := h.Mutate("client-A", "create", "issues", "td-PP1", map[string]any{
		"title": "Full data", "status": "open", "priority": "P1",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Sync to B
	if err := h.Sync("client-A", proj); err != nil {
		t.Fatalf("sync A: %v", err)
	}
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	// Verify B has all fields
	ent := h.QueryEntity("client-B", "issues", "td-PP1")
	if ent == nil {
		t.Fatal("client-B: td-PP1 should exist")
	}
	if p, _ := ent["priority"].(string); p != "P1" {
		t.Fatalf("client-B: expected priority 'P1', got %q", p)
	}

	// A pushes update with only title (no status, no priority)
	if err := h.Mutate("client-A", "update", "issues", "td-PP1", map[string]any{
		"title": "Partial update",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A: %v", err)
	}

	// B pulls the partial update
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	// INSERT OR REPLACE with only title+id → other columns get DEFAULT values
	ent = h.QueryEntity("client-B", "issues", "td-PP1")
	if ent == nil {
		t.Fatal("client-B: td-PP1 should still exist")
	}
	title, _ := ent["title"].(string)
	if title != "Partial update" {
		t.Fatalf("client-B: expected 'Partial update', got %q", title)
	}

	// status and priority should be their DEFAULT values (not the original values)
	// since INSERT OR REPLACE drops the old row and inserts fresh
	status, _ := ent["status"].(string)
	if status != "open" {
		// Default is 'open' per schema
		t.Logf("client-B: status after partial update = %q (default 'open')", status)
	}
	priority, _ := ent["priority"].(string)
	if priority != "P2" {
		// Default is 'P2' per schema
		t.Logf("client-B: priority after partial update = %q (default 'P2')", priority)
	}

	// The key point: original values P1 and any non-default status are LOST
	// because INSERT OR REPLACE replaces the entire row
	if priority == "P1" {
		t.Fatalf("client-B: priority should NOT be 'P1' after partial update (INSERT OR REPLACE drops old row)")
	}
}

// ─── Test 18: Concurrent push — two goroutines push simultaneously ───

func TestConcurrentPush(t *testing.T) {
	h := NewHarness(t, 3, proj) // A, B push concurrently; C verifies

	// A creates 10 issues
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("td-CPA%03d", i)
		if err := h.Mutate("client-A", "create", "issues", id, map[string]any{
			"title": fmt.Sprintf("A-%d", i), "status": "open",
		}); err != nil {
			t.Fatalf("mutate A: %v", err)
		}
	}

	// B creates 10 issues (different IDs)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("td-CPB%03d", i)
		if err := h.Mutate("client-B", "create", "issues", id, map[string]any{
			"title": fmt.Sprintf("B-%d", i), "status": "open",
		}); err != nil {
			t.Fatalf("mutate B: %v", err)
		}
	}

	// Push simultaneously from A and B
	var wg sync.WaitGroup
	var errA, errB error
	var resA, resB tdsync.PushResult

	wg.Add(2)
	start := make(chan struct{})

	go func() {
		defer wg.Done()
		<-start
		resA, errA = h.Push("client-A", proj)
	}()
	go func() {
		defer wg.Done()
		<-start
		resB, errB = h.Push("client-B", proj)
	}()

	close(start) // fire both at once
	wg.Wait()

	if errA != nil {
		t.Fatalf("push A: %v", errA)
	}
	if errB != nil {
		t.Fatalf("push B: %v", errB)
	}

	// All 20 events should be accepted
	totalAccepted := resA.Accepted + resB.Accepted
	if totalAccepted != 20 {
		t.Fatalf("expected 20 accepted total, got %d (A=%d B=%d)", totalAccepted, resA.Accepted, resB.Accepted)
	}

	// Verify server_seqs are sequential with no gaps
	allSeqs := make(map[int64]bool)
	for _, ack := range resA.Acks {
		allSeqs[ack.ServerSeq] = true
	}
	for _, ack := range resB.Acks {
		if allSeqs[ack.ServerSeq] {
			t.Fatalf("duplicate server_seq %d between A and B", ack.ServerSeq)
		}
		allSeqs[ack.ServerSeq] = true
	}
	for seq := int64(1); seq <= 20; seq++ {
		if !allSeqs[seq] {
			t.Fatalf("gap in server_seqs: missing seq %d", seq)
		}
	}

	// Duplicate push should accept 0
	resA2, err := h.Push("client-A", proj)
	if err != nil {
		t.Fatalf("re-push A: %v", err)
	}
	if resA2.Accepted != 0 {
		t.Fatalf("re-push A: expected 0 accepted, got %d", resA2.Accepted)
	}

	// C pulls all events and verifies convergence
	if _, err := h.Pull("client-C", proj); err != nil {
		t.Fatalf("pull C: %v", err)
	}

	// A and B also pull to converge
	if _, err := h.PullAll("client-A", proj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", proj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(proj)

	for _, cid := range []string{"client-A", "client-B", "client-C"} {
		count := h.CountEntities(cid, "issues")
		if count != 20 {
			t.Fatalf("%s: expected 20 issues, got %d", cid, count)
		}
	}
}

// ─── Test 19: Crash recovery — client re-pushes after skipping MarkEventsSynced ───

func TestCrashRecovery(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// A creates 5 issues
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("td-CR%03d", i)
		if err := h.Mutate("client-A", "create", "issues", id, map[string]any{
			"title": fmt.Sprintf("Crash %d", i), "status": "open",
		}); err != nil {
			t.Fatalf("mutate A: %v", err)
		}
	}

	// A pushes but "crashes" — server accepts but client doesn't mark synced
	res1, err := h.PushWithoutMark("client-A", proj)
	if err != nil {
		t.Fatalf("push-without-mark: %v", err)
	}
	if res1.Accepted != 5 {
		t.Fatalf("first push: expected 5 accepted, got %d", res1.Accepted)
	}

	// A "recovers" and pushes the same events again
	res2, err := h.Push("client-A", proj)
	if err != nil {
		t.Fatalf("re-push: %v", err)
	}
	// Server should dedup all — 0 accepted, 5 rejected as duplicates
	if res2.Accepted != 0 {
		t.Fatalf("re-push: expected 0 accepted (dedup), got %d", res2.Accepted)
	}
	if len(res2.Rejected) != 5 {
		t.Fatalf("re-push: expected 5 rejected, got %d", len(res2.Rejected))
	}
	for _, r := range res2.Rejected {
		if r.Reason != "duplicate" {
			t.Fatalf("re-push: expected reason 'duplicate', got %q", r.Reason)
		}
	}

	// B pulls and sees exactly one copy of each event
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	count := h.CountEntities("client-B", "issues")
	if count != 5 {
		t.Fatalf("client-B: expected 5 issues (no duplicates), got %d", count)
	}

	// Verify each issue exists exactly once with correct data
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("td-CR%03d", i)
		ent := h.QueryEntity("client-B", "issues", id)
		if ent == nil {
			t.Fatalf("client-B: %s not found", id)
		}
		title, _ := ent["title"].(string)
		expected := fmt.Sprintf("Crash %d", i)
		if title != expected {
			t.Fatalf("client-B: %s expected title %q, got %q", id, expected, title)
		}
	}

	// A's events are now marked synced (from the re-push MarkEventsSynced path,
	// but since they were rejected as duplicates, MarkEventsSynced gets no acks).
	// A should still be able to converge by pulling all.
	if _, err := h.PullAll("client-A", proj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}

	h.AssertConverged(proj)
}
