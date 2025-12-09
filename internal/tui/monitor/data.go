package monitor

import (
	"sort"
	"time"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// FetchData retrieves all data needed for the monitor display
func FetchData(database *db.DB, sessionID string) RefreshDataMsg {
	msg := RefreshDataMsg{
		Timestamp: time.Now(),
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

	// Get task list
	msg.TaskList = fetchTaskList(database, sessionID)

	return msg
}

// fetchActivity combines logs, actions, and comments into a unified activity feed
func fetchActivity(database *db.DB, limit int) []ActivityItem {
	var items []ActivityItem

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

	return items
}

// fetchTaskList retrieves categorized issues for the task list panel
func fetchTaskList(database *db.DB, sessionID string) TaskListData {
	var data TaskListData

	// Ready issues: open status, not blocked, sorted by priority
	openIssues, _ := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusOpen},
		SortBy: "priority",
	})

	// Filter out blocked issues (those with unresolved dependencies)
	for _, issue := range openIssues {
		deps, _ := database.GetDependencies(issue.ID)
		isBlocked := false
		for _, depID := range deps {
			depIssue, err := database.GetIssue(depID)
			if err == nil && depIssue.Status != models.StatusClosed {
				isBlocked = true
				break
			}
		}
		if !isBlocked {
			data.Ready = append(data.Ready, issue)
		}
	}

	// Reviewable issues: in_review status, different implementer than current session
	reviewable, _ := database.ListIssues(db.ListIssuesOptions{
		ReviewableBy: sessionID,
		SortBy:       "priority",
	})
	data.Reviewable = reviewable

	// Blocked issues
	blocked, _ := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusBlocked},
		SortBy: "priority",
	})
	data.Blocked = blocked

	return data
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
