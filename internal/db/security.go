package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const securityEventsFile = ".todos/security_events.jsonl"

// SecurityEvent represents a security-relevant workflow exception
// (for example, self-close or creator-approval exceptions).
type SecurityEvent struct {
	Timestamp time.Time `json:"ts"`
	IssueID   string    `json:"issue_id"`
	SessionID string    `json:"session_id"`
	AgentType string    `json:"agent_type,omitempty"`
	Reason    string    `json:"reason"`
}

// LogSecurityEvent appends a security event to the jsonl file
func LogSecurityEvent(baseDir string, event SecurityEvent) error {
	errPath := filepath.Join(baseDir, securityEventsFile)

	// Ensure .todos directory exists
	todosDir := filepath.Dir(errPath)
	if _, err := os.Stat(todosDir); os.IsNotExist(err) {
		if err := os.MkdirAll(todosDir, 0755); err != nil {
			return err
		}
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(errPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, err = f.Write(append(data, '\n'))
	return err
}

// ReadSecurityEvents reads all security events from the file
func ReadSecurityEvents(baseDir string) ([]SecurityEvent, error) {
	errPath := filepath.Join(baseDir, securityEventsFile)

	data, err := os.ReadFile(errPath)
	if os.IsNotExist(err) {
		return []SecurityEvent{}, nil
	}
	if err != nil {
		return nil, err
	}

	var events []SecurityEvent
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			if i > start {
				line := data[start:i]
				var e SecurityEvent
				if err := json.Unmarshal(line, &e); err == nil {
					events = append(events, e)
				}
			}
			start = i + 1
		}
	}

	return events, nil
}

// ClearSecurityEvents removes the security events file
func ClearSecurityEvents(baseDir string) error {
	errPath := filepath.Join(baseDir, securityEventsFile)
	err := os.Remove(errPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
