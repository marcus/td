package db

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/marcus/td/internal/models"
)

// Lifecycle commands write an action_log row and then a log entry narrating
// it, usually within a quarter second (start takes a git snapshot in
// between, so up to a few seconds). That log is folded into the action's
// event as its detail instead of appearing as a separate event. The small
// negative slack only absorbs clock rounding between the two writes.
const (
	timelinePairBefore = 500 * time.Millisecond
	timelinePairAfter  = 5 * time.Second
)

// narratingLogTypes lists the actions whose command always writes its own
// log, and the log types it writes. Only these pair: update, delete, and
// restore write no log, so a note logged next to them is the user's own.
var narratingLogTypes = map[models.ActionType][]models.LogType{
	models.ActionStart:                  {models.LogTypeProgress},
	models.ActionReview:                 {models.LogTypeProgress},
	models.ActionApprove:                {models.LogTypeProgress, models.LogTypeSecurity},
	models.ActionReject:                 {models.LogTypeProgress, models.LogTypeSecurity},
	models.ActionClose:                  {models.LogTypeProgress, models.LogTypeSecurity},
	models.ActionCloseAfterReview:       {models.LogTypeProgress, models.LogTypeSecurity},
	models.ActionReviewApprove:          {models.LogTypeProgress, models.LogTypeSecurity},
	models.ActionReviewChangesRequested: {models.LogTypeProgress, models.LogTypeSecurity},
	models.ActionBlock:                  {models.LogTypeBlocker, models.LogTypeProgress},
	models.ActionUnblock:                {models.LogTypeProgress},
	models.ActionReopen:                 {models.LogTypeProgress},
}

// timelineAction is an action event plus the log types its command narrates
// itself with (none for actions that write no log).
type timelineAction struct {
	event    models.TimelineEvent
	logTypes []models.LogType
}

// commentSummaryLen is the longest comment, in runes, shown inline in the
// summary rather than as detail.
const commentSummaryLen = 80

// GetIssueTimeline returns a chronologically sorted timeline of an issue:
// lifecycle actions, field updates, logs, handoffs, comments, git snapshots,
// and linked files. If limit > 0, only the most recent limit events are
// returned. Event timestamps are UTC.
func (db *DB) GetIssueTimeline(issueID string, limit int) ([]models.TimelineEvent, error) {
	issue, err := db.GetIssue(issueID)
	if err != nil {
		return nil, err
	}
	issueID = issue.ID

	actions, err := db.getIssueActions(issueID)
	if err != nil {
		return nil, fmt.Errorf("query action log: %w", err)
	}
	var timelineActions []timelineAction
	hasCreate := false
	for _, a := range actions {
		ev, ok := issueActionToEvent(a)
		if !ok {
			continue
		}
		if ev.EventType == models.EventCreated {
			hasCreate = true
		}
		timelineActions = append(timelineActions, timelineAction{ev, narratingLogTypes[a.ActionType]})
	}
	// Issues created without an action_log row (imports, older data) still
	// get a starting point.
	if !hasCreate && !issue.CreatedAt.IsZero() {
		timelineActions = append([]timelineAction{{event: models.TimelineEvent{
			Timestamp: issue.CreatedAt,
			EventType: models.EventCreated,
			Summary:   createdSummary(string(issue.Type), string(issue.Priority)),
			SessionID: issue.CreatorSession,
		}}}, timelineActions...)
	}

	logs, err := db.GetLogs(issueID, 0)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}
	events := foldLogsIntoActions(timelineActions, logs)

	handoffs, err := db.GetHandoffs(issueID)
	if err != nil {
		return nil, fmt.Errorf("query handoffs: %w", err)
	}
	for _, h := range handoffs {
		events = append(events, models.TimelineEvent{
			Timestamp: h.Timestamp,
			EventType: models.EventHandoff,
			Summary:   "Handoff",
			Detail:    formatHandoffDetail(h),
			SessionID: h.SessionID,
		})
	}

	comments, err := db.GetComments(issueID)
	if err != nil {
		return nil, fmt.Errorf("query comments: %w", err)
	}
	for _, c := range comments {
		// A short one-line comment fits in the summary; anything longer is
		// shown once, in full, as the detail.
		ev := models.TimelineEvent{
			Timestamp: c.CreatedAt,
			EventType: models.EventComment,
			Summary:   "Comment",
			SessionID: c.SessionID,
		}
		text := strings.TrimSpace(c.Text)
		if utf8.RuneCountInString(text) <= commentSummaryLen && !strings.Contains(text, "\n") {
			ev.Summary += ": " + text
		} else {
			ev.Detail = text
		}
		events = append(events, ev)
	}

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

	files, err := db.GetLinkedFiles(issueID)
	if err != nil {
		return nil, fmt.Errorf("query linked files: %w", err)
	}
	for _, f := range files {
		events = append(events, models.TimelineEvent{
			Timestamp: f.LinkedAt,
			EventType: models.EventFileLink,
			Summary:   fmt.Sprintf("Linked file: %s (%s)", f.FilePath, f.Role),
		})
	}

	for i := range events {
		events[i].Timestamp = events[i].Timestamp.UTC()
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	events = collapseRepeats(events)

	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}

// collapseRepeats merges runs of identical consecutive events into one
// event carrying a Count.
func collapseRepeats(events []models.TimelineEvent) []models.TimelineEvent {
	var out []models.TimelineEvent
	for _, ev := range events {
		if n := len(out); n > 0 {
			last := &out[n-1]
			if last.EventType == ev.EventType && last.Summary == ev.Summary &&
				last.Detail == ev.Detail && last.SessionID == ev.SessionID {
				if last.Count == 0 {
					last.Count = 1
				}
				last.Count++
				continue
			}
		}
		out = append(out, ev)
	}
	return out
}

// foldLogsIntoActions attaches each narrating log to the action it narrates
// and returns all action events followed by the remaining logs as events of
// their own. A log pairs with an action from the same session, of a type that
// action's command writes, inside the pairing window; closest pairs are
// matched first, and each side pairs at most once. A paired log that only
// repeats the summary ("Approved" for "Approved and closed") is dropped, and
// a leading "<summary>: " is trimmed ("Rejected: reason" becomes "reason").
func foldLogsIntoActions(actions []timelineAction, logs []models.Log) []models.TimelineEvent {
	type candidate struct {
		action, log int
		gap         time.Duration
	}
	var candidates []candidate
	for ai, a := range actions {
		if len(a.logTypes) == 0 || a.event.SessionID == "" {
			continue
		}
		for li, l := range logs {
			if l.SessionID != a.event.SessionID || !slices.Contains(a.logTypes, l.Type) {
				continue
			}
			gap := l.Timestamp.Sub(a.event.Timestamp)
			if gap < -timelinePairBefore || gap > timelinePairAfter {
				continue
			}
			candidates = append(candidates, candidate{ai, li, gap.Abs()})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].gap < candidates[j].gap })

	events := make([]models.TimelineEvent, 0, len(actions)+len(logs))
	for _, a := range actions {
		events = append(events, a.event)
	}
	actionPaired := make([]bool, len(actions))
	logPaired := make([]bool, len(logs))
	for _, c := range candidates {
		if actionPaired[c.action] || logPaired[c.log] {
			continue
		}
		actionPaired[c.action], logPaired[c.log] = true, true
		events[c.action].Detail = narrationDetail(events[c.action].Summary, logs[c.log].Message)
	}

	for li, l := range logs {
		if logPaired[li] {
			continue
		}
		summary := l.Message
		if l.Type != "" && l.Type != models.LogTypeProgress {
			summary = fmt.Sprintf("[%s] %s", l.Type, l.Message)
		}
		events = append(events, models.TimelineEvent{
			Timestamp: l.Timestamp,
			EventType: models.EventLog,
			Summary:   summary,
			SessionID: l.SessionID,
		})
	}
	return events
}

// narrationDetail returns what a narrating log adds to its action's summary.
func narrationDetail(summary, msg string) string {
	msg = strings.TrimSpace(msg)
	lowerSummary, lowerMsg := strings.ToLower(summary), strings.ToLower(msg)
	if strings.HasPrefix(lowerSummary, lowerMsg) {
		return ""
	}
	if strings.HasPrefix(lowerMsg, lowerSummary+":") {
		return strings.TrimSpace(msg[len(summary)+1:])
	}
	return msg
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
	defer func() { _ = rows.Close() }()

	var snapshots []models.GitSnapshot
	for rows.Next() {
		var s models.GitSnapshot
		if err := rows.Scan(&s.ID, &s.IssueID, &s.Event, &s.CommitSHA, &s.Branch, &s.DirtyFiles, &s.Timestamp); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, rows.Err()
}

// getIssueActions returns the issue's own action_log rows, oldest first,
// excluding undone actions.
func (db *DB) getIssueActions(issueID string) ([]models.ActionLog, error) {
	rows, err := db.conn.Query(`
		SELECT CAST(id AS TEXT), session_id, action_type, entity_type, entity_id,
		       previous_data, new_data, timestamp
		FROM action_log
		WHERE entity_type = 'issue' AND entity_id = ? AND undone = 0
		ORDER BY timestamp
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var actions []models.ActionLog
	for rows.Next() {
		var a models.ActionLog
		if err := rows.Scan(&a.ID, &a.SessionID, &a.ActionType, &a.EntityType,
			&a.EntityID, &a.PreviousData, &a.NewData, &a.Timestamp); err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}
	return actions, rows.Err()
}

// actionSummaries maps lifecycle actions to their timeline event.
var actionSummaries = map[models.ActionType]struct {
	eventType models.EventType
	summary   string
}{
	models.ActionStart:                  {models.EventStatusChange, "Started work"},
	models.ActionReview:                 {models.EventStatusChange, "Submitted for review"},
	models.ActionApprove:                {models.EventStatusChange, "Approved and closed"},
	models.ActionCloseAfterReview:       {models.EventStatusChange, "Closed after review"},
	models.ActionReject:                 {models.EventStatusChange, "Rejected"},
	models.ActionBlock:                  {models.EventStatusChange, "Blocked"},
	models.ActionUnblock:                {models.EventStatusChange, "Unblocked"},
	models.ActionClose:                  {models.EventStatusChange, "Closed"},
	models.ActionReopen:                 {models.EventStatusChange, "Reopened"},
	models.ActionDelete:                 {models.EventStatusChange, "Deleted"},
	models.ActionRestore:                {models.EventStatusChange, "Restored"},
	models.ActionReviewApprove:          {models.EventReview, "Review recorded: approved"},
	models.ActionReviewChangesRequested: {models.EventReview, "Review recorded: changes requested"},
}

func issueActionToEvent(a models.ActionLog) (models.TimelineEvent, bool) {
	ev := models.TimelineEvent{Timestamp: a.Timestamp, SessionID: a.SessionID}
	switch a.ActionType {
	case models.ActionUpdate:
		return issueUpdateToEvent(a, ev)
	case models.ActionCreate:
		var created struct{ Type, Priority string }
		_ = json.Unmarshal([]byte(a.NewData), &created) // best effort: the summary just omits what it can't read
		ev.EventType, ev.Summary = models.EventCreated, createdSummary(created.Type, created.Priority)
		return ev, true
	}
	s, ok := actionSummaries[a.ActionType]
	if !ok {
		return ev, false
	}
	ev.EventType, ev.Summary = s.eventType, s.summary
	return ev, true
}

// createdSummary reads "Created task (P1)", dropping whatever is unknown.
func createdSummary(issueType, priority string) string {
	s := "Created"
	if issueType != "" {
		s += " " + issueType
	}
	if priority != "" {
		s += " (" + priority + ")"
	}
	return s
}

// timelineIgnoredFields change on every write and say nothing about the issue.
var timelineIgnoredFields = map[string]bool{"updated_at": true}

// issueUpdateToEvent describes an update by the fields it changed. An update
// that changed only bookkeeping fields produces no event.
func issueUpdateToEvent(a models.ActionLog, ev models.TimelineEvent) (models.TimelineEvent, bool) {
	var prev, next map[string]any
	if json.Unmarshal([]byte(a.PreviousData), &prev) != nil || json.Unmarshal([]byte(a.NewData), &next) != nil ||
		prev == nil || next == nil {
		ev.EventType, ev.Summary = models.EventUpdate, "Updated"
		return ev, true
	}

	// Union of both key sets: omitempty fields vanish from new_data when
	// they are cleared.
	var keys []string
	seen := map[string]bool{}
	for _, m := range []map[string]any{prev, next} {
		for k := range m {
			if seen[k] || timelineIgnoredFields[k] {
				continue
			}
			seen[k] = true
			if !reflect.DeepEqual(prev[k], next[k]) {
				keys = append(keys, k)
			}
		}
	}
	if len(keys) == 0 {
		return ev, false
	}
	sort.Strings(keys)

	var changes []string
	statusChanged := false
	for _, k := range keys {
		if k == "status" {
			statusChanged = true
		}
		changes = append(changes, describeFieldChange(k, prev[k], next[k]))
	}
	ev.EventType = models.EventUpdate
	if statusChanged {
		ev.EventType = models.EventStatusChange
	}
	ev.Summary = "Updated " + strings.Join(changes, ", ")
	return ev, true
}

// describeFieldChange shows short scalar changes as "field: old → new" and
// anything longer by name only.
func describeFieldChange(field string, oldVal, newVal any) string {
	oldS, okOld := shortScalar(oldVal)
	newS, okNew := shortScalar(newVal)
	if !okOld || !okNew {
		return field
	}
	return fmt.Sprintf("%s: %s → %s", field, oldS, newS)
}

func shortScalar(v any) (string, bool) {
	var s string
	switch x := v.(type) {
	case nil:
		s = "(none)"
	case string:
		s = x
		if s == "" {
			s = "(none)"
		}
	case bool, float64:
		s = fmt.Sprint(x)
	default:
		return "", false
	}
	if utf8.RuneCountInString(s) > 40 || strings.Contains(s, "\n") {
		return "", false
	}
	return s, true
}

func formatHandoffDetail(h models.Handoff) string {
	var parts []string
	for _, section := range []struct {
		label string
		items []string
	}{
		{"Done", h.Done},
		{"Remaining", h.Remaining},
		{"Decisions", h.Decisions},
		{"Uncertain", h.Uncertain},
	} {
		if len(section.items) > 0 {
			parts = append(parts, section.label+": "+strings.Join(section.items, "; "))
		}
	}
	return strings.Join(parts, "\n")
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
