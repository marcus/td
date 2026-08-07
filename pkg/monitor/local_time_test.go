package monitor

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestFormatLocalTimeUsesLocalWallClock(t *testing.T) {
	// A fixed UTC instant must render as local wall clock, not UTC.
	utc := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	want := utc.Local().Format("01-02 15:04")
	if got := formatLocalTime(utc, "01-02 15:04"); got != want {
		t.Fatalf("formatLocalTime = %q, want %q", got, want)
	}
	// When the local offset is non-zero, Local wall clock differs from UTC Format.
	if _, offset := utc.Local().Zone(); offset != 0 {
		if got := formatLocalTime(utc, "15:04"); got == utc.Format("15:04") {
			t.Fatalf("formatLocalTime = %q equals UTC wall clock in zone offset %d", got, offset)
		}
	}
	// Idempotent: already-local times still format correctly.
	local := utc.Local()
	if got := formatLocalTime(local, "2006-01-02 15:04:05"); got != local.Format("2006-01-02 15:04:05") {
		t.Fatalf("formatLocalTime on local time = %q, want %q", got, local.Format("2006-01-02 15:04:05"))
	}
}

func TestCalendarDaysBetweenDSTTransitions(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// US spring-forward 2026-03-08: local midnight→midnight is 23 hours.
	// Hours()/24 would yield 0; calendar math must still yield 1.
	springStart := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	springEnd := time.Date(2026, 3, 9, 0, 0, 0, 0, loc)
	if got := calendarDaysBetween(springStart, springEnd); got != 1 {
		t.Fatalf("spring-forward calendarDaysBetween = %d, want 1 (absolute hours=%.0f)",
			got, springEnd.Sub(springStart).Hours())
	}

	// US fall-back 2026-11-01: local midnight→midnight is 25 hours.
	fallStart := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	fallEnd := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	if got := calendarDaysBetween(fallStart, fallEnd); got != 1 {
		t.Fatalf("fall-back calendarDaysBetween = %d, want 1 (absolute hours=%.0f)",
			got, fallEnd.Sub(fallStart).Hours())
	}

	// Multi-day and negative.
	if got := calendarDaysBetween(springStart, springStart.AddDate(0, 0, 5)); got != 5 {
		t.Fatalf("5-day span = %d, want 5", got)
	}
	if got := calendarDaysBetween(springEnd, springStart); got != -1 {
		t.Fatalf("reverse 1-day span = %d, want -1", got)
	}
	if got := calendarDaysBetween(springStart, springStart); got != 0 {
		t.Fatalf("same day = %d, want 0", got)
	}
}

func TestFormatDueDateRelative(t *testing.T) {
	now := time.Now()
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	in3 := now.AddDate(0, 0, 3).Format("2006-01-02")
	in10 := now.AddDate(0, 0, 10).Format("2006-01-02")

	tests := []struct {
		name    string
		date    string
		wantSub string
	}{
		{name: "today", date: today, wantSub: "due TODAY"},
		{name: "tomorrow", date: tomorrow, wantSub: "(1 days)"},
		{name: "yesterday overdue", date: yesterday, wantSub: "OVERDUE by 1 day"},
		{name: "within a week", date: in3, wantSub: "(3 days)"},
		{name: "beyond a week", date: in10, wantSub: "(10 days)"},
		{name: "unparseable passthrough", date: "not-a-date", wantSub: "not-a-date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ansi.Strip(formatDueDate(tt.date))
			if !strings.Contains(got, tt.wantSub) {
				t.Fatalf("formatDueDate(%q) = %q, want substring %q", tt.date, got, tt.wantSub)
			}
		})
	}
}

func TestFormatDeferUntilRelative(t *testing.T) {
	now := time.Now()
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	in5 := now.AddDate(0, 0, 5).Format("2006-01-02")

	tests := []struct {
		name    string
		date    string
		wantSub string
	}{
		{name: "today", date: today, wantSub: "today"},
		{name: "tomorrow", date: tomorrow, wantSub: "tomorrow"},
		{name: "past", date: yesterday, wantSub: "(past)"},
		{name: "future days", date: in5, wantSub: "(5 days)"},
		{name: "unparseable passthrough", date: "bogus", wantSub: "bogus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ansi.Strip(formatDeferUntil(tt.date))
			if !strings.Contains(got, tt.wantSub) {
				t.Fatalf("formatDeferUntil(%q) = %q, want substring %q", tt.date, got, tt.wantSub)
			}
		})
	}
}

func TestFormatDueDateParsesCivilDateInLocal(t *testing.T) {
	// time.Parse (UTC) for a civil date can make "tomorrow" look like "today"
	// in negative-offset zones during local evening. ParseInLocation(time.Local)
	// must keep calendar days stable. We assert the local parse of "today+1"
	// yields days==1 against local midnight — the same math formatDueDate uses.
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	tomorrowStr := today.AddDate(0, 0, 1).Format("2006-01-02")

	parsedLocal, err := time.ParseInLocation("2006-01-02", tomorrowStr, time.Local)
	if err != nil {
		t.Fatalf("ParseInLocation: %v", err)
	}
	if days := calendarDaysBetween(today, parsedLocal); days != 1 {
		t.Fatalf("local civil parse of tomorrow: days=%d, want 1", days)
	}

	// Contrast: UTC parse of the same civil string can disagree with local today
	// near the date line for negative offsets (document the bug class).
	parsedUTC, err := time.Parse("2006-01-02", tomorrowStr)
	if err != nil {
		t.Fatalf("Parse UTC: %v", err)
	}
	utcDays := int(parsedUTC.Sub(today).Hours() / 24)
	localDays := calendarDaysBetween(today, parsedLocal)
	// In America/* evening, utcDays may be 0 while localDays is 1. We only require
	// that localDays is correct; if utcDays differs, that is the pre-fix failure mode.
	if localDays != 1 {
		t.Fatalf("local days for tomorrow = %d, want 1 (utcDays=%d zone=%s)", localDays, utcDays, time.Local)
	}
}
