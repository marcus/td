// Package dateparse provides utilities for parsing relative and absolute date strings
// into ISO 8601 (YYYY-MM-DD) format.
package dateparse

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDate parses a date input string and returns an ISO 8601 date (YYYY-MM-DD).
// Uses the current time as the reference point.
//
// Supported formats:
//   - Exact dates: "2026-03-01"
//   - Relative days: "+7d"
//   - Relative weeks: "+2w"
//   - Relative months: "+1m"
//   - Day names: "monday", "tuesday", etc. (next occurrence)
//   - Keywords: "today", "tomorrow", "next-week", "next-month"
func ParseDate(input string) (string, error) {
	return ParseDateFrom(input, time.Now())
}

// ParseDateFrom parses a date input string relative to the given reference time.
// This variant enables deterministic testing with a fixed "now".
func ParseDateFrom(input string, now time.Time) (string, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return "", fmt.Errorf("empty date input")
	}

	// Exact date: YYYY-MM-DD
	if t, err := time.Parse("2006-01-02", input); err == nil {
		return t.Format("2006-01-02"), nil
	}

	// Keywords
	switch input {
	case "today":
		return formatDate(now), nil
	case "tomorrow":
		return formatDate(now.AddDate(0, 0, 1)), nil
	case "next-week":
		// Next Monday
		daysUntilMonday := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		return formatDate(now.AddDate(0, 0, daysUntilMonday)), nil
	case "next-month":
		// 1st of next month
		year, month, _ := now.Date()
		nextMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, now.Location())
		return formatDate(nextMonth), nil
	}

	// Relative offsets: +Nd, +Nw, +Nm
	if strings.HasPrefix(input, "+") && len(input) >= 3 {
		suffix := input[len(input)-1]
		numStr := input[1 : len(input)-1]
		n, err := strconv.Atoi(numStr)
		if err == nil && n >= 0 {
			switch suffix {
			case 'd':
				return formatDate(now.AddDate(0, 0, n)), nil
			case 'w':
				return formatDate(now.AddDate(0, 0, n*7)), nil
			case 'm':
				return formatDate(now.AddDate(0, n, 0)), nil
			default:
				return "", fmt.Errorf("unknown relative unit %q in %q (use d, w, or m)", string(suffix), input)
			}
		}
	}

	// Day names: next occurrence of that weekday
	dayMap := map[string]time.Weekday{
		"sunday":    time.Sunday,
		"monday":    time.Monday,
		"tuesday":   time.Tuesday,
		"wednesday": time.Wednesday,
		"thursday":  time.Thursday,
		"friday":    time.Friday,
		"saturday":  time.Saturday,
	}
	if target, ok := dayMap[input]; ok {
		daysAhead := (int(target) - int(now.Weekday()) + 7) % 7
		if daysAhead == 0 {
			daysAhead = 7 // always advance to next occurrence
		}
		return formatDate(now.AddDate(0, 0, daysAhead)), nil
	}

	return "", fmt.Errorf("unrecognized date format: %q", input)
}

func formatDate(t time.Time) string {
	return t.Format("2006-01-02")
}
