package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var (
	// Styles matching pkg/monitor/styles.go
	analyticsHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	analyticsLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	analyticsValueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
	analyticsErrorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	analyticsWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

const (
	barFilled = "█"
	barEmpty  = "░"
	dotChar   = "▪"
)

var statsAnalyticsCmd = &cobra.Command{
	Use:     "analytics",
	Aliases: []string{"usage"},
	Short:   "View command usage analytics",
	Long: `Shows local CLI usage statistics including most-used commands,
least-used commands, never-used commands, flag frequencies, and activity patterns.

Analytics are enabled by default. Set TD_ANALYTICS=false to disable.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		// Handle --clear
		if clear, _ := cmd.Flags().GetBool("clear"); clear {
			if err := db.ClearCommandUsage(baseDir); err != nil {
				output.Error("failed to clear analytics: %v", err)
				return err
			}
			fmt.Println("Cleared command usage analytics")
			return nil
		}

		// Parse filters
		sinceStr, _ := cmd.Flags().GetString("since")
		limit, _ := cmd.Flags().GetInt("limit")
		jsonOut, _ := cmd.Flags().GetBool("json")

		var since time.Time
		if sinceStr != "" {
			dur, err := session.ParseDuration(sinceStr)
			if err != nil {
				output.Error("invalid duration: %v", err)
				return err
			}
			since = time.Now().Add(-dur)
		}

		events, err := db.ReadCommandUsageFiltered(baseDir, since, limit)
		if err != nil {
			output.Error("failed to read analytics: %v", err)
			return err
		}

		if len(events) == 0 {
			fmt.Println("No analytics data recorded")
			return nil
		}

		// Get all registered commands for never-used detection
		allCommands := getAllCommandNames()
		summary := db.ComputeAnalyticsSummary(events, allCommands)

		if jsonOut {
			return output.JSON(summary)
		}

		// Human-readable output with charts
		renderAnalyticsSummary(summary)
		return nil
	},
}

// getAllCommandNames returns all registered command names (excluding help/completion)
func getAllCommandNames() []string {
	var names []string
	for _, cmd := range rootCmd.Commands() {
		// Skip hidden and built-in commands
		if cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion" {
			continue
		}
		names = append(names, cmd.Name())
	}
	return names
}

func renderAnalyticsSummary(s *db.AnalyticsSummary) {
	// Title
	fmt.Println(analyticsHeaderStyle.Render("COMMAND USAGE ANALYTICS"))
	fmt.Println()

	// Overview stats
	fmt.Printf("%s %d\n", analyticsLabelStyle.Render("Total commands:"), s.TotalCommands)
	fmt.Printf("%s %d\n", analyticsLabelStyle.Render("Unique commands:"), s.UniqueCommands)
	fmt.Printf("%s %.1f%%\n", analyticsLabelStyle.Render("Success rate:"), (1-s.ErrorRate)*100)
	fmt.Printf("%s %dms\n", analyticsLabelStyle.Render("Avg duration:"), s.AvgDurationMs)
	fmt.Println()

	// Most used commands bar chart
	if len(s.CommandCounts) > 0 {
		fmt.Println(analyticsHeaderStyle.Render("MOST USED COMMANDS"))
		renderBarChart(s.CommandCounts, 10, 30, false)
		fmt.Println()
	}

	// Least used commands (simple list)
	if len(s.CommandCounts) > 1 {
		fmt.Println(analyticsHeaderStyle.Render("LEAST USED COMMANDS"))
		renderLeastUsed(s.CommandCounts, 5)
		fmt.Println()
	}

	// Never used commands
	if len(s.NeverUsed) > 0 {
		fmt.Println(analyticsHeaderStyle.Render("NEVER USED"))
		if len(s.NeverUsed) > 5 {
			// Comma-separated on single line
			fmt.Printf("  %s\n", analyticsWarnStyle.Render(strings.Join(s.NeverUsed, ", ")))
		} else {
			for _, cmd := range s.NeverUsed {
				fmt.Printf("  %s\n", analyticsWarnStyle.Render(cmd))
			}
		}
		fmt.Println()
	}

	// Flag usage
	if len(s.FlagCounts) > 0 {
		fmt.Println(analyticsHeaderStyle.Render("POPULAR FLAGS"))
		renderBarChart(s.FlagCounts, 8, 25, false)
		fmt.Println()
	}

	// Daily activity (last 7 days)
	if len(s.DailyActivity) > 0 {
		fmt.Println(analyticsHeaderStyle.Render("DAILY ACTIVITY"))
		renderDailyChart(s.DailyActivity, 7)
		fmt.Println()
	}

	// Errors by command
	if len(s.ErrorsByCommand) > 0 {
		fmt.Println(analyticsHeaderStyle.Render("ERRORS BY COMMAND"))
		for cmd, count := range s.ErrorsByCommand {
			fmt.Printf("  %s %s\n", analyticsErrorStyle.Render(fmt.Sprintf("%d", count)), cmd)
		}
		fmt.Println()
	}

	// Session activity (top 5)
	if len(s.SessionActivity) > 0 {
		fmt.Println(analyticsHeaderStyle.Render("SESSION ACTIVITY"))
		renderSessionActivity(s.SessionActivity, 5)
	}
}

func renderBarChart(data map[string]int, maxItems, barWidth int, reverse bool) {
	// Sort by value
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range data {
		sorted = append(sorted, kv{k, v})
	}
	if reverse {
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Value < sorted[j].Value
		})
	} else {
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Value > sorted[j].Value
		})
	}

	// Limit items
	if len(sorted) > maxItems {
		sorted = sorted[:maxItems]
	}

	// Find max for scaling
	maxVal := 1
	for _, kv := range sorted {
		if kv.Value > maxVal {
			maxVal = kv.Value
		}
	}

	// Render bars
	for _, kv := range sorted {
		barLen := (kv.Value * barWidth) / maxVal
		filled := strings.Repeat(barFilled, barLen)
		empty := strings.Repeat(barEmpty, barWidth-barLen)

		label := fmt.Sprintf("%-14s", kv.Key)
		if len(label) > 14 {
			label = label[:13] + "…"
		}

		bar := analyticsValueStyle.Render(filled) + analyticsLabelStyle.Render(empty)
		fmt.Printf("  %s %s %d\n", label, bar, kv.Value)
	}
}

func renderLeastUsed(data map[string]int, maxItems int) {
	// Sort by value ascending
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range data {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value < sorted[j].Value
	})

	// Limit items
	if len(sorted) > maxItems {
		sorted = sorted[:maxItems]
	}

	// Render simple list
	for _, kv := range sorted {
		fmt.Printf("  %-14s %d\n", kv.Key, kv.Value)
	}
}

func renderDailyChart(data map[string]int, days int) {
	// Get last N days
	now := time.Now()
	maxVal := 1
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		if count := data[date]; count > maxVal {
			maxVal = count
		}
	}

	for i := days - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		dayLabel := now.AddDate(0, 0, -i).Format("Mon 01/02")
		count := data[date]

		// Scale bar to max 20 dots
		barLen := 0
		if maxVal > 0 {
			barLen = (count * 20) / maxVal
		}
		bar := strings.Repeat(dotChar, barLen)

		fmt.Printf("  %s %s %d\n", analyticsLabelStyle.Render(dayLabel), analyticsValueStyle.Render(bar), count)
	}
}

func renderSessionActivity(data map[string]int, maxItems int) {
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range data {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	if len(sorted) > maxItems {
		sorted = sorted[:maxItems]
	}

	for _, kv := range sorted {
		sessDisplay := kv.Key
		if len(sessDisplay) > 12 {
			sessDisplay = sessDisplay[:12]
		}
		fmt.Printf("  %s %d commands\n", analyticsLabelStyle.Render(sessDisplay), kv.Value)
	}
}

func init() {
	statsCmd.AddCommand(statsAnalyticsCmd)

	statsAnalyticsCmd.Flags().Bool("clear", false, "Clear analytics data")
	statsAnalyticsCmd.Flags().Bool("json", false, "Output as JSON")
	statsAnalyticsCmd.Flags().String("since", "", "Show data since duration (e.g., 7d, 24h)")
	statsAnalyticsCmd.Flags().Int("limit", 0, "Max events to analyze (0 = all)")
}
