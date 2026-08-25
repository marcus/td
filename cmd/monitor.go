package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/marcus/td/pkg/monitor"
	"github.com/marcus/td/pkg/tdsync"
	"github.com/spf13/cobra"
)

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Live TUI dashboard for observing agent activity",
	Long: `Launch a live-updating TUI dashboard showing:
- Current work: focused issue and in-progress tasks
- Activity log: recent logs, actions, and comments from all sessions
- Task list: ready, reviewable, and blocked issues

Key bindings:
  Tab/Shift+Tab  Switch panels
  1/2/3          Jump to panel
  ↑/↓            Select row in active panel
  j/k            Scroll viewport
  Enter          Open issue details modal
  Esc            Close modal
  r              Force refresh
  ?              Toggle help
  q              Quit

Mouse support:
  Click          Select panel/row
  Double-click   Open issue details
  Scroll wheel   Scroll hovered panel`,
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		interval, _ := cmd.Flags().GetDuration("interval")
		if interval < 500*time.Millisecond {
			interval = 2 * time.Second
		}

		model := monitor.NewModel(database, sess.ID, interval, versionStr, baseDir)

		// The monitor is still the cadence owner in Phase 1, but gate policy and
		// steady-state sync now come from the shared in-process facade.
		syncInterval := time.Duration(0)
		syncer, _ := tdsync.New(tdsync.Options{BaseDir: baseDir, DB: database})
		if gate := syncer.Gate(); gate.Open {
			model.AutoSyncFunc = func() {
				if _, err := syncer.Once(context.Background()); err != nil {
					slog.Debug("monitor: autosync", "err", err)
				}
			}
			syncInterval = syncconfig.GetAutoSyncInterval()
			model.AutoSyncInterval = syncInterval
			slog.Debug("monitor: autosync configured", "interval", syncInterval)
		}

		// Start independent periodic sync goroutine. BubbleTea's tea.Cmd dispatch
		// can stall under certain terminal/PTY conditions, so we run sync outside
		// the event loop to guarantee it fires reliably.
		ctx, cancelSync := context.WithCancel(context.Background())
		if syncInterval > 0 {
			go func() {
				ticker := time.NewTicker(syncInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if _, err := syncer.Once(ctx); err != nil && ctx.Err() == nil {
							slog.Debug("monitor: autosync", "err", err)
						}
					}
				}
			}()
		}

		p := tea.NewProgram(model)
		if _, err := p.Run(); err != nil {
			cancelSync()
			return fmt.Errorf("error running monitor: %w", err)
		}

		cancelSync()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(monitorCmd)
	monitorCmd.Flags().Duration("interval", 2*time.Second, "Refresh interval (default 2s)")
}
