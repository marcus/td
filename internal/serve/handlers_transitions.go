package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/marcus/td/internal/db"
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
func ServeReviewerDecisionForTest(mode reviewpolicy.Mode, issue *models.Issue, sessionID string, hasImplementationHistory, wasAnyInvolved, hasActiveApproval, selfReviewAcknowledged bool) reviewpolicy.ReviewerEligibility {
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
		SelfReviewAcknowledged:   selfReviewAcknowledged,
	})
}

// ServeReviewerDecisionAttributedForTest and ServeCloseDecisionAttributedForTest
// are the attribution-carrying variants used by the cross-surface parity suite.
func ServeReviewerDecisionAttributedForTest(mode reviewpolicy.Mode, issue *models.Issue, sessionID string, hasImplementationHistory, wasAnyInvolved, hasActiveApproval, selfReviewAcknowledged bool, attributedTo string) reviewpolicy.ReviewerEligibility {
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
		SelfReviewAcknowledged:   selfReviewAcknowledged,
		AttributedTo:             attributedTo,
	})
}

func ServeCloseDecisionAttributedForTest(mode reviewpolicy.Mode, issue *models.Issue, sessionID string, hasImplementationHistory, wasAnyInvolved, hasActiveApproval, selfReviewAcknowledged bool, attributedTo string) reviewpolicy.CloseEligibility {
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
		SelfReviewAcknowledged:    selfReviewAcknowledged,
		AttributedTo:              attributedTo,
	})
}

// ServeCloseDecisionForTest exposes the close-eligibility decision that the
// serve close handler runs. See ServeReviewerDecisionForTest for why this
// shim exists.
func ServeCloseDecisionForTest(mode reviewpolicy.Mode, issue *models.Issue, sessionID string, hasImplementationHistory, wasAnyInvolved, hasActiveApproval, selfReviewAcknowledged bool) reviewpolicy.CloseEligibility {
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
		SelfReviewAcknowledged:    selfReviewAcknowledged,
	})
}

// serveReviewerDecision runs reviewpolicy.EvaluateReviewerEligibility for the
// given issue/session pair under the context's configured mode. Used by
// HandleApprove to align the serve transition path with the CLI policy.
// Exported as an unexported package helper so the parity suite can exercise
// the exact decision the runtime uses.
func serveReviewerDecision(ctx HandlerContext, issue *models.Issue, selfReviewAcknowledged bool) reviewpolicy.ReviewerEligibility {
	return serveReviewerDecisionAttributed(ctx, issue, selfReviewAcknowledged, "")
}

// serveReviewerDecisionAttributed is serveReviewerDecision plus the
// --reviewed-by attestation. Kept as a separate entry point so the many
// existing callers that never attribute stay unchanged.
func serveReviewerDecisionAttributed(ctx HandlerContext, issue *models.Issue, selfReviewAcknowledged bool, attributedTo string) reviewpolicy.ReviewerEligibility {
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
		SelfReviewAcknowledged:   selfReviewAcknowledged,
		AttributedTo:             attributedTo,
	})
}

// serveCloseDecision runs reviewpolicy.EvaluateCloseEligibility for the given
// issue/session pair. Serves the close endpoint harden check.
func serveCloseDecision(ctx HandlerContext, issue *models.Issue, selfReviewAcknowledged bool) reviewpolicy.CloseEligibility {
	return serveCloseDecisionAttributed(ctx, issue, selfReviewAcknowledged, "")
}

// serveCloseDecisionAttributed is serveCloseDecision plus the --reviewed-by
// attestation. The approve path MUST use this: on the direct review+close fast
// path, close eligibility re-runs the trusted reviewer predicate, so dropping
// the attribution here rejects an approval the reviewer check just allowed.
func serveCloseDecisionAttributed(ctx HandlerContext, issue *models.Issue, selfReviewAcknowledged bool, attributedTo string) reviewpolicy.CloseEligibility {
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
		SelfReviewAcknowledged:    selfReviewAcknowledged,
		AttributedTo:              attributedTo,
	})
}

// canApprove mirrors HandleApprove's branch logic to decide whether the given
// session may approve the issue right now (without a self_review ack). It is
// kept next to HandleApprove so the two stay in sync: any path that lets
// HandleApprove succeed must return true here, and vice versa. Used by
// availableTransitionsFor so clients can hide an Approve button that would 403.
func canApprove(ctx HandlerContext, issue *models.Issue) bool {
	if issue == nil || issue.Status != models.StatusInReview {
		return false
	}

	// Mode-C (delegated): an active approval lets an eligible closer finish the
	// close even when they are not the reviewer of record. Mirrors
	// handledCloseAfterReview.
	mode := reviewpolicy.ModeStrict
	if ctx.BaseDir != "" {
		if m, err := features.ResolveReviewPolicyMode(ctx.BaseDir); err == nil {
			mode = m
		}
	}
	if mode == reviewpolicy.ModeDelegated || mode == reviewpolicy.ModeTrusted {
		if active, err := ctx.DB.GetActiveApprovalReview(issue.ID); err == nil && active != nil {
			if serveCloseDecision(ctx, issue, false).Allowed {
				return true
			}
		}
	}

	// Primary path: reviewer eligibility AND close eligibility, matching the
	// approve handler's policyCheck.
	if serveReviewerDecision(ctx, issue, false).Allowed &&
		serveCloseDecision(ctx, issue, false).Allowed {
		return true
	}

	// Trusted mode also permits an implementation-involved session to approve
	// once it says who reviewed the work. Probe with a placeholder attribution:
	// this reports "approve is reachable", not "approve needs no input", and a
	// client that hides the button here would hide it for a call that succeeds.
	// The placeholder is never persisted — it only exercises the predicate.
	if mode == reviewpolicy.ModeTrusted {
		const probe = "probe"
		if serveReviewerDecisionAttributed(ctx, issue, false, probe).Allowed &&
			serveCloseDecisionAttributed(ctx, issue, false, probe).Allowed {
			return true
		}
	}
	return false
}

// availableTransitionsFor returns the transition action names the requesting
// session can perform on the issue right now. It mirrors the validity each
// transition endpoint enforces — the per-action validFrom set, the in_review/
// non-minor close rule (HandleClose.policyCheck), and the approve review-policy
// decision (canApprove) — so a client rendering exactly these actions never
// shows a button that the corresponding endpoint would reject. Action names
// match the endpoints: start/review/approve/reject/block/unblock/close/reopen.
func availableTransitionsFor(ctx HandlerContext, issue *models.Issue) []string {
	actions := []string{}
	if issue == nil {
		return actions
	}
	sm := workflow.DefaultMachine()
	status := issue.Status

	// consider appends name when the issue's status is a valid source for the
	// transition (state-machine path exists, status in validFrom) and the
	// action-specific guard (allowed) passes.
	consider := func(name string, validFrom []models.Status, to models.Status, allowed bool) {
		if !statusIn(status, validFrom) || !sm.IsValidTransition(status, to) || !allowed {
			return
		}
		actions = append(actions, name)
	}

	consider("start", []models.Status{models.StatusOpen}, models.StatusInProgress, true)
	consider("review", []models.Status{models.StatusOpen, models.StatusInProgress}, models.StatusInReview, true)
	consider("approve", []models.Status{models.StatusInReview}, models.StatusClosed, canApprove(ctx, issue))
	consider("reject", []models.Status{models.StatusInReview}, models.StatusOpen, true)
	consider("block", []models.Status{models.StatusOpen, models.StatusInProgress}, models.StatusBlocked, true)
	consider("unblock", []models.Status{models.StatusBlocked}, models.StatusOpen, true)
	// HandleClose rejects in_review non-minor issues (must use approve).
	closeAllowed := !(status == models.StatusInReview && !issue.Minor)
	consider("close", []models.Status{models.StatusOpen, models.StatusInProgress, models.StatusBlocked, models.StatusInReview}, models.StatusClosed, closeAllowed)
	consider("reopen", []models.Status{models.StatusClosed}, models.StatusOpen, true)

	return actions
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
	// ReviewedBy is the API equivalent of the CLI's --reviewed-by: who actually
	// performed the review, when that is not the session recording it. In
	// trusted mode it satisfies the involved-session acknowledgement without
	// claiming the caller did the review; in every other mode it is recorded as
	// metadata and grants nothing.
	ReviewedBy string `json:"reviewed_by"`

	// SelfReview is the API equivalent of the CLI's --self-review flag. It is
	// only meaningful for the approve transition under trusted mode, where an
	// implementer acknowledging the self-review converts the otherwise-blocked
	// approval into an audited self-review allow.
	SelfReview bool `json:"self_review"`
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

	// logMsgFn, when set, builds the log message from the reason and takes
	// precedence over defaultLogMsg. Set by transitions whose log line depends
	// on the policy decision rather than only on the request.
	logMsgFn func(reason string) string
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

	// Log reason or default message. logMsgFn, when set, owns the whole
	// message — used by approve so an attributed approval names the reviewer
	// in the issue log the way the CLI does, instead of logging a bare reason.
	logMsg := spec.defaultLogMsg
	if reason != "" {
		logMsg = reason
	}
	if spec.logMsgFn != nil {
		logMsg = spec.logMsgFn(reason)
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
	// Keep available_transitions fresh on the mutation response so clients that
	// replace their in-memory issue with this payload (instead of re-fetching)
	// still render the authoritative action set for the new status.
	dto.AvailableTransitions = availableTransitionsFor(ctx, updated)
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
	// The approve transition accepts a `self_review` boolean (the API
	// equivalent of the CLI's --self-review) plus the usual `reason`. Read the
	// body once here and restore it so handleTransition can re-parse the
	// reason. self_review only has an effect in trusted mode.
	var approveBody transitionReasonBody
	if r.Body != nil {
		if bodyBytes, readErr := io.ReadAll(r.Body); readErr == nil {
			if len(bodyBytes) > 0 {
				_ = json.Unmarshal(bodyBytes, &approveBody)
			}
			// Restore the body for downstream readers (handleTransition,
			// handledCloseAfterReview).
			r.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
		}
	}
	selfReviewAck := approveBody.SelfReview
	reviewedBy := strings.TrimSpace(approveBody.ReviewedBy)

	// Mirror the CLI's edge validation so the two surfaces reject the same
	// inputs. Without this the API would accept combinations the CLI refuses,
	// and the policy layer's evaluation order would silently favor attribution.
	if reviewedBy != "" && selfReviewAck {
		WriteError(w, ErrValidation, "reviewed_by and self_review are mutually exclusive: use reviewed_by when someone else reviewed the work, self_review when you reviewed your own", http.StatusBadRequest)
		return
	}
	if approveBody.ReviewedBy != "" && reviewedBy == "" {
		WriteError(w, ErrValidation, "reviewed_by requires a name (who performed the review?)", http.StatusBadRequest)
		return
	}
	if strings.ContainsFunc(reviewedBy, func(r rune) bool {
		return r == '\n' || r == '\r' || (unicode.IsControl(r) && r != '\t')
	}) {
		WriteError(w, ErrValidation, "reviewed_by must not contain newlines or control characters", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(reviewedBy) > reviewpolicy.MaxReviewedByLen {
		WriteError(w, ErrValidation, fmt.Sprintf("reviewed_by is limited to %d characters (got %d)", reviewpolicy.MaxReviewedByLen, utf8.RuneCountInString(reviewedBy)), http.StatusBadRequest)
		return
	}

	// Pre-inspect the issue for the Mode-C branch decision. We still
	// delegate most work to handleTransition for consistency with other
	// transitions; the branch below short-circuits when Mode-C applies.
	issueID := r.PathValue("id")
	if issueID != "" && ctx.DB != nil {
		if issue, err := ctx.DB.GetIssue(issueID); err == nil && issue != nil {
			if handledCloseAfterReview(ctx, w, r, issue, reviewedBy) {
				return
			}
		}
	}

	// decisionSelfReview captures whether the policy decision classified this
	// approval as an audited self-review, so postCommit can stamp the
	// issue_reviews row accordingly.
	var decisionSelfReview bool
	// decisionAttributedTo captures the attribution the policy accepted, so the
	// row is stamped from the decision rather than the raw request body.
	var decisionAttributedTo string

	handleTransition(ctx, w, r, transitionSpec{
		validFrom:  []models.Status{models.StatusInReview},
		toStatus:   models.StatusClosed,
		actionType: models.ActionApprove,
		policyCheck: func(c HandlerContext, issue *models.Issue) (int, string) {
			decision := serveReviewerDecisionAttributed(c, issue, selfReviewAck, reviewedBy)
			if !decision.Allowed {
				return http.StatusForbidden, decision.RejectionMessage
			}
			// Also check close-eligibility in case delegated mode adds a
			// further restriction (Step 2 flips close-after-review through
			// this same path).
			closeDecision := serveCloseDecisionAttributed(c, issue, selfReviewAck, reviewedBy)
			if !closeDecision.Allowed {
				return http.StatusForbidden, closeDecision.RejectionMessage
			}
			// Mirror the CLI reason gate: a trusted-mode self-review approval
			// requires a reason. Reject before mutating so API and CLI enforce
			// the same rule.
			// Note RequiresReason is false on the attributed path by design
			// (the attribution is the substance), so this gate only bites for
			// an unattributed self-review and the balanced creator exception.
			if decision.RequiresReason && strings.TrimSpace(approveBody.Reason) == "" {
				if decision.SelfReview && decision.AttributedTo == "" {
					return http.StatusBadRequest, fmt.Sprintf("self_review approval requires `reason` for %s", issue.ID)
				}
				return http.StatusBadRequest, fmt.Sprintf("approval requires `reason` for %s", issue.ID)
			}
			decisionSelfReview = decision.SelfReview
			decisionAttributedTo = decision.AttributedTo
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
			// effort: a write error must not roll back the transition. The
			// self_review flag is stamped from the policy decision so a
			// trusted-mode self-review is auditable.
			_, _ = c.DB.CreateIssueReview(db.NewReview{
				IssueID:            issue.ID,
				ReviewerSession:    c.SessionID,
				Decision:           reviewpolicy.DecisionApproved,
				Summary:            approveBody.Reason,
				RequestedBySession: issue.ReviewRequestedBySession,
				SelfReview:         decisionSelfReview,
				ReviewedBy:         decisionAttributedTo,
			})

			// Audit parity with the CLI. An approval recorded by an
			// implementation-involved session goes to the out-of-band audit
			// file whichever acknowledgement was used — that is the fact worth
			// being able to grep for. An independent session's approval is
			// unremarkable and is deliberately absent, or the file fills with
			// routine entries and stops surfacing the case it exists for.
			if decisionSelfReview && c.BaseDir != "" {
				reason := strings.TrimSpace(approveBody.Reason)
				auditReason := "self_review: " + reason
				if decisionAttributedTo != "" {
					auditReason = "attributed_review by " + decisionAttributedTo + ": " + reason
				}
				_ = db.LogSecurityEvent(c.BaseDir, db.SecurityEvent{
					IssueID:   issue.ID,
					SessionID: c.SessionID,
					Reason:    auditReason,
				})
			}
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
		logMsgFn: func(reason string) string {
			// Mirror the CLI: an attributed approval names the reviewer, so
			// someone reading `td show` sees who vouched for the work rather
			// than a bare reason line.
			if decisionAttributedTo == "" {
				if reason != "" {
					return reason
				}
				return "Approved"
			}
			msg := "Approved (reviewed by " + decisionAttributedTo + ")"
			if reason != "" {
				msg += ": " + reason
			}
			return msg
		},
	})
}

// handledCloseAfterReview performs the delegated-mode Mode-C close when the
// issue carries an active approval and the caller is not the reviewer. Returns
// true when the request was handled (response written) and the caller should
// return. Returns false to let the standard HandleApprove path run.
func handledCloseAfterReview(ctx HandlerContext, w http.ResponseWriter, r *http.Request, issue *models.Issue, reviewedBy string) bool {
	if issue.Status != models.StatusInReview {
		return false
	}
	mode := reviewpolicy.ModeStrict
	if ctx.BaseDir != "" {
		if m, err := features.ResolveReviewPolicyMode(ctx.BaseDir); err == nil {
			mode = m
		}
	}
	// Delegated and trusted both close on a recorded approval. Trusted is
	// delegated plus the self-review escape hatch (reviewpolicy.evaluateCloseTrusted
	// Case 1 is identical to delegated's), so withholding this path left the
	// API able to RECORD an approval it could then never act on.
	if mode != reviewpolicy.ModeDelegated && mode != reviewpolicy.ModeTrusted {
		return false
	}
	active, err := ctx.DB.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil {
		return false
	}

	// Mode C closes on an approval that was already recorded, so there is no
	// new review row to attribute. Accepting reviewed_by here would let a
	// caller believe it credited a reviewer when nothing was written — the CLI
	// rejects this for the same reason (cmd/review.go), and a surface that
	// silently swallows what another rejects is a parity bug.
	if reviewedBy != "" {
		WriteError(w, ErrValidation, fmt.Sprintf(
			"reviewed_by is ignored for %s: it already has a recorded approval by %s (review %s), and this close records no new review",
			issue.ID, active.ReviewerSession, active.ID), http.StatusBadRequest)
		return true
	}

	closeDec := serveCloseDecision(ctx, issue, false)
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
