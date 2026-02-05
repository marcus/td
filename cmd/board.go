package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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

		queryStr, _ := cmd.Flags().GetString("query")

		sess, _ := session.GetOrCreate(database)
		sessionID := ""
		if sess != nil {
			sessionID = sess.ID
		}

		board, err := database.CreateBoardLogged(name, queryStr, sessionID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

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

		sess, _ := session.GetOrCreate(database)
		sessionID := ""
		if sess != nil {
			sessionID = sess.ID
		}

		if err := database.DeleteBoardLogged(board.ID, sessionID); err != nil {
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

		// Apply updates
		if name, _ := cmd.Flags().GetString("name"); name != "" {
			board.Name = name
		}
		if queryStr, _ := cmd.Flags().GetString("query"); cmd.Flags().Changed("query") {
			board.Query = queryStr
		}
		if viewMode, _ := cmd.Flags().GetString("view-mode"); cmd.Flags().Changed("view-mode") {
			if err := database.UpdateBoardViewMode(board.ID, viewMode); err != nil {
				output.Error("%v", err)
				return err
			}
			board.ViewMode = viewMode
		}

		sess, _ := session.GetOrCreate(database)
		sessionID := ""
		if sess != nil {
			sessionID = sess.ID
		}

		if err := database.UpdateBoardLogged(board, sessionID); err != nil {
			output.Error("%v", err)
			return err
		}

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

		sess, _ := session.GetOrCreate(database)
		sessionID := ""
		if sess != nil {
			sessionID = sess.ID
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
				if err := database.SetIssuePositionLogged(board.ID, r.IssueID, r.NewPosition, sessionID); err != nil {
					output.Error("respace log: %v", err)
					return err
				}
			}
		}

		if err := database.SetIssuePositionLogged(board.ID, issue.ID, sortKey, sessionID); err != nil {
			output.Error("%v", err)
			return err
		}

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

		sess, _ := session.GetOrCreate(database)
		sessionID := ""
		if sess != nil {
			sessionID = sess.ID
		}

		// Verify issue exists
		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		if err := database.RemoveIssuePositionLogged(board.ID, issue.ID, sessionID); err != nil {
			output.Error("%v", err)
			return err
		}

		output.Success("Removed explicit position for %s on %s", issue.ID, board.Name)
		return nil
	},
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
