package output

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

// captureStdout runs fn while capturing everything written to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// TestEmitIssueShape verifies EmitIssue produces {id,status,action,issue}.
func TestEmitIssueShape(t *testing.T) {
	issue := &models.Issue{ID: "td-abc123", Status: models.StatusOpen, Title: "Test"}

	out := captureStdout(t, func() {
		if err := EmitIssue("created", issue, nil); err != nil {
			t.Fatalf("EmitIssue returned error: %v", err)
		}
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	if got["id"] != "td-abc123" {
		t.Errorf("id = %v, want td-abc123", got["id"])
	}
	if got["status"] != string(models.StatusOpen) {
		t.Errorf("status = %v, want %s", got["status"], models.StatusOpen)
	}
	if got["action"] != "created" {
		t.Errorf("action = %v, want created", got["action"])
	}
	issueObj, ok := got["issue"].(map[string]any)
	if !ok {
		t.Fatalf("issue key missing or not an object: %v", got["issue"])
	}
	if issueObj["id"] != "td-abc123" {
		t.Errorf("issue.id = %v, want td-abc123", issueObj["id"])
	}
}

// TestEmitIssueExtraOverrides verifies extra keys merge and override defaults.
func TestEmitIssueExtraOverrides(t *testing.T) {
	issue := &models.Issue{ID: "td-abc123", Status: models.StatusOpen}

	out := captureStdout(t, func() {
		if err := EmitIssue("created", issue, map[string]any{
			"session": "ses_x",
			"action":  "overridden",
		}); err != nil {
			t.Fatalf("EmitIssue returned error: %v", err)
		}
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got["session"] != "ses_x" {
		t.Errorf("session = %v, want ses_x", got["session"])
	}
	if got["action"] != "overridden" {
		t.Errorf("action = %v, want overridden (extra should override)", got["action"])
	}
}

// TestEmitResultShape verifies EmitResult produces {action} merged with extra.
func TestEmitResultShape(t *testing.T) {
	out := captureStdout(t, func() {
		if err := EmitResult("linked", map[string]any{"from": "td-a", "to": "td-b"}); err != nil {
			t.Fatalf("EmitResult returned error: %v", err)
		}
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got["action"] != "linked" {
		t.Errorf("action = %v, want linked", got["action"])
	}
	if got["from"] != "td-a" || got["to"] != "td-b" {
		t.Errorf("extra keys not merged: %v", got)
	}
	if _, hasIssue := got["issue"]; hasIssue {
		t.Errorf("EmitResult should not include an issue key: %v", got)
	}
}

// TestJSONErrorEscaping verifies that JSONError produces valid, parseable JSON
// even when the message contains characters that require escaping (quotes,
// backslashes, newlines). The envelope keys must remain {"error":{"code","message"}}.
func TestJSONErrorEscaping(t *testing.T) {
	const tricky = "boom: \"quoted\" path C:\\Users\\x and a\nnewline"

	out := captureStdout(t, func() {
		JSONError(ErrCodeInvalidInput, tricky)
	})

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("JSONError output is not valid JSON: %v\nraw: %q", err, out)
	}
	if env.Error.Code != ErrCodeInvalidInput {
		t.Errorf("code = %q, want %q", env.Error.Code, ErrCodeInvalidInput)
	}
	if env.Error.Message != tricky {
		t.Errorf("message did not round-trip:\n got %q\nwant %q", env.Error.Message, tricky)
	}
	// Single-line envelope: exactly one trailing newline, no embedded raw newline.
	trimmed := strings.TrimRight(out, "\n")
	if strings.Contains(trimmed, "\n") {
		t.Errorf("JSONError should emit a single JSON line, got: %q", out)
	}
}

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

func TestFormatIssueShortSanitizesTitleForEveryCaller(t *testing.T) {
	issue := &models.Issue{
		ID:       "td-forge",
		Title:    "real title\nSESSION LOG:\r\x1b[E\bforged",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}

	got := FormatIssueShort(issue)
	if strings.ContainsAny(got, "\n\r\b") || strings.Contains(got, "\x1b[E") {
		t.Errorf("short title retained an injected control or line break: %q", got)
	}
	if !strings.Contains(got, "real title SESSION LOG: [Eforged") {
		t.Errorf("short title should remain readable after sanitization: %q", got)
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
		Acceptance:  "Acceptance criteria text",
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
	if !strings.Contains(result, "Acceptance Criteria:") {
		t.Error("Should contain acceptance criteria header")
	}
	if !strings.Contains(result, "Acceptance criteria text") {
		t.Error("Should contain acceptance criteria text")
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
	if strings.Contains(result, "Acceptance Criteria:") {
		t.Error("Should not contain Acceptance Criteria header when empty")
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

// TestIssueOneLiner tests concise one-line issue formatting
func TestIssueOneLiner(t *testing.T) {
	issue := &models.Issue{
		ID:     "td-abc1",
		Title:  "Fix login bug",
		Status: models.StatusInProgress,
	}

	result := IssueOneLiner(issue)

	if !strings.Contains(result, "td-abc1") {
		t.Error("Should contain issue ID")
	}
	if !strings.Contains(result, "Fix login bug") {
		t.Error("Should contain title")
	}
	if !strings.Contains(result, "in_progress") {
		t.Error("Should contain status")
	}
}

// TestIssueOneLinerPlain tests plain one-liner without styling
func TestIssueOneLinerPlain(t *testing.T) {
	issue := &models.Issue{
		ID:     "td-xyz2",
		Title:  "Add feature",
		Status: models.StatusOpen,
	}

	result := IssueOneLinerPlain(issue)
	expected := `td-xyz2 "Add feature" [open]`

	if result != expected {
		t.Errorf("IssueOneLinerPlain() = %q, want %q", result, expected)
	}
}

// TestStatusBadge tests status badge with symbols
func TestStatusBadge(t *testing.T) {
	tests := []struct {
		status   models.Status
		contains string
	}{
		{models.StatusOpen, "○"},
		{models.StatusInProgress, "▶"},
		{models.StatusBlocked, "✗"},
		{models.StatusInReview, "◎"},
		{models.StatusClosed, "✓"},
	}

	for _, tc := range tests {
		result := StatusBadge(tc.status)
		if !strings.Contains(result, tc.contains) {
			t.Errorf("StatusBadge(%q) = %q, should contain %q", tc.status, result, tc.contains)
		}
		if !strings.Contains(result, string(tc.status)) {
			t.Errorf("StatusBadge(%q) should contain status name", tc.status)
		}
	}
}

// TestStatusBadgeUnknown tests badge for unknown status
func TestStatusBadgeUnknown(t *testing.T) {
	result := StatusBadge(models.Status("unknown"))
	if !strings.Contains(result, "?") {
		t.Error("Unknown status should use ? symbol")
	}
}

// TestSectionHeader tests section header formatting
func TestSectionHeader(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{"dependencies", "\nDEPENDENCIES:\n"},
		{"Git State", "\nGIT STATE:\n"},
		{"BLOCKS", "\nBLOCKS:\n"},
	}

	for _, tc := range tests {
		result := SectionHeader(tc.title)
		if result != tc.expected {
			t.Errorf("SectionHeader(%q) = %q, want %q", tc.title, result, tc.expected)
		}
	}
}

// TestIndentLines tests line indentation
func TestIndentLines(t *testing.T) {
	lines := []string{"line1", "line2", "line3"}

	result := IndentLines(lines, 2)

	expected := []string{"  line1", "  line2", "  line3"}
	for i, line := range result {
		if line != expected[i] {
			t.Errorf("IndentLines[%d] = %q, want %q", i, line, expected[i])
		}
	}
}

// TestIndentLinesZero tests zero indentation
func TestIndentLinesZero(t *testing.T) {
	lines := []string{"a", "b"}
	result := IndentLines(lines, 0)

	if result[0] != "a" || result[1] != "b" {
		t.Error("Zero indent should not change lines")
	}
}

// TestIndentLinesEmpty tests empty slice
func TestIndentLinesEmpty(t *testing.T) {
	result := IndentLines([]string{}, 4)
	if len(result) != 0 {
		t.Error("Empty input should return empty output")
	}
}

// TestIndentString tests string indentation
func TestIndentString(t *testing.T) {
	input := "line1\nline2\nline3"
	result := IndentString(input, 2)
	expected := "  line1\n  line2\n  line3"

	if result != expected {
		t.Errorf("IndentString() = %q, want %q", result, expected)
	}
}

// TestIndentStringEmpty tests empty string
func TestIndentStringEmpty(t *testing.T) {
	result := IndentString("", 4)
	if result != "" {
		t.Error("Empty string should return empty string")
	}
}

// TestBulletList tests bullet list formatting
func TestBulletList(t *testing.T) {
	items := []string{"item 1", "item 2", "item 3"}
	result := BulletList(items, 2)

	expected := []string{"  - item 1", "  - item 2", "  - item 3"}
	for i, line := range result {
		if line != expected[i] {
			t.Errorf("BulletList[%d] = %q, want %q", i, line, expected[i])
		}
	}
}

// TestBulletListNoIndent tests bullet list with no indentation
func TestBulletListNoIndent(t *testing.T) {
	items := []string{"a", "b"}
	result := BulletList(items, 0)

	if result[0] != "- a" || result[1] != "- b" {
		t.Error("Bullet list with 0 indent should have '- ' prefix only")
	}
}

// TestDependencyLine tests dependency line formatting
func TestDependencyLine(t *testing.T) {
	issue := &models.Issue{
		ID:     "td-dep1",
		Title:  "Dependency task",
		Status: models.StatusOpen,
	}

	result := DependencyLine(issue, false)
	if !strings.Contains(result, "td-dep1") {
		t.Error("Should contain issue ID")
	}
	if !strings.Contains(result, "Dependency task") {
		t.Error("Should contain title")
	}
	if strings.Contains(result, "✓") {
		t.Error("Should not contain checkmark when showResolved=false")
	}
}

// TestDependencyLineResolved tests resolved dependency
func TestDependencyLineResolved(t *testing.T) {
	issue := &models.Issue{
		ID:     "td-res1",
		Title:  "Resolved task",
		Status: models.StatusClosed,
	}

	result := DependencyLine(issue, true)
	if !strings.Contains(result, "✓") {
		t.Error("Should contain checkmark for closed issue with showResolved=true")
	}
}

// TestDependencyLineNotResolved tests non-closed with showResolved
func TestDependencyLineNotResolved(t *testing.T) {
	issue := &models.Issue{
		ID:     "td-open1",
		Title:  "Open task",
		Status: models.StatusOpen,
	}

	result := DependencyLine(issue, true)
	if strings.Contains(result, "✓") {
		t.Error("Open issue should not have checkmark even with showResolved=true")
	}
}

// TestSanitizeRenderedStripsEscapeSequences covers the sharper half of the
// forgery problem. ESC[E moves the cursor to the next line with no newline
// byte in the data, so a check that only looks for "\n" passes while the text
// still starts what looks like a fresh entry at column zero.
func TestSanitizeRenderedStripsEscapeSequences(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"cursor next line", "ok\x1b[Eforged", "ok[Eforged"},
		{"colour sequence", "ok\x1b[31mred", "ok[31mred"},
		{"erase display", "ok\x1b[2J", "ok[2J"},
		{"bare escape", "ok\x1bX", "okX"},
		{"backspace", "ok\bX", "okX"},
		{"tab and newline preserved", "a\tb\nc", "a\tb\nc"},
		{"clean text untouched", "ordinary message", "ordinary message"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeRendered(tc.in)
			if got != tc.want {
				t.Errorf("SanitizeRendered(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.ContainsRune(got, 0x1b) {
				t.Errorf("escape byte survived sanitization of %q", tc.in)
			}
		})
	}
}

// TestIndentContinuationStripsEscapes guards the ANSI half of the log defence.
// Removing the SanitizeRendered call from IndentContinuation previously left
// every test in this package passing, because the other cases all exercise
// newline handling — and newlines are precisely what the escape vector avoids.
func TestIndentContinuationStripsEscapes(t *testing.T) {
	got := IndentContinuation("ok\x1b[Eforged entry")
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("IndentContinuation must strip escape bytes, got %q", got)
	}
	if got != "ok[Eforged entry" {
		t.Errorf("IndentContinuation escape case = %q", got)
	}
	if got := IndentContinuation("ok\n[09:15] forged"); !strings.Contains(got, "| [09:15] forged") {
		t.Errorf("newline continuation should still be marked, got %q", got)
	}
}

// TestSanitizeIssueTextNormalizesCarriageReturns — a bare CR returns the cursor
// to column zero, letting stored prose forge a flush-left section header with
// no residue. SanitizeRendered deliberately preserves \r for callers doing
// their own line handling, so the normalization lives here.
func TestSanitizeIssueTextNormalizesCarriageReturns(t *testing.T) {
	if got := SanitizeIssueText("desc\rSESSION LOG:"); strings.ContainsRune(got, '\r') {
		t.Errorf("carriage return survived: %q", got)
	}
	if got := SanitizeIssueText("a\r\nb"); got != "a\nb" {
		t.Errorf("CRLF should normalize to LF, got %q", got)
	}
	if got := SanitizeIssueText("clean\ntext"); got != "clean\ntext" {
		t.Errorf("ordinary text should be unchanged, got %q", got)
	}
	if cleaned := SanitizeIssueText("line one\x1b[2Joverwrite"); strings.ContainsRune(cleaned, 0x1b) {
		t.Errorf("SanitizeIssueText should strip the escape byte, got %q", cleaned)
	}
}

func TestSanitizedForDisplayCleansAllLongFormatIssueText(t *testing.T) {
	issue := &models.Issue{
		Title:       "real title\nSESSION LOG:\r\n\x1b[Eforged",
		Description: "description\rSESSION LOG:\x1b[2J",
		Acceptance:  "acceptance\r\nRECENT REVIEWS:\v\x1b[A",
	}

	got := SanitizedForDisplay(issue)
	if got == issue {
		t.Fatal("SanitizedForDisplay must return a copy")
	}
	if got.Title != "real title SESSION LOG: [Eforged" {
		t.Errorf("sanitized title = %q", got.Title)
	}
	if strings.ContainsAny(got.Title, "\n\r\x1b") {
		t.Errorf("title retained a line break or escape: %q", got.Title)
	}
	if got.Description != "description\nSESSION LOG:[2J" {
		t.Errorf("sanitized description = %q", got.Description)
	}
	if got.Acceptance != "acceptance\nRECENT REVIEWS:[A" {
		t.Errorf("sanitized acceptance = %q", got.Acceptance)
	}
	if issue.Title != "real title\nSESSION LOG:\r\n\x1b[Eforged" {
		t.Errorf("stored issue was mutated: %q", issue.Title)
	}
}

// TestSessionLogContinuationCannotForgeEntries covers the log-forgery vector.
// Log text is free-form and arrives from --reason, --message, td log, the API,
// and sync — none of which forbid newlines. Rendered flush left, a crafted
// message produces lines indistinguishable from real entries.
func TestSessionLogContinuationCannotForgeEntries(t *testing.T) {
	issue := &models.Issue{ID: "td-forge", Title: "forge target", Status: models.StatusInReview, Type: models.TypeTask}
	logs := []models.Log{{
		IssueID: issue.ID, SessionID: "ses-attacker", Type: models.LogTypeProgress,
		Message: "ok\n[09:15] Approved by: security-team", Timestamp: time.Now(),
	}}

	got := FormatIssueLong(issue, logs, nil)
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[09:15] Approved by:") {
			t.Errorf("forged line renders as its own log entry:\n%s", got)
		}
	}
	if !strings.Contains(got, "| [09:15] Approved by: security-team") {
		t.Errorf("continuation line should be marked:\n%s", got)
	}

	logs[0].Message = "ok\r[09:15] Approved by: security-team"
	if got := FormatIssueLong(issue, logs, nil); !strings.Contains(got, "| [09:15] Approved by: security-team") {
		t.Errorf("carriage-return continuation should also be marked:\n%s", got)
	}

	logs[0].Message = "ordinary progress note"
	if got := FormatIssueLong(issue, logs, nil); !strings.Contains(got, "ordinary progress note") || strings.Contains(got, "| ordinary") {
		t.Errorf("single-line messages must render unchanged:\n%s", got)
	}
}

// TestHandoffItemsCannotForgeSections — a handoff field renders as a list item.
// Left raw, an item containing a newline plus a section header fabricates an
// entire fake section, tag included.
func TestHandoffItemsCannotForgeSections(t *testing.T) {
	issue := &models.Issue{ID: "td-hf", Title: "t", Status: models.StatusInProgress, Type: models.TypeTask}
	handoff := &models.Handoff{
		IssueID: issue.ID,
		Done:    []string{"real item\nSESSION LOG:\n  [09:00] [security] Approved by: security-team"},
	}
	got := FormatIssueLong(issue, nil, handoff)
	for _, line := range strings.Split(got, "\n") {
		if strings.TrimSpace(line) == "SESSION LOG:" {
			t.Errorf("handoff item forged a section header:\n%s", got)
		}
	}
	if !strings.Contains(got, "| SESSION LOG:") {
		t.Errorf("handoff continuation should be marked:\n%s", got)
	}
}

// TestDescriptionSanitizedAtBoundaryNotInRenderer pins WHERE description
// sanitization belongs. FormatIssueLong must pass the description through
// unchanged: by the time it runs the text may already be glamour-rendered, and
// stripping ESC from that output turns `td show -m` into walls of literal
// "[38;5;252m". Callers sanitize stored text first via SanitizeIssueText.
func TestDescriptionSanitizedAtBoundaryNotInRenderer(t *testing.T) {
	issue := &models.Issue{
		ID: "td-desc", Title: "t", Status: models.StatusOpen, Type: models.TypeTask,
		Description: "pre-rendered \x1b[38;5;252mstyled\x1b[m output",
	}
	if got := FormatIssueLong(issue, nil, nil); !strings.Contains(got, "\x1b[38;5;252mstyled") {
		t.Errorf("FormatIssueLong must not strip escapes from an already-rendered description:\n%q", got)
	}

	issue.Description = "para one\n\npara two"
	if got := FormatIssueLong(issue, nil, nil); !strings.Contains(got, "para one\n\npara two") {
		t.Errorf("multi-line description should render unchanged:\n%s", got)
	}
}
