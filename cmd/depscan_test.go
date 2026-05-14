package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/marcus/td/internal/depscan"
)

// runDepscan executes the depscan command with the given flags, capturing
// stdout. Flags are reset to their defaults afterward.
func runDepscan(t *testing.T, flags map[string]string) (string, error) {
	t.Helper()
	for k, v := range flags {
		if err := depscanCmd.Flags().Set(k, v); err != nil {
			t.Fatalf("set flag %s=%s: %v", k, v, err)
		}
	}
	defer func() {
		for k := range flags {
			if f := depscanCmd.Flags().Lookup(k); f != nil {
				_ = depscanCmd.Flags().Set(k, f.DefValue)
			}
		}
	}()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := depscanCmd.RunE(depscanCmd, []string{})

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), runErr
}

func TestDepscanFlagsDefined(t *testing.T) {
	for _, name := range []string{"json", "check-updates", "vuln", "severity"} {
		if depscanCmd.Flags().Lookup(name) == nil {
			t.Errorf("expected --%s flag to be defined", name)
		}
	}
	if depscanCmd.GroupID != "system" {
		t.Errorf("depscan should be in the system group, got %q", depscanCmd.GroupID)
	}
}

func TestDepscanRegisteredOnRoot(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		if c.Name() == "depscan" {
			return
		}
	}
	t.Error("depscan command not registered on rootCmd")
}

// TestDepscanDefault runs the command against td's own go.mod (the test
// process runs inside the module, so `go env GOMOD` resolves it).
func TestDepscanDefault(t *testing.T) {
	out, err := runDepscan(t, nil)
	if err != nil {
		t.Fatalf("depscan returned error: %v", err)
	}
	if !strings.Contains(out, "Dependency Risk Scan:") {
		t.Errorf("expected report header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "github.com/marcus/td") {
		t.Errorf("expected td module path in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Dependencies:") {
		t.Errorf("expected dependency summary line, got:\n%s", out)
	}
}

func TestDepscanJSON(t *testing.T) {
	out, err := runDepscan(t, map[string]string{"json": "true"})
	if err != nil {
		t.Fatalf("depscan --json returned error: %v", err)
	}
	var report depscan.Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("depscan --json did not emit valid JSON: %v\noutput:\n%s", err, out)
	}
	if report.Module != "github.com/marcus/td" {
		t.Errorf("unexpected module in JSON report: %q", report.Module)
	}
	if report.Summary.Total != len(report.Findings) {
		t.Errorf("summary total %d != findings length %d", report.Summary.Total, len(report.Findings))
	}
	if report.Summary.DirectDeps == 0 {
		t.Error("expected at least one direct dependency in JSON report")
	}
	// Findings must be severity-ordered.
	for i := 1; i < len(report.Findings); i++ {
		prev, cur := report.Findings[i-1].Severity, report.Findings[i].Severity
		if rankOf(prev) < rankOf(cur) {
			t.Errorf("findings not severity-ordered at %d: %s before %s", i, prev, cur)
		}
	}
}

func TestDepscanSeverityFilter(t *testing.T) {
	out, err := runDepscan(t, map[string]string{"json": "true", "severity": "high"})
	if err != nil {
		t.Fatalf("depscan --severity high returned error: %v", err)
	}
	var report depscan.Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, f := range report.Findings {
		if f.Severity != depscan.SeverityHigh {
			t.Errorf("severity=high filter leaked a %s finding: %+v", f.Severity, f)
		}
	}
}

func TestDepscanInvalidSeverity(t *testing.T) {
	_, err := runDepscan(t, map[string]string{"severity": "bogus"})
	if err == nil {
		t.Error("expected error for invalid --severity value")
	}
}

func rankOf(s depscan.Severity) int {
	switch s {
	case depscan.SeverityHigh:
		return 3
	case depscan.SeverityMedium:
		return 2
	default:
		return 1
	}
}
