package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcus/td/internal/config"
)

func setupSessionStateTestDB(t *testing.T) (*DB, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database, dir
}

func TestSessionStateFocusSetGetClear(t *testing.T) {
	database, _ := setupSessionStateTestDB(t)
	scope := SessionStateScope{SessionID: "ses_focus", WorktreeID: "wt_a"}

	got, err := database.GetFocus(scope)
	if err != nil {
		t.Fatalf("GetFocus empty: %v", err)
	}
	if got != "" {
		t.Fatalf("GetFocus empty: got %q want empty", got)
	}

	if err := database.SetFocus(scope, "td-focus"); err != nil {
		t.Fatalf("SetFocus: %v", err)
	}
	got, err = database.GetFocus(scope)
	if err != nil {
		t.Fatalf("GetFocus: %v", err)
	}
	if got != "td-focus" {
		t.Fatalf("GetFocus: got %q want td-focus", got)
	}

	if err := database.ClearFocus(scope); err != nil {
		t.Fatalf("ClearFocus: %v", err)
	}
	got, err = database.GetFocus(scope)
	if err != nil {
		t.Fatalf("GetFocus after clear: %v", err)
	}
	if got != "" {
		t.Fatalf("GetFocus after clear: got %q want empty", got)
	}
}

func TestSessionStateActiveWorkSessionSetGetClear(t *testing.T) {
	database, _ := setupSessionStateTestDB(t)
	scope := SessionStateScope{SessionID: "ses_ws", WorktreeID: "wt_a"}

	got, err := database.GetActiveWorkSession(scope)
	if err != nil {
		t.Fatalf("GetActiveWorkSession empty: %v", err)
	}
	if got != "" {
		t.Fatalf("GetActiveWorkSession empty: got %q want empty", got)
	}

	if err := database.SetActiveWorkSession(scope, "ws-active"); err != nil {
		t.Fatalf("SetActiveWorkSession: %v", err)
	}
	got, err = database.GetActiveWorkSession(scope)
	if err != nil {
		t.Fatalf("GetActiveWorkSession: %v", err)
	}
	if got != "ws-active" {
		t.Fatalf("GetActiveWorkSession: got %q want ws-active", got)
	}

	if err := database.ClearActiveWorkSession(scope); err != nil {
		t.Fatalf("ClearActiveWorkSession: %v", err)
	}
	got, err = database.GetActiveWorkSession(scope)
	if err != nil {
		t.Fatalf("GetActiveWorkSession after clear: %v", err)
	}
	if got != "" {
		t.Fatalf("GetActiveWorkSession after clear: got %q want empty", got)
	}
}

func TestSessionStateScopesDoNotOverwriteEachOther(t *testing.T) {
	database, _ := setupSessionStateTestDB(t)
	scopes := []SessionStateScope{
		{SessionID: "ses_a", WorktreeID: "wt_a"},
		{SessionID: "ses_a", WorktreeID: "wt_b"},
		{SessionID: "ses_b", WorktreeID: "wt_a"},
	}

	for i, scope := range scopes {
		if err := database.SetFocus(scope, []string{"td-a", "td-b", "td-c"}[i]); err != nil {
			t.Fatalf("SetFocus[%d]: %v", i, err)
		}
		if err := database.SetActiveWorkSession(scope, []string{"ws-a", "ws-b", "ws-c"}[i]); err != nil {
			t.Fatalf("SetActiveWorkSession[%d]: %v", i, err)
		}
	}

	for i, scope := range scopes {
		focus, err := database.GetFocus(scope)
		if err != nil {
			t.Fatalf("GetFocus[%d]: %v", i, err)
		}
		if want := []string{"td-a", "td-b", "td-c"}[i]; focus != want {
			t.Fatalf("GetFocus[%d]: got %q want %q", i, focus, want)
		}
		ws, err := database.GetActiveWorkSession(scope)
		if err != nil {
			t.Fatalf("GetActiveWorkSession[%d]: %v", i, err)
		}
		if want := []string{"ws-a", "ws-b", "ws-c"}[i]; ws != want {
			t.Fatalf("GetActiveWorkSession[%d]: got %q want %q", i, ws, want)
		}
	}
}

func TestSessionStateEmptyWorktreeID(t *testing.T) {
	database, _ := setupSessionStateTestDB(t)
	scope := SessionStateScope{SessionID: "ses_empty_worktree"}

	if err := database.SetFocus(scope, "td-empty-wt"); err != nil {
		t.Fatalf("SetFocus: %v", err)
	}
	if err := database.SetActiveWorkSession(scope, "ws-empty-wt"); err != nil {
		t.Fatalf("SetActiveWorkSession: %v", err)
	}

	focus, err := database.GetFocus(scope)
	if err != nil {
		t.Fatalf("GetFocus: %v", err)
	}
	if focus != "td-empty-wt" {
		t.Fatalf("GetFocus: got %q want td-empty-wt", focus)
	}
	ws, err := database.GetActiveWorkSession(scope)
	if err != nil {
		t.Fatalf("GetActiveWorkSession: %v", err)
	}
	if ws != "ws-empty-wt" {
		t.Fatalf("GetActiveWorkSession: got %q want ws-empty-wt", ws)
	}
}

func TestSessionStateWritesUpdateTimestamp(t *testing.T) {
	database, _ := setupSessionStateTestDB(t)
	scope := SessionStateScope{SessionID: "ses_timestamp", WorktreeID: "wt_time"}

	if err := database.SetFocus(scope, "td-first"); err != nil {
		t.Fatalf("SetFocus first: %v", err)
	}
	first := sessionStateUpdatedAt(t, database, scope)
	time.Sleep(20 * time.Millisecond)
	if err := database.SetActiveWorkSession(scope, "ws-second"); err != nil {
		t.Fatalf("SetActiveWorkSession: %v", err)
	}
	second := sessionStateUpdatedAt(t, database, scope)
	if second == first {
		t.Fatalf("updated_at did not change: first=%q second=%q", first, second)
	}
}

func TestSessionStateConfigFallbackIsReadOnly(t *testing.T) {
	database, dir := setupSessionStateTestDB(t)
	if err := config.SetFocus(dir, "td-config-focus"); err != nil {
		t.Fatalf("config.SetFocus: %v", err)
	}
	if err := config.SetActiveWorkSession(dir, "ws-config-active"); err != nil {
		t.Fatalf("config.SetActiveWorkSession: %v", err)
	}
	configPath := filepath.Join(dir, ".todos", "config.json")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config before: %v", err)
	}

	scope := SessionStateScope{
		SessionID:                  "ses_fallback",
		WorktreeID:                 "wt_fallback",
		ConfigBaseDir:              dir,
		LegacyGetFocus:             config.GetFocus,
		LegacyGetActiveWorkSession: config.GetActiveWorkSession,
	}
	focus, err := database.GetFocus(scope)
	if err != nil {
		t.Fatalf("GetFocus fallback: %v", err)
	}
	if focus != "td-config-focus" {
		t.Fatalf("GetFocus fallback: got %q want td-config-focus", focus)
	}
	ws, err := database.GetActiveWorkSession(scope)
	if err != nil {
		t.Fatalf("GetActiveWorkSession fallback: %v", err)
	}
	if ws != "ws-config-active" {
		t.Fatalf("GetActiveWorkSession fallback: got %q want ws-config-active", ws)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("fallback reads modified config.json\nbefore: %s\nafter: %s", before, after)
	}
	var rows int
	if err := database.conn.QueryRow(`SELECT COUNT(*) FROM session_state`).Scan(&rows); err != nil {
		t.Fatalf("count session_state: %v", err)
	}
	if rows != 0 {
		t.Fatalf("fallback reads wrote %d session_state rows, want 0", rows)
	}
}

func TestSessionStateScopedRowSuppressesLegacyFallbackAfterClear(t *testing.T) {
	database, dir := setupSessionStateTestDB(t)
	if err := config.SetFocus(dir, "td-stale-config"); err != nil {
		t.Fatalf("config.SetFocus: %v", err)
	}
	scope := SessionStateScope{
		SessionID:      "ses_clear",
		WorktreeID:     "wt_clear",
		ConfigBaseDir:  dir,
		LegacyGetFocus: config.GetFocus,
	}

	if err := database.SetFocus(scope, "td-db-focus"); err != nil {
		t.Fatalf("SetFocus: %v", err)
	}
	if err := database.ClearFocus(scope); err != nil {
		t.Fatalf("ClearFocus: %v", err)
	}
	got, err := database.GetFocus(scope)
	if err != nil {
		t.Fatalf("GetFocus after clear: %v", err)
	}
	if got != "" {
		t.Fatalf("GetFocus after clear: got %q want empty", got)
	}
}

func sessionStateUpdatedAt(t *testing.T, database *DB, scope SessionStateScope) string {
	t.Helper()
	var updated string
	err := database.conn.QueryRow(
		`SELECT updated_at FROM session_state WHERE session_id = ? AND worktree_id = ?`,
		scope.SessionID, scope.WorktreeID,
	).Scan(&updated)
	if err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	return updated
}
