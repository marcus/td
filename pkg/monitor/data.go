package monitor

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/query"
	"github.com/marcus/td/internal/session"
)

// StatsData holds statistics for the stats modal
type StatsData struct {
	ExtendedStats *models.ExtendedStats
	Error         error
}

// StatsDataMsg carries fetched stats data
type StatsDataMsg struct {
	Data  *StatsData
	Error error
}

// FetchData retrieves all data needed for the monitor display
func FetchData(database *db.DB, sessionID string, startedAt time.Time, searchQuery string, includeClosed bool, sortMode SortMode) RefreshDataMsg {
	msg := RefreshDataMsg{
		Timestamp: time.Now(),
	}

	// Auto-detect current session for reviewable calculation
	// This allows the monitor to see reviewable issues when a new session starts
	currentSessionID := sessionID
	if sess, err := session.GetOrCreate(database); err == nil {
		currentSessionID = sess.ID
	}

	// Get focused issue
	focusedID, _ := config.GetFocus(database.BaseDir())
	if focusedID != "" {
		if issue, err := database.GetIssue(focusedID); err == nil {
			msg.FocusedIssue = issue
		}
	}

	// Get in-progress issues
	inProgress, _ := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusInProgress},
		SortBy: "priority",
	})
	msg.InProgress = inProgress

	// Get activity feed
	msg.Activity = fetchActivity(database, 50)

	// Get task list (uses current session for reviewable calculation)
	msg.TaskList = fetchTaskList(database, currentSessionID, searchQuery, includeClosed, sortMode)

	// Get recent handoffs since monitor started
	msg.RecentHandoffs = fetchRecentHandoffs(database, startedAt)

	// Get active sessions (activity in last 5 minutes)
	msg.ActiveSessions = fetchActiveSessions(database)

	return msg
}

// fetchActivity combines logs, actions, and comments into a unified activity feed
func fetchActivity(database *db.DB, limit int) []ActivityItem {
	// Pre-allocate for logs + actions + comments (3x limit max)
	items := make([]ActivityItem, 0, limit*3)

	// Fetch logs
	logs, _ := database.GetRecentLogsAll(limit)
	for _, log := range logs {
		items = append(items, ActivityItem{
			Timestamp: log.Timestamp,
			SessionID: log.SessionID,
			Type:      "log",
			IssueID:   log.IssueID,
			Message:   log.Message,
			LogType:   log.Type,
		})
	}

	// Fetch actions
	actions, _ := database.GetRecentActionsAll(limit)
	for _, action := range actions {
		items = append(items, ActivityItem{
			Timestamp: action.Timestamp,
			SessionID: action.SessionID,
			Type:      "action",
			IssueID:   action.EntityID,
			Message:   formatActionMessage(action),
			Action:    action.ActionType,
		})
	}

	// Fetch comments
	comments, _ := database.GetRecentCommentsAll(limit)
	for _, comment := range comments {
		items = append(items, ActivityItem{
			Timestamp: comment.CreatedAt,
			SessionID: comment.SessionID,
			Type:      "comment",
			IssueID:   comment.IssueID,
			Message:   comment.Text,
		})
	}

	// Sort by timestamp descending
	sort.Slice(items, func(i, j int) bool {
		return items[i].Timestamp.After(items[j].Timestamp)
	})

	// Limit total items
	if len(items) > limit {
		items = items[:limit]
	}

	// Collect unique issue IDs
	issueIDs := make([]string, 0, len(items))
	seen := make(map[string]bool)
	for _, item := range items {
		if item.IssueID != "" && !seen[item.IssueID] {
			seen[item.IssueID] = true
			issueIDs = append(issueIDs, item.IssueID)
		}
	}

	// Batch fetch all titles in single query
	titles, _ := database.GetIssueTitles(issueIDs)

	// Apply titles to items
	for i := range items {
		if items[i].IssueID != "" {
			items[i].IssueTitle = titles[items[i].IssueID]
		}
	}

	return items
}

// isTDQQuery checks if the query uses TDQ syntax (operators, functions, etc.)
func isTDQQuery(q string) bool {
	// Check for TDQ operators and patterns (with spaces)
	tdqPatterns := []string{
		" = ", " != ", " ~ ", " !~ ",
		" < ", " > ", " <= ", " >= ",
		" AND ", " OR ", "NOT ",
		"has(", "is(", "any(", "blocks(", "blocked_by(", "descendant_of(",
		"log.", "comment.", "handoff.", "file.",
		"@me", "EMPTY",
		"sort:", // Sort prefix is considered TDQ
	}
	upper := strings.ToUpper(q)
	for _, pattern := range tdqPatterns {
		if strings.Contains(upper, strings.ToUpper(pattern)) {
			return true
		}
	}

	// Check for spaceless field=value patterns (e.g., type=epic, status!=open)
	spacelessPattern := regexp.MustCompile(`\w+([=!<>~]=?|!~)\w`)
	if spacelessPattern.MatchString(q) {
		return true
	}

	return false
}

// fetchTaskList retrieves categorized issues for the task list panel
func fetchTaskList(database *db.DB, sessionID string, searchQuery string, includeClosed bool, sortMode SortMode) TaskListData {
	var data TaskListData

	// Get default sort from SortMode (used for non-TDQ queries)
	sortBy, sortDesc := sortMode.ToDBOptions()

	// Helper to extract issues from ranked results
	extractIssues := func(results []db.SearchResult) []models.Issue {
		issues := make([]models.Issue, len(results))
		for i, r := range results {
			issues[i] = r.Issue
		}
		return issues
	}

	// Batch load all dependencies and their statuses upfront
	allDeps, _ := database.GetAllDependencies()
	var allDepIDs []string
	for _, deps := range allDeps {
		allDepIDs = append(allDepIDs, deps...)
	}
	depStatuses, _ := database.GetIssueStatuses(allDepIDs)

	// Get rejected in_progress issue IDs for "needs rework" detection
	rejectedIDs, err := database.GetRejectedInProgressIssueIDs()
	if err != nil {
		rejectedIDs = make(map[string]bool) // Safe fallback on error
	}

	// Helper to check if issue is blocked by unclosed dependencies
	isBlockedByDeps := func(issueID string) bool {
		deps := allDeps[issueID]
		for _, depID := range deps {
			if status, ok := depStatuses[depID]; ok && status != models.StatusClosed {
				return true
			}
		}
		return false
	}

	// Check if this is a TDQ query
	useTDQ := searchQuery != "" && isTDQQuery(searchQuery)

	if useTDQ {
		// Use TDQ to filter issues across all categories
		allIssues, err := query.Execute(database, searchQuery, sessionID, query.ExecuteOptions{})
		if err != nil {
			// Fall back to simple search on TDQ parse error
			useTDQ = false
		} else {
			// Categorize the TDQ results
			for _, issue := range allIssues {
				switch issue.Status {
				case models.StatusOpen:
					if isBlockedByDeps(issue.ID) {
						data.Blocked = append(data.Blocked, issue)
					} else {
						data.Ready = append(data.Ready, issue)
					}
				case models.StatusInProgress:
					// In-progress issues: check if needs rework (rejected without re-submission)
					if rejectedIDs[issue.ID] {
						data.NeedsRework = append(data.NeedsRework, issue)
					} else {
						data.Ready = append(data.Ready, issue)
					}
				case models.StatusBlocked:
					data.Blocked = append(data.Blocked, issue)
				case models.StatusInReview:
					if issue.ImplementerSession != sessionID {
						data.Reviewable = append(data.Reviewable, issue)
					}
				case models.StatusClosed:
					if includeClosed {
						data.Closed = append(data.Closed, issue)
					}
				}
			}
			return data
		}
	}

	// Standard search (simple text or when TDQ fails)
	// Ready issues: open status, not blocked, sorted by priority
	var openIssues []models.Issue
	if searchQuery != "" && !useTDQ {
		results, _ := database.SearchIssuesRanked(searchQuery, db.ListIssuesOptions{
			Status: []models.Status{models.StatusOpen},
		})
		openIssues = extractIssues(results)
	} else if searchQuery == "" {
		openIssues, _ = database.ListIssues(db.ListIssuesOptions{
			Status:   []models.Status{models.StatusOpen},
			SortBy:   sortBy,
			SortDesc: sortDesc,
		})
	}

	// Separate open issues into ready vs blocked-by-dependency
	var blockedByDep []models.Issue
	for _, issue := range openIssues {
		if isBlockedByDeps(issue.ID) {
			blockedByDep = append(blockedByDep, issue)
		} else {
			data.Ready = append(data.Ready, issue)
		}
	}

	// In-progress issues: categorize as Ready or NeedsRework
	var inProgressIssues []models.Issue
	if searchQuery != "" && !useTDQ {
		results, _ := database.SearchIssuesRanked(searchQuery, db.ListIssuesOptions{
			Status: []models.Status{models.StatusInProgress},
		})
		inProgressIssues = extractIssues(results)
	} else if searchQuery == "" {
		inProgressIssues, _ = database.ListIssues(db.ListIssuesOptions{
			Status:   []models.Status{models.StatusInProgress},
			SortBy:   sortBy,
			SortDesc: sortDesc,
		})
	}
	for _, issue := range inProgressIssues {
		if rejectedIDs[issue.ID] {
			data.NeedsRework = append(data.NeedsRework, issue)
		} else {
			data.Ready = append(data.Ready, issue)
		}
	}

	// Reviewable issues: in_review status, different implementer than current session
	if searchQuery != "" && !useTDQ {
		results, _ := database.SearchIssuesRanked(searchQuery, db.ListIssuesOptions{
			ReviewableBy: sessionID,
		})
		data.Reviewable = extractIssues(results)
	} else if searchQuery == "" {
		data.Reviewable, _ = database.ListIssues(db.ListIssuesOptions{
			ReviewableBy: sessionID,
			SortBy:       sortBy,
			SortDesc:     sortDesc,
		})
	}

	// Blocked issues: explicit blocked status + issues blocked by dependencies
	if searchQuery != "" && !useTDQ {
		results, _ := database.SearchIssuesRanked(searchQuery, db.ListIssuesOptions{
			Status: []models.Status{models.StatusBlocked},
		})
		data.Blocked = append(extractIssues(results), blockedByDep...)
	} else if searchQuery == "" {
		blocked, _ := database.ListIssues(db.ListIssuesOptions{
			Status:   []models.Status{models.StatusBlocked},
			SortBy:   sortBy,
			SortDesc: sortDesc,
		})
		data.Blocked = append(blocked, blockedByDep...)
	} else {
		data.Blocked = blockedByDep
	}

	// Closed issues (if toggle enabled)
	if includeClosed {
		if searchQuery != "" && !useTDQ {
			results, _ := database.SearchIssuesRanked(searchQuery, db.ListIssuesOptions{
				Status: []models.Status{models.StatusClosed},
			})
			data.Closed = extractIssues(results)
		} else if searchQuery == "" {
			data.Closed, _ = database.ListIssues(db.ListIssuesOptions{
				Status:   []models.Status{models.StatusClosed},
				SortBy:   sortBy,
				SortDesc: sortDesc,
			})
		}
	}

	return data
}

// fetchActiveSessions retrieves sessions with activity in the last 5 minutes
func fetchActiveSessions(database *db.DB) []string {
	since := time.Now().Add(-5 * time.Minute)
	sessions, err := database.GetActiveSessions(since)
	if err != nil {
		return nil
	}
	return sessions
}

// fetchRecentHandoffs retrieves handoffs since the given time
func fetchRecentHandoffs(database *db.DB, since time.Time) []RecentHandoff {
	var result []RecentHandoff

	handoffs, err := database.GetRecentHandoffs(10, since)
	if err != nil {
		return result
	}

	for _, h := range handoffs {
		result = append(result, RecentHandoff{
			IssueID:   h.IssueID,
			SessionID: h.SessionID,
			Timestamp: h.Timestamp,
		})
	}

	return result
}

// formatActionMessage creates a human-readable message for an action
func formatActionMessage(action models.ActionLog) string {
	switch action.ActionType {
	case models.ActionCreate:
		return "created issue"
	case models.ActionUpdate:
		return "updated issue"
	case models.ActionDelete:
		return "deleted issue"
	case models.ActionRestore:
		return "restored issue"
	case models.ActionStart:
		return "started work"
	case models.ActionReview:
		return "submitted for review"
	case models.ActionApprove:
		return "approved"
	case models.ActionReject:
		return "rejected"
	case models.ActionBlock:
		return "marked as blocked"
	case models.ActionUnblock:
		return "unblocked"
	case models.ActionClose:
		return "closed"
	case models.ActionReopen:
		return "reopened"
	case models.ActionAddDep:
		return "added dependency"
	case models.ActionRemoveDep:
		return "removed dependency"
	case models.ActionLinkFile:
		return "linked file"
	case models.ActionUnlinkFile:
		return "unlinked file"
	default:
		return string(action.ActionType)
	}
}

// FetchStats retrieves extended statistics for the stats modal
func FetchStats(database *db.DB) StatsDataMsg {
	stats, err := database.GetExtendedStats()
	if err != nil {
		return StatsDataMsg{
			Data:  &StatsData{Error: err},
			Error: err,
		}
	}
	return StatsDataMsg{
		Data: &StatsData{ExtendedStats: stats},
	}
}

// ComputeBoardIssueCategories sets the Category field on each BoardIssueView.
// This is the single source of truth for issue categorization, considering
// dependency blocking, rejection status, and reviewability.
// If precomputedRejectedIDs is non-nil, it's used instead of querying the DB.
func ComputeBoardIssueCategories(database *db.DB, issues []models.BoardIssueView, sessionID string, precomputedRejectedIDs map[string]bool) {
	if len(issues) == 0 {
		return
	}

	// Use pre-computed rejected IDs if available, otherwise query
	rejectedIDs := precomputedRejectedIDs
	if rejectedIDs == nil {
		var err error
		rejectedIDs, err = database.GetRejectedInProgressIssueIDs()
		if err != nil {
			rejectedIDs = make(map[string]bool)
		}
	}

	// Batch load all dependencies and their statuses
	allDeps, _ := database.GetAllDependencies()
	var allDepIDs []string
	for _, deps := range allDeps {
		allDepIDs = append(allDepIDs, deps...)
	}
	depStatuses, _ := database.GetIssueStatuses(allDepIDs)

	// Helper to check if issue is blocked by unclosed dependencies
	isBlockedByDeps := func(issueID string) bool {
		deps := allDeps[issueID]
		for _, depID := range deps {
			if status, ok := depStatuses[depID]; ok && status != models.StatusClosed {
				return true
			}
		}
		return false
	}

	// Set category on each issue
	for i := range issues {
		issue := &issues[i].Issue
		var category TaskListCategory

		switch issue.Status {
		case models.StatusOpen:
			if isBlockedByDeps(issue.ID) {
				category = CategoryBlocked
			} else {
				category = CategoryReady
			}
		case models.StatusInProgress:
			if rejectedIDs[issue.ID] {
				category = CategoryNeedsRework
			} else {
				category = CategoryReady
			}
		case models.StatusBlocked:
			category = CategoryBlocked
		case models.StatusInReview:
			if issue.ImplementerSession != sessionID {
				category = CategoryReviewable
			} else {
				category = CategoryReady // Own issues in review show as ready
			}
		case models.StatusClosed:
			category = CategoryClosed
		default:
			category = CategoryReady
		}

		issues[i].Category = string(category)
	}
}

// CategorizeBoardIssues takes board issues and groups them by status category
// for the swimlanes view. Issues are sorted within each category respecting
// backlog positions: positioned issues first (by position), then unpositioned
// (by sortMode). Also sets Category on each BoardIssueView.
// If rejectedIDs is non-nil, it's passed through to avoid a synchronous DB query.
func CategorizeBoardIssues(database *db.DB, issues []models.BoardIssueView, sessionID string, sortMode SortMode, rejectedIDs map[string]bool) TaskListData {
	var data TaskListData

	if len(issues) == 0 {
		return data
	}

	// Compute categories (sets Category field on each issue)
	ComputeBoardIssueCategories(database, issues, sessionID, rejectedIDs)

	// Group by category (preserve BoardIssueView for position-aware sorting)
	categories := map[TaskListCategory][]models.BoardIssueView{
		CategoryReady:       {},
		CategoryNeedsRework: {},
		CategoryBlocked:     {},
		CategoryReviewable:  {},
		CategoryClosed:      {},
	}
	for _, biv := range issues {
		cat := TaskListCategory(biv.Category)
		categories[cat] = append(categories[cat], biv)
	}

	// Sort each category with position awareness
	sortFunc := getSortFuncWithPosition(sortMode)
	for cat := range categories {
		sort.Slice(categories[cat], sortFunc(categories[cat]))
	}

	// Extract Issues into TaskListData
	for _, biv := range categories[CategoryReady] {
		data.Ready = append(data.Ready, biv.Issue)
	}
	for _, biv := range categories[CategoryReviewable] {
		data.Reviewable = append(data.Reviewable, biv.Issue)
	}
	for _, biv := range categories[CategoryNeedsRework] {
		data.NeedsRework = append(data.NeedsRework, biv.Issue)
	}
	for _, biv := range categories[CategoryBlocked] {
		data.Blocked = append(data.Blocked, biv.Issue)
	}
	for _, biv := range categories[CategoryClosed] {
		data.Closed = append(data.Closed, biv.Issue)
	}

	return data
}

// filterBoardIssuesByQuery filters BoardIssueView slices by search query.
// Matches against issue ID, title, and type (case-insensitive).
// Sort clauses (sort:xxx) are stripped. Type filters (type=xxx) are applied.
func filterBoardIssuesByQuery(issues []models.BoardIssueView, query string) []models.BoardIssueView {
	if query == "" {
		return issues
	}

	words := strings.Fields(query)
	var searchTerms []string
	var typeFilter string

	for _, word := range words {
		lower := strings.ToLower(word)
		if t, found := strings.CutPrefix(lower, "type="); found {
			typeFilter = t
		} else if !strings.HasPrefix(lower, "sort:") {
			searchTerms = append(searchTerms, word)
		}
	}

	// Apply type filter first
	var filtered []models.BoardIssueView
	if typeFilter != "" {
		for _, biv := range issues {
			if strings.EqualFold(string(biv.Issue.Type), typeFilter) {
				filtered = append(filtered, biv)
			}
		}
	} else {
		filtered = issues
	}

	// Return if no text search needed
	if len(searchTerms) == 0 {
		return filtered
	}

	// Apply text search on filtered results
	query = strings.ToLower(strings.Join(searchTerms, " "))
	var result []models.BoardIssueView
	for _, biv := range filtered {
		if strings.Contains(strings.ToLower(biv.Issue.ID), query) ||
			strings.Contains(strings.ToLower(biv.Issue.Title), query) ||
			strings.Contains(strings.ToLower(string(biv.Issue.Type)), query) {
			result = append(result, biv)
		}
	}
	return result
}

// getSortFuncWithPosition returns a sort function that respects backlog positions.
// Positioned issues come first (by position ASC), then unpositioned (by sortMode).
func getSortFuncWithPosition(sortMode SortMode) func(issues []models.BoardIssueView) func(i, j int) bool {
	return func(issues []models.BoardIssueView) func(i, j int) bool {
		return func(i, j int) bool {
			// Positioned issues come before unpositioned
			if issues[i].HasPosition && !issues[j].HasPosition {
				return true
			}
			if !issues[i].HasPosition && issues[j].HasPosition {
				return false
			}
			// Both positioned: sort by position ASC
			if issues[i].HasPosition && issues[j].HasPosition {
				return issues[i].Position < issues[j].Position
			}
			// Both unpositioned: use SortMode
			switch sortMode {
			case SortByCreatedDesc:
				return issues[i].Issue.CreatedAt.After(issues[j].Issue.CreatedAt)
			case SortByUpdatedDesc:
				return issues[i].Issue.UpdatedAt.After(issues[j].Issue.UpdatedAt)
			default: // SortByPriority
				if issues[i].Issue.Priority != issues[j].Issue.Priority {
					return issues[i].Issue.Priority < issues[j].Issue.Priority
				}
				return issues[i].Issue.UpdatedAt.After(issues[j].Issue.UpdatedAt)
			}
		}
	}
}

// BuildSwimlaneRows flattens categorized TaskListData into TaskListRow slice
// for cursor navigation in swimlanes view
func BuildSwimlaneRows(data TaskListData) []TaskListRow {
	var rows []TaskListRow

	// Add reviewable issues
	for _, issue := range data.Reviewable {
		rows = append(rows, TaskListRow{Issue: issue, Category: CategoryReviewable})
	}

	// Add needs rework issues
	for _, issue := range data.NeedsRework {
		rows = append(rows, TaskListRow{Issue: issue, Category: CategoryNeedsRework})
	}

	// Add ready issues
	for _, issue := range data.Ready {
		rows = append(rows, TaskListRow{Issue: issue, Category: CategoryReady})
	}

	// Add blocked issues
	for _, issue := range data.Blocked {
		rows = append(rows, TaskListRow{Issue: issue, Category: CategoryBlocked})
	}

	// Add closed issues
	for _, issue := range data.Closed {
		rows = append(rows, TaskListRow{Issue: issue, Category: CategoryClosed})
	}

	return rows
}
