package serve

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/query"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/pkg/monitor"
)

// This file contains the read-side HTTP handlers (GET endpoints). Each handler
// is exported as a pure function that takes a HandlerContext, so the same code
// can be mounted from td-serve (`*Server`) and from td-sync (per-project
// HandlerContext built per request). The `(s *Server) handleXxx` methods are
// thin wrappers retained so the route registrations and any external callers
// continue to work unchanged.

// ============================================================================
// GET /health
// ============================================================================

// HandleHealth returns server status, the active session id, and the current
// change_token. Pure-function form of (s *Server).handleHealth.
func HandleHealth(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	changeToken, _ := ctx.DB.GetChangeToken()

	WriteSuccess(w, map[string]interface{}{
		"status":       "ok",
		"session_id":   ctx.SessionID,
		"change_token": changeToken,
	}, http.StatusOK)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	HandleHealth(s.handlerContext(), w, r)
}

// ============================================================================
// GET /v1/monitor
// ============================================================================

// HandleMonitor returns the consolidated monitor view (boards, focus, recent
// activity). Pure-function form of (s *Server).handleMonitor.
func HandleMonitor(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	includeClosed := q.Get("include_closed") == "true"
	sortMode := monitor.SortModeFromString(q.Get("sort"))
	search := q.Get("search")
	searchMode := q.Get("search_mode") // auto, text, tdq

	// For search_mode=tdq, validate the query first
	if searchMode == "tdq" && search != "" {
		_, err := query.Parse(search)
		if err != nil {
			WriteError(w, ErrValidation, "invalid TDQ query: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	msg := monitor.FetchDataWithSearchMode(ctx.DB, ctx.SessionID, time.Now().Add(-24*time.Hour), search, searchMode, includeClosed, sortMode)
	dto := MonitorDataToDTO(&msg)

	changeToken, _ := ctx.DB.GetChangeToken()

	WriteSuccess(w, map[string]interface{}{
		"monitor":      dto,
		"session_id":   ctx.SessionID,
		"change_token": changeToken,
	}, http.StatusOK)
}

func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request) {
	HandleMonitor(s.handlerContext(), w, r)
}

// ============================================================================
// GET /v1/issues
// ============================================================================

// HandleListIssues returns a paginated list of issues with optional filtering
// (status, type, priority, labels, epic, search). Pure-function form of
// (s *Server).handleListIssues.
func HandleListIssues(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// Parse pagination
	limit := 200
	if v := q.Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			limit = parsed
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			offset = parsed
		}
	}

	// Validate pagination
	if errs := ValidatePagination(limit, offset); len(errs) > 0 {
		WriteValidation(w, errs)
		return
	}

	// Parse filters
	statuses := parseStatusParams(q["status"])
	types := parseTypeParams(q["type"])
	priorities := q["priority"]
	labels := parseStringParams(q["labels"])
	if len(labels) == 0 {
		labels = parseStringParams(q["label"])
	}
	epicID := strings.TrimSpace(q.Get("epic"))
	if epicID == "" {
		epicID = strings.TrimSpace(q.Get("epic_id"))
	}
	search := q.Get("search")
	searchMode := q.Get("search_mode") // auto, text, tdq
	includeClosed := q.Get("include_closed") == "true"
	sortBy := q.Get("sort")
	order := q.Get("order")

	// Determine sort column and direction
	sortCol, sortDesc := resolveSortOptions(sortBy, order)

	// If not include_closed and no explicit status filter, exclude closed
	if !includeClosed && len(statuses) == 0 {
		statuses = []models.Status{
			models.StatusOpen,
			models.StatusInProgress,
			models.StatusBlocked,
			models.StatusInReview,
		}
	}

	// Build priority filter string (single value for ListIssuesOptions)
	priorityFilter := ""
	if len(priorities) == 1 {
		priorityFilter = priorities[0]
	}

	// Handle TDQ search
	if search != "" && (searchMode == "tdq" || searchMode == "auto" || searchMode == "") {
		issues, err := tryTDQSearch(ctx, search, searchMode, statuses)
		if err == nil {
			// TDQ succeeded - apply type, priority filters and pagination manually
			filtered := filterIssues(issues, types, priorities)
			filtered = filterByLabels(filtered, labels)
			if epicID != "" {
				epicIssues, epicErr := ctx.DB.ListIssues(db.ListIssuesOptions{EpicID: epicID})
				if epicErr != nil {
					WriteError(w, ErrInternal, "failed to list epic issues: "+epicErr.Error(), http.StatusInternalServerError)
					return
				}
				allowed := make(map[string]bool, len(epicIssues))
				for _, issue := range epicIssues {
					allowed[issue.ID] = true
				}
				filtered = filterByIssueIDs(filtered, allowed)
			}
			total := len(filtered)
			paged := applyPagination(filtered, offset, limit)

			WriteSuccess(w, map[string]interface{}{
				"issues":   IssuesToDTOs(paged),
				"total":    total,
				"limit":    limit,
				"offset":   offset,
				"has_more": offset+limit < total,
			}, http.StatusOK)
			return
		}
		// TDQ failed
		if searchMode == "tdq" {
			// Explicit TDQ mode - return error
			WriteError(w, ErrValidation, "invalid TDQ query: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Auto mode - fall through to text search
	}

	// Text search or no search
	opts := db.ListIssuesOptions{
		Status:   statuses,
		Type:     types,
		Priority: priorityFilter,
		Labels:   labels,
		EpicID:   epicID,
		Search:   search,
		SortBy:   sortCol,
		SortDesc: sortDesc,
	}

	// Get all matching issues (we need total count)
	allIssues, err := ctx.DB.ListIssues(opts)
	if err != nil {
		WriteError(w, ErrInternal, "failed to list issues: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply multi-priority filter if more than one priority specified
	if len(priorities) > 1 {
		allIssues = filterByPriorities(allIssues, priorities)
	}

	total := len(allIssues)
	paged := applyPagination(allIssues, offset, limit)

	WriteSuccess(w, map[string]interface{}{
		"issues":   issuesToDTOsNonNil(paged),
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+limit < total,
	}, http.StatusOK)
}

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	HandleListIssues(s.handlerContext(), w, r)
}

// ============================================================================
// GET /v1/issues/{id}
// ============================================================================

// HandleGetIssue returns a single issue with its logs, comments, latest
// handoff, and dependency graph. Pure-function form of
// (s *Server).handleGetIssue.
func HandleGetIssue(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		WriteError(w, ErrValidation, "issue ID is required", http.StatusBadRequest)
		return
	}

	issue, err := ctx.DB.GetIssue(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, "issue not found: "+id, http.StatusNotFound)
		} else {
			WriteError(w, ErrInternal, "failed to get issue: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Fetch logs
	logs, err := ctx.DB.GetLogs(issue.ID, 0)
	if err != nil {
		logs = nil
	}

	// Fetch comments
	comments, err := ctx.DB.GetComments(issue.ID)
	if err != nil {
		comments = nil
	}

	// Fetch latest handoff
	handoff, _ := ctx.DB.GetLatestHandoff(issue.ID)

	// Fetch dependencies (outgoing: what this issue depends on)
	depIDs, _ := ctx.DB.GetDependencies(issue.ID)
	dependencies := make([]DependencyDTO, 0, len(depIDs))
	for _, depID := range depIDs {
		dependencies = append(dependencies, DependencyDTO{
			DepID:        db.DependencyID(issue.ID, depID, "depends_on"),
			IssueID:      issue.ID,
			DependsOnID:  depID,
			RelationType: "depends_on",
		})
	}

	// Fetch blocked_by (incoming: issues that depend on this one)
	blockedByIDs, _ := ctx.DB.GetBlockedBy(issue.ID)
	blockedBy := make([]DependencyDTO, 0, len(blockedByIDs))
	for _, blockerID := range blockedByIDs {
		blockedBy = append(blockedBy, DependencyDTO{
			DepID:        db.DependencyID(blockerID, issue.ID, "depends_on"),
			IssueID:      blockerID,
			DependsOnID:  issue.ID,
			RelationType: "depends_on",
		})
	}

	// Build response
	var handoffDTO *HandoffDTO
	if handoff != nil {
		h := HandoffToDTO(handoff)
		handoffDTO = &h
	}

	issueDTO := IssueToDTO(issue)
	// Always populate active_review on the single-issue read so clients can
	// immediately tell "reviewed, awaiting close" from "not yet reviewed".
	if summary := activeReviewSummary(ctx, issue.ID); summary != nil {
		issueDTO.ActiveReview = summary
	}
	// Opt-in full review history via ?with=reviews. Split on ',' to allow
	// future composition (e.g. ?with=reviews,logs) while avoiding accidental
	// matches against stray query params that contain the substring "with=".
	if hasWithValue(r.URL.Query().Get("with"), "reviews") {
		if reviews, err := ctx.DB.ListIssueReviews(issue.ID); err == nil {
			dtos := make([]IssueReviewDTO, 0, len(reviews))
			for _, r := range reviews {
				dtos = append(dtos, IssueReviewToDTO(r))
			}
			issueDTO.Reviews = dtos
		}
	}

	WriteSuccess(w, map[string]interface{}{
		"issue":          issueDTO,
		"logs":           logsToDTOsNonNil(logs),
		"comments":       commentsToDTOsNonNil(comments),
		"latest_handoff": handoffDTO,
		"dependencies":   dependencies,
		"blocked_by":     blockedBy,
	}, http.StatusOK)
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	HandleGetIssue(s.handlerContext(), w, r)
}

// ============================================================================
// GET /v1/sessions
// ============================================================================

// HandleListSessions returns the list of known sessions and the caller's
// current session id. Pure-function form of (s *Server).handleListSessions.
func HandleListSessions(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	sessions, err := session.ListSessions(ctx.DB)
	if err != nil {
		WriteError(w, ErrInternal, "failed to list sessions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"sessions":           SessionsToDTOs(sessions),
		"current_session_id": ctx.SessionID,
	}, http.StatusOK)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	HandleListSessions(s.handlerContext(), w, r)
}

// ============================================================================
// GET /v1/stats
// ============================================================================

// HandleStats returns aggregate counts and progress metrics. Pure-function
// form of (s *Server).handleStats.
func HandleStats(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	stats, err := ctx.DB.GetExtendedStats()
	if err != nil {
		WriteError(w, ErrInternal, "failed to get stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	WriteSuccess(w, StatsToDTO(stats), http.StatusOK)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	HandleStats(s.handlerContext(), w, r)
}

// ============================================================================
// GET /v1/labels
// ============================================================================

// HandleListLabels returns the distinct label catalog for the project. Pure-
// function form of (s *Server).handleListLabels.
func HandleListLabels(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	labels, err := ctx.DB.ListDistinctLabels()
	if err != nil {
		WriteError(w, ErrInternal, "failed to list labels: "+err.Error(), http.StatusInternalServerError)
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"labels":           labels,
		"workflows":        []interface{}{},
		"default_workflow": "standard",
	}, http.StatusOK)
}

func (s *Server) handleListLabels(w http.ResponseWriter, r *http.Request) {
	HandleListLabels(s.handlerContext(), w, r)
}

// ============================================================================
// GET /v1/boards
// ============================================================================

// HandleListBoards returns all boards. Pure-function form of
// (s *Server).handleListBoards.
func HandleListBoards(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	boards, err := ctx.DB.ListBoards()
	if err != nil {
		WriteError(w, ErrInternal, "failed to list boards: "+err.Error(), http.StatusInternalServerError)
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"boards": boardsToDTOsNonNil(boards),
	}, http.StatusOK)
}

func (s *Server) handleListBoards(w http.ResponseWriter, r *http.Request) {
	HandleListBoards(s.handlerContext(), w, r)
}

// ============================================================================
// GET /v1/boards/{id}
// ============================================================================

// HandleGetBoard returns a board and its issue list (resolved either via the
// board's TDQ query or via stored positions). Pure-function form of
// (s *Server).handleGetBoard.
func HandleGetBoard(ctx HandlerContext, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		WriteError(w, ErrValidation, "board ID is required", http.StatusBadRequest)
		return
	}

	board, err := ctx.DB.ResolveBoardRef(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, "board not found: "+id, http.StatusNotFound)
		} else {
			WriteError(w, ErrInternal, "failed to get board: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	q := r.URL.Query()
	includeClosed := q.Get("include_closed") == "true"

	// Build status filter
	var statusFilter []models.Status
	if includeClosed {
		statusFilter = nil // no filter = all statuses
	} else {
		statusFilter = []models.Status{
			models.StatusOpen,
			models.StatusInProgress,
			models.StatusBlocked,
			models.StatusInReview,
		}
	}

	// Resolve board issues
	var boardIssues []models.BoardIssueView
	if board.Query != "" {
		// Execute TDQ query with neutral @me behavior
		// Pass empty session ID to neutralize @me clauses
		queryResults, err := query.Execute(ctx.DB, board.Query, "", query.ExecuteOptions{})
		if err != nil {
			WriteError(w, ErrInternal, "board query error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Filter by status
		var filtered []models.Issue
		if len(statusFilter) > 0 {
			statusSet := make(map[models.Status]bool)
			for _, st := range statusFilter {
				statusSet[st] = true
			}
			for _, issue := range queryResults {
				if statusSet[issue.Status] {
					filtered = append(filtered, issue)
				}
			}
		} else {
			filtered = queryResults
		}

		boardIssues, err = ctx.DB.ApplyBoardPositions(board.ID, filtered)
		if err != nil {
			WriteError(w, ErrInternal, "failed to apply board positions: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// Empty query - use GetBoardIssues
		boardIssues, err = ctx.DB.GetBoardIssues(board.ID, ctx.SessionID, statusFilter)
		if err != nil {
			WriteError(w, ErrInternal, "failed to get board issues: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Convert board issues to DTOs
	issueDTOs := make([]map[string]interface{}, 0, len(boardIssues))
	for _, biv := range boardIssues {
		issueDTOs = append(issueDTOs, map[string]interface{}{
			"issue":        IssueToDTO(&biv.Issue),
			"board_id":     biv.BoardID,
			"position":     biv.Position,
			"has_position": biv.HasPosition,
			"category":     biv.Category,
		})
	}

	WriteSuccess(w, map[string]interface{}{
		"board":  BoardToDTO(board),
		"issues": issueDTOs,
	}, http.StatusOK)
}

func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request) {
	HandleGetBoard(s.handlerContext(), w, r)
}

// ============================================================================
// Helpers
// ============================================================================

// parseStatusParams converts repeated query params like ?status=open&status=closed
// into a slice of models.Status values.
func parseStatusParams(values []string) []models.Status {
	var statuses []models.Status
	for _, v := range values {
		// Support comma-separated within a single param
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			status := models.NormalizeStatus(part)
			if models.IsValidStatus(status) {
				statuses = append(statuses, status)
			}
		}
	}
	return statuses
}

// parseTypeParams converts repeated query params into models.Type values.
func parseTypeParams(values []string) []models.Type {
	var types []models.Type
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			t := models.NormalizeType(part)
			if models.IsValidType(t) {
				types = append(types, t)
			}
		}
	}
	return types
}

// parseStringParams converts repeated query params into trimmed string values.
func parseStringParams(values []string) []string {
	var result []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}

// resolveSortOptions converts sort/order query params to DB options.
func resolveSortOptions(sortBy, order string) (string, bool) {
	// Map API sort names to DB column names
	colMap := map[string]string{
		"priority": "priority",
		"created":  "created_at",
		"updated":  "updated_at",
		"id":       "id",
		"title":    "title",
		"status":   "status",
		"type":     "type",
		"points":   "points",
	}

	col := "priority" // default
	if mapped, ok := colMap[sortBy]; ok {
		col = mapped
	}

	// Default direction depends on sort column
	desc := false
	switch col {
	case "created_at", "updated_at":
		desc = true // newest first by default
	}

	// Explicit order overrides default
	if order == "asc" {
		desc = false
	} else if order == "desc" {
		desc = true
	}

	return col, desc
}

// tryTDQSearch attempts a TDQ search and returns issues or an error. Reads
// from ctx.DB and uses ctx.SessionID for @me resolution.
func tryTDQSearch(ctx HandlerContext, search, searchMode string, statuses []models.Status) ([]models.Issue, error) {
	issues, err := query.Execute(ctx.DB, search, ctx.SessionID, query.ExecuteOptions{})
	if err != nil {
		return nil, err
	}

	// Filter by statuses if provided
	if len(statuses) > 0 {
		statusSet := make(map[models.Status]bool)
		for _, st := range statuses {
			statusSet[st] = true
		}
		var filtered []models.Issue
		for _, issue := range issues {
			if statusSet[issue.Status] {
				filtered = append(filtered, issue)
			}
		}
		return filtered, nil
	}

	return issues, nil
}

// filterIssues applies type and priority filters to a slice of issues.
func filterIssues(issues []models.Issue, types []models.Type, priorities []string) []models.Issue {
	if len(types) == 0 && len(priorities) == 0 {
		return issues
	}

	var typeSet map[models.Type]bool
	if len(types) > 0 {
		typeSet = make(map[models.Type]bool)
		for _, t := range types {
			typeSet[t] = true
		}
	}

	var prioSet map[string]bool
	if len(priorities) > 0 {
		prioSet = make(map[string]bool)
		for _, p := range priorities {
			prioSet[p] = true
		}
	}

	var result []models.Issue
	for _, issue := range issues {
		if typeSet != nil && !typeSet[issue.Type] {
			continue
		}
		if prioSet != nil && !prioSet[string(issue.Priority)] {
			continue
		}
		result = append(result, issue)
	}
	return result
}

// filterByLabels filters issues that contain all requested labels.
func filterByLabels(issues []models.Issue, labels []string) []models.Issue {
	if len(labels) == 0 {
		return issues
	}

	var result []models.Issue
	for _, issue := range issues {
		if issueHasAllLabels(issue.Labels, labels) {
			result = append(result, issue)
		}
	}
	return result
}

// filterByIssueIDs keeps only issues whose IDs appear in the allowed set.
func filterByIssueIDs(issues []models.Issue, allowed map[string]bool) []models.Issue {
	if len(allowed) == 0 {
		return []models.Issue{}
	}

	var result []models.Issue
	for _, issue := range issues {
		if allowed[issue.ID] {
			result = append(result, issue)
		}
	}
	return result
}

// issueHasAllLabels checks whether issueLabels contains every label in requiredLabels.
func issueHasAllLabels(issueLabels, requiredLabels []string) bool {
	if len(requiredLabels) == 0 {
		return true
	}

	labelSet := make(map[string]bool, len(issueLabels))
	for _, label := range issueLabels {
		labelSet[label] = true
	}
	for _, required := range requiredLabels {
		if !labelSet[required] {
			return false
		}
	}
	return true
}

// filterByPriorities filters issues by multiple priority values.
func filterByPriorities(issues []models.Issue, priorities []string) []models.Issue {
	prioSet := make(map[string]bool)
	for _, p := range priorities {
		prioSet[p] = true
	}

	var result []models.Issue
	for _, issue := range issues {
		if prioSet[string(issue.Priority)] {
			result = append(result, issue)
		}
	}
	return result
}

// applyPagination applies offset and limit to a slice of issues.
func applyPagination(issues []models.Issue, offset, limit int) []models.Issue {
	if offset >= len(issues) {
		return nil
	}
	end := offset + limit
	if end > len(issues) {
		end = len(issues)
	}
	return issues[offset:end]
}

// logsToDTOsNonNil converts logs to DTOs, returning empty slice instead of nil.
func logsToDTOsNonNil(logs []models.Log) []LogDTO {
	if len(logs) == 0 {
		return []LogDTO{}
	}
	return LogsToDTOs(logs)
}

// commentsToDTOsNonNil converts comments to DTOs, returning empty slice instead of nil.
func commentsToDTOsNonNil(comments []models.Comment) []CommentDTO {
	if len(comments) == 0 {
		return []CommentDTO{}
	}
	return CommentsToDTOs(comments)
}

// boardsToDTOsNonNil converts boards to DTOs, returning empty slice instead of nil.
func boardsToDTOsNonNil(boards []models.Board) []BoardDTO {
	if len(boards) == 0 {
		return []BoardDTO{}
	}
	return BoardsToDTOs(boards)
}

// hasWithValue reports whether `want` appears in the comma-separated list of
// values supplied via the ?with=... query param. Trims whitespace on each
// split token. Used so ?with=reviews,logs can opt into multiple includes
// without falling for substring collisions on the raw query.
func hasWithValue(raw, want string) bool {
	if raw == "" || want == "" {
		return false
	}
	for _, v := range strings.Split(raw, ",") {
		if strings.TrimSpace(v) == want {
			return true
		}
	}
	return false
}
