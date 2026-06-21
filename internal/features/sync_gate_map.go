package features

// GateMapEntry records a concrete sync surface that must be gated.
type GateMapEntry struct {
	Feature string
	Surface string
	Notes   string
}

// SyncGateMap is the authoritative map of sync-related surfaces to feature flags.
// Paths refer to the sync implementation currently on the "sunc" branch.
var SyncGateMap = []GateMapEntry{
	{
		Feature: SyncCLI.Name,
		Surface: "cmd/auth.go",
		Notes:   "Gates auth login/logout/status commands",
	},
	{
		Feature: SyncCLI.Name,
		Surface: "cmd/sync.go",
		Notes:   "Gates td sync command entry point",
	},
	{
		Feature: SyncCLI.Name,
		Surface: "cmd/sync_conflicts.go",
		Notes:   "Gates td sync conflicts subcommand",
	},
	{
		Feature: SyncCLI.Name,
		Surface: "cmd/sync_tail.go",
		Notes:   "Gates td sync tail subcommand",
	},
	{
		Feature: SyncCLI.Name,
		Surface: "cmd/sync_init.go",
		Notes:   "Gates guided sync setup wizard",
	},
	{
		Feature: SyncCLI.Name,
		Surface: "cmd/project.go",
		Notes:   "Gates sync-project management commands",
	},
	{
		Feature: SyncCLI.Name,
		Surface: "cmd/config.go",
		Notes:   "Gates sync configuration commands",
	},
	{
		Feature: SyncCLI.Name,
		Surface: "cmd/doctor.go",
		Notes:   "Gates sync diagnostics command",
	},
	{
		Feature: SyncAutosync.Name,
		Surface: "cmd/root.go#PersistentPreRun",
		Notes:   "Explicit override for startup push/pull hook (default: per-project sync config decides; td-a4c721)",
	},
	{
		Feature: SyncAutosync.Name,
		Surface: "cmd/root.go#PersistentPostRun",
		Notes:   "Explicit override for post-mutation autosync hook (default: per-project sync config decides; td-a4c721)",
	},
	{
		Feature: SyncAutosync.Name,
		Surface: "cmd/monitor.go",
		Notes:   "Explicit override for monitor periodic autosync loop (default: per-project sync config decides; td-a4c721)",
	},
	{
		Feature: SyncMonitorPrompt.Name,
		Surface: "pkg/monitor/commands.go#checkSyncPrompt",
		Notes:   "Gates first-run monitor sync prompt trigger",
	},
	{
		Feature: SyncMonitorPrompt.Name,
		Surface: "pkg/monitor/sync_prompt.go",
		Notes:   "Gates monitor sync prompt modal flows",
	},
	{
		Feature: SyncNotes.Name,
		Surface: "cmd/sync.go#syncEntityValidator",
		Notes:   "Gates notes entity handling during manual sync push/pull",
	},
	{
		Feature: SyncNotes.Name,
		Surface: "cmd/autosync.go",
		Notes:   "Gates notes entity handling during autosync push/pull",
	},
}
