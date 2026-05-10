package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/diff"
	"github.com/marcus/td/internal/git"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:     "diff",
	Short:   "Explain the semantic meaning of code changes",
	Long:    `Parses git diffs and uses Go AST analysis to identify symbol-level changes (functions, types, methods, imports added/removed/modified).`,
	GroupID: "query",
	RunE:    runDiff,
}

var (
	diffStaged bool
	diffRef    string
	diffIssue  string
	diffJSON   bool
)

func init() {
	diffCmd.Flags().BoolVar(&diffStaged, "staged", false, "Analyze staged changes")
	diffCmd.Flags().StringVar(&diffRef, "ref", "HEAD~1", "Git ref to diff against (e.g. HEAD~3)")
	diffCmd.Flags().StringVar(&diffIssue, "issue", "", "Diff since issue start snapshot")
	diffCmd.Flags().BoolVar(&diffJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	ref := diffRef

	// If --issue is set, resolve the start snapshot SHA
	if diffIssue != "" {
		database, err := db.Open(getBaseDir())
		if err != nil {
			return fmt.Errorf("opening database: %w", err)
		}
		defer database.Close()

		snapshot, err := database.GetStartSnapshot(diffIssue)
		if err != nil {
			return fmt.Errorf("getting start snapshot for issue %s: %w", diffIssue, err)
		}
		if snapshot == nil {
			return fmt.Errorf("no start snapshot found for issue %s", diffIssue)
		}
		ref = snapshot.CommitSHA
	}

	// Get diff content
	var rawDiff string
	var err error

	if diffStaged {
		rawDiff, err = git.GetDiffContentStaged()
	} else {
		rawDiff, err = git.GetDiffContent(ref)
	}
	if err != nil {
		return fmt.Errorf("getting diff: %w", err)
	}

	if rawDiff == "" {
		fmt.Fprintln(os.Stderr, "No changes found.")
		return nil
	}

	// Parse and analyze
	parsed := diff.Parse(rawDiff)
	summaries := diff.Summarize(parsed, ref, diffStaged)

	if diffJSON {
		return json.NewEncoder(os.Stdout).Encode(summaries)
	}

	printSummaries(summaries)
	return nil
}

func printSummaries(summaries []diff.FileSummary) {
	for _, s := range summaries {
		statusIcon := statusToIcon(s.Status)
		fmt.Printf("%s %s", statusIcon, s.Path)
		if s.Language != "" {
			fmt.Printf("  [%s]", s.Language)
		}
		fmt.Println()

		if len(s.Changes) > 0 {
			grouped := make(map[diff.ChangeCategory][]diff.Change)
			for _, c := range s.Changes {
				grouped[c.Category] = append(grouped[c.Category], c)
			}

			var cats []diff.ChangeCategory
			for cat := range grouped {
				cats = append(cats, cat)
			}
			sort.Slice(cats, func(i, j int) bool {
				return string(cats[i]) < string(cats[j])
			})

			for _, cat := range cats {
				changes := grouped[cat]
				for _, c := range changes {
					kindIcon := changeKindIcon(c.Kind)
					line := fmt.Sprintf("    %s %s: %s", kindIcon, cat, c.Symbol)
					if c.Detail != "" {
						line += "  — " + c.Detail
					}
					fmt.Println(line)
				}
			}
		} else if s.Category != "" {
			fmt.Printf("    %s (%s)\n", s.Status, s.Category)
		}

		fmt.Println()
	}
}

func statusToIcon(status string) string {
	switch status {
	case "added":
		return "+"
	case "deleted":
		return "-"
	case "renamed":
		return "~"
	default:
		return "*"
	}
}

func changeKindIcon(kind diff.ChangeKind) string {
	switch kind {
	case diff.ChangeAdded:
		return "+"
	case diff.ChangeRemoved:
		return "-"
	case diff.ChangeModified:
		return "~"
	default:
		return " "
	}
}

// DiffFlags exposes flag values for testing.
type DiffFlags struct {
	Staged bool
	Ref    string
	Issue  string
	JSON   bool
}

// GetDiffFlags returns the current diff flag values (for testing).
func GetDiffFlags() DiffFlags {
	return DiffFlags{
		Staged: diffStaged,
		Ref:    diffRef,
		Issue:  diffIssue,
		JSON:   diffJSON,
	}
}

// ResetDiffFlags resets diff flags to defaults (for testing).
func ResetDiffFlags() {
	diffStaged = false
	diffRef = "HEAD~1"
	diffIssue = ""
	diffJSON = false
}

// ParseDiffOutput is exported for testing: parses and summarizes raw diff text.
func ParseDiffOutput(rawDiff, ref string, staged bool) []diff.FileSummary {
	parsed := diff.Parse(rawDiff)
	return diff.Summarize(parsed, ref, staged)
}
