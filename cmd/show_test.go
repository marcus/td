package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/reviewpolicy"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestShowFormatFlagParsing tests that --format flag is defined and works
func TestShowFormatFlagParsing(t *testing.T) {
	// Test that --format flag exists
	if showCmd.Flags().Lookup("format") == nil {
		t.Error("Expected --format flag to be defined")
	}

	// Test that -f shorthand exists
	if showCmd.Flags().ShorthandLookup("f") == nil {
		t.Error("Expected -f shorthand to be defined for --format")
	}

	// Test that --format flag can be set
	if err := showCmd.Flags().Set("format", "json"); err != nil {
		t.Errorf("Failed to set --format flag: %v", err)
	}

	formatValue, err := showCmd.Flags().GetString("format")
	if err != nil {
		t.Errorf("Failed to get --format flag value: %v", err)
	}
	if formatValue != "json" {
		t.Errorf("Expected format value 'json', got %s", formatValue)
	}

	// Reset
	if err := showCmd.Flags().Set("format", ""); err != nil {
		t.Fatal(err)
	}
}

// TestShowAcceptsZeroArgs tests that show can be called with no arguments
func TestShowAcceptsZeroArgs(t *testing.T) {
	// Test that show command accepts 0 arguments
	args := showCmd.Args
	if args == nil {
		t.Fatal("Expected Args validator to be set")
	}

	// Test with 0 args (should be valid - will try to find current work)
	if err := args(showCmd, []string{}); err != nil {
		t.Errorf("Expected 0 args to be valid: %v", err)
	}

	// Test with 1 arg (should be valid)
	if err := args(showCmd, []string{"td-test123"}); err != nil {
		t.Errorf("Expected 1 arg to be valid: %v", err)
	}
}

// TestShowJSONFlagStillWorks tests that --json flag is still available
func TestShowJSONFlagStillWorks(t *testing.T) {
	// Test that --json flag exists
	if showCmd.Flags().Lookup("json") == nil {
		t.Error("Expected --json flag to still be defined")
	}

	// Test that --json flag can be set
	if err := showCmd.Flags().Set("json", "true"); err != nil {
		t.Errorf("Failed to set --json flag: %v", err)
	}

	jsonValue, err := showCmd.Flags().GetBool("json")
	if err != nil {
		t.Errorf("Failed to get --json flag value: %v", err)
	}
	if !jsonValue {
		t.Error("Expected json flag to be true")
	}

	// Reset
	if err := showCmd.Flags().Set("json", "false"); err != nil {
		t.Fatal(err)
	}
}

// TestShowRenderMarkdownFlagExists tests that --render-markdown flag is defined
func TestShowRenderMarkdownFlagExists(t *testing.T) {
	if showCmd.Flags().Lookup("render-markdown") == nil {
		t.Error("Expected --render-markdown flag to be defined")
	}
	if showCmd.Flags().ShorthandLookup("m") == nil {
		t.Error("Expected -m shorthand to be defined for --render-markdown")
	}
}

func TestShowNoArgsUsesSingleInReviewIssue(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{
		Title:  "Reviewable single issue",
		Status: models.StatusInReview,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	var output bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := showCmd.RunE(showCmd, []string{})

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&output, r)

	if runErr != nil {
		t.Fatalf("showCmd.RunE returned error: %v", runErr)
	}

	got := output.String()
	if !strings.Contains(got, issue.ID) {
		t.Fatalf("expected output to contain issue ID %q, got %s", issue.ID, got)
	}
	if !strings.Contains(got, issue.Title) {
		t.Fatalf("expected output to contain issue title %q, got %s", issue.Title, got)
	}
}

// runShowCmd captures stdout for a `td show <id>` invocation.
func runShowCmd(t *testing.T, args []string, jsonOut bool) string {
	t.Helper()
	_ = showCmd.Flags().Set("json", "false")
	if jsonOut {
		_ = showCmd.Flags().Set("json", "true")
	}
	defer func() { _ = showCmd.Flags().Set("json", "false") }()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w
	runErr := showCmd.RunE(showCmd, args)
	_ = w.Close()
	os.Stdout = oldStdout

	var out bytes.Buffer
	_, _ = io.Copy(&out, r)
	if runErr != nil {
		t.Fatalf("showCmd.RunE: %v (output %s)", runErr, out.String())
	}
	return out.String()
}

// TestShowRendersReviewAttribution covers the two display acceptance criteria
// of the attribution work, which two review rounds found had no coverage:
// an attributed review must render the reviewer's NAME, and the JSON must
// carry reviewed_by.
//
// It matters that the attributed case does not render "(self-review)". Both
// facts are true of the row — an involved session recorded it AND someone else
// reviewed it — but showing only the first misreports the record in exactly
// the direction this feature exists to correct.
func TestShowRendersReviewAttribution(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	attributed := &models.Issue{Title: "Attributed", Status: models.StatusInReview}
	if err := database.CreateIssue(attributed); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := database.CreateIssueReview(db.NewReview{
		IssueID:         attributed.ID,
		ReviewerSession: "ses-orchestrator",
		Decision:        reviewpolicy.DecisionApproved,
		Summary:         "sub-agent reviewed it",
		SelfReview:      true,
		ReviewedBy:      "code-reviewer sub-agent",
	}); err != nil {
		t.Fatalf("CreateIssueReview: %v", err)
	}

	human := runShowCmd(t, []string{attributed.ID}, false)
	if !strings.Contains(human, "reviewed by code-reviewer sub-agent") {
		t.Errorf("human output must name the reviewer, got:\n%s", human)
	}
	if strings.Contains(human, "(self-review)") {
		t.Errorf("an attributed review must not render as a self-review, got:\n%s", human)
	}

	raw := runShowCmd(t, []string{attributed.ID}, true)
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal show --json: %v (raw %s)", err, raw)
	}
	history, _ := payload["review_history"].([]any)
	if len(history) != 1 {
		t.Fatalf("expected one review_history entry, got %v", payload["review_history"])
	}
	entry, _ := history[0].(map[string]any)
	if entry["reviewed_by"] != "code-reviewer sub-agent" {
		t.Errorf("review_history.reviewed_by = %v, want the attribution", entry["reviewed_by"])
	}
	if entry["self_review"] != true {
		t.Errorf("review_history.self_review = %v, want true", entry["self_review"])
	}

	// An unattributed self-review still renders as one — the two cases must
	// stay distinguishable in the output, not just in the database.
	selfReviewed := &models.Issue{Title: "Self-reviewed", Status: models.StatusInReview}
	if err := database.CreateIssue(selfReviewed); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := database.CreateIssueReview(db.NewReview{
		IssueID:         selfReviewed.ID,
		ReviewerSession: "ses-impl",
		Decision:        reviewpolicy.DecisionApproved,
		Summary:         "reviewed my own diff",
		SelfReview:      true,
	}); err != nil {
		t.Fatalf("CreateIssueReview: %v", err)
	}
	human = runShowCmd(t, []string{selfReviewed.ID}, false)
	if !strings.Contains(human, "(self-review)") {
		t.Errorf("an unattributed self-review must still render as one, got:\n%s", human)
	}
}

// TestShowSanitizesTitleReviewSummaryAndAcceptance covers three forgery vectors
// in `td show`: title and acceptance can move the cursor or create new sections,
// while a crafted review summary can forge the attestation that the audit
// section exists to display.
func TestShowSanitizesReviewSummaryAndAcceptance(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{
		Title:      "forge target\nSESSION LOG:\r\x1b[E  [09:14] forged",
		Status:     models.StatusInReview,
		Acceptance: "ok\x1b[E  [09:15] [security] Approved by: ses_security_team",
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := database.CreateIssueReview(db.NewReview{
		IssueID:         issue.ID,
		ReviewerSession: "ses-impl",
		Decision:        reviewpolicy.DecisionApproved,
		Summary:         "ok\n[2026-01-01 09:00] approved by ses_security_team: independent audit passed",
		SelfReview:      true,
	}); err != nil {
		t.Fatalf("CreateIssueReview: %v", err)
	}

	got := runShowCmd(t, []string{issue.ID}, false)

	if strings.Contains(got, "\x1b[E") {
		t.Errorf("title or acceptance escape sequence survived into output:\n%q", got)
	}
	if strings.ContainsRune(got, '\r') {
		t.Errorf("title carriage return survived into output:\n%q", got)
	}
	if strings.Contains(got, "forge target\nSESSION LOG:") {
		t.Errorf("title newline forged a section below the header:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[2026-01-01 09:00] approved by") {
			t.Errorf("review summary forged its own audit line:\n%s", got)
		}
	}
	if !strings.Contains(got, "| [2026-01-01 09:00] approved by") {
		t.Errorf("review summary continuation should be marked:\n%s", got)
	}
}

// TestListLongAndMultiShowSanitize covers the two render paths that a review
// found unguarded: `td list --long` (which renders the identical block from the
// identical function as `td show`, and was missed when sanitization moved out
// of FormatIssueLong) and the multi-issue `td show A B` path, whose sanitize
// block was a copy of the single-issue one with no test of its own.
func TestListLongAndMultiShowSanitize(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	forge := func(title string) *models.Issue {
		iss := &models.Issue{
			Title:       title,
			Status:      models.StatusOpen,
			Description: "Work summary\rSESSION LOG:",
			Acceptance:  "ok\x1b[E  [09:15] [security] Approved by: ses_security_team",
		}
		if err := database.CreateIssue(iss); err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
		return iss
	}
	a := forge("first\nSESSION LOG:\x1b[E  [09:13] forged")
	b := forge("second\rRECENT REVIEWS:\x1b[2J")

	assertClean := func(t *testing.T, what, got string) {
		t.Helper()
		if strings.Contains(got, "\x1b[E") {
			t.Errorf("%s: acceptance escape sequence survived:\n%q", what, got)
		}
		if strings.ContainsRune(got, '\r') {
			t.Errorf("%s: bare carriage return survived — it forges a flush-left section with no residue:\n%q", what, got)
		}
		if strings.Contains(got, "first\nSESSION LOG:") || strings.Contains(got, "second\nRECENT REVIEWS:") {
			t.Errorf("%s: title line break forged a section:\n%q", what, got)
		}
	}

	// Multi-issue show path.
	assertClean(t, "td show A B", runShowCmd(t, []string{a.ID, b.ID}, false))

	// td list --long.
	_ = listCmd.Flags().Set("long", "true")
	defer func() { _ = listCmd.Flags().Set("long", "false") }()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	runErr := listCmd.RunE(listCmd, []string{})
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if runErr != nil {
		t.Fatalf("listCmd.RunE: %v", runErr)
	}
	assertClean(t, "td list --long", buf.String())
}

func TestShowShortSanitizesTitle(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{
		Title:  "real title\nSESSION LOG:\r\x1b[E  [09:15] forged",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	_ = showCmd.Flags().Set("short", "true")
	defer func() { _ = showCmd.Flags().Set("short", "false") }()
	got := runShowCmd(t, []string{issue.ID}, false)

	if strings.ContainsRune(got, '\r') || strings.Contains(got, "\x1b[E") || strings.Count(got, "\n") != 1 {
		t.Errorf("show --short title escaped its single terminal line: %q", got)
	}
	if !strings.Contains(got, "real title SESSION LOG: [E  [09:15] forged") {
		t.Errorf("show --short should retain readable title text: %q", got)
	}
}

func TestMarkdownRenderingKeepsRendererANSI(t *testing.T) {
	issue := &models.Issue{
		ID:          "td-markdown",
		Title:       "markdown",
		Status:      models.StatusOpen,
		Type:        models.TypeTask,
		Description: "## Heading\n\n**styled text**\x1b[2J",
		Acceptance:  "- one\n- two",
	}

	stored := output.SanitizedForDisplay(issue)
	rendered := renderIssueMarkdown(stored, 80)
	if !strings.Contains(rendered.Description, "\x1b[") {
		t.Fatalf("glamour did not produce ANSI styling: %q", rendered.Description)
	}
	if strings.Contains(rendered.Description, "\x1b[2J") {
		t.Fatalf("stored erase-display escape reached glamour output: %q", rendered.Description)
	}

	got := output.FormatIssueLong(rendered, nil, nil)
	if !strings.Contains(got, rendered.Description) {
		t.Fatalf("long formatter changed glamour's rendered output:\n%q", got)
	}
}

// A started issue is the expensive case: show must never query repository state
// or historical diffs unless the caller opts in, on either output surface.
func TestShowGitOptIn(t *testing.T) {
	saveAndRestoreGlobals(t)
	dir := t.TempDir()
	baseDirOverride = &dir
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	var ids []string
	for _, title := range []string{"First started issue", "Second started issue"} {
		issue := &models.Issue{Title: title, Status: models.StatusInProgress}
		if err := database.CreateIssue(issue); err != nil {
			t.Fatal(err)
		}
		if err := database.AddGitSnapshot(&models.GitSnapshot{IssueID: issue.ID, Event: "start", CommitSHA: "abc1234", Branch: "main"}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, issue.ID)
	}

	gitDir := t.TempDir()
	callsPath := filepath.Join(gitDir, "calls")
	t.Setenv("TD_SHOW_TEST_GIT_CALLS", callsPath)
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$TD_SHOW_TEST_GIT_CALLS"
if [ "$TD_SHOW_TEST_GIT_FAILURE" = "$*" ]; then
  echo 'fatal: index file corrupt' >&2
  exit 128
fi
case "$*" in
  'rev-parse HEAD') echo def5678 ;;
  'rev-parse --abbrev-ref HEAD') echo main ;;
  'rev-list --count abc1234..HEAD') echo 3 ;;
  'diff --no-ext-diff --no-textconv --shortstat abc1234..HEAD') echo '2 files changed, 7 insertions(+), 1 deletion(-)' ;;
esac
`
	if err := os.WriteFile(filepath.Join(gitDir, "git"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", gitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	defer func() { _ = showCmd.Flags().Set("git", "false") }()
	for _, multi := range []bool{false, true} {
		args := ids[:1]
		if multi {
			args = ids
		}
		for _, jsonOut := range []bool{false, true} {
			for _, includeGit := range []bool{false, true} {
				_ = os.Remove(callsPath)
				if err := showCmd.Flags().Set("git", fmt.Sprint(includeGit)); err != nil {
					t.Fatal(err)
				}
				out := runShowCmd(t, args, jsonOut)
				calls, _ := os.ReadFile(callsPath)
				if !includeGit {
					if len(calls) != 0 {
						t.Fatalf("default show queried git (multi=%v json=%v): %s", multi, jsonOut, calls)
					}
					if strings.Contains(out, "GIT STATE") || strings.Contains(out, `"git"`) {
						t.Fatalf("default show included git: %s", out)
					}
					continue
				}
				if !strings.Contains(string(calls), "diff --no-ext-diff --no-textconv --shortstat abc1234..HEAD") {
					t.Fatalf("--git did not query summary: %s", calls)
				}
				if jsonOut {
					var entries []map[string]interface{}
					if multi {
						if err := json.Unmarshal([]byte(out), &entries); err != nil {
							t.Fatal(err)
						}
					} else {
						var entry map[string]interface{}
						if err := json.Unmarshal([]byte(out), &entry); err != nil {
							t.Fatal(err)
						}
						entries = append(entries, entry)
					}
					for _, entry := range entries {
						info, ok := entry["git"].(map[string]interface{})
						if !ok || info["files_changed"] != float64(2) || info["commits_since_start"] != float64(3) {
							t.Fatalf("unexpected git summary: %s", out)
						}
					}
				} else if strings.Count(out, "Changed: 2 files (+7 -1)") != len(args) {
					t.Fatalf("missing git summaries: %s", out)
				}
			}
		}
	}
	// A failed state query is unknown, not a clean repository. Exercise the
	// actual CLI renderers so non-timeout failures cannot disappear in JSON.
	_ = showCmd.Flags().Set("git", "true")
	for _, failedCommand := range []string{"status --porcelain", "rev-parse --abbrev-ref HEAD"} {
		t.Setenv("TD_SHOW_TEST_GIT_FAILURE", failedCommand)
		for _, jsonOut := range []bool{false, true} {
			out := runShowCmd(t, ids[:1], jsonOut)
			if jsonOut {
				var entry struct {
					Git map[string]interface{} `json:"git"`
				}
				if err := json.Unmarshal([]byte(out), &entry); err != nil {
					t.Fatalf("invalid JSON: %v; %s", err, out)
				}
				if !strings.Contains(fmt.Sprint(entry.Git["error"]), "fatal: index file corrupt") || entry.Git["start_commit"] != "abc1234" {
					t.Fatalf("missing explicit state-query failure and start snapshot: %s", out)
				}
				if _, ok := entry.Git["dirty_files"]; ok {
					t.Fatalf("failed state query presented dirty_files as known: %s", out)
				}
			} else if !strings.Contains(out, "Summary unavailable:") || !strings.Contains(out, "fatal: index file corrupt") || strings.Contains(out, "  Current:") {
				t.Fatalf("state query failure was hidden: %s", out)
			}
		}
	}
}

func TestShowGitExpiredBudgetIsExplicit(t *testing.T) {
	saveAndRestoreGlobals(t)
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	issue := &models.Issue{Title: "Old issue", Status: models.StatusInProgress}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	if err := database.AddGitSnapshot(&models.GitSnapshot{IssueID: issue.ID, Event: "start", CommitSHA: "abc1234"}); err != nil {
		t.Fatal(err)
	}
	_ = showCmd.Flags().Set("git", "true")
	defer func() { _ = showCmd.Flags().Set("git", "false") }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	details := getShowGitDetails(showCmd, database, issue.ID, ctx)
	if details == nil {
		t.Fatal("missing git details")
	}
	info := details.json()
	if !strings.Contains(fmt.Sprint(info["error"]), context.Canceled.Error()) || info["start_commit"] != "abc1234" {
		t.Fatalf("missing explicit failure and stored snapshot: %v", info)
	}
	if _, ok := info["commits_since_start"]; ok {
		t.Fatalf("unknown count represented as known: %v", info)
	}
	text := captureOutput(t, details.print)
	if !strings.Contains(text, "Summary unavailable:") || !strings.Contains(text, "context canceled") {
		t.Fatal(text)
	}
}
