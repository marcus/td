package cmd

import (
	"testing"
)

// TestShowFormatFlagParsing tests that --format flag is defined and works
func TestShowFormatFlagParsing(t *testing.T) {
	// Test that --format flag exists
	if showCmd.Flags().Lookup("format") == nil {
		t.Error("Expected --format flag to be defined")
	}

	// Test that -f shorthand exists
	if showCmd.Flags().ShorthandLookup("f") == nil {
		t.Error("Expected -f shorthand to be defined for --format")
	}

	// Test that --format flag can be set
	if err := showCmd.Flags().Set("format", "json"); err != nil {
		t.Errorf("Failed to set --format flag: %v", err)
	}

	formatValue, err := showCmd.Flags().GetString("format")
	if err != nil {
		t.Errorf("Failed to get --format flag value: %v", err)
	}
	if formatValue != "json" {
		t.Errorf("Expected format value 'json', got %s", formatValue)
	}

	// Reset
	showCmd.Flags().Set("format", "")
}

// TestShowAcceptsZeroArgs tests that show can be called with no arguments
func TestShowAcceptsZeroArgs(t *testing.T) {
	// Test that show command accepts 0 arguments
	args := showCmd.Args
	if args == nil {
		t.Fatal("Expected Args validator to be set")
	}

	// Test with 0 args (should be valid - will try to find current work)
	if err := args(showCmd, []string{}); err != nil {
		t.Errorf("Expected 0 args to be valid: %v", err)
	}

	// Test with 1 arg (should be valid)
	if err := args(showCmd, []string{"td-test123"}); err != nil {
		t.Errorf("Expected 1 arg to be valid: %v", err)
	}
}

// TestShowJSONFlagStillWorks tests that --json flag is still available
func TestShowJSONFlagStillWorks(t *testing.T) {
	// Test that --json flag exists
	if showCmd.Flags().Lookup("json") == nil {
		t.Error("Expected --json flag to still be defined")
	}

	// Test that --json flag can be set
	if err := showCmd.Flags().Set("json", "true"); err != nil {
		t.Errorf("Failed to set --json flag: %v", err)
	}

	jsonValue, err := showCmd.Flags().GetBool("json")
	if err != nil {
		t.Errorf("Failed to get --json flag value: %v", err)
	}
	if !jsonValue {
		t.Error("Expected json flag to be true")
	}

	// Reset
	showCmd.Flags().Set("json", "false")
}

// TestShowRenderMarkdownFlagExists tests that --render-markdown flag is defined
func TestShowRenderMarkdownFlagExists(t *testing.T) {
	if showCmd.Flags().Lookup("render-markdown") == nil {
		t.Error("Expected --render-markdown flag to be defined")
	}
	if showCmd.Flags().ShorthandLookup("m") == nil {
		t.Error("Expected -m shorthand to be defined for --render-markdown")
	}
}
