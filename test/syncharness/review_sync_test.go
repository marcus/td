package syncharness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/db"
)

const reviewSyncProject = "proj-review-sync"

func reviewDB(t *testing.T, h *Harness, clientID string) *db.DB {
	t.Helper()
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, ".todos"), 0o755); err != nil {
		t.Fatalf("create lock directory: %v", err)
	}
	return db.NewWithConn(h.Clients[clientID].DB, baseDir)
}

func markReviewPeersBootstrapped(t *testing.T, h *Harness) {
	t.Helper()
	for _, clientID := range []string{"client-A", "client-B"} {
		if _, err := h.Clients[clientID].DB.Exec(`
			INSERT INTO sync_state (project_id, last_pulled_server_seq)
			VALUES (?, 1)
			ON CONFLICT(project_id) DO UPDATE SET last_pulled_server_seq = 1
		`, reviewSyncProject); err != nil {
			t.Fatalf("mark %s bootstrapped: %v", clientID, err)
		}
	}
}

func TestIssueReviewCreate_SyncsActiveApprovalAcrossPeers(t *testing.T) {
	h := NewHarness(t, 2, reviewSyncProject)

	const issueID = "td-review-sync"
	if err := h.Mutate("client-A", "create", "issues", issueID, map[string]any{
		"title":  "Review me",
		"status": "in_review",
	}); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := h.Sync("client-A", reviewSyncProject); err != nil {
		t.Fatalf("sync issue from A: %v", err)
	}
	if err := h.Sync("client-B", reviewSyncProject); err != nil {
		t.Fatalf("sync issue to B: %v", err)
	}
	// A real client persists that it has pulled before subsequent mutations.
	// Mark both peers bootstrapped so this test proves live emission rather
	// than passing through the legacy orphan-backfill safety net.
	markReviewPeersBootstrapped(t, h)

	peerA := reviewDB(t, h, "client-A")
	reviewID, err := peerA.CreateIssueReview(db.NewReview{
		IssueID:            issueID,
		ReviewerSession:    h.Clients["client-A"].SessionID,
		Decision:           "approved",
		Summary:            "looks good",
		RequestedBySession: h.Clients["client-B"].SessionID,
	})
	if err != nil {
		t.Fatalf("create review on A: %v", err)
	}

	if err := h.Sync("client-A", reviewSyncProject); err != nil {
		t.Fatalf("sync review from A: %v", err)
	}
	if err := h.Sync("client-B", reviewSyncProject); err != nil {
		t.Fatalf("sync review to B: %v", err)
	}

	peerB := reviewDB(t, h, "client-B")
	active, err := peerB.GetActiveApprovalReview(issueID)
	if err != nil {
		t.Fatalf("get active approval on B: %v", err)
	}
	if active == nil {
		t.Fatal("peer B has no active approval after peer A created and synced one")
	}
	if active.ID != reviewID {
		t.Fatalf("peer B active review ID = %q, want %q", active.ID, reviewID)
	}
}

func TestIssueReviewSupersedeAndUndo_ConvergeAcrossPeers(t *testing.T) {
	h := NewHarness(t, 2, reviewSyncProject)

	const issueID = "td-review-supersede"
	if err := h.Mutate("client-A", "create", "issues", issueID, map[string]any{
		"title":  "Review twice",
		"status": "in_review",
	}); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := h.Sync("client-A", reviewSyncProject); err != nil {
		t.Fatalf("sync issue from A: %v", err)
	}
	if err := h.Sync("client-B", reviewSyncProject); err != nil {
		t.Fatalf("sync issue to B: %v", err)
	}
	markReviewPeersBootstrapped(t, h)

	peerA := reviewDB(t, h, "client-A")
	firstID, err := peerA.CreateIssueReview(db.NewReview{
		IssueID:         issueID,
		ReviewerSession: h.Clients["client-A"].SessionID,
		Decision:        "approved",
		Summary:         "first approval",
	})
	if err != nil {
		t.Fatalf("create first review: %v", err)
	}
	if err := h.Sync("client-A", reviewSyncProject); err != nil {
		t.Fatalf("sync first review from A: %v", err)
	}
	if err := h.Sync("client-B", reviewSyncProject); err != nil {
		t.Fatalf("sync first review to B: %v", err)
	}

	if err := peerA.SupersedeActiveReviewsLogged(issueID, h.Clients["client-A"].SessionID); err != nil {
		t.Fatalf("supersede first review: %v", err)
	}
	secondID, err := peerA.CreateIssueReview(db.NewReview{
		IssueID:         issueID,
		ReviewerSession: h.Clients["client-A"].SessionID,
		Decision:        "approved",
		Summary:         "second approval",
	})
	if err != nil {
		t.Fatalf("create second review: %v", err)
	}
	if err := h.Sync("client-A", reviewSyncProject); err != nil {
		t.Fatalf("sync replacement review from A: %v", err)
	}
	if err := h.Sync("client-B", reviewSyncProject); err != nil {
		t.Fatalf("sync replacement review to B: %v", err)
	}

	peerB := reviewDB(t, h, "client-B")
	reviews, err := peerB.ListIssueReviews(issueID)
	if err != nil {
		t.Fatalf("list reviews on B: %v", err)
	}
	if len(reviews) != 2 {
		t.Fatalf("peer B reviews = %d, want 2", len(reviews))
	}
	if reviews[0].ID != firstID || reviews[0].SupersededAt == nil {
		t.Fatalf("peer B first review not superseded: %+v", reviews[0])
	}
	active, err := peerB.GetActiveApprovalReview(issueID)
	if err != nil || active == nil || active.ID != secondID {
		t.Fatalf("peer B active review = %+v, err=%v; want %s", active, err, secondID)
	}

	// This is the review side of undo-after-approve: remove the review the
	// approve action created and reactivate the prior approval it superseded.
	if err := peerA.DeleteIssueReviewLogged(secondID, h.Clients["client-A"].SessionID); err != nil {
		t.Fatalf("undo created review: %v", err)
	}
	if err := peerA.ClearReviewSupersededAtLogged(firstID, h.Clients["client-A"].SessionID); err != nil {
		t.Fatalf("undo prior supersede: %v", err)
	}
	if err := h.Sync("client-A", reviewSyncProject); err != nil {
		t.Fatalf("sync undo from A: %v", err)
	}
	if err := h.Sync("client-B", reviewSyncProject); err != nil {
		t.Fatalf("sync undo to B: %v", err)
	}

	active, err = peerB.GetActiveApprovalReview(issueID)
	if err != nil || active == nil || active.ID != firstID {
		t.Fatalf("peer B active review after undo = %+v, err=%v; want %s", active, err, firstID)
	}
	if got := h.QueryEntityRaw("client-B", "issue_reviews", secondID); got != nil {
		t.Fatalf("peer B still has undone review %s: %v", secondID, got)
	}
	h.AssertConverged(reviewSyncProject)
}
