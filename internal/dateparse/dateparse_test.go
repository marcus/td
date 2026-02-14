package dateparse

import (
	"testing"
	"time"
)

// Fixed reference time: Wednesday, 2026-02-18 12:00:00 UTC
var testNow = time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)

func TestParseDate_ExactDate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2026-03-01", "2026-03-01"},
		{"2025-12-31", "2025-12-31"},
		{"2026-01-01", "2026-01-01"},
	}
	for _, tt := range tests {
		got, err := ParseDateFrom(tt.input, testNow)
		if err != nil {
			t.Errorf("ParseDateFrom(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDateFrom(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseDate_RelativeDays(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"+0d", "2026-02-18"},
		{"+1d", "2026-02-19"},
		{"+7d", "2026-02-25"},
		{"+10d", "2026-02-28"},
		{"+14d", "2026-03-04"},
	}
	for _, tt := range tests {
		got, err := ParseDateFrom(tt.input, testNow)
		if err != nil {
			t.Errorf("ParseDateFrom(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDateFrom(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseDate_RelativeWeeks(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"+1w", "2026-02-25"},
		{"+2w", "2026-03-04"},
		{"+0w", "2026-02-18"},
	}
	for _, tt := range tests {
		got, err := ParseDateFrom(tt.input, testNow)
		if err != nil {
			t.Errorf("ParseDateFrom(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDateFrom(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseDate_RelativeMonths(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"+1m", "2026-03-18"},
		{"+2m", "2026-04-18"},
		{"+0m", "2026-02-18"},
	}
	for _, tt := range tests {
		got, err := ParseDateFrom(tt.input, testNow)
		if err != nil {
			t.Errorf("ParseDateFrom(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDateFrom(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseDate_MonthEndOverflow(t *testing.T) {
	// Jan 31 + 1 month: Go's AddDate normalizes to Feb 28 (or 29 in leap year)
	jan31 := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	got, err := ParseDateFrom("+1m", jan31)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2026 is not a leap year, so Feb has 28 days → Go normalizes Jan 31 + 1m to Mar 3
	if got != "2026-03-03" {
		t.Errorf("Jan 31 + 1m = %q, want %q", got, "2026-03-03")
	}
}

func TestParseDate_DayNames(t *testing.T) {
	// testNow is Wednesday 2026-02-18
	tests := []struct {
		input string
		want  string
	}{
		{"monday", "2026-02-23"},    // next Monday
		{"tuesday", "2026-02-24"},   // next Tuesday
		{"wednesday", "2026-02-25"}, // next Wednesday (not today)
		{"thursday", "2026-02-19"},  // next Thursday (tomorrow)
		{"friday", "2026-02-20"},    // next Friday
		{"saturday", "2026-02-21"},  // next Saturday
		{"sunday", "2026-02-22"},    // next Sunday
	}
	for _, tt := range tests {
		got, err := ParseDateFrom(tt.input, testNow)
		if err != nil {
			t.Errorf("ParseDateFrom(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDateFrom(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseDate_DayNamesCaseInsensitive(t *testing.T) {
	tests := []string{"Monday", "FRIDAY", "Thursday"}
	for _, input := range tests {
		_, err := ParseDateFrom(input, testNow)
		if err != nil {
			t.Errorf("ParseDateFrom(%q): should accept mixed case, got error: %v", input, err)
		}
	}
}

func TestParseDate_Keywords(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"today", "2026-02-18"},
		{"tomorrow", "2026-02-19"},
		{"next-week", "2026-02-23"},  // next Monday from Wed Feb 18
		{"next-month", "2026-03-01"}, // 1st of next month
	}
	for _, tt := range tests {
		got, err := ParseDateFrom(tt.input, testNow)
		if err != nil {
			t.Errorf("ParseDateFrom(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDateFrom(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseDate_NextWeekOnMonday(t *testing.T) {
	// If today is Monday, "next-week" should be *next* Monday, not today
	monday := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC) // Monday
	got, err := ParseDateFrom("next-week", monday)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2026-02-23" {
		t.Errorf("next-week on Monday = %q, want %q", got, "2026-02-23")
	}
}

func TestParseDate_NextMonthFromDecember(t *testing.T) {
	dec := time.Date(2025, 12, 15, 12, 0, 0, 0, time.UTC)
	got, err := ParseDateFrom("next-month", dec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2026-01-01" {
		t.Errorf("next-month from December = %q, want %q", got, "2026-01-01")
	}
}

func TestParseDate_WhitespaceHandling(t *testing.T) {
	got, err := ParseDateFrom("  tomorrow  ", testNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2026-02-19" {
		t.Errorf("trimmed 'tomorrow' = %q, want %q", got, "2026-02-19")
	}
}

func TestParseDate_Errors(t *testing.T) {
	invalids := []string{
		"",
		"yesterday",
		"next year",
		"+3x",
		"notaday",
		"2026/03/01",
		"+d",
		"+w",
	}
	for _, input := range invalids {
		_, err := ParseDateFrom(input, testNow)
		if err == nil {
			t.Errorf("ParseDateFrom(%q): expected error, got nil", input)
		}
	}
}

func TestParseDate_UsesCurrentTime(t *testing.T) {
	// Verify ParseDate works (uses time.Now internally)
	result, err := ParseDate("today")
	if err != nil {
		t.Fatalf("ParseDate('today'): unexpected error: %v", err)
	}
	expected := time.Now().Format("2006-01-02")
	if result != expected {
		t.Errorf("ParseDate('today') = %q, want %q", result, expected)
	}
}
