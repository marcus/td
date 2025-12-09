package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var treeCmd = &cobra.Command{
	Use:     "tree [issue-id]",
	Short:   "Visualize parent/child relationships",
	GroupID: "query",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		issueID := args[0]
		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		maxDepth, _ := cmd.Flags().GetInt("depth")
		jsonOutput, _ := cmd.Flags().GetBool("json")

		if jsonOutput {
			tree := buildTree(database, issueID, 0, maxDepth)
			return output.JSON(tree)
		}

		// Print root
		fmt.Printf("%s %s: %s\n", issue.Type, issue.ID, issue.Title)

		// Print children
		printTree(database, issueID, 0, maxDepth)

		return nil
	},
}

func printTree(database *db.DB, parentID string, depth int, maxDepth int) {
	if maxDepth > 0 && depth >= maxDepth {
		return
	}

	// Get children
	children, _ := database.ListIssues(db.ListIssuesOptions{
		ParentID: parentID,
	})

	for i, child := range children {
		prefix := ""
		for j := 0; j < depth; j++ {
			prefix += "│   "
		}

		isLast := i == len(children)-1
		connector := "├── "
		if isLast {
			connector = "└── "
		}

		statusMark := ""
		switch child.Status {
		case models.StatusClosed:
			statusMark = " ✓"
		case models.StatusInReview:
			statusMark = " ⧗"
		case models.StatusInProgress:
			statusMark = " ●"
		case models.StatusBlocked:
			statusMark = " ✗"
		}

		fmt.Printf("%s%s%s %s: %s %s%s\n",
			prefix, connector, child.Type, child.ID, child.Title,
			output.FormatStatus(child.Status), statusMark)

		// Recurse
		newPrefix := prefix
		if isLast {
			newPrefix += "    "
		} else {
			newPrefix += "│   "
		}
		_ = newPrefix // Would be used for proper tree formatting

		printTree(database, child.ID, depth+1, maxDepth)
	}
}

func buildTree(database *db.DB, issueID string, depth int, maxDepth int) map[string]interface{} {
	issue, err := database.GetIssue(issueID)
	if err != nil {
		return nil
	}

	node := map[string]interface{}{
		"id":       issue.ID,
		"title":    issue.Title,
		"type":     issue.Type,
		"status":   issue.Status,
		"priority": issue.Priority,
	}

	if maxDepth > 0 && depth >= maxDepth {
		return node
	}

	children, _ := database.ListIssues(db.ListIssuesOptions{
		ParentID: issueID,
	})

	if len(children) > 0 {
		childNodes := make([]map[string]interface{}, 0)
		for _, child := range children {
			childNode := buildTree(database, child.ID, depth+1, maxDepth)
			if childNode != nil {
				childNodes = append(childNodes, childNode)
			}
		}
		node["children"] = childNodes
	}

	return node
}

var commentCmd = &cobra.Command{
	Use:     "comment [issue-id] \"text\"",
	Short:   "Add a comment to an issue (alias for 'comments add')",
	GroupID: "workflow",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.Get(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		issueID := args[0]
		text := args[1]

		// Verify issue exists
		_, err = database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		comment := &models.Comment{
			IssueID:   issueID,
			SessionID: sess.ID,
			Text:      text,
		}

		if err := database.AddComment(comment); err != nil {
			output.Error("failed to add comment: %v", err)
			return err
		}

		fmt.Printf("COMMENT ADDED %s\n", issueID)
		return nil
	},
}

var commentsCmd = &cobra.Command{
	Use:     "comments [issue-id]",
	Short:   "List comments for an issue",
	GroupID: "workflow",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		issueID := args[0]

		// Verify issue exists
		_, err = database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		comments, err := database.GetComments(issueID)
		if err != nil {
			output.Error("failed to get comments: %v", err)
			return err
		}

		for _, c := range comments {
			fmt.Printf("[%s] (%s) %s\n", c.CreatedAt.Format("2006-01-02 15:04"), c.SessionID, c.Text)
		}

		if len(comments) == 0 {
			fmt.Println("No comments")
		}

		return nil
	},
}

var commentsAddCmd = &cobra.Command{
	Use:   "add [issue-id] \"text\"",
	Short: "Add a comment to an issue",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.Get(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		issueID := args[0]
		text := args[1]

		// Verify issue exists
		_, err = database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		comment := &models.Comment{
			IssueID:   issueID,
			SessionID: sess.ID,
			Text:      text,
		}

		if err := database.AddComment(comment); err != nil {
			output.Error("failed to add comment: %v", err)
			return err
		}

		fmt.Printf("COMMENT ADDED %s\n", issueID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(treeCmd)
	rootCmd.AddCommand(commentCmd)
	rootCmd.AddCommand(commentsCmd)

	commentsCmd.AddCommand(commentsAddCmd)

	treeCmd.Flags().Int("depth", 0, "Max depth (0=unlimited)")
	treeCmd.Flags().Bool("json", false, "JSON output")
}
