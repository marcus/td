package syncharness

import (
	"testing"
)

const undoProj = "proj-undo"

// ─── Test 1: Undo BEFORE sync — undone event is never sent ───

func TestUndoBeforeSync_EventNeverSent(t *testing.T) {
	h := NewHarness(t, 2, undoProj)

	// Client A creates an issue
	err := h.Mutate("client-A", "create", "issues", "td-UBS1", map[string]any{
		"title":  "Will be undone",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Verify A has the issue locally
	ent := h.QueryEntity("client-A", "issues", "td-UBS1")
	if ent == nil {
		t.Fatal("client-A: issue should exist locally after create")
	}

	// A undoes the create BEFORE syncing
	if err := h.UndoLastAction("client-A"); err != nil {
		t.Fatalf("undo: %v", err)
	}

	// Verify A no longer has the issue (soft-deleted)
	ent = h.QueryEntity("client-A", "issues", "td-UBS1")
	if ent != nil {
		t.Fatalf("client-A: issue should be gone after undo, got %v", ent)
	}

	// A syncs — the original create event should NOT be sent (undone=1)
	// The compensating soft_delete is sent, but it's for a non-existent entity on the server
	if err := h.Sync("client-A", undoProj); err != nil {
		t.Fatalf("sync A: %v", err)
	}

	// B syncs
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B: %v", err)
	}

	// Neither client should have the issue
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-UBS1")
		if ent != nil {
			t.Fatalf("%s: issue should not exist (undo before sync), got %v", cid, ent)
		}
	}
}

// ─── Test 2: Undo AFTER sync — compensating soft_delete propagates ───

func TestUndoAfterSync_GeneratesCompensatingEvent(t *testing.T) {
	h := NewHarness(t, 2, undoProj)

	// Client A creates an issue
	err := h.Mutate("client-A", "create", "issues", "td-UAS1", map[string]any{
		"title":  "Created then undone",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A pushes, B pulls — both have the issue
	if _, err := h.Push("client-A", undoProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Pull("client-B", undoProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	// Verify both have the issue
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-UAS1")
		if ent == nil {
			t.Fatalf("%s: issue should exist after sync", cid)
		}
	}

	// A undoes the creation AFTER sync
	if err := h.UndoLastAction("client-A"); err != nil {
		t.Fatalf("undo: %v", err)
	}

	// Verify A no longer has the issue
	ent := h.QueryEntity("client-A", "issues", "td-UAS1")
	if ent != nil {
		t.Fatalf("client-A: issue should be gone after undo, got %v", ent)
	}

	// A pushes the compensating soft_delete, B pulls
	if _, err := h.Push("client-A", undoProj); err != nil {
		t.Fatalf("push A2: %v", err)
	}
	if _, err := h.Pull("client-B", undoProj); err != nil {
		t.Fatalf("pull B2: %v", err)
	}

	h.AssertConverged(undoProj)

	// Both clients should now have the issue soft-deleted
	for _, cid := range []string{"client-A", "client-B"} {
		// QueryEntity filters soft-deleted, should return nil
		ent := h.QueryEntity(cid, "issues", "td-UAS1")
		if ent != nil {
			t.Fatalf("%s: issue should be soft-deleted after undo sync, got %v", cid, ent)
		}
		// Raw query should show the row with deleted_at set
		raw := h.QueryEntityRaw(cid, "issues", "td-UAS1")
		if raw == nil {
			t.Fatalf("%s: issue row should still exist (soft-deleted)", cid)
		}
		if raw["deleted_at"] == nil {
			t.Fatalf("%s: deleted_at should be set after undo sync", cid)
		}
	}
}

// ─── Test 3: Undo update AFTER sync — reverts to previous state ───

func TestUndoUpdateAfterSync_RevertsToPreviousState(t *testing.T) {
	h := NewHarness(t, 2, undoProj)

	// Client A creates an issue with title-v1
	err := h.Mutate("client-A", "create", "issues", "td-UUS1", map[string]any{
		"title":  "title-v1",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A syncs, B syncs — both have title-v1
	if err := h.Sync("client-A", undoProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Verify both have title-v1
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-UUS1")
		if ent == nil {
			t.Fatalf("%s: issue should exist", cid)
		}
		if title, _ := ent["title"].(string); title != "title-v1" {
			t.Fatalf("%s: expected title 'title-v1', got %q", cid, title)
		}
	}

	// Client A updates to title-v2
	err = h.Mutate("client-A", "update", "issues", "td-UUS1", map[string]any{
		"title":  "title-v2",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	// A syncs, B syncs — both have title-v2
	if err := h.Sync("client-A", undoProj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	// Verify both have title-v2
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-UUS1")
		if title, _ := ent["title"].(string); title != "title-v2" {
			t.Fatalf("%s: expected title 'title-v2', got %q", cid, title)
		}
	}

	// A undoes the update — should revert to title-v1
	if err := h.UndoLastAction("client-A"); err != nil {
		t.Fatalf("undo: %v", err)
	}

	// Verify A reverted locally
	ent := h.QueryEntity("client-A", "issues", "td-UUS1")
	if ent == nil {
		t.Fatal("client-A: issue should still exist after undo update")
	}
	if title, _ := ent["title"].(string); title != "title-v1" {
		t.Fatalf("client-A: expected title 'title-v1' after undo, got %q", title)
	}

	// A syncs, B syncs
	if err := h.Sync("client-A", undoProj); err != nil {
		t.Fatalf("sync A3: %v", err)
	}
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B3: %v", err)
	}

	h.AssertConverged(undoProj)

	// Both should have title-v1 again
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-UUS1")
		if ent == nil {
			t.Fatalf("%s: issue should exist", cid)
		}
		if title, _ := ent["title"].(string); title != "title-v1" {
			t.Fatalf("%s: expected title 'title-v1' after undo sync, got %q", cid, title)
		}
	}
}

// ─── Test 4: Remote modification after sync, then undo — LWW determines outcome ───

func TestUndoWithRemoteModification_LWWDeterminesOutcome(t *testing.T) {
	h := NewHarness(t, 2, undoProj)

	// Client A creates issue
	err := h.Mutate("client-A", "create", "issues", "td-ULWW1", map[string]any{
		"title":  "Original",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Both sync
	if err := h.Sync("client-A", undoProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// B modifies the issue (adds description)
	err = h.Mutate("client-B", "update", "issues", "td-ULWW1", map[string]any{
		"title":       "Original",
		"status":      "in_progress",
		"description": "Added by B",
	})
	if err != nil {
		t.Fatalf("update B: %v", err)
	}

	// B syncs the update
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	// A undoes the creation — generates soft_delete
	if err := h.UndoLastAction("client-A"); err != nil {
		t.Fatalf("undo A: %v", err)
	}

	// A pushes soft_delete (higher server_seq than B's update)
	if _, err := h.Push("client-A", undoProj); err != nil {
		t.Fatalf("push A: %v", err)
	}

	// Both PullAll for convergence
	if _, err := h.PullAll("client-A", undoProj); err != nil {
		t.Fatalf("pullAll A: %v", err)
	}
	if _, err := h.PullAll("client-B", undoProj); err != nil {
		t.Fatalf("pullAll B: %v", err)
	}

	h.AssertConverged(undoProj)

	// A's soft_delete has higher server_seq → issue should be deleted
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-ULWW1")
		if ent != nil {
			t.Fatalf("%s: issue should be soft-deleted (undo's soft_delete has higher seq), got %v", cid, ent)
		}
	}
}

// ─── Test 5: Undo restore (re-delete) — restore event propagates ───

func TestUndoRestore_ReDeletePropagates(t *testing.T) {
	h := NewHarness(t, 2, undoProj)

	// Client A creates an issue
	err := h.Mutate("client-A", "create", "issues", "td-UR1", map[string]any{
		"title":  "Will be deleted then restored",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Both sync
	if err := h.Sync("client-A", undoProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// A deletes the issue
	err = h.Mutate("client-A", "soft_delete", "issues", "td-UR1", nil)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Both sync — both see deletion
	if err := h.Sync("client-A", undoProj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	// Verify both have issue deleted
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-UR1")
		if ent != nil {
			t.Fatalf("%s: issue should be deleted, got %v", cid, ent)
		}
	}

	// A undoes the delete — generates restore event
	if err := h.UndoLastAction("client-A"); err != nil {
		t.Fatalf("undo: %v", err)
	}

	// Verify A has the issue restored
	ent := h.QueryEntity("client-A", "issues", "td-UR1")
	if ent == nil {
		t.Fatal("client-A: issue should be restored after undo delete")
	}

	// A syncs, B syncs
	if err := h.Sync("client-A", undoProj); err != nil {
		t.Fatalf("sync A3: %v", err)
	}
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B3: %v", err)
	}

	h.AssertConverged(undoProj)

	// Both should have the issue restored
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-UR1")
		if ent == nil {
			t.Fatalf("%s: issue should be restored after undo delete sync", cid)
		}
		// Verify deleted_at is cleared
		raw := h.QueryEntityRaw(cid, "issues", "td-UR1")
		if raw["deleted_at"] != nil {
			t.Fatalf("%s: deleted_at should be nil after restore sync, got %v", cid, raw["deleted_at"])
		}
	}
}

// ─── Test 6: Undo of already-synced action — compensating event propagates ───

func TestUndoAlreadySynced_CompensatingEventPropagates(t *testing.T) {
	h := NewHarness(t, 2, undoProj)

	// Client A creates issue
	err := h.Mutate("client-A", "create", "issues", "td-UNR1", map[string]any{
		"title":  "Already synced",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A pushes — create event is now synced (synced_at set)
	res1, err := h.Push("client-A", undoProj)
	if err != nil {
		t.Fatalf("push A1: %v", err)
	}
	if res1.Accepted != 1 {
		t.Fatalf("push A1: expected 1 accepted, got %d", res1.Accepted)
	}

	// B pulls to get the issue
	if _, err := h.Pull("client-B", undoProj); err != nil {
		t.Fatalf("pull B1: %v", err)
	}

	// Verify B has the issue
	ent := h.QueryEntity("client-B", "issues", "td-UNR1")
	if ent == nil {
		t.Fatal("client-B: issue should exist after first pull")
	}

	// A undoes — original action marked undone, new soft_delete event created
	if err := h.UndoLastAction("client-A"); err != nil {
		t.Fatalf("undo: %v", err)
	}

	// Verify A has the issue soft-deleted locally
	ent = h.QueryEntity("client-A", "issues", "td-UNR1")
	if ent != nil {
		t.Fatalf("client-A: issue should be soft-deleted after undo, got %v", ent)
	}

	// A pushes again — sends the soft_delete event
	// Note: backfill may also create a synthetic create event for the orphan
	// entity, but the soft_delete should still propagate correctly.
	_, err = h.Push("client-A", undoProj)
	if err != nil {
		t.Fatalf("push A2: %v", err)
	}

	// B pulls — should see the soft_delete
	if _, err := h.Pull("client-B", undoProj); err != nil {
		t.Fatalf("pull B2: %v", err)
	}

	// B should have the issue soft-deleted
	ent = h.QueryEntity("client-B", "issues", "td-UNR1")
	if ent != nil {
		t.Fatalf("client-B: issue should be soft-deleted, got %v", ent)
	}

	// Verify the row exists but is soft-deleted
	raw := h.QueryEntityRaw("client-B", "issues", "td-UNR1")
	if raw == nil {
		t.Fatal("client-B: issue row should exist (soft-deleted)")
	}
	if raw["deleted_at"] == nil {
		t.Fatal("client-B: deleted_at should be set")
	}

	h.AssertConverged(undoProj)
}

// ─── Test 7: Undo then redo (undo the undo) ───

func TestUndoThenRedo_TogglesBetweenStates(t *testing.T) {
	h := NewHarness(t, 2, undoProj)

	// A creates issue with v1
	err := h.Mutate("client-A", "create", "issues", "td-UTR1", map[string]any{
		"title":  "v1",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A updates to v2
	err = h.Mutate("client-A", "update", "issues", "td-UTR1", map[string]any{
		"title":  "v2",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	// Both sync
	if err := h.Sync("client-A", undoProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Verify both have v2
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-UTR1")
		if title, _ := ent["title"].(string); title != "v2" {
			t.Fatalf("%s: expected 'v2', got %q", cid, title)
		}
	}

	// A undoes update to v2 → reverts to v1
	if err := h.UndoLastAction("client-A"); err != nil {
		t.Fatalf("undo1: %v", err)
	}

	ent := h.QueryEntity("client-A", "issues", "td-UTR1")
	if title, _ := ent["title"].(string); title != "v1" {
		t.Fatalf("client-A after undo1: expected 'v1', got %q", title)
	}

	// A undoes again — this undoes the "revert to v1" action, going back to v2
	// (This is "redo" behavior: undo undoes the last action, including previous undos)
	if err := h.UndoLastAction("client-A"); err != nil {
		t.Fatalf("undo2 (redo): %v", err)
	}

	ent = h.QueryEntity("client-A", "issues", "td-UTR1")
	if title, _ := ent["title"].(string); title != "v2" {
		t.Fatalf("client-A after undo2 (redo): expected 'v2', got %q", title)
	}

	// Both sync
	if err := h.Sync("client-A", undoProj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}
	if err := h.Sync("client-B", undoProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}

	h.AssertConverged(undoProj)

	// Both should have v2 (after undo then redo)
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-UTR1")
		if title, _ := ent["title"].(string); title != "v2" {
			t.Fatalf("%s: expected 'v2' after undo/redo cycle, got %q", cid, title)
		}
	}
}
