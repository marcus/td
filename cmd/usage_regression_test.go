package cmd

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

// registeredSilenceUsage is the SilenceUsage each workflow command carries as
// registered — captured once, before any test can touch it.
//
// It cannot be a package-level initializer: variable initialization runs BEFORE
// the init() functions that register the commands, so it would snapshot the
// zero value and assert nothing. TestMain runs after all init(), which is
// exactly the state a real `td` process starts in. Every command in the test
// binary shares one singleton and several tests mutate SilenceUsage (RunE sets
// it), so reading it at assert time measures the test binary's history rather
// than the registration.
var registeredSilenceUsage map[string]bool

func TestMain(m *testing.M) {
	registeredSilenceUsage = make(map[string]bool)
	for name, cmd := range workflowUsageCommands() {
		registeredSilenceUsage[name] = cmd.SilenceUsage
	}
	os.Exit(m.Run())
}

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
	if len(registeredSilenceUsage) != len(workflowUsageCommands()) {
		t.Fatalf("registration snapshot covers %d of %d commands",
			len(registeredSilenceUsage), len(workflowUsageCommands()))
	}
	for name, cmd := range workflowUsageCommands() {
		if registeredSilenceUsage[name] {
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
