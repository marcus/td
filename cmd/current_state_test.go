package cmd

import (
	"os"
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func setTestBaseDir(t *testing.T, dir string) {
	t.Helper()
	saveAndRestoreGlobals(t)
	baseDirOverride = &dir
}

func setTestContext(t *testing.T, contextID string) {
	t.Helper()
	if os.Getenv("TD_SESSION_ID") == "" {
		t.Setenv("TD_SESSION_ID", "current-state-agent")
	}
	if err := os.Setenv("TD_CONTEXT_ID", contextID); err != nil {
		t.Fatalf("Setenv TD_CONTEXT_ID failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("TD_CONTEXT_ID")
	})
}

func resetLogCommandFlags(t *testing.T) {
	t.Helper()
	for name, value := range map[string]string{
		"issue": "",
		"task":  "",
		"type":  "",
	} {
		if err := logCmd.Flags().Set(name, value); err != nil {
			t.Fatalf("reset log flag %s failed: %v", name, err)
		}
	}
	for _, name := range []string{"blocker", "decision", "hypothesis", "tried", "result"} {
		if err := logCmd.Flags().Set(name, "false"); err != nil {
			t.Fatalf("reset log flag %s failed: %v", name, err)
		}
	}
}

func TestCLIFocusIsScopedBySessionState(t *testing.T) {
	dir := t.TempDir()
	setTestBaseDir(t, dir)
	t.Setenv("TD_SESSION_ID", "current-state-focus-agent")

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issueA := &models.Issue{Title: "Session A focus", Status: models.StatusOpen}
	issueB := &models.Issue{Title: "Session B focus", Status: models.StatusOpen}
	if err := database.CreateIssue(issueA); err != nil {
		t.Fatalf("CreateIssue A failed: %v", err)
	}
	if err := database.CreateIssue(issueB); err != nil {
		t.Fatalf("CreateIssue B failed: %v", err)
	}

	setTestContext(t, "ctx-focus-a")
	if err := focusCmd.RunE(focusCmd, []string{issueA.ID}); err != nil {
		t.Fatalf("focus A failed: %v", err)
	}
	sessA, scopeA, err := getCurrentStateSession(database, dir)
	if err != nil {
		t.Fatalf("session A failed: %v", err)
	}

	setTestContext(t, "ctx-focus-b")
	if err := focusCmd.RunE(focusCmd, []string{issueB.ID}); err != nil {
		t.Fatalf("focus B failed: %v", err)
	}
	sessB, scopeB, err := getCurrentStateSession(database, dir)
	if err != nil {
		t.Fatalf("session B failed: %v", err)
	}
	if sessA.ID == sessB.ID {
		t.Fatalf("expected distinct sessions, both got %s", sessA.ID)
	}

	focusedA, err := database.GetFocus(scopeA)
	if err != nil {
		t.Fatalf("GetFocus A failed: %v", err)
	}
	focusedB, err := database.GetFocus(scopeB)
	if err != nil {
		t.Fatalf("GetFocus B failed: %v", err)
	}
	if focusedA != issueA.ID || focusedB != issueB.ID {
		t.Fatalf("scoped focus mismatch: A=%q B=%q", focusedA, focusedB)
	}

	legacyFocus, err := config.GetFocus(dir)
	if err != nil {
		t.Fatalf("legacy GetFocus failed: %v", err)
	}
	if legacyFocus != "" {
		t.Fatalf("focus command wrote legacy config focus %q", legacyFocus)
	}
}

func TestCLIActiveWorkSessionIsScopedBySessionState(t *testing.T) {
	dir := t.TempDir()
	setTestBaseDir(t, dir)
	t.Setenv("TD_SESSION_ID", "current-state-ws-agent")

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	setTestContext(t, "ctx-ws-a")
	if err := wsStartCmd.RunE(wsStartCmd, []string{"session-a"}); err != nil {
		t.Fatalf("ws start A failed: %v", err)
	}
	_, scopeA, err := getCurrentStateSession(database, dir)
	if err != nil {
		t.Fatalf("session A failed: %v", err)
	}
	activeA, err := database.GetActiveWorkSession(scopeA)
	if err != nil {
		t.Fatalf("GetActiveWorkSession A failed: %v", err)
	}

	setTestContext(t, "ctx-ws-b")
	if err := wsStartCmd.RunE(wsStartCmd, []string{"session-b"}); err != nil {
		t.Fatalf("ws start B failed: %v", err)
	}
	_, scopeB, err := getCurrentStateSession(database, dir)
	if err != nil {
		t.Fatalf("session B failed: %v", err)
	}
	activeB, err := database.GetActiveWorkSession(scopeB)
	if err != nil {
		t.Fatalf("GetActiveWorkSession B failed: %v", err)
	}

	if activeA == "" || activeB == "" || activeA == activeB {
		t.Fatalf("expected distinct active work sessions, got A=%q B=%q", activeA, activeB)
	}
	recheckedA, err := database.GetActiveWorkSession(scopeA)
	if err != nil {
		t.Fatalf("recheck A failed: %v", err)
	}
	if recheckedA != activeA {
		t.Fatalf("session B overwrote session A active ws: got %q want %q", recheckedA, activeA)
	}

	legacyActive, err := config.GetActiveWorkSession(dir)
	if err != nil {
		t.Fatalf("legacy GetActiveWorkSession failed: %v", err)
	}
	if legacyActive != "" {
		t.Fatalf("ws start wrote legacy active work session %q", legacyActive)
	}
}

func TestCLILegacyFocusFallbackReadOnlyAndClearWins(t *testing.T) {
	dir := t.TempDir()
	setTestBaseDir(t, dir)
	t.Setenv("TD_SESSION_ID", "current-state-legacy-agent")
	setTestContext(t, "ctx-legacy-focus")

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Legacy focused issue", Status: models.StatusInProgress}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("legacy SetFocus failed: %v", err)
	}

	resetLogCommandFlags(t)
	if err := logCmd.RunE(logCmd, []string{"legacy fallback log"}); err != nil {
		t.Fatalf("log with legacy focus fallback failed: %v", err)
	}
	logs, err := database.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("expected log command to use legacy focus fallback")
	}

	if err := unfocusCmd.RunE(unfocusCmd, nil); err != nil {
		t.Fatalf("unfocus failed: %v", err)
	}
	legacyFocus, err := config.GetFocus(dir)
	if err != nil {
		t.Fatalf("legacy GetFocus failed: %v", err)
	}
	if legacyFocus != issue.ID {
		t.Fatalf("unfocus should not clear legacy config focus: got %q want %q", legacyFocus, issue.ID)
	}

	_, scope, err := getCurrentStateSession(database, dir)
	if err != nil {
		t.Fatalf("getCurrentStateSession failed: %v", err)
	}
	scopedFocus, err := database.GetFocus(scope)
	if err != nil {
		t.Fatalf("GetFocus failed: %v", err)
	}
	if scopedFocus != "" {
		t.Fatalf("scoped focus should stay cleared, got %q", scopedFocus)
	}

	resetLogCommandFlags(t)
	err = logCmd.RunE(logCmd, []string{"should not use cleared legacy fallback"})
	if err == nil {
		t.Fatal("expected log to ignore legacy fallback after scoped clear")
	}
}
