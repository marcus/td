package e2e_test

import (
	"testing"

	"github.com/marcus/td/test/e2e"
)

func TestServerRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	cfg.NumActors = 2
	h := e2e.Setup(t, cfg)

	results := e2e.ScenarioServerRestart(h)

	passed, failed := 0, 0
	for _, r := range results {
		if r.Passed {
			passed++
			t.Logf("PASS: %-30s %s", r.Name, r.Details)
		} else {
			failed++
			t.Errorf("FAIL: %-30s %s", r.Name, r.Details)
		}
	}

	t.Logf("results: %d passed, %d failed", passed, failed)
	if failed > 0 {
		t.Logf("server log:\n%s", h.ServerLogContents())
	}
}
