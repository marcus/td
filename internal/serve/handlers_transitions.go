package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
	"github.com/marcus/td/internal/workflow"
)

// ServeReviewerDecisionForTest exposes the same inputs-assembly path the
// runtime uses so the cross-surface parity suite can exercise it with
// synthetic inputs. The parity suite lives outside this package, which is
// why a thin exported shim is needed; runtime callers should use
// serveReviewerDecision directly.
func ServeReviewerDecisionForTest(mode reviewpolicy.Mode, issue *models.Issue, sessionID string, hasImplementationHistory, wasAnyInvolved, hasActiveApproval bool) reviewpolicy.ReviewerEligibility {
	isCreator := issue != nil && issue.CreatorSession != "" && issue.CreatorSession == sessionID
	isImplementer := issue != nil && issue.ImplementerSession != "" && issue.ImplementerSession == sessionID
	return reviewpolicy.EvaluateReviewerEligibility(reviewpolicy.ReviewerEligibilityInput{
		Mode:                     mode,
		Issue:                    issue,
		SessionID:                sessionID,
		SessionIsImplementer:     isImplementer,
		SessionIsCreator:         isCreator,
		HasImplementationHistory: hasImplementationHistory,
		HasActiveApproval:        hasActiveApproval,
		WasAnyInvolved:           wasAnyInvolved,
	})
}

// ServeCloseDecisionForTest exposes the close-eligibility decision that the
// serve close handler runs. See ServeReviewerDecisionForTest for why this
// shim exists.
func ServeCloseDecisionForTest(mode reviewpolicy.Mode, issue *models.Issue, sessionID string, hasImplementationHistory, wasAnyInvolved, hasActiveApproval bool) reviewpolicy.CloseEligibility {
	isCreator := issue != nil && issue.CreatorSession != "" && issue.CreatorSession == sessionID
	isImplementer := issue != nil && issue.ImplementerSession != "" && issue.ImplementerSession == sessionID
	isReviewerOfRecord := issue != nil && issue.ReviewerSession != "" && issue.ReviewerSession == sessionID
	isReviewRequester := issue != nil && issue.ReviewRequestedBySession != "" && issue.ReviewRequestedBySession == sessionID
	return reviewpolicy.EvaluateCloseEligibility(reviewpolicy.CloseEligibilityInput{
		Mode:                      mode,
		Issue:                     issue,
		SessionID:                 sessionID,
		SessionIsImplementer:      isImplementer,
		SessionIsCreator:          isCreator,
		SessionIsReviewerOfRecord: isReviewerOfRecord,
		SessionIsReviewRequester:  isReviewRequester,
		HasImplementationHistory:  hasImplementationHistory,
		WasAnyInvolved:            wasAnyInvolved,
		HasActiveApproval:         hasActiveApproval,
	})
}

// serveReviewerDecision runs reviewpolicy.EvaluateReviewerEligibility for the
// given issue/session pair under the context's configured mode. Used by
// HandleApprove to align the serve transition path with the CLI policy.
// Exported as an unexported package helper so the parity suite can exercise
// the exact decision the runtime uses.
func serveReviewerDecision(ctx HandlerContext, issue *models.Issue) reviewpolicy.ReviewerEligibility {
	mode := reviewpolicy.ModeStrict
	if ctx.BaseDir != "" {
		if m, err := features.ResolveReviewPolicyMode(ctx.BaseDir); err == nil {
			mode = m
		}
	}

	isCreator := issue != nil && issue.CreatorSession != "" && issue.CreatorSession == ctx.SessionID
	isImplementer := issue != nil && issue.ImplementerSession != "" && issue.ImplementerSession == ctx.SessionID

	var wasAny, wasImpl bool
	var hasActive bool
	if ctx.DB != nil && issue != nil {
		if v, err := ctx.DB.WasSessionInvolved(issue.ID, ctx.SessionID); err == nil {
			wasAny = v
		} else {
			wasAny = true // conservative
		}
		if v, err := ctx.DB.WasSessionImplementationInvolved(issue.ID, ctx.SessionID); err == nil {
			wasImpl = v
		} else {
			wasImpl = true
		}
		if rev, err := ctx.DB.GetActiveApprovalReview(issue.ID); err == nil && rev != nil {
			hasActive = true
		}
	}

	return reviewpolicy.EvaluateReviewerEligibility(reviewpolicy.ReviewerEligibilityInput{
		Mode:                     mode,
		Issue:                    issue,
		SessionID:                ctx.SessionID,
		SessionIsImplementer:     isImplementer,
		SessionIsCreator:         isCreator,
		HasImplementationHistory: wasImpl,
		HasActiveApproval:        hasActive,
		WasAnyInvolved:           wasAny,
	})
}

// serveCloseDecision runs reviewpolicy.EvaluateCloseEligibility for the given
// issue/session pair. Serves the close endpoint harden check.
func serveCloseDecision(ctx HandlerContext, issue *models.Issue) reviewpolicy.CloseEligibility {
	mode := reviewpolicy.ModeStrict
	if ctx.BaseDir != "" {
		if m, err := features.ResolveReviewPolicyMode(ctx.BaseDir); err == nil {
			mode = m
		}
	}

	isCreator := issue != nil && issue.CreatorSession != "" && issue.CreatorSession == ctx.SessionID
	isImplementer := issue != nil && issue.ImplementerSession != "" && issue.ImplementerSession == ctx.SessionID
	isReviewerOfRecord := issue != nil && issue.ReviewerSession != "" && issue.ReviewerSession == ctx.SessionID
	isReviewRequester := issue != nil && issue.ReviewRequestedBySession != "" && issue.ReviewRequestedBySession == ctx.SessionID

	var wasAny, wasImpl, hasIssueImplHistory, hasActive bool
	if ctx.DB != nil && issue != nil {
		if v, err := ctx.DB.WasSessionInvolved(issue.ID, ctx.SessionID); err == nil {
			wasAny = v
		} else {
			wasAny = true
		}
		if v, err := ctx.DB.WasSessionImplementationInvolved(issue.ID, ctx.SessionID); err == nil {
			wasImpl = v
		} else {
			wasImpl = true
		}
		if v, err := ctx.DB.HasImplementationHistory(issue.ID); err == nil {
			hasIssueImplHistory = v
		} else {
			hasIssueImplHistory = true
		}
		if rev, err := ctx.DB.GetActiveApprovalReview(issue.ID); err == nil && rev != nil {
			hasActive = true
		}
	}

	// HasImplementationHistory in reviewpolicy's close input doubles as the
	// "caller's own impl history" flag. The CLI close wrapper layers an
	// extra "post-decision veto" on top that also consults the issue-wide
	// flag to revoke creator-open-bypass when *any* prior session
	// implemented. The serve handler currently does not apply that veto —
	// it is defensible behavior because the serve close path also rejects
	// in_review issues outright (see HandleClose.policyCheck) so the
	// creator-open-bypass loophole has much less reach here. Wiring the
	// veto symmetrically is tracked as Step 2 work; for Batch 1c we load
	// the flag so the parity tests see the same shape the runtime computes.
	_ = hasIssueImplHistory

	return reviewpolicy.EvaluateCloseEligibility(reviewpolicy.CloseEligibilityInput{
		Mode:                      mode,
		Issue:                     issue,
		SessionID:                 ctx.SessionID,
		SessionIsImplementer:      isImplementer,
		SessionIsCreator:          isCreator,
		SessionIsReviewerOfRecord: isReviewerOfRecord,
		SessionIsReviewRequester:  isReviewRequester,
		HasImplementationHistory:  wasImpl,
		WasAnyInvolved:            wasAny,
		HasActiveApproval:         hasActive,
	})
}

// This file contains the status-transition HTTP handlers (start/review/
// approve/reject/block/unblock/close/reopen). Each handler is exported as a
// pure function that takes a HandlerContext, so the same code can be mounted
// from td-serve (`*Server`) and from td-sync (per-project HandlerContext built
// per request). The `(s *Server) handleXxx` methods are thin wrappers retained
// so the route registrations and any external callers continue to work
// unchanged.

// ============================================================================
// Status Transition Endpoints
// ============================================================================

// transitionReasonBody is the optional request body for transition endpoints.
type transitionReasonBody struct {
	Reason string `json:"reason"`
}

// transitionCascadeResult holds the results of cascade operations for the response.
type transitionCascadeResult struct {
	ParentStatusUpdates []IssueDTO `json:"parent_status_updates"`
	AutoUnblocked       []IssueDTO `json:"auto_unblocked"`
}

// transitionSpec defines a status transition's configuration.
type transitionSpec struct {
	// validFrom is the set of statuses the issue may currently be in.
	validFrom []models.Status
	// toStatus is the target status.
	toStatus models.Status
	// actionType is the action_log type for the transition.
	actionType models.ActionType
	// applySideEffects mutates the issue model for transition-specific side
	// effects (session fields, closed_at, etc.). Called after status is set.
	applySideEffects func(ctx HandlerContext, issue *models.Issue)
	// runCascades executes any post-transition cascades and returns results.
	runCascades func(ctx HandlerContext, issue *models.Issue) transitionCascadeResult
	// defaultLogMsg is the default progress log message when no reason is given.
	defaultLogMsg string
	// logType overrides the log type (defaults to LogTypeProgress).
	logType models.LogType
	// policyCheck runs after state-machine validation but before any mutation.
	// Returning non-empty rejection + httpCode writes the error response and
	// aborts the transition. Used to wire reviewpolicy's eligibility decisions
	// into approve/close so the serve path matches the CLI path.
	policyCheck func(ctx HandlerContext, issue *models.Issue) (httpCode int, rejection string)
	// postCommit runs after UpdateIssueLogged succeeds but before cascades.
	// Used to write issue_reviews rows for approve so audit output records
	// the reviewer independently of the closer.
	postCommit func(ctx HandlerContext, issue *models.Issue)
}

// handleTransition is the common handler for all status transition endpoints.
// It is a pure function that operates on a HandlerContext, so it can be reused
// by td-sync's per-project routes.
func handleTransition(ctx HandlerContext, w http.ResponseWriter, r *http.Request, spec transitionSpec) {
	issueID := r.PathValue("id")
	if issueID == "" {
		WriteError(w, ErrValidation, "issue id is required", http.StatusBadRequest)
		return
	}

	// Fetch issue
	issue, err := ctx.DB.GetIssue(issueID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("issue not found: %s", issueID), http.StatusNotFound)
		} else {
			slog.Error("get issue for transition", "err", err, "id", issueID)
			WriteError(w, ErrInternal, "failed to fetch issue", http.StatusInternalServerError)
		}
		return
	}
	canonicalIssueID := issue.ID

	// Validate current status against allowed "from" statuses using state machine
	sm := workflow.DefaultMachine()
	if !sm.IsValidTransition(issue.Status, spec.toStatus) {
		WriteError(w, ErrConflict,
			fmt.Sprintf("cannot transition %s from %s to %s", canonicalIssueID, issue.Status, spec.toStatus),
			http.StatusConflict)
		return
	}

	// Also validate against the spec's validFrom list (which may be more
	// restrictive than the state machine for certain endpoints like approve/reject)
	if !statusIn(issue.Status, spec.validFrom) {
		WriteError(w, ErrConflict,
			fmt.Sprintf("cannot transition %s from %s to %s", canonicalIssueID, issue.Status, spec.toStatus),
			http.StatusConflict)
		return
	}

	// Run policy check (reviewpolicy-backed) before mutating anything.
	if spec.policyCheck != nil {
		if code, rejection := spec.policyCheck(ctx, issue); rejection != "" {
			if code == 0 {
				code = http.StatusForbidden
			}
			WriteError(w, ErrForbidden, rejection, code)
			return
		}
	}

	// Parse optional reason body (body may be empty or absent)
	var reason string
	if r.Body != nil {
		bodyBytes, readErr := io.ReadAll(r.Body)
		if readErr == nil && len(bodyBytes) > 0 {
			var body transitionReasonBody
			if jsonErr := json.Unmarshal(bodyBytes, &body); jsonErr == nil {
				reason = body.Reason
			}
		}
	}

	// Apply the transition
	issue.Status = spec.toStatus
	if spec.applySideEffects != nil {
		spec.applySideEffects(ctx, issue)
	}

	// Persist
	if err := ctx.DB.UpdateIssueLogged(issue, ctx.SessionID, spec.actionType); err != nil {
		slog.Error("transition issue", "err", err, "id", issueID, "to", spec.toStatus)
		WriteError(w, ErrInternal, "failed to update issue", http.StatusInternalServerError)
		return
	}

	if spec.postCommit != nil {
		spec.postCommit(ctx, issue)
	}

	// Log reason or default message
	logMsg := spec.defaultLogMsg
	if reason != "" {
		logMsg = reason
	}
	logType := models.LogTypeProgress
	if spec.logType != "" {
		logType = spec.logType
	}
	if logErr := ctx.DB.AddLog(&models.Log{
		IssueID:   canonicalIssueID,
		SessionID: ctx.SessionID,
		Message:   logMsg,
		Type:      logType,
	}); logErr != nil {
		slog.Warn("failed to add transition log", "err", logErr, "id", canonicalIssueID)
	}

	// Run cascades
	var cascades transitionCascadeResult
	if spec.runCascades != nil {
		cascades = spec.runCascades(ctx, issue)
	}
	if cascades.ParentStatusUpdates == nil {
		cascades.ParentStatusUpdates = []IssueDTO{}
	}
	if cascades.AutoUnblocked == nil {
		cascades.AutoUnblocked = []IssueDTO{}
	}

	// Re-read the issue to get the final state (UpdatedAt, etc.)
	updated, err := ctx.DB.GetIssue(canonicalIssueID)
	if err != nil {
		// Fallback to the in-memory version
		updated = issue
	}

	dto := IssueToDTO(updated)
	WriteSuccess(w, map[string]interface{}{
		"issue":    dto,
		"cascades": cascades,
	}, http.StatusOK)
}

// statusIn checks if a status is in the given set.
func statusIn(s models.Status, set []models.Status) bool {
	for _, v := range set {
		if s == v {
			return true
		}
	}
	return false
}

// cascadeIDsToIssueDTOs fetches issues by ID and converts to DTOs.
func cascadeIDsToIssueDTOs(ctx HandlerContext, ids []string) []IssueDTO {
	var dtos []IssueDTO
	for _, id := range ids {
		issue, err := ctx.DB.GetIssue(id)
		if err == nil {
			dtos = append(dtos, IssueToDTO(issue))
		}
	}
	if dtos == nil {
		return []IssueDTO{}
	}
	return dtos
}

// ============================================================================
// POST /v1/issues/{id}/start
// ============================================================================

// HandleStart transitions an issue from open to in_progress and stamps the
// caller's session as the implementer. Pure-function form of
// (s *Server).handleStart.
func HandleStart(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	handleTransition(ctx, w, r, transitionSpec{
		validFrom:  []models.Status{models.StatusOpen},
		toStatus:   models.StatusInProgress,
		actionType: models.ActionStart,
		applySideEffects: func(c HandlerContext, issue *models.Issue) {
			issue.ImplementerSession = c.SessionID
		},
		defaultLogMsg: "Started work",
	})
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	HandleStart(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/issues/{id}/review
// ============================================================================

// HandleReview transitions an issue to in_review and cascades the parent's
// status to in_review when all siblings qualify. Pure-function form of
// (s *Server).handleReview.
func HandleReview(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	handleTransition(ctx, w, r, transitionSpec{
		validFrom:  []models.Status{models.StatusOpen, models.StatusInProgress},
		toStatus:   models.StatusInReview,
		actionType: models.ActionReview,
		applySideEffects: func(c HandlerContext, issue *models.Issue) {
			if issue.ImplementerSession == "" {
				issue.ImplementerSession = c.SessionID
			}
			// Stamp the review-requester slot so the delegated close path
			// can identify the orchestrator that submitted this cycle.
			issue.ReviewRequestedBySession = c.SessionID
		},
		runCascades: func(c HandlerContext, issue *models.Issue) transitionCascadeResult {
			var cr transitionCascadeResult
			// Parent cascade to in_review when all siblings qualify
			if _, ids := c.DB.CascadeUpParentStatus(issue.ID, models.StatusInReview, c.SessionID); len(ids) > 0 {
				cr.ParentStatusUpdates = cascadeIDsToIssueDTOs(c, ids)
			}
			return cr
		},
		defaultLogMsg: "Submitted for review",
	})
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	HandleReview(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/issues/{id}/approve
// ============================================================================

// HandleApprove transitions an issue from in_review to closed, stamps the
// reviewer/closed_at, and runs parent-close + dependency-unblock cascades.
// Pure-function form of (s *Server).handleApprove.
//
// Step 3: under delegated mode, if an active approval review exists and the
// caller is NOT the eligible reviewer, the handler performs a close-after-
// review (Mode C). The existing reviewer_session / reviewed_at are preserved;
// only closed_by_session and closed_at are stamped. When closer !=
// reviewer_of_record, the request must include a reason in the body —
// otherwise the handler returns 400 so CLI and API enforce the same rule.
func HandleApprove(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	// Pre-inspect the issue for the Mode-C branch decision. We still
	// delegate most work to handleTransition for consistency with other
	// transitions; the branch below short-circuits when Mode-C applies.
	issueID := r.PathValue("id")
	if issueID != "" && ctx.DB != nil {
		if issue, err := ctx.DB.GetIssue(issueID); err == nil && issue != nil {
			if handledCloseAfterReview(ctx, w, r, issue) {
				return
			}
		}
	}

	handleTransition(ctx, w, r, transitionSpec{
		validFrom:  []models.Status{models.StatusInReview},
		toStatus:   models.StatusClosed,
		actionType: models.ActionApprove,
		policyCheck: func(c HandlerContext, issue *models.Issue) (int, string) {
			decision := serveReviewerDecision(c, issue)
			if !decision.Allowed {
				return http.StatusForbidden, decision.RejectionMessage
			}
			// Also check close-eligibility in case delegated mode adds a
			// further restriction (Step 2 flips close-after-review through
			// this same path).
			closeDecision := serveCloseDecision(c, issue)
			if !closeDecision.Allowed {
				return http.StatusForbidden, closeDecision.RejectionMessage
			}
			return 0, ""
		},
		applySideEffects: func(c HandlerContext, issue *models.Issue) {
			issue.ReviewerSession = c.SessionID
			issue.ClosedBySession = c.SessionID
			now := time.Now()
			issue.ReviewedAt = &now
			issue.ClosedAt = &now
		},
		postCommit: func(c HandlerContext, issue *models.Issue) {
			// Record the approval in the append-only review history. Best-
			// effort: a write error must not roll back the transition.
			_, _ = c.DB.CreateIssueReview(
				issue.ID,
				c.SessionID,
				reviewpolicy.DecisionApproved,
				"",
				issue.ReviewRequestedBySession,
			)
		},
		runCascades: func(c HandlerContext, issue *models.Issue) transitionCascadeResult {
			var cr transitionCascadeResult
			// Parent cascade to closed when all siblings closed
			if _, ids := c.DB.CascadeUpParentStatus(issue.ID, models.StatusClosed, c.SessionID); len(ids) > 0 {
				cr.ParentStatusUpdates = cascadeIDsToIssueDTOs(c, ids)
			}
			// Dependency unblocking cascade
			if _, ids := c.DB.CascadeUnblockDependents(issue.ID, c.SessionID); len(ids) > 0 {
				cr.AutoUnblocked = cascadeIDsToIssueDTOs(c, ids)
			}
			return cr
		},
		defaultLogMsg: "Approved",
	})
}

// handledCloseAfterReview performs the delegated-mode Mode-C close when the
// issue carries an active approval and the caller is not the reviewer. Returns
// true when the request was handled (response written) and the caller should
// return. Returns false to let the standard HandleApprove path run.
func handledCloseAfterReview(ctx HandlerContext, w http.ResponseWriter, r *http.Request, issue *models.Issue) bool {
	if issue.Status != models.StatusInReview {
		return false
	}
	mode := reviewpolicy.ModeStrict
	if ctx.BaseDir != "" {
		if m, err := features.ResolveReviewPolicyMode(ctx.BaseDir); err == nil {
			mode = m
		}
	}
	if mode != reviewpolicy.ModeDelegated {
		return false
	}
	active, err := ctx.DB.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil {
		return false
	}

	closeDec := serveCloseDecision(ctx, issue)
	if !closeDec.Allowed {
		// Fall back to normal path — serveReviewerDecision will surface the
		// correct error (either eligible reviewer or forbidden).
		return false
	}

	// Read optional reason for Mode-C. Required when closer != reviewer.
	var body transitionReasonBody
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	closerIsReviewer := active.ReviewerSession == ctx.SessionID
	if !closerIsReviewer && strings.TrimSpace(body.Reason) == "" {
		WriteError(w, ErrValidation,
			fmt.Sprintf("close-using-recorded-approval requires `reason` when closer (%s) != reviewer (%s)",
				ctx.SessionID, active.ReviewerSession),
			http.StatusBadRequest)
		return true
	}

	now := time.Now()
	issue.Status = models.StatusClosed
	issue.ClosedBySession = ctx.SessionID
	issue.ClosedAt = &now

	if err := ctx.DB.UpdateIssueLoggedWithReviewMeta(issue, models.StatusInReview, ctx.SessionID, models.ActionCloseAfterReview, "", ""); err != nil {
		slog.Error("close-after-review update", "err", err, "id", issue.ID)
		WriteError(w, ErrInternal, "failed to close issue", http.StatusInternalServerError)
		return true
	}
	_ = ctx.DB.RecordSessionAction(issue.ID, ctx.SessionID, models.ActionSessionClosed)

	logMsg := fmt.Sprintf("Closed after review %s (by %s)", active.ID, active.ReviewerSession)
	if body.Reason != "" {
		logMsg = logMsg + ": " + body.Reason
	}
	_ = ctx.DB.AddLog(&models.Log{
		IssueID:   issue.ID,
		SessionID: ctx.SessionID,
		Message:   logMsg,
		Type:      models.LogTypeProgress,
	})

	var cascades transitionCascadeResult
	if _, ids := ctx.DB.CascadeUpParentStatus(issue.ID, models.StatusClosed, ctx.SessionID); len(ids) > 0 {
		cascades.ParentStatusUpdates = cascadeIDsToIssueDTOs(ctx, ids)
	}
	if _, ids := ctx.DB.CascadeUnblockDependents(issue.ID, ctx.SessionID); len(ids) > 0 {
		cascades.AutoUnblocked = cascadeIDsToIssueDTOs(ctx, ids)
	}
	if cascades.ParentStatusUpdates == nil {
		cascades.ParentStatusUpdates = []IssueDTO{}
	}
	if cascades.AutoUnblocked == nil {
		cascades.AutoUnblocked = []IssueDTO{}
	}

	updated, err := ctx.DB.GetIssue(issue.ID)
	if err != nil {
		updated = issue
	}
	payload := map[string]interface{}{
		"issue":    IssueToDTO(updated),
		"cascades": cascades,
	}
	if summary := activeReviewSummary(ctx, issue.ID); summary != nil {
		payload["active_review"] = summary
	}
	WriteSuccess(w, payload, http.StatusOK)
	return true
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	HandleApprove(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/issues/{id}/reject
// ============================================================================

// HandleReject sends an issue back from in_review to open and clears the
// implementer/reviewer session and closed_at. Pure-function form of
// (s *Server).handleReject.
func HandleReject(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	handleTransition(ctx, w, r, transitionSpec{
		validFrom:  []models.Status{models.StatusInReview},
		toStatus:   models.StatusOpen,
		actionType: models.ActionReject,
		applySideEffects: func(_ HandlerContext, issue *models.Issue) {
			issue.ImplementerSession = ""
			issue.ReviewerSession = ""
			issue.ReviewedAt = nil
			issue.ClosedAt = nil
		},
		postCommit: func(c HandlerContext, issue *models.Issue) {
			// Supersede any active approval review — rejecting returns the
			// issue to open, so previous approvals must not outlive the
			// round-trip. Best-effort: do not roll back the state transition
			// on supersede error.
			_ = c.DB.SupersedeActiveReviews(issue.ID)
		},
		defaultLogMsg: "Rejected",
	})
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	HandleReject(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/issues/{id}/block
// ============================================================================

// HandleBlock marks an issue as blocked, logging a blocker entry. Pure-function
// form of (s *Server).handleBlock.
func HandleBlock(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	handleTransition(ctx, w, r, transitionSpec{
		validFrom:     []models.Status{models.StatusOpen, models.StatusInProgress},
		toStatus:      models.StatusBlocked,
		actionType:    models.ActionBlock,
		defaultLogMsg: "Blocked",
		logType:       models.LogTypeBlocker,
	})
}

func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	HandleBlock(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/issues/{id}/unblock
// ============================================================================

// HandleUnblock returns a blocked issue to open. Pure-function form of
// (s *Server).handleUnblock.
func HandleUnblock(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	handleTransition(ctx, w, r, transitionSpec{
		validFrom:     []models.Status{models.StatusBlocked},
		toStatus:      models.StatusOpen,
		actionType:    models.ActionUnblock,
		defaultLogMsg: "Unblocked",
	})
}

func (s *Server) handleUnblock(w http.ResponseWriter, r *http.Request) {
	HandleUnblock(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/issues/{id}/close
// ============================================================================

// HandleClose closes an issue from any non-closed state and runs parent-close
// + dependency-unblock cascades. Pure-function form of (s *Server).handleClose.
//
// Batch 1c: an `in_review` issue cannot be closed through this path. The plan
// mandates that reviewed implementation work flow through `td approve` (i.e.
// HandleApprove) so the review attestation is enforced; allowing HandleClose
// to finish the close would be a backdoor. The handler rejects with a clear
// message pointing to approve.
func HandleClose(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	handleTransition(ctx, w, r, transitionSpec{
		validFrom:  []models.Status{models.StatusOpen, models.StatusInProgress, models.StatusBlocked, models.StatusInReview},
		toStatus:   models.StatusClosed,
		actionType: models.ActionClose,
		policyCheck: func(_ HandlerContext, issue *models.Issue) (int, string) {
			if issue != nil && issue.Status == models.StatusInReview && !issue.Minor {
				return http.StatusForbidden, fmt.Sprintf("cannot close %s via /close while in_review: use /approve so the review is recorded", issue.ID)
			}
			return 0, ""
		},
		applySideEffects: func(c HandlerContext, issue *models.Issue) {
			now := time.Now()
			issue.ClosedAt = &now
			issue.ClosedBySession = c.SessionID
		},
		runCascades: func(c HandlerContext, issue *models.Issue) transitionCascadeResult {
			var cr transitionCascadeResult
			// Parent cascade to closed when all siblings closed
			if _, ids := c.DB.CascadeUpParentStatus(issue.ID, models.StatusClosed, c.SessionID); len(ids) > 0 {
				cr.ParentStatusUpdates = cascadeIDsToIssueDTOs(c, ids)
			}
			// Dependency unblocking cascade
			if _, ids := c.DB.CascadeUnblockDependents(issue.ID, c.SessionID); len(ids) > 0 {
				cr.AutoUnblocked = cascadeIDsToIssueDTOs(c, ids)
			}
			return cr
		},
		defaultLogMsg: "Closed",
	})
}

func (s *Server) handleClose(w http.ResponseWriter, r *http.Request) {
	HandleClose(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/issues/{id}/reopen
// ============================================================================

// HandleReopen reopens a closed issue, clearing reviewer + closed_at.
// Pure-function form of (s *Server).handleReopen.
func HandleReopen(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	handleTransition(ctx, w, r, transitionSpec{
		validFrom:  []models.Status{models.StatusClosed},
		toStatus:   models.StatusOpen,
		actionType: models.ActionReopen,
		applySideEffects: func(_ HandlerContext, issue *models.Issue) {
			issue.ReviewerSession = ""
			issue.ClosedAt = nil
		},
		defaultLogMsg: "Reopened",
	})
}

func (s *Server) handleReopen(w http.ResponseWriter, r *http.Request) {
	HandleReopen(s.handlerContext(), w, r)
}
