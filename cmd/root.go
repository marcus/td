package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	version string
	baseDir string
)

// SetVersion sets the version string
func SetVersion(v string) {
	version = v
}

var rootCmd = &cobra.Command{
	Use:   "td",
	Short: "Local task and session management CLI",
	Long: `td - A minimalist local task and session management CLI designed for AI-assisted development workflows.

Optimized for session continuity—capturing working state so new context windows can resume where previous ones stopped.`,
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
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
	return baseDir
}
