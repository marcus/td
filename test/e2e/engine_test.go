package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcus/td/test/e2e"
)

func TestEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	cfg.NumActors = 2
	h := e2e.Setup(t, cfg)

	engine := e2e.NewChaosEngine(h, 42, 2)

	// Seed some initial issues so non-create actions have targets
	for i := 0; i < 5; i++ {
		r := engine.RunAction()
		// Force creates at the start by re-running if we get a non-create
		if r.Action != "create" || !r.OK {
			actor := "alice"
			if i%2 == 1 {
				actor = "bob"
			}
			out, err := h.Td(actor, "create", fmt.Sprintf("Seed issue %d for chaos testing", i),
				"--type", "task", "--priority", "P1")
			if err == nil {
				id := e2e.ExtractIssueID(out)
				if id != "" {
					engine.TrackCreatedIssue(id, "open", actor)
				}
			}
		}
	}

	// Run 20 random actions
	results := engine.RunN(20)

	for i, r := range results {
		status := "ok"
		if r.Skipped {
			status = "skip"
		} else if r.ExpFail {
			status = "expfail"
		} else if !r.OK {
			status = "FAIL"
			// Log output for unexpected failures
			outSnippet := strings.ReplaceAll(r.Output, "\n", "\\n")
			if len(outSnippet) > 120 {
				outSnippet = outSnippet[:120] + "..."
			}
			t.Logf("action %2d: %-20s actor=%-6s target=%-12s %s out=%q",
				i+1, r.Action, r.Actor, r.Target, status, outSnippet)
			continue
		}
		t.Logf("action %2d: %-20s actor=%-6s target=%-12s %s",
			i+1, r.Action, r.Actor, r.Target, status)
	}

	t.Logf("\n%s", engine.Summary())

	// Verify no unexpected failures
	if engine.Stats.UnexpectedFailures > 0 {
		t.Errorf("got %d unexpected failures", engine.Stats.UnexpectedFailures)
		for name, pa := range engine.Stats.PerAction {
			if pa.UnexpFail > 0 {
				t.Errorf("  %s: %d unexpected failures", name, pa.UnexpFail)
			}
		}
	}
}

func TestEngineWeightedSelection(t *testing.T) {
	// Verify weight distribution is reasonable (no panics, all actions reachable)
	counts := make(map[string]int)
	rng := e2e.NewChaosEngine(nil, 123, 2).Rng
	for i := 0; i < 10000; i++ {
		def := e2e.SelectAction(rng)
		counts[def.Name]++
	}

	// "create" should be the most frequent (weight 15)
	if counts["create"] < 500 {
		t.Errorf("create selected only %d/10000 times (expected ~1200)", counts["create"])
	}

	// Low-weight actions should still appear
	if counts["restore"] == 0 {
		t.Error("restore never selected in 10000 rolls")
	}
	if counts["log_result"] == 0 {
		t.Error("log_result never selected in 10000 rolls")
	}

	t.Logf("weight distribution (10000 samples): create=%d update=%d comment=%d restore=%d",
		counts["create"], counts["update"], counts["comment"], counts["restore"])
}
