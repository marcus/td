package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/marcus/td/internal/semdiff"
	"github.com/spf13/cobra"
)

var (
	explainJSON   bool
	explainStaged bool
	explainStdin  bool

	// stdinReader is overridable in tests.
	stdinReader io.Reader = os.Stdin
)

var explainCmd = &cobra.Command{
	Use:   "explain [<rev>] [<rev>]",
	Short: "Explain the semantic meaning of code changes",
	Long: `Run a git diff (or read one from stdin) and print a human-readable
summary of what changed, grouped by file with semantic categories
(functions added/removed, signature changes, comment-only edits, tests,
imports, config, formatting-only, etc.).

Examples:
  td explain                       # diff HEAD vs working tree
  td explain --staged              # diff staged changes
  td explain main HEAD             # diff main..HEAD
  git diff | td explain --stdin    # explain a piped diff
  td explain --json                # machine-readable output`,
	Args: cobra.MaximumNArgs(2),
	RunE: runExplain,
}

func runExplain(cmd *cobra.Command, args []string) error {
	if explainStdin {
		return runExplainWithReader(cmd, args, stdinReader)
	}
	out, err := collectGitDiff(args, explainStaged)
	if err != nil {
		return err
	}
	return runExplainWithReader(cmd, args, bytes.NewReader(out))
}

func runExplainWithReader(cmd *cobra.Command, _ []string, r io.Reader) error {
	files, err := semdiff.Parse(r)
	if err != nil {
		return fmt.Errorf("parse diff: %w", err)
	}
	summary := semdiff.Classify(files)
	semdiff.SortFiles(summary.Files)
	if explainJSON {
		return semdiff.RenderJSON(cmd.OutOrStdout(), summary)
	}
	return semdiff.RenderText(cmd.OutOrStdout(), summary)
}

func collectGitDiff(args []string, staged bool) ([]byte, error) {
	cmdArgs := []string{"diff", "--no-color"}
	if staged {
		cmdArgs = append(cmdArgs, "--cached")
	}
	cmdArgs = append(cmdArgs, args...)

	gitCmd := exec.Command("git", cmdArgs...)
	var out, stderr bytes.Buffer
	gitCmd.Stdout = &out
	gitCmd.Stderr = &stderr
	if err := gitCmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git diff failed: %s", msg)
	}
	return out.Bytes(), nil
}

func init() {
	rootCmd.AddCommand(explainCmd)
	explainCmd.Flags().BoolVar(&explainJSON, "json", false, "Output machine-readable JSON")
	explainCmd.Flags().BoolVar(&explainStaged, "staged", false, "Diff staged changes (git diff --cached)")
	explainCmd.Flags().BoolVar(&explainStdin, "stdin", false, "Read unified diff from stdin instead of running git")
}
