package syncharness

import (
	"testing"
)

// TestServerMigration verifies that a client can re-sync all events after the server
// loses track of previously synced events (e.g., migration to a new server).
// The scenario:
//   1. Client creates an issue, pushes to server - synced_at is set
//   2. Server "dies" - simulate by clearing synced_at on client's action_log
//   3. Client pushes again - events should sync successfully to the new server
func TestServerMigration(t *testing.T) {
	const projID = "proj-migration"
	h := NewHarness(t, 1, projID)

	clientID := "client-A"
	client := h.Clients[clientID]

	// Step 1: Client creates an issue
	err := h.Mutate(clientID, "create", "issues", "td-mig-001", map[string]any{
		"title":  "Migration test issue",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Step 2: Push to server
	res, err := h.Push(clientID, projID)
	if err != nil {
		t.Fatalf("initial push: %v", err)
	}
	if res.Accepted != 1 {
		t.Fatalf("initial push: expected 1 accepted, got %d", res.Accepted)
	}

	// Verify synced_at is set (event is marked as synced)
	var syncedCount int64
	err = client.DB.QueryRow(`SELECT COUNT(*) FROM action_log WHERE synced_at IS NOT NULL`).Scan(&syncedCount)
	if err != nil {
		t.Fatalf("count synced events: %v", err)
	}
	if syncedCount != 1 {
		t.Fatalf("expected 1 synced event, got %d", syncedCount)
	}

	// Step 3: Simulate server death by clearing synced_at and server_seq
	// This mimics what happens when migrating to a new server that has no history
	_, err = client.DB.Exec(`UPDATE action_log SET synced_at = NULL, server_seq = NULL`)
	if err != nil {
		t.Fatalf("clear sync state: %v", err)
	}

	// Verify events are now pending
	var pendingCount int64
	err = client.DB.QueryRow(`SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0`).Scan(&pendingCount)
	if err != nil {
		t.Fatalf("count pending events: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("expected 1 pending event after clearing sync state, got %d", pendingCount)
	}

	// Also clear the server events table to simulate a fresh server
	serverDB := h.ProjectDBs[projID]
	_, err = serverDB.Exec(`DELETE FROM events`)
	if err != nil {
		t.Fatalf("clear server events: %v", err)
	}

	// Step 4: Push again - events should sync to the new server
	res2, err := h.Push(clientID, projID)
	if err != nil {
		t.Fatalf("re-push after migration: %v", err)
	}
	if res2.Accepted != 1 {
		t.Fatalf("re-push: expected 1 accepted, got %d", res2.Accepted)
	}

	// Verify synced_at is set again
	err = client.DB.QueryRow(`SELECT COUNT(*) FROM action_log WHERE synced_at IS NOT NULL`).Scan(&syncedCount)
	if err != nil {
		t.Fatalf("count synced events after re-push: %v", err)
	}
	if syncedCount != 1 {
		t.Fatalf("expected 1 synced event after re-push, got %d", syncedCount)
	}

	// Verify the issue exists on the client
	ent := h.QueryEntity(clientID, "issues", "td-mig-001")
	if ent == nil {
		t.Fatal("issue should exist on client")
	}
	title, _ := ent["title"].(string)
	if title != "Migration test issue" {
		t.Fatalf("expected title 'Migration test issue', got %q", title)
	}

	t.Logf("Server migration test passed: client successfully re-synced %d events to new server", res2.Accepted)
}

// TestServerMigrationMultipleEvents tests migration with multiple events and verifies
// they all re-sync correctly.
func TestServerMigrationMultipleEvents(t *testing.T) {
	const projID = "proj-migration-multi"
	h := NewHarness(t, 1, projID)

	clientID := "client-A"
	client := h.Clients[clientID]

	// Create multiple issues
	for i := 1; i <= 5; i++ {
		id := "td-mig-multi-" + string(rune('0'+i))
		err := h.Mutate(clientID, "create", "issues", id, map[string]any{
			"title":  "Migration multi " + string(rune('0'+i)),
			"status": "open",
		})
		if err != nil {
			t.Fatalf("create issue %s: %v", id, err)
		}
	}

	// Also add an update
	err := h.Mutate(clientID, "update", "issues", "td-mig-multi-1", map[string]any{
		"title":  "Updated migration multi 1",
		"status": "in_progress",
	})
	if err != nil {
		t.Fatalf("update issue: %v", err)
	}

	// Push all events
	res, err := h.Push(clientID, projID)
	if err != nil {
		t.Fatalf("initial push: %v", err)
	}
	if res.Accepted != 6 {
		t.Fatalf("initial push: expected 6 accepted, got %d", res.Accepted)
	}

	// Simulate server migration
	_, err = client.DB.Exec(`UPDATE action_log SET synced_at = NULL, server_seq = NULL`)
	if err != nil {
		t.Fatalf("clear sync state: %v", err)
	}

	// Clear server
	serverDB := h.ProjectDBs[projID]
	_, err = serverDB.Exec(`DELETE FROM events`)
	if err != nil {
		t.Fatalf("clear server: %v", err)
	}

	// Verify all events are pending
	var pendingCount int64
	err = client.DB.QueryRow(`SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0`).Scan(&pendingCount)
	if err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pendingCount != 6 {
		t.Fatalf("expected 6 pending events, got %d", pendingCount)
	}

	// Push again
	res2, err := h.Push(clientID, projID)
	if err != nil {
		t.Fatalf("re-push: %v", err)
	}
	if res2.Accepted != 6 {
		t.Fatalf("re-push: expected 6 accepted, got %d", res2.Accepted)
	}

	// Verify all synced
	var syncedCount int64
	err = client.DB.QueryRow(`SELECT COUNT(*) FROM action_log WHERE synced_at IS NOT NULL`).Scan(&syncedCount)
	if err != nil {
		t.Fatalf("count synced: %v", err)
	}
	if syncedCount != 6 {
		t.Fatalf("expected 6 synced events, got %d", syncedCount)
	}

	// Verify issue count
	count := h.CountEntities(clientID, "issues")
	if count != 5 {
		t.Fatalf("expected 5 issues, got %d", count)
	}

	// Verify the update was applied
	ent := h.QueryEntity(clientID, "issues", "td-mig-multi-1")
	if ent == nil {
		t.Fatal("issue td-mig-multi-1 should exist")
	}
	title, _ := ent["title"].(string)
	if title != "Updated migration multi 1" {
		t.Fatalf("expected updated title, got %q", title)
	}

	t.Logf("Multi-event migration test passed: %d events re-synced", res2.Accepted)
}

// TestServerMigrationWithTwoClients tests the migration scenario with two clients
// to verify convergence after a server migration.
func TestServerMigrationWithTwoClients(t *testing.T) {
	const projID = "proj-migration-2c"
	h := NewHarness(t, 2, projID)

	clientA := h.Clients["client-A"]

	// Client A creates an issue and pushes
	err := h.Mutate("client-A", "create", "issues", "td-mig2c-001", map[string]any{
		"title":  "Two-client migration test",
		"status": "open",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = h.Push("client-A", projID)
	if err != nil {
		t.Fatalf("push A: %v", err)
	}

	// Client B pulls
	_, err = h.Pull("client-B", projID)
	if err != nil {
		t.Fatalf("pull B: %v", err)
	}

	// Verify both have the issue
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", "td-mig2c-001")
		if ent == nil {
			t.Fatalf("%s should have issue", cid)
		}
	}

	// Simulate server death - clear client A's sync state
	_, err = clientA.DB.Exec(`UPDATE action_log SET synced_at = NULL, server_seq = NULL`)
	if err != nil {
		t.Fatalf("clear A sync state: %v", err)
	}

	// Clear server events table
	serverDB := h.ProjectDBs[projID]
	_, err = serverDB.Exec(`DELETE FROM events`)
	if err != nil {
		t.Fatalf("clear server: %v", err)
	}

	// Reset B's pulled seq (simulating B also connecting to new server)
	h.Clients["client-B"].LastPulledSeq = 0

	// Client A re-pushes
	res, err := h.Push("client-A", projID)
	if err != nil {
		t.Fatalf("re-push A: %v", err)
	}
	if res.Accepted != 1 {
		t.Fatalf("re-push: expected 1 accepted, got %d", res.Accepted)
	}

	// Client B pulls from new server
	_, err = h.Pull("client-B", projID)
	if err != nil {
		t.Fatalf("pull B from new server: %v", err)
	}

	// Assert convergence
	h.AssertConverged(projID)

	t.Log("Two-client migration test passed")
}
