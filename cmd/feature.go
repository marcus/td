package cmd

import (
	"fmt"
	"strings"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var featureCmd = &cobra.Command{
	Use:     "feature",
	Short:   "Manage experimental feature flags",
	Long: `Manage experimental feature flags.

List, get, set, and unset feature flags that gate experimental functionality
such as sync CLI commands and auto-sync. Flags can be set per-project or via
environment variables. Use 'td feature list' to see all flags and their
resolved state.`,
	GroupID: "system",
}

var featureListCmd = &cobra.Command{
	Use:   "list",
	Short: "List known feature flags and their resolved state",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		fmt.Printf("%-20s  %-5s  %-7s  %s\n", "NAME", "STATE", "SOURCE", "DESCRIPTION")
		for _, feature := range features.ListAll() {
			enabled, source := features.Resolve(baseDir, feature.Name)
			state := "off"
			if enabled {
				state = "on"
			}

			fmt.Printf("%-20s  %-5s  %-7s  %s\n", feature.Name, state, source, feature.Description)
		}

		return nil
	},
}

var featureGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Get a feature flag state",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := normalizeFeatureArg(args[0])
		if !features.IsKnownFeature(name) {
			return unknownFeatureError(name)
		}

		enabled, source := features.Resolve(getBaseDir(), name)
		fmt.Printf("%s=%t (source=%s)\n", name, enabled, source)
		return nil
	},
}

var featureSetCmd = &cobra.Command{
	Use:   "set <name> <true|false>",
	Short: "Set a feature flag in local project config",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := normalizeFeatureArg(args[0])
		if !features.IsKnownFeature(name) {
			return unknownFeatureError(name)
		}

		enabled, err := parseBoolString(args[1])
		if err != nil {
			output.Error("%v", err)
			return err
		}

		if err := config.SetFeatureFlag(getBaseDir(), name, enabled); err != nil {
			output.Error("set feature flag: %v", err)
			return err
		}

		output.Success("feature %s set to %t", name, enabled)
		return nil
	},
}

var featureUnsetCmd = &cobra.Command{
	Use:   "unset <name>",
	Short: "Remove a local feature flag override",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := normalizeFeatureArg(args[0])
		if !features.IsKnownFeature(name) {
			return unknownFeatureError(name)
		}

		if err := config.UnsetFeatureFlag(getBaseDir(), name); err != nil {
			output.Error("unset feature flag: %v", err)
			return err
		}

		output.Success("feature %s unset", name)
		return nil
	},
}

func normalizeFeatureArg(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func parseBoolString(raw string) (bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "1", "true", "on", "yes":
		return true, nil
	case "0", "false", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool value %q", raw)
	}
}

func unknownFeatureError(name string) error {
	var names []string
	for _, feature := range features.ListAll() {
		names = append(names, feature.Name)
	}
	return fmt.Errorf("unknown feature %q (known: %s)", name, strings.Join(names, ", "))
}

func init() {
	featureCmd.AddCommand(featureListCmd)
	featureCmd.AddCommand(featureGetCmd)
	featureCmd.AddCommand(featureSetCmd)
	featureCmd.AddCommand(featureUnsetCmd)

	featureSetCmd.Example = "  td feature set sync_cli true\n  td feature set sync_autosync false"

	rootCmd.AddCommand(featureCmd)
}
