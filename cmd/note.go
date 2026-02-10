package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var noteCmd = &cobra.Command{
	Use:     "note",
	Short:   "Manage freeform notes",
	Long:    `Create, list, view, edit, and manage notes.`,
	GroupID: "core",
}

var noteAddCmd = &cobra.Command{
	Use:   "add <title>",
	Short: "Create a new note",
	Long: `Create a new note with a title and optional content.

Examples:
  td note add "Architecture decisions"
  td note add "Meeting notes" --content "Discussed API design"
  td note add "Design doc"                # opens editor for content`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		title := args[0]
		content, _ := cmd.Flags().GetString("content")

		// If no content flag, open editor
		if !cmd.Flags().Changed("content") {
			edited, err := openEditorForContent("")
			if err != nil {
				output.Error("editor failed: %v", err)
				return err
			}
			content = edited
		}

		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		note, err := database.CreateNote(title, content)
		if err != nil {
			output.Error("failed to create note: %v", err)
			return err
		}

		fmt.Printf("CREATED %s %s\n", note.ID, note.Title)
		return nil
	},
}

var noteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List notes",
	Long: `List notes with optional filters.

Examples:
  td note list                  # list non-archived notes
  td note list --pinned         # show only pinned notes
  td note list --archived       # show only archived notes
  td note list --all            # include archived notes
  td note list --search "api"   # search by title/content`,
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		opts := db.ListNotesOptions{}
		opts.Limit, _ = cmd.Flags().GetInt("limit")
		opts.Search, _ = cmd.Flags().GetString("search")

		showAll, _ := cmd.Flags().GetBool("all")

		if pinned, _ := cmd.Flags().GetBool("pinned"); pinned {
			b := true
			opts.Pinned = &b
		}

		if archived, _ := cmd.Flags().GetBool("archived"); archived {
			b := true
			opts.Archived = &b
		} else if !showAll {
			// Default: exclude archived
			b := false
			opts.Archived = &b
		}

		notes, err := database.ListNotes(opts)
		if err != nil {
			output.Error("failed to list notes: %v", err)
			return err
		}

		// JSON output
		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
			return output.JSON(notes)
		}
		if format, _ := cmd.Flags().GetString("output"); format == "json" {
			return output.JSON(notes)
		}

		if len(notes) == 0 {
			fmt.Println("No notes found")
			return nil
		}

		// Table output
		for _, n := range notes {
			pin := " "
			if n.Pinned {
				pin = "*"
			}
			title := n.Title
			if len(title) > 50 {
				title = title[:47] + "..."
			}
			fmt.Printf("%s %s  %-50s  %s  %s\n",
				pin, n.ID, title,
				output.FormatTimeAgo(n.CreatedAt),
				output.FormatTimeAgo(n.UpdatedAt))
		}
		return nil
	},
}

var noteShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Display a note",
	Long: `Display full details of a note.

Examples:
  td note show nt-abc123
  td note show nt-abc123 --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		note, err := database.GetNote(args[0])
		if err != nil {
			output.Error("%v", err)
			return err
		}

		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
			return output.JSON(note)
		}

		// Display note
		fmt.Printf("%s: %s\n", note.ID, note.Title)
		if note.Pinned {
			fmt.Println("Pinned: yes")
		}
		if note.Archived {
			fmt.Println("Archived: yes")
		}
		fmt.Printf("Created: %s\n", output.FormatTimeAgo(note.CreatedAt))
		fmt.Printf("Updated: %s\n", output.FormatTimeAgo(note.UpdatedAt))
		if note.Content != "" {
			fmt.Printf("\n%s\n", note.Content)
		}
		return nil
	},
}

var noteEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit a note",
	Long: `Edit a note's title or content.

Examples:
  td note edit nt-abc123 --title "New title"
  td note edit nt-abc123 --content "Updated content"
  td note edit nt-abc123                              # opens editor`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		note, err := database.GetNote(args[0])
		if err != nil {
			output.Error("%v", err)
			return err
		}

		newTitle := note.Title
		newContent := note.Content

		if cmd.Flags().Changed("title") {
			newTitle, _ = cmd.Flags().GetString("title")
		}
		if cmd.Flags().Changed("content") {
			newContent, _ = cmd.Flags().GetString("content")
		}

		// If neither flag given, open editor with current content
		if !cmd.Flags().Changed("title") && !cmd.Flags().Changed("content") {
			edited, err := openEditorForContent(note.Content)
			if err != nil {
				output.Error("editor failed: %v", err)
				return err
			}
			newContent = edited
		}

		_, err = database.UpdateNote(note.ID, newTitle, newContent)
		if err != nil {
			output.Error("failed to update note: %v", err)
			return err
		}

		fmt.Printf("UPDATED %s\n", note.ID)
		return nil
	},
}

var noteDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a note (soft-delete)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		if err := database.DeleteNote(args[0]); err != nil {
			output.Error("%v", err)
			return err
		}

		fmt.Printf("DELETED %s\n", args[0])
		return nil
	},
}

var notePinCmd = &cobra.Command{
	Use:   "pin <id>",
	Short: "Pin a note",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		if err := database.PinNote(args[0]); err != nil {
			output.Error("%v", err)
			return err
		}

		fmt.Printf("PINNED %s\n", args[0])
		return nil
	},
}

var noteUnpinCmd = &cobra.Command{
	Use:   "unpin <id>",
	Short: "Unpin a note",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		if err := database.UnpinNote(args[0]); err != nil {
			output.Error("%v", err)
			return err
		}

		fmt.Printf("UNPINNED %s\n", args[0])
		return nil
	},
}

var noteArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Archive a note",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		if err := database.ArchiveNote(args[0]); err != nil {
			output.Error("%v", err)
			return err
		}

		fmt.Printf("ARCHIVED %s\n", args[0])
		return nil
	},
}

var noteUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <id>",
	Short: "Unarchive a note",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		if err := database.UnarchiveNote(args[0]); err != nil {
			output.Error("%v", err)
			return err
		}

		fmt.Printf("UNARCHIVED %s\n", args[0])
		return nil
	},
}

// openEditorForContent opens the user's default editor with the given initial
// content and returns the edited result. Uses $EDITOR or falls back to "vi".
func openEditorForContent(initial string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	tmpFile, err := os.CreateTemp("", "td-note-*.md")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if initial != "" {
		if _, err := tmpFile.WriteString(initial); err != nil {
			tmpFile.Close()
			return "", fmt.Errorf("write temp file: %w", err)
		}
	}
	tmpFile.Close()

	// Split editor command in case it includes args (e.g. "code --wait")
	parts := strings.Fields(editor)
	cmdArgs := append(parts[1:], tmpFile.Name())
	editorCmd := exec.Command(parts[0], cmdArgs...)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return "", fmt.Errorf("read edited file: %w", err)
	}

	return strings.TrimRight(string(data), "\n"), nil
}

func init() {
	rootCmd.AddCommand(noteCmd)
	noteCmd.AddCommand(noteAddCmd)
	noteCmd.AddCommand(noteListCmd)
	noteCmd.AddCommand(noteShowCmd)
	noteCmd.AddCommand(noteEditCmd)
	noteCmd.AddCommand(noteDeleteCmd)
	noteCmd.AddCommand(notePinCmd)
	noteCmd.AddCommand(noteUnpinCmd)
	noteCmd.AddCommand(noteArchiveCmd)
	noteCmd.AddCommand(noteUnarchiveCmd)

	// noteAddCmd flags
	noteAddCmd.Flags().StringP("content", "c", "", "Note content (opens editor if omitted)")

	// noteListCmd flags
	noteListCmd.Flags().Bool("pinned", false, "Show only pinned notes")
	noteListCmd.Flags().Bool("archived", false, "Show only archived notes")
	noteListCmd.Flags().BoolP("all", "a", false, "Include archived notes")
	noteListCmd.Flags().StringP("search", "s", "", "Search title/content")
	noteListCmd.Flags().IntP("limit", "n", 50, "Max results")
	noteListCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")
	noteListCmd.Flags().Bool("json", false, "JSON output")

	// noteShowCmd flags
	noteShowCmd.Flags().Bool("json", false, "JSON output")

	// noteEditCmd flags
	noteEditCmd.Flags().StringP("title", "t", "", "New title")
	noteEditCmd.Flags().StringP("content", "c", "", "New content")
}
