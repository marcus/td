package serve

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

// reviewRecordBody is the request payload for POST /v1/issues/{id}/reviews.
type reviewRecordBody struct {
	Decision string `json:"decision"`
	Summary  string `json:"summary"`
}

// IssueReviewSummary is the nested DTO used for the issue's active approval
// review when an active (non-superseded) row exists.
type IssueReviewSummary struct {
	ID                 string    `json:"id"`
	Decision           string    `json:"decision"`
	ReviewerSession    string    `json:"reviewer_session"`
	RequestedBySession string    `json:"requested_by_session,omitempty"`
	Summary            string    `json:"summary,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

// IssueReviewDTO is the full per-row representation returned when the caller
// explicitly asks for review history (GET /v1/issues/{id}?with=reviews).
type IssueReviewDTO struct {
	ID                 string     `json:"id"`
	IssueID            string     `json:"issue_id"`
	ReviewerSession    string     `json:"reviewer_session"`
	Decision           string     `json:"decision"`
	Summary            string     `json:"summary,omitempty"`
	RequestedBySession string     `json:"requested_by_session,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	SupersededAt       *time.Time `json:"superseded_at,omitempty"`
}

// IssueReviewToDTO converts a models.IssueReview to a DTO for API consumers.
func IssueReviewToDTO(r *models.IssueReview) IssueReviewDTO {
	return IssueReviewDTO{
		ID:                 r.ID,
		IssueID:            r.IssueID,
		ReviewerSession:    r.ReviewerSession,
		Decision:           r.Decision,
		Summary:            r.Summary,
		RequestedBySession: r.RequestedBySession,
		CreatedAt:          r.CreatedAt,
		SupersededAt:       r.SupersededAt,
	}
}

// HandleRecordReview implements POST /v1/issues/{id}/reviews. Under the
// delegated policy mode, it records an approval (or changes_requested) review
// without closing the issue. Rejects with 409 when the mode isn't delegated
// so clients get a clear signal rather than a silent success that doesn't
// unblock the close-after-review flow.
//
// Status codes:
//   - 201 Created on success
//   - 400 on missing summary / invalid decision / unknown issue
//   - 403 on ineligible reviewer
//   - 404 when issue doesn't exist
//   - 409 when mode != delegated or an active approval already exists
func HandleRecordReview(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	issueID := r.PathValue("id")
	if issueID == "" {
		WriteError(w, ErrValidation, "issue id is required", http.StatusBadRequest)
		return
	}

	var body reviewRecordBody
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	body.Decision = strings.TrimSpace(body.Decision)
	body.Summary = strings.TrimSpace(body.Summary)
	if body.Decision == "" {
		body.Decision = reviewpolicy.DecisionApproved
	}
	if body.Decision != reviewpolicy.DecisionApproved && body.Decision != reviewpolicy.DecisionChangesRequested {
		WriteError(w, ErrValidation, fmt.Sprintf("invalid decision %q (want approved|changes_requested)", body.Decision), http.StatusBadRequest)
		return
	}
	if body.Summary == "" {
		WriteError(w, ErrValidation, "summary is required for record-only review", http.StatusBadRequest)
		return
	}

	mode := reviewpolicy.ModeStrict
	if ctx.BaseDir != "" {
		if m, err := features.ResolveReviewPolicyMode(ctx.BaseDir); err == nil {
			mode = m
		}
	}
	if mode != reviewpolicy.ModeDelegated {
		WriteError(w, ErrConflict, "record-only review requires review_policy_mode=delegated", http.StatusConflict)
		return
	}

	issue, err := ctx.DB.GetIssue(issueID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("issue not found: %s", issueID), http.StatusNotFound)
		} else {
			slog.Error("get issue for review", "err", err, "id", issueID)
			WriteError(w, ErrInternal, "failed to fetch issue", http.StatusInternalServerError)
		}
		return
	}
	// Minor issues bypass review entirely — they're meant to self-review and
	// close in one step. A record-only review row on a minor issue is
	// semantically meaningless and would mislead consumers of issue_reviews.
	if issue.Minor {
		WriteError(w, ErrValidation, fmt.Sprintf("minor issues do not require reviews: %s", issue.ID), http.StatusBadRequest)
		return
	}
	if issue.Status != models.StatusInReview {
		WriteError(w, ErrConflict, fmt.Sprintf("cannot record review: %s is not in_review", issue.ID), http.StatusConflict)
		return
	}

	// Reviewer eligibility — share the same decision path as approve.
	decision := serveReviewerDecision(ctx, issue)
	if !decision.Allowed {
		WriteError(w, ErrForbidden, decision.RejectionMessage, http.StatusForbidden)
		return
	}

	// For approved decisions, reject if an active approval already exists —
	// the caller should use /approve (close-after-review) instead. This
	// mirrors cmd/review.go behavior.
	if body.Decision == reviewpolicy.DecisionApproved {
		if active, _ := ctx.DB.GetActiveApprovalReview(issue.ID); active != nil {
			WriteError(w, ErrConflict,
				fmt.Sprintf("%s already has an active approval (review %s by %s); use /approve to close",
					issue.ID, active.ID, active.ReviewerSession),
				http.StatusConflict)
			return
		}
	}

	// Supersede stale rows + snapshot prior-active id for undo parity with CLI.
	priorActive := ""
	if pa, _ := ctx.DB.GetActiveApprovalReview(issue.ID); pa != nil {
		priorActive = pa.ID
	}
	_ = ctx.DB.SupersedeActiveReviews(issue.ID)

	reviewID, err := ctx.DB.CreateIssueReview(issue.ID, ctx.SessionID, body.Decision, body.Summary, issue.ReviewRequestedBySession, false)
	if err != nil {
		slog.Error("create issue review", "err", err, "id", issue.ID)
		WriteError(w, ErrInternal, "failed to record review", http.StatusInternalServerError)
		return
	}

	actionType := models.ActionReviewApprove
	if body.Decision == reviewpolicy.DecisionChangesRequested {
		actionType = models.ActionReviewChangesRequested
	} else {
		now := time.Now()
		issue.ReviewerSession = ctx.SessionID
		issue.ReviewedAt = &now
	}

	if err := ctx.DB.UpdateIssueLoggedWithReviewMeta(issue, models.StatusInReview, ctx.SessionID, actionType, reviewID, priorActive); err != nil {
		slog.Error("update issue for review", "err", err, "id", issue.ID)
		WriteError(w, ErrInternal, "failed to stamp reviewer metadata", http.StatusInternalServerError)
		return
	}

	sessionAction := models.ActionSessionReviewApproved
	if body.Decision == reviewpolicy.DecisionChangesRequested {
		sessionAction = models.ActionSessionReviewChangesRequested
	}
	_ = ctx.DB.RecordSessionAction(issue.ID, ctx.SessionID, sessionAction)

	_ = ctx.DB.AddLog(&models.Log{
		IssueID:   issue.ID,
		SessionID: ctx.SessionID,
		Message:   "Review recorded (" + body.Decision + "): " + body.Summary,
		Type:      models.LogTypeProgress,
	})

	// Load the freshly-persisted review row for the response.
	updated, err := ctx.DB.GetIssue(issue.ID)
	if err != nil {
		updated = issue
	}
	var reviewRow *models.IssueReview
	if reviews, lerr := ctx.DB.ListIssueReviews(issue.ID); lerr == nil {
		for i := range reviews {
			if reviews[i].ID == reviewID {
				reviewRow = reviews[i]
				break
			}
		}
	}

	payload := map[string]interface{}{
		"issue": IssueToDTO(updated),
	}
	if reviewRow != nil {
		payload["review"] = IssueReviewToDTO(reviewRow)
	}
	if active := activeReviewSummary(ctx, issue.ID); active != nil {
		payload["active_review"] = active
	}
	WriteSuccess(w, payload, http.StatusCreated)
}

func (s *Server) handleRecordReview(w http.ResponseWriter, r *http.Request) {
	HandleRecordReview(s.handlerContext(), w, r)
}

// activeReviewSummary returns the issue's current active approval review as
// a compact summary, or nil when none exists. Exported internally so other
// handlers (approve/reject) can include it in their response payloads.
func activeReviewSummary(ctx HandlerContext, issueID string) *IssueReviewSummary {
	if ctx.DB == nil {
		return nil
	}
	rev, err := ctx.DB.GetActiveApprovalReview(issueID)
	if err != nil || rev == nil {
		return nil
	}
	return &IssueReviewSummary{
		ID:                 rev.ID,
		Decision:           rev.Decision,
		ReviewerSession:    rev.ReviewerSession,
		RequestedBySession: rev.RequestedBySession,
		Summary:            rev.Summary,
		CreatedAt:          rev.CreatedAt,
	}
}
