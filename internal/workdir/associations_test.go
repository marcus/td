package workdir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAssociations_MissingFile(t *testing.T) {
	// Point HOME to a temp dir so ConfigDir returns an empty config
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	assoc, err := LoadAssociations()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(assoc) != 0 {
		t.Fatalf("expected empty map, got %v", assoc)
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	input := map[string]string{
		"/Users/alice/code/repo-one":   "/Users/alice/notes/vault-one",
		"/Users/alice/code/repo-two":   "/Users/alice/notes/vault-two",
	}

	if err := SaveAssociations(input); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := LoadAssociations()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if len(loaded) != len(input) {
		t.Fatalf("expected %d entries, got %d", len(input), len(loaded))
	}
	for k, v := range input {
		if loaded[k] != v {
			t.Errorf("key %s: expected %s, got %s", k, v, loaded[k])
		}
	}
}

func TestLookupAssociation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Create config dir and write associations
	configDir := filepath.Join(tmp, ".config", "td")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	assoc := map[string]string{
		"/Users/alice/code/myrepo": "/Users/alice/projects/myproject",
	}
	data, _ := json.Marshal(assoc)
	if err := os.WriteFile(filepath.Join(configDir, associationsFile), data, 0644); err != nil {
		t.Fatal(err)
	}

	// Found
	target, ok := LookupAssociation("/Users/alice/code/myrepo")
	if !ok {
		t.Fatal("expected association to be found")
	}
	if target != "/Users/alice/projects/myproject" {
		t.Errorf("expected /Users/alice/projects/myproject, got %s", target)
	}

	// Not found
	_, ok = LookupAssociation("/Users/alice/code/other")
	if ok {
		t.Fatal("expected no association")
	}
}

func TestResolveBaseDir_TdRootPriorityOverAssociation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Set up a directory with both .td-root and an association
	projectDir := filepath.Join(tmp, "project")
	tdRootTarget := filepath.Join(tmp, "tdroot-target")
	assocTarget := filepath.Join(tmp, "assoc-target")

	for _, d := range []string{projectDir, tdRootTarget, assocTarget} {
		if err := os.MkdirAll(filepath.Join(d, ".todos"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Write .td-root pointing to tdRootTarget
	if err := os.WriteFile(filepath.Join(projectDir, ".td-root"), []byte(tdRootTarget), 0644); err != nil {
		t.Fatal(err)
	}

	// Write association pointing to assocTarget
	configDir := filepath.Join(tmp, ".config", "td")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	assoc := map[string]string{projectDir: assocTarget}
	data, _ := json.Marshal(assoc)
	if err := os.WriteFile(filepath.Join(configDir, associationsFile), data, 0644); err != nil {
		t.Fatal(err)
	}

	// .td-root should win
	result := ResolveBaseDir(projectDir)
	if result != tdRootTarget {
		t.Errorf("expected .td-root target %s, got %s", tdRootTarget, result)
	}
}

func TestResolveBaseDir_AssociationUsed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Set up a directory with only an association (no .td-root, no .todos)
	projectDir := filepath.Join(tmp, "project")
	assocTarget := filepath.Join(tmp, "assoc-target")

	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(assocTarget, ".todos"), 0755); err != nil {
		t.Fatal(err)
	}

	// Write association
	configDir := filepath.Join(tmp, ".config", "td")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	assoc := map[string]string{projectDir: assocTarget}
	data, _ := json.Marshal(assoc)
	if err := os.WriteFile(filepath.Join(configDir, associationsFile), data, 0644); err != nil {
		t.Fatal(err)
	}

	result := ResolveBaseDir(projectDir)
	if result != assocTarget {
		t.Errorf("expected association target %s, got %s", assocTarget, result)
	}
}
