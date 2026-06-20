package cmd

import (
	"fmt"
	"strings"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"current"},
	Short:   "Show dashboard: session, focus, review buckets (awaiting/ready-to-close/pending), blocked, ready",
	Long: `Shows a dashboard of the current session state.

Review buckets:
  AWAITING YOUR REVIEW    Issues in_review that you can independently review
  READY TO CLOSE          Issues with a recorded approval that you can close
  PENDING REVIEW          Issues you implemented that are still waiting on review`,
	GroupID: "session",
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

		jsonOutput := jsonMode(cmd)

		if jsonOutput {
			return outputStatusJSON(database, baseDir, sess.ID)
		}

		return outputStatusDashboard(database, baseDir, sess.ID)
	},
}

// outputStatusDashboard renders a dashboard view
func outputStatusDashboard(database *db.DB, baseDir, sessionID string) error {
	fmt.Printf("SESSION: %s\n\n", sessionID)

	// Get focused issue
	focusedID, _ := config.GetFocus(baseDir)
	if focusedID != "" {
		issue, err := database.GetIssue(focusedID)
		if err == nil {
			logs, _ := database.GetLogs(issue.ID, 10)
			logCount := len(logs)
			lastLog := ""
			if logCount > 0 {
				lastLog = output.FormatTimeAgo(logs[logCount-1].Timestamp)
			}

			fmt.Printf("FOCUSED: %s \"%s\" [%s]\n", issue.ID, issue.Title, issue.Status)
			if logCount > 0 {
				fmt.Printf("  └── %d logs, last: %s\n", logCount, lastLog)
			}
			fmt.Println()
		}
	}

	// Get in-review issues
	inReview, _ := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusInReview},
		SortBy: "priority",
	})
	reviewableByMe, _ := database.ListIssues(reviewableByOptions(baseDir, sessionID))
	reviewableByMeMap := make(map[string]bool, len(reviewableByMe))
	for _, issue := range reviewableByMe {
		reviewableByMeMap[issue.ID] = true
	}

	if len(inReview) > 0 {
		awaitingReview := make([]models.Issue, 0)
		readyToClose := make([]models.Issue, 0)
		pendingMine := make([]models.Issue, 0)
		readyReviews := make(map[string]*models.IssueReview)
		for _, issue := range inReview {
			rev, _ := database.GetActiveApprovalReview(issue.ID)
			if rev != nil && closerAllowed(&issue, sessionID, rev) {
				readyToClose = append(readyToClose, issue)
				readyReviews[issue.ID] = rev
				continue
			}
			if reviewableByMeMap[issue.ID] && rev == nil {
				awaitingReview = append(awaitingReview, issue)
				continue
			}
			if issue.ImplementerSession == sessionID {
				pendingMine = append(pendingMine, issue)
				continue
			}
		}
		if len(awaitingReview) > 0 {
			fmt.Printf("AWAITING YOUR REVIEW (%d):\n", len(awaitingReview))
			for _, issue := range awaitingReview {
				fmt.Printf("  %s \"%s\"\n", issue.ID, issue.Title)
			}
			fmt.Println()
		}
		if len(readyToClose) > 0 {
			fmt.Printf("READY TO CLOSE (%d):\n", len(readyToClose))
			for _, issue := range readyToClose {
				rev := readyReviews[issue.ID]
				fmt.Printf("  %s \"%s\" (reviewed by %s)\n", issue.ID, issue.Title, rev.ReviewerSession)
			}
			fmt.Println()
		}
		if len(pendingMine) > 0 {
			fmt.Printf("PENDING REVIEW (your implementation, %d):\n", len(pendingMine))
			for _, issue := range pendingMine {
				fmt.Printf("  %s \"%s\"\n", issue.ID, issue.Title)
			}
			fmt.Println()
		}
	}

	// Get blocked issues
	blocked, _ := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusBlocked},
		SortBy: "priority",
	})

	if len(blocked) > 0 {
		fmt.Printf("BLOCKED (%d):\n", len(blocked))
		for _, issue := range blocked {
			// Get what this issue depends on
			deps, _ := database.GetDependencies(issue.ID)
			waitingOn := ""
			if len(deps) > 0 {
				waitingOn = fmt.Sprintf(" waiting on %s", strings.Join(deps, ", "))
			}
			fmt.Printf("  %s \"%s\"%s\n", issue.ID, issue.Title, waitingOn)
		}
		fmt.Println()
	}

	// Get ready to start issues
	ready, _ := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusOpen},
		SortBy: "priority",
		Limit:  10,
	})

	if len(ready) > 0 {
		fmt.Printf("READY TO START (%d):\n", len(ready))
		for _, issue := range ready {
			fmt.Printf("  %s \"%s\" %s\n", issue.ID, issue.Title, issue.Priority)
		}
		fmt.Println()
	}

	// Summary if nothing to show
	if focusedID == "" && len(inReview) == 0 && len(blocked) == 0 && len(ready) == 0 {
		fmt.Println("No active work. Run 'td next' to see the next issue to start.")
	}

	return nil
}

// outputStatusJSON outputs status in JSON format
func outputStatusJSON(database *db.DB, baseDir, sessionID string) error {
	result := map[string]interface{}{
		"session": sessionID,
	}

	// Get focused issue
	focusedID, _ := config.GetFocus(baseDir)
	if focusedID != "" {
		issue, err := database.GetIssue(focusedID)
		if err == nil {
			logs, _ := database.GetLogs(issue.ID, 10)
			result["focused"] = map[string]interface{}{
				"issue":     issue,
				"log_count": len(logs),
				"last_log":  getLastLogTime(logs),
			}
		}
	}

	// Get in-review issues
	inReview, _ := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusInReview},
		SortBy: "priority",
	})
	reviewableByMeList, _ := database.ListIssues(reviewableByOptions(baseDir, sessionID))
	reviewableByMeMap := make(map[string]bool, len(reviewableByMeList))
	for _, issue := range reviewableByMeList {
		reviewableByMeMap[issue.ID] = true
	}

	reviewableByMe := []models.Issue{}
	implementedByMe := []models.Issue{}
	readyToClose := []models.Issue{}
	for _, issue := range inReview {
		rev, _ := database.GetActiveApprovalReview(issue.ID)
		if rev != nil && closerAllowed(&issue, sessionID, rev) {
			readyToClose = append(readyToClose, issue)
			continue
		}
		if reviewableByMeMap[issue.ID] && rev == nil {
			reviewableByMe = append(reviewableByMe, issue)
			continue
		}
		implementedByMe = append(implementedByMe, issue)
	}

	result["in_review"] = map[string]interface{}{
		"reviewable_by_you":  reviewableByMe,
		"ready_to_close":     readyToClose,
		"implemented_by_you": implementedByMe,
		"total":              len(inReview),
	}

	// Get blocked issues
	blocked, _ := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusBlocked},
		SortBy: "priority",
	})

	blockedWithDeps := []map[string]interface{}{}
	for _, issue := range blocked {
		deps, _ := database.GetDependencies(issue.ID)
		blockedWithDeps = append(blockedWithDeps, map[string]interface{}{
			"issue":      issue,
			"depends_on": deps,
		})
	}
	result["blocked"] = blockedWithDeps

	// Get ready to start issues
	ready, _ := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusOpen},
		SortBy: "priority",
		Limit:  10,
	})
	result["ready_to_start"] = ready

	return output.JSON(result)
}

// getLastLogTime returns the timestamp of the last log, or nil if no logs
func getLastLogTime(logs []models.Log) interface{} {
	if len(logs) == 0 {
		return nil
	}
	return logs[len(logs)-1].Timestamp
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
