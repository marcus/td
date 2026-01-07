package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
)

func TestErrorsCommandBasic(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Log some errors
	if err := db.LogAgentError(baseDir, []string{"create", "task"}, "permission denied", "ses_1"); err != nil {
		t.Fatalf("LogAgentError failed: %v", err)
	}
	if err := db.LogAgentError(baseDir, []string{"update", "id"}, "not found", "ses_2"); err != nil {
		t.Fatalf("LogAgentError failed: %v", err)
	}

	// Test reading errors
	errors, err := db.ReadAgentErrors(baseDir)
	if err != nil {
		t.Errorf("ReadAgentErrors failed: %v", err)
	}

	if len(errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errors))
	}

	if errors[0].Error != "permission denied" {
		t.Errorf("first error: got %q, want %q", errors[0].Error, "permission denied")
	}

	if errors[1].Error != "not found" {
		t.Errorf("second error: got %q, want %q", errors[1].Error, "not found")
	}
}

func TestErrorsCommandClear(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Log an error
	if err := db.LogAgentError(baseDir, []string{"test"}, "error", "ses_1"); err != nil {
		t.Fatalf("LogAgentError failed: %v", err)
	}

	// Verify error exists
	count, err := db.CountAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("CountAgentErrors failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 error before clear, got %d", count)
	}

	// Clear errors
	if err := db.ClearAgentErrors(baseDir); err != nil {
		t.Fatalf("ClearAgentErrors failed: %v", err)
	}

	// Verify errors are gone
	count, err = db.CountAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("CountAgentErrors failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 errors after clear, got %d", count)
	}
}

func TestErrorsCommandCount(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	tests := []struct {
		name     string
		numErrs  int
		wantCnt  int
	}{
		{
			name:    "no errors",
			numErrs: 0,
			wantCnt: 0,
		},
		{
			name:    "single error",
			numErrs: 1,
			wantCnt: 1,
		},
		{
			name:    "multiple errors",
			numErrs: 5,
			wantCnt: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := t.TempDir()
			testDb, err := db.Initialize(testDir)
			if err != nil {
				t.Fatalf("Failed to initialize database: %v", err)
			}
			defer testDb.Close()

			// Log errors
			for i := 0; i < tt.numErrs; i++ {
				if err := db.LogAgentError(testDir, []string{"cmd"}, "error", "ses_1"); err != nil {
					t.Fatalf("LogAgentError failed: %v", err)
				}
			}

			count, err := db.CountAgentErrors(testDir)
			if err != nil {
				t.Fatalf("CountAgentErrors failed: %v", err)
			}

			if count != tt.wantCnt {
				t.Errorf("expected %d errors, got %d", tt.wantCnt, count)
			}
		})
	}
}

func TestErrorsCommandSessionFilter(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Log errors with different sessions
	if err := db.LogAgentError(baseDir, []string{"cmd1"}, "error1", "ses_1"); err != nil {
		t.Fatalf("LogAgentError failed: %v", err)
	}
	if err := db.LogAgentError(baseDir, []string{"cmd2"}, "error2", "ses_2"); err != nil {
		t.Fatalf("LogAgentError failed: %v", err)
	}
	if err := db.LogAgentError(baseDir, []string{"cmd3"}, "error3", "ses_1"); err != nil {
		t.Fatalf("LogAgentError failed: %v", err)
	}

	// Filter by session
	errors, err := db.ReadAgentErrorsFiltered(baseDir, "ses_1", time.Time{}, 0)
	if err != nil {
		t.Fatalf("ReadAgentErrorsFiltered failed: %v", err)
	}

	if len(errors) != 2 {
		t.Errorf("expected 2 errors for ses_1, got %d", len(errors))
	}

	for _, e := range errors {
		if e.SessionID != "ses_1" {
			t.Errorf("session filter failed: got %q, want ses_1", e.SessionID)
		}
	}
}

func TestErrorsCommandLimitFilter(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Log multiple errors
	for i := 0; i < 5; i++ {
		if err := db.LogAgentError(baseDir, []string{"cmd"}, "error", "ses_1"); err != nil {
			t.Fatalf("LogAgentError failed: %v", err)
		}
	}

	// Test with different limits
	tests := []struct {
		name    string
		limit   int
		wantMax int
	}{
		{
			name:    "limit 1",
			limit:   1,
			wantMax: 1,
		},
		{
			name:    "limit 3",
			limit:   3,
			wantMax: 3,
		},
		{
			name:    "limit 10 (more than available)",
			limit:   10,
			wantMax: 5,
		},
		{
			name:    "no limit",
			limit:   0,
			wantMax: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, err := db.ReadAgentErrorsFiltered(baseDir, "", time.Time{}, tt.limit)
			if err != nil {
				t.Fatalf("ReadAgentErrorsFiltered failed: %v", err)
			}

			if len(errors) != tt.wantMax {
				t.Errorf("expected max %d errors, got %d", tt.wantMax, len(errors))
			}
		})
	}
}

func TestFormatArgsJSON(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "empty args",
			args: []string{},
			want: "",
		},
		{
			name: "single arg",
			args: []string{"create"},
			want: `"create"`,
		},
		{
			name: "multiple args",
			args: []string{"create", "task"},
			want: `"create","task"`,
		},
		{
			name: "args with quotes",
			args: []string{`cmd`, `arg"with"quotes`},
			want: `"cmd","arg\"with\"quotes"`,
		},
		{
			name: "args with backslashes",
			args: []string{`path\to\file`},
			want: `"path\\to\\file"`,
		},
		{
			name: "args with newlines",
			args: []string{`line1\nline2`},
			want: `"line1\\nline2"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatArgsJSON(tt.args)
			if got != tt.want {
				t.Errorf("formatArgsJSON() got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEscapeJSON(t *testing.T) {
	tests := []struct {
		name string
		str  string
		want string
	}{
		{
			name: "simple string",
			str:  "hello",
			want: "hello",
		},
		{
			name: "string with quotes",
			str:  `hello "world"`,
			want: `hello \"world\"`,
		},
		{
			name: "string with backslash",
			str:  `path\to\file`,
			want: `path\\to\\file`,
		},
		{
			name: "string with newline",
			str:  "line1\nline2",
			want: `line1\nline2`,
		},
		{
			name: "string with tab",
			str:  "col1\tcol2",
			want: `col1\tcol2`,
		},
		{
			name: "complex string",
			str:  "error: \"connection failed\nretry after\t5s\"",
			want: `error: \"connection failed\nretry after\t5s\"`,
		},
		{
			name: "multiple backslashes",
			str:  `\\`,
			want: `\\\\`,
		},
		{
			name: "empty string",
			str:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeJSON(tt.str)
			if got != tt.want {
				t.Errorf("escapeJSON() got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorsCommandJSONOutput(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Log an error with special characters
	if err := db.LogAgentError(baseDir, []string{`cmd`, `arg"quoted"`}, `error "msg" with\nnewline`, "ses_1"); err != nil {
		t.Fatalf("LogAgentError failed: %v", err)
	}

	// Read and format as JSON
	errors, err := db.ReadAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("ReadAgentErrors failed: %v", err)
	}

	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}

	// Format as JSON (mimicking the command output)
	e := errors[0]
	jsonOut := bytes.NewBufferString("")

	// Mimic the JSON output from the command
	jsonLine := `{"ts":"` + e.Timestamp.Format(time.RFC3339) + `","args":[` +
		formatArgsJSON(e.Args) + `],"error":"` +
		escapeJSON(e.Error) + `","session":"` + e.SessionID + `"}`

	jsonOut.WriteString(jsonLine + "\n")

	// Verify it's valid JSON-like format
	outputStr := jsonOut.String()
	if !strings.Contains(outputStr, `"ts":`) {
		t.Error("JSON output missing timestamp field")
	}
	if !strings.Contains(outputStr, `"args":`) {
		t.Error("JSON output missing args field")
	}
	if !strings.Contains(outputStr, `"error":`) {
		t.Error("JSON output missing error field")
	}
	if !strings.Contains(outputStr, `"session":"ses_1"`) {
		t.Error("JSON output missing or incorrect session field")
	}
}

func TestErrorsCommandReadFilter(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Log errors
	if err := db.LogAgentError(baseDir, []string{"cmd1"}, "error1", "ses_1"); err != nil {
		t.Fatalf("LogAgentError failed: %v", err)
	}
	if err := db.LogAgentError(baseDir, []string{"cmd2"}, "error2", "ses_2"); err != nil {
		t.Fatalf("LogAgentError failed: %v", err)
	}
	if err := db.LogAgentError(baseDir, []string{"cmd3"}, "error3", "ses_1"); err != nil {
		t.Fatalf("LogAgentError failed: %v", err)
	}

	// Test filtering by session and limit
	errors, err := db.ReadAgentErrorsFiltered(baseDir, "ses_1", time.Time{}, 1)
	if err != nil {
		t.Fatalf("ReadAgentErrorsFiltered failed: %v", err)
	}

	if len(errors) != 1 {
		t.Errorf("expected 1 error with ses_1 filter and limit 1, got %d", len(errors))
	}

	if errors[0].SessionID != "ses_1" {
		t.Errorf("session filter: got %q, want ses_1", errors[0].SessionID)
	}
}

func TestErrorsCommandArgsPreservation(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "simple command",
			args: []string{"create"},
		},
		{
			name: "command with id",
			args: []string{"update", "issue_abc123"},
		},
		{
			name: "command with flags",
			args: []string{"approve", "id", "--force", "--comment", "looks good"},
		},
		{
			name: "command with special chars in args",
			args: []string{`cmd`, `arg"with"quotes`, `path\with\slashes`},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testDir := t.TempDir()
			testDb, err := db.Initialize(testDir)
			if err != nil {
				t.Fatalf("Failed to initialize database: %v", err)
			}
			defer testDb.Close()

			if err := db.LogAgentError(testDir, tc.args, "error", "ses_1"); err != nil {
				t.Fatalf("LogAgentError failed: %v", err)
			}

			errors, err := db.ReadAgentErrors(testDir)
			if err != nil {
				t.Fatalf("ReadAgentErrors failed: %v", err)
			}

			if len(errors) != 1 {
				t.Fatalf("expected 1 error, got %d", len(errors))
			}

			if len(errors[0].Args) != len(tc.args) {
				t.Errorf("args length: got %d, want %d", len(errors[0].Args), len(tc.args))
			}

			for i, arg := range tc.args {
				if errors[0].Args[i] != arg {
					t.Errorf("arg %d: got %q, want %q", i, errors[0].Args[i], arg)
				}
			}
		})
	}
}

func TestErrorsCommandEmptyLog(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Test reading when no errors exist
	errors, err := db.ReadAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("ReadAgentErrors failed: %v", err)
	}

	if len(errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errors))
	}

	// Test count when no errors exist
	count, err := db.CountAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("CountAgentErrors failed: %v", err)
	}

	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

func TestErrorsCommandTimestampOrdering(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Log multiple errors
	errorCount := 3
	for i := 0; i < errorCount; i++ {
		if err := db.LogAgentError(baseDir, []string{"cmd"}, "error", "ses_1"); err != nil {
			t.Fatalf("LogAgentError failed: %v", err)
		}
	}

	// Read without filter (should be in order)
	errors, err := db.ReadAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("ReadAgentErrors failed: %v", err)
	}

	if len(errors) != errorCount {
		t.Fatalf("expected %d errors, got %d", errorCount, len(errors))
	}

	// Verify timestamps are set (and in order)
	for i := 0; i < len(errors)-1; i++ {
		if errors[i].Timestamp.IsZero() {
			t.Errorf("error %d has zero timestamp", i)
		}
		// Timestamps should be monotonically non-decreasing (or equal)
		if errors[i].Timestamp.After(errors[i+1].Timestamp) {
			t.Errorf("timestamps not in order: error %d after error %d", i, i+1)
		}
	}
}

func TestErrorsCommandFiltering(t *testing.T) {
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	tests := []struct {
		name      string
		sessionID string
		limit     int
		wantMin   int
		wantMax   int
	}{
		{
			name:      "no filters",
			sessionID: "",
			limit:     0,
			wantMin:   0,
			wantMax:   3,
		},
		{
			name:      "with limit",
			sessionID: "",
			limit:     2,
			wantMin:   0,
			wantMax:   2,
		},
		{
			name:      "by session",
			sessionID: "ses_1",
			limit:     0,
			wantMin:   0,
			wantMax:   2,
		},
		{
			name:      "session and limit",
			sessionID: "ses_1",
			limit:     1,
			wantMin:   0,
			wantMax:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := t.TempDir()
			testDb, err := db.Initialize(testDir)
			if err != nil {
				t.Fatalf("Failed to initialize database: %v", err)
			}
			defer testDb.Close()

			// Log test errors
			if err := db.LogAgentError(testDir, []string{"cmd1"}, "error1", "ses_1"); err != nil {
				t.Fatalf("LogAgentError failed: %v", err)
			}
			if err := db.LogAgentError(testDir, []string{"cmd2"}, "error2", "ses_2"); err != nil {
				t.Fatalf("LogAgentError failed: %v", err)
			}
			if err := db.LogAgentError(testDir, []string{"cmd3"}, "error3", "ses_1"); err != nil {
				t.Fatalf("LogAgentError failed: %v", err)
			}

			errors, err := db.ReadAgentErrorsFiltered(testDir, tt.sessionID, time.Time{}, tt.limit)
			if err != nil {
				t.Fatalf("ReadAgentErrorsFiltered failed: %v", err)
			}

			if len(errors) < tt.wantMin || len(errors) > tt.wantMax {
				t.Errorf("expected %d-%d errors, got %d", tt.wantMin, tt.wantMax, len(errors))
			}
		})
	}
}

func TestErrorsCommandNoTodosDir(t *testing.T) {
	// When .todos directory doesn't exist, operations should handle gracefully
	baseDir := t.TempDir()
	// Don't create .todos directory

	// LogAgentError should silently return nil
	err := db.LogAgentError(baseDir, []string{"cmd"}, "error", "ses_1")
	if err != nil {
		t.Errorf("LogAgentError should silently drop when .todos doesn't exist, got error: %v", err)
	}

	// ReadAgentErrors should return empty slice
	errors, err := db.ReadAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("ReadAgentErrors failed: %v", err)
	}

	if len(errors) != 0 {
		t.Errorf("ReadAgentErrors should return empty slice, got %d errors", len(errors))
	}

	// CountAgentErrors should return 0
	count, err := db.CountAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("CountAgentErrors failed: %v", err)
	}

	if count != 0 {
		t.Errorf("CountAgentErrors should return 0, got %d", count)
	}
}

func TestErrorLoggerIntegration(t *testing.T) {
	// Integration test: log multiple errors and verify all operations work together
	baseDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Log diverse errors
	testErrors := []struct {
		args      []string
		errMsg    string
		sessionID string
	}{
		{[]string{"create"}, "permission denied", "ses_abc"},
		{[]string{"update", "id"}, "not found", "ses_def"},
		{[]string{"delete"}, "operation failed", "ses_abc"},
		{[]string{"list", "--filter", "status=open"}, "invalid filter", "ses_ghi"},
	}

	for _, te := range testErrors {
		if err := db.LogAgentError(baseDir, te.args, te.errMsg, te.sessionID); err != nil {
			t.Fatalf("LogAgentError failed: %v", err)
		}
	}

	// Test count
	count, err := db.CountAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("CountAgentErrors failed: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4 errors, got %d", count)
	}

	// Test read all
	errors, err := db.ReadAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("ReadAgentErrors failed: %v", err)
	}
	if len(errors) != 4 {
		t.Errorf("expected 4 errors, got %d", len(errors))
	}

	// Test session filter
	sesErrors, err := db.ReadAgentErrorsFiltered(baseDir, "ses_abc", time.Time{}, 0)
	if err != nil {
		t.Fatalf("ReadAgentErrorsFiltered failed: %v", err)
	}
	if len(sesErrors) != 2 {
		t.Errorf("expected 2 errors for ses_abc, got %d", len(sesErrors))
	}

	// Test limit
	limitErrors, err := db.ReadAgentErrorsFiltered(baseDir, "", time.Time{}, 2)
	if err != nil {
		t.Fatalf("ReadAgentErrorsFiltered with limit failed: %v", err)
	}
	if len(limitErrors) != 2 {
		t.Errorf("expected 2 errors with limit, got %d", len(limitErrors))
	}

	// Test clear
	if err := db.ClearAgentErrors(baseDir); err != nil {
		t.Fatalf("ClearAgentErrors failed: %v", err)
	}

	count, err = db.CountAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("CountAgentErrors after clear failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 errors after clear, got %d", count)
	}
}
