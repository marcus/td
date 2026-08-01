package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcus/td/internal/models"
)

// This file covers td-1e090e: --json surfaces that already existed but emitted
// a bare `null` for an empty result, where the documented contract is `[]`.
// A caller that has to special-case null before it can range over the answer
// is a caller that will forget to.
//
// Two shapes are covered, because the defect has two forms:
//
//  1. Bare top-level lists (`td list --json` with no matching issues).
//  2. List-valued FIELDS nested inside an object (`td depends-on --json`'s
//     "dependencies"), which no top-level fix reaches.
//
// A genuinely singular field is NOT covered here: `usage --json`'s "focused"
// and "work_session" are single objects, and null is the correct rendering of
// "nothing is focused". The rule is about lists, not about nulls.

// requireEmptyJSONField asserts a named field of a JSON object serialized as
// an empty array rather than null.
func requireEmptyJSONField(t *testing.T, out, field string) {
	t.Helper()
	var result map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		t.Fatalf("output is not a JSON object: %v\noutput: %q", err, out)
	}
	raw, ok := result[field]
	if !ok {
		t.Fatalf("output has no %q field: %q", field, out)
	}
	if strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("%q must serialize as [] when empty, got: %s", field, raw)
	}
}

// TestListJSONEmptyIsArray pins the flagship case: `td list --json` in a
// project with no issues.
func TestListJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := listCmd.RunE(listCmd, nil); err != nil {
			t.Fatalf("list RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

// TestListJSONNonEmptyStillArray guards against a fix that emits [] for
// everything.
func TestListJSONNonEmptyStillArray(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Listed task", models.StatusOpen, "")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := listCmd.RunE(listCmd, nil); err != nil {
			t.Fatalf("list RunE: %v", err)
		}
	})
	issues := decodeIssueArray(t, out)
	if len(issues) != 1 || issues[0].ID != issue.ID {
		t.Fatalf("list --json = %+v, want the one issue %s", issues, issue.ID)
	}
}

// TestDeletedJSONEmptyIsArray covers the deleted-issue listing, which shares
// list.go's result plumbing.
func TestDeletedJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := deletedCmd.RunE(deletedCmd, nil); err != nil {
			t.Fatalf("deleted RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

// TestNoteListJSONEmptyIsArray covers a non-issue list surface.
func TestNoteListJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := noteListCmd.RunE(noteListCmd, nil); err != nil {
			t.Fatalf("note list RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

// TestDependsOnJSONEmptyFieldIsArray covers the nested-field form of the bug:
// the object is fine, but its list-valued field was null. This is the instance
// the ticket names.
func TestDependsOnJSONEmptyFieldIsArray(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "No dependencies", models.StatusOpen, "")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := dependsOnCmd.RunE(dependsOnCmd, []string{issue.ID}); err != nil {
			t.Fatalf("depends-on RunE: %v", err)
		}
	})
	requireEmptyJSONField(t, out, "dependencies")
}

// TestBlockedByJSONEmptyFieldIsArray covers the sibling dependency read.
func TestBlockedByJSONEmptyFieldIsArray(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Nothing blocked", models.StatusOpen, "")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := blockedByCmd.RunE(blockedByCmd, []string{issue.ID}); err != nil {
			t.Fatalf("blocked-by RunE: %v", err)
		}
	})
	requireEmptyJSONField(t, out, "direct")
}

// TestShowJSONEmptyLabelsIsArray covers `td show --json` on an issue with no
// labels: "labels" is a list field and must not be null.
func TestShowJSONEmptyLabelsIsArray(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Unlabelled", models.StatusOpen, "")

	// showCmd declares its OWN --json flag, which shadows the root persistent
	// one, so setJSONFlag does not reach it. Set the local flag directly.
	if err := showCmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set show --json: %v", err)
	}
	t.Cleanup(func() { _ = showCmd.Flags().Set("json", "false") })

	out := captureStdout(t, func() {
		if err := showCmd.RunE(showCmd, []string{issue.ID}); err != nil {
			t.Fatalf("show RunE: %v", err)
		}
	})
	requireEmptyJSONField(t, out, "labels")
}

// TestStatusJSONEmptyReadyIsArray covers the dashboard read, whose
// ready_to_start field was null in an empty project.
func TestStatusJSONEmptyReadyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := statusCmd.RunE(statusCmd, nil); err != nil {
			t.Fatalf("status RunE: %v", err)
		}
	})
	requireEmptyJSONField(t, out, "ready_to_start")
}

// TestUsageJSONEmptyListsAreArrays covers the command CLAUDE.md tells agents to
// run first in a new session — the worst place to hand back a null.
func TestUsageJSONEmptyListsAreArrays(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := usageCmd.RunE(usageCmd, nil); err != nil {
			t.Fatalf("usage RunE: %v", err)
		}
	})
	for _, field := range []string{"in_progress", "reviewable", "ready_to_close", "ready", "ws_issues"} {
		requireEmptyJSONField(t, out, field)
	}
}

// TestUsageJSONSingularFieldsStayNull pins the boundary of the contract: a
// field holding one object, not a list, still reports null when there is
// nothing — replacing that with [] would be a different lie.
func TestUsageJSONSingularFieldsStayNull(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := usageCmd.RunE(usageCmd, nil); err != nil {
			t.Fatalf("usage RunE: %v", err)
		}
	})
	var result map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		t.Fatalf("output is not a JSON object: %v\noutput: %q", err, out)
	}
	for _, field := range []string{"focused", "work_session"} {
		raw, ok := result[field]
		if !ok {
			t.Fatalf("output has no %q field: %q", field, out)
		}
		if strings.TrimSpace(string(raw)) != "null" {
			t.Fatalf("%q holds a single object and should stay null when unset, got: %s", field, raw)
		}
	}
}
