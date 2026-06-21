package db

import (
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
