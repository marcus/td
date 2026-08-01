package cmd

import (
	"encoding/json"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
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
