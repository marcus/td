package sync

import (
	"database/sql"
	"encoding/json"
	_ "github.com/mattn/go-sqlite3"
	"testing"
)

func setupIssuesTable(t *testing.T) *sql.DB {
	db, _ := sql.Open("sqlite3", ":memory:")
	_, err := db.Exec(`CREATE TABLE issues (id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT NOT NULL DEFAULT 'open', type TEXT NOT NULL DEFAULT 'task', priority TEXT NOT NULL DEFAULT 'P2', points INTEGER DEFAULT 0, labels TEXT DEFAULT '', parent_id TEXT DEFAULT '', acceptance TEXT DEFAULT '', implementer_session TEXT DEFAULT '', reviewer_session TEXT DEFAULT '', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, closed_at DATETIME, deleted_at DATETIME, minor INTEGER DEFAULT 0, created_branch TEXT DEFAULT '', creator_session TEXT DEFAULT '', sprint TEXT DEFAULT '', defer_until TEXT, due_date TEXT, defer_count INTEGER DEFAULT 0)`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func applyAndGetLabels(t *testing.T, db *sql.DB, id string, rawJSON string) sql.NullString {
	tx, _ := db.Begin()
	event := Event{EntityType: "issues", EntityID: id, ActionType: "create", Payload: json.RawMessage(rawJSON)}
	_, err := ApplyEvent(tx, event, func(s string) bool { return s == "issues" })
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	tx.Commit()
	var labels sql.NullString
	db.QueryRow("SELECT labels FROM issues WHERE id=?", id).Scan(&labels)
	return labels
}

func TestReproNullLabels_AbsentKey(t *testing.T) {
	db := setupIssuesTable(t)
	defer db.Close()
	l := applyAndGetLabels(t, db, "td-a1", `{"id":"td-a1","title":"x"}`)
	t.Logf("ABSENT: valid=%v string=%q", l.Valid, l.String)
	if !l.Valid {
		t.Errorf("labels NULL when key absent")
	}
}
func TestReproNullLabels_ExplicitNull(t *testing.T) {
	db := setupIssuesTable(t)
	defer db.Close()
	l := applyAndGetLabels(t, db, "td-a2", `{"id":"td-a2","title":"x","labels":null}`)
	t.Logf("EXPLICIT null: valid=%v string=%q", l.Valid, l.String)
	if !l.Valid {
		t.Errorf("labels NULL when key explicitly null -- THIS IS THE BUG")
	}
}
func TestReproNullLabels_EmptyArray(t *testing.T) {
	db := setupIssuesTable(t)
	defer db.Close()
	l := applyAndGetLabels(t, db, "td-a3", `{"id":"td-a3","title":"x","labels":[]}`)
	t.Logf("EMPTY arr: valid=%v string=%q", l.Valid, l.String)
	if !l.Valid {
		t.Errorf("labels NULL when array []")
	}
}
func TestReproNullLabels_EmptyString(t *testing.T) {
	db := setupIssuesTable(t)
	defer db.Close()
	l := applyAndGetLabels(t, db, "td-a4", `{"id":"td-a4","title":"x","labels":""}`)
	t.Logf("EMPTY string: valid=%v string=%q", l.Valid, l.String)
	if !l.Valid {
		t.Errorf("labels NULL when string ''")
	}
}

// TestReproNullLabels_AllTextDefaultsNull verifies that ALL TEXT DEFAULT ”
// columns on issues get stored as ” (not NULL) when the payload explicitly
// sets them to null. This is the systemic fix: any TEXT column declared
// with DEFAULT ” must get ” instead of NULL on the apply path, so readers
// that scan into plain string don't crash.
func TestReproNullLabels_AllTextDefaultsNull(t *testing.T) {
	db := setupIssuesTable(t)
	defer db.Close()
	tx, _ := db.Begin()
	payload := `{
    "id":"td-a5","title":"x",
    "description":null,"labels":null,"parent_id":null,"acceptance":null,
    "implementer_session":null,"reviewer_session":null,"creator_session":null,
    "created_branch":null,"sprint":null
  }`
	event := Event{EntityType: "issues", EntityID: "td-a5", ActionType: "create", Payload: json.RawMessage(payload)}
	if _, err := ApplyEvent(tx, event, func(s string) bool { return s == "issues" }); err != nil {
		t.Fatalf("apply: %v", err)
	}
	tx.Commit()

	cols := []string{"description", "labels", "parent_id", "acceptance",
		"implementer_session", "reviewer_session", "creator_session",
		"created_branch", "sprint"}
	for _, col := range cols {
		var v sql.NullString
		q := "SELECT " + col + " FROM issues WHERE id=?"
		if err := db.QueryRow(q, "td-a5").Scan(&v); err != nil {
			t.Fatalf("scan %s: %v", col, err)
		}
		if !v.Valid {
			t.Errorf("%s stored as NULL (expected '')", col)
		} else if v.String != "" {
			t.Errorf("%s stored as %q (expected '')", col, v.String)
		}
	}
}

// TestReproNullLabels_PartialUpdateNull verifies the partial-update apply
// path (applyPartialUpdate) also defaults nil -> ” for TEXT DEFAULT ”
// columns — without this, an update event that sets a field to null would
// write NULL and crash readers the same way a create would.
func TestReproNullLabels_PartialUpdateNull(t *testing.T) {
	db := setupIssuesTable(t)
	defer db.Close()

	// Seed a row with non-empty values.
	tx1, _ := db.Begin()
	seed := `{"id":"td-a6","title":"x","labels":"bug","description":"hi"}`
	if _, err := ApplyEvent(tx1, Event{EntityType: "issues", EntityID: "td-a6", ActionType: "create", Payload: json.RawMessage(seed)}, func(s string) bool { return s == "issues" }); err != nil {
		t.Fatalf("seed apply: %v", err)
	}
	tx1.Commit()

	// Partial update that explicitly nulls labels + description.
	tx2, _ := db.Begin()
	prev := json.RawMessage(`{"id":"td-a6","title":"x","labels":"bug","description":"hi"}`)
	next := json.RawMessage(`{"id":"td-a6","title":"x","labels":null,"description":null}`)
	res, err := applyPartialUpdateEvent(tx2, Event{EntityType: "issues", EntityID: "td-a6", ActionType: "update", Payload: next}, prev)
	if err != nil {
		t.Fatalf("partial update: %v", err)
	}
	_ = res
	tx2.Commit()

	for _, col := range []string{"labels", "description"} {
		var v sql.NullString
		if err := db.QueryRow("SELECT "+col+" FROM issues WHERE id=?", "td-a6").Scan(&v); err != nil {
			t.Fatalf("scan %s: %v", col, err)
		}
		if !v.Valid {
			t.Errorf("partial update stored %s as NULL (expected '')", col)
		}
	}
}
