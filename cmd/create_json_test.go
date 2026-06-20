package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
)

// setJSONFlag flips the inherited persistent --json flag on rootCmd for the
// duration of a test, restoring it afterward. create/add read --json via
// jsonMode(), which resolves the inherited persistent flag, so the flag must
// live on rootCmd (not the local createCmd flag set).
func setJSONFlag(t *testing.T, on bool) {
	t.Helper()
	flag := rootCmd.PersistentFlags().Lookup("json")
	if flag == nil {
		t.Fatalf("rootCmd is missing the persistent --json flag")
	}
	orig := flag.Value.String()
	val := "false"
	if on {
		val = "true"
	}
	if err := rootCmd.PersistentFlags().Set("json", val); err != nil {
		t.Fatalf("set json flag: %v", err)
	}
	t.Cleanup(func() {
		_ = rootCmd.PersistentFlags().Set("json", orig)
	})
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	fn()

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// TestCreateJSONOutputEmitsFullIssue verifies that `td add "..." --json`
// prints a JSON envelope whose "id" matches the created issue and whose
// "issue" object carries the full record.
func TestCreateJSONOutputEmitsFullIssue(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := createCmd.RunE(createCmd, []string{"JSON output smoke task"}); err != nil {
			t.Fatalf("createCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Action string `json:"action"`
		Issue  struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Type  string `json:"type"`
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}

	if env.ID == "" {
		t.Fatalf("expected non-empty id in JSON envelope, got: %q", out)
	}
	if !strings.HasPrefix(env.ID, "td-") {
		t.Fatalf("expected id to start with td-, got %q", env.ID)
	}
	if env.Action != "created" {
		t.Fatalf("expected action=created, got %q", env.Action)
	}
	if env.Issue.ID != env.ID {
		t.Fatalf("envelope id %q does not match issue.id %q", env.ID, env.Issue.ID)
	}
	if env.Issue.Title != "JSON output smoke task" {
		t.Fatalf("expected full issue with title, got %q", env.Issue.Title)
	}

	// The id must round-trip to a real issue in the database.
	if _, err := database.GetIssue(env.ID); err != nil {
		t.Fatalf("created issue %q not found in db: %v", env.ID, err)
	}

	// Human output must NOT appear in json mode.
	if strings.Contains(out, "CREATED ") {
		t.Fatalf("json output should not contain human CREATED line: %q", out)
	}
}

// TestCreateHumanOutputUnchanged verifies that without --json the command still
// prints exactly "CREATED td-...".
func TestCreateHumanOutputUnchanged(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	setJSONFlag(t, false)

	out := captureStdout(t, func() {
		if err := createCmd.RunE(createCmd, []string{"Human output smoke task"}); err != nil {
			t.Fatalf("createCmd.RunE failed: %v", err)
		}
	})

	if !strings.HasPrefix(out, "CREATED td-") {
		t.Fatalf("expected human output to start with 'CREATED td-', got %q", out)
	}
	if strings.Contains(out, "{") {
		t.Fatalf("human output should not contain JSON, got %q", out)
	}
}

// TestEpicCreateJSONOutput verifies that `td epic create "..." --json`
// flows through createCmd.RunE and emits a JSON envelope for the new epic.
func TestEpicCreateJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := epicCreateCmd.RunE(epicCreateCmd, []string{"JSON epic via delegation"}); err != nil {
			t.Fatalf("epicCreateCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID    string `json:"id"`
		Issue struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("epic json output invalid: %v\noutput: %q", err, out)
	}
	if env.ID == "" || !strings.HasPrefix(env.ID, "td-") {
		t.Fatalf("expected td- id from epic create --json, got %q", out)
	}
	if env.Issue.Type != "epic" {
		t.Fatalf("expected issue.type=epic, got %q", env.Issue.Type)
	}
}
