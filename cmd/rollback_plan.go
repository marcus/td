package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var (
	rollbackBase   string
	rollbackFormat string
	rollbackOutput string
	rollbackSince  string
)

// CommitInfo describes a single commit in the rollback plan.
type CommitInfo struct {
	SHA     string   `json:"sha"`
	Short   string   `json:"short"`
	Author  string   `json:"author"`
	Date    string   `json:"date"`
	Subject string   `json:"subject"`
	IsMerge bool     `json:"is_merge"`
	Files   []string `json:"files"`
}

// RollbackPlan is the structured output produced by the command.
type RollbackPlan struct {
	Base           string       `json:"base"`
	Head           string       `json:"head"`
	Since          string       `json:"since,omitempty"`
	Commits        []CommitInfo `json:"commits"`
	FilesTouched   []string     `json:"files_touched"`
	RevertCommands []string     `json:"revert_commands"`
	Notes          []string     `json:"notes"`
}

var rollbackPlanCmd = &cobra.Command{
	Use:   "rollback-plan",
	Short: "Generate a rollback plan from git history",
	Long: `Analyze git history between HEAD and a base branch to produce a structured
rollback plan including commits to revert, files touched, suggested git
commands, and migration/config considerations.

Output formats: text (default), json, markdown.`,
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		plan, err := buildRollbackPlan(rollbackBase, rollbackSince)
		if err != nil {
			return err
		}

		out, err := renderRollbackPlan(plan, rollbackFormat)
		if err != nil {
			return err
		}

		if rollbackOutput != "" {
			if err := os.WriteFile(rollbackOutput, []byte(out), 0644); err != nil {
				return fmt.Errorf("writing output: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote rollback plan to %s\n", rollbackOutput)
			return nil
		}

		fmt.Fprint(cmd.OutOrStdout(), out)
		return nil
	},
}

func init() {
	rollbackPlanCmd.Flags().StringVar(&rollbackBase, "base", "main", "base branch to compare against")
	rollbackPlanCmd.Flags().StringVarP(&rollbackFormat, "format", "f", "text", "output format: text, json, markdown")
	rollbackPlanCmd.Flags().StringVarP(&rollbackOutput, "output", "o", "", "write output to file instead of stdout")
	rollbackPlanCmd.Flags().StringVar(&rollbackSince, "since", "", "commit/tag to use as the lower bound (overrides --base)")
	rootCmd.AddCommand(rollbackPlanCmd)
}

// gitRunner allows tests to inject a fake git executor.
var gitRunner = runGitForRollback

func runGitForRollback(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func buildRollbackPlan(base, since string) (*RollbackPlan, error) {
	lower := base
	if since != "" {
		lower = since
	}

	headOut, err := gitRunner("rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	head := strings.TrimSpace(headOut)

	rangeSpec := fmt.Sprintf("%s..HEAD", lower)
	// Format fields separated by NUL; record separator is \x1e.
	const sep = "\x1f"
	const recSep = "\x1e"
	format := strings.Join([]string{"%H", "%h", "%an", "%aI", "%s", "%P"}, sep)
	logOut, err := gitRunner("log", "--no-color", "--pretty=format:"+format+recSep, rangeSpec)
	if err != nil {
		return nil, err
	}

	plan := &RollbackPlan{
		Base:           base,
		Head:           head,
		Since:          since,
		Commits:        []CommitInfo{},
		FilesTouched:   []string{},
		RevertCommands: []string{},
		Notes:          []string{},
	}

	commits, err := parseCommitLog(logOut, sep, recSep)
	if err != nil {
		return nil, err
	}

	fileSet := map[string]struct{}{}
	for i := range commits {
		c := &commits[i]
		files, err := commitFiles(c.SHA)
		if err != nil {
			return nil, err
		}
		c.Files = files
		for _, f := range files {
			fileSet[f] = struct{}{}
		}
		if !c.IsMerge {
			plan.RevertCommands = append(plan.RevertCommands, fmt.Sprintf("git revert --no-edit %s", c.Short))
		} else {
			plan.RevertCommands = append(plan.RevertCommands, fmt.Sprintf("git revert -m 1 --no-edit %s", c.Short))
		}
	}
	plan.Commits = commits

	for f := range fileSet {
		plan.FilesTouched = append(plan.FilesTouched, f)
	}
	sort.Strings(plan.FilesTouched)

	plan.Notes = deriveNotes(plan.FilesTouched, commits)
	return plan, nil
}

func parseCommitLog(raw, sep, recSep string) ([]CommitInfo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []CommitInfo{}, nil
	}
	records := strings.Split(raw, recSep)
	commits := make([]CommitInfo, 0, len(records))
	for _, rec := range records {
		rec = strings.TrimLeft(rec, "\n")
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, sep, 6)
		if len(parts) < 6 {
			return nil, fmt.Errorf("unexpected git log record: %q", rec)
		}
		parents := strings.Fields(parts[5])
		commits = append(commits, CommitInfo{
			SHA:     parts[0],
			Short:   parts[1],
			Author:  parts[2],
			Date:    parts[3],
			Subject: parts[4],
			IsMerge: len(parents) > 1,
		})
	}
	return commits, nil
}

func commitFiles(sha string) ([]string, error) {
	out, err := gitRunner("show", "--name-only", "--pretty=format:", sha)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, line)
	}
	sort.Strings(files)
	return files, nil
}

func deriveNotes(files []string, commits []CommitInfo) []string {
	var notes []string
	for _, f := range files {
		lower := strings.ToLower(f)
		switch {
		case strings.Contains(lower, "migration") || strings.Contains(lower, "schema.sql") || strings.HasSuffix(lower, ".sql"):
			notes = append(notes, fmt.Sprintf("Database migration touched (%s): reverting code may not undo schema changes — plan a down-migration.", f))
		case strings.HasSuffix(lower, "go.mod") || strings.HasSuffix(lower, "go.sum") || strings.HasSuffix(lower, "package.json") || strings.HasSuffix(lower, "package-lock.json"):
			notes = append(notes, fmt.Sprintf("Dependency manifest touched (%s): re-run dependency install after revert.", f))
		case strings.Contains(lower, "config") && (strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".toml")):
			notes = append(notes, fmt.Sprintf("Config file touched (%s): verify deployed configuration is rolled back in lockstep.", f))
		}
	}
	for _, c := range commits {
		if c.IsMerge {
			notes = append(notes, fmt.Sprintf("Commit %s is a merge — revert with `git revert -m 1`.", c.Short))
		}
	}
	if len(commits) == 0 {
		notes = append(notes, "No commits found between base and HEAD — nothing to roll back.")
	}
	return notes
}

func renderRollbackPlan(plan *RollbackPlan, format string) (string, error) {
	switch strings.ToLower(format) {
	case "", "text":
		return renderText(plan), nil
	case "json":
		b, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b) + "\n", nil
	case "markdown", "md":
		return renderMarkdown(plan), nil
	default:
		return "", fmt.Errorf("unknown format %q (expected text, json, or markdown)", format)
	}
}

func renderText(plan *RollbackPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Rollback plan: %s..HEAD\n", planLowerBound(plan))
	fmt.Fprintf(&b, "HEAD: %s\n", plan.Head)
	fmt.Fprintf(&b, "Commits: %d\n", len(plan.Commits))
	fmt.Fprintf(&b, "Files touched: %d\n\n", len(plan.FilesTouched))

	if len(plan.Commits) > 0 {
		b.WriteString("Commits (newest first):\n")
		for _, c := range plan.Commits {
			marker := ""
			if c.IsMerge {
				marker = " [merge]"
			}
			fmt.Fprintf(&b, "  %s  %s  %s%s\n", c.Short, c.Date, c.Subject, marker)
		}
		b.WriteString("\n")
	}

	if len(plan.RevertCommands) > 0 {
		b.WriteString("Suggested revert commands (apply in order):\n")
		for _, cmd := range plan.RevertCommands {
			fmt.Fprintf(&b, "  %s\n", cmd)
		}
		b.WriteString("\n")
	}

	if len(plan.FilesTouched) > 0 {
		b.WriteString("Files touched:\n")
		for _, f := range plan.FilesTouched {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		b.WriteString("\n")
	}

	if len(plan.Notes) > 0 {
		b.WriteString("Notes:\n")
		for _, n := range plan.Notes {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}
	return b.String()
}

func renderMarkdown(plan *RollbackPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Rollback Plan\n\n")
	fmt.Fprintf(&b, "- **Range:** `%s..HEAD`\n", planLowerBound(plan))
	fmt.Fprintf(&b, "- **HEAD:** `%s`\n", plan.Head)
	fmt.Fprintf(&b, "- **Commits:** %d\n", len(plan.Commits))
	fmt.Fprintf(&b, "- **Files touched:** %d\n\n", len(plan.FilesTouched))

	if len(plan.Commits) > 0 {
		b.WriteString("## Commits\n\n")
		b.WriteString("| SHA | Date | Author | Subject |\n")
		b.WriteString("|-----|------|--------|---------|\n")
		for _, c := range plan.Commits {
			subject := c.Subject
			if c.IsMerge {
				subject = "[merge] " + subject
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", c.Short, c.Date, c.Author, escapeMD(subject))
		}
		b.WriteString("\n")
	}

	if len(plan.RevertCommands) > 0 {
		b.WriteString("## Suggested Revert Commands\n\n```bash\n")
		for _, cmd := range plan.RevertCommands {
			b.WriteString(cmd + "\n")
		}
		b.WriteString("```\n\n")
	}

	if len(plan.FilesTouched) > 0 {
		b.WriteString("## Files Touched\n\n")
		for _, f := range plan.FilesTouched {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
		b.WriteString("\n")
	}

	if len(plan.Notes) > 0 {
		b.WriteString("## Notes\n\n")
		for _, n := range plan.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func planLowerBound(plan *RollbackPlan) string {
	if plan.Since != "" {
		return plan.Since
	}
	return plan.Base
}

func escapeMD(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
