package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const agentErrorsFile = ".todos/agent_errors.jsonl"

// AgentError represents a failed command invocation
type AgentError struct {
	Timestamp time.Time `json:"ts"`
	Args      []string  `json:"args"`
	Error     string    `json:"error"`
	SessionID string    `json:"session,omitempty"`
}

// LogAgentError appends a failed command to the agent errors file.
// baseDir is the project root. If the .todos directory doesn't exist,
// the error is silently dropped (project not initialized).
func LogAgentError(baseDir string, args []string, errMsg string, sessionID string) error {
	errPath := filepath.Join(baseDir, agentErrorsFile)

	// Check if .todos directory exists - if not, project not initialized
	todosDir := filepath.Dir(errPath)
	if _, err := os.Stat(todosDir); os.IsNotExist(err) {
		return nil // silently drop - project not initialized
	}

	entry := AgentError{
		Timestamp: time.Now().UTC(),
		Args:      args,
		Error:     errMsg,
		SessionID: sessionID,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(errPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
}

// ReadAgentErrors reads all agent errors from the file.
// Returns empty slice if file doesn't exist.
func ReadAgentErrors(baseDir string) ([]AgentError, error) {
	errPath := filepath.Join(baseDir, agentErrorsFile)

	data, err := os.ReadFile(errPath)
	if os.IsNotExist(err) {
		return []AgentError{}, nil
	}
	if err != nil {
		return nil, err
	}

	return parseAgentErrors(data)
}

// ReadAgentErrorsFiltered reads agent errors matching the filter criteria.
func ReadAgentErrorsFiltered(baseDir string, sessionID string, since time.Time, limit int) ([]AgentError, error) {
	all, err := ReadAgentErrors(baseDir)
	if err != nil {
		return nil, err
	}

	var filtered []AgentError
	for i := len(all) - 1; i >= 0; i-- { // reverse order (newest first)
		e := all[i]
		if sessionID != "" && e.SessionID != sessionID {
			continue
		}
		if !since.IsZero() && e.Timestamp.Before(since) {
			continue
		}
		filtered = append(filtered, e)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}

	return filtered, nil
}

// ClearAgentErrors removes the agent errors file.
func ClearAgentErrors(baseDir string) error {
	errPath := filepath.Join(baseDir, agentErrorsFile)
	err := os.Remove(errPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// CountAgentErrors returns the number of logged errors.
func CountAgentErrors(baseDir string) (int, error) {
	all, err := ReadAgentErrors(baseDir)
	if err != nil {
		return 0, err
	}
	return len(all), nil
}

// parseAgentErrors parses JSONL data into AgentError slice.
func parseAgentErrors(data []byte) ([]AgentError, error) {
	var errors []AgentError

	// Split by newlines and parse each line
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			if i > start {
				line := data[start:i]
				var e AgentError
				if err := json.Unmarshal(line, &e); err == nil {
					errors = append(errors, e)
				}
			}
			start = i + 1
		}
	}

	return errors, nil
}
