package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const commandUsageFile = ".todos/command_usage.jsonl"

// CommandUsageEvent represents a single CLI invocation
type CommandUsageEvent struct {
	Timestamp  time.Time         `json:"ts"`
	Command    string            `json:"cmd"`
	Subcommand string            `json:"sub,omitempty"`
	Flags      map[string]string `json:"flags,omitempty"`
	SessionID  string            `json:"session,omitempty"`
	Success    bool              `json:"ok"`
	DurationMs int64             `json:"dur_ms"`
	Error      string            `json:"err,omitempty"`
}

// AnalyticsSummary holds aggregated analytics data
type AnalyticsSummary struct {
	TotalCommands   int            `json:"total_commands"`
	UniqueCommands  int            `json:"unique_commands"`
	CommandCounts   map[string]int `json:"by_command"`
	FlagCounts      map[string]int `json:"by_flag"`
	DailyActivity   map[string]int `json:"daily"`
	ErrorRate       float64        `json:"error_rate"`
	ErrorsByCommand map[string]int `json:"errors_by_command"`
	SessionActivity map[string]int `json:"by_session"`
	AvgDurationMs   int64          `json:"avg_dur_ms"`
	NeverUsed       []string       `json:"never_used,omitempty"`
}

// sensitiveKeywords are flag name substrings that should have values redacted
var sensitiveKeywords = []string{
	"password", "token", "secret", "key", "cred", "auth", "api-key", "private",
}

// AnalyticsEnabled returns true unless TD_ANALYTICS is explicitly disabled
func AnalyticsEnabled() bool {
	val := os.Getenv("TD_ANALYTICS")
	switch strings.ToLower(val) {
	case "false", "0", "off", "no":
		return false
	default:
		return true
	}
}

// LogCommandUsage appends a usage event to the JSONL file
func LogCommandUsage(baseDir string, event CommandUsageEvent) error {
	usagePath := filepath.Join(baseDir, commandUsageFile)

	// Check if .todos directory exists - if not, project not initialized
	todosDir := filepath.Dir(usagePath)
	if _, err := os.Stat(todosDir); os.IsNotExist(err) {
		return nil // silently drop - project not initialized
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(usagePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
}

// LogCommandUsageAsync logs without blocking the caller
func LogCommandUsageAsync(baseDir string, event CommandUsageEvent) {
	go func() {
		defer func() { recover() }() // don't crash on panic
		_ = LogCommandUsage(baseDir, event)
	}()
}

// ReadCommandUsage reads all usage events from the file
func ReadCommandUsage(baseDir string) ([]CommandUsageEvent, error) {
	usagePath := filepath.Join(baseDir, commandUsageFile)

	data, err := os.ReadFile(usagePath)
	if os.IsNotExist(err) {
		return []CommandUsageEvent{}, nil
	}
	if err != nil {
		return nil, err
	}

	return parseCommandUsage(data)
}

// ReadCommandUsageFiltered reads usage events matching filter criteria
func ReadCommandUsageFiltered(baseDir string, since time.Time, limit int) ([]CommandUsageEvent, error) {
	all, err := ReadCommandUsage(baseDir)
	if err != nil {
		return nil, err
	}

	var filtered []CommandUsageEvent
	for i := len(all) - 1; i >= 0; i-- { // reverse order (newest first)
		e := all[i]
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

// ComputeAnalyticsSummary aggregates events into summary stats
func ComputeAnalyticsSummary(events []CommandUsageEvent, allCommands []string) *AnalyticsSummary {
	summary := &AnalyticsSummary{
		CommandCounts:   make(map[string]int),
		FlagCounts:      make(map[string]int),
		DailyActivity:   make(map[string]int),
		ErrorsByCommand: make(map[string]int),
		SessionActivity: make(map[string]int),
	}

	if len(events) == 0 {
		return summary
	}

	var totalDuration int64
	errorCount := 0

	for _, e := range events {
		summary.TotalCommands++

		// Command with subcommand
		cmdKey := e.Command
		if e.Subcommand != "" {
			cmdKey = e.Command + " " + e.Subcommand
		}
		summary.CommandCounts[cmdKey]++

		// Flags
		for flag := range e.Flags {
			summary.FlagCounts["--"+flag]++
		}

		// Daily activity
		day := e.Timestamp.Format("2006-01-02")
		summary.DailyActivity[day]++

		// Errors
		if !e.Success {
			errorCount++
			summary.ErrorsByCommand[cmdKey]++
		}

		// Session
		if e.SessionID != "" {
			summary.SessionActivity[e.SessionID]++
		}

		totalDuration += e.DurationMs
	}

	summary.UniqueCommands = len(summary.CommandCounts)
	if summary.TotalCommands > 0 {
		summary.ErrorRate = float64(errorCount) / float64(summary.TotalCommands)
		summary.AvgDurationMs = totalDuration / int64(summary.TotalCommands)
	}

	// Compute never-used commands
	usedCommands := make(map[string]bool)
	for cmd := range summary.CommandCounts {
		// Handle both "cmd" and "cmd subcmd" formats
		parts := strings.SplitN(cmd, " ", 2)
		usedCommands[parts[0]] = true
	}

	for _, cmd := range allCommands {
		if !usedCommands[cmd] {
			summary.NeverUsed = append(summary.NeverUsed, cmd)
		}
	}

	return summary
}

// ClearCommandUsage removes the usage file
func ClearCommandUsage(baseDir string) error {
	usagePath := filepath.Join(baseDir, commandUsageFile)
	err := os.Remove(usagePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// CountCommandUsage returns the number of logged events
func CountCommandUsage(baseDir string) (int, error) {
	all, err := ReadCommandUsage(baseDir)
	if err != nil {
		return 0, err
	}
	return len(all), nil
}

// SanitizeFlags redacts sensitive flag values
func SanitizeFlags(flags map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range flags {
		lower := strings.ToLower(k)
		if containsSensitiveKeyword(lower) {
			result[k] = "[REDACTED]"
		} else {
			result[k] = v
		}
	}
	return result
}

func containsSensitiveKeyword(s string) bool {
	for _, keyword := range sensitiveKeywords {
		if strings.Contains(s, keyword) {
			return true
		}
	}
	return false
}

func parseCommandUsage(data []byte) ([]CommandUsageEvent, error) {
	var events []CommandUsageEvent

	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			if i > start {
				line := data[start:i]
				var e CommandUsageEvent
				if err := json.Unmarshal(line, &e); err == nil {
					events = append(events, e)
				}
			}
			start = i + 1
		}
	}

	return events, nil
}
