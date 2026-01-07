package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogAgentError(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		errMsg    string
		sessionID string
		wantErr   bool
	}{
		{
			name:      "basic error logging",
			args:      []string{"create", "task"},
			errMsg:    "permission denied",
			sessionID: "ses_123abc",
			wantErr:   false,
		},
		{
			name:      "empty args",
			args:      []string{},
			errMsg:    "unknown command",
			sessionID: "ses_456def",
			wantErr:   false,
		},
		{
			name:      "empty session ID",
			args:      []string{"list"},
			errMsg:    "database not initialized",
			sessionID: "",
			wantErr:   false,
		},
		{
			name:      "error message with special characters",
			args:      []string{"start", "id_with_dash"},
			errMsg:    "connection failed: network error (timeout after 30s)",
			sessionID: "ses_789ghi",
			wantErr:   false,
		},
		{
			name:      "error message with quotes and escapes",
			args:      []string{"update", "id"},
			errMsg:    `failed to parse: "invalid" value`,
			sessionID: "ses_escape",
			wantErr:   false,
		},
		{
			name:      "multiple args",
			args:      []string{"approve", "id1", "--comment", "looks good", "--force"},
			errMsg:    "not approved by reviewer",
			sessionID: "ses_multi",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			// Initialize the .todos directory
			todosDir := filepath.Join(baseDir, ".todos")
			if err := os.MkdirAll(todosDir, 0755); err != nil {
				t.Fatalf("failed to create .todos dir: %v", err)
			}

			err := LogAgentError(baseDir, tt.args, tt.errMsg, tt.sessionID)
			if (err != nil) != tt.wantErr {
				t.Errorf("LogAgentError() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify error was logged
			errPath := filepath.Join(baseDir, agentErrorsFile)
			if _, err := os.Stat(errPath); os.IsNotExist(err) {
				if !tt.wantErr {
					t.Error("error file not created")
				}
			}
		})
	}
}

func TestLogAgentErrorNoTodosDir(t *testing.T) {
	// When .todos directory doesn't exist, LogAgentError should silently return nil
	baseDir := t.TempDir()
	// Do not create .todos directory

	err := LogAgentError(baseDir, []string{"test"}, "error message", "ses_123")
	if err != nil {
		t.Errorf("LogAgentError() should silently drop when .todos doesn't exist, got error: %v", err)
	}

	// Verify no error file was created
	errPath := filepath.Join(baseDir, agentErrorsFile)
	if _, err := os.Stat(errPath); !os.IsNotExist(err) {
		t.Error("error file should not be created when .todos doesn't exist")
	}
}

func TestReadAgentErrors(t *testing.T) {
	baseDir := t.TempDir()
	todosDir := filepath.Join(baseDir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatalf("failed to create .todos dir: %v", err)
	}

	tests := []struct {
		name       string
		logErrors  []struct {
			args      []string
			errMsg    string
			sessionID string
		}
		wantCount int
	}{
		{
			name:      "single error",
			logErrors: []struct {
				args      []string
				errMsg    string
				sessionID string
			}{
				{[]string{"create"}, "error1", "ses_1"},
			},
			wantCount: 1,
		},
		{
			name: "multiple errors",
			logErrors: []struct {
				args      []string
				errMsg    string
				sessionID string
			}{
				{[]string{"create"}, "error1", "ses_1"},
				{[]string{"update"}, "error2", "ses_2"},
				{[]string{"delete"}, "error3", "ses_1"},
			},
			wantCount: 3,
		},
		{
			name:      "no errors",
			logErrors: []struct {
				args      []string
				errMsg    string
				sessionID string
			}{},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fresh directory for each test
			testDir := t.TempDir()
			testTodosDir := filepath.Join(testDir, ".todos")
			if err := os.MkdirAll(testTodosDir, 0755); err != nil {
				t.Fatalf("failed to create .todos dir: %v", err)
			}

			// Log errors
			for _, e := range tt.logErrors {
				err := LogAgentError(testDir, e.args, e.errMsg, e.sessionID)
				if err != nil {
					t.Fatalf("LogAgentError() failed: %v", err)
				}
			}

			// Read errors
			errors, err := ReadAgentErrors(testDir)
			if err != nil {
				t.Fatalf("ReadAgentErrors() failed: %v", err)
			}

			if len(errors) != tt.wantCount {
				t.Errorf("ReadAgentErrors() got %d errors, want %d", len(errors), tt.wantCount)
			}

			// Verify error content
			for i, e := range errors {
				if tt.logErrors[i].errMsg != e.Error {
					t.Errorf("error %d: got %q, want %q", i, e.Error, tt.logErrors[i].errMsg)
				}
				if tt.logErrors[i].sessionID != e.SessionID {
					t.Errorf("sessionID %d: got %q, want %q", i, e.SessionID, tt.logErrors[i].sessionID)
				}
			}
		})
	}
}

func TestReadAgentErrorsNonexistent(t *testing.T) {
	// When error file doesn't exist, should return empty slice
	baseDir := t.TempDir()

	errors, err := ReadAgentErrors(baseDir)
	if err != nil {
		t.Errorf("ReadAgentErrors() failed: %v", err)
	}

	if len(errors) != 0 {
		t.Errorf("ReadAgentErrors() should return empty slice for nonexistent file, got %d errors", len(errors))
	}
}

func TestReadAgentErrorsFiltered(t *testing.T) {
	baseDir := t.TempDir()
	todosDir := filepath.Join(baseDir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatalf("failed to create .todos dir: %v", err)
	}

	// Log some errors with different timestamps and sessions
	// We can't directly control timestamps, so we'll log errors and verify filtering
	tests := []struct {
		name       string
		sessionID  string
		since      time.Time
		limit      int
		wantMinErr int
	}{
		{
			name:       "no filter - limit 2",
			sessionID:  "",
			since:      time.Time{},
			limit:      2,
			wantMinErr: 1, // At least 1 error should be returned
		},
		{
			name:       "filter by session",
			sessionID:  "ses_1",
			since:      time.Time{},
			limit:      0,
			wantMinErr: 1,
		},
		{
			name:       "filter with limit 1",
			sessionID:  "",
			since:      time.Time{},
			limit:      1,
			wantMinErr: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := t.TempDir()
			testTodosDir := filepath.Join(testDir, ".todos")
			if err := os.MkdirAll(testTodosDir, 0755); err != nil {
				t.Fatalf("failed to create .todos dir: %v", err)
			}

			// Log test errors
			LogAgentError(testDir, []string{"cmd1"}, "error1", "ses_1")
			LogAgentError(testDir, []string{"cmd2"}, "error2", "ses_2")
			LogAgentError(testDir, []string{"cmd3"}, "error3", "ses_1")

			errors, err := ReadAgentErrorsFiltered(testDir, tt.sessionID, tt.since, tt.limit)
			if err != nil {
				t.Fatalf("ReadAgentErrorsFiltered() failed: %v", err)
			}

			if len(errors) < tt.wantMinErr {
				t.Errorf("ReadAgentErrorsFiltered() got %d errors, want at least %d", len(errors), tt.wantMinErr)
			}

			if tt.limit > 0 && len(errors) > tt.limit {
				t.Errorf("ReadAgentErrorsFiltered() limit not respected: got %d, limit %d", len(errors), tt.limit)
			}

			// Check session filter
			if tt.sessionID != "" {
				for _, e := range errors {
					if e.SessionID != tt.sessionID {
						t.Errorf("ReadAgentErrorsFiltered() session filter failed: got %q, want %q", e.SessionID, tt.sessionID)
					}
				}
			}
		})
	}
}

func TestReadAgentErrorsFilteredNewest(t *testing.T) {
	// Verify that errors are returned in reverse chronological order (newest first)
	testDir := t.TempDir()
	testTodosDir := filepath.Join(testDir, ".todos")
	if err := os.MkdirAll(testTodosDir, 0755); err != nil {
		t.Fatalf("failed to create .todos dir: %v", err)
	}

	// Log multiple errors
	LogAgentError(testDir, []string{"cmd1"}, "error1", "ses_1")
	LogAgentError(testDir, []string{"cmd2"}, "error2", "ses_1")
	LogAgentError(testDir, []string{"cmd3"}, "error3", "ses_1")

	errors, err := ReadAgentErrorsFiltered(testDir, "", time.Time{}, 0)
	if err != nil {
		t.Fatalf("ReadAgentErrorsFiltered() failed: %v", err)
	}

	if len(errors) < 2 {
		t.Fatalf("expected at least 2 errors, got %d", len(errors))
	}

	// First error should be newer than second
	if errors[0].Timestamp.Before(errors[1].Timestamp) {
		t.Error("ReadAgentErrorsFiltered() should return newest first, got oldest first")
	}
}

func TestClearAgentErrors(t *testing.T) {
	tests := []struct {
		name      string
		hasErrors bool
	}{
		{
			name:      "clear existing errors",
			hasErrors: true,
		},
		{
			name:      "clear nonexistent file",
			hasErrors: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			todosDir := filepath.Join(baseDir, ".todos")
			if err := os.MkdirAll(todosDir, 0755); err != nil {
				t.Fatalf("failed to create .todos dir: %v", err)
			}

			if tt.hasErrors {
				err := LogAgentError(baseDir, []string{"test"}, "error", "ses_1")
				if err != nil {
					t.Fatalf("LogAgentError() failed: %v", err)
				}
			}

			err := ClearAgentErrors(baseDir)
			if err != nil {
				t.Errorf("ClearAgentErrors() failed: %v", err)
			}

			// Verify file is gone
			errPath := filepath.Join(baseDir, agentErrorsFile)
			if _, err := os.Stat(errPath); !os.IsNotExist(err) {
				t.Error("error file should not exist after clear")
			}
		})
	}
}

func TestCountAgentErrors(t *testing.T) {
	tests := []struct {
		name    string
		numErrs int
	}{
		{
			name:    "no errors",
			numErrs: 0,
		},
		{
			name:    "single error",
			numErrs: 1,
		},
		{
			name:    "multiple errors",
			numErrs: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			todosDir := filepath.Join(baseDir, ".todos")
			if err := os.MkdirAll(todosDir, 0755); err != nil {
				t.Fatalf("failed to create .todos dir: %v", err)
			}

			// Log errors
			for i := 0; i < tt.numErrs; i++ {
				err := LogAgentError(baseDir, []string{"cmd"}, "error", "ses_1")
				if err != nil {
					t.Fatalf("LogAgentError() failed: %v", err)
				}
			}

			count, err := CountAgentErrors(baseDir)
			if err != nil {
				t.Errorf("CountAgentErrors() failed: %v", err)
			}

			if count != tt.numErrs {
				t.Errorf("CountAgentErrors() got %d, want %d", count, tt.numErrs)
			}
		})
	}
}

func TestCountAgentErrorsNonexistent(t *testing.T) {
	baseDir := t.TempDir()

	count, err := CountAgentErrors(baseDir)
	if err != nil {
		t.Errorf("CountAgentErrors() failed: %v", err)
	}

	if count != 0 {
		t.Errorf("CountAgentErrors() should return 0 for nonexistent file, got %d", count)
	}
}

func TestParseAgentErrors(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantLen int
		wantErr bool
	}{
		{
			name:    "single error",
			data:    []byte(`{"ts":"2024-01-01T00:00:00Z","args":["cmd"],"error":"err1","session":"ses_1"}`),
			wantLen: 1,
			wantErr: false,
		},
		{
			name:    "multiple errors",
			data:    []byte(`{"ts":"2024-01-01T00:00:00Z","args":["cmd1"],"error":"err1","session":"ses_1"}` + "\n" + `{"ts":"2024-01-02T00:00:00Z","args":["cmd2"],"error":"err2","session":"ses_2"}`),
			wantLen: 2,
			wantErr: false,
		},
		{
			name:    "empty data",
			data:    []byte(""),
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "invalid json ignored",
			data:    []byte(`{"ts":"2024-01-01T00:00:00Z","args":["cmd"],"error":"err1","session":"ses_1"}` + "\n" + `invalid json` + "\n" + `{"ts":"2024-01-02T00:00:00Z","args":["cmd2"],"error":"err2","session":"ses_2"}`),
			wantLen: 2,
			wantErr: false,
		},
		{
			name:    "trailing newline",
			data:    []byte(`{"ts":"2024-01-01T00:00:00Z","args":["cmd"],"error":"err1","session":"ses_1"}` + "\n"),
			wantLen: 1,
			wantErr: false,
		},
		{
			name:    "empty args array",
			data:    []byte(`{"ts":"2024-01-01T00:00:00Z","args":[],"error":"err1","session":"ses_1"}`),
			wantLen: 1,
			wantErr: false,
		},
		{
			name:    "empty session field",
			data:    []byte(`{"ts":"2024-01-01T00:00:00Z","args":["cmd"],"error":"err1","session":""}`),
			wantLen: 1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, err := parseAgentErrors(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAgentErrors() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(errors) != tt.wantLen {
				t.Errorf("parseAgentErrors() got %d errors, want %d", len(errors), tt.wantLen)
			}
		})
	}
}

func TestAgentErrorStructure(t *testing.T) {
	// Test that AgentError properly stores and preserves data
	baseDir := t.TempDir()
	todosDir := filepath.Join(baseDir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatalf("failed to create .todos dir: %v", err)
	}

	args := []string{"approve", "issue_123", "--force"}
	errMsg := `validation failed: "invalid state"`
	sessionID := "ses_test_abc"

	if err := LogAgentError(baseDir, args, errMsg, sessionID); err != nil {
		t.Fatalf("LogAgentError() failed: %v", err)
	}

	errors, err := ReadAgentErrors(baseDir)
	if err != nil {
		t.Fatalf("ReadAgentErrors() failed: %v", err)
	}

	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}

	e := errors[0]
	if e.Error != errMsg {
		t.Errorf("error message: got %q, want %q", e.Error, errMsg)
	}
	if e.SessionID != sessionID {
		t.Errorf("session ID: got %q, want %q", e.SessionID, sessionID)
	}
	if len(e.Args) != len(args) {
		t.Errorf("args length: got %d, want %d", len(e.Args), len(args))
	}
	for i, a := range e.Args {
		if a != args[i] {
			t.Errorf("arg %d: got %q, want %q", i, a, args[i])
		}
	}

	// Verify timestamp is set and reasonable
	if e.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
	if e.Timestamp.After(time.Now().UTC().Add(time.Second)) {
		t.Error("timestamp is in the future")
	}
}
