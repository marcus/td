package syncharness

import (
	"testing"
)

const sessProj = "proj-sess"

// TestSessionFields_SyncConvergence verifies that implementer_session,
// reviewer_session, and creator_session fields survive a full sync round-trip
// without being dropped by omitempty.
func TestSessionFields_SyncConvergence(t *testing.T) {
	h := NewHarness(t, 2, sessProj)

	issueID := "td-sess1"

	// Step 1: Client A creates an issue with creator_session set
	err := h.Mutate("client-A", "create", "issues", issueID, map[string]any{
		"title":           "Session sync test",
		"status":          "open",
		"type":            "task",
		"priority":        "P2",
		"creator_session": "ses-creator-A",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Step 2: Client A sets implementer_session (simulates starting work)
	err = h.Mutate("client-A", "update", "issues", issueID, map[string]any{
		"title":               "Session sync test",
		"status":              "in_progress",
		"type":                "task",
		"priority":            "P2",
		"creator_session":     "ses-creator-A",
		"implementer_session": "ses-impl-A",
	})
	if err != nil {
		t.Fatalf("update implementer_session: %v", err)
	}

	// Step 3: Sync A -> server -> B
	if _, err := h.Push("client-A", sessProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Pull("client-B", sessProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	// Step 4: Verify B has implementer_session and creator_session
	ent := h.QueryEntity("client-B", "issues", issueID)
	if ent == nil {
		t.Fatal("client-B: issue not found after pull")
	}
	if v, _ := ent["implementer_session"].(string); v != "ses-impl-A" {
		t.Fatalf("client-B: expected implementer_session='ses-impl-A', got %q", v)
	}
	if v, _ := ent["creator_session"].(string); v != "ses-creator-A" {
		t.Fatalf("client-B: expected creator_session='ses-creator-A', got %q", v)
	}

	// Step 5: Client B sets reviewer_session (simulates review)
	err = h.Mutate("client-B", "update", "issues", issueID, map[string]any{
		"title":               "Session sync test",
		"status":              "in_review",
		"type":                "task",
		"priority":            "P2",
		"creator_session":     "ses-creator-A",
		"implementer_session": "ses-impl-A",
		"reviewer_session":    "ses-reviewer-B",
	})
	if err != nil {
		t.Fatalf("update reviewer_session: %v", err)
	}

	// Step 6: Sync B -> server -> A
	if _, err := h.Push("client-B", sessProj); err != nil {
		t.Fatalf("push B: %v", err)
	}
	if _, err := h.Pull("client-A", sessProj); err != nil {
		t.Fatalf("pull A: %v", err)
	}

	h.AssertConverged(sessProj)

	// Step 7: Verify both clients have all three session fields
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", issueID)
		if ent == nil {
			t.Fatalf("%s: issue not found", cid)
		}
		if v, _ := ent["implementer_session"].(string); v != "ses-impl-A" {
			t.Fatalf("%s: expected implementer_session='ses-impl-A', got %q", cid, v)
		}
		if v, _ := ent["creator_session"].(string); v != "ses-creator-A" {
			t.Fatalf("%s: expected creator_session='ses-creator-A', got %q", cid, v)
		}
		if v, _ := ent["reviewer_session"].(string); v != "ses-reviewer-B" {
			t.Fatalf("%s: expected reviewer_session='ses-reviewer-B', got %q", cid, v)
		}
	}
}

// TestSessionFields_EmptyStringSyncs verifies that empty string session fields
// are preserved through sync (not dropped as NULL/missing).
func TestSessionFields_EmptyStringSyncs(t *testing.T) {
	h := NewHarness(t, 2, sessProj)

	issueID := "td-sess2"

	// Client A creates issue with implementer_session set, others empty
	err := h.Mutate("client-A", "create", "issues", issueID, map[string]any{
		"title":               "Empty session test",
		"status":              "in_progress",
		"type":                "task",
		"priority":            "P2",
		"implementer_session": "ses-impl-X",
		"creator_session":     "",
		"reviewer_session":    "",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Sync A -> server -> B
	if _, err := h.Push("client-A", sessProj); err != nil {
		t.Fatalf("push A: %v", err)
	}
	if _, err := h.Pull("client-B", sessProj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	h.AssertConverged(sessProj)

	// Verify B has implementer_session set, and the empty fields are empty (not NULL)
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", issueID)
		if ent == nil {
			t.Fatalf("%s: issue not found", cid)
		}
		if v, _ := ent["implementer_session"].(string); v != "ses-impl-X" {
			t.Fatalf("%s: expected implementer_session='ses-impl-X', got %q", cid, v)
		}
		// Empty strings should be preserved, not nil/missing
		if v, ok := ent["creator_session"]; ok {
			if s, isStr := v.(string); isStr && s != "" {
				t.Fatalf("%s: expected creator_session='', got %q", cid, s)
			}
		}
		if v, ok := ent["reviewer_session"]; ok {
			if s, isStr := v.(string); isStr && s != "" {
				t.Fatalf("%s: expected reviewer_session='', got %q", cid, s)
			}
		}
	}
}

// TestSessionFields_NotOverwrittenBySync verifies that when client B syncs an
// update that doesn't touch session fields, the previously-synced session values
// on client A are not wiped out.
func TestSessionFields_NotOverwrittenBySync(t *testing.T) {
	h := NewHarness(t, 2, sessProj)

	issueID := "td-sess3"

	// Client A creates issue with all session fields set
	err := h.Mutate("client-A", "create", "issues", issueID, map[string]any{
		"title":               "Overwrite test",
		"status":              "in_progress",
		"type":                "task",
		"priority":            "P2",
		"implementer_session": "ses-impl-orig",
		"creator_session":     "ses-creator-orig",
		"reviewer_session":    "ses-reviewer-orig",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Sync to both
	if err := h.Sync("client-A", sessProj); err != nil {
		t.Fatalf("sync A1: %v", err)
	}
	if err := h.Sync("client-B", sessProj); err != nil {
		t.Fatalf("sync B1: %v", err)
	}

	// Client B updates title only but includes all fields in the update
	// (as the sync system does INSERT OR REPLACE with full row)
	err = h.Mutate("client-B", "update", "issues", issueID, map[string]any{
		"title":               "Overwrite test - updated",
		"status":              "in_progress",
		"type":                "task",
		"priority":            "P2",
		"implementer_session": "ses-impl-orig",
		"creator_session":     "ses-creator-orig",
		"reviewer_session":    "ses-reviewer-orig",
	})
	if err != nil {
		t.Fatalf("update B: %v", err)
	}

	// Sync B -> server -> A
	if err := h.Sync("client-B", sessProj); err != nil {
		t.Fatalf("sync B2: %v", err)
	}
	if err := h.Sync("client-A", sessProj); err != nil {
		t.Fatalf("sync A2: %v", err)
	}

	h.AssertConverged(sessProj)

	// Verify session fields are intact on both
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "issues", issueID)
		if ent == nil {
			t.Fatalf("%s: issue not found", cid)
		}
		if v, _ := ent["implementer_session"].(string); v != "ses-impl-orig" {
			t.Fatalf("%s: implementer_session lost, got %q", cid, v)
		}
		if v, _ := ent["creator_session"].(string); v != "ses-creator-orig" {
			t.Fatalf("%s: creator_session lost, got %q", cid, v)
		}
		if v, _ := ent["reviewer_session"].(string); v != "ses-reviewer-orig" {
			t.Fatalf("%s: reviewer_session lost, got %q", cid, v)
		}
	}
}
