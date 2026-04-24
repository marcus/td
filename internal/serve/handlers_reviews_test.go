package serve

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

// setDelegatedMode flips review_policy_mode=delegated in the project config so
// the record-review endpoint is enabled for the test.
func setDelegatedMode(t *testing.T, baseDir string) {
	t.Helper()
	if err := config.SetFeatureStringFlag(baseDir, features.ReviewPolicyMode, string(reviewpolicy.ModeDelegated)); err != nil {
		t.Fatalf("set review policy: %v", err)
	}
}

// seedInReviewIssue creates an issue, marks it in_progress with an
// implementer that isn't the given session, then pushes it to in_review.
// Returns the issue ID.
func seedInReviewIssue(t *testing.T, database *db.DB, implementer string) string {
	t.Helper()
	issue := &models.Issue{
		Title:       "Review-target issue",
		Type:        models.TypeTask,
		Status:      models.StatusOpen,
		Priority:    models.PriorityP2,
		Description: "body",
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	issue.Status = models.StatusInReview
	issue.ImplementerSession = implementer
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("update issue: %v", err)
	}
	_ = database.RecordSessionAction(issue.ID, implementer, models.ActionSessionStarted)
	return issue.ID
}

func TestIntegration_Reviews_Success(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	body := map[string]interface{}{
		"decision": reviewpolicy.DecisionApproved,
		"summary":  "looks good",
	}
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", body)
	ok, data, errP := iParseEnvelope(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d, err=%v", resp.StatusCode, errP)
	}
	if !ok {
		t.Fatalf("ok=false data=%v err=%v", data, errP)
	}
	// Active review should be populated.
	if _, got := data["active_review"]; !got {
		t.Fatalf("expected active_review in response, got %v", data)
	}
	// Issue should still be in_review.
	issue, _ := database.GetIssue(issueID)
	if issue == nil || issue.Status != models.StatusInReview {
		t.Fatalf("issue status = %v, want in_review", issue)
	}
	if issue.ReviewerSession == "" {
		t.Fatalf("reviewer_session not stamped")
	}
	active, _ := database.GetActiveApprovalReview(issueID)
	if active == nil {
		t.Fatalf("no active approval found after /reviews")
	}
}

func TestIntegration_Reviews_MissingSummary(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision": "approved",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIntegration_Reviews_InvalidDecision(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision": "totally-fine",
		"summary":  "nope",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIntegration_Reviews_StrictMode_Conflict(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	// Explicitly pin to strict mode. Step 5 flipped the default to delegated,
	// so this test now has to opt into strict via env.
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "strict")

	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision": "approved",
		"summary":  "ok",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestIntegration_Reviews_MinorRejected asserts that minor issues cannot
// accept record-only reviews. Minor issues bypass review entirely and
// self-close in one step — a review row would be meaningless. The handler
// must reject with 400 regardless of the issue's current status.
func TestIntegration_Reviews_MinorRejected(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	// Seed a minor issue in in_review status (worst case — this is where the
	// previous broken gate would have let the review through).
	issue := &models.Issue{
		Title:    "Minor task",
		Type:     models.TypeTask,
		Status:   models.StatusInReview,
		Priority: models.PriorityP2,
		Minor:    true,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	issue.ImplementerSession = "ses-other-impl"
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("update issue: %v", err)
	}

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issue.ID+"/reviews", map[string]interface{}{
		"decision": reviewpolicy.DecisionApproved,
		"summary":  "nope, minor shouldn't need this",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// Also cover the case the previous bug exposed: open minor issue. Before
	// the fix, `status != in_review && !minor` short-circuited to skip the
	// gate entirely.
	openMinor := &models.Issue{
		Title:    "Minor open",
		Type:     models.TypeTask,
		Status:   models.StatusOpen,
		Priority: models.PriorityP2,
		Minor:    true,
	}
	if err := database.CreateIssue(openMinor); err != nil {
		t.Fatalf("create: %v", err)
	}
	resp2 := iDoJSON(t, "POST", baseURL+"/v1/issues/"+openMinor.ID+"/reviews", map[string]interface{}{
		"decision": reviewpolicy.DecisionApproved,
		"summary":  "should also be rejected",
	})
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("open minor status=%d, want 400", resp2.StatusCode)
	}
	resp2.Body.Close()
}

func TestIntegration_Reviews_IneligibleReviewer(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	// Implementer == web session means the web session can't review itself.
	sess, err := GetOrCreateWebSession(database)
	if err != nil {
		t.Fatalf("get web session: %v", err)
	}
	issueID := seedInReviewIssue(t, database, sess.ID)

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision": "approved",
		"summary":  "LGTM",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIntegration_Approve_CloseAfterReview(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	sess, err := GetOrCreateWebSession(database)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	// Create an issue reviewed by another session. The web session does not
	// need a close role once the independent approval exists.
	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	issue, _ := database.GetIssue(issueID)
	issue.ReviewRequestedBySession = sess.ID
	_ = database.UpdateIssue(issue)

	// Another session records an approval review directly in DB (simulates a
	// different reviewer having recorded review).
	_, err = database.CreateIssueReview(issueID, "ses-reviewer", reviewpolicy.DecisionApproved, "looks good", sess.ID)
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	issue, _ = database.GetIssue(issueID)
	issue.ReviewerSession = "ses-reviewer"
	_ = database.UpdateIssue(issue)

	// Web session approves = close-after-review, must supply a reason because
	// closer != reviewer_of_record.
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
		"reason": "shipping it",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	final, _ := database.GetIssue(issueID)
	if final.Status != models.StatusClosed {
		t.Fatalf("final status=%v want closed", final.Status)
	}
	if final.ClosedBySession != sess.ID {
		t.Fatalf("closed_by_session=%q want %q", final.ClosedBySession, sess.ID)
	}
	// Reviewer of record must be preserved.
	if final.ReviewerSession != "ses-reviewer" {
		t.Fatalf("reviewer_session=%q want ses-reviewer", final.ReviewerSession)
	}
}

func TestIntegration_Approve_CloseAfterReview_RequiresReason(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	sess, err := GetOrCreateWebSession(database)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	issue, _ := database.GetIssue(issueID)
	issue.ReviewRequestedBySession = sess.ID
	_ = database.UpdateIssue(issue)

	_, _ = database.CreateIssueReview(issueID, "ses-reviewer", reviewpolicy.DecisionApproved, "", sess.ID)
	issue, _ = database.GetIssue(issueID)
	issue.ReviewerSession = "ses-reviewer"
	_ = database.UpdateIssue(issue)

	// Missing reason → 400
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIntegration_Reject_SupersedesActiveApproval(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	reviewID, err := database.CreateIssueReview(issueID, "ses-reviewer", reviewpolicy.DecisionApproved, "", "")
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	issue, _ := database.GetIssue(issueID)
	issue.ReviewerSession = "ses-reviewer"
	_ = database.UpdateIssue(issue)

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reject", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject status=%d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	active, _ := database.GetActiveApprovalReview(issueID)
	if active != nil {
		t.Fatalf("active approval %s should have been superseded by reject (row still live)", reviewID)
	}
}

// TestIntegration_Reviews_UndoPayload verifies the action_log row written for
// a record-review carries the new ReviewUndoPayload shape so `td undo` can
// remove the inserted review row and clear active approval state. This is
// the undo-integration parity check Step 3 calls out.
func TestIntegration_Reviews_UndoPayload(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision": reviewpolicy.DecisionApproved,
		"summary":  "good",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	// The inserted review row must be findable.
	reviews, err := database.ListIssueReviews(issueID)
	if err != nil || len(reviews) != 1 {
		t.Fatalf("reviews=%v err=%v", reviews, err)
	}
	reviewID := reviews[0].ID

	// Undoing the most recent action on the reviewer session must remove
	// the review row. Undo path is covered by cmd/undo_test.go; here we
	// verify the action_log carries the created_review_id needed for that
	// undo to succeed.
	logs, err := database.GetRecentActionsAll(5)
	if err != nil {
		t.Fatalf("get actions: %v", err)
	}
	var actionType string
	var newData string
	for _, a := range logs {
		if a.EntityID == issueID && (a.ActionType == models.ActionReviewApprove) {
			actionType = string(a.ActionType)
			newData = a.NewData
			break
		}
	}
	if actionType == "" {
		t.Fatalf("did not find ActionReviewApprove for %s in action_log", issueID)
	}
	if newData == "" {
		t.Fatalf("action_log NewData is empty; undo path cannot roll back review %s", reviewID)
	}
	// NewData should deserialize into ReviewUndoPayload with CreatedReviewID set.
	var payload models.ReviewUndoPayload
	if err := json.Unmarshal([]byte(newData), &payload); err != nil {
		t.Fatalf("unmarshal NewData as ReviewUndoPayload: %v", err)
	}
	if payload.CreatedReviewID != reviewID {
		t.Fatalf("CreatedReviewID=%q, want %q", payload.CreatedReviewID, reviewID)
	}
}

func TestIntegration_Approve_DirectReviewerClose(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	// An issue with implementer=different, web session uninvolved → web
	// session can act as reviewer.
	issueID := seedInReviewIssue(t, database, "ses-other-impl")

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	final, _ := database.GetIssue(issueID)
	if final.Status != models.StatusClosed {
		t.Fatalf("status=%v want closed", final.Status)
	}
	if final.ClosedBySession == "" || final.ReviewerSession == "" {
		t.Fatalf("expected reviewer+closed-by stamped: %+v", final)
	}
}
