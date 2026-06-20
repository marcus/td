package cmd

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

// TestJSONModeLocalFlag verifies jsonMode reads a locally-defined --json flag.
func TestJSONModeLocalFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().Bool("json", false, "")

	if jsonMode(cmd) {
		t.Fatalf("jsonMode should be false when --json not set")
	}

	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set json: %v", err)
	}
	if !jsonMode(cmd) {
		t.Fatalf("jsonMode should be true when local --json=true")
	}
}

// TestJSONModeInheritedPersistentFlag verifies jsonMode resolves the inherited
// persistent --json flag from a parent command when the child has no local one.
func TestJSONModeInheritedPersistentFlag(t *testing.T) {
	parent := &cobra.Command{Use: "parent"}
	parent.PersistentFlags().Bool("json", false, "")
	child := &cobra.Command{Use: "child"}
	parent.AddCommand(child)

	if jsonMode(child) {
		t.Fatalf("jsonMode should be false before set")
	}
	if err := parent.PersistentFlags().Set("json", "true"); err != nil {
		t.Fatalf("set json: %v", err)
	}
	if !jsonMode(child) {
		t.Fatalf("jsonMode should resolve inherited persistent --json=true")
	}
}

// TestJSONModeMissingFlag verifies jsonMode is robust when no json flag exists.
func TestJSONModeMissingFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	if jsonMode(cmd) {
		t.Fatalf("jsonMode should be false when no json flag exists")
	}
}

// TestJSONModeNil verifies jsonMode handles a nil command.
func TestJSONModeNil(t *testing.T) {
	if jsonMode(nil) {
		t.Fatalf("jsonMode(nil) should be false")
	}
}

// TestJSONErrorRequestedArgsFallback verifies the os.Args fallback used by the
// top-level error path: even if flag parsing failed, a raw --json in args is
// honored so json callers get a JSON error envelope.
func TestJSONErrorRequestedArgsFallback(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"td", "add", "x", "--json", "--bogus"}
	if !jsonErrorRequested() {
		t.Fatalf("jsonErrorRequested should be true when --json present in os.Args")
	}

	os.Args = []string{"td", "add", "x"}
	// Ensure the parsed persistent flag is not stuck true from a prior run.
	_ = rootCmd.PersistentFlags().Set("json", "false")
	if jsonErrorRequested() {
		t.Fatalf("jsonErrorRequested should be false with no --json")
	}
}
