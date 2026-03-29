package cmd

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/dateparse"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:     "create [title]",
	Aliases: []string{"add", "new"},
	Short:   "Create a new issue",
	Long:    `Create a new issue with optional flags for type, priority, labels, and more.`,
	GroupID: "core",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Route "td new task Title" → td create --type task "Title"
		// When first arg is a known type and there are more args, treat it as --type
		if len(args) >= 2 {
			candidate := strings.ToLower(args[0])
			normalized := models.NormalizeType(candidate)
			if models.IsValidType(normalized) {
				typeFlag, _ := cmd.Flags().GetString("type")
				if typeFlag == "" {
					cmd.Flags().Set("type", string(normalized))
				}
				args = args[1:]
			}
		}

		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		// Get title from args or flag
		title, _ := cmd.Flags().GetString("title")
		if len(args) > 0 {
			title = args[0]
		}

		if title == "" {
			output.Error("title is required")
			return fmt.Errorf("title is required")
		}

		// Parse type prefix from title if --type not explicitly provided
		var extractedType models.Type
		typeFlag, _ := cmd.Flags().GetString("type")
		if typeFlag == "" {
			extractedType, title = parseTypeFromTitle(title)
		}

		// Validate title quality
		minLen, maxLen, _ := config.GetTitleLengthLimits(baseDir)
		if err := validateTitle(title, minLen, maxLen); err != nil {
			output.Error("%v", err)
			return err
		}

		// Build issue
		issue := &models.Issue{
			Title: title,
		}

		// Apply extracted type if no explicit --type
		if extractedType != "" {
			issue.Type = extractedType
		}

		// Type (supports "story" as alias for "feature")
		if t := typeFlag; t != "" {
			issue.Type = models.NormalizeType(t)
			if !models.IsValidType(issue.Type) {
				output.Error("invalid type: %s (valid: bug, feature, task, epic, chore)", t)
				return fmt.Errorf("invalid type: %s", t)
			}
		}

		// Priority (supports numeric: "1" as alias for "P1")
		if p, _ := cmd.Flags().GetString("priority"); p != "" {
			issue.Priority = models.NormalizePriority(p)
			if !models.IsValidPriority(issue.Priority) {
				output.Error("invalid priority: %s (valid: P0, P1, P2, P3, P4)", p)
				return fmt.Errorf("invalid priority: %s", p)
			}
		}

		// Points
		if pts, _ := cmd.Flags().GetInt("points"); pts > 0 {
			if !models.IsValidPoints(pts) {
				output.Error("invalid points: %d (must be Fibonacci: 1,2,3,5,8,13,21)", pts)
				return fmt.Errorf("invalid points")
			}
			issue.Points = pts
		}

		// Labels (support --labels, --label, --tags, --tag)
		// All label flags support repeated flags (-l a -l b) and comma-separated (-l "a,b")
		labelsArr, _ := cmd.Flags().GetStringArray("labels")
		if len(labelsArr) == 0 {
			if arr, _ := cmd.Flags().GetStringArray("label"); len(arr) > 0 {
				labelsArr = arr
			}
		}
		if len(labelsArr) == 0 {
			if arr, _ := cmd.Flags().GetStringArray("tags"); len(arr) > 0 {
				labelsArr = arr
			}
		}
		if len(labelsArr) == 0 {
			if arr, _ := cmd.Flags().GetStringArray("tag"); len(arr) > 0 {
				labelsArr = arr
			}
		}
		if len(labelsArr) > 0 {
			issue.Labels = mergeMultiValueFlag(labelsArr)
		}

		// Description (support --description, --desc, and --body)
		issue.Description, _ = cmd.Flags().GetString("description")
		if issue.Description == "" {
			if desc, _ := cmd.Flags().GetString("desc"); desc != "" {
				issue.Description = desc
			}
		}
		if issue.Description == "" {
			if body, _ := cmd.Flags().GetString("body"); body != "" {
				issue.Description = body
			}
		}
		if issue.Description == "" {
			if notes, _ := cmd.Flags().GetString("notes"); notes != "" {
				issue.Description = notes
			}
		}

		// Acceptance
		issue.Acceptance, _ = cmd.Flags().GetString("acceptance")

		// Parent (supports --parent and --epic)
		issue.ParentID, _ = cmd.Flags().GetString("parent")
		if issue.ParentID == "" {
			if epic, _ := cmd.Flags().GetString("epic"); epic != "" {
				issue.ParentID = epic
			}
		}

		// Minor (allows self-review)
		issue.Minor, _ = cmd.Flags().GetBool("minor")

		// Defer date
		if deferStr, _ := cmd.Flags().GetString("defer"); deferStr != "" {
			parsed, err := dateparse.ParseDate(deferStr)
			if err != nil {
				output.Error("invalid defer date: %v", err)
				return fmt.Errorf("invalid defer date: %v", err)
			}
			issue.DeferUntil = &parsed
		}

		// Due date
		if dueStr, _ := cmd.Flags().GetString("due"); dueStr != "" {
			parsed, err := dateparse.ParseDate(dueStr)
			if err != nil {
				output.Error("invalid due date: %v", err)
				return fmt.Errorf("invalid due date: %v", err)
			}
			issue.DueDate = &parsed
		}

		// Get session BEFORE creating issue (needed for CreatorSession)
		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("failed to create session: %v", err)
			return fmt.Errorf("failed to create session: %w", err)
		}
		issue.CreatorSession = sess.ID

		// Capture current git branch
		gitState, _ := git.GetState()
		if gitState != nil {
			issue.CreatedBranch = gitState.Branch
		}

		// Create the issue (atomic create + action log)
		if err := database.CreateIssueLogged(issue, sess.ID); err != nil {
			output.Error("failed to create issue: %v", err)
			return err
		}

		// Record session action for bypass prevention
		if err := database.RecordSessionAction(issue.ID, sess.ID, models.ActionSessionCreated); err != nil {
			output.Warning("failed to record session history: %v", err)
		}

		// Handle dependencies (support repeated flags and comma-separated)
		if dependsArr, _ := cmd.Flags().GetStringArray("depends-on"); len(dependsArr) > 0 {
			for _, dep := range mergeMultiValueFlag(dependsArr) {
				if err := database.AddDependencyLogged(issue.ID, dep, "depends_on", sess.ID); err != nil {
					output.Warning("failed to add dependency %s: %v", dep, err)
				}
			}
		}

		if blocksArr, _ := cmd.Flags().GetStringArray("blocks"); len(blocksArr) > 0 {
			for _, blocked := range mergeMultiValueFlag(blocksArr) {
				if err := database.AddDependencyLogged(blocked, issue.ID, "depends_on", sess.ID); err != nil {
					output.Warning("failed to add blocks %s: %v", blocked, err)
				}
			}
		}

		fmt.Printf("CREATED %s\n", issue.ID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(createCmd)

	createCmd.Flags().String("title", "", "Issue title (max 200 characters)")
	createCmd.Flags().StringP("type", "t", "", "Issue type (bug, feature, task, epic, chore)")
	createCmd.Flags().StringP("priority", "p", "", "Priority (P0, P1, P2, P3, P4)")
	createCmd.Flags().Int("points", 0, "Story points (Fibonacci: 1,2,3,5,8,13,21)")
	createCmd.Flags().StringArrayP("labels", "l", nil, "Labels (repeatable, comma-separated)")
	createCmd.Flags().StringArray("label", nil, "Alias for --labels")
	createCmd.Flags().StringArray("tags", nil, "Alias for --labels")
	createCmd.Flags().StringArray("tag", nil, "Alias for --labels")
	createCmd.Flags().StringP("description", "d", "", "Description text")
	createCmd.Flags().String("desc", "", "Alias for --description")
	createCmd.Flags().String("body", "", "Alias for --description")
	createCmd.Flags().String("notes", "", "Alias for --description")
	createCmd.Flags().String("acceptance", "", "Acceptance criteria")
	createCmd.Flags().String("parent", "", "Parent issue ID")
	createCmd.Flags().String("epic", "", "Parent issue ID (alias for --parent)")
	createCmd.Flags().StringArray("depends-on", nil, "Issues this depends on (repeatable, comma-separated)")
	createCmd.Flags().StringArray("blocks", nil, "Issues this blocks (repeatable, comma-separated)")
	createCmd.Flags().Bool("minor", false, "Mark as minor task (allows self-review)")
	createCmd.Flags().String("defer", "", "Defer until date (e.g., +7d, monday, 2026-03-01)")
	createCmd.Flags().String("due", "", "Due date (e.g., friday, +2w, 2026-03-15)")
}

// parseTypeFromTitle extracts type prefix from title (e.g., "epic: Title" → "epic", "Title")
// Returns the extracted type (or empty Type) and the cleaned title
func parseTypeFromTitle(title string) (models.Type, string) {
	// Check for "type: title" pattern
	if idx := strings.Index(title, ":"); idx > 0 && idx < len(title)-1 {
		prefix := strings.TrimSpace(title[:idx])
		prefixLower := strings.ToLower(prefix)

		// Only extract if prefix is a valid type
		normalizedType := models.NormalizeType(prefixLower)
		if models.IsValidType(normalizedType) {
			rest := strings.TrimSpace(title[idx+1:])
			if rest != "" {
				return normalizedType, rest
			}
		}
	}
	return "", title
}

// mergeMultiValueFlag takes a string array from a repeated flag, splits each
// element on commas, trims whitespace, and deduplicates. This allows both
// `-l "a,b"` and `-l a -l b` (and mixed: `-l "a,b" -l c`) to work.
func mergeMultiValueFlag(values []string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" && !seen[part] {
				seen[part] = true
				result = append(result, part)
			}
		}
	}
	return result
}

// validateTitle checks that the title is descriptive enough
func validateTitle(title string, minLength, maxLength int) error {
	// Generic titles that should be rejected (case-insensitive)
	genericTitles := []string{
		"task", "issue", "bug", "feature", "fix", "update", "change",
		"todo", "work", "item", "thing", "stuff", "test", "new", "add",
	}

	trimmed := strings.TrimSpace(title)
	lower := strings.ToLower(trimmed)

	// Check for exact match with generic titles
	for _, generic := range genericTitles {
		if lower == generic {
			return fmt.Errorf("title '%s' is too generic - describe what it does or fixes", title)
		}
	}

	// Check length using rune count (correct for unicode)
	// Use trimmed length to prevent whitespace padding exploit
	runeCount := utf8.RuneCountInString(trimmed)
	if runeCount < minLength {
		return fmt.Errorf("title too short (%d chars, need %d) - e.g. 'Fix login timeout' not 'Fix bug'", runeCount, minLength)
	}
	if runeCount > maxLength {
		return fmt.Errorf("title too long (%d chars, max %d) - move details to description", runeCount, maxLength)
	}

	return nil
}
