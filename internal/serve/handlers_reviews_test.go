package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	_, err = database.CreateIssueReview(db.NewReview{IssueID: issueID, ReviewerSession: "ses-reviewer", Decision: reviewpolicy.DecisionApproved, Summary: "looks good", RequestedBySession: sess.ID})
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

	_, _ = database.CreateIssueReview(db.NewReview{IssueID: issueID, ReviewerSession: "ses-reviewer", Decision: reviewpolicy.DecisionApproved, RequestedBySession: sess.ID})
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
	reviewID, err := database.CreateIssueReview(db.NewReview{IssueID: issueID, ReviewerSession: "ses-reviewer", Decision: reviewpolicy.DecisionApproved})
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

func TestIntegration_Reject_ReviewEventFailureLeavesIssueUnchanged(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	reviewID, err := database.CreateIssueReview(db.NewReview{
		IssueID:         issueID,
		ReviewerSession: "ses-reviewer",
		Decision:        reviewpolicy.DecisionApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().Exec(`
		CREATE TRIGGER fail_issue_review_action
		BEFORE INSERT ON action_log
		WHEN NEW.entity_type = 'issue_reviews'
		BEGIN
			SELECT RAISE(ABORT, 'injected issue_reviews action failure');
		END;
	`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reject", nil)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("reject status=%d, want 500", resp.StatusCode)
	}
	resp.Body.Close()

	unchanged, err := database.GetIssue(issueID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != models.StatusInReview {
		t.Fatalf("issue status = %s, want in_review", unchanged.Status)
	}
	active, err := database.GetActiveApprovalReview(issueID)
	if err != nil || active == nil || active.ID != reviewID {
		t.Fatalf("active review changed: active=%+v err=%v", active, err)
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

func TestIntegration_RecordReview_SupersedeEventFailureLeavesHistoryUnchanged(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	firstID, err := database.CreateIssueReview(db.NewReview{
		IssueID:         issueID,
		ReviewerSession: "ses-first-reviewer",
		Decision:        reviewpolicy.DecisionChangesRequested,
		Summary:         "first pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().Exec(`
		CREATE TRIGGER fail_issue_review_action
		BEFORE INSERT ON action_log
		WHEN NEW.entity_type = 'issue_reviews'
		BEGIN
			SELECT RAISE(ABORT, 'injected issue_reviews action failure');
		END;
	`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision": reviewpolicy.DecisionChangesRequested,
		"summary":  "second pass",
	})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("record review status=%d, want 500", resp.StatusCode)
	}
	resp.Body.Close()

	reviews, err := database.ListIssueReviews(issueID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].ID != firstID || reviews[0].SupersededAt != nil {
		t.Fatalf("review history changed after failed supersede: %+v", reviews)
	}
	unchanged, err := database.GetIssue(issueID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != models.StatusInReview {
		t.Fatalf("issue status = %s, want in_review", unchanged.Status)
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

func TestIntegration_Approve_ReviewEventFailureDoesNotCloseIssue(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	if _, err := database.Conn().Exec(`
		CREATE TRIGGER fail_issue_review_action
		BEFORE INSERT ON action_log
		WHEN NEW.entity_type = 'issue_reviews'
		BEGIN
			SELECT RAISE(ABORT, 'injected issue_reviews action failure');
		END;
	`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", nil)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("approve status=%d, want 500", resp.StatusCode)
	}
	resp.Body.Close()

	unchanged, err := database.GetIssue(issueID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != models.StatusInReview {
		t.Fatalf("issue status = %s, want in_review", unchanged.Status)
	}
	reviews, err := database.ListIssueReviews(issueID)
	if err != nil || len(reviews) != 0 {
		t.Fatalf("review row committed without event: count=%d err=%v", len(reviews), err)
	}
}

// setTrustedMode flips review_policy_mode=trusted in the project config.
func setTrustedMode(t *testing.T, baseDir string) {
	t.Helper()
	if err := config.SetFeatureStringFlag(baseDir, features.ReviewPolicyMode, string(reviewpolicy.ModeTrusted)); err != nil {
		t.Fatalf("set review policy: %v", err)
	}
}

// webSessionID returns the server's fixed web session ID (idempotent fetch).
func webSessionID(t *testing.T, database *db.DB) string {
	t.Helper()
	sess, err := GetOrCreateWebSession(database)
	if err != nil {
		t.Fatalf("GetOrCreateWebSession: %v", err)
	}
	return sess.ID
}

// TestIntegration_Approve_TrustedSelfReview_RequiresAck proves that in trusted
// mode an implementer (the web session here) cannot approve their own work
// without acknowledging the self-review — the serve approve path enforces the
// same rule as the CLI and surfaces the trusted teaching message.
func TestIntegration_Approve_TrustedSelfReview_RequiresAck(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	// Seed an issue implemented by the web session itself → self-review.
	issueID := seedInReviewIssue(t, database, webSessionID(t, database))

	// Without self_review: rejected with the trusted teaching message.
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
		"reason": "I reviewed my own diff",
	})
	_, _, errP := iParseEnvelope(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403 (self-review without ack)", resp.StatusCode)
	}
	if errP == nil || !strings.Contains(asString(errP["message"]), "self-review") {
		t.Fatalf("expected trusted teaching message, got %v", errP)
	}
	// Issue must remain in_review.
	issue, _ := database.GetIssue(issueID)
	if issue.Status != models.StatusInReview {
		t.Fatalf("status=%v want in_review (rejected)", issue.Status)
	}
}

// TestIntegration_Approve_TrustedSelfReview_RequiresReason proves the reason
// gate: self_review=true without a reason is rejected (400), mirroring the CLI.
func TestIntegration_Approve_TrustedSelfReview_RequiresReason(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, webSessionID(t, database))

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
		"self_review": true,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 (self_review without reason)", resp.StatusCode)
	}
	resp.Body.Close()

	issue, _ := database.GetIssue(issueID)
	if issue.Status != models.StatusInReview {
		t.Fatalf("status=%v want in_review (rejected)", issue.Status)
	}
}

// TestIntegration_Approve_TrustedSelfReview_Allowed proves the happy path: an
// implementer with self_review=true and a reason approves+closes, and the
// recorded issue_review row is stamped self_review=true for audit.
func TestIntegration_Approve_TrustedSelfReview_Allowed(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, webSessionID(t, database))

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
		"self_review": true,
		"reason":      "reviewed my own diff, tests pass",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200 (self_review+reason)", resp.StatusCode)
	}
	resp.Body.Close()

	final, _ := database.GetIssue(issueID)
	if final.Status != models.StatusClosed {
		t.Fatalf("status=%v want closed", final.Status)
	}
	// The recorded review row must carry self_review=true for audit.
	reviews, err := database.ListIssueReviews(issueID)
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}
	if len(reviews) == 0 {
		t.Fatalf("expected a recorded approval review, got none")
	}
	var found bool
	for _, rv := range reviews {
		if rv.Decision == reviewpolicy.DecisionApproved {
			found = true
			if !rv.SelfReview {
				t.Fatalf("approval review should be stamped self_review=true, got %+v", rv)
			}
		}
	}
	if !found {
		t.Fatalf("no approved review row found in %v", reviews)
	}
}

// TestIntegration_Approve_TrustedNonImplementer_NoSelfReview proves that a
// non-implementer (the web session, with the issue implemented by a different
// session) approves+closes in trusted mode without needing self_review, and the
// recorded review is NOT a self-review.
func TestIntegration_Approve_TrustedNonImplementer_NoSelfReview(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	// Implemented by a different session → web session is an independent reviewer.
	issueID := seedInReviewIssue(t, database, "ses-other-impl")

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200 (non-implementer approve)", resp.StatusCode)
	}
	resp.Body.Close()

	final, _ := database.GetIssue(issueID)
	if final.Status != models.StatusClosed {
		t.Fatalf("status=%v want closed", final.Status)
	}
	reviews, _ := database.ListIssueReviews(issueID)
	for _, rv := range reviews {
		if rv.Decision == reviewpolicy.DecisionApproved && rv.SelfReview {
			t.Fatalf("non-implementer approval must not be stamped self_review: %+v", rv)
		}
	}
}

// TestIntegration_Reviews_TrustedMode_Allowed asserts the record-review
// endpoint works in the default trusted mode. It used to 409 outside delegated
// mode, which left trusted able to CLOSE on a recorded approval it could not
// create.
func TestIntegration_Reviews_TrustedMode_Allowed(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, "ses-other-impl")
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision": reviewpolicy.DecisionApproved,
		"summary":  "reviewed the diff",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	final, _ := database.GetIssue(issueID)
	if final.Status != models.StatusInReview {
		t.Fatalf("status=%v want in_review (record-only must not close)", final.Status)
	}
	active, err := database.GetActiveApprovalReview(issueID)
	if err != nil || active == nil {
		t.Fatalf("expected an active approval review, got %v (err=%v)", active, err)
	}
	if active.SelfReview {
		t.Fatalf("independent record-only review must not be flagged self_review: %+v", active)
	}
}

// TestIntegration_Reviews_TrustedMode_ImplementerNeedsAck mirrors the CLI: an
// implementation-involved session cannot silently attest to its own work on the
// record-only path, but may with an explicit self_review acknowledgement.
func TestIntegration_Reviews_TrustedMode_ImplementerNeedsAck(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, webSessionID(t, database))

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision": reviewpolicy.DecisionApproved,
		"summary":  "my own work",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403 without self_review ack", resp.StatusCode)
	}
	resp.Body.Close()
	if active, _ := database.GetActiveApprovalReview(issueID); active != nil {
		t.Fatal("expected no review row for an unacknowledged self-review")
	}

	resp = iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision":    reviewpolicy.DecisionApproved,
		"summary":     "reviewed my own diff",
		"self_review": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d, want 201 with self_review ack", resp.StatusCode)
	}
	resp.Body.Close()

	active, _ := database.GetActiveApprovalReview(issueID)
	if active == nil {
		t.Fatal("expected a recorded review")
	}
	if !active.SelfReview {
		t.Fatalf("acknowledged self-review must be stamped self_review: %+v", active)
	}
}

// TestIntegration_Approve_CloseAfterReview_TrustedMode is the end-to-end
// regression for the whole point of opening record-only to trusted: the
// implementer records nothing, an independent session attests, and the
// implementer then closes on that attestation — with no self_review claim
// anywhere. handledCloseAfterReview used to be delegated-only, so this
// dead-ended at 403 in the default mode and left the recorded approval
// unusable on the API.
func TestIntegration_Approve_CloseAfterReview_TrustedMode(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	// The web session is the IMPLEMENTER here — the session that, without a
	// recorded approval, would be forced into --self-review.
	implSession := webSessionID(t, database)
	issueID := seedInReviewIssue(t, database, implSession)

	// An independent session records the approval.
	if _, err := database.CreateIssueReview(db.NewReview{IssueID: issueID, ReviewerSession: "ses-independent-reviewer", Decision: reviewpolicy.DecisionApproved, Summary: "reviewed the diff", RequestedBySession: implSession}); err != nil {
		t.Fatalf("create review: %v", err)
	}
	issue, _ := database.GetIssue(issueID)
	issue.ReviewerSession = "ses-independent-reviewer"
	_ = database.UpdateIssue(issue)

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
		"reason": "landing after reviewer sign-off",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200 (implementer closing on an independent approval)", resp.StatusCode)
	}
	resp.Body.Close()

	final, _ := database.GetIssue(issueID)
	if final.Status != models.StatusClosed {
		t.Fatalf("status=%v want closed", final.Status)
	}
	if final.ClosedBySession != implSession {
		t.Fatalf("closed_by_session=%q want %q", final.ClosedBySession, implSession)
	}
	if final.ReviewerSession != "ses-independent-reviewer" {
		t.Fatalf("reviewer_session=%q want the independent reviewer", final.ReviewerSession)
	}
	// No self-review was claimed or recorded anywhere in this flow.
	reviews, _ := database.ListIssueReviews(issueID)
	for _, rv := range reviews {
		if rv.SelfReview {
			t.Fatalf("no row should be flagged self_review in this flow: %+v", rv)
		}
	}
}

// asString coerces an interface map value to a string for assertions.
func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

// TestIntegration_Approve_ReviewedBy_ImplementerApproves is the API twin of the
// CLI's headline case: an implementation-involved session approves by naming
// who actually reviewed the work, and the row records both facts.
func TestIntegration_Approve_ReviewedBy_ImplementerApproves(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, webSessionID(t, database))

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
		"reviewed_by": "code-reviewer sub-agent",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200 (attributed approve, no reason required)", resp.StatusCode)
	}
	resp.Body.Close()

	final, _ := database.GetIssue(issueID)
	if final.Status != models.StatusClosed {
		t.Fatalf("status=%v want closed", final.Status)
	}
	active, _ := database.GetActiveApprovalReview(issueID)
	if active == nil {
		t.Fatal("expected an approval review row")
	}
	if active.ReviewedBy != "code-reviewer sub-agent" {
		t.Errorf("ReviewedBy = %q, want the attribution", active.ReviewedBy)
	}
	if !active.SelfReview {
		t.Error("row written by an involved session must be stamped self_review")
	}
}

// TestIntegration_Approve_ReviewedBy_Validation mirrors the CLI's edge checks.
// A surface that accepts input the CLI rejects is a parity bug — an agent
// switching between them would get different answers for the same request.
func TestIntegration_Approve_ReviewedBy_Validation(t *testing.T) {
	cases := []struct {
		name string
		body map[string]interface{}
	}{
		{"mutually exclusive with self_review", map[string]interface{}{"reviewed_by": "x", "self_review": true, "reason": "r"}},
		{"blank attribution", map[string]interface{}{"reviewed_by": "   "}},
		{"newline forgery", map[string]interface{}{"reviewed_by": "reviewer\nApproved by: someone-else"}},
		{"over the rune cap", map[string]interface{}{"reviewed_by": strings.Repeat("x", reviewpolicy.MaxReviewedByLen+1)}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			baseURL, database, cleanup := setupIntegrationServer(t)
			defer cleanup()
			setTrustedMode(t, database.BaseDir())

			issueID := seedInReviewIssue(t, database, webSessionID(t, database))
			resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", tc.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d want 400", resp.StatusCode)
			}
			resp.Body.Close()

			if final, _ := database.GetIssue(issueID); final.Status != models.StatusInReview {
				t.Errorf("status=%v want in_review (nothing should have happened)", final.Status)
			}
			if active, _ := database.GetActiveApprovalReview(issueID); active != nil {
				t.Error("no review row should be written when validation rejects")
			}
		})
	}
}

// TestIntegration_Approve_ReviewedBy_DoesNotGrantInDelegated is the security
// property at the API boundary — the same one the CLI test pins. A free-text
// field must not dissolve a mode chosen for a mechanical independence boundary.
func TestIntegration_Approve_ReviewedBy_DoesNotGrantInDelegated(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setDelegatedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, webSessionID(t, database))
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
		"reviewed_by": "sub-agent that definitely reviewed it",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
	resp.Body.Close()

	if final, _ := database.GetIssue(issueID); final.Status != models.StatusInReview {
		t.Errorf("status=%v want in_review", final.Status)
	}
}

// TestIntegration_Reviews_ReviewedBy covers the record-only endpoint and the
// DTO: a UI client must be able to render who reviewed the work without a
// second call.
func TestIntegration_Reviews_ReviewedBy(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, webSessionID(t, database))
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision":    reviewpolicy.DecisionApproved,
		"summary":     "sub-agent reviewed the diff",
		"reviewed_by": "code-reviewer sub-agent",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d want 201", resp.StatusCode)
	}
	ok, data, _ := iParseEnvelope(t, resp)
	if !ok {
		t.Fatal("expected a success envelope")
	}
	// The attribution must be visible on both the created row and the issue's
	// active_review, so a UI can render "reviewed by X" without a second call.
	review, _ := data["review"].(map[string]interface{})
	if got := asString(review["reviewed_by"]); got != "code-reviewer sub-agent" {
		t.Errorf("response review.reviewed_by = %q, want the attribution", got)
	}
	activeReview, _ := data["active_review"].(map[string]interface{})
	if got := asString(activeReview["reviewed_by"]); got != "code-reviewer sub-agent" {
		t.Errorf("response active_review.reviewed_by = %q, want the attribution", got)
	}
	if sr, _ := activeReview["self_review"].(bool); !sr {
		t.Error("active_review.self_review should be true for an involved recorder")
	}

	active, _ := database.GetActiveApprovalReview(issueID)
	if active == nil || active.ReviewedBy != "code-reviewer sub-agent" {
		t.Fatalf("expected an attributed review row, got %+v", active)
	}
	if !active.SelfReview {
		t.Error("row written by an involved session must be stamped self_review")
	}
}

// TestIntegration_Approve_ReviewedBy_ModeCRejects is the API twin of the CLI's
// Mode C rejection. The close-after-recorded-approval branch runs before the
// reviewer decision, so it needs its own guard — without it the API returned
// 200 and discarded the attribution while the CLI rejected the same request.
func TestIntegration_Approve_ReviewedBy_ModeCRejects(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	implSession := webSessionID(t, database)
	issueID := seedInReviewIssue(t, database, implSession)

	// An independent session records the approval.
	if _, err := database.CreateIssueReview(db.NewReview{
		IssueID:         issueID,
		ReviewerSession: "ses-independent-reviewer",
		Decision:        reviewpolicy.DecisionApproved,
		Summary:         "reviewed the diff",
	}); err != nil {
		t.Fatalf("create review: %v", err)
	}

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
		"reviewed_by": "a-DIFFERENT-reviewer-nobody-recorded",
		"reason":      "landing it",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 (attribution is meaningless when closing on an existing approval)", resp.StatusCode)
	}
	resp.Body.Close()

	if final, _ := database.GetIssue(issueID); final.Status != models.StatusInReview {
		t.Errorf("status=%v want in_review (the close should not have proceeded)", final.Status)
	}
	active, _ := database.GetActiveApprovalReview(issueID)
	if active == nil || active.ReviewedBy != "" || active.ReviewerSession != "ses-independent-reviewer" {
		t.Errorf("the recorded approval must be untouched: %+v", active)
	}

	// Without the flag the same close succeeds.
	resp = iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
		"reason": "landing it",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	resp.Body.Close()
	if final, _ := database.GetIssue(issueID); final.Status != models.StatusClosed {
		t.Errorf("status=%v want closed", final.Status)
	}
}

// TestIntegration_CanApprove_AttributionUnlocks covers the available-transitions
// hint. A UI that hides the Approve button for a call that would succeed is the
// same parity bug in reverse — the capability exists but is invisible.
func TestIntegration_CanApprove_AttributionUnlocks(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, webSessionID(t, database))

	resp := iDoJSON(t, "GET", baseURL+"/v1/issues/"+issueID, nil)
	ok, data, _ := iParseEnvelope(t, resp)
	if !ok {
		t.Fatal("expected a success envelope")
	}
	// available_transitions rides on the issue object, not the envelope root.
	issueObj, _ := data["issue"].(map[string]interface{})
	if issueObj == nil {
		issueObj = data
	}
	raw, _ := issueObj["available_transitions"].([]interface{})
	var found bool
	for _, v := range raw {
		if asString(v) == "approve" {
			found = true
		}
	}
	if !found {
		t.Errorf("approve should be offered: the implementer can approve by naming a reviewer, and POST /approve with reviewed_by succeeds (transitions: %v)", raw)
	}
}

// TestIntegration_Approve_AuditParityWithCLI pins the audit behavior decided
// for this epic, on the API side. The rule is about WHO RECORDED the row, not
// which flag was used: an approval written by an implementation-involved
// session goes to the out-of-band audit file either way, and an independent
// session's approval must not, or the file fills with routine entries and stops
// surfacing the case it exists for.
func TestIntegration_Approve_AuditParityWithCLI(t *testing.T) {
	readAudit := func(t *testing.T, baseDir string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(baseDir, ".todos", "security_events.jsonl"))
		if err != nil {
			if os.IsNotExist(err) {
				return ""
			}
			t.Fatalf("read security_events.jsonl: %v", err)
		}
		return string(b)
	}

	t.Run("involved recorder with attribution is audited", func(t *testing.T) {
		baseURL, database, cleanup := setupIntegrationServer(t)
		defer cleanup()
		setTrustedMode(t, database.BaseDir())

		issueID := seedInReviewIssue(t, database, webSessionID(t, database))
		resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
			"reviewed_by": "code-reviewer sub-agent",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200", resp.StatusCode)
		}
		resp.Body.Close()

		audit := readAudit(t, database.BaseDir())
		if !strings.Contains(audit, "attributed_review") || !strings.Contains(audit, "code-reviewer sub-agent") {
			t.Errorf("expected an attributed_review audit entry naming the reviewer, got: %s", audit)
		}
	})

	t.Run("independent recorder is not audited", func(t *testing.T) {
		baseURL, database, cleanup := setupIntegrationServer(t)
		defer cleanup()
		setTrustedMode(t, database.BaseDir())

		// Implemented by a different session, so the web session is independent.
		issueID := seedInReviewIssue(t, database, "ses-other-impl")
		resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
			"reviewed_by": "a human on the team",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200", resp.StatusCode)
		}
		resp.Body.Close()

		if audit := readAudit(t, database.BaseDir()); audit != "" {
			t.Errorf("an independent session's approval must not be audited: %s", audit)
		}
	})

	t.Run("self review is audited", func(t *testing.T) {
		baseURL, database, cleanup := setupIntegrationServer(t)
		defer cleanup()
		setTrustedMode(t, database.BaseDir())

		issueID := seedInReviewIssue(t, database, webSessionID(t, database))
		resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
			"self_review": true,
			"reason":      "reviewed my own diff",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200", resp.StatusCode)
		}
		resp.Body.Close()

		if audit := readAudit(t, database.BaseDir()); !strings.Contains(audit, "self_review") {
			t.Errorf("expected a self_review audit entry, got: %s", audit)
		}
	})
}

// TestIntegration_Reviews_RecordOnlyAuditAndLog covers the two API behaviors
// added late in td-ec8445 that shipped without tests the first time — an audit
// control with no test is how the record-only hole got missed in the first
// place.
func TestIntegration_Reviews_RecordOnlyAuditAndLog(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	implSession := webSessionID(t, database)
	issueID := seedInReviewIssue(t, database, implSession)

	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/reviews", map[string]interface{}{
		"decision":    reviewpolicy.DecisionApproved,
		"summary":     "reviewed the diff",
		"reviewed_by": "code-reviewer sub-agent",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d want 201", resp.StatusCode)
	}
	resp.Body.Close()

	// The audit file must record that an involved session wrote the row —
	// otherwise record-only followed by a Mode C close leaves no trace.
	audit, err := os.ReadFile(filepath.Join(database.BaseDir(), ".todos", "security_events.jsonl"))
	if err != nil {
		t.Fatalf("expected a security_events entry for an involved recorder: %v", err)
	}
	if !strings.Contains(string(audit), "record_only") || !strings.Contains(string(audit), "code-reviewer sub-agent") {
		t.Errorf("audit entry should mark record_only and name the reviewer, got: %s", audit)
	}

	// The issue log must name the credited reviewer, matching the CLI.
	logs, err := database.GetLogs(issueID, 0)
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	var named bool
	for _, l := range logs {
		if strings.Contains(l.Message, "by code-reviewer sub-agent") {
			named = true
		}
	}
	if !named {
		t.Errorf("record-only log should name the credited reviewer, got %+v", logs)
	}
}

// TestIntegration_Approve_LogNamesReviewer pins the approve transition's log
// line. The API logged a bare reason where the CLI named the reviewer, which
// made a published CHANGELOG claim false.
func TestIntegration_Approve_LogNamesReviewer(t *testing.T) {
	baseURL, database, cleanup := setupIntegrationServer(t)
	defer cleanup()
	setTrustedMode(t, database.BaseDir())

	issueID := seedInReviewIssue(t, database, webSessionID(t, database))
	resp := iDoJSON(t, "POST", baseURL+"/v1/issues/"+issueID+"/approve", map[string]interface{}{
		"reviewed_by": "code-reviewer sub-agent",
		"reason":      "diff plus tests",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	resp.Body.Close()

	logs, _ := database.GetLogs(issueID, 0)
	var named bool
	for _, l := range logs {
		if strings.Contains(l.Message, "Approved (reviewed by code-reviewer sub-agent)") {
			named = true
		}
		if l.Type == models.LogTypeSecurity {
			t.Errorf("attributed approval must not write a security-typed log: %q", l.Message)
		}
	}
	if !named {
		t.Errorf("approve log should name the credited reviewer, got %+v", logs)
	}
}
