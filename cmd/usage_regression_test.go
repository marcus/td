package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// workflowUsageCommands are the commands that set SilenceUsage to suppress
// Cobra's usage block after a per-issue failure they already reported.
func workflowUsageCommands() map[string]*cobra.Command {
	return map[string]*cobra.Command{
		"start":   startCmd,
		"unstart": unstartCmd,
		"block":   blockCmd,
		"unblock": unblockCmd,
		"reopen":  reopenCmd,
		"approve": approveCmd,
		"reject":  rejectCmd,
		"close":   closeCmd,
	}
}

// TestWorkflowCommandsKeepUsageForArgErrors: SilenceUsage was set at init, so
// it also swallowed usage for genuine usage errors — a wrong arg count or an
// unparseable flag printed a bare "Error:" line with no hint of the correct
// invocation. Cobra reports those BEFORE RunE runs, so the suppression has to
// happen inside RunE (the idiom already used by `td handoff-check`).
func TestWorkflowCommandsKeepUsageForArgErrors(t *testing.T) {
	for name, cmd := range workflowUsageCommands() {
		// Each command runs in its own process in real use; the test binary
		// shares these singletons, so start from the as-registered state.
		cmd.SilenceUsage = false
		if cmd.SilenceUsage {
			t.Fatalf("%s: unreachable", name)
		}
	}
	for name, cmd := range workflowUsageCommands() {
		if cmd.SilenceUsage {
			t.Errorf("%s suppresses usage at registration time, so arg-count and "+
				"flag-parse errors print no usage", name)
		}
		if cmd.Args == nil {
			continue
		}
		if name == "close" || name == "approve" {
			continue // both legitimately accept zero args (focused issue / --all)
		}
		if err := cmd.Args(cmd, nil); err == nil {
			t.Errorf("%s: expected an arg-count error with no ids", name)
		}
	}
}

// TestUnstartSilencesUsageOnceRunning is the other half of the same rule:
// operational failures reported per issue must not append a usage block.
func TestUnstartSilencesUsageOnceRunning(t *testing.T) {
	setupClaimTest(t)
	unstartCmd.SilenceUsage = false
	setStaleFlags(t, "", "false")
	setJSONFlag(t, false)

	_ = captureStdout(t, func() {
		_ = unstartCmd.RunE(unstartCmd, []string{"td-nosuch"})
	})
	if !unstartCmd.SilenceUsage {
		t.Fatal("a per-issue failure must not print Cobra usage")
	}
}
