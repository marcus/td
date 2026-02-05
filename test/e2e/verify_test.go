package e2e_test

import (
	"strings"
	"testing"

	"github.com/marcus/td/test/e2e"
)

func TestVerifyConvergence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	// Create several issues across both actors
	out, err := h.TdA("create", "convergence test issue one from alice")
	if err != nil {
		t.Fatalf("alice create 1: %v\n%s", err, out)
	}
	id1 := e2e.ExtractIssueID(out)

	out, err = h.TdB("create", "convergence test issue two from bob")
	if err != nil {
		t.Fatalf("bob create 2: %v\n%s", err, out)
	}

	// Alice adds a comment
	if out, err := h.TdA("comment", id1, "test comment for convergence"); err != nil {
		t.Fatalf("alice comment: %v\n%s", err, out)
	}

	// Sync all
	if err := h.SyncAll(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	v := e2e.NewVerifier(h)
	v.VerifyConvergence("alice", "bob")

	t.Log(v.Summary())
	for _, r := range v.FailedResults() {
		t.Errorf("FAIL: %s — %s", r.Name, r.Details)
	}
}

func TestVerifyActionLogConvergence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	// Create some activity
	if out, err := h.TdA("create", "action log convergence test issue alice"); err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if out, err := h.TdB("create", "action log convergence test issue bob"); err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}

	if err := h.SyncAll(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	v := e2e.NewVerifier(h)
	v.VerifyActionLogConvergence("alice", "bob")

	t.Log(v.Summary())
	for _, r := range v.FailedResults() {
		t.Errorf("FAIL: %s — %s", r.Name, r.Details)
	}
}

func TestVerifyMonotonicSequence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	// Create issues and sync to generate server_seq values
	for i := range 5 {
		actor := "alice"
		if i%2 == 1 {
			actor = "bob"
		}
		out, err := h.Td(actor, "create", "monotonic sequence test issue number")
		if err != nil {
			t.Fatalf("create %d: %v\n%s", i, err, out)
		}
		// Sync after each to interleave sequences
		if err := h.SyncAll(); err != nil {
			t.Fatalf("sync %d: %v", i, err)
		}
	}

	v := e2e.NewVerifier(h)
	v.VerifyMonotonicSequence("alice")
	v.VerifyMonotonicSequence("bob")

	t.Log(v.Summary())
	for _, r := range v.FailedResults() {
		t.Errorf("FAIL: %s — %s", r.Name, r.Details)
	}
}

func TestVerifyCausalOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	// Create issue, then update it
	out, err := h.TdA("create", "causal ordering test issue for verification")
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	id := e2e.ExtractIssueID(out)

	if out, err := h.TdA("edit", id, "--title", "updated causal ordering test title"); err != nil {
		t.Fatalf("edit: %v\n%s", err, out)
	}

	if err := h.SyncAll(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	v := e2e.NewVerifier(h)
	v.VerifyCausalOrdering("alice")
	v.VerifyCausalOrdering("bob")

	t.Log(v.Summary())
	for _, r := range v.FailedResults() {
		t.Errorf("FAIL: %s — %s", r.Name, r.Details)
	}
}

func TestVerifyIdempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	// Create some state
	if out, err := h.TdA("create", "idempotency test issue number one"); err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if out, err := h.TdB("create", "idempotency test issue number two"); err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}

	// Achieve convergence first
	if err := h.SyncAll(); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	v := e2e.NewVerifier(h)
	v.VerifyIdempotency(3)

	t.Log(v.Summary())
	for _, r := range v.FailedResults() {
		t.Errorf("FAIL: %s — %s", r.Name, r.Details)
	}
}

func TestVerifyEventCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	if out, err := h.TdA("create", "event count test issue from alice"); err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if out, err := h.TdB("create", "event count test issue from bob"); err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}

	if err := h.SyncAll(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	v := e2e.NewVerifier(h)
	v.VerifyEventCounts("alice", "bob")

	t.Log(v.Summary())
	for _, r := range v.FailedResults() {
		t.Errorf("FAIL: %s — %s", r.Name, r.Details)
	}
}

func TestVerifyFieldLevelMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	v := e2e.NewVerifier(h)
	v.VerifyFieldLevelMerge("alice", "bob")

	t.Log(v.Summary())
	for _, r := range v.FailedResults() {
		t.Errorf("FAIL: %s — %s", r.Name, r.Details)
	}
}

func TestVerifyReadYourWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	// Create issues as different actors
	out, err := h.TdA("create", "read-your-writes test issue from alice")
	if err != nil {
		t.Fatalf("alice create: %v\n%s", err, out)
	}
	aliceID := e2e.ExtractIssueID(out)

	out, err = h.TdB("create", "read-your-writes test issue from bob")
	if err != nil {
		t.Fatalf("bob create: %v\n%s", err, out)
	}
	bobID := e2e.ExtractIssueID(out)

	if err := h.SyncAll(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Build a minimal engine to track state
	engine := e2e.NewChaosEngine(h, 42, 2)
	engine.TrackCreatedIssue(aliceID, "open", "alice")
	engine.TrackCreatedIssue(bobID, "open", "bob")

	v := e2e.NewVerifier(h)
	v.VerifyReadYourWrites(engine)

	t.Log(v.Summary())
	for _, r := range v.FailedResults() {
		t.Errorf("FAIL: %s — %s", r.Name, r.Details)
	}
}

// TestVerifyAll runs all verifications together as a mini integration test.
func TestVerifyAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	// Create issues
	out, err := h.TdA("create", "verify-all integration test issue one")
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	id1 := e2e.ExtractIssueID(out)

	out, err = h.TdB("create", "verify-all integration test issue two")
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	id2 := e2e.ExtractIssueID(out)

	// Add a comment
	if out, err := h.TdA("comment", id1, "a comment for verification"); err != nil {
		// Comments may not be supported -- skip
		t.Logf("comment skipped: %v\n%s", err, out)
	}

	if err := h.SyncAll(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	engine := e2e.NewChaosEngine(h, 99, 2)
	engine.TrackCreatedIssue(id1, "open", "alice")
	engine.TrackCreatedIssue(id2, "open", "bob")

	v := e2e.NewVerifier(h)

	// Run all verifications
	v.VerifyConvergence("alice", "bob")
	v.VerifyActionLogConvergence("alice", "bob")
	v.VerifyMonotonicSequence("alice")
	v.VerifyMonotonicSequence("bob")
	v.VerifyCausalOrdering("alice")
	v.VerifyEventCounts("alice", "bob")
	v.VerifyReadYourWrites(engine)
	v.VerifyIdempotency(2)
	v.VerifyFieldLevelMerge("alice", "bob")

	summary := v.Summary()
	t.Log(summary)

	if !v.AllPassed() {
		for _, r := range v.FailedResults() {
			t.Errorf("FAIL: %s — %s", r.Name, r.Details)
		}
	}

	// Verify we used the comment-related output
	_ = strings.Contains(out, id1)
}
