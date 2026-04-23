package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

// Styles for sync tail output
var (
	pushArrow = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("→")  // green
	pullArrow = lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Render("←")  // cyan
	dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

var syncTailCmd = &cobra.Command{
	Use:   "tail",
	Short: "Show recent sync activity",
	Long: `Show recent push/pull events. Use -f to follow in real-time.

Examples:
  td sync tail          # Show last 20 sync events
  td sync tail -f       # Follow new events in real-time
  td sync tail -n 50    # Show last 50 events
  td sync tail -f -n 0  # Follow only new events, skip history`,
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")
		lines, _ := cmd.Flags().GetInt("lines")

		baseDir := getBaseDir()
		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("open database: %v", err)
			return err
		}
		defer database.Close()

		// Show initial entries
		var entries []db.SyncHistoryEntry
		if lines > 0 {
			entries, err = database.GetSyncHistoryTail(lines)
			if err != nil {
				output.Error("query sync history: %v", err)
				return err
			}
		}

		var maxID int64
		for _, e := range entries {
			printSyncEntry(e)
			if e.ID > maxID {
				maxID = e.ID
			}
		}

		if !follow {
			if len(entries) == 0 {
				fmt.Println("No sync activity recorded.")
			}
			return nil
		}

		// If no initial entries were shown but we're following,
		// get the current max ID to only show new events
		if maxID == 0 && lines == 0 {
			tail, _ := database.GetSyncHistoryTail(1)
			if len(tail) > 0 {
				maxID = tail[0].ID
			}
		}

		// Follow mode: poll for new entries, handle Ctrl+C gracefully
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-sigCh:
				fmt.Println() // clean line after ^C
				return nil
			case <-ticker.C:
				newEntries, err := database.GetSyncHistory(maxID, 100)
				if err != nil {
					slog.Debug("sync tail: poll", "err", err)
					continue
				}
				for _, e := range newEntries {
					printSyncEntry(e)
					if e.ID > maxID {
						maxID = e.ID
					}
				}
			}
		}
	},
}

func printSyncEntry(e db.SyncHistoryEntry) {
	arrow := pullArrow
	if e.Direction == "push" {
		arrow = pushArrow
	}

	ts := dimStyle.Render(e.Timestamp.Format("15:04:05"))
	seq := fmt.Sprintf("seq:%d", e.ServerSeq)

	line := fmt.Sprintf("%s %s %s %s/%s (%s) %s",
		ts, arrow, e.Direction, e.EntityType,
		truncateID(e.EntityID, 16), e.ActionType, seq)

	if e.Direction == "pull" && e.DeviceID != "" {
		line += fmt.Sprintf(" from:%s", truncateID(e.DeviceID, 12))
	}
	fmt.Println(line)
}

func truncateID(id string, max int) string {
	if len(id) <= max {
		return id
	}
	return id[:max-3] + "..."
}

func init() {
	syncTailCmd.Flags().BoolP("follow", "f", false, "Follow new events in real-time")
	syncTailCmd.Flags().IntP("lines", "n", 20, "Number of initial lines to show")
	syncCmd.AddCommand(syncTailCmd)
}
