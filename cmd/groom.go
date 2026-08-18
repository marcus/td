package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

// Finding represents a single grooming issue found on a task.
type Finding struct {
	IssueID  string `json:"issue_id"`
	Title    string `json:"title"`
	Check    string `json:"check"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // "warning" or "info"
}

// GroomResult is the structured output for --json mode.
type GroomResult struct {
	Findings []Finding `json:"findings"`
	Total    int       `json:"total"`
	Checked  int       `json:"checked"`
}

var groomCmd = &cobra.Command{
	Use:     "groom",
	Short:   "Identify tasks needing refinement",
	Long:    `Scans open/active issues and identifies tasks needing refinement: missing descriptions, no acceptance criteria, unestimated points, vague titles, stale issues, and empty epics.`,
	GroupID: "shortcuts",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		statusFlag, _ := cmd.Flags().GetString("status")
		checkFlag, _ := cmd.Flags().GetString("check")
		jsonOutput, _ := cmd.Flags().GetBool("json")

		statuses := parseStatuses(statusFlag)
		enabledChecks := parseChecks(checkFlag)

		now := time.Now()

		issues, err := database.ListIssues(db.ListIssuesOptions{
			Status: statuses,
		})
		if err != nil {
			output.Error("Failed to list issues: %v", err)
			return err
		}

		var findings []Finding

		for i := range issues {
			issue := &issues[i]
			isEpic := issue.Type == models.TypeEpic

			if checkEnabled(enabledChecks, "description") {
				if f := checkMissingDescription(issue); f != nil {
					findings = append(findings, *f)
				}
			}

			if checkEnabled(enabledChecks, "acceptance") && !isEpic {
				if f := checkMissingAcceptance(issue); f != nil {
					findings = append(findings, *f)
				}
			}

			if checkEnabled(enabledChecks, "points") && !isEpic {
				if f := checkUnestimatedPoints(issue); f != nil {
					findings = append(findings, *f)
				}
			}

			if checkEnabled(enabledChecks, "title") {
				if f := checkVagueTitle(issue); f != nil {
					findings = append(findings, *f)
				}
			}

			if checkEnabled(enabledChecks, "stale") {
				if f := checkStale(issue, now); f != nil {
					findings = append(findings, *f)
				}
			}

			if checkEnabled(enabledChecks, "epic") && isEpic {
				if f := checkEmptyEpic(issue, database); f != nil {
					findings = append(findings, *f)
				}
			}
		}

		if jsonOutput {
			return output.JSON(GroomResult{
				Findings: findings,
				Total:    len(findings),
				Checked:  len(issues),
			})
		}

		printFindings(findings, len(issues))
		return nil
	},
}

func parseStatuses(flag string) []models.Status {
	parts := strings.Split(flag, ",")
	var statuses []models.Status
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch p {
		case "open":
			statuses = append(statuses, models.StatusOpen)
		case "in_progress":
			statuses = append(statuses, models.StatusInProgress)
		case "in_review":
			statuses = append(statuses, models.StatusInReview)
		case "blocked":
			statuses = append(statuses, models.StatusBlocked)
		}
	}
	return statuses
}

func parseChecks(flag string) map[string]bool {
	if flag == "" {
		return nil // nil means all checks enabled
	}
	checks := make(map[string]bool)
	for _, c := range strings.Split(flag, ",") {
		checks[strings.TrimSpace(c)] = true
	}
	return checks
}

func checkEnabled(enabled map[string]bool, name string) bool {
	if enabled == nil {
		return true
	}
	return enabled[name]
}

func checkMissingDescription(issue *models.Issue) *Finding {
	if strings.TrimSpace(issue.Description) == "" {
		return &Finding{
			IssueID:  issue.ID,
			Title:    issue.Title,
			Check:    "description",
			Message:  "Missing description",
			Severity: "warning",
		}
	}
	return nil
}

func checkMissingAcceptance(issue *models.Issue) *Finding {
	if strings.TrimSpace(issue.Acceptance) == "" {
		return &Finding{
			IssueID:  issue.ID,
			Title:    issue.Title,
			Check:    "acceptance",
			Message:  "Missing acceptance criteria",
			Severity: "warning",
		}
	}
	return nil
}

func checkUnestimatedPoints(issue *models.Issue) *Finding {
	if issue.Points == 0 {
		return &Finding{
			IssueID:  issue.ID,
			Title:    issue.Title,
			Check:    "points",
			Message:  "No points estimated",
			Severity: "info",
		}
	}
	return nil
}

func checkVagueTitle(issue *models.Issue) *Finding {
	if len(issue.Title) < 15 {
		return &Finding{
			IssueID:  issue.ID,
			Title:    issue.Title,
			Check:    "title",
			Message:  fmt.Sprintf("Title too short (%d chars)", len(issue.Title)),
			Severity: "info",
		}
	}
	return nil
}

func checkStale(issue *models.Issue, now time.Time) *Finding {
	daysSinceUpdate := int(now.Sub(issue.UpdatedAt).Hours() / 24)

	if issue.Status == models.StatusInProgress && daysSinceUpdate > 7 {
		return &Finding{
			IssueID:  issue.ID,
			Title:    issue.Title,
			Check:    "stale",
			Message:  fmt.Sprintf("In progress with no update for %d days", daysSinceUpdate),
			Severity: "warning",
		}
	}

	if issue.Status == models.StatusOpen && daysSinceUpdate > 30 {
		return &Finding{
			IssueID:  issue.ID,
			Title:    issue.Title,
			Check:    "stale",
			Message:  fmt.Sprintf("Open with no update for %d days", daysSinceUpdate),
			Severity: "info",
		}
	}

	return nil
}

func checkEmptyEpic(issue *models.Issue, database *db.DB) *Finding {
	children, err := database.ListIssues(db.ListIssuesOptions{
		ParentID: issue.ID,
	})
	if err != nil || len(children) == 0 {
		return &Finding{
			IssueID:  issue.ID,
			Title:    issue.Title,
			Check:    "epic",
			Message:  "Epic has no children",
			Severity: "warning",
		}
	}
	return nil
}

var (
	groomHeaderStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	groomWarnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	groomInfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	groomIDStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
	groomSummaryStyle = lipgloss.NewStyle().Bold(true)
)

var checkLabels = map[string]string{
	"description": "Missing Description",
	"acceptance":  "Missing Acceptance Criteria",
	"points":      "Unestimated Points",
	"title":       "Vague Titles",
	"stale":       "Stale Issues",
	"epic":        "Empty Epics",
}

var checkOrder = []string{"description", "acceptance", "points", "title", "stale", "epic"}

func printFindings(findings []Finding, checked int) {
	if len(findings) == 0 {
		output.Success("All %d issues look well-groomed!", checked)
		return
	}

	// Group by check
	grouped := make(map[string][]Finding)
	for _, f := range findings {
		grouped[f.Check] = append(grouped[f.Check], f)
	}

	for _, check := range checkOrder {
		group := grouped[check]
		if len(group) == 0 {
			continue
		}

		label := checkLabels[check]
		fmt.Println(groomHeaderStyle.Render(fmt.Sprintf("\n%s (%d)", label, len(group))))

		for _, f := range group {
			id := groomIDStyle.Render(f.IssueID)
			var msg string
			if f.Severity == "warning" {
				msg = groomWarnStyle.Render(f.Message)
			} else {
				msg = groomInfoStyle.Render(f.Message)
			}
			fmt.Printf("  %s  %s — %s\n", id, f.Title, msg)
		}
	}

	fmt.Println()
	fmt.Println(groomSummaryStyle.Render(fmt.Sprintf("%d findings across %d issues", len(findings), checked)))
}

func init() {
	rootCmd.AddCommand(groomCmd)
	groomCmd.Flags().Bool("json", false, "Output as JSON")
	groomCmd.Flags().String("status", "open,in_progress,in_review", "Statuses to groom (comma-separated)")
	groomCmd.Flags().String("check", "", "Run only specific checks (comma-separated: description,acceptance,points,title,stale,epic)")
}
