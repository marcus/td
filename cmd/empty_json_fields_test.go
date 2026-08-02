package cmd

import (
	"encoding/json"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
)

// TestShowJSONAlwaysCarriesLogsAndHandoff pins the shape decided for td-9ff2fe:
// list-valued fields are always present as [], singular objects always present
// as null. An absent key was a third rendering of "nothing" that a caller could
// not field-access at all.
func TestShowJSONAlwaysCarriesLogsAndHandoff(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDirOverride = &dir
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	issue := &models.Issue{Title: "bare", Status: models.StatusOpen}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	setJSONFlag(t, true)
	withJSONFlag(t, showCmd)

	out := captureStdout(t, func() {
		if err := showCmd.RunE(showCmd, []string{issue.ID}); err != nil {
			t.Fatalf("show: %v", err)
		}
	})

	var result map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("show --json is not valid JSON (%v): %q", err, out)
	}

	logs, ok := result["logs"]
	if !ok {
		t.Fatal(`show --json must always carry a "logs" key`)
	}
	if string(logs) != "[]" {
		t.Errorf("logs = %s, want []", logs)
	}

	handoff, ok := result["handoff"]
	if !ok {
		t.Fatal(`show --json must always carry a "handoff" key`)
	}
	if string(handoff) != "null" {
		t.Errorf("handoff = %s, want null", handoff)
	}
}

// TestBuildTreeAlwaysCarriesChildren covers the tree side: every node has a
// children key, empty for a leaf and for a node cut off by --depth, so a walker
// never has to test for the key's existence.
func TestBuildTreeAlwaysCarriesChildren(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	parent := &models.Issue{Title: "parent", Status: models.StatusOpen}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue(parent): %v", err)
	}
	child := &models.Issue{Title: "child", Status: models.StatusOpen, ParentID: parent.ID}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue(child): %v", err)
	}

	// Unlimited depth: the leaf still reports an empty list, not a missing key.
	node := buildTree(database, parent.ID, 0, 0)
	children, ok := node["children"].([]map[string]interface{})
	if !ok {
		t.Fatalf(`root node must carry a "children" list, got %#v`, node["children"])
	}
	if len(children) != 1 {
		t.Fatalf("root children = %d, want 1", len(children))
	}
	leafChildren, ok := children[0]["children"].([]map[string]interface{})
	if !ok {
		t.Fatalf(`leaf node must carry a "children" list, got %#v`, children[0]["children"])
	}
	if len(leafChildren) != 0 {
		t.Errorf("leaf children = %d, want 0", len(leafChildren))
	}

	// A node at the depth cut carries the key too, empty.
	cut := buildTree(database, parent.ID, 0, 1)
	cutChildren, ok := cut["children"].([]map[string]interface{})
	if !ok {
		t.Fatalf(`depth-limited root must carry a "children" list, got %#v`, cut["children"])
	}
	if len(cutChildren) != 1 {
		t.Fatalf("depth-limited root children = %d, want 1", len(cutChildren))
	}
	if _, ok := cutChildren[0]["children"]; !ok {
		t.Error(`a node at the --depth cut must still carry a "children" key`)
	}

	// The whole tree must marshal with the key present at every level.
	blob, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal tree: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("unmarshal tree: %v", err)
	}
	if _, ok := decoded["children"]; !ok {
		t.Error(`marshalled tree lost its "children" key`)
	}
}

// TestCheckHandoffJSONAlwaysCarriesInProgressIssues extends the td-9ff2fe shape
// to `td check-handoff --json`, which omitted in_progress_issues entirely when
// nothing was in progress — the exact case a caller hits most often, and the
// one where a missing key is hardest to notice.
func TestCheckHandoffJSONAlwaysCarriesInProgressIssues(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDirOverride = &dir
	t.Setenv("TD_SESSION_ID", "ses_check_handoff_json")
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	// check-handoff filters by the session GetOrCreate hands it, so the test
	// must claim the issue for that same session.
	sess, err := session.GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	setJSONFlag(t, true)
	withJSONFlag(t, checkHandoffCmd)

	runCheckHandoff := func() map[string]json.RawMessage {
		t.Helper()
		out := captureStdout(t, func() {
			_ = checkHandoffCmd.RunE(checkHandoffCmd, nil)
		})
		var result map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("check-handoff --json is not valid JSON (%v): %q", err, out)
		}
		return result
	}

	// Empty case: the key is present and is [], not absent and not null.
	result := runCheckHandoff()
	ids, ok := result["in_progress_issues"]
	if !ok {
		t.Fatal(`check-handoff --json must always carry an "in_progress_issues" key`)
	}
	if string(ids) != "[]" {
		t.Errorf("in_progress_issues = %s, want []", ids)
	}

	// Populated case: the same key still reports the real work.
	issue := &models.Issue{Title: "in flight", Status: models.StatusOpen}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	issue.Status = models.StatusInProgress
	issue.ImplementerSession = sess.ID
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	result = runCheckHandoff()
	var populated []string
	if err := json.Unmarshal(result["in_progress_issues"], &populated); err != nil {
		t.Fatalf("in_progress_issues is not a list: %v", err)
	}
	if len(populated) != 1 || populated[0] != issue.ID {
		t.Errorf("in_progress_issues = %v, want [%s]", populated, issue.ID)
	}
}

// TestCloseJSONAlwaysCarriesCascadeLists covers the two cascade lists on
// `td close --json`. Both were dropped from the envelope when nothing
// cascaded, so a caller reading result["unblocked_dependents"] worked only on
// the runs where something happened to unblock — the worst possible shape to
// discover in production.
func TestCloseJSONAlwaysCarriesCascadeLists(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDirOverride = &dir
	t.Setenv("TD_SESSION_ID", "ses_close_cascade_json")
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	setJSONFlag(t, true)
	withJSONFlag(t, closeCmd)

	runClose := func(id string) map[string]json.RawMessage {
		t.Helper()
		out := captureStdout(t, func() {
			if err := closeCmd.RunE(closeCmd, []string{id}); err != nil {
				t.Fatalf("close %s: %v", id, err)
			}
		})
		var result map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("close --json is not valid JSON (%v): %q", err, out)
		}
		return result
	}

	// Empty case: a standalone issue with no parent and no dependents.
	lonely := &models.Issue{Title: "standalone", Status: models.StatusOpen}
	if err := database.CreateIssue(lonely); err != nil {
		t.Fatalf("CreateIssue(lonely): %v", err)
	}

	result := runClose(lonely.ID)
	for _, key := range []string{"cascaded_parents", "unblocked_dependents"} {
		raw, ok := result[key]
		if !ok {
			t.Fatalf("close --json must always carry a %q key", key)
		}
		if string(raw) != "[]" {
			t.Errorf("%s = %s, want []", key, raw)
		}
	}

	// Populated case: closing the only child cascades the parent closed, and
	// the blocked dependent is auto-unblocked.
	parent := &models.Issue{Title: "epic", Status: models.StatusOpen, Type: models.TypeEpic}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue(parent): %v", err)
	}
	child := &models.Issue{Title: "only child", Status: models.StatusOpen, ParentID: parent.ID}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue(child): %v", err)
	}
	dependent := &models.Issue{Title: "waiting", Status: models.StatusBlocked}
	if err := database.CreateIssue(dependent); err != nil {
		t.Fatalf("CreateIssue(dependent): %v", err)
	}
	if err := database.AddDependency(dependent.ID, child.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	result = runClose(child.ID)

	var parents []string
	if err := json.Unmarshal(result["cascaded_parents"], &parents); err != nil {
		t.Fatalf("cascaded_parents is not a list: %v", err)
	}
	if len(parents) != 1 || parents[0] != parent.ID {
		t.Errorf("cascaded_parents = %v, want [%s]", parents, parent.ID)
	}

	var unblocked []string
	if err := json.Unmarshal(result["unblocked_dependents"], &unblocked); err != nil {
		t.Fatalf("unblocked_dependents is not a list: %v", err)
	}
	if len(unblocked) != 1 || unblocked[0] != dependent.ID {
		t.Errorf("unblocked_dependents = %v, want [%s]", unblocked, dependent.ID)
	}
}
