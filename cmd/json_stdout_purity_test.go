package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The property under test is not "the error code is right" (that is covered
// elsewhere) but the strictly stronger one that actually broke: when a --json
// command fails, STDOUT contains exactly one JSON value and NOTHING ELSE.
//
// It regressed once already because output.Error printed the human "ERROR:"
// line to stdout, so `td list --json 2>/dev/null` produced a bare text line
// followed by a valid envelope — unparseable as a whole. That failure mode is
// invisible to any test that greps stdout for a substring, or that unmarshals
// only the last line: it only shows up if you decode the WHOLE stream and
// require that it be exhausted.
//
// It has to run the real binary. The envelope is emitted by Execute(), which
// calls os.Exit, so an in-process RunE test never reaches the code path that
// produced the corruption.

var (
	tdBinOnce sync.Once
	tdBinPath string
	tdBinErr  error
)

// buildTD compiles the td binary once per test run and returns its path.
func buildTD(t *testing.T) string {
	t.Helper()
	tdBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "td-jsonpurity")
		if err != nil {
			tdBinErr = err
			return
		}
		bin := filepath.Join(dir, "td")
		// The package under test is ./cmd; the main package is its parent.
		build := exec.Command("go", "build", "-o", bin, ".")
		build.Dir = ".."
		build.Env = append(os.Environ(), "GOWORK=off")
		if out, err := build.CombinedOutput(); err != nil {
			tdBinErr = &buildError{out: string(out), err: err}
			return
		}
		tdBinPath = bin
	})
	if tdBinErr != nil {
		t.Fatalf("building td binary: %v", tdBinErr)
	}
	return tdBinPath
}

type buildError struct {
	out string
	err error
}

func (b *buildError) Error() string { return b.err.Error() + "\n" + b.out }

// runTD runs the td binary in dir and returns stdout and stderr separately.
func runTD(t *testing.T, dir string, args ...string) (stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(buildTD(t), args...)
	cmd.Dir = dir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	// Keep the child off the developer's real config/sync state.
	cmd.Env = append(os.Environ(),
		"TD_FEATURE_SYNC_AUTOSYNC=false",
		"HOME="+dir,
	)
	_ = cmd.Run() // these invocations are expected to exit non-zero
	return outBuf.String(), errBuf.String()
}

// assertSoleJSONValue fails unless s decodes as exactly one JSON value with no
// trailing content. Decoding the whole stream (rather than a line of it) is
// the point: leading or trailing junk is precisely the bug.
func assertSoleJSONValue(t *testing.T, label, s string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("%s: stdout is not parseable JSON: %v\n--- stdout ---\n%s", label, err, s)
	}
	if _, err := dec.Token(); err != io.EOF {
		rest, _ := io.ReadAll(dec.Buffered())
		t.Fatalf("%s: stdout has trailing content after the JSON value (%q)\n--- stdout ---\n%s",
			label, strings.TrimSpace(string(rest)), s)
	}
	return v
}

// TestJSONErrorStdoutIsPureJSON covers the uninitialized-project failure, which
// is the path where every command calls output.Error and then falls through to
// the top-level JSON envelope.
func TestJSONErrorStdoutIsPureJSON(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		args []string
		// wantHumanLine is set for the commands that call output.Error on this
		// path. Not every command does — `show` deliberately suppresses the
		// human line under --json (td-d762a5) — and the purity property below
		// must hold either way, so this is asserted per case rather than
		// globally.
		wantHumanLine bool
	}{
		{args: []string{"list", "--json"}, wantHumanLine: true},
		{args: []string{"next", "--json"}, wantHumanLine: true},
		{args: []string{"reviewable", "--json"}, wantHumanLine: true},
		{args: []string{"tree", "td-nosuch", "--json"}, wantHumanLine: true},
		{args: []string{"show", "td-nosuch", "--json"}},
	}

	for _, tc := range cases {
		label := strings.Join(tc.args, " ")
		t.Run(label, func(t *testing.T) {
			stdout, stderr := runTD(t, dir, tc.args...)

			got := assertSoleJSONValue(t, label, stdout)
			if _, ok := got["error"]; !ok {
				t.Fatalf("%s: expected an error envelope, got %v", label, got)
			}
			// The regression itself: the human line must never be on stdout.
			if strings.Contains(stdout, "ERROR:") {
				t.Errorf("%s: human ERROR line leaked onto stdout:\n%s", label, stdout)
			}
			// ...and where it is emitted, it must still reach the user.
			if tc.wantHumanLine && !strings.Contains(stderr, "ERROR:") {
				t.Errorf("%s: expected the human ERROR line on stderr, got:\n%s", label, stderr)
			}
		})
	}
}

// TestJSONErrorStdoutIsPureJSONInitialized covers the other half: an
// initialized project where the command itself resolves an issue and fails.
// This is the `td tree <missing-id> --json` case from the bug report.
func TestJSONErrorStdoutIsPureJSONInitialized(t *testing.T) {
	dir := t.TempDir()
	stdout, stderr := runTD(t, dir, "init")
	if _, err := os.Stat(filepath.Join(dir, ".todos", "issues.db")); err != nil {
		t.Fatalf("td init did not create the database: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	for _, args := range [][]string{
		{"tree", "td-nosuch", "--json"},
		{"show", "td-nosuch", "--json"},
		{"log", "td-nosuch", "-m", "x", "--json"},
	} {
		label := strings.Join(args, " ")
		t.Run(label, func(t *testing.T) {
			stdout, _ := runTD(t, dir, args...)
			got := assertSoleJSONValue(t, label, stdout)
			if _, ok := got["error"]; !ok {
				t.Fatalf("%s: expected an error envelope, got %v", label, got)
			}
		})
	}
}

// TestDiagnosticsGoToStderrNotStdout is the direct, human-mode statement of
// the same rule: with no --json in play, a failing command still must not put
// its diagnostic on stdout, and stdout must be empty.
func TestDiagnosticsGoToStderrNotStdout(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr := runTD(t, dir, "list")
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("failing `td list` wrote to stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "ERROR:") {
		t.Errorf("failing `td list` did not write the ERROR line to stderr:\n%s", stderr)
	}
}
