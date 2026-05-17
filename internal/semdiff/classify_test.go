package semdiff

import (
	"strings"
	"testing"
)

func parseAndClassify(t *testing.T, diff string) Summary {
	t.Helper()
	files, err := Parse(strings.NewReader(diff))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Classify(files)
}

func hasCategory(s Summary, path string, cat Category) bool {
	for _, f := range s.Files {
		if f.Path != path {
			continue
		}
		for _, c := range f.Changes {
			if c.Category == cat {
				return true
			}
		}
	}
	return false
}

func TestCommentOnlyChange(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,3 @@
 package foo
-// old comment
+// new comment
`
	s := parseAndClassify(t, diff)
	if !hasCategory(s, "foo.go", CatCommentOnly) {
		t.Fatalf("expected comment-only, got %+v", s)
	}
}

func TestFunctionAdded(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,6 @@
 package foo
+
+func Greet(name string) string {
+    return "hi " + name
+}
`
	s := parseAndClassify(t, diff)
	if !hasCategory(s, "foo.go", CatFuncAdded) {
		t.Fatalf("expected function-added, got %+v", s)
	}
}

func TestSignatureChange(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,4 +1,4 @@
 package foo
-func Greet(name string) string {
+func Greet(name, salutation string) string {
     return "hi " + name
 }
`
	s := parseAndClassify(t, diff)
	if !hasCategory(s, "foo.go", CatSignatureChange) {
		t.Fatalf("expected signature-change, got %+v", s)
	}
}

func TestImportOnlyChange(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,5 +1,6 @@
 package foo

 import (
+    "fmt"
     "strings"
 )
`
	s := parseAndClassify(t, diff)
	if !hasCategory(s, "foo.go", CatImport) {
		t.Fatalf("expected import-change, got %+v", s)
	}
}

func TestTestOnlyChange(t *testing.T) {
	diff := `diff --git a/foo_test.go b/foo_test.go
--- a/foo_test.go
+++ b/foo_test.go
@@ -1,4 +1,5 @@
 package foo

 func TestThing(t *testing.T) {
+    t.Log("extra")
 }
`
	s := parseAndClassify(t, diff)
	if !hasCategory(s, "foo_test.go", CatTestChange) {
		t.Fatalf("expected test-change, got %+v", s)
	}
}

func TestDependencyChange(t *testing.T) {
	diff := `diff --git a/go.mod b/go.mod
--- a/go.mod
+++ b/go.mod
@@ -3,2 +3,3 @@
 require (
+    example.com/new v1.0.0
 )
`
	s := parseAndClassify(t, diff)
	if !hasCategory(s, "go.mod", CatDependency) {
		t.Fatalf("expected dependency, got %+v", s)
	}
}

func TestNewFile(t *testing.T) {
	diff := `diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,2 @@
+package foo
+
`
	s := parseAndClassify(t, diff)
	if !hasCategory(s, "new.go", CatFileAdded) {
		t.Fatalf("expected file-added, got %+v", s)
	}
}

func TestDeletedFile(t *testing.T) {
	diff := `diff --git a/old.go b/old.go
deleted file mode 100644
--- a/old.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package foo
-
`
	s := parseAndClassify(t, diff)
	if !hasCategory(s, "old.go", CatFileRemoved) {
		t.Fatalf("expected file-removed, got %+v", s)
	}
}

func TestMixedChange(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,10 +1,12 @@
 package foo

-func Old() {}
+func New() {
+    if true {
+        return
+    }
+}
`
	s := parseAndClassify(t, diff)
	if !hasCategory(s, "foo.go", CatFuncAdded) {
		t.Fatalf("expected function-added in mixed diff, got %+v", s)
	}
	if !hasCategory(s, "foo.go", CatFuncRemoved) {
		t.Fatalf("expected function-removed in mixed diff, got %+v", s)
	}
}

func TestFormattingOnly(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,2 +1,2 @@
-x := 1
+x  :=  1
`
	s := parseAndClassify(t, diff)
	if !hasCategory(s, "foo.go", CatFormatting) {
		t.Fatalf("expected formatting-only, got %+v", s)
	}
}

func TestHeadlineCounts(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
new file mode 100644
--- /dev/null
+++ b/a.go
@@ -0,0 +1,1 @@
+package a
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1,1 +1,2 @@
 package b
+// added
`
	s := parseAndClassify(t, diff)
	if s.Headline == "" || s.Headline == "No changes detected." {
		t.Fatalf("expected non-empty headline, got %q", s.Headline)
	}
	if !strings.Contains(s.Headline, "new file") {
		t.Errorf("expected new-file count, got %q", s.Headline)
	}
	if !strings.Contains(s.Headline, "modified file") {
		t.Errorf("expected modified-file count, got %q", s.Headline)
	}
}

func TestEmptyDiff(t *testing.T) {
	s := parseAndClassify(t, "")
	if s.Headline != "No changes detected." {
		t.Fatalf("expected no-changes headline, got %q", s.Headline)
	}
}
