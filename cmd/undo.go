package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var undoCmd = &cobra.Command{
	Use:   "undo",
	Short: "Undo the last action",
	Long: `Undo the last reversible action performed in this session.

Supported actions:
  - create: Deletes the created issue
  - delete: Restores the deleted issue
  - update: Reverts issue to previous state
  - start: Reverts issue to open status
  - review: Reverts issue to in_progress status
  - approve/reject: Reverts issue to in_review status

Use 'td undo --list' to see recent undoable actions.`,
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		// Run migrations to ensure action_log table exists
		if _, err := database.RunMigrations(); err != nil {
			output.Error("failed to run migrations: %v", err)
			return err
		}

		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// List mode
		if list, _ := cmd.Flags().GetBool("list"); list {
			actions, err := database.GetRecentActions(sess.ID, 10)
			if err != nil {
				output.Error("failed to get actions: %v", err)
				return err
			}

			if len(actions) == 0 {
				fmt.Println("No actions to undo")
				return nil
			}

			fmt.Println("RECENT ACTIONS:")
			for _, action := range actions {
				status := ""
				if action.Undone {
					status = " [undone]"
				}
				ago := formatTimeAgo(action.Timestamp)
				fmt.Printf("  %s %s %s (%s)%s\n",
					action.ActionType, action.EntityType, action.EntityID, ago, status)
			}
			return nil
		}

		// Get last undoable action
		action, err := database.GetLastAction(sess.ID)
		if err != nil {
			output.Error("failed to get last action: %v", err)
			return err
		}

		if action == nil {
			fmt.Printf("No actions to undo in current session (%s)\n", sess.ID)
			return nil
		}

		// Perform the undo
		if err := performUndo(database, action); err != nil {
			output.Error("failed to undo: %v", err)
			return err
		}

		// Mark action as undone
		if err := database.MarkActionUndone(action.ID); err != nil {
			output.Error("failed to mark action undone: %v", err)
			return err
		}

		fmt.Printf("UNDONE: %s %s %s\n", action.ActionType, action.EntityType, action.EntityID)
		return nil
	},
}

func performUndo(database *db.DB, action *models.ActionLog) error {
	switch action.EntityType {
	case "issue":
		return undoIssueAction(database, action)
	case "dependency", "issue_dependencies":
		return undoDependencyAction(database, action)
	case "file_link", "issue_files":
		return undoFileLinkAction(database, action)
	case "board_position", "board_issue_positions":
		return undoBoardPositionAction(database, action)
	case "board", "boards":
		return undoBoardAction(database, action)
	case "handoff":
		return undoHandoffAction(database, action)
	case "logs", "comments", "work_sessions":
		return fmt.Errorf("undo not supported for %s", action.EntityType)
	default:
		return fmt.Errorf("unknown entity type: %s", action.EntityType)
	}
}

func undoIssueAction(database *db.DB, action *models.ActionLog) error {
	switch action.ActionType {
	case models.ActionCreate:
		// Undo create by deleting
		return database.DeleteIssue(action.EntityID)

	case models.ActionDelete:
		// Undo delete by restoring
		return database.RestoreIssue(action.EntityID)

	case models.ActionUpdate, models.ActionStart, models.ActionReview,
		models.ActionApprove, models.ActionReject, models.ActionBlock, models.ActionUnblock, models.ActionClose, models.ActionReopen:
		// Restore previous state
		if action.PreviousData == "" {
			return fmt.Errorf("no previous data to restore")
		}
		var issue models.Issue
		if err := json.Unmarshal([]byte(action.PreviousData), &issue); err != nil {
			return fmt.Errorf("failed to parse previous data: %w", err)
		}
		return database.UpdateIssue(&issue)

	default:
		return fmt.Errorf("cannot undo action type: %s", action.ActionType)
	}
}

func undoDependencyAction(database *db.DB, action *models.ActionLog) error {
	// Parse the dependency info from entity_id (format: "issueID:dependsOnID")
	var depInfo struct {
		IssueID     string `json:"issue_id"`
		DependsOnID string `json:"depends_on_id"`
	}
	if err := json.Unmarshal([]byte(action.NewData), &depInfo); err != nil {
		return fmt.Errorf("failed to parse dependency data: %w", err)
	}

	switch action.ActionType {
	case models.ActionAddDep:
		return database.RemoveDependency(depInfo.IssueID, depInfo.DependsOnID)
	case models.ActionRemoveDep:
		return database.AddDependency(depInfo.IssueID, depInfo.DependsOnID, "depends_on")
	default:
		return fmt.Errorf("cannot undo dependency action: %s", action.ActionType)
	}
}

func undoFileLinkAction(database *db.DB, action *models.ActionLog) error {
	var linkInfo struct {
		IssueID   string `json:"issue_id"`
		FilePath  string `json:"file_path"`
		Role      string `json:"role"`
		SHA       string `json:"sha"`        // legacy field
		LinkedSHA string `json:"linked_sha"` // new canonical field
	}
	if err := json.Unmarshal([]byte(action.NewData), &linkInfo); err != nil {
		return fmt.Errorf("failed to parse file link data: %w", err)
	}
	// Prefer linked_sha, fall back to legacy sha
	sha := linkInfo.LinkedSHA
	if sha == "" {
		sha = linkInfo.SHA
	}

	switch action.ActionType {
	case models.ActionLinkFile:
		return database.UnlinkFile(linkInfo.IssueID, linkInfo.FilePath)
	case models.ActionUnlinkFile:
		return database.LinkFile(linkInfo.IssueID, linkInfo.FilePath, models.FileRole(linkInfo.Role), sha)
	default:
		return fmt.Errorf("cannot undo file link action: %s", action.ActionType)
	}
}

func undoBoardPositionAction(database *db.DB, action *models.ActionLog) error {
	var posInfo struct {
		BoardID  string `json:"board_id"`
		IssueID  string `json:"issue_id"`
		Position int    `json:"position"`
	}
	if err := json.Unmarshal([]byte(action.NewData), &posInfo); err != nil {
		return fmt.Errorf("failed to parse board position data: %w", err)
	}

	switch action.ActionType {
	case models.ActionBoardSetPosition:
		return database.RemoveIssuePosition(posInfo.BoardID, posInfo.IssueID)
	case models.ActionBoardUnposition:
		if posInfo.Position > 0 {
			return database.SetIssuePosition(posInfo.BoardID, posInfo.IssueID, posInfo.Position)
		}
		return nil // no position to restore
	default:
		return fmt.Errorf("cannot undo board position action: %s", action.ActionType)
	}
}

func undoHandoffAction(database *db.DB, action *models.ActionLog) error {
	switch action.ActionType {
	case models.ActionHandoff:
		return database.DeleteHandoff(action.EntityID)
	default:
		return fmt.Errorf("cannot undo handoff action: %s", action.ActionType)
	}
}

func undoBoardAction(database *db.DB, action *models.ActionLog) error {
	switch action.ActionType {
	case models.ActionBoardCreate, models.ActionCreate:
		// Undo create by deleting (handles both "board_create" and backfill "create")
		return database.DeleteBoard(action.EntityID)

	case models.ActionBoardDelete, models.ActionDelete:
		// Undo delete by restoring from previous data
		if action.PreviousData == "" {
			return fmt.Errorf("no previous data to restore")
		}
		var board models.Board
		if err := json.Unmarshal([]byte(action.PreviousData), &board); err != nil {
			return fmt.Errorf("failed to parse previous data: %w", err)
		}
		return database.RestoreBoard(&board)

	case models.ActionBoardUpdate, models.ActionUpdate:
		// Restore previous state (handles both "board_update" and generic "update")
		if action.PreviousData == "" {
			return fmt.Errorf("no previous data to restore")
		}
		var board models.Board
		if err := json.Unmarshal([]byte(action.PreviousData), &board); err != nil {
			return fmt.Errorf("failed to parse previous data: %w", err)
		}
		return database.UpdateBoard(&board)

	default:
		return fmt.Errorf("cannot undo board action: %s", action.ActionType)
	}
}

func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1d ago"
	}
	return fmt.Sprintf("%dd ago", days)
}

var lastCmd = &cobra.Command{
	Use:     "last",
	Short:   "Show the last action performed",
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		// Run migrations to ensure action_log table exists
		if _, err := database.RunMigrations(); err != nil {
			output.Error("failed to run migrations: %v", err)
			return err
		}

		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		n, _ := cmd.Flags().GetInt("n")
		if n <= 0 {
			n = 1
		}

		actions, err := database.GetRecentActions(sess.ID, n)
		if err != nil {
			output.Error("failed to get actions: %v", err)
			return err
		}

		if len(actions) == 0 {
			fmt.Printf("No actions in current session (%s)\n", sess.ID)
			return nil
		}

		for _, action := range actions {
			status := ""
			if action.Undone {
				status = " [undone]"
			}
			ago := formatTimeAgo(action.Timestamp)
			fmt.Printf("%s %s %s (%s)%s\n",
				action.ActionType, action.EntityType, action.EntityID, ago, status)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(undoCmd)
	rootCmd.AddCommand(lastCmd)
	undoCmd.Flags().Bool("list", false, "List recent undoable actions")
	lastCmd.Flags().IntP("n", "n", 1, "Number of actions to show")
}
