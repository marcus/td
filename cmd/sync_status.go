package cmd

import (
	"fmt"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/spf13/cobra"
)

// SyncStatusReport is the machine-readable result of `td sync status`. It is a
// pure data struct so the gather logic can be unit-tested without a cobra
// command or terminal. The thin printer (printSyncStatusText / output.JSON)
// renders it.
type SyncStatusReport struct {
	// Gate is the resolved autosync gate state: "ON", "OFF", or "KILLED".
	// It mirrors the real decision (autosyncGateOpen + globalKillSwitchOff) so
	// this never diverges from what autosync actually does.
	Gate string `json:"gate"`
	// GateSource explains WHERE the gate decision came from:
	//   "global-kill-switch"   — global autosync override resolved to false
	//   "explicit-env"         — sync_autosync set explicitly via env var
	//   "explicit-config"      — sync_autosync set explicitly via project config
	//   "derived-per-project"  — no explicit override; per-project config decides
	GateSource string `json:"gate_source"`

	// Configured reports whether the project has a usable sync_state row
	// (present, non-empty ProjectID, not disabled).
	Configured bool   `json:"configured"`
	ProjectID  string `json:"project_id,omitempty"`

	Authenticated bool   `json:"authenticated"`
	ServerURL     string `json:"server_url"`

	// PendingEvents is the count of unsynced action_log rows. -1 means the
	// count could not be determined (e.g. no DB), distinct from a real 0.
	PendingEvents int64  `json:"pending_events"`
	LastSyncAt    string `json:"last_sync_at,omitempty"`

	// Notes carries non-fatal degradation messages (e.g. DB could not be opened)
	// so the diagnostic never hard-errors but still surfaces the problem.
	Notes []string `json:"notes,omitempty"`
}

// gatherSyncStatus builds a SyncStatusReport for the project at baseDir. It is
// the one-stop diagnosis used by `td sync status`. It must NEVER hard-error: a
// missing DB or absent sync_state degrades to "configured: no" with a note.
//
// It reuses the real decision helpers (globalKillSwitchOff, features.Resolve,
// projectSyncConfigured) so the reported gate state matches autosyncGateOpen
// exactly rather than recomputing the logic divergently.
func gatherSyncStatus(baseDir string) SyncStatusReport {
	r := SyncStatusReport{
		Authenticated: syncconfig.IsAuthenticated(),
		ServerURL:     syncconfig.GetServerURL(),
		PendingEvents: -1,
	}

	// Resolve the gate state + source, mirroring autosyncGateOpen's order:
	//   1. global kill-switch (explicit global false) wins -> KILLED
	//   2. explicit sync_autosync override decides outright -> ON/OFF
	//   3. otherwise per-project config decides -> ON/OFF
	configured := projectSyncConfigured(baseDir)
	switch {
	case globalKillSwitchOff():
		r.Gate = "KILLED"
		r.GateSource = "global-kill-switch"
	default:
		if v, source := features.Resolve(baseDir, features.SyncAutosync.Name); source != "default" {
			if v {
				r.Gate = "ON"
			} else {
				r.Gate = "OFF"
			}
			r.GateSource = "explicit-" + source // explicit-env | explicit-config
		} else {
			if configured {
				r.Gate = "ON"
			} else {
				r.Gate = "OFF"
			}
			r.GateSource = "derived-per-project"
		}
	}

	r.Configured = configured

	// Read sync_state + pending count directly. This is the one place we touch
	// the DB; any failure degrades gracefully into a note rather than an error.
	if baseDir == "" {
		r.Notes = append(r.Notes, "no project directory resolved")
		return r
	}
	database, err := db.Open(baseDir)
	if err != nil {
		r.Notes = append(r.Notes, fmt.Sprintf("database unavailable: %v", err))
		return r
	}
	defer database.Close()

	state, err := database.GetSyncState()
	if err != nil {
		r.Notes = append(r.Notes, fmt.Sprintf("read sync state: %v", err))
	} else if state != nil {
		r.ProjectID = state.ProjectID
		if state.LastSyncAt != nil {
			r.LastSyncAt = state.LastSyncAt.Format(time.RFC3339)
		}
	}

	if pending, err := database.CountPendingEvents(); err != nil {
		r.Notes = append(r.Notes, fmt.Sprintf("count pending events: %v", err))
	} else {
		r.PendingEvents = pending
	}

	return r
}

// printSyncStatusText renders the human-readable diagnosis.
func printSyncStatusText(r SyncStatusReport) {
	fmt.Printf("Autosync gate ......... %s (%s)\n", r.Gate, r.GateSource)

	if r.Configured {
		if r.ProjectID != "" {
			fmt.Printf("Configured for sync ... yes (project %s)\n", r.ProjectID)
		} else {
			fmt.Printf("Configured for sync ... yes\n")
		}
	} else {
		fmt.Printf("Configured for sync ... no\n")
	}

	if r.Authenticated {
		fmt.Printf("Authenticated ......... yes\n")
	} else {
		fmt.Printf("Authenticated ......... no\n")
	}

	fmt.Printf("Server URL ............ %s\n", r.ServerURL)

	if r.PendingEvents < 0 {
		fmt.Printf("Pending events ........ unknown\n")
	} else {
		fmt.Printf("Pending events ........ %d\n", r.PendingEvents)
	}

	if r.LastSyncAt != "" {
		fmt.Printf("Last sync ............. %s\n", r.LastSyncAt)
	} else {
		fmt.Printf("Last sync ............. never\n")
	}

	for _, note := range r.Notes {
		fmt.Printf("  note: %s\n", note)
	}
}

// syncStatusCmd is the always-on read-only sync diagnostic. It is registered on
// the always-on `sync` surface (syncAlwaysOnCmd) when SyncCLI is OFF and under
// the full syncCmd when SyncCLI is ON — see init() in cmd/sync.go. Either way it
// is reachable, so a user debugging "why isn't this syncing" always has it.
var syncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Diagnose sync configuration and state (always available)",
	Long: `Show a one-stop diagnosis of local sync state: the autosync gate state and
the source of that decision, whether the project is configured for sync, auth
status, the server URL, pending unsynced events, and the last sync time.

This read-only command is always available, even when the sync CLI feature is
otherwise off, so a stranded project can always be diagnosed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		report := gatherSyncStatus(getBaseDir())
		if jsonMode(cmd) {
			return output.JSON(report)
		}
		printSyncStatusText(report)
		return nil
	},
}
