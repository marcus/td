package db

import (
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func TestActionLogTimestampHelpers(t *testing.T) {
	ts := actionLogTimestampNow()
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("actionLogTimestampNow not RFC3339Nano: %v (%q)", err, ts)
	}
	if !parsed.Equal(parsed.UTC()) {
		t.Fatalf("expected UTC timestamp, got %q", ts)
	}
	if !strings.Contains(ts, "T") || !strings.HasSuffix(ts, "Z") {
		t.Fatalf("expected RFC3339 UTC shape, got %q", ts)
	}
}

func TestActionLogWritesUseCanonicalTimestampFormat(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if err := db.LogAction(&models.ActionLog{
		SessionID:    "ses_test",
		ActionType:   models.ActionUpdate,
		EntityType:   "issue",
		EntityID:     issue.ID,
		PreviousData: `{}`,
		NewData:      `{"status":"in_progress"}`,
	}); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	if err := db.AddDependencyLogged(issue.ID, "td-999", "depends_on", "ses_test"); err != nil {
		t.Fatalf("AddDependencyLogged failed: %v", err)
	}

	if _, err := db.CreateNote("n1", "body"); err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}

	rows, err := db.conn.Query(`SELECT CAST(timestamp AS TEXT) FROM action_log ORDER BY rowid ASC`)
	if err != nil {
		t.Fatalf("query action_log timestamps: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var ts string
		if err := rows.Scan(&ts); err != nil {
			t.Fatalf("scan timestamp: %v", err)
		}
		count++

		if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
			t.Fatalf("timestamp %q is not RFC3339Nano: %v", ts, err)
		}
		if !strings.Contains(ts, "T") || !strings.HasSuffix(ts, "Z") {
			t.Fatalf("timestamp not canonical RFC3339 UTC: %q", ts)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate timestamps: %v", err)
	}
	if count < 3 {
		t.Fatalf("expected at least 3 action_log rows, got %d", count)
	}
}
