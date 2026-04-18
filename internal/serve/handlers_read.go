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

// ============================================================================
// GET /health
// ============================================================================

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	changeToken, _ := s.db.GetChangeToken()

	WriteSuccess(w, map[string]interface{}{
		"status":       "ok",
		"session_id":   s.sessionID,
		"change_token": changeToken,
	}, http.StatusOK)
}

// ============================================================================
// GET /v1/monitor
// ============================================================================

func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request) {
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

	msg := monitor.FetchDataWithSearchMode(s.db, s.sessionID, time.Now().Add(-24*time.Hour), search, searchMode, includeClosed, sortMode)
	dto := MonitorDataToDTO(&msg)

	changeToken, _ := s.db.GetChangeToken()

	WriteSuccess(w, map[string]interface{}{
		"monitor":      dto,
		"session_id":   s.sessionID,
		"change_token": changeToken,
	}, http.StatusOK)
}

// ============================================================================
// GET /v1/issues
// ============================================================================

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
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
		issues, err := s.tryTDQSearch(search, searchMode, statuses)
		if err == nil {
			// TDQ succeeded - apply type, priority filters and pagination manually
			filtered := filterIssues(issues, types, priorities)
			filtered = filterByLabels(filtered, labels)
			if epicID != "" {
				epicIssues, epicErr := s.db.ListIssues(db.ListIssuesOptions{EpicID: epicID})
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
	allIssues, err := s.db.ListIssues(opts)
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

// ============================================================================
// GET /v1/issues/{id}
// ============================================================================

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		WriteError(w, ErrValidation, "issue ID is required", http.StatusBadRequest)
		return
	}

	issue, err := s.db.GetIssue(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, ErrNotFound, "issue not found: "+id, http.StatusNotFound)
		} else {
			WriteError(w, ErrInternal, "failed to get issue: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Fetch logs
	logs, err := s.db.GetLogs(issue.ID, 0)
	if err != nil {
		logs = nil
	}

	// Fetch comments
	comments, err := s.db.GetComments(issue.ID)
	if err != nil {
		comments = nil
	}

	// Fetch latest handoff
	handoff, _ := s.db.GetLatestHandoff(issue.ID)

	// Fetch dependencies (outgoing: what this issue depends on)
	depIDs, _ := s.db.GetDependencies(issue.ID)
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
	blockedByIDs, _ := s.db.GetBlockedBy(issue.ID)
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

	WriteSuccess(w, map[string]interface{}{
		"issue":          IssueToDTO(issue),
		"logs":           logsToDTOsNonNil(logs),
		"comments":       commentsToDTOsNonNil(comments),
		"latest_handoff": handoffDTO,
		"dependencies":   dependencies,
		"blocked_by":     blockedBy,
	}, http.StatusOK)
}

// ============================================================================
// GET /v1/sessions
// ============================================================================

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := session.ListSessions(s.db)
	if err != nil {
		WriteError(w, ErrInternal, "failed to list sessions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"sessions":           SessionsToDTOs(sessions),
		"current_session_id": s.sessionID,
	}, http.StatusOK)
}

// ============================================================================
// GET /v1/stats
// ============================================================================

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetExtendedStats()
	if err != nil {
		WriteError(w, ErrInternal, "failed to get stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	WriteSuccess(w, StatsToDTO(stats), http.StatusOK)
}

// ============================================================================
// GET /v1/boards
// ============================================================================

func (s *Server) handleListBoards(w http.ResponseWriter, r *http.Request) {
	boards, err := s.db.ListBoards()
	if err != nil {
		WriteError(w, ErrInternal, "failed to list boards: "+err.Error(), http.StatusInternalServerError)
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"boards": boardsToDTOsNonNil(boards),
	}, http.StatusOK)
}

// ============================================================================
// GET /v1/boards/{id}
// ============================================================================

func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		WriteError(w, ErrValidation, "board ID is required", http.StatusBadRequest)
		return
	}

	board, err := s.db.ResolveBoardRef(id)
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
		queryResults, err := query.Execute(s.db, board.Query, "", query.ExecuteOptions{})
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

		boardIssues, err = s.db.ApplyBoardPositions(board.ID, filtered)
		if err != nil {
			WriteError(w, ErrInternal, "failed to apply board positions: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// Empty query - use GetBoardIssues
		boardIssues, err = s.db.GetBoardIssues(board.ID, s.sessionID, statusFilter)
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

// tryTDQSearch attempts a TDQ search and returns issues or an error.
func (s *Server) tryTDQSearch(search, searchMode string, statuses []models.Status) ([]models.Issue, error) {
	issues, err := query.Execute(s.db, search, s.sessionID, query.ExecuteOptions{})
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
