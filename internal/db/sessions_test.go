package db

import (
	"testing"
	"time"
)

func setupSessionTestDB(t *testing.T) *DB {
	t.Helper()
	tmpDir := t.TempDir()
	database, err := Initialize(tmpDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestUpsertAndGetSessionByID(t *testing.T) {
	db := setupSessionTestDB(t)

	now := time.Now().Truncate(time.Second)
	sess := &SessionRow{
		ID:                "ses_abc123",
		Name:              "test-session",
		Branch:            "main",
		AgentType:         "claude-code",
		AgentPID:          12345,
		ContextID:         "ctx_1",
		PreviousSessionID: "",
		StartedAt:         now,
		LastActivity:      now,
	}

	if err := db.UpsertSession(sess); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := db.GetSessionByID("ses_abc123")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.Name != "test-session" {
		t.Errorf("name = %q, want %q", got.Name, "test-session")
	}
	if got.Branch != "main" {
		t.Errorf("branch = %q, want %q", got.Branch, "main")
	}
	if got.AgentType != "claude-code" {
		t.Errorf("agent_type = %q, want %q", got.AgentType, "claude-code")
	}
	if got.AgentPID != 12345 {
		t.Errorf("agent_pid = %d, want %d", got.AgentPID, 12345)
	}
}

func TestUpsertUpdatesExisting(t *testing.T) {
	db := setupSessionTestDB(t)

	now := time.Now().Truncate(time.Second)
	sess := &SessionRow{
		ID: "ses_abc123", Name: "v1", Branch: "main",
		AgentType: "claude-code", AgentPID: 100,
		StartedAt: now, LastActivity: now,
	}
	if err := db.UpsertSession(sess); err != nil {
		t.Fatalf("upsert v1: %v", err)
	}

	sess.Name = "v2"
	sess.LastActivity = now.Add(time.Minute)
	if err := db.UpsertSession(sess); err != nil {
		t.Fatalf("upsert v2: %v", err)
	}

	got, err := db.GetSessionByID("ses_abc123")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "v2" {
		t.Errorf("name = %q, want %q", got.Name, "v2")
	}
}

func TestGetSessionByBranchAgent(t *testing.T) {
	db := setupSessionTestDB(t)

	now := time.Now().Truncate(time.Second)
	sess := &SessionRow{
		ID: "ses_abc123", Branch: "main",
		AgentType: "claude-code", AgentPID: 100,
		StartedAt: now, LastActivity: now,
	}
	if err := db.UpsertSession(sess); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := db.GetSessionByBranchAgent("main", "claude-code", 100)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.ID != "ses_abc123" {
		t.Errorf("id = %q, want %q", got.ID, "ses_abc123")
	}

	// Different agent = not found
	got, err = db.GetSessionByBranchAgent("main", "cursor", 200)
	if err != nil {
		t.Fatalf("get different: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for different agent, got %+v", got)
	}
}

func TestGetSessionByIDNotFound(t *testing.T) {
	db := setupSessionTestDB(t)
	got, err := db.GetSessionByID("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestListAllSessionsOrdering(t *testing.T) {
	db := setupSessionTestDB(t)

	now := time.Now().Truncate(time.Second)
	for i, id := range []string{"ses_old", "ses_mid", "ses_new"} {
		sess := &SessionRow{
			ID: id, Branch: "main", AgentType: "test", AgentPID: i,
			StartedAt:    now.Add(time.Duration(i) * time.Hour),
			LastActivity: now.Add(time.Duration(i) * time.Hour),
		}
		if err := db.UpsertSession(sess); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	list, err := db.ListAllSessions()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}
	// Most recent first
	if list[0].ID != "ses_new" {
		t.Errorf("first = %q, want ses_new", list[0].ID)
	}
	if list[2].ID != "ses_old" {
		t.Errorf("last = %q, want ses_old", list[2].ID)
	}
}

func TestDeleteStaleSessions(t *testing.T) {
	db := setupSessionTestDB(t)

	now := time.Now().Truncate(time.Second)
	old := &SessionRow{
		ID: "ses_old", Branch: "main", AgentType: "test", AgentPID: 1,
		StartedAt: now.Add(-48 * time.Hour), LastActivity: now.Add(-48 * time.Hour),
	}
	fresh := &SessionRow{
		ID: "ses_fresh", Branch: "main", AgentType: "test", AgentPID: 2,
		StartedAt: now, LastActivity: now,
	}

	if err := db.UpsertSession(old); err != nil {
		t.Fatalf("upsert old: %v", err)
	}
	if err := db.UpsertSession(fresh); err != nil {
		t.Fatalf("upsert fresh: %v", err)
	}

	deleted, err := db.DeleteStaleSessions(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("delete stale: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	// Fresh should remain
	got, err := db.GetSessionByID("ses_fresh")
	if err != nil {
		t.Fatalf("get fresh: %v", err)
	}
	if got == nil {
		t.Error("expected fresh session to remain")
	}

	// Old should be gone
	got, err = db.GetSessionByID("ses_old")
	if err != nil {
		t.Fatalf("get old: %v", err)
	}
	if got != nil {
		t.Error("expected old session to be deleted")
	}
}

func TestUpdateSessionActivity(t *testing.T) {
	db := setupSessionTestDB(t)

	now := time.Now().Truncate(time.Second)
	sess := &SessionRow{
		ID: "ses_abc", Branch: "main", AgentType: "test",
		StartedAt: now, LastActivity: now,
	}
	if err := db.UpsertSession(sess); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	later := now.Add(5 * time.Minute)
	if err := db.UpdateSessionActivity("ses_abc", later); err != nil {
		t.Fatalf("update activity: %v", err)
	}

	got, err := db.GetSessionByID("ses_abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastActivity.Before(now.Add(4 * time.Minute)) {
		t.Errorf("last_activity not updated: %v", got.LastActivity)
	}
}

func TestUpdateSessionName(t *testing.T) {
	db := setupSessionTestDB(t)

	now := time.Now().Truncate(time.Second)
	sess := &SessionRow{
		ID: "ses_abc", Branch: "main", AgentType: "test",
		StartedAt: now, LastActivity: now,
	}
	if err := db.UpsertSession(sess); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := db.UpdateSessionName("ses_abc", "my-session"); err != nil {
		t.Fatalf("update name: %v", err)
	}

	got, err := db.GetSessionByID("ses_abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "my-session" {
		t.Errorf("name = %q, want %q", got.Name, "my-session")
	}
}
