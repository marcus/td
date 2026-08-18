package models

import "time"

// EventType categorizes timeline events
type EventType string

const (
	EventStatusChange EventType = "status_change"
	EventLog          EventType = "log"
	EventHandoff      EventType = "handoff"
	EventGitSnapshot  EventType = "git_snapshot"
	EventComment      EventType = "comment"
	EventFileLink     EventType = "file_link"
)

// TimelineEvent represents a single event in an issue's history
type TimelineEvent struct {
	Timestamp time.Time `json:"timestamp"`
	EventType EventType `json:"event_type"`
	Summary   string    `json:"summary"`
	Detail    string    `json:"detail,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
}
