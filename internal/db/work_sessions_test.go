package db

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func TestCreateWorkSessionStoresOriginatingWorktreeMetadata(t *testing.T) {
	database := setupSessionTestDB(t)

	now := time.Now().Truncate(time.Second)
	sessionRow := &SessionRow{
		ID:           "ses_origin",
		Branch:       "main",
		AgentType:    "codex",
		AgentPID:     42,
		WorktreeID:   "wt_origin",
		WorktreeRoot: "/tmp/td-worktree",
		RepoRoot:     "/tmp/td-main",
		StartedAt:    now,
		LastActivity: now,
	}
	if err := database.UpsertSession(sessionRow); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	ws := &models.WorkSession{
		Name:      "origin-test",
		SessionID: sessionRow.ID,
		StartSHA:  "abc123",
	}
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession: %v", err)
	}

	got, err := database.GetWorkSession(ws.ID)
	if err != nil {
		t.Fatalf("GetWorkSession: %v", err)
	}

	if got.WorktreeID != sessionRow.WorktreeID {
		t.Fatalf("WorktreeID: got %q want %q", got.WorktreeID, sessionRow.WorktreeID)
	}
	if got.WorktreeRoot != sessionRow.WorktreeRoot {
		t.Fatalf("WorktreeRoot: got %q want %q", got.WorktreeRoot, sessionRow.WorktreeRoot)
	}
	if got.RepoRoot != sessionRow.RepoRoot {
		t.Fatalf("RepoRoot: got %q want %q", got.RepoRoot, sessionRow.RepoRoot)
	}
}

func TestWorkSessionActionLogOmitsWorktreeMetadata(t *testing.T) {
	database := setupSessionTestDB(t)

	ws := &models.WorkSession{
		Name:         "sync-payload-test",
		SessionID:    "ses_payload",
		WorktreeID:   "wt_local",
		WorktreeRoot: "/tmp/local-worktree",
		RepoRoot:     "/tmp/local-repo",
		StartSHA:     "abc123",
	}
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession: %v", err)
	}

	assertWorkSessionActionLogOmitsLocalFields(t, database, ws.ID, "create")

	now := time.Now()
	ws.Name = "sync-payload-test-updated"
	ws.EndedAt = &now
	ws.EndSHA = "def456"
	if err := database.UpdateWorkSession(ws); err != nil {
		t.Fatalf("UpdateWorkSession: %v", err)
	}

	assertWorkSessionActionLogOmitsLocalFields(t, database, ws.ID, "update")
}

func assertWorkSessionActionLogOmitsLocalFields(t *testing.T, database *DB, entityID, actionType string) {
	t.Helper()

	var raw string
	if err := database.conn.QueryRow(`
		SELECT new_data FROM action_log
		WHERE entity_type = 'work_sessions' AND entity_id = ? AND action_type = ?
		ORDER BY rowid DESC LIMIT 1
	`, entityID, actionType).Scan(&raw); err != nil {
		t.Fatalf("read action_log %s: %v", actionType, err)
	}

	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatalf("unmarshal action_log %s: %v", actionType, err)
	}
	for _, key := range []string{"worktree_id", "worktree_root", "repo_root"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("%s action_log leaked %s in %v", actionType, key, fields)
		}
	}
}
