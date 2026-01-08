package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	versionStr      string
	baseDir         string
	baseDirOverride *string // For testing
	cmdStartTime    time.Time
)

// SetVersion sets the version string
func SetVersion(v string) {
	versionStr = v
}

var rootCmd = &cobra.Command{
	Use:   "td",
	Short: "Local task and session management CLI",
	Long: `td - A minimalist local task and session management CLI designed for AI-assisted development workflows.

Optimized for session continuity—capturing working state so new context windows can resume where previous ones stopped.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cmdStartTime = time.Now()
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if !db.AnalyticsEnabled() {
			return
		}
		dir := getBaseDir()
		if dir == "" {
			return
		}
		event := buildCommandEvent(cmd, nil)
		_ = db.LogCommandUsage(dir, event) // Sync for now to debug
	},
}

// Execute runs the root command
func Execute() {
	cmdStartTime = time.Now()
	if err := rootCmd.Execute(); err != nil {
		args := os.Args[1:]

		// Log agent error for analysis
		logAgentError(args, err.Error())

		// Log failed command analytics
		if db.AnalyticsEnabled() {
			dir := getBaseDir()
			if dir == "" {
				dir, _ = os.Getwd()
			}
			if dir != "" {
				event := buildCommandEvent(nil, err)
				if len(args) > 0 {
					event.Command = args[0]
				}
				_ = db.LogCommandUsage(dir, event)
			}
		}

		// Check if this is an unknown command that we can provide workflow hints for
		if len(args) > 0 && handleWorkflowHint(args[0]) {
			os.Exit(1)
		}
		// Print the error for non-workflow unknown commands
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// logAgentError logs a failed command for agent UX analysis
func logAgentError(args []string, errMsg string) {
	dir := getBaseDir()
	if dir == "" {
		// Fallback: get cwd directly (OnInitialize may not have run for unknown commands)
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return
		}
	}

	// Try to get session ID (may fail if not initialized)
	var sessionID string
	if sess, err := session.Get(dir); err == nil {
		sessionID = sess.ID
	}

	// Log the error (silently fails if project not initialized)
	db.LogAgentError(dir, args, errMsg, sessionID)
}

// handleWorkflowHint checks if the command is a common workflow alias and shows a hint
// Returns true if a hint was shown
func handleWorkflowHint(cmd string) bool {
	switch cmd {
	case "done", "complete", "submit":
		showWorkflowHint(cmd, "review",
			"Or use 'td close <id>' to close directly without review.")
		return true
	case "finish":
		showWorkflowHint(cmd, "close",
			"Use 'td close <id>' for direct close, or 'td review' → 'td approve' for reviewed completion.")
		return true
	}
	return false
}

// nameWithAliases returns "name, alias1, alias2" if aliases exist, else just "name"
func nameWithAliases(cmd *cobra.Command) string {
	if len(cmd.Aliases) > 0 {
		return cmd.Name() + ", " + strings.Join(cmd.Aliases, ", ")
	}
	return cmd.Name()
}

func init() {
	cobra.OnInitialize(initBaseDir)

	// Add custom template function for showing aliases
	cobra.AddTemplateFunc("nameWithAliases", nameWithAliases)

	// Custom usage template that shows aliases inline
	usageTemplate := `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad (nameWithAliases .) (add .NamePadding 8)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad (nameWithAliases .) (add .NamePadding 8)}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad (nameWithAliases .) (add .NamePadding 8)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

	// Need to add the 'add' function for padding calculation
	cobra.AddTemplateFunc("add", func(a, b int) int { return a + b })

	rootCmd.SetUsageTemplate(usageTemplate)

	// Define command groups for organized help output
	rootCmd.AddGroup(
		&cobra.Group{ID: "core", Title: "Core Commands:"},
		&cobra.Group{ID: "workflow", Title: "Workflow Commands:"},
		&cobra.Group{ID: "query", Title: "Query Commands:"},
		&cobra.Group{ID: "shortcuts", Title: "Shortcuts:"},
		&cobra.Group{ID: "session", Title: "Session Commands:"},
		&cobra.Group{ID: "files", Title: "File Commands:"},
		&cobra.Group{ID: "system", Title: "System Commands:"},
	)

	// Assign built-in commands to system group
	rootCmd.SetHelpCommandGroupID("system")
	rootCmd.SetCompletionCommandGroupID("system")

	// Don't print Cobra's default error message - we handle it ourselves
	rootCmd.SilenceErrors = true
}

func initBaseDir() {
	var err error
	baseDir, err = os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}
}

// getBaseDir returns the base directory for the project
func getBaseDir() string {
	if baseDirOverride != nil {
		return *baseDirOverride
	}
	return baseDir
}

// ValidateIssueID checks if an issue ID is valid (non-empty, non-whitespace)
// Returns an error with helpful usage info if invalid
func ValidateIssueID(id string, cmdUsage string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("issue ID required. Usage: td %s", cmdUsage)
	}
	return nil
}

// ValidateIssueIDs validates multiple issue IDs
func ValidateIssueIDs(ids []string, cmdUsage string) error {
	for _, id := range ids {
		if err := ValidateIssueID(id, cmdUsage); err != nil {
			return err
		}
	}
	return nil
}

// showWorkflowHint prints a helpful hint when a user tries an unknown workflow command
func showWorkflowHint(attempted, suggested, hint string) {
	fmt.Fprintf(os.Stderr, "\nUnknown command: '%s'\n\n", attempted)
	fmt.Fprintf(os.Stderr, "Did you mean: td %s <id>\n\n", suggested)
	fmt.Fprintf(os.Stderr, "td workflow:\n")
	fmt.Fprintf(os.Stderr, "  1. td start <id>     - Begin work\n")
	fmt.Fprintf(os.Stderr, "  2. td handoff <id>   - Capture state (required)\n")
	fmt.Fprintf(os.Stderr, "  3. td review <id>    - Submit for review\n")
	fmt.Fprintf(os.Stderr, "  4. td approve <id>   - Complete (different session)\n\n")
	fmt.Fprintf(os.Stderr, "%s\n\n", hint)
	fmt.Fprintf(os.Stderr, "Run 'td usage -q' for full reference.\n")
}

// buildCommandEvent creates a CommandUsageEvent from the current command state
func buildCommandEvent(cmd *cobra.Command, err error) db.CommandUsageEvent {
	event := db.CommandUsageEvent{
		Timestamp:  cmdStartTime,
		DurationMs: time.Since(cmdStartTime).Milliseconds(),
		Success:    err == nil,
	}

	if err != nil {
		event.Error = err.Error()
	}

	if cmd != nil {
		event.Command = cmd.Name()
		// Check for subcommand (parent is not "td")
		if cmd.Parent() != nil && cmd.Parent().Name() != "td" {
			event.Subcommand = cmd.Name()
			event.Command = cmd.Parent().Name()
		}
		event.Flags = extractFlags(cmd)
	}

	// Try to get session ID
	dir := getBaseDir()
	if dir != "" {
		if sess, err := session.Get(dir); err == nil {
			event.SessionID = sess.ID
		}
	}

	return event
}

// extractFlags extracts changed flags from a command and sanitizes them
func extractFlags(cmd *cobra.Command) map[string]string {
	flags := make(map[string]string)
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			flags[f.Name] = f.Value.String()
		}
	})
	return db.SanitizeFlags(flags)
}
