package db

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/marcus/td/internal/models"
)

// GetIssueTimeline returns a chronologically sorted timeline of all events
// for a given issue: logs, handoffs, comments, git snapshots, and status
// changes from the action_log.
func (db *DB) GetIssueTimeline(issueID string, limit int) ([]models.TimelineEvent, error) {
	var events []models.TimelineEvent

	// 1. Logs
	logs, err := db.GetLogs(issueID, 0)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}
	for _, l := range logs {
		events = append(events, models.TimelineEvent{
			Timestamp: l.Timestamp,
			EventType: models.EventLog,
			Summary:   fmt.Sprintf("[%s] %s", l.Type, l.Message),
			SessionID: l.SessionID,
		})
	}

	// 2. Handoffs
	handoffs, err := db.getHandoffsForIssue(issueID)
	if err != nil {
		return nil, fmt.Errorf("query handoffs: %w", err)
	}
	for _, h := range handoffs {
		detail := formatHandoffDetail(h)
		events = append(events, models.TimelineEvent{
			Timestamp: h.Timestamp,
			EventType: models.EventHandoff,
			Summary:   "Handoff recorded",
			Detail:    detail,
			SessionID: h.SessionID,
		})
	}

	// 3. Comments
	comments, err := db.GetComments(issueID)
	if err != nil {
		return nil, fmt.Errorf("query comments: %w", err)
	}
	for _, c := range comments {
		summary := c.Text
		if len(summary) > 80 {
			summary = summary[:77] + "..."
		}
		events = append(events, models.TimelineEvent{
			Timestamp: c.CreatedAt,
			EventType: models.EventComment,
			Summary:   summary,
			Detail:    c.Text,
			SessionID: c.SessionID,
		})
	}

	// 4. Git snapshots
	snapshots, err := db.getGitSnapshots(issueID)
	if err != nil {
		return nil, fmt.Errorf("query git snapshots: %w", err)
	}
	for _, s := range snapshots {
		summary := fmt.Sprintf("Git %s: %s on %s", s.Event, shortSHA(s.CommitSHA), s.Branch)
		if s.DirtyFiles > 0 {
			summary += fmt.Sprintf(" (%d dirty)", s.DirtyFiles)
		}
		events = append(events, models.TimelineEvent{
			Timestamp: s.Timestamp,
			EventType: models.EventGitSnapshot,
			Summary:   summary,
		})
	}

	// 5. Action log entries for this issue (status changes, file links, etc.)
	actions, err := db.getActionLogForEntity(issueID)
	if err != nil {
		return nil, fmt.Errorf("query action log: %w", err)
	}
	for _, a := range actions {
		ev := actionToTimelineEvent(a)
		if ev != nil {
			events = append(events, *ev)
		}
	}

	// Sort chronologically
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}

	return events, nil
}

// getHandoffsForIssue returns all handoffs for an issue, chronologically.
func (db *DB) getHandoffsForIssue(issueID string) ([]models.Handoff, error) {
	rows, err := db.conn.Query(`
		SELECT CAST(id AS TEXT), issue_id, session_id, done, remaining, decisions, uncertain, timestamp
		FROM handoffs WHERE issue_id = ? ORDER BY timestamp
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var handoffs []models.Handoff
	for rows.Next() {
		var h models.Handoff
		var doneJSON, remainingJSON, decisionsJSON, uncertainJSON string
		if err := rows.Scan(&h.ID, &h.IssueID, &h.SessionID,
			&doneJSON, &remainingJSON, &decisionsJSON, &uncertainJSON, &h.Timestamp); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(doneJSON), &h.Done)
		json.Unmarshal([]byte(remainingJSON), &h.Remaining)
		json.Unmarshal([]byte(decisionsJSON), &h.Decisions)
		json.Unmarshal([]byte(uncertainJSON), &h.Uncertain)
		handoffs = append(handoffs, h)
	}
	return handoffs, nil
}

// getGitSnapshots returns all git snapshots for an issue.
func (db *DB) getGitSnapshots(issueID string) ([]models.GitSnapshot, error) {
	rows, err := db.conn.Query(`
		SELECT CAST(id AS TEXT), issue_id, event, commit_sha, branch, dirty_files, timestamp
		FROM git_snapshots WHERE issue_id = ? ORDER BY timestamp
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []models.GitSnapshot
	for rows.Next() {
		var s models.GitSnapshot
		if err := rows.Scan(&s.ID, &s.IssueID, &s.Event, &s.CommitSHA, &s.Branch, &s.DirtyFiles, &s.Timestamp); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, nil
}

// getActionLogForEntity returns action_log rows where entity_id matches the issue
// or entity_type is 'issue' with matching entity_id.
func (db *DB) getActionLogForEntity(issueID string) ([]models.ActionLog, error) {
	rows, err := db.conn.Query(`
		SELECT CAST(id AS TEXT), session_id, action_type, entity_type, entity_id,
		       previous_data, new_data, timestamp, undone
		FROM action_log
		WHERE entity_id = ? AND entity_type IN ('issue', 'issue_files')
		ORDER BY timestamp
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []models.ActionLog
	for rows.Next() {
		var a models.ActionLog
		var undone int
		if err := rows.Scan(&a.ID, &a.SessionID, &a.ActionType, &a.EntityType,
			&a.EntityID, &a.PreviousData, &a.NewData, &a.Timestamp, &undone); err != nil {
			return nil, err
		}
		a.Undone = undone == 1
		actions = append(actions, a)
	}
	return actions, nil
}

func actionToTimelineEvent(a models.ActionLog) *models.TimelineEvent {
	if a.Undone {
		return nil
	}

	switch a.EntityType {
	case "issue":
		return issueActionToEvent(a)
	case "issue_files":
		return fileActionToEvent(a)
	default:
		return nil
	}
}

func issueActionToEvent(a models.ActionLog) *models.TimelineEvent {
	switch models.ActionType(a.ActionType) {
	case models.ActionCreate:
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   "Issue created",
			SessionID: a.SessionID,
		}
	case models.ActionStart:
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   "Status changed to in_progress",
			SessionID: a.SessionID,
		}
	case models.ActionReview:
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   "Submitted for review",
			SessionID: a.SessionID,
		}
	case models.ActionApprove:
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   "Approved and closed",
			SessionID: a.SessionID,
		}
	case models.ActionReject:
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   "Rejected (needs rework)",
			SessionID: a.SessionID,
		}
	case models.ActionBlock:
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   "Blocked",
			SessionID: a.SessionID,
		}
	case models.ActionUnblock:
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   "Unblocked",
			SessionID: a.SessionID,
		}
	case models.ActionClose:
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   "Closed",
			SessionID: a.SessionID,
		}
	case models.ActionReopen:
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   "Reopened",
			SessionID: a.SessionID,
		}
	case models.ActionUpdate:
		return issueUpdateToEvent(a)
	default:
		return nil
	}
}

func issueUpdateToEvent(a models.ActionLog) *models.TimelineEvent {
	// Try to extract what changed from previous_data vs new_data
	var prev, next map[string]interface{}
	json.Unmarshal([]byte(a.PreviousData), &prev)
	json.Unmarshal([]byte(a.NewData), &next)

	if prev == nil || next == nil {
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   "Issue updated",
			SessionID: a.SessionID,
		}
	}

	// Check for status change specifically
	oldStatus, _ := prev["status"].(string)
	newStatus, _ := next["status"].(string)
	if oldStatus != "" && newStatus != "" && oldStatus != newStatus {
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   fmt.Sprintf("Status: %s → %s", oldStatus, newStatus),
			SessionID: a.SessionID,
		}
	}

	// Generic update - summarize changed fields
	var changed []string
	for key, newVal := range next {
		if oldVal, ok := prev[key]; ok && fmt.Sprintf("%v", oldVal) != fmt.Sprintf("%v", newVal) {
			changed = append(changed, key)
		}
	}
	if len(changed) > 0 {
		return &models.TimelineEvent{
			Timestamp: a.Timestamp,
			EventType: models.EventStatusChange,
			Summary:   fmt.Sprintf("Updated: %s", strings.Join(changed, ", ")),
			SessionID: a.SessionID,
		}
	}

	return nil
}

func fileActionToEvent(a models.ActionLog) *models.TimelineEvent {
	var data map[string]interface{}
	if a.NewData != "" {
		json.Unmarshal([]byte(a.NewData), &data)
	} else {
		json.Unmarshal([]byte(a.PreviousData), &data)
	}

	filePath, _ := data["file_path"].(string)
	if filePath == "" {
		filePath = "unknown"
	}

	action := "Linked"
	if models.ActionType(a.ActionType) == models.ActionUnlinkFile {
		action = "Unlinked"
	}

	return &models.TimelineEvent{
		Timestamp: a.Timestamp,
		EventType: models.EventFileLink,
		Summary:   fmt.Sprintf("%s file: %s", action, filePath),
		SessionID: a.SessionID,
	}
}

func formatHandoffDetail(h models.Handoff) string {
	var parts []string
	if len(h.Done) > 0 {
		parts = append(parts, fmt.Sprintf("Done: %s", strings.Join(h.Done, "; ")))
	}
	if len(h.Remaining) > 0 {
		parts = append(parts, fmt.Sprintf("Remaining: %s", strings.Join(h.Remaining, "; ")))
	}
	if len(h.Decisions) > 0 {
		parts = append(parts, fmt.Sprintf("Decisions: %s", strings.Join(h.Decisions, "; ")))
	}
	if len(h.Uncertain) > 0 {
		parts = append(parts, fmt.Sprintf("Uncertain: %s", strings.Join(h.Uncertain, "; ")))
	}
	return strings.Join(parts, "\n")
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
