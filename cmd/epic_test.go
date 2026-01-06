package cmd

import "testing"

// TestEpicCmdExists tests that epic command exists
func TestEpicCmdExists(t *testing.T) {
	if epicCmd == nil {
		t.Error("Expected epicCmd to be defined")
	}
}

// TestEpicCreateSubcommandExists tests that epic create subcommand exists
func TestEpicCreateSubcommandExists(t *testing.T) {
	if epicCreateCmd == nil {
		t.Error("Expected epicCreateCmd to be defined")
	}

	// Verify it's registered as a subcommand
	found := false
	for _, sub := range epicCmd.Commands() {
		if sub.Name() == "create" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'create' to be a subcommand of epic")
	}
}

// TestEpicListSubcommandExists tests that epic list subcommand exists
func TestEpicListSubcommandExists(t *testing.T) {
	if epicListCmd == nil {
		t.Error("Expected epicListCmd to be defined")
	}

	// Verify it's registered as a subcommand
	found := false
	for _, sub := range epicCmd.Commands() {
		if sub.Name() == "list" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'list' to be a subcommand of epic")
	}
}

// TestEpicCreateRequiresTitle tests that epic create requires a title argument
func TestEpicCreateRequiresTitle(t *testing.T) {
	args := epicCreateCmd.Args
	if args == nil {
		t.Fatal("Expected Args validator to be set")
	}

	// Test with 1 arg (should be valid)
	if err := args(epicCreateCmd, []string{"Test Epic"}); err != nil {
		t.Errorf("Expected 1 arg to be valid: %v", err)
	}

	// Test with 0 args (should fail)
	if err := args(epicCreateCmd, []string{}); err == nil {
		t.Error("Expected 0 args to fail")
	}
}
