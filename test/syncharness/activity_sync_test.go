package syncharness

import (
	"testing"
)

// ─── TestLogSync: log round-trip via push/pull ───

func TestLogSync(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// Client A creates a log
	err := h.Mutate("client-A", "create", "logs", "log-001", map[string]any{
		"issue_id":   "td-FAKE1",
		"session_id": "sess-A-0001",
		"message":    "Started investigation",
		"type":       "progress",
	})
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}

	// A pushes
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A: %v", err)
	}

	// B pulls
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	h.AssertConverged(proj)

	// Verify log appears on both clients with correct fields
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "logs", "log-001")
		if ent == nil {
			t.Fatalf("%s: log-001 not found", cid)
		}
		if v, _ := ent["issue_id"].(string); v != "td-FAKE1" {
			t.Fatalf("%s: expected issue_id 'td-FAKE1', got %q", cid, v)
		}
		if v, _ := ent["session_id"].(string); v != "sess-A-0001" {
			t.Fatalf("%s: expected session_id 'sess-A-0001', got %q", cid, v)
		}
		if v, _ := ent["message"].(string); v != "Started investigation" {
			t.Fatalf("%s: expected message 'Started investigation', got %q", cid, v)
		}
		if v, _ := ent["type"].(string); v != "progress" {
			t.Fatalf("%s: expected type 'progress', got %q", cid, v)
		}
	}
}

// ─── TestCommentSync: comment round-trip via push/pull ───

func TestCommentSync(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// Client A creates a comment
	err := h.Mutate("client-A", "create", "comments", "cmt-001", map[string]any{
		"issue_id":   "td-FAKE2",
		"session_id": "sess-A-0001",
		"text":       "This needs review",
	})
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}

	// A pushes
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A: %v", err)
	}

	// B pulls
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	h.AssertConverged(proj)

	// Verify comment appears on both clients with correct fields
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "comments", "cmt-001")
		if ent == nil {
			t.Fatalf("%s: cmt-001 not found", cid)
		}
		if v, _ := ent["issue_id"].(string); v != "td-FAKE2" {
			t.Fatalf("%s: expected issue_id 'td-FAKE2', got %q", cid, v)
		}
		if v, _ := ent["session_id"].(string); v != "sess-A-0001" {
			t.Fatalf("%s: expected session_id 'sess-A-0001', got %q", cid, v)
		}
		if v, _ := ent["text"].(string); v != "This needs review" {
			t.Fatalf("%s: expected text 'This needs review', got %q", cid, v)
		}
	}
}

// ─── TestWorkSessionSync: work_session round-trip via push/pull ───

func TestWorkSessionSync(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// Client A creates a work session
	err := h.Mutate("client-A", "create", "work_sessions", "ws-001", map[string]any{
		"name":       "Morning sprint",
		"session_id": "sess-A-0001",
	})
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}

	// A pushes
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A: %v", err)
	}

	// B pulls
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B: %v", err)
	}

	h.AssertConverged(proj)

	// Verify work session appears on both clients with correct fields
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "work_sessions", "ws-001")
		if ent == nil {
			t.Fatalf("%s: ws-001 not found", cid)
		}
		if v, _ := ent["name"].(string); v != "Morning sprint" {
			t.Fatalf("%s: expected name 'Morning sprint', got %q", cid, v)
		}
		if v, _ := ent["session_id"].(string); v != "sess-A-0001" {
			t.Fatalf("%s: expected session_id 'sess-A-0001', got %q", cid, v)
		}
	}
}

// ─── TestWorkSessionUpdateSync: work_session create + update round-trip ───

func TestWorkSessionUpdateSync(t *testing.T) {
	h := NewHarness(t, 2, proj)

	// Client A creates a work session
	err := h.Mutate("client-A", "create", "work_sessions", "ws-002", map[string]any{
		"name":       "Afternoon session",
		"session_id": "sess-A-0001",
		"start_sha":  "abc123",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A pushes the create
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A1: %v", err)
	}

	// B pulls to get the initial work session
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B1: %v", err)
	}

	// Verify B has the initial version
	ent := h.QueryEntity("client-B", "work_sessions", "ws-002")
	if ent == nil {
		t.Fatal("client-B: ws-002 should exist after first pull")
	}
	if v, _ := ent["name"].(string); v != "Afternoon session" {
		t.Fatalf("client-B: expected name 'Afternoon session', got %q", v)
	}

	// Client A updates the work session (sets ended_at and end_sha)
	err = h.Mutate("client-A", "update", "work_sessions", "ws-002", map[string]any{
		"name":       "Afternoon session",
		"session_id": "sess-A-0001",
		"start_sha":  "abc123",
		"ended_at":   "2025-01-15T17:30:00Z",
		"end_sha":    "def456",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	// A pushes the update
	if _, err := h.Push("client-A", proj); err != nil {
		t.Fatalf("push A2: %v", err)
	}

	// B pulls the update
	if _, err := h.Pull("client-B", proj); err != nil {
		t.Fatalf("pull B2: %v", err)
	}

	h.AssertConverged(proj)

	// Verify updated fields on both clients
	for _, cid := range []string{"client-A", "client-B"} {
		ent := h.QueryEntity(cid, "work_sessions", "ws-002")
		if ent == nil {
			t.Fatalf("%s: ws-002 not found", cid)
		}
		if v, _ := ent["name"].(string); v != "Afternoon session" {
			t.Fatalf("%s: expected name 'Afternoon session', got %q", cid, v)
		}
		if v, _ := ent["session_id"].(string); v != "sess-A-0001" {
			t.Fatalf("%s: expected session_id 'sess-A-0001', got %q", cid, v)
		}
		if v, _ := ent["start_sha"].(string); v != "abc123" {
			t.Fatalf("%s: expected start_sha 'abc123', got %q", cid, v)
		}
		if v, _ := ent["end_sha"].(string); v != "def456" {
			t.Fatalf("%s: expected end_sha 'def456', got %q", cid, v)
		}
		// ended_at is a timestamp column so it's excluded from convergence checks,
		// but we can verify it's non-nil on the source client
		if cid == "client-A" {
			if ent["ended_at"] == nil {
				t.Fatalf("%s: ended_at should be set", cid)
			}
		}
	}
}
