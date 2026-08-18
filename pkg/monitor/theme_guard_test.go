package monitor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/td/internal/themecheck"
)

func TestProductionMonitorHasNoFrozenOrRawThemeState(t *testing.T) {
	root := moduleRoot(t)
	findings, err := themecheck.ScanMonitor(root)
	if err != nil {
		t.Fatalf("scan monitor theme state: %v", err)
	}
	if len(findings) == 0 {
		return
	}

	var detail strings.Builder
	for _, finding := range findings {
		detail.WriteString("\n  " + finding.String())
	}
	t.Fatalf("production monitor theme guard found %d violation(s):%s\n\n"+
		"Build styles from the model Theme at render/construction time. Raw colors belong only in the documented default/derivation allowlist.",
		len(findings), detail.String())
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
