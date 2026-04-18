package db

import (
	"strings"
	"testing"
)

// TestFKEnforcement_InvalidHandoffRejected verifies that inserting a handoff
// whose issue_id does not exist fails once migration 30 has run.
func TestFKEnforcement_InvalidHandoffRejected(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Confirm FK pragma is ON (central opener's default after td-4846e6).
	var fk int
	if err := database.Conn().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("read pragma: %v", err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}

	_, err = database.Conn().Exec(
		`INSERT INTO handoffs (id, issue_id, session_id) VALUES ('ho-bad', 'td-nonexistent', 'ses')`,
	)
	if err == nil {
		t.Fatalf("expected FK violation inserting handoff with missing issue_id, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign") && !strings.Contains(strings.ToLower(err.Error()), "constraint") {
		t.Fatalf("expected FK/constraint error, got: %v", err)
	}
}

// TestFKEnforcement_DeleteChildLeavesParent confirms that deleting a child
// issue row does NOT cascade-delete the parent.
func TestFKEnforcement_DeleteChildLeavesParent(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	conn := database.Conn()

	if _, err := conn.Exec(`INSERT INTO issues (id, title) VALUES ('td-parent', 'P')`); err != nil {
		t.Fatalf("insert parent: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO issues (id, title, parent_id) VALUES ('td-child', 'C', 'td-parent')`); err != nil {
		t.Fatalf("insert child: %v", err)
	}

	if _, err := conn.Exec(`DELETE FROM issues WHERE id = 'td-child'`); err != nil {
		t.Fatalf("delete child: %v", err)
	}

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM issues WHERE id = 'td-parent'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("parent row gone after child delete; n=%d", n)
	}
}

// TestFKEnforcement_CascadeDeleteHandoff verifies that a schema-level cascade
// removes the handoff when the issue is deleted directly at the DB layer,
// with NO manual cascade helper running.
func TestFKEnforcement_CascadeDeleteHandoff(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	conn := database.Conn()

	if _, err := conn.Exec(`INSERT INTO issues (id, title) VALUES ('td-x', 'x')`); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO handoffs (id, issue_id, session_id) VALUES ('ho-x', 'td-x', 'ses1')`); err != nil {
		t.Fatalf("insert handoff: %v", err)
	}

	if _, err := conn.Exec(`DELETE FROM issues WHERE id = 'td-x'`); err != nil {
		t.Fatalf("delete issue: %v", err)
	}

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM handoffs WHERE id = 'ho-x'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected handoff cascade-deleted; got %d rows", n)
	}
}

// TestFKEnforcement_ParentDeleteDoesNotCascade verifies that deleting a
// parent issue does NOT cascade-delete children. The schema intentionally
// omits a FK on parent_id (see migrateEnableFKEnforcement rationale), so
// children survive with their (now stale) parent_id.
func TestFKEnforcement_ParentDeleteDoesNotCascade(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	conn := database.Conn()

	if _, err := conn.Exec(`INSERT INTO issues (id, title) VALUES ('td-parent2', 'P')`); err != nil {
		t.Fatalf("insert parent: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO issues (id, title, parent_id) VALUES ('td-child2', 'C', 'td-parent2')`); err != nil {
		t.Fatalf("insert child: %v", err)
	}

	if _, err := conn.Exec(`DELETE FROM issues WHERE id = 'td-parent2'`); err != nil {
		t.Fatalf("delete parent: %v", err)
	}

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM issues WHERE id = 'td-child2'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected child to survive parent delete; got %d rows", n)
	}
}
