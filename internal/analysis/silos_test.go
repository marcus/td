package analysis

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Create test schema
	schema := `
	CREATE TABLE issues (
		id TEXT PRIMARY KEY,
		implementer_session TEXT DEFAULT ''
	);
	CREATE TABLE issue_files (
		id TEXT PRIMARY KEY,
		issue_id TEXT NOT NULL,
		file_path TEXT NOT NULL,
		FOREIGN KEY (issue_id) REFERENCES issues(id),
		UNIQUE(issue_id, file_path)
	);
	`
	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	return db
}

func TestAnalyzeSilosEmpty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	report, err := AnalyzeSilos(db, t.TempDir())
	if err != nil {
		t.Fatalf("AnalyzeSilos failed: %v", err)
	}

	if len(report.FileOwnership) != 0 {
		t.Errorf("expected 0 files, got %d", len(report.FileOwnership))
	}

	if len(report.AuthorContribution) != 0 {
		t.Errorf("expected 0 authors, got %d", len(report.AuthorContribution))
	}

	if report.SiloRiskScore != 0.0 {
		t.Errorf("expected risk score 0.0, got %.2f", report.SiloRiskScore)
	}
}

func TestAnalyzeSilosSingleAuthorPerFile(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Insert test data
	_, err := db.Exec(`INSERT INTO issues (id, implementer_session) VALUES ('issue1', 'session1')`)
	if err != nil {
		t.Fatalf("failed to insert issue: %v", err)
	}

	_, err = db.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file1', 'issue1', 'main.go')`)
	if err != nil {
		t.Fatalf("failed to insert file: %v", err)
	}

	_, err = db.Exec(`INSERT INTO issues (id, implementer_session) VALUES ('issue2', 'session1')`)
	if err != nil {
		t.Fatalf("failed to insert second issue: %v", err)
	}

	_, err = db.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file2', 'issue2', 'main.go')`)
	if err != nil {
		t.Fatalf("failed to insert second file: %v", err)
	}

	report, err := AnalyzeSilos(db, t.TempDir())
	if err != nil {
		t.Fatalf("AnalyzeSilos failed: %v", err)
	}

	if len(report.FileOwnership) != 1 {
		t.Errorf("expected 1 file, got %d", len(report.FileOwnership))
	}

	if len(report.CriticalFiles) != 1 {
		t.Errorf("expected 1 critical file, got %d", len(report.CriticalFiles))
	}

	if report.FileOwnership[0].Critical == false {
		t.Error("expected file to be critical (single author)")
	}

	if report.SiloRiskScore <= 0 {
		t.Errorf("expected positive risk score, got %.2f", report.SiloRiskScore)
	}
}

func TestAnalyzeSilosMultipleAuthors(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// File touched by 2 authors
	_, err := db.Exec(`INSERT INTO issues (id, implementer_session) VALUES ('issue1', 'session1')`)
	if err != nil {
		t.Fatalf("failed to insert issue: %v", err)
	}

	_, err = db.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file1', 'issue1', 'main.go')`)
	if err != nil {
		t.Fatalf("failed to insert file: %v", err)
	}

	_, err = db.Exec(`INSERT INTO issues (id, implementer_session) VALUES ('issue2', 'session2')`)
	if err != nil {
		t.Fatalf("failed to insert second issue: %v", err)
	}

	_, err = db.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file2', 'issue2', 'main.go')`)
	if err != nil {
		t.Fatalf("failed to insert second file: %v", err)
	}

	report, err := AnalyzeSilos(db, t.TempDir())
	if err != nil {
		t.Fatalf("AnalyzeSilos failed: %v", err)
	}

	if len(report.FileOwnership) != 1 {
		t.Errorf("expected 1 file, got %d", len(report.FileOwnership))
	}

	if len(report.CriticalFiles) != 0 {
		t.Errorf("expected 0 critical files (shared file), got %d", len(report.CriticalFiles))
	}

	if report.FileOwnership[0].Critical == true {
		t.Error("expected file to NOT be critical (multiple authors)")
	}

	if report.FileOwnership[0].Count != 2 {
		t.Errorf("expected 2 authors, got %d", report.FileOwnership[0].Count)
	}

	if len(report.AuthorContribution) != 2 {
		t.Errorf("expected 2 author contributions, got %d", len(report.AuthorContribution))
	}
}

func TestAnalyzeSilosAuthorRatio(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create issues where session1 owns 3 files, session2 owns 1 file
	for i := 1; i <= 3; i++ {
		_, err := db.Exec(`INSERT INTO issues (id, implementer_session) VALUES (?, 'session1')`, "issue"+string(rune(48+i)))
		if err != nil {
			t.Fatalf("failed to insert issue: %v", err)
		}

		_, err = db.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES (?, ?, ?)`, "file"+string(rune(48+i)), "issue"+string(rune(48+i)), "file"+string(rune(48+i))+".go")
		if err != nil {
			t.Fatalf("failed to insert file: %v", err)
		}
	}

	_, err := db.Exec(`INSERT INTO issues (id, implementer_session) VALUES ('issue4', 'session2')`)
	if err != nil {
		t.Fatalf("failed to insert issue: %v", err)
	}

	_, err = db.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file4', 'issue4', 'file4.go')`)
	if err != nil {
		t.Fatalf("failed to insert file: %v", err)
	}

	report, err := AnalyzeSilos(db, t.TempDir())
	if err != nil {
		t.Fatalf("AnalyzeSilos failed: %v", err)
	}

	if len(report.AuthorContribution) != 2 {
		t.Errorf("expected 2 authors, got %d", len(report.AuthorContribution))
	}

	// Verify session1 has highest ratio
	if report.AuthorContribution[0].FileCount < report.AuthorContribution[1].FileCount {
		t.Error("expected session1 to be first (highest file count)")
	}

	if report.AuthorContribution[0].RatioOfAll != 0.75 {
		t.Errorf("expected session1 ratio of 0.75, got %.2f", report.AuthorContribution[0].RatioOfAll)
	}
}

func TestDetectPatterns(t *testing.T) {
	report := &SiloReport{
		FileOwnership: []FileOwnership{
			{FilePath: "file1.go", Critical: true},
			{FilePath: "file2.go", Critical: true},
			{FilePath: "file3.go", Critical: false},
		},
		AuthorContribution: []AuthorContribution{
			{AuthorID: "session1", FileCount: 2, CriticalRisk: 2, RatioOfAll: 0.67},
		},
		TotalCodeFiles:    10,
		ExploredCodeRatio: 0.3,
		CriticalFiles:     []string{"file1.go", "file2.go"},
	}

	patterns := detectPatterns(report)

	if len(patterns) == 0 {
		t.Error("expected to detect at least one suspicious pattern")
	}

	// Check for critical files pattern
	foundCriticalPattern := false
	for _, p := range patterns {
		if strings.Contains(p.Pattern, "files with single author") {
			foundCriticalPattern = true
			// With 66% of files being critical, severity should be "high"
			if p.Severity != "high" {
				t.Errorf("expected high severity (>30%% critical), got %s", p.Severity)
			}
		}
	}

	if !foundCriticalPattern {
		t.Error("expected to find critical files pattern")
	}
}

func TestCalculateRiskScore(t *testing.T) {
	tests := []struct {
		name         string
		report       *SiloReport
		expectedMin  float64
		expectedMax  float64
	}{
		{
			name: "No files",
			report: &SiloReport{
				FileOwnership: []FileOwnership{},
			},
			expectedMin: 0.0,
			expectedMax: 0.0,
		},
		{
			name: "All critical files",
			report: &SiloReport{
				FileOwnership: []FileOwnership{
					{Critical: true},
					{Critical: true},
					{Critical: true},
				},
				AuthorContribution: []AuthorContribution{
					{RatioOfAll: 1.0, FileCount: 3, CriticalRisk: 3},
				},
				CriticalFiles: []string{"file1", "file2", "file3"},
				TotalCodeFiles:    10,
				ExploredCodeRatio: 0.3,
			},
			// Score: critical_ratio * 0.4 (1.0 * 0.4 = 0.4) + author_concentration * 0.3 (0.3) + explored * 0 + critical_risk * 0.1 (0.0) = 0.7
			expectedMin: 0.6,
			expectedMax: 0.8,
		},
		{
			name: "Low risk (few critical, spread authors)",
			report: &SiloReport{
				FileOwnership: []FileOwnership{
					{Critical: false},
					{Critical: false},
					{Critical: false},
				},
				AuthorContribution: []AuthorContribution{
					{RatioOfAll: 0.4},
					{RatioOfAll: 0.35},
					{RatioOfAll: 0.25},
				},
				TotalCodeFiles:    30,
				ExploredCodeRatio: 0.1,
			},
			// Score: critical_ratio * 0.4 (0 * 0.4 = 0.0) + author_concentration * 0.3 (0.1) + explored * 0.2 (0.2) + critical_risk * 0 = 0.3
			expectedMin: 0.2,
			expectedMax: 0.4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculateRiskScore(tt.report)
			if score < tt.expectedMin || score > tt.expectedMax {
				t.Errorf("expected score between %.2f and %.2f, got %.2f", tt.expectedMin, tt.expectedMax, score)
			}
		})
	}
}

func TestCountRepositoryFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	files := []string{
		"main.go",
		"utils.go",
		"test.py",
		"script.sh",
		"README.md",
	}

	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	// Create subdirectory with files
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(subDir, "other.go"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	count, err := countRepositoryFiles(tmpDir)
	if err != nil {
		t.Fatalf("countRepositoryFiles failed: %v", err)
	}

	// Should count: main.go, utils.go, test.py, script.sh, other.go (5 files)
	// README.md should not be counted
	if count != 5 {
		t.Errorf("expected 5 code files, got %d", count)
	}
}

func TestEdgeCaseShortSessionID(t *testing.T) {
	// Test that session IDs shorter than 8 characters don't panic
	report := &SiloReport{
		FileOwnership: []FileOwnership{
			{FilePath: "file1.go", Critical: true},
			{FilePath: "file2.go", Critical: false},
		},
		AuthorContribution: []AuthorContribution{
			{AuthorID: "a", FileCount: 2, CriticalRisk: 1, RatioOfAll: 1.0}, // Very short ID
		},
		CriticalFiles:     []string{"file1.go"},
		TotalCodeFiles:    10,
		ExploredCodeRatio: 0.2,
	}

	// detectPatterns should not panic on short session IDs
	patterns := detectPatterns(report)

	// Should find some patterns
	if len(patterns) == 0 {
		t.Error("expected to detect patterns even with short session ID")
	}

	// All patterns should have valid AuthorID strings (not truncated incorrectly)
	for _, p := range patterns {
		if strings.Contains(p.Pattern, "owns") && strings.HasPrefix(p.Pattern, "owns") {
			t.Error("unexpected pattern format - author ID missing")
		}
	}
}

func TestEdgeCaseEmptyDatabase(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Don't insert any data
	report, err := AnalyzeSilos(db, t.TempDir())
	if err != nil {
		t.Fatalf("AnalyzeSilos should not fail on empty database: %v", err)
	}

	if report == nil {
		t.Error("expected non-nil report")
	}

	if len(report.FileOwnership) != 0 {
		t.Errorf("expected 0 files, got %d", len(report.FileOwnership))
	}

	if len(report.SuspiciousPatterns) != 0 {
		t.Errorf("expected 0 patterns for empty database, got %d", len(report.SuspiciousPatterns))
	}
}

func TestEdgeCaseNullFilePaths(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Insert issue with empty file path (should be filtered by query)
	_, err := db.Exec(`INSERT INTO issues (id, implementer_session) VALUES ('issue1', 'session1')`)
	if err != nil {
		t.Fatalf("failed to insert issue: %v", err)
	}

	_, err = db.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file1', 'issue1', '')`)
	if err != nil {
		t.Fatalf("failed to insert file: %v", err)
	}

	// Also insert a valid file
	_, err = db.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file2', 'issue1', 'main.go')`)
	if err != nil {
		t.Fatalf("failed to insert file: %v", err)
	}

	report, err := AnalyzeSilos(db, t.TempDir())
	if err != nil {
		t.Fatalf("AnalyzeSilos failed: %v", err)
	}

	// Should only have 1 file (empty paths filtered out)
	if len(report.FileOwnership) != 1 {
		t.Errorf("expected 1 file (empty paths filtered), got %d", len(report.FileOwnership))
	}

	if report.FileOwnership[0].FilePath != "main.go" {
		t.Errorf("expected main.go, got %s", report.FileOwnership[0].FilePath)
	}
}

func TestEdgeCaseEmptySessions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Insert issue with empty implementer_session (should be filtered by query)
	_, err := db.Exec(`INSERT INTO issues (id, implementer_session) VALUES ('issue1', '')`)
	if err != nil {
		t.Fatalf("failed to insert issue: %v", err)
	}

	_, err = db.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file1', 'issue1', 'main.go')`)
	if err != nil {
		t.Fatalf("failed to insert file: %v", err)
	}

	// Also insert a valid issue with session
	_, err = db.Exec(`INSERT INTO issues (id, implementer_session) VALUES ('issue2', 'session1')`)
	if err != nil {
		t.Fatalf("failed to insert issue: %v", err)
	}

	_, err = db.Exec(`INSERT INTO issue_files (id, issue_id, file_path) VALUES ('file2', 'issue2', 'main.go')`)
	if err != nil {
		t.Fatalf("failed to insert file: %v", err)
	}

	report, err := AnalyzeSilos(db, t.TempDir())
	if err != nil {
		t.Fatalf("AnalyzeSilos failed: %v", err)
	}

	// Should only have 1 author (empty sessions filtered out)
	if len(report.AuthorContribution) != 1 {
		t.Errorf("expected 1 author, got %d", len(report.AuthorContribution))
	}

	if report.AuthorContribution[0].AuthorID != "session1" {
		t.Errorf("expected session1, got %s", report.AuthorContribution[0].AuthorID)
	}

	// File should have only 1 author (the empty session was filtered)
	if report.FileOwnership[0].Count != 1 {
		t.Errorf("expected 1 author for file (empty session filtered), got %d", report.FileOwnership[0].Count)
	}
}
