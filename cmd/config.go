package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/spf13/cobra"
)

// validConfigKeys lists the supported config keys for set/get.
var validConfigKeys = []string{
	"sync.url",
	"sync.enabled",
	"sync.auto.enabled",
	"sync.auto.debounce",
	"sync.auto.interval",
	"sync.auto.pull",
	"sync.auto.on_start",
	"sync.snapshot_threshold",
}

func isValidConfigKey(key string) bool {
	for _, k := range validConfigKeys {
		if k == key {
			return true
		}
	}
	return false
}

func parseBool(val string) (bool, error) {
	switch strings.ToLower(val) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool value %q (use true/false/1/0)", val)
	}
}

func boolPtr(b bool) *bool { return &b }
func intPtr(n int) *int    { return &n }

var configCmd = &cobra.Command{
	Use:     "config",
	Short:   "Manage td configuration",
	GroupID: "system",
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, val := args[0], args[1]

		if !isValidConfigKey(key) {
			output.Error("unknown config key: %s", key)
			fmt.Println("Valid keys:", strings.Join(validConfigKeys, ", "))
			return fmt.Errorf("unknown config key: %s", key)
		}

		cfg, err := syncconfig.LoadConfig()
		if err != nil {
			output.Error("load config: %v", err)
			return err
		}

		switch key {
		case "sync.url":
			cfg.Sync.URL = val
		case "sync.enabled":
			b, err := parseBool(val)
			if err != nil {
				output.Error("%v", err)
				return err
			}
			cfg.Sync.Enabled = b
		case "sync.auto.enabled":
			b, err := parseBool(val)
			if err != nil {
				output.Error("%v", err)
				return err
			}
			cfg.Sync.Auto.Enabled = boolPtr(b)
		case "sync.auto.debounce":
			cfg.Sync.Auto.Debounce = val
		case "sync.auto.interval":
			cfg.Sync.Auto.Interval = val
		case "sync.auto.pull":
			b, err := parseBool(val)
			if err != nil {
				output.Error("%v", err)
				return err
			}
			cfg.Sync.Auto.Pull = boolPtr(b)
		case "sync.auto.on_start":
			b, err := parseBool(val)
			if err != nil {
				output.Error("%v", err)
				return err
			}
			cfg.Sync.Auto.OnStart = boolPtr(b)
		case "sync.snapshot_threshold":
			n, err := strconv.Atoi(val)
			if err != nil {
				output.Error("invalid int value %q: %v", val, err)
				return fmt.Errorf("invalid int value %q: %v", val, err)
			}
			cfg.Sync.SnapshotThreshold = intPtr(n)
		}

		if err := syncconfig.SaveConfig(cfg); err != nil {
			output.Error("save config: %v", err)
			return err
		}

		output.Success("set %s = %s", key, val)
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a config value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]

		if !isValidConfigKey(key) {
			output.Error("unknown config key: %s", key)
			fmt.Println("Valid keys:", strings.Join(validConfigKeys, ", "))
			return fmt.Errorf("unknown config key: %s", key)
		}

		cfg, err := syncconfig.LoadConfig()
		if err != nil {
			output.Error("load config: %v", err)
			return err
		}

		var val string
		switch key {
		case "sync.url":
			val = cfg.Sync.URL
		case "sync.enabled":
			val = strconv.FormatBool(cfg.Sync.Enabled)
		case "sync.auto.enabled":
			if cfg.Sync.Auto.Enabled != nil {
				val = strconv.FormatBool(*cfg.Sync.Auto.Enabled)
			} else {
				val = "true (default)"
			}
		case "sync.auto.debounce":
			val = cfg.Sync.Auto.Debounce
			if val == "" {
				val = "3s (default)"
			}
		case "sync.auto.interval":
			val = cfg.Sync.Auto.Interval
			if val == "" {
				val = "5m (default)"
			}
		case "sync.auto.pull":
			if cfg.Sync.Auto.Pull != nil {
				val = strconv.FormatBool(*cfg.Sync.Auto.Pull)
			} else {
				val = "true (default)"
			}
		case "sync.auto.on_start":
			if cfg.Sync.Auto.OnStart != nil {
				val = strconv.FormatBool(*cfg.Sync.Auto.OnStart)
			} else {
				val = "true (default)"
			}
		case "sync.snapshot_threshold":
			if cfg.Sync.SnapshotThreshold != nil {
				val = strconv.Itoa(*cfg.Sync.SnapshotThreshold)
			} else {
				val = "100 (default)"
			}
		}

		fmt.Println(val)
		return nil
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all config values",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := syncconfig.LoadConfig()
		if err != nil {
			output.Error("load config: %v", err)
			return err
		}

		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			output.Error("marshal config: %v", err)
			return err
		}

		fmt.Println(string(data))
		return nil
	},
}

func init() {
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configListCmd)
	AddFeatureGatedCommand(features.SyncCLI.Name, configCmd)
}
