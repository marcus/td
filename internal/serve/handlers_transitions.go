package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/workflow"
)

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
func HandleApprove(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	handleTransition(ctx, w, r, transitionSpec{
		validFrom:  []models.Status{models.StatusInReview},
		toStatus:   models.StatusClosed,
		actionType: models.ActionApprove,
		applySideEffects: func(c HandlerContext, issue *models.Issue) {
			issue.ReviewerSession = c.SessionID
			now := time.Now()
			issue.ClosedAt = &now
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
			issue.ClosedAt = nil
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
func HandleClose(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	handleTransition(ctx, w, r, transitionSpec{
		validFrom:  []models.Status{models.StatusOpen, models.StatusInProgress, models.StatusBlocked, models.StatusInReview},
		toStatus:   models.StatusClosed,
		actionType: models.ActionClose,
		applySideEffects: func(_ HandlerContext, issue *models.Issue) {
			now := time.Now()
			issue.ClosedAt = &now
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
