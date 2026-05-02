package db

import (
	"testing"
)

// TestAuditForeignKeys_CleanDB verifies a freshly initialized DB has zero
// orphans across every audited relation.
func TestAuditForeignKeys_CleanDB(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	results, err := AuditForeignKeys(database.Conn())
	if err != nil {
		t.Fatalf("AuditForeignKeys failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected at least one relation audited, got 0")
	}

	for _, r := range results {
		if r.Count != 0 {
			t.Errorf("expected 0 orphans for %s, got %d", r.Relation, r.Count)
		}
	}
}

// TestAuditForeignKeys_DetectsOrphans seeds rows that reference non-existent
// parents in each child table and asserts the audit finds them.
func TestAuditForeignKeys_DetectsOrphans(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	conn := database.Conn()

	// FK enforcement is ON by default (td-4846e6); disable temporarily so
	// we can intentionally seed orphan rows for the audit to detect.
	if _, err := conn.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("disable foreign_keys: %v", err)
	}
	defer conn.Exec("PRAGMA foreign_keys=ON")

	// Seed direct INSERTs that bypass model layer so we can intentionally
	// create orphans.
	seed := []struct {
		desc string
		sql  string
	}{
		// issues.parent_id -> issues.id (1 orphan: self-row points at missing parent)
		{"issue with bad parent", `INSERT INTO issues (id, title, parent_id) VALUES ('iss-child', 'c', 'iss-missing')`},

		// handoffs.issue_id -> issues.id (1 orphan)
		{"orphan handoff", `INSERT INTO handoffs (id, issue_id, session_id) VALUES ('h1', 'iss-ghost', 'ses1')`},

		// git_snapshots.issue_id -> issues.id (1 orphan)
		{"orphan git_snapshot", `INSERT INTO git_snapshots (id, issue_id, event, commit_sha, branch) VALUES ('g1', 'iss-ghost', 'e', 'sha', 'b')`},

		// issue_files.issue_id -> issues.id (1 orphan)
		{"orphan issue_file", `INSERT INTO issue_files (id, issue_id, file_path) VALUES ('f1', 'iss-ghost', '/p')`},

		// issue_dependencies -> 1 orphan each on issue_id and depends_on_id.
		// Using distinct bad parents so both columns count.
		{"orphan dep issue_id", `INSERT INTO issue_dependencies (id, issue_id, depends_on_id) VALUES ('d1', 'iss-ghost-a', 'iss-ghost-b')`},

		// work_session_issues -> orphan on both FK columns
		{"orphan wsi", `INSERT INTO work_session_issues (id, work_session_id, issue_id) VALUES ('wsi1', 'ws-ghost', 'iss-ghost')`},

		// comments.issue_id -> issues.id (1 orphan)
		{"orphan comment", `INSERT INTO comments (id, issue_id, session_id, text) VALUES ('c1', 'iss-ghost', 'ses1', 't')`},
	}
	for _, s := range seed {
		if _, err := conn.Exec(s.sql); err != nil {
			t.Fatalf("seed %q: %v", s.desc, err)
		}
	}

	results, err := AuditForeignKeys(conn)
	if err != nil {
		t.Fatalf("AuditForeignKeys: %v", err)
	}

	// Build lookup keyed by relation string.
	got := map[string]int{}
	for _, r := range results {
		got[r.Relation] = r.Count
	}

	expect := map[string]int{
		"issues.parent_id -> issues.id":                           1,
		"handoffs.issue_id -> issues.id":                          1,
		"git_snapshots.issue_id -> issues.id":                     1,
		"issue_files.issue_id -> issues.id":                       1,
		"issue_dependencies.issue_id -> issues.id":                1,
		"issue_dependencies.depends_on_id -> issues.id":           1,
		"work_session_issues.work_session_id -> work_sessions.id": 1,
		"work_session_issues.issue_id -> issues.id":               1,
		"comments.issue_id -> issues.id":                          1,
	}

	for rel, want := range expect {
		if got[rel] != want {
			t.Errorf("%s: got %d orphans, want %d", rel, got[rel], want)
		}
	}
}

// TestAuditForeignKeys_IgnoresEmptyFKValues ensures default-empty FK columns
// (e.g. issues.parent_id=”) are not counted as orphans.
func TestAuditForeignKeys_IgnoresEmptyFKValues(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	conn := database.Conn()

	// Top-level issue with empty parent_id (td's default sentinel).
	if _, err := conn.Exec(`INSERT INTO issues (id, title, parent_id) VALUES ('iss-top', 't', '')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	results, err := AuditForeignKeys(conn)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	for _, r := range results {
		if r.Count != 0 {
			t.Errorf("empty-string FK should not count as orphan; %s had %d", r.Relation, r.Count)
		}
	}
}
