package e2e_test

import (
	"math/rand"
	"testing"

	"github.com/marcus/td/test/e2e"
)

func reportResults(t *testing.T, results []e2e.VerifyResult) {
	t.Helper()
	passed, failed := 0, 0
	for _, r := range results {
		if r.Passed {
			passed++
			t.Logf("  PASS: %s", r.Name)
		} else {
			failed++
			t.Errorf("  FAIL: %s — %s", r.Name, r.Details)
		}
	}
	t.Logf("results: %d passed, %d failed", passed, failed)
}

func TestPartitionRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	rng := rand.New(rand.NewSource(42))
	results := e2e.ScenarioPartitionRecovery(h, rng)
	reportResults(t, results)
}

func TestUndoSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	results := e2e.ScenarioUndoSync(h)
	reportResults(t, results)
}

func TestMultiFieldCollision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	results := e2e.ScenarioMultiFieldCollision(h)
	reportResults(t, results)
}

func TestRapidCreateDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	results := e2e.ScenarioRapidCreateDelete(h)
	reportResults(t, results)
}

func TestCascadeConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	results := e2e.ScenarioCascadeConflict(h)
	reportResults(t, results)
}

func TestDependencyCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	results := e2e.ScenarioDependencyCycle(h)
	reportResults(t, results)
}

func TestThunderingHerd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	results := e2e.ScenarioThunderingHerd(h)
	reportResults(t, results)
}

func TestBurstNoSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	rng := rand.New(rand.NewSource(99))
	results := e2e.ScenarioBurstNoSync(h, rng)
	reportResults(t, results)
}
