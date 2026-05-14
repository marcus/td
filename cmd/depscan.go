package cmd

import (
	"fmt"
	"strings"

	"github.com/marcus/td/internal/depscan"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var (
	depscanJSON         bool
	depscanCheckUpdates bool
	depscanVuln         bool
	depscanSeverity     string
)

var depscanCmd = &cobra.Command{
	Use:     "depscan",
	Short:   "Analyze Go module dependencies for security and maintenance risks",
	GroupID: "system",
	Long: `Analyze the current project's Go module dependencies for security and
maintenance risks.

Static, offline heuristics always run:
  - pseudo-versions   dependencies pinned to untagged commits
  - pre-1.0           v0.x modules with no API-stability guarantee
  - +incompatible     modules not migrated to Go modules
  - replace directive supply-chain / local-override risk
  - indirect surface  unusually large transitive dependency graphs
  - go directive      a go.mod 'go' version lagging the toolchain

Optional dynamic checks degrade gracefully when offline:
  --vuln            run govulncheck for known CVEs (requires govulncheck on PATH)
  --check-updates   run 'go list -m -u' to flag outdated modules`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		minSeverity, err := depscan.ParseSeverity(depscanSeverity)
		if err != nil {
			return err
		}

		report, err := depscan.Scan(depscan.Options{
			CheckUpdates: depscanCheckUpdates,
			Vuln:         depscanVuln,
		})
		if err != nil {
			return err
		}
		report = depscan.FilterBySeverity(report, minSeverity)

		if depscanJSON {
			return output.JSON(report)
		}
		renderReport(report, minSeverity)
		return nil
	},
}

// renderReport prints a grouped, human-readable dependency risk report.
func renderReport(report *depscan.Report, minSeverity depscan.Severity) {
	output.Info("Dependency Risk Scan: %s", report.Module)
	output.Info("go.mod: %s  (go %s)", report.GoMod, report.Go)
	s := report.Summary
	output.Info("Dependencies: %d direct, %d indirect  |  go.sum: %d verified modules",
		s.DirectDeps, s.IndirectDeps, s.GoSumModules)
	output.Info("Findings: %d total  (%d high, %d medium, %d low)",
		s.Total, s.High, s.Medium, s.Low)
	if minSeverity != depscan.SeverityLow {
		output.Info("Filtered to severity >= %s", minSeverity)
	}

	for _, note := range report.Notes {
		output.Warning("%s", note)
	}

	if len(report.Findings) == 0 {
		fmt.Println()
		output.Success("No dependency risks found at the requested severity.")
		return
	}

	groups := []struct {
		sev   depscan.Severity
		label string
	}{
		{depscan.SeverityHigh, "HIGH"},
		{depscan.SeverityMedium, "MEDIUM"},
		{depscan.SeverityLow, "LOW"},
	}
	for _, g := range groups {
		var lines []string
		for _, f := range report.Findings {
			if f.Severity != g.sev {
				continue
			}
			lines = append(lines, formatFinding(f))
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Print(output.SectionHeader(fmt.Sprintf("%s severity (%d)", g.label, len(lines))))
		for _, line := range lines {
			fmt.Println(line)
		}
	}
}

// formatFinding renders a single finding as an indented bullet line.
func formatFinding(f depscan.Finding) string {
	var b strings.Builder
	b.WriteString("  - [")
	b.WriteString(f.Category)
	b.WriteString("] ")
	if f.Module != "" {
		b.WriteString(f.Module)
		if f.Version != "" {
			b.WriteString("@")
			b.WriteString(f.Version)
		}
		b.WriteString(": ")
	}
	b.WriteString(f.Detail)
	return b.String()
}

func init() {
	depscanCmd.Flags().BoolVar(&depscanJSON, "json", false, "Output the report as JSON")
	depscanCmd.Flags().BoolVar(&depscanCheckUpdates, "check-updates", false, "Check for outdated modules via 'go list -m -u' (requires network)")
	depscanCmd.Flags().BoolVar(&depscanVuln, "vuln", false, "Run govulncheck for known CVEs (requires govulncheck on PATH)")
	depscanCmd.Flags().StringVar(&depscanSeverity, "severity", "low", "Minimum severity to report: high, medium, or low")
	rootCmd.AddCommand(depscanCmd)
}
