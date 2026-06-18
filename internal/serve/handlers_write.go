package serve

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/dependency"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/query"
)

// This file contains the write-side HTTP handlers (POST/PATCH/PUT/DELETE for
// issues, boards, comments, dependencies, focus). Each handler is exported as
// a pure function that takes a HandlerContext, so the same code can be mounted
// from td-serve (`*Server`) and from td-sync (per-project HandlerContext built
// per request). The `(s *Server) handleXxx` methods are thin wrappers retained
// so the existing route registrations continue to work unchanged.
//
// Local-process couplings have been gated:
//   - git.GetState() is only consulted when ctx.BaseDir != "" (we read it from
//     the on-disk repo). td-sync passes BaseDir == "" and skips branch capture.
//   - config.SetFocus / config.ClearFocus require ctx.BaseDir; handlers return
//     a 503-equivalent when called without one.
//   - title length limits route through titleLengthLimitsFor(ctx), which
//     prefers ctx.Config values, then on-disk config, then package defaults.
//   - Post-mutation notification routes through ctx.Config.NotifyChange (nil
//     on td-sync; the route adapter promotes action_log to events.db there
//     instead).

// notifyChange invokes the optional NotifyChange callback. Safe to call when
// ctx.Config.NotifyChange is nil (td-sync path).
func notifyChange(ctx HandlerContext) {
	if ctx.Config.NotifyChange != nil {
		ctx.Config.NotifyChange()
	}
}

// ============================================================================
// POST /v1/issues — Create Issue
// ============================================================================

// HandleCreateIssue creates a new issue from a JSON request body. Pure-function
// form of (s *Server).handleCreateIssue.
func HandleCreateIssue(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	var body IssueCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, ErrValidation, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Resolve configurable title length limits (ctx.Config → on-disk → defaults)
	titleMin, titleMax := titleLengthLimitsFor(ctx)

	// Validate
	if errs := ValidateIssueCreate(&body, titleMin, titleMax); len(errs) > 0 {
		WriteValidation(w, errs)
		return
	}

	// Normalize type and priority, apply defaults
	issueType := models.TypeTask
	if body.Type != "" {
		issueType = models.NormalizeType(body.Type)
	}

	issuePriority := models.PriorityP2
	if body.Priority != "" {
		issuePriority = models.NormalizePriority(body.Priority)
	}

	// If parent_id provided, verify it exists
	if body.ParentID != "" {
		normalizedParent := db.NormalizeIssueID(body.ParentID)
		_, err := ctx.DB.GetIssue(normalizedParent)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				WriteError(w, ErrNotFound, fmt.Sprintf("parent issue not found: %s", body.ParentID), http.StatusNotFound)
			} else {
				slog.Error("lookup parent issue", "err", err, "parent_id", body.ParentID)
				WriteError(w, ErrInternal, "failed to verify parent issue", http.StatusInternalServerError)
			}
			return
		}
		body.ParentID = normalizedParent
	}

	// Parse nullable date fields
	var deferUntil *string
	if body.DeferUntil != "" {
		deferUntil = &body.DeferUntil
	}
	var dueDate *string
	if body.DueDate != "" {
		dueDate = &body.DueDate
	}

	// Build the issue model
	issue := &models.Issue{
		Title:          body.Title,
		Description:    body.Description,
		Type:           issueType,
		Priority:       issuePriority,
		Points:         body.Points,
		Labels:         body.Labels,
		ParentID:       body.ParentID,
		Acceptance:     body.Acceptance,
		Sprint:         body.Sprint,
		Minor:          body.Minor,
		CreatorSession: ctx.SessionID,
		DeferUntil:     deferUntil,
		DueDate:        dueDate,
	}

	// Capture current git branch only when running against an on-disk td root.
	// td-sync invocations pass BaseDir == "" and intentionally skip branch
	// capture (no meaningful local repo to inspect).
	if ctx.BaseDir != "" {
		gitState, _ := git.GetState()
		if gitState != nil {
			issue.CreatedBranch = gitState.Branch
		}
	}

	// Create atomically with action log
	if err := ctx.DB.CreateIssueLogged(issue, ctx.SessionID); err != nil {
		slog.Error("create issue", "err", err)
		WriteError(w, ErrInternal, "failed to create issue", http.StatusInternalServerError)
		return
	}

	// Record session action for bypass prevention
	if err := ctx.DB.RecordSessionAction(issue.ID, ctx.SessionID, models.ActionSessionCreated); err != nil {
		slog.Warn("failed to record session history", "err", err)
	}

	notifyChange(ctx)

	dto := IssueToDTO(issue)
	WriteSuccess(w, map[string]interface{}{"issue": dto}, http.StatusCreated)
}

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	HandleCreateIssue(s.handlerContext(), w, r)
}

// ============================================================================
// PATCH /v1/issues/{id} — Update Issue
// ============================================================================

// HandleUpdateIssue applies a partial update to an existing issue.
// Pure-function form of (s *Server).handleUpdateIssue.
func HandleUpdateIssue(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	issueID := r.PathValue("id")
	if issueID == "" {
		WriteError(w, ErrValidation, "issue id is required", http.StatusBadRequest)
		return
	}

	var body IssueUpdateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, ErrValidation, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Resolve configurable title length limits
	titleMin, titleMax := titleLengthLimitsFor(ctx)

	// Validate provided fields
	if errs := ValidateIssueUpdate(&body, titleMin, titleMax); len(errs) > 0 {
		WriteValidation(w, errs)
		return
	}

	// Fetch existing issue
	issue, err := ctx.DB.GetIssue(issueID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("issue not found: %s", issueID), http.StatusNotFound)
		} else {
			slog.Error("get issue for update", "err", err, "id", issueID)
			WriteError(w, ErrInternal, "failed to fetch issue", http.StatusInternalServerError)
		}
		return
	}

	// Apply only non-nil fields
	if body.Title != nil {
		issue.Title = *body.Title
	}
	if body.Description != nil {
		issue.Description = *body.Description
	}
	if body.Acceptance != nil {
		issue.Acceptance = *body.Acceptance
	}
	if body.Type != nil && *body.Type != "" {
		issue.Type = models.NormalizeType(*body.Type)
	}
	if body.Priority != nil && *body.Priority != "" {
		issue.Priority = models.NormalizePriority(*body.Priority)
	}
	if body.Points != nil {
		issue.Points = *body.Points
	}
	if body.Labels != nil {
		issue.Labels = body.Labels
	}
	if body.ParentID != nil {
		parentID := *body.ParentID
		if parentID != "" {
			// Verify parent exists
			normalizedParent := db.NormalizeIssueID(parentID)
			_, err := ctx.DB.GetIssue(normalizedParent)
			if err != nil {
				if strings.Contains(err.Error(), "not found") {
					WriteError(w, ErrNotFound, fmt.Sprintf("parent issue not found: %s", parentID), http.StatusNotFound)
				} else {
					slog.Error("lookup parent issue", "err", err, "parent_id", parentID)
					WriteError(w, ErrInternal, "failed to verify parent issue", http.StatusInternalServerError)
				}
				return
			}
			issue.ParentID = normalizedParent
		} else {
			issue.ParentID = ""
		}
	}
	if body.Sprint != nil {
		issue.Sprint = *body.Sprint
	}
	if body.Minor != nil {
		issue.Minor = *body.Minor
	}
	if body.DeferUntil != nil {
		if *body.DeferUntil == "" {
			issue.DeferUntil = nil
		} else {
			issue.DeferUntil = body.DeferUntil
		}
	}
	if body.DueDate != nil {
		if *body.DueDate == "" {
			issue.DueDate = nil
		} else {
			issue.DueDate = body.DueDate
		}
	}

	// Update atomically with action log
	if err := ctx.DB.UpdateIssueLogged(issue, ctx.SessionID, models.ActionUpdate); err != nil {
		slog.Error("update issue", "err", err, "id", issueID)
		WriteError(w, ErrInternal, "failed to update issue", http.StatusInternalServerError)
		return
	}

	notifyChange(ctx)

	dto := IssueToDTO(issue)
	// Keep available_transitions fresh: a field edit (e.g. toggling `minor`) can
	// change which transitions are valid, and clients may use this response to
	// update their in-memory issue without a follow-up GET.
	dto.AvailableTransitions = availableTransitionsFor(ctx, issue)
	WriteSuccess(w, map[string]interface{}{"issue": dto}, http.StatusOK)
}

func (s *Server) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	HandleUpdateIssue(s.handlerContext(), w, r)
}

// ============================================================================
// DELETE /v1/issues/{id} — Soft Delete
// ============================================================================

// HandleDeleteIssue soft-deletes an issue. Pure-function form of
// (s *Server).handleDeleteIssue.
func HandleDeleteIssue(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	issueID := r.PathValue("id")
	if issueID == "" {
		WriteError(w, ErrValidation, "issue id is required", http.StatusBadRequest)
		return
	}

	// Verify issue exists
	issue, err := ctx.DB.GetIssue(issueID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("issue not found: %s", issueID), http.StatusNotFound)
		} else {
			slog.Error("get issue for delete", "err", err, "id", issueID)
			WriteError(w, ErrInternal, "failed to fetch issue", http.StatusInternalServerError)
		}
		return
	}

	// Soft delete with action log
	if err := ctx.DB.DeleteIssueLogged(issue.ID, ctx.SessionID); err != nil {
		slog.Error("delete issue", "err", err, "id", issue.ID)
		WriteError(w, ErrInternal, "failed to delete issue", http.StatusInternalServerError)
		return
	}

	notifyChange(ctx)

	WriteSuccess(w, map[string]interface{}{"deleted": true}, http.StatusOK)
}

func (s *Server) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	HandleDeleteIssue(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/boards — Create Board
// ============================================================================

// BoardCreateBody represents the expected JSON body for creating a board.
type BoardCreateBody struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

// HandleCreateBoard creates a new board from a JSON request body. Pure-function
// form of (s *Server).handleCreateBoard.
func HandleCreateBoard(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	var body BoardCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, ErrValidation, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate: name is required
	if strings.TrimSpace(body.Name) == "" {
		WriteValidation(w, []FieldError{{
			Field:   "name",
			Rule:    "required",
			Message: "name is required",
		}})
		return
	}

	// Validate: query must parse as TDQ if provided
	if body.Query != "" {
		if _, err := query.Parse(body.Query); err != nil {
			WriteValidation(w, []FieldError{{
				Field:   "query",
				Rule:    "tdq_syntax",
				Value:   body.Query,
				Message: "invalid TDQ query: " + err.Error(),
			}})
			return
		}
	}

	board, err := ctx.DB.CreateBoardLogged(body.Name, body.Query, ctx.SessionID)
	if err != nil {
		slog.Error("create board", "err", err)
		WriteError(w, ErrInternal, "failed to create board", http.StatusInternalServerError)
		return
	}

	notifyChange(ctx)

	dto := BoardToDTO(board)
	WriteSuccess(w, map[string]interface{}{"board": dto}, http.StatusCreated)
}

func (s *Server) handleCreateBoard(w http.ResponseWriter, r *http.Request) {
	HandleCreateBoard(s.handlerContext(), w, r)
}

// ============================================================================
// PATCH /v1/boards/{id} — Update Board
// ============================================================================

// BoardUpdateBody represents the expected JSON body for updating a board.
// All fields are optional; only present fields are applied.
type BoardUpdateBody struct {
	Name  *string `json:"name"`
	Query *string `json:"query"`
}

// HandleUpdateBoard applies a partial update to an existing board.
// Pure-function form of (s *Server).handleUpdateBoard.
func HandleUpdateBoard(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	boardID := r.PathValue("id")
	if boardID == "" {
		WriteError(w, ErrValidation, "board id is required", http.StatusBadRequest)
		return
	}

	var body BoardUpdateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, ErrValidation, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Resolve board by ID or name
	board, err := ctx.DB.ResolveBoardRef(boardID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("board not found: %s", boardID), http.StatusNotFound)
		} else {
			slog.Error("get board for update", "err", err, "id", boardID)
			WriteError(w, ErrInternal, "failed to fetch board", http.StatusInternalServerError)
		}
		return
	}

	// Cannot modify builtin boards
	if board.IsBuiltin {
		WriteError(w, ErrForbidden, "cannot modify builtin board", http.StatusForbidden)
		return
	}

	// Validate query if provided
	if body.Query != nil && *body.Query != "" {
		if _, err := query.Parse(*body.Query); err != nil {
			WriteValidation(w, []FieldError{{
				Field:   "query",
				Rule:    "tdq_syntax",
				Value:   *body.Query,
				Message: "invalid TDQ query: " + err.Error(),
			}})
			return
		}
	}

	// Apply only provided fields
	if body.Name != nil {
		if strings.TrimSpace(*body.Name) == "" {
			WriteValidation(w, []FieldError{{
				Field:   "name",
				Rule:    "required",
				Message: "name cannot be empty",
			}})
			return
		}
		board.Name = *body.Name
	}
	if body.Query != nil {
		board.Query = *body.Query
	}

	if err := ctx.DB.UpdateBoardLogged(board, ctx.SessionID); err != nil {
		slog.Error("update board", "err", err, "id", boardID)
		WriteError(w, ErrInternal, "failed to update board", http.StatusInternalServerError)
		return
	}

	notifyChange(ctx)

	dto := BoardToDTO(board)
	WriteSuccess(w, map[string]interface{}{"board": dto}, http.StatusOK)
}

func (s *Server) handleUpdateBoard(w http.ResponseWriter, r *http.Request) {
	HandleUpdateBoard(s.handlerContext(), w, r)
}

// ============================================================================
// DELETE /v1/boards/{id} — Delete Board
// ============================================================================

// HandleDeleteBoard deletes a board. Pure-function form of
// (s *Server).handleDeleteBoard.
func HandleDeleteBoard(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	boardID := r.PathValue("id")
	if boardID == "" {
		WriteError(w, ErrValidation, "board id is required", http.StatusBadRequest)
		return
	}

	// Resolve board by ID or name
	board, err := ctx.DB.ResolveBoardRef(boardID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("board not found: %s", boardID), http.StatusNotFound)
		} else {
			slog.Error("get board for delete", "err", err, "id", boardID)
			WriteError(w, ErrInternal, "failed to fetch board", http.StatusInternalServerError)
		}
		return
	}

	// Cannot delete builtin boards
	if board.IsBuiltin {
		WriteError(w, ErrForbidden, "cannot delete builtin board", http.StatusForbidden)
		return
	}

	if err := ctx.DB.DeleteBoardLogged(board.ID, ctx.SessionID); err != nil {
		slog.Error("delete board", "err", err, "id", boardID)
		WriteError(w, ErrInternal, "failed to delete board", http.StatusInternalServerError)
		return
	}

	notifyChange(ctx)

	WriteSuccess(w, map[string]interface{}{"deleted": true}, http.StatusOK)
}

func (s *Server) handleDeleteBoard(w http.ResponseWriter, r *http.Request) {
	HandleDeleteBoard(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/boards/{id}/issues — Set Board Position
// ============================================================================

// BoardPositionBody represents the expected JSON body for setting an issue position on a board.
type BoardPositionBody struct {
	IssueID  string `json:"issue_id"`
	Position int    `json:"position"`
}

// HandleSetBoardPosition sets or updates an issue's position on a board.
// Pure-function form of (s *Server).handleSetBoardPosition.
func HandleSetBoardPosition(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	boardID := r.PathValue("id")
	if boardID == "" {
		WriteError(w, ErrValidation, "board id is required", http.StatusBadRequest)
		return
	}

	var body BoardPositionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, ErrValidation, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate: issue_id is required
	if body.IssueID == "" {
		WriteValidation(w, []FieldError{{
			Field:   "issue_id",
			Rule:    "required",
			Message: "issue_id is required",
		}})
		return
	}

	// Verify board exists
	board, err := ctx.DB.ResolveBoardRef(boardID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("board not found: %s", boardID), http.StatusNotFound)
		} else {
			slog.Error("get board for position", "err", err, "id", boardID)
			WriteError(w, ErrInternal, "failed to fetch board", http.StatusInternalServerError)
		}
		return
	}

	// Verify issue exists
	normalizedIssueID := db.NormalizeIssueID(body.IssueID)
	_, err = ctx.DB.GetIssue(normalizedIssueID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("issue not found: %s", body.IssueID), http.StatusNotFound)
		} else {
			slog.Error("get issue for board position", "err", err, "issue_id", body.IssueID)
			WriteError(w, ErrInternal, "failed to fetch issue", http.StatusInternalServerError)
		}
		return
	}

	// Compute sort key from the position slot
	sortKey, _, err := ctx.DB.ComputeInsertPosition(board.ID, body.Position)
	if err != nil {
		slog.Error("compute insert position", "err", err, "board_id", board.ID, "position", body.Position)
		WriteError(w, ErrInternal, "failed to compute position", http.StatusInternalServerError)
		return
	}

	// Set the position
	if err := ctx.DB.SetIssuePositionLogged(board.ID, normalizedIssueID, sortKey, ctx.SessionID); err != nil {
		slog.Error("set board position", "err", err, "board_id", board.ID, "issue_id", normalizedIssueID)
		WriteError(w, ErrInternal, "failed to set position", http.StatusInternalServerError)
		return
	}

	notifyChange(ctx)

	WriteSuccess(w, map[string]interface{}{"positioned": true}, http.StatusOK)
}

func (s *Server) handleSetBoardPosition(w http.ResponseWriter, r *http.Request) {
	HandleSetBoardPosition(s.handlerContext(), w, r)
}

// ============================================================================
// DELETE /v1/boards/{id}/issues/{issue_id} — Remove Board Position
// ============================================================================

// HandleRemoveBoardPosition removes an issue's explicit position from a board.
// Pure-function form of (s *Server).handleRemoveBoardPosition.
func HandleRemoveBoardPosition(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	boardID := r.PathValue("id")
	issueID := r.PathValue("issue_id")

	if boardID == "" {
		WriteError(w, ErrValidation, "board id is required", http.StatusBadRequest)
		return
	}
	if issueID == "" {
		WriteError(w, ErrValidation, "issue id is required", http.StatusBadRequest)
		return
	}

	// Verify board exists
	board, err := ctx.DB.ResolveBoardRef(boardID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("board not found: %s", boardID), http.StatusNotFound)
		} else {
			slog.Error("get board for position removal", "err", err, "id", boardID)
			WriteError(w, ErrInternal, "failed to fetch board", http.StatusInternalServerError)
		}
		return
	}

	normalizedIssueID := db.NormalizeIssueID(issueID)

	if err := ctx.DB.RemoveIssuePositionLogged(board.ID, normalizedIssueID, ctx.SessionID); err != nil {
		if strings.Contains(err.Error(), "not positioned") {
			WriteError(w, ErrNotFound, fmt.Sprintf("issue %s not positioned on board %s", issueID, boardID), http.StatusNotFound)
		} else {
			slog.Error("remove board position", "err", err, "board_id", board.ID, "issue_id", normalizedIssueID)
			WriteError(w, ErrInternal, "failed to remove position", http.StatusInternalServerError)
		}
		return
	}

	notifyChange(ctx)

	WriteSuccess(w, map[string]interface{}{"removed": true}, http.StatusOK)
}

func (s *Server) handleRemoveBoardPosition(w http.ResponseWriter, r *http.Request) {
	HandleRemoveBoardPosition(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/issues/{id}/comments — Add Comment
// ============================================================================

// CommentCreateBody represents the expected JSON body for adding a comment.
type CommentCreateBody struct {
	Text string `json:"text"`
}

// HandleAddComment adds a comment to an issue. Pure-function form of
// (s *Server).handleAddComment.
func HandleAddComment(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	issueID := r.PathValue("id")
	if issueID == "" {
		WriteError(w, ErrValidation, "issue id is required", http.StatusBadRequest)
		return
	}

	var body CommentCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, ErrValidation, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate: text must not be empty
	if strings.TrimSpace(body.Text) == "" {
		WriteValidation(w, []FieldError{{
			Field:   "text",
			Rule:    "required",
			Message: "text is required",
		}})
		return
	}

	// Verify issue exists
	issue, err := ctx.DB.GetIssue(issueID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("issue not found: %s", issueID), http.StatusNotFound)
		} else {
			slog.Error("get issue for comment", "err", err, "id", issueID)
			WriteError(w, ErrInternal, "failed to fetch issue", http.StatusInternalServerError)
		}
		return
	}

	comment := &models.Comment{
		IssueID:   issue.ID,
		SessionID: ctx.SessionID,
		Text:      body.Text,
	}

	if err := ctx.DB.AddComment(comment); err != nil {
		slog.Error("add comment", "err", err, "issue_id", issue.ID)
		WriteError(w, ErrInternal, "failed to add comment", http.StatusInternalServerError)
		return
	}

	notifyChange(ctx)

	dto := CommentToDTO(comment)
	WriteSuccess(w, map[string]interface{}{"comment": dto}, http.StatusCreated)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	HandleAddComment(s.handlerContext(), w, r)
}

// ============================================================================
// DELETE /v1/issues/{id}/comments/{comment_id} — Delete Comment
// ============================================================================

// HandleDeleteComment deletes a comment from an issue. Pure-function form of
// (s *Server).handleDeleteComment.
func HandleDeleteComment(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	issueID := db.NormalizeIssueID(r.PathValue("id"))
	commentID := r.PathValue("comment_id")

	if issueID == "" {
		WriteError(w, ErrValidation, "issue id is required", http.StatusBadRequest)
		return
	}
	if commentID == "" {
		WriteError(w, ErrValidation, "comment id is required", http.StatusBadRequest)
		return
	}

	// Look up the comment and verify it belongs to this issue
	comment, err := ctx.DB.GetCommentByID(commentID)
	if err != nil {
		slog.Error("get comment for delete", "err", err, "comment_id", commentID)
		WriteError(w, ErrInternal, "failed to fetch comment", http.StatusInternalServerError)
		return
	}
	if comment == nil {
		WriteError(w, ErrNotFound, fmt.Sprintf("comment not found: %s", commentID), http.StatusNotFound)
		return
	}
	if comment.IssueID != issueID {
		WriteError(w, ErrNotFound, fmt.Sprintf("comment %s not found on issue %s", commentID, issueID), http.StatusNotFound)
		return
	}

	// Hard-delete with action log
	if err := ctx.DB.DeleteCommentLogged(commentID, ctx.SessionID); err != nil {
		slog.Error("delete comment", "err", err, "comment_id", commentID)
		WriteError(w, ErrInternal, "failed to delete comment", http.StatusInternalServerError)
		return
	}

	notifyChange(ctx)

	WriteSuccess(w, map[string]interface{}{"deleted": true}, http.StatusOK)
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	HandleDeleteComment(s.handlerContext(), w, r)
}

// ============================================================================
// POST /v1/issues/{id}/dependencies — Add Dependency
// ============================================================================

// DependencyCreateBody represents the expected JSON body for adding a dependency.
type DependencyCreateBody struct {
	DependsOn string `json:"depends_on"`
}

// HandleAddDependency adds a dependency between two issues. Pure-function form
// of (s *Server).handleAddDependency.
func HandleAddDependency(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	requestedIssueID := r.PathValue("id")
	if requestedIssueID == "" {
		WriteError(w, ErrValidation, "issue id is required", http.StatusBadRequest)
		return
	}

	var body DependencyCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, ErrValidation, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if body.DependsOn == "" {
		WriteValidation(w, []FieldError{{
			Field:   "depends_on",
			Rule:    "required",
			Message: "depends_on is required",
		}})
		return
	}

	issue, err := ctx.DB.GetIssue(requestedIssueID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("issue not found: %s", requestedIssueID), http.StatusNotFound)
		} else {
			slog.Error("get issue for dependency", "err", err, "id", requestedIssueID)
			WriteError(w, ErrInternal, "failed to fetch issue", http.StatusInternalServerError)
		}
		return
	}
	issueID := issue.ID
	dependsOnID := db.NormalizeIssueID(body.DependsOn)

	// Validate both issues exist, check for cycles and duplicates
	if err := dependency.Validate(ctx.DB, issueID, dependsOnID); err != nil {
		if err == dependency.ErrDependencyExists {
			WriteError(w, ErrConflict, "dependency already exists", http.StatusConflict)
			return
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			WriteError(w, ErrNotFound, errMsg, http.StatusNotFound)
			return
		}
		if strings.Contains(errMsg, "circular") {
			WriteError(w, ErrValidation, errMsg, http.StatusBadRequest)
			return
		}
		slog.Error("validate dependency", "err", err, "issue_id", issueID, "depends_on", dependsOnID)
		WriteError(w, ErrInternal, "failed to validate dependency", http.StatusInternalServerError)
		return
	}

	// Add the dependency with action log
	if err := ctx.DB.AddDependencyLogged(issueID, dependsOnID, "depends_on", ctx.SessionID); err != nil {
		slog.Error("add dependency", "err", err, "issue_id", issueID, "depends_on", dependsOnID)
		WriteError(w, ErrInternal, "failed to add dependency", http.StatusInternalServerError)
		return
	}

	notifyChange(ctx)

	depID := db.DependencyID(issueID, dependsOnID, "depends_on")
	dto := DependencyDTO{
		DepID:        depID,
		IssueID:      issueID,
		DependsOnID:  dependsOnID,
		RelationType: "depends_on",
	}

	WriteSuccess(w, map[string]interface{}{"dependency": dto}, http.StatusCreated)
}

func (s *Server) handleAddDependency(w http.ResponseWriter, r *http.Request) {
	HandleAddDependency(s.handlerContext(), w, r)
}

// ============================================================================
// DELETE /v1/issues/{id}/dependencies/{dep_id} — Remove Dependency
// ============================================================================

// HandleDeleteDependency removes a dependency by its dep_id. Pure-function form
// of (s *Server).handleDeleteDependency.
func HandleDeleteDependency(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	issueID := db.NormalizeIssueID(r.PathValue("id"))
	depID := r.PathValue("dep_id")

	if issueID == "" {
		WriteError(w, ErrValidation, "issue id is required", http.StatusBadRequest)
		return
	}
	if depID == "" {
		WriteError(w, ErrValidation, "dependency id is required", http.StatusBadRequest)
		return
	}

	// Look up the dependency by its deterministic dep_id
	dep, err := ctx.DB.GetDependencyByDepID(depID)
	if err != nil {
		slog.Error("get dependency for delete", "err", err, "dep_id", depID)
		WriteError(w, ErrInternal, "failed to fetch dependency", http.StatusInternalServerError)
		return
	}
	if dep == nil {
		WriteError(w, ErrNotFound, fmt.Sprintf("dependency not found: %s", depID), http.StatusNotFound)
		return
	}

	// Verify the dependency belongs to the issue in the URL
	if dep.IssueID != issueID {
		WriteError(w, ErrNotFound, fmt.Sprintf("dependency %s not found on issue %s", depID, issueID), http.StatusNotFound)
		return
	}

	// Remove with action log
	if err := ctx.DB.RemoveDependencyLogged(dep.IssueID, dep.DependsOnID, ctx.SessionID); err != nil {
		slog.Error("remove dependency", "err", err, "dep_id", depID)
		WriteError(w, ErrInternal, "failed to remove dependency", http.StatusInternalServerError)
		return
	}

	notifyChange(ctx)

	WriteSuccess(w, map[string]interface{}{"removed": true}, http.StatusOK)
}

func (s *Server) handleDeleteDependency(w http.ResponseWriter, r *http.Request) {
	HandleDeleteDependency(s.handlerContext(), w, r)
}

// ============================================================================
// PUT /v1/focus — Set or Clear Focused Issue
// ============================================================================

// FocusBody represents the expected JSON body for setting focus.
// IssueID is a pointer so that null/absent can be distinguished from empty string.
type FocusBody struct {
	IssueID *string `json:"issue_id"`
}

// HandleSetFocus sets or clears the focused issue via config.json. Requires
// ctx.BaseDir to be non-empty (focus state lives in the local on-disk config).
// Pure-function form of (s *Server).handleSetFocus.
func HandleSetFocus(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	var body FocusBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, ErrValidation, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Focus is a local-process concept (lives in baseDir/config.json). Callers
	// without an on-disk td root (td-sync) cannot set focus.
	if ctx.BaseDir == "" {
		WriteError(w, ErrInternal, "focus is not available without a local td root", http.StatusServiceUnavailable)
		return
	}

	if body.IssueID == nil || *body.IssueID == "" {
		// Clear focus
		if err := config.ClearFocus(ctx.BaseDir); err != nil {
			slog.Error("clear focus", "err", err)
			WriteError(w, ErrInternal, "failed to clear focus", http.StatusInternalServerError)
			return
		}
		WriteSuccess(w, map[string]interface{}{"focused_issue_id": nil}, http.StatusOK)
		return
	}

	// Set focus — verify issue exists first
	issueID := *body.IssueID
	issue, err := ctx.DB.GetIssue(issueID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, fmt.Sprintf("issue not found: %s", issueID), http.StatusNotFound)
		} else {
			slog.Error("get issue for focus", "err", err, "id", issueID)
			WriteError(w, ErrInternal, "failed to fetch issue", http.StatusInternalServerError)
		}
		return
	}

	if err := config.SetFocus(ctx.BaseDir, issue.ID); err != nil {
		slog.Error("set focus", "err", err, "issue_id", issue.ID)
		WriteError(w, ErrInternal, "failed to set focus", http.StatusInternalServerError)
		return
	}

	// Do NOT trigger notifyChange for focus changes
	WriteSuccess(w, map[string]interface{}{"focused_issue_id": issue.ID}, http.StatusOK)
}

func (s *Server) handleSetFocus(w http.ResponseWriter, r *http.Request) {
	HandleSetFocus(s.handlerContext(), w, r)
}
