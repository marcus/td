package cmd

import "github.com/spf13/cobra"

// jsonMode reports whether --json was requested, checking the command's own
// flag first (for commands that still define a local --json) then the
// inherited persistent flag. This avoids cobra's local-shadows-persistent
// gotcha during the migration window: cmd.Flags() resolves both a locally
// registered flag and the inherited persistent flag, preferring the local one.
//
// It is intentionally robust: if the "json" flag does not exist on the command
// (e.g. in a test that builds a bare command), GetBool returns an error and we
// treat that as "not json mode".
func jsonMode(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	// Trigger cobra's lazy merge of inherited persistent flags into the
	// command's own flag set. cmd.Flags() alone does NOT merge parent
	// persistent flags; InheritedFlags() calls mergePersistentFlags(), which
	// adds them into cmd.Flags(). At normal runtime parsing has already merged
	// them, but calling this makes jsonMode correct even when invoked outside
	// the parse path (and in tests that build commands directly).
	_ = cmd.InheritedFlags()
	v, err := cmd.Flags().GetBool("json")
	if err != nil {
		return false
	}
	return v
}
