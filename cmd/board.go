package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/query"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var boardCmd = &cobra.Command{
	Use:     "board",
	Short:   "Manage issue boards",
	Long:    `Create, list, and manage query-based issue boards.`,
	GroupID: "core",
}

var boardListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all boards",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		boards, err := database.ListBoards()
		if err != nil {
			output.Error("%v", err)
			return err
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			data, _ := json.MarshalIndent(boards, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if len(boards) == 0 {
			output.Info("No boards found")
			return nil
		}

		for _, b := range boards {
			builtin := ""
			if b.IsBuiltin {
				builtin = " [builtin]"
			}
			queryDisplay := ""
			if b.Query != "" {
				queryDisplay = fmt.Sprintf(" (%s)", b.Query)
			}
			fmt.Printf("%s: %s%s%s\n", b.ID, b.Name, queryDisplay, builtin)
		}

		return nil
	},
}

var boardCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new board",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("requires board name")
		}
		if len(args) > 1 {
			return fmt.Errorf("too many arguments\n  Use: td board create \"%s\" --query '%s'", args[0], strings.Join(args[1:], " "))
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		name := args[0]

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		query, _ := cmd.Flags().GetString("query")

		board, err := database.CreateBoard(name, query)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Log action for undo
		newData, _ := json.Marshal(board)
		logActionWithSession(baseDir, database, &models.ActionLog{
			ActionType: models.ActionBoardCreate,
			EntityType: "board",
			EntityID:   board.ID,
			NewData:    string(newData),
		})

		output.Success("Created board %s (%s)", board.Name, board.ID)
		return nil
	},
}

var boardDeleteCmd = &cobra.Command{
	Use:   "delete <board>",
	Short: "Delete a board",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		ref := args[0]

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		board, err := database.ResolveBoardRef(ref)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Log action for undo
		prevData, _ := json.Marshal(board)
		logActionWithSession(baseDir, database, &models.ActionLog{
			ActionType:   models.ActionBoardDelete,
			EntityType:   "board",
			EntityID:     board.ID,
			PreviousData: string(prevData),
		})

		if err := database.DeleteBoard(board.ID); err != nil {
			output.Error("%v", err)
			return err
		}

		output.Success("Deleted board %s", board.Name)
		return nil
	},
}

var boardShowCmd = &cobra.Command{
	Use:   "show <board>",
	Short: "Show issues in a board",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		ref := args[0]

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		board, err := database.ResolveBoardRef(ref)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		sess, _ := session.GetOrCreate(database)
		sessionID := ""
		if sess != nil {
			sessionID = sess.ID
		}

		// Parse status filter
		var statusFilter []models.Status
		if statusStr, _ := cmd.Flags().GetStringArray("status"); len(statusStr) > 0 {
			for _, s := range statusStr {
				for _, part := range strings.Split(s, ",") {
					part = strings.TrimSpace(part)
					if part != "" {
						status := models.NormalizeStatus(part)
						if models.IsValidStatus(status) {
							statusFilter = append(statusFilter, status)
						}
					}
				}
			}
		}

		// Default: hide closed unless specified
		if len(statusFilter) == 0 {
			statusFilter = []models.Status{
				models.StatusOpen,
				models.StatusInProgress,
				models.StatusBlocked,
				models.StatusInReview,
			}
		}

		// Get issues for the board
		var issues []models.BoardIssueView
		if board.Query != "" {
			// Execute TDQ query, then apply positions
			queryResults, err := query.Execute(database, board.Query, sessionID, query.ExecuteOptions{})
			if err != nil {
				output.Error("Query error: %v", err)
				return err
			}
			// Filter by status (query.Execute doesn't filter by status)
			var filtered []models.Issue
			statusSet := make(map[models.Status]bool)
			for _, s := range statusFilter {
				statusSet[s] = true
			}
			for _, issue := range queryResults {
				if statusSet[issue.Status] {
					filtered = append(filtered, issue)
				}
			}
			issues, err = database.ApplyBoardPositions(board.ID, filtered)
			if err != nil {
				output.Error("%v", err)
				return err
			}
		} else {
			// Empty query - use GetBoardIssues which handles this case
			issues, err = database.GetBoardIssues(board.ID, sessionID, statusFilter)
			if err != nil {
				output.Error("%v", err)
				return err
			}
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			data, _ := json.MarshalIndent(issues, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("Board: %s (%s)\n", board.Name, board.ID)
		if board.Query != "" {
			fmt.Printf("Query: %s\n", board.Query)
		}
		fmt.Println()

		if len(issues) == 0 {
			output.Info("No issues on this board")
			return nil
		}

		for i, view := range issues {
			posIndicator := ""
			if view.HasPosition {
				posIndicator = fmt.Sprintf("[%d] ", view.Position)
			} else {
				posIndicator = fmt.Sprintf("(%d) ", i+1)
			}

			statusIcon := getStatusIcon(view.Issue.Status)
			fmt.Printf("%s%s %s %s [%s] %s\n",
				posIndicator,
				view.Issue.ID,
				statusIcon,
				view.Issue.Priority,
				view.Issue.Type,
				view.Issue.Title)
		}

		return nil
	},
}

var boardEditCmd = &cobra.Command{
	Use:   "edit <board>",
	Short: "Edit a board's name or query",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		ref := args[0]

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		board, err := database.ResolveBoardRef(ref)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Capture previous state for undo
		prevData, _ := json.Marshal(board)

		// Apply updates
		if name, _ := cmd.Flags().GetString("name"); name != "" {
			board.Name = name
		}
		if query, _ := cmd.Flags().GetString("query"); cmd.Flags().Changed("query") {
			board.Query = query
		}
		if viewMode, _ := cmd.Flags().GetString("view-mode"); cmd.Flags().Changed("view-mode") {
			if err := database.UpdateBoardViewMode(board.ID, viewMode); err != nil {
				output.Error("%v", err)
				return err
			}
			board.ViewMode = viewMode
		}

		if err := database.UpdateBoard(board); err != nil {
			output.Error("%v", err)
			return err
		}

		// Log action for undo
		newData, _ := json.Marshal(board)
		logActionWithSession(baseDir, database, &models.ActionLog{
			ActionType:   models.ActionBoardUpdate,
			EntityType:   "board",
			EntityID:     board.ID,
			PreviousData: string(prevData),
			NewData:      string(newData),
		})

		output.Success("Updated board %s", board.Name)
		return nil
	},
}

var boardMoveCmd = &cobra.Command{
	Use:   "move <board> <issue-id> <position>",
	Short: "Set an issue's position on a board",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		boardRef := args[0]
		issueID := args[1]
		posStr := args[2]

		position, err := strconv.Atoi(posStr)
		if err != nil || position < 1 {
			output.Error("Position must be a positive integer")
			return fmt.Errorf("invalid position: %s", posStr)
		}

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		board, err := database.ResolveBoardRef(boardRef)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Verify issue exists
		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Compute sparse sort key from visual slot
		sortKey, respaced, err := database.ComputeInsertPosition(board.ID, position)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Log respace events for sync if gap exhaustion triggered re-spacing
		if len(respaced) > 0 {
			for _, r := range respaced {
				bipID := db.BoardIssuePosID(board.ID, r.IssueID)
				rData, _ := json.Marshal(map[string]interface{}{
					"id":       bipID,
					"board_id": board.ID,
					"issue_id": r.IssueID,
					"position": r.NewPosition,
					"added_at": time.Now().Format(time.RFC3339),
				})
				logActionWithSession(baseDir, database, &models.ActionLog{
					ActionType: models.ActionBoardSetPosition,
					EntityType: "board_issue_positions",
					EntityID:   bipID,
					NewData:    string(rData),
				})
			}
		}

		if err := database.SetIssuePosition(board.ID, issue.ID, sortKey); err != nil {
			output.Error("%v", err)
			return err
		}

		// Log action for undo — full row data for sync
		bipID := db.BoardIssuePosID(board.ID, issue.ID)
		bipData, _ := json.Marshal(map[string]interface{}{
			"id":       bipID,
			"board_id": board.ID,
			"issue_id": issue.ID,
			"position": sortKey,
			"added_at": time.Now().Format(time.RFC3339),
		})
		logActionWithSession(baseDir, database, &models.ActionLog{
			ActionType: models.ActionBoardSetPosition,
			EntityType: "board_issue_positions",
			EntityID:   bipID,
			NewData:    string(bipData),
		})

		output.Success("Set %s to position %d on %s", issue.ID, position, board.Name)
		return nil
	},
}

var boardUnpositionCmd = &cobra.Command{
	Use:   "unposition <board> <issue-id>",
	Short: "Remove an issue's explicit position from a board",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		boardRef := args[0]
		issueID := args[1]

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		board, err := database.ResolveBoardRef(boardRef)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Verify issue exists
		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Capture current position before removal (for undo)
		oldPos, err := database.GetIssuePosition(board.ID, issue.ID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		if err := database.RemoveIssuePosition(board.ID, issue.ID); err != nil {
			output.Error("%v", err)
			return err
		}

		// Log action for undo — full row data for sync
		ubipID := db.BoardIssuePosID(board.ID, issue.ID)
		ubipData, _ := json.Marshal(map[string]any{
			"id":       ubipID,
			"board_id": board.ID,
			"issue_id": issue.ID,
			"position": oldPos,
		})
		logActionWithSession(baseDir, database, &models.ActionLog{
			ActionType: models.ActionBoardUnposition,
			EntityType: "board_issue_positions",
			EntityID:   ubipID,
			NewData:    string(ubipData),
		})

		output.Success("Removed explicit position for %s on %s", issue.ID, board.Name)
		return nil
	},
}

// logActionWithSession logs an action if a session exists, filling in the SessionID.
func logActionWithSession(_ string, database *db.DB, action *models.ActionLog) {
	sess, _ := session.GetOrCreate(database)
	if sess != nil {
		action.SessionID = sess.ID
		database.LogAction(action)
	}
}

func getStatusIcon(status models.Status) string {
	switch status {
	case models.StatusOpen:
		return "○"
	case models.StatusInProgress:
		return "◐"
	case models.StatusBlocked:
		return "◉"
	case models.StatusInReview:
		return "◑"
	case models.StatusClosed:
		return "●"
	default:
		return "○"
	}
}

func init() {
	rootCmd.AddCommand(boardCmd)
	boardCmd.AddCommand(boardListCmd)
	boardCmd.AddCommand(boardCreateCmd)
	boardCmd.AddCommand(boardDeleteCmd)
	boardCmd.AddCommand(boardShowCmd)
	boardCmd.AddCommand(boardEditCmd)
	boardCmd.AddCommand(boardMoveCmd)
	boardCmd.AddCommand(boardUnpositionCmd)

	// Flags
	boardListCmd.Flags().Bool("json", false, "Output as JSON")
	boardCreateCmd.Flags().StringP("query", "q", "", "TDQ query for the board")
	boardShowCmd.Flags().Bool("json", false, "Output as JSON")
	boardShowCmd.Flags().StringArrayP("status", "s", nil, "Filter by status")
	boardEditCmd.Flags().StringP("name", "n", "", "New name for the board")
	boardEditCmd.Flags().StringP("query", "q", "", "New query for the board")
	boardEditCmd.Flags().String("view-mode", "", "View mode: swimlanes or backlog")
}
