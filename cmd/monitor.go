package cmd

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/pkg/monitor"
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
  q              Quit`,
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		interval, _ := cmd.Flags().GetDuration("interval")
		if interval < 500*time.Millisecond {
			interval = 2 * time.Second
		}

		model := monitor.NewModel(database, sess.ID, interval, versionStr)

		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("error running monitor: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(monitorCmd)
	monitorCmd.Flags().Duration("interval", 2*time.Second, "Refresh interval (default 2s)")
}
