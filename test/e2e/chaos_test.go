package e2e_test

import (
	"encoding/json"
	"flag"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/marcus/td/test/e2e"
)

// CLI flags for chaos test configuration.
var (
	chaosSeed         = flag.Int64("chaos.seed", 0, "PRNG seed (0 = time-based)")
	chaosActions      = flag.Int("chaos.actions", 100, "total actions to perform")
	chaosDuration     = flag.Int("chaos.duration", 0, "seconds to run (overrides actions when >0)")
	chaosActors       = flag.Int("chaos.actors", 2, "number of actors: 2 or 3")
	chaosVerbose      = flag.Bool("chaos.verbose", false, "detailed per-action output")
	chaosSyncMode     = flag.String("chaos.sync-mode", "adaptive", "sync strategy: adaptive, aggressive, random")
	chaosConflictRate = flag.Int("chaos.conflict-rate", 20, "percentage of conflict rounds")
	chaosJSONReport   = flag.String("chaos.json-report", "", "write JSON report to file")
)

// TestSmoke runs a quick 10-action chaos test suitable for CI.
func TestSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	seed := time.Now().UnixNano()
	t.Logf("seed: %d (use -chaos.seed=%d to reproduce)", seed, seed)

	cfg := e2e.DefaultConfig()
	cfg.NumActors = 2
	h := e2e.Setup(t, cfg)

	eng := e2e.NewChaosEngine(h, seed, 2)
	results := eng.RunN(10)

	// Sync for convergence
	if err := h.SyncAll(); err != nil {
		t.Fatalf("final sync: %v", err)
	}

	// Verify convergence
	v := e2e.NewVerifier(h)
	v.VerifyConvergence("alice", "bob")
	v.VerifyIdempotency(2)

	// Log results
	t.Log(eng.Summary())
	t.Log(v.Summary())

	for _, r := range v.FailedResults() {
		t.Errorf("verification failed: %s — %s", r.Name, r.Details)
	}

	// Check for unexpected failures in actions
	unexpected := 0
	for _, r := range results {
		if !r.OK && !r.ExpFail && !r.Skipped {
			unexpected++
			if *chaosVerbose {
				t.Logf("unexpected fail: action=%s actor=%s target=%s output=%s",
					r.Action, r.Actor, r.Target, r.Output)
			}
		}
	}
	if unexpected > 0 {
		t.Errorf("%d unexpected action failures", unexpected)
	}
}

// TestChaosSync is the main randomized chaos test, configurable via flags.
func TestChaosSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	seed := *chaosSeed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	t.Logf("seed: %d (use -chaos.seed=%d to reproduce)", seed, seed)
	t.Logf("config: actions=%d duration=%ds actors=%d sync-mode=%s conflict-rate=%d%%",
		*chaosActions, *chaosDuration, *chaosActors, *chaosSyncMode, *chaosConflictRate)

	cfg := e2e.DefaultConfig()
	cfg.NumActors = *chaosActors
	h := e2e.Setup(t, cfg)

	eng := e2e.NewChaosEngine(h, seed, *chaosActors)
	rng := rand.New(rand.NewSource(seed))

	// Sync batch sizing
	batchMin, batchMax := 3, 10
	switch *chaosSyncMode {
	case "aggressive":
		batchMin, batchMax = 1, 3
	case "random":
		batchMin, batchMax = 1, 20
	}

	// Main loop
	startTime := time.Now()
	actionsDone := 0
	nextSync := batchMin + rng.Intn(batchMax-batchMin+1)
	sinceSync := 0

	isDone := func() bool {
		if *chaosDuration > 0 {
			return time.Since(startTime) >= time.Duration(*chaosDuration)*time.Second
		}
		return actionsDone >= *chaosActions
	}

	for !isDone() {
		// Conflict round?
		if rng.Intn(100) < *chaosConflictRate && len(eng.Issues) > 0 {
			// Both actors do an action on a random shared issue
			eng.RunAction()
			eng.RunAction()
			actionsDone += 2
			sinceSync += 2
		} else {
			r := eng.RunAction()
			actionsDone++
			sinceSync++
			if *chaosVerbose {
				status := "ok"
				if r.ExpFail {
					status = "exp_fail"
				} else if !r.OK && !r.Skipped {
					status = "UNEXP_FAIL"
				} else if r.Skipped {
					status = "skip"
				}
				t.Logf("[%d] %s %s %s -> %s", actionsDone, r.Actor, r.Action, r.Target, status)
			}
		}

		// Sync check
		if sinceSync >= nextSync {
			if err := h.SyncAll(); err != nil {
				t.Logf("sync error (continuing): %v", err)
			}
			eng.Stats.SyncCount++
			sinceSync = 0
			nextSync = batchMin + rng.Intn(batchMax-batchMin+1)
		}

		// Progress every 25 actions
		if actionsDone > 0 && actionsDone%25 == 0 {
			if *chaosDuration > 0 {
				t.Logf("progress: %d actions, %v / %ds", actionsDone, time.Since(startTime).Round(time.Second), *chaosDuration)
			} else {
				t.Logf("progress: %d / %d actions", actionsDone, *chaosActions)
			}
		}
	}

	// Final convergence sync
	t.Log("final convergence sync")
	if err := h.SyncAll(); err != nil {
		t.Fatalf("final sync: %v", err)
	}

	// Run all verifications
	v := e2e.NewVerifier(h)

	actors := []string{"alice", "bob"}
	if *chaosActors >= 3 {
		actors = append(actors, "carol")
	}

	// Pairwise convergence
	for i := 0; i < len(actors); i++ {
		for j := i + 1; j < len(actors); j++ {
			v.VerifyConvergence(actors[i], actors[j])
			v.VerifyActionLogConvergence(actors[i], actors[j])
			v.VerifyEventCounts(actors[i], actors[j])
		}
	}

	// Per-actor checks
	for _, actor := range actors {
		v.VerifyMonotonicSequence(actor)
		v.VerifyCausalOrdering(actor)
	}

	// Cross-cutting checks
	v.VerifyIdempotency(3)
	v.VerifyReadYourWrites(eng)
	v.VerifyFieldLevelMerge("alice", "bob")

	// Report
	elapsed := time.Since(startTime)
	t.Log(eng.Summary())
	t.Log(v.Summary())
	t.Logf("elapsed: %v", elapsed.Round(time.Millisecond))

	// JSON report
	if *chaosJSONReport != "" {
		report := e2e.BuildReport(seed, eng, v, elapsed)
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Errorf("marshal JSON report: %v", err)
		} else if err := os.WriteFile(*chaosJSONReport, data, 0644); err != nil {
			t.Errorf("write JSON report: %v", err)
		} else {
			t.Logf("JSON report written to %s", *chaosJSONReport)
		}
	}

	// Fail test on verification failures
	for _, r := range v.FailedResults() {
		t.Errorf("verification failed: %s — %s", r.Name, r.Details)
	}

	// Fail test on unexpected action failures
	if eng.Stats.UnexpectedFailures > 0 {
		t.Errorf("%d unexpected action failures", eng.Stats.UnexpectedFailures)
	}
}
