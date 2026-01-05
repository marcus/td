package cmd

import (
	"testing"
)

// TestUnstartStopAlias tests that 'stop' is an alias for 'unstart'
func TestUnstartStopAlias(t *testing.T) {
	// Test that unstart command has 'stop' alias
	aliases := unstartCmd.Aliases
	found := false
	for _, alias := range aliases {
		if alias == "stop" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'stop' to be an alias for unstart command, got aliases: %v", aliases)
	}
}

// TestUnstartReasonFlag tests that --reason flag exists
func TestUnstartReasonFlag(t *testing.T) {
	if unstartCmd.Flags().Lookup("reason") == nil {
		t.Error("Expected --reason flag to be defined on unstart command")
	}

	// Test that --reason flag can be set
	if err := unstartCmd.Flags().Set("reason", "test reason"); err != nil {
		t.Errorf("Failed to set --reason flag: %v", err)
	}

	reasonValue, err := unstartCmd.Flags().GetString("reason")
	if err != nil {
		t.Errorf("Failed to get --reason flag value: %v", err)
	}
	if reasonValue != "test reason" {
		t.Errorf("Expected reason value 'test reason', got %s", reasonValue)
	}

	// Reset
	unstartCmd.Flags().Set("reason", "")
}
