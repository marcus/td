package e2e

import (
	"fmt"
	"strings"
	"testing"
)

// TestRateLimitEnforced validates the server enforces rate limits.
// Uses very low limits (overriding the high test defaults) to trigger 429 quickly.
func TestRateLimitEnforced(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	cfg := DefaultConfig()
	h := Setup(t, cfg)

	// Stop the server started with high limits and restart with very low limits
	if err := h.StopServer(); err != nil {
		t.Fatalf("stop server: %v", err)
	}

	// Override rate limits to very low values for this test
	h.SetServerEnv(
		"SYNC_RATE_LIMIT_PUSH=2",
		"SYNC_RATE_LIMIT_PULL=2",
		"SYNC_RATE_LIMIT_OTHER=5",
	)

	if err := h.StartServer(); err != nil {
		t.Fatalf("restart server with low limits: %v", err)
	}

	// First sync should work
	out, err := h.Td("alice", "sync")
	if err != nil {
		t.Logf("first sync output: %s err: %v", out, err)
	}

	// Rapid syncs should eventually trigger 429
	got429 := false
	for i := range 20 {
		out, err := h.Td("alice", "sync")
		combined := fmt.Sprintf("%s %v", out, err)
		if strings.Contains(combined, "429") || strings.Contains(strings.ToLower(combined), "rate") {
			t.Logf("got rate limit on attempt %d", i+1)
			got429 = true
			break
		}
	}

	if !got429 {
		t.Error("expected rate limit (429) but never triggered")
	}
}
