package models

import "time"

// EventType categorizes timeline events
type EventType string

const (
	EventCreated      EventType = "created"
	EventStatusChange EventType = "status_change"
	EventUpdate       EventType = "update"
	EventReview       EventType = "review"
	EventLog          EventType = "log"
	EventHandoff      EventType = "handoff"
	EventGitSnapshot  EventType = "git_snapshot"
	EventComment      EventType = "comment"
	EventFileLink     EventType = "file_link"
)

// TimelineEvent represents a single event in an issue's history.
// Timestamps are always UTC. Count is set when a run of identical
// consecutive events (e.g. a retry loop posting the same comment) was
// collapsed into this one; Timestamp is then the first occurrence.
type TimelineEvent struct {
	Timestamp time.Time `json:"timestamp"`
	EventType EventType `json:"event_type"`
	Summary   string    `json:"summary"`
	Detail    string    `json:"detail,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Count     int       `json:"count,omitempty"`
}
