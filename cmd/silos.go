package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/marcus/td/internal/analysis"
	"github.com/marcus/td/internal/db"
	"github.com/spf13/cobra"
)

var (
	silosFormat     string
	silosThreshold  float64
	silosCriticalOnly bool
)

var silosCmd = &cobra.Command{
	Use:   "silos",
	Short: "Analyze knowledge silos in the codebase",
	Long: `Analyze knowledge silos by examining file ownership patterns.

Shows which files are owned by single developers, identifies high-risk knowledge
concentrations, and suggests areas needing knowledge sharing.

Use --threshold to filter authors by file coverage ratio (default 0.8 = 80%).
Use --critical-only to show only files with single-author ownership.
Use --format to control output: table (default), json, or csv.

Examples:
  td silos                           Show all files and contributors
  td silos --critical-only           Show only single-author files
  td silos --threshold 0.5           Show contributors with >50% file coverage
  td silos --format json             Output as JSON for parsing
  td silos --format csv > silos.csv  Export to CSV`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSilos()
	},
	GroupID: "query",
}

func init() {
	rootCmd.AddCommand(silosCmd)
	silosCmd.Flags().StringVar(&silosFormat, "format", "table", "Output format: table, json, csv")
	silosCmd.Flags().Float64Var(&silosThreshold, "threshold", 0.8, "Author coverage threshold (0.0-1.0)")
	silosCmd.Flags().BoolVar(&silosCriticalOnly, "critical-only", false, "Show only files with single author")
}

func runSilos() error {
	baseDir := getBaseDir()
	database, err := db.Open(baseDir)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer database.Close()

	// Get underlying *sql.DB from the db.DB wrapper
	report, err := analysis.AnalyzeSilos(database.Conn(), baseDir)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	switch strings.ToLower(silosFormat) {
	case "json":
		return renderSilosJSON(report)
	case "csv":
		return renderSilosCSV(report)
	default:
		return renderSilosTable(report)
	}
}

func renderSilosTable(report *analysis.SiloReport) error {
	fmt.Printf("\n📊 KNOWLEDGE SILO ANALYSIS\n")
	fmt.Printf("Risk Score: %.1f/10\n\n", report.SiloRiskScore*10)

	// Summary stats
	fmt.Println("📈 COVERAGE SUMMARY")
	fmt.Printf("  Files tracked by issues: %d\n", len(report.FileOwnership))
	fmt.Printf("  Total code files in repo: %d\n", report.TotalCodeFiles)
	if report.TotalCodeFiles > 0 {
		fmt.Printf("  Code exploration: %.1f%%\n", report.ExploredCodeRatio*100)
	}
	fmt.Printf("  Issues with file attachments: %d\n", report.IssueCoverage)
	fmt.Printf("  Unique author-file pairs: %d\n\n", report.FileAuthorPairs)

	// Critical files
	if len(report.CriticalFiles) > 0 {
		fmt.Println("⚠️  CRITICAL FILES (single author)")
		printTableHeader([]string{"File Path"})

		for i, file := range report.CriticalFiles {
			if i >= 20 { // Limit display
				fmt.Printf("... and %d more critical files\n", len(report.CriticalFiles)-20)
				break
			}
			fmt.Printf("%-80s\n", truncatePath(file, 80))
		}
		fmt.Println()
	}

	// Author contributions
	if len(report.AuthorContribution) > 0 {
		fmt.Println("👥 AUTHOR CONTRIBUTIONS")
		printTableHeader([]string{"Author ID", "Files Owned", "Critical Risk", "Coverage %"})

		for _, ac := range report.AuthorContribution {
			if ac.RatioOfAll >= silosThreshold || !silosCriticalOnly {
				authorID := ac.AuthorID
				if len(authorID) > 8 {
					authorID = authorID[:8]
				}
				fmt.Printf("%-12s%-15d%-15d%-10s\n",
					authorID,
					ac.FileCount,
					ac.CriticalRisk,
					fmt.Sprintf("%.1f%%", ac.RatioOfAll*100),
				)
			}
		}
		fmt.Println()
	}

	// Suspicious patterns
	if len(report.SuspiciousPatterns) > 0 {
		fmt.Println("🚨 SUSPICIOUS PATTERNS")
		for _, pattern := range report.SuspiciousPatterns {
			emoji := "⚠️ "
			if pattern.Severity == "high" {
				emoji = "🔴"
			}
			fmt.Printf("%s %s [%s]\n", emoji, pattern.Pattern, pattern.Severity)
			fmt.Printf("   Reason: %s\n\n", pattern.Reason)
		}
	}

	// Recommendations
	fmt.Println("💡 RECOMMENDATIONS")
	if report.SiloRiskScore > 0.7 {
		fmt.Println("  • High silo risk detected—prioritize code reviews and knowledge sharing")
		fmt.Println("  • Consider pair programming on critical files")
	}
	if len(report.CriticalFiles) > len(report.FileOwnership)/2 {
		fmt.Println("  • More than 50% of files are single-author—establish code review guidelines")
	}
	if report.ExploredCodeRatio < 0.2 {
		fmt.Println("  • Less than 20% of codebase is tracked—link more files to issues")
	}
	fmt.Println()

	return nil
}

func printTableHeader(headers []string) {
	// Simple table header
	for i, h := range headers {
		if i == len(headers)-1 {
			fmt.Printf("%s\n", h)
		} else {
			fmt.Printf("%-30s ", h)
		}
	}
	fmt.Println(strings.Repeat("-", 80))
}

func renderSilosJSON(report *analysis.SiloReport) error {
	data := map[string]interface{}{
		"silo_risk_score":     report.SiloRiskScore,
		"files_tracked":       len(report.FileOwnership),
		"total_code_files":    report.TotalCodeFiles,
		"code_exploration":    report.ExploredCodeRatio,
		"issues_with_files":   report.IssueCoverage,
		"author_file_pairs":   report.FileAuthorPairs,
		"critical_files_count": len(report.CriticalFiles),
		"critical_files":      report.CriticalFiles,
		"author_contributions": report.AuthorContribution,
		"suspicious_patterns":  report.SuspiciousPatterns,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func renderSilosCSV(report *analysis.SiloReport) error {
	writer := csv.NewWriter(os.Stdout)
	defer writer.Flush()

	// Header
	writer.Write([]string{"Type", "Author/File", "Value", "Risk"})

	// Critical files
	for _, file := range report.CriticalFiles {
		writer.Write([]string{"critical_file", file, "1", "high"})
	}

	// Author contributions
	for _, ac := range report.AuthorContribution {
		writer.Write([]string{
			"author",
			ac.AuthorID,
			fmt.Sprintf("files=%d,critical=%d,coverage=%.1f%%", ac.FileCount, ac.CriticalRisk, ac.RatioOfAll*100),
			fmt.Sprintf("%.2f", ac.RatioOfAll),
		})
	}

	// Summary
	writer.Write([]string{"summary", "risk_score", fmt.Sprintf("%.2f", report.SiloRiskScore), ""})
	writer.Write([]string{"summary", "code_exploration", fmt.Sprintf("%.1f%%", report.ExploredCodeRatio*100), ""})

	return nil
}

func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	// Show end of path if too long
	return "..." + path[len(path)-(maxLen-3):]
}
