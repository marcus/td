package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var (
	busFactorHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	busFactorLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	busFactorValueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
	busFactorWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	busFactorDangerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

var statsBusFactorCmd = &cobra.Command{
	Use:   "bus-factor",
	Short: "Analyze code ownership concentration risks",
	Long: `Analyzes git blame data to identify code ownership concentration.

Computes bus factor scores per directory — the minimum number of people
who collectively own more than 50% of the code. A bus factor of 1 means
a single person owns the majority of that code, which is a risk.

Results are sorted by risk (lowest bus factor first).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !git.IsRepo() {
			output.Error("not a git repository")
			return fmt.Errorf("not a git repository")
		}

		path, _ := cmd.Flags().GetString("path")
		depth, _ := cmd.Flags().GetInt("depth")
		minBF, _ := cmd.Flags().GetInt("min-bus-factor")
		jsonOut, _ := cmd.Flags().GetBool("json")

		result, err := git.AnalyzeBusFactor(git.BusFactorOptions{
			Path:  path,
			Depth: depth,
		})
		if err != nil {
			output.Error("analysis failed: %v", err)
			return err
		}

		// Filter by min bus factor if set
		if minBF > 0 {
			filtered := make([]git.DirOwnership, 0, len(result.Dirs))
			for _, d := range result.Dirs {
				if d.BusFactor <= minBF {
					filtered = append(filtered, d)
				}
			}
			result.Dirs = filtered
		}

		if jsonOut {
			return outputJSON(result)
		}

		renderBusFactorTable(result)
		return nil
	},
}

func outputJSON(result *git.BusFactorResult) error {
	type jsonDir struct {
		Path         string  `json:"path"`
		BusFactor    int     `json:"bus_factor"`
		TopOwnerPct  float64 `json:"top_owner_pct"`
		TopOwner     string  `json:"top_owner"`
		Contributors int     `json:"contributors"`
		TotalLines   int     `json:"total_lines"`
		FileCount    int     `json:"file_count"`
	}

	dirs := make([]jsonDir, 0, len(result.Dirs))
	for _, d := range result.Dirs {
		topOwner := ""
		if len(d.Authors) > 0 {
			topOwner = d.Authors[0].Author
		}
		dirs = append(dirs, jsonDir{
			Path:         d.Path,
			BusFactor:    d.BusFactor,
			TopOwnerPct:  d.TopOwnerPct,
			TopOwner:     topOwner,
			Contributors: d.Contributors,
			TotalLines:   d.TotalLines,
			FileCount:    d.FileCount,
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(dirs)
}

func renderBusFactorTable(result *git.BusFactorResult) {
	fmt.Println(busFactorHeaderStyle.Render("BUS FACTOR ANALYSIS"))
	fmt.Println()

	if len(result.Dirs) == 0 {
		fmt.Println("  No directories to analyze")
		return
	}

	// Compute column widths
	maxPath := 4 // "PATH"
	maxOwner := 9 // "TOP OWNER"
	for _, d := range result.Dirs {
		if len(d.Path) > maxPath {
			maxPath = len(d.Path)
		}
		if len(d.Authors) > 0 && len(d.Authors[0].Author) > maxOwner {
			maxOwner = len(d.Authors[0].Author)
		}
	}
	if maxPath > 40 {
		maxPath = 40
	}
	if maxOwner > 20 {
		maxOwner = 20
	}

	// Header
	header := fmt.Sprintf("  %-*s  %s  %s  %s  %-*s  %s",
		maxPath, "PATH",
		"BF",
		"TOP %",
		"#",
		maxOwner, "TOP OWNER",
		"LINES",
	)
	fmt.Println(busFactorLabelStyle.Render(header))
	fmt.Println(busFactorLabelStyle.Render("  " + strings.Repeat("─", len(header)-2)))

	for _, d := range result.Dirs {
		topOwner := ""
		if len(d.Authors) > 0 {
			topOwner = d.Authors[0].Author
		}

		// Truncate long values
		pathDisplay := d.Path
		if len(pathDisplay) > maxPath {
			pathDisplay = pathDisplay[:maxPath-1] + "…"
		}
		if len(topOwner) > maxOwner {
			topOwner = topOwner[:maxOwner-1] + "…"
		}

		// Color the bus factor score
		bfStr := fmt.Sprintf("%2d", d.BusFactor)
		switch {
		case d.BusFactor <= 1:
			bfStr = busFactorDangerStyle.Render(bfStr)
		case d.BusFactor <= 2:
			bfStr = busFactorWarnStyle.Render(bfStr)
		default:
			bfStr = busFactorValueStyle.Render(bfStr)
		}

		// Color top owner percentage
		pctStr := fmt.Sprintf("%5.1f", d.TopOwnerPct)
		switch {
		case d.TopOwnerPct >= 80:
			pctStr = busFactorDangerStyle.Render(pctStr)
		case d.TopOwnerPct >= 60:
			pctStr = busFactorWarnStyle.Render(pctStr)
		default:
			pctStr = busFactorValueStyle.Render(pctStr)
		}

		fmt.Printf("  %-*s  %s  %s  %2d  %-*s  %5d\n",
			maxPath, pathDisplay,
			bfStr,
			pctStr,
			d.Contributors,
			maxOwner, topOwner,
			d.TotalLines,
		)
	}

	// Summary
	fmt.Println()
	riskCount := 0
	for _, d := range result.Dirs {
		if d.BusFactor <= 1 {
			riskCount++
		}
	}
	if riskCount > 0 {
		fmt.Printf("  %s %d of %d directories have a bus factor of 1\n",
			busFactorDangerStyle.Render("⚠"),
			riskCount, len(result.Dirs))
	} else {
		fmt.Printf("  %s No single-owner hotspots detected\n",
			busFactorValueStyle.Render("✓"))
	}
}

func init() {
	statsCmd.AddCommand(statsBusFactorCmd)

	statsBusFactorCmd.Flags().String("path", "", "Scope analysis to subdirectory")
	statsBusFactorCmd.Flags().Int("depth", 2, "Directory tree aggregation depth")
	statsBusFactorCmd.Flags().Int("min-bus-factor", 0, "Only show directories with bus factor <= this value")
	statsBusFactorCmd.Flags().Bool("json", false, "Output as JSON")
}
