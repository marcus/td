package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/topology"
	"github.com/spf13/cobra"
)

var topologyCmd = &cobra.Command{
	Use:     "topology",
	Aliases: []string{"topo"},
	Short:   "Visualize repository file/directory structure",
	Long: `Visualize the current git repository's file/directory structure as an ASCII tree.

Scans git-tracked files and builds a hierarchical tree. Optionally annotates
entries with git activity stats (commit count, last modified) and td issue
linkage (via issue_files table).

Examples:
  td topology                     # Show full repo tree
  td topology --depth 2           # Limit to 2 levels
  td topology --stats             # Show git stats per file
  td topology --filter "*.go"     # Only show Go files
  td topology --issues            # Annotate files linked to td issues
  td topology --json              # Output as JSON`,
	GroupID: "query",
	RunE: func(cmd *cobra.Command, args []string) error {
		rootDir, err := git.GetRootDir()
		if err != nil {
			output.Error("not a git repository")
			return fmt.Errorf("not a git repository")
		}

		maxDepth, _ := cmd.Flags().GetInt("depth")
		withStats, _ := cmd.Flags().GetBool("stats")
		filter, _ := cmd.Flags().GetString("filter")
		withIssues, _ := cmd.Flags().GetBool("issues")
		jsonOutput, _ := cmd.Flags().GetBool("json")

		tree, err := topology.Scan(topology.ScanOptions{
			RootDir:   rootDir,
			MaxDepth:  maxDepth,
			Filter:    filter,
			WithStats: withStats,
		})
		if err != nil {
			output.Error("scan failed: %v", err)
			return err
		}

		if withIssues {
			baseDir := getBaseDir()
			database, dbErr := db.Open(baseDir)
			if dbErr == nil {
				defer database.Close()
				fileIssues, mapErr := database.GetFileIssueMap()
				if mapErr == nil {
					topology.AnnotateIssues(tree, fileIssues)
				}
			}
		}

		if jsonOutput {
			return output.JSON(tree)
		}

		result := topology.Render(tree, topology.RenderOptions{
			ShowStats:  withStats,
			ShowIssues: withIssues,
		})
		fmt.Println(result)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(topologyCmd)

	topologyCmd.Flags().IntP("depth", "d", 0, "Max depth (0=unlimited)")
	topologyCmd.Flags().BoolP("stats", "s", false, "Show git stats per file (commit count, last modified)")
	topologyCmd.Flags().StringP("filter", "f", "", "Glob pattern to filter files (e.g. \"*.go\")")
	topologyCmd.Flags().BoolP("issues", "i", false, "Annotate files linked to td issues")
	topologyCmd.Flags().Bool("json", false, "Output as JSON")
}
