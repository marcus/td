package cmd

import (
	"github.com/marcus/td/internal/features"
	"github.com/spf13/cobra"
)

// SyncFeatureHooks exposes optional sync hooks that can be registered by
// sync-specific command files. These hooks are gated by feature flags.
type SyncFeatureHooks struct {
	OnStartup         func(commandName string)
	OnAfterMutation   func()
	IsMutatingCommand func(commandName string) bool
}

var syncFeatureHooks SyncFeatureHooks

// RegisterSyncFeatureHooks registers sync-related hooks.
func RegisterSyncFeatureHooks(hooks SyncFeatureHooks) {
	syncFeatureHooks = hooks
}

// AddFeatureGatedCommand registers a command only when its feature is enabled
// for the current process (env overrides + defaults).
func AddFeatureGatedCommand(featureName string, command *cobra.Command) {
	if features.IsEnabledForProcess(featureName) {
		rootCmd.AddCommand(command)
	}
}

// autosyncGateOpen decides whether the autosync hooks should run for the
// project at baseDir (td-a4c721).
//
// Resolution order:
//  1. Global kill-switch (td-735875) wins — when the global autosync override
//     (config.json sync.autosync, or TD_FEATURE_SYNC_AUTOSYNC / TD_SYNC_AUTO)
//     resolves to an explicit false, the gate is closed everywhere.
//  2. The sync_autosync feature flag acts as an explicit override: when set
//     explicitly (env or project config), its value decides outright.
//  3. Otherwise (flag unset / source=default) the per-project sync config
//     decides — a project that is actually configured for sync autosyncs.
func autosyncGateOpen(baseDir string) bool {
	return syncGate(baseDir, nil).Open
}

func runGatedSyncStartupHook(cmd *cobra.Command) {
	if syncFeatureHooks.OnStartup == nil {
		return
	}
	if !autosyncGateOpen(getBaseDir()) {
		return
	}
	syncFeatureHooks.OnStartup(resolveCommandName(cmd))
}

func runGatedSyncMutationHook(cmd *cobra.Command) {
	if syncFeatureHooks.OnAfterMutation == nil {
		return
	}

	// Skip everything for non-mutating (read-only) commands: the stranded warning
	// and the autosync hook both only concern commands that change local data.
	commandName := resolveCommandName(cmd)
	if syncFeatureHooks.IsMutatingCommand != nil && !syncFeatureHooks.IsMutatingCommand(commandName) {
		return
	}

	// Independent of whether the autosync hook fires below, warn (throttled) when
	// this project is configured for sync but the gate is closed and changes are
	// piling up — otherwise that case is totally silent. strandedSyncShouldWarn
	// short-circuits with NO DB work unless the gate is closed by an explicit OFF,
	// and no-ops for unconfigured projects and the gate-OPEN case (autosync owns
	// the warning there), so this is safe to call here.
	warnIfSyncStranded(getBaseDir())

	if !autosyncGateOpen(getBaseDir()) {
		return
	}

	syncFeatureHooks.OnAfterMutation()
}

func resolveCommandName(cmd *cobra.Command) string {
	name := cmd.Name()
	if cmd.Parent() != nil && cmd.Parent().Name() != "td" {
		name = cmd.Parent().Name()
	}
	return name
}
