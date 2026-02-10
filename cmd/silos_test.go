package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
)

func TestSilosCommand(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	// Insert test data
	conn := database.Conn()
	_, err = conn.Exec(`
		INSERT INTO issues (id, title, implementer_session) VALUES ('issue1', 'Test 1', 'session1');
		INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file1', 'issue1', 'main.go');
		INSERT INTO issues (id, title, implementer_session) VALUES ('issue2', 'Test 2', 'session1');
		INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file2', 'issue2', 'main.go');
		INSERT INTO issues (id, title, implementer_session) VALUES ('issue3', 'Test 3', 'session2');
		INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file3', 'issue3', 'utils.go');
	`)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Temporarily set base dir for command execution
	oldBaseDir := baseDirOverride
	p := tmpDir
	baseDirOverride = &p
	defer func() { baseDirOverride = oldBaseDir }()

	// Test table output (default)
	silosFormat = "table"
	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runSilos()

	w.Close()
	os.Stdout = oldOut

	if err != nil {
		t.Fatalf("runSilos failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "KNOWLEDGE SILO ANALYSIS") {
		t.Error("expected output to contain title")
	}

	if !strings.Contains(output, "COVERAGE SUMMARY") {
		t.Error("expected output to contain summary section")
	}
}

func TestSilosCommandJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	// Insert test data
	conn := database.Conn()
	_, err = conn.Exec(`
		INSERT INTO issues (id, title, implementer_session) VALUES ('issue1', 'Test 1', 'session1');
		INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file1', 'issue1', 'main.go');
		INSERT INTO issues (id, title, implementer_session) VALUES ('issue2', 'Test 2', 'session2');
		INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file2', 'issue2', 'utils.go');
	`)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Set base dir
	oldBaseDir := baseDirOverride
	p := tmpDir
	baseDirOverride = &p
	defer func() { baseDirOverride = oldBaseDir }()

	// Test JSON output
	silosFormat = "json"
	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runSilos()

	w.Close()
	os.Stdout = oldOut

	if err != nil {
		t.Fatalf("runSilos failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Parse JSON
	var result map[string]interface{}
	err = json.Unmarshal([]byte(output), &result)
	if err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if _, ok := result["silo_risk_score"]; !ok {
		t.Error("expected silo_risk_score in JSON output")
	}

	if _, ok := result["author_contributions"]; !ok {
		t.Error("expected author_contributions in JSON output")
	}
}

func TestSilosCommandCSV(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	// Insert test data
	conn := database.Conn()
	_, err = conn.Exec(`
		INSERT INTO issues (id, title, implementer_session) VALUES ('issue1', 'Test 1', 'session1');
		INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file1', 'issue1', 'main.go');
	`)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Set base dir
	oldBaseDir := baseDirOverride
	p := tmpDir
	baseDirOverride = &p
	defer func() { baseDirOverride = oldBaseDir }()

	// Test CSV output
	silosFormat = "csv"
	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runSilos()

	w.Close()
	os.Stdout = oldOut

	if err != nil {
		t.Fatalf("runSilos failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify CSV header
	if !strings.Contains(output, "Type,Author/File,Value,Risk") {
		t.Error("expected CSV header in output")
	}
}

func TestSilosCommandWithFlags(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	// Insert test data with single author on multiple files
	conn := database.Conn()
	for i := 1; i <= 5; i++ {
		id := string(rune(48 + i))
		_, err = conn.Exec(`INSERT INTO issues (id, title, implementer_session) VALUES ('issue`+id+`', 'Test `+id+`', 'session1')`)
		if err != nil {
			t.Fatalf("failed to insert issue: %v", err)
		}

		_, err = conn.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file`+id+`', 'issue`+id+`', 'file`+id+`.go')`)
		if err != nil {
			t.Fatalf("failed to insert file: %v", err)
		}
	}

	// Set base dir
	oldBaseDir := baseDirOverride
	p := tmpDir
	baseDirOverride = &p
	defer func() { baseDirOverride = oldBaseDir }()

	// Test with critical-only flag
	silosFormat = "table"
	silosCriticalOnly = true

	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runSilos()

	w.Close()
	os.Stdout = oldOut

	if err != nil {
		t.Fatalf("runSilos failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Should show critical files section
	if !strings.Contains(output, "CRITICAL FILES") {
		t.Error("expected CRITICAL FILES section with --critical-only flag")
	}

	silosCriticalOnly = false // Reset for other tests
}

func TestTruncatePath(t *testing.T) {
	tests := []struct {
		path    string
		maxLen  int
		wantMin int
		wantMax int
	}{
		{
			path:    "short.go",
			maxLen:  50,
			wantMin: 8,
			wantMax: 8,
		},
		{
			path:    "very/long/path/to/some/deeply/nested/file/structure.go",
			maxLen:  20,
			wantMin: 20,
			wantMax: 20,
		},
	}

	for _, tt := range tests {
		result := truncatePath(tt.path, tt.maxLen)
		if len(result) < tt.wantMin || len(result) > tt.maxLen {
			t.Errorf("truncatePath(%q, %d) got length %d, want %d-%d", tt.path, tt.maxLen, len(result), tt.wantMin, tt.maxLen)
		}
	}
}

func TestInitializeDatabase(t *testing.T) {
	tmpDir := t.TempDir()

	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	// Verify database file exists
	dbPath := filepath.Join(tmpDir, ".todos", "issues.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}

	// Verify tables exist
	conn := database.Conn()
	var tableName string
	err = conn.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='issues'").Scan(&tableName)
	if err != nil {
		t.Fatalf("issues table not found: %v", err)
	}

	if tableName != "issues" {
		t.Errorf("expected table 'issues', got %q", tableName)
	}
}
