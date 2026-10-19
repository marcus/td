package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func setListCompletenessFlag(t *testing.T, name, value string) {
	t.Helper()
	flag := listCmd.Flags().Lookup(name)
	orig, changed := flag.Value.String(), flag.Changed
	if err := listCmd.Flags().Set(name, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = flag.Value.Set(orig)
		flag.Changed = changed
	})
}

func runListCompleteness(t *testing.T, args ...string) (string, string) {
	t.Helper()
	var stderr bytes.Buffer
	orig := listCmd.ErrOrStderr()
	listCmd.SetErr(&stderr)
	t.Cleanup(func() { listCmd.SetErr(orig) })
	out := captureStdout(t, func() {
		if err := listCmd.RunE(listCmd, args); err != nil {
			t.Fatal(err)
		}
	})
	return out, stderr.String()
}

func seedListCompleteness(t *testing.T, database *db.DB, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		newReadJSONIssue(t, database, fmt.Sprintf("Matching issue %03d", i), models.StatusOpen, "")
	}
}

func TestListCompletenessDefaultLimit(t *testing.T) {
	for _, count := range []int{50, 55} {
		for _, format := range []string{"text", "json", "format-json", "long"} {
			t.Run(fmt.Sprintf("%d/%s", count, format), func(t *testing.T) {
				database, _ := setupReadJSONTest(t)
				seedListCompleteness(t, database, count)
				switch format {
				case "json":
					setJSONFlag(t, true)
				case "format-json":
					setListCompletenessFlag(t, "format", "json")
				case "long":
					setListCompletenessFlag(t, "long", "true")
				}
				out, stderr := runListCompleteness(t)
				if strings.Contains(format, "json") {
					if issues := decodeIssueArray(t, out); len(issues) != 50 {
						t.Fatalf("got %d issues, want 50", len(issues))
					}
				} else if got := strings.Count(out, "Matching issue "); got != 50 {
					t.Fatalf("rendered %d issues, want 50", got)
				}
				if count == 50 && stderr != "" {
					t.Fatalf("complete result warned: %q", stderr)
				}
				if count > 50 && !strings.Contains(stderr, "50 of 55 issues shown; use --limit N or --limit 0") {
					t.Fatalf("missing useful truncation notice: %q", stderr)
				}
			})
		}
	}
}

func TestListCompletenessEnvelopeAndUnlimited(t *testing.T) {
	for _, tdq := range []bool{false, true} {
		for _, limit := range []int{0, 3, 50} {
			t.Run(fmt.Sprintf("tdq=%t/limit=%d", tdq, limit), func(t *testing.T) {
				database, _ := setupReadJSONTest(t)
				seedListCompleteness(t, database, 55)
				newReadJSONIssue(t, database, "Other issue", models.StatusOpen, "")
				setListCompletenessFlag(t, "json-envelope", "true")
				setListCompletenessFlag(t, "limit", fmt.Sprint(limit))
				setListCompletenessFlag(t, "sort", "title")
				setListCompletenessFlag(t, "reverse", "true")
				var args []string
				if tdq {
					// The matching set must be counted after the TDQ matcher runs.
					args = []string{`title ~ "Matching issue"`}
				} else {
					setListCompletenessFlag(t, "search", "Matching issue")
				}
				out, stderr := runListCompleteness(t, args...)
				var result listEnvelope
				if err := json.Unmarshal([]byte(out), &result); err != nil {
					t.Fatal(err)
				}
				shown := limit
				if shown == 0 {
					shown = 55
				}
				if result.Total != 55 || result.Shown != shown || result.Limit != limit || result.Truncated != (shown < 55) || !result.TotalExact || result.ScanLimited || len(result.Issues) != shown {
					t.Fatalf("incorrect completeness metadata: %+v", result)
				}
				if result.Issues[0].Title != "Matching issue 054" {
					t.Fatalf("limit applied before descending sort: first=%q", result.Issues[0].Title)
				}
				if stderr != "" {
					t.Fatalf("envelope output needs no stderr notice: %q", stderr)
				}
			})
		}
	}
}

func TestListCompletenessTDQLegacyJSON(t *testing.T) {
	for _, count := range []int{50, 55} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			database, _ := setupReadJSONTest(t)
			seedListCompleteness(t, database, count)
			setJSONFlag(t, true)
			setListCompletenessFlag(t, "filter", "status = open")
			out, stderr := runListCompleteness(t)
			if issues := decodeIssueArray(t, out); len(issues) != 50 {
				t.Fatalf("got %d issues, want 50", len(issues))
			}
			if count == 55 && !strings.Contains(stderr, "50 of 55 issues shown") {
				t.Fatalf("missing TDQ notice: %q", stderr)
			}
			if count == 50 && stderr != "" {
				t.Fatalf("complete TDQ result warned: %q", stderr)
			}
		})
	}
}

func TestListCompletenessAllStillIncludesClosed(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	seedListCompleteness(t, database, 55)
	newReadJSONIssue(t, database, "Closed issue", models.StatusClosed, "ses_other")
	setListCompletenessFlag(t, "all", "true")
	setListCompletenessFlag(t, "json-envelope", "true")
	out, _ := runListCompleteness(t)
	var result listEnvelope
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 56 || result.Shown != 50 || !result.Truncated {
		t.Fatalf("--all must include closed issues but retain output limit: %+v", result)
	}
}

func TestListCompletenessReviewableFiltersBeforeLimit(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	for i := 0; i < 6; i++ {
		issue := newReadJSONIssue(t, database, fmt.Sprintf("Review %d", i), models.StatusInReview, "ses_other")
		if i < 3 {
			if _, err := database.CreateIssueReview(db.NewReview{IssueID: issue.ID, ReviewerSession: "ses_reviewer", Decision: "approved"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	setListCompletenessFlag(t, "reviewable", "true")
	setListCompletenessFlag(t, "sort", "title")
	setListCompletenessFlag(t, "limit", "2")
	setListCompletenessFlag(t, "json-envelope", "true")
	out, _ := runListCompleteness(t)
	var result listEnvelope
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || result.Shown != 2 || result.Issues[0].Title != "Review 3" || !result.Truncated {
		t.Fatalf("approval filtering must precede counting and limiting: %+v", result)
	}

	setListCompletenessFlag(t, "include-approved", "true")
	out, _ = runListCompleteness(t)
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 6 || result.Shown != 2 || result.Issues[0].Title != "Review 0" || !result.Truncated {
		t.Fatalf("review buckets must share sorting, counting, and one limit: %+v", result)
	}
}

func TestListCompletenessEmptyEnvelope(t *testing.T) {
	setupReadJSONTest(t)
	setListCompletenessFlag(t, "json-envelope", "true")
	out, stderr := runListCompleteness(t)
	requireEmptyJSONField(t, out, "issues")
	if stderr != "" {
		t.Fatalf("empty complete result warned: %q", stderr)
	}
}

func TestListCompletenessUnknownTDQTotal(t *testing.T) {
	result := newListEnvelope(nil, 0, 50, true)
	if result.TotalExact || !result.ScanLimited || !result.Truncated {
		t.Fatalf("scan cap must not look complete: %+v", result)
	}
	var stderr bytes.Buffer
	orig := listCmd.ErrOrStderr()
	listCmd.SetErr(&stderr)
	t.Cleanup(func() { listCmd.SetErr(orig) })
	result.notice(listCmd)
	if !strings.Contains(stderr.String(), "total unknown") || !strings.Contains(stderr.String(), "--limit 0 removes only the output limit") {
		t.Fatalf("missing scan-cap explanation: %q", stderr.String())
	}
}
