package e2e_test

import (
	"strings"
	"testing"

	"github.com/marcus/td/test/e2e"
)

func TestHarnessBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := e2e.DefaultConfig()
	h := e2e.Setup(t, cfg)

	// Alice creates an issue
	out, err := h.TdA("create", "test issue from alice")
	if err != nil {
		t.Fatalf("alice create: %v\n%s", err, out)
	}
	t.Logf("alice create: %s", strings.TrimSpace(out))

	// Alice lists issues
	out, err = h.TdA("list")
	if err != nil {
		t.Fatalf("alice list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "test issue from alice") {
		t.Fatalf("alice list missing issue: %s", out)
	}

	// Sync all actors
	if err := h.SyncAll(); err != nil {
		t.Fatalf("sync all: %v", err)
	}

	// Bob should see the issue
	out, err = h.TdB("list")
	if err != nil {
		t.Fatalf("bob list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "test issue from alice") {
		t.Fatalf("bob list missing synced issue: %s", out)
	}

	// Verify DB paths exist
	aliceDB := h.DBPath("alice")
	if aliceDB == "" {
		t.Fatal("alice DB path is empty")
	}
	bobDB := h.DBPath("bob")
	if bobDB == "" {
		t.Fatal("bob DB path is empty")
	}
	t.Logf("alice db: %s", aliceDB)
	t.Logf("bob db: %s", bobDB)
}
