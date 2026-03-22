package diff

import (
	"testing"
)

func TestParse_Empty(t *testing.T) {
	result := Parse("")
	if len(result) != 0 {
		t.Fatalf("expected 0 diffs, got %d", len(result))
	}
}

func TestParse_AddedFile(t *testing.T) {
	input := `diff --git a/new.go b/new.go
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package main
+
+func Hello() {}
`
	diffs := Parse(input)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	d := diffs[0]
	if d.Status != "added" {
		t.Errorf("expected status added, got %s", d.Status)
	}
	if d.NewPath != "new.go" {
		t.Errorf("expected new path new.go, got %s", d.NewPath)
	}
	if len(d.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(d.Hunks))
	}
	h := d.Hunks[0]
	if h.NewStart != 1 || h.NewCount != 3 {
		t.Errorf("expected new range 1,3 got %d,%d", h.NewStart, h.NewCount)
	}
	addedCount := 0
	for _, l := range h.Lines {
		if l.Kind == LineAdded {
			addedCount++
		}
	}
	if addedCount != 3 {
		t.Errorf("expected 3 added lines, got %d", addedCount)
	}
}

func TestParse_DeletedFile(t *testing.T) {
	input := `diff --git a/old.go b/old.go
deleted file mode 100644
index abc1234..0000000
--- a/old.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package old
-func Gone() {}
`
	diffs := Parse(input)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Status != "deleted" {
		t.Errorf("expected status deleted, got %s", diffs[0].Status)
	}
	removedCount := 0
	for _, l := range diffs[0].Hunks[0].Lines {
		if l.Kind == LineRemoved {
			removedCount++
		}
	}
	if removedCount != 2 {
		t.Errorf("expected 2 removed lines, got %d", removedCount)
	}
}

func TestParse_ModifiedFile(t *testing.T) {
	input := `diff --git a/main.go b/main.go
index abc1234..def5678 100644
--- a/main.go
+++ b/main.go
@@ -1,4 +1,4 @@
 package main

-func old() {}
+func new() {}

`
	diffs := Parse(input)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Status != "modified" {
		t.Errorf("expected status modified, got %s", diffs[0].Status)
	}
	if diffs[0].OldPath != "main.go" || diffs[0].NewPath != "main.go" {
		t.Errorf("unexpected paths: old=%s new=%s", diffs[0].OldPath, diffs[0].NewPath)
	}
}

func TestParse_RenamedFile(t *testing.T) {
	input := `diff --git a/old.go b/new.go
similarity index 95%
rename from old.go
rename to new.go
index abc1234..def5678 100644
--- a/old.go
+++ b/new.go
@@ -1,3 +1,3 @@
 package main

-func Old() {}
+func New() {}
`
	diffs := Parse(input)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	d := diffs[0]
	if d.Status != "renamed" {
		t.Errorf("expected status renamed, got %s", d.Status)
	}
	if d.OldPath != "old.go" {
		t.Errorf("expected old path old.go, got %s", d.OldPath)
	}
	if d.NewPath != "new.go" {
		t.Errorf("expected new path new.go, got %s", d.NewPath)
	}
}

func TestParse_MultipleFiles(t *testing.T) {
	input := `diff --git a/a.go b/a.go
index abc..def 100644
--- a/a.go
+++ b/a.go
@@ -1,2 +1,2 @@
 package a
-var x = 1
+var x = 2
diff --git a/b.go b/b.go
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/b.go
@@ -0,0 +1 @@
+package b
`
	diffs := Parse(input)
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d", len(diffs))
	}
	if diffs[0].Status != "modified" {
		t.Errorf("first file expected modified, got %s", diffs[0].Status)
	}
	if diffs[1].Status != "added" {
		t.Errorf("second file expected added, got %s", diffs[1].Status)
	}
}

func TestParse_HunkRangeNoCounts(t *testing.T) {
	// When count is omitted it defaults to 1
	input := `diff --git a/f.go b/f.go
index abc..def 100644
--- a/f.go
+++ b/f.go
@@ -1 +1 @@
-old
+new
`
	diffs := Parse(input)
	if len(diffs) != 1 || len(diffs[0].Hunks) != 1 {
		t.Fatalf("expected 1 diff with 1 hunk")
	}
	h := diffs[0].Hunks[0]
	if h.OldStart != 1 || h.OldCount != 1 || h.NewStart != 1 || h.NewCount != 1 {
		t.Errorf("expected range 1,1 1,1 got %d,%d %d,%d",
			h.OldStart, h.OldCount, h.NewStart, h.NewCount)
	}
}
