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

// TestEpicCreateHasHiddenTypeFlag tests that epicCreateCmd has a hidden type flag
// This is critical for createCmd.RunE to correctly set the type to "epic"
func TestEpicCreateHasHiddenTypeFlag(t *testing.T) {
	// The type flag must exist for cmd.Flags().Set("type", "epic") to work
	typeFlag := epicCreateCmd.Flags().Lookup("type")
	if typeFlag == nil {
		t.Fatal("Expected epicCreateCmd to have --type flag (hidden)")
	}

	// Verify the flag can be set to "epic"
	if err := epicCreateCmd.Flags().Set("type", "epic"); err != nil {
		t.Errorf("Failed to set type flag: %v", err)
	}

	typeValue, err := epicCreateCmd.Flags().GetString("type")
	if err != nil {
		t.Errorf("Failed to get type flag value: %v", err)
	}
	if typeValue != "epic" {
		t.Errorf("Expected type 'epic', got %q", typeValue)
	}

	// Reset
	epicCreateCmd.Flags().Set("type", "") //nolint:errcheck // test setup
}
