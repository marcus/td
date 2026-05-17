package cmd

import (
	"bytes"
	"strings"
	"testing"
)

const fixtureDiff = `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,5 +1,7 @@
 package foo

-func Greet(name string) string {
+func Greet(name, salutation string) string {
     return "hi " + name
 }
+
+// new comment line
`

func TestExplainStdinText(t *testing.T) {
	// Reset flags between tests
	explainJSON = false
	explainStaged = false
	explainStdin = true
	t.Cleanup(func() {
		explainJSON = false
		explainStaged = false
		explainStdin = false
	})

	buf := &bytes.Buffer{}
	cmd := explainCmd
	cmd.SetOut(buf)

	// Re-route stdin
	origStdin := stdinReader
	stdinReader = strings.NewReader(fixtureDiff)
	t.Cleanup(func() { stdinReader = origStdin })

	if err := runExplainWithReader(cmd, nil, stdinReader); err != nil {
		t.Fatalf("explain failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "foo.go") {
		t.Fatalf("expected output to mention foo.go, got %q", out)
	}
	if !strings.Contains(out, "signature-change") {
		t.Fatalf("expected signature-change category, got %q", out)
	}
}

func TestExplainStdinJSON(t *testing.T) {
	explainJSON = true
	explainStdin = true
	t.Cleanup(func() {
		explainJSON = false
		explainStdin = false
	})

	buf := &bytes.Buffer{}
	cmd := explainCmd
	cmd.SetOut(buf)

	if err := runExplainWithReader(cmd, nil, strings.NewReader(fixtureDiff)); err != nil {
		t.Fatalf("explain failed: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("expected JSON output, got %q", out)
	}
	if !strings.Contains(out, "\"signature-change\"") {
		t.Fatalf("expected signature-change in JSON, got %q", out)
	}
}
