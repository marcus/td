package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestChangelogCmdRequiresVersion(t *testing.T) {
	cmd := &cobra.Command{Use: "root"}
	cmd.AddGroup(&cobra.Group{ID: "system", Title: "System Commands:"})
	cmd.AddCommand(changelogCmd)

	cmd.SetArgs([]string{"changelog"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --version is missing")
	}
}

func TestChangelogCmdFlags(t *testing.T) {
	flags := []string{"from", "to", "version", "prepend"}
	for _, name := range flags {
		f := changelogCmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("expected flag --%s to be registered", name)
		}
	}

	if v := changelogCmd.Flags().Lookup("to").DefValue; v != "HEAD" {
		t.Errorf("--to default = %q, want HEAD", v)
	}
	if v := changelogCmd.Flags().Lookup("prepend").DefValue; v != "false" {
		t.Errorf("--prepend default = %q, want false", v)
	}
}
