package output

import (
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

// TestFormatTimeAgoJustNow tests times less than a minute ago
func TestFormatTimeAgoJustNow(t *testing.T) {
	now := time.Now()
	tests := []time.Time{
		now,
		now.Add(-30 * time.Second),
		now.Add(-59 * time.Second),
	}

	for _, tm := range tests {
		result := FormatTimeAgo(tm)
		if result != "just now" {
			t.Errorf("FormatTimeAgo(%v) = %q, want 'just now'", tm, result)
		}
	}
}

// TestFormatTimeAgoMinutes tests times 1-59 minutes ago
func TestFormatTimeAgoMinutes(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{1 * time.Minute, "1m ago"},
		{2 * time.Minute, "2m ago"},
		{30 * time.Minute, "30m ago"},
		{59 * time.Minute, "59m ago"},
	}

	for _, tc := range tests {
		tm := time.Now().Add(-tc.duration)
		result := FormatTimeAgo(tm)
		if result != tc.expected {
			t.Errorf("FormatTimeAgo(-%v) = %q, want %q", tc.duration, result, tc.expected)
		}
	}
}

// TestFormatTimeAgoHours tests times 1-23 hours ago
func TestFormatTimeAgoHours(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{1 * time.Hour, "1h ago"},
		{2 * time.Hour, "2h ago"},
		{12 * time.Hour, "12h ago"},
		{23 * time.Hour, "23h ago"},
	}

	for _, tc := range tests {
		tm := time.Now().Add(-tc.duration)
		result := FormatTimeAgo(tm)
		if result != tc.expected {
			t.Errorf("FormatTimeAgo(-%v) = %q, want %q", tc.duration, result, tc.expected)
		}
	}
}

// TestFormatTimeAgoDays tests times 1-6 days ago
func TestFormatTimeAgoDays(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{24 * time.Hour, "1d ago"},
		{48 * time.Hour, "2d ago"},
		{6 * 24 * time.Hour, "6d ago"},
	}

	for _, tc := range tests {
		tm := time.Now().Add(-tc.duration)
		result := FormatTimeAgo(tm)
		if result != tc.expected {
			t.Errorf("FormatTimeAgo(-%v) = %q, want %q", tc.duration, result, tc.expected)
		}
	}
}

// TestFormatTimeAgoDate tests times 7+ days ago (returns date)
func TestFormatTimeAgoDate(t *testing.T) {
	tm := time.Now().Add(-8 * 24 * time.Hour)
	result := FormatTimeAgo(tm)
	expected := tm.Format("2006-01-02")
	if result != expected {
		t.Errorf("FormatTimeAgo(-8d) = %q, want %q", result, expected)
	}
}

// TestFormatPoints tests point formatting
func TestFormatPoints(t *testing.T) {
	tests := []struct {
		points   int
		expected string
	}{
		{0, ""},
		{1, "1pts"},
		{5, "5pts"},
		{13, "13pts"},
		{21, "21pts"},
	}

	for _, tc := range tests {
		result := FormatPoints(tc.points)
		if result != tc.expected {
			t.Errorf("FormatPoints(%d) = %q, want %q", tc.points, result, tc.expected)
		}
	}
}

// TestFormatPointsSuffix tests point suffix formatting
func TestFormatPointsSuffix(t *testing.T) {
	tests := []struct {
		points   int
		expected string
	}{
		{0, ""},
		{1, "  1pts"},
		{5, "  5pts"},
		{13, "  13pts"},
	}

	for _, tc := range tests {
		result := FormatPointsSuffix(tc.points)
		if result != tc.expected {
			t.Errorf("FormatPointsSuffix(%d) = %q, want %q", tc.points, result, tc.expected)
		}
	}
}

// TestFormatStatus tests all status values
func TestFormatStatus(t *testing.T) {
	statuses := []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
		models.StatusBlocked,
		models.StatusInReview,
		models.StatusClosed,
	}

	for _, s := range statuses {
		result := FormatStatus(s)
		// Should contain the status in brackets
		if !strings.Contains(result, string(s)) {
			t.Errorf("FormatStatus(%q) = %q, should contain status", s, result)
		}
	}
}

// TestFormatStatusUnknown tests unknown status
func TestFormatStatusUnknown(t *testing.T) {
	unknown := models.Status("unknown")
	result := FormatStatus(unknown)
	if result != "unknown" {
		t.Errorf("FormatStatus(unknown) = %q, want 'unknown'", result)
	}
}

// TestFormatPriority tests priority formatting
func TestFormatPriority(t *testing.T) {
	priorities := []models.Priority{
		models.PriorityP0,
		models.PriorityP1,
		models.PriorityP2,
		models.PriorityP3,
		models.PriorityP4,
	}

	for _, p := range priorities {
		result := FormatPriority(p)
		if !strings.Contains(result, string(p)) {
			t.Errorf("FormatPriority(%q) should contain priority", p)
		}
	}
}

// TestFormatGitState tests git state formatting
func TestFormatGitState(t *testing.T) {
	tests := []struct {
		sha      string
		branch   string
		dirty    int
		contains []string
	}{
		{"abc1234567890", "main", 0, []string{"abc1234", "main", "clean"}},
		{"def4567890abc", "feature", 3, []string{"def4567", "feature", "3 dirty"}},
		{"1234567890abc", "develop", 1, []string{"1234567", "develop", "1 dirty"}},
	}

	for _, tc := range tests {
		result := FormatGitState(tc.sha, tc.branch, tc.dirty)
		for _, c := range tc.contains {
			if !strings.Contains(result, c) {
				t.Errorf("FormatGitState(%q, %q, %d) = %q, should contain %q",
					tc.sha, tc.branch, tc.dirty, result, c)
			}
		}
	}
}

// TestFormatIssueShort tests short issue formatting
func TestFormatIssueShort(t *testing.T) {
	issue := &models.Issue{
		ID:       "td-abc1",
		Title:    "Test issue title",
		Status:   models.StatusOpen,
		Type:     models.TypeBug,
		Priority: models.PriorityP1,
		Points:   5,
	}

	result := FormatIssueShort(issue)

	// Should contain ID, title, type
	if !strings.Contains(result, "td-abc1") {
		t.Error("FormatIssueShort should contain issue ID")
	}
	if !strings.Contains(result, "Test issue title") {
		t.Error("FormatIssueShort should contain title")
	}
	if !strings.Contains(result, "bug") {
		t.Error("FormatIssueShort should contain type")
	}
	if !strings.Contains(result, "5pts") {
		t.Error("FormatIssueShort should contain points")
	}
}

// TestFormatIssueShortNoPoints tests short format without points
func TestFormatIssueShortNoPoints(t *testing.T) {
	issue := &models.Issue{
		ID:       "td-def2",
		Title:    "No points issue",
		Status:   models.StatusClosed,
		Type:     models.TypeTask,
		Priority: models.PriorityP3,
		Points:   0,
	}

	result := FormatIssueShort(issue)

	if !strings.Contains(result, "td-def2") {
		t.Error("Should contain issue ID")
	}
	if strings.Contains(result, "pts") {
		t.Error("Should not contain pts when points is 0")
	}
}

// TestFormatIssueDeleted tests deleted issue formatting
func TestFormatIssueDeleted(t *testing.T) {
	issue := &models.Issue{
		ID:       "td-del1",
		Title:    "Deleted issue",
		Status:   models.StatusClosed,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}

	result := FormatIssueDeleted(issue)

	if !strings.Contains(result, "td-del1") {
		t.Error("Should contain issue ID")
	}
	if !strings.Contains(result, "[deleted]") {
		t.Error("Should contain [deleted] marker")
	}
}

// TestFormatIssueLong tests long issue formatting
func TestFormatIssueLong(t *testing.T) {
	issue := &models.Issue{
		ID:          "td-long1",
		Title:       "Long format issue",
		Description: "This is a detailed description",
		Status:      models.StatusInProgress,
		Type:        models.TypeFeature,
		Priority:    models.PriorityP1,
		Points:      8,
		Labels:      []string{"backend", "api"},
	}

	logs := []models.Log{
		{
			Message:   "Started work",
			Type:      models.LogTypeProgress,
			Timestamp: time.Now().Add(-30 * time.Minute),
		},
		{
			Message:   "Found a blocker",
			Type:      models.LogTypeBlocker,
			Timestamp: time.Now().Add(-10 * time.Minute),
		},
	}

	handoff := &models.Handoff{
		SessionID: "ses_test",
		Done:      []string{"Implemented core logic"},
		Remaining: []string{"Write tests"},
		Decisions: []string{"Use map instead of slice"},
		Uncertain: []string{"Edge case handling"},
		Timestamp: time.Now().Add(-5 * time.Minute),
	}

	result := FormatIssueLong(issue, logs, handoff)

	// Check issue details
	if !strings.Contains(result, "td-long1") {
		t.Error("Should contain issue ID")
	}
	if !strings.Contains(result, "Long format issue") {
		t.Error("Should contain title")
	}
	if !strings.Contains(result, "This is a detailed description") {
		t.Error("Should contain description")
	}
	if !strings.Contains(result, "Points: 8") {
		t.Error("Should contain points")
	}
	if !strings.Contains(result, "backend, api") {
		t.Error("Should contain labels")
	}

	// Check logs
	if !strings.Contains(result, "SESSION LOG") {
		t.Error("Should contain SESSION LOG header")
	}
	if !strings.Contains(result, "Started work") {
		t.Error("Should contain log message")
	}
	if !strings.Contains(result, "[blocker]") {
		t.Error("Should contain log type for non-progress")
	}

	// Check handoff
	if !strings.Contains(result, "CURRENT HANDOFF") {
		t.Error("Should contain CURRENT HANDOFF header")
	}
	if !strings.Contains(result, "Implemented core logic") {
		t.Error("Should contain done items")
	}
	if !strings.Contains(result, "Write tests") {
		t.Error("Should contain remaining items")
	}
	if !strings.Contains(result, "Use map instead of slice") {
		t.Error("Should contain decisions")
	}
	if !strings.Contains(result, "Edge case handling") {
		t.Error("Should contain uncertain items")
	}
}

// TestFormatIssueLongNoOptional tests long format without optional fields
func TestFormatIssueLongNoOptional(t *testing.T) {
	issue := &models.Issue{
		ID:       "td-min1",
		Title:    "Minimal issue",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}

	result := FormatIssueLong(issue, nil, nil)

	if !strings.Contains(result, "td-min1") {
		t.Error("Should contain issue ID")
	}
	if strings.Contains(result, "Description:") {
		t.Error("Should not contain Description header when empty")
	}
	if strings.Contains(result, "Points:") {
		t.Error("Should not contain Points when 0")
	}
	if strings.Contains(result, "Labels:") {
		t.Error("Should not contain Labels when empty")
	}
	if strings.Contains(result, "SESSION LOG") {
		t.Error("Should not contain SESSION LOG when no logs")
	}
	if strings.Contains(result, "CURRENT HANDOFF") {
		t.Error("Should not contain CURRENT HANDOFF when nil")
	}
}

// TestFormatIssueLongInReview tests review status message
func TestFormatIssueLongInReview(t *testing.T) {
	issue := &models.Issue{
		ID:       "td-rev1",
		Title:    "Review issue",
		Status:   models.StatusInReview,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}

	result := FormatIssueLong(issue, nil, nil)

	if !strings.Contains(result, "AWAITING REVIEW") {
		t.Error("Should contain AWAITING REVIEW for in_review status")
	}
}

// TestOutputModeConstants tests output mode constants
func TestOutputModeConstants(t *testing.T) {
	if ModeShort != 0 {
		t.Error("ModeShort should be 0")
	}
	if ModeLong != 1 {
		t.Error("ModeLong should be 1")
	}
	if ModeJSON != 2 {
		t.Error("ModeJSON should be 2")
	}
}

// TestErrorCodeConstants tests error code constants
func TestErrorCodeConstants(t *testing.T) {
	codes := []struct {
		code     string
		expected string
	}{
		{ErrCodeNotFound, "not_found"},
		{ErrCodeInvalidInput, "invalid_input"},
		{ErrCodeConflict, "conflict"},
		{ErrCodeCannotSelfApprove, "cannot_self_approve"},
		{ErrCodeHandoffRequired, "handoff_required"},
		{ErrCodeDatabaseError, "database_error"},
		{ErrCodeGitError, "git_error"},
		{ErrCodeNoActiveSession, "no_active_session"},
	}

	for _, tc := range codes {
		if tc.code != tc.expected {
			t.Errorf("Error code %q != %q", tc.code, tc.expected)
		}
	}
}

// TestFormatTimeAgoEdgeCases tests edge cases in time formatting
func TestFormatTimeAgoEdgeCases(t *testing.T) {
	// Exactly at minute boundary
	tm := time.Now().Add(-60 * time.Second)
	result := FormatTimeAgo(tm)
	if result != "1m ago" {
		t.Errorf("At 60s boundary: got %q, want '1m ago'", result)
	}

	// Exactly at hour boundary
	tm = time.Now().Add(-60 * time.Minute)
	result = FormatTimeAgo(tm)
	if result != "1h ago" {
		t.Errorf("At 60m boundary: got %q, want '1h ago'", result)
	}

	// Exactly at day boundary
	tm = time.Now().Add(-24 * time.Hour)
	result = FormatTimeAgo(tm)
	if result != "1d ago" {
		t.Errorf("At 24h boundary: got %q, want '1d ago'", result)
	}

	// Exactly at week boundary
	tm = time.Now().Add(-7 * 24 * time.Hour)
	result = FormatTimeAgo(tm)
	expected := tm.Format("2006-01-02")
	if result != expected {
		t.Errorf("At 7d boundary: got %q, want %q", result, expected)
	}
}

// TestFormatIssueLongWithEmptyHandoffSections tests handoff with some empty sections
func TestFormatIssueLongWithEmptyHandoffSections(t *testing.T) {
	issue := &models.Issue{
		ID:       "td-hand1",
		Title:    "Handoff test",
		Status:   models.StatusInProgress,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}

	handoff := &models.Handoff{
		SessionID: "ses_test",
		Done:      []string{"Only done items"},
		Timestamp: time.Now(),
	}

	result := FormatIssueLong(issue, nil, handoff)

	if !strings.Contains(result, "Done:") {
		t.Error("Should contain Done section")
	}
	if strings.Contains(result, "Remaining:") {
		t.Error("Should not contain Remaining section when empty")
	}
	if strings.Contains(result, "Decisions:") {
		t.Error("Should not contain Decisions section when empty")
	}
	if strings.Contains(result, "Uncertain:") {
		t.Error("Should not contain Uncertain section when empty")
	}
}

// TestFormatGitStateShortSHA tests SHA truncation
func TestFormatGitStateShortSHA(t *testing.T) {
	// SHA should be truncated to 7 chars
	fullSHA := "abc1234567890def"
	result := FormatGitState(fullSHA, "main", 0)
	if !strings.Contains(result, "abc1234") {
		t.Error("Should contain first 7 chars of SHA")
	}
	if strings.Contains(result, "567890") {
		t.Error("Should not contain more than 7 chars of SHA")
	}
}
