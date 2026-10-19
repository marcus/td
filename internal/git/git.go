// Package git provides utilities for reading git repository state (branch,
// commit, dirty status).
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// State represents the current git state
type State struct {
	CommitSHA  string
	Branch     string
	IsClean    bool
	Modified   int
	Untracked  int
	DirtyFiles int
}

// GetState returns the current git state
func GetState() (*State, error) {
	return getState(context.Background())
}

func getState(ctx context.Context) (*State, error) {
	state := &State{}

	// Get current commit SHA
	sha, err := runGitContext(ctx, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read git state: %w", err)
	}
	state.CommitSHA = strings.TrimSpace(sha)

	// Get current branch
	branch, err := runGitContext(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read git branch: %w", err)
	}
	state.Branch = strings.TrimSpace(branch)

	// Get status
	status, err := runGitContext(ctx, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("read git status: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(status), "\n")

	if status == "" || (len(lines) == 1 && lines[0] == "") {
		state.IsClean = true
	} else {
		for _, line := range lines {
			if len(line) < 2 {
				continue
			}
			// Check first two characters for status
			if line[0] == '?' && line[1] == '?' {
				state.Untracked++
			} else {
				state.Modified++
			}
		}
		state.DirtyFiles = state.Modified + state.Untracked
	}

	return state, nil
}

// GetCommitsSince returns the number of commits since a given SHA
func GetCommitsSince(sha string) (int, error) {
	return getCommitsSince(context.Background(), sha)
}

func getCommitsSince(ctx context.Context, sha string) (int, error) {
	output, err := runGitContext(ctx, "rev-list", "--count", sha+"..HEAD")
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, err
	}
	return count, nil
}

// GetChangedFilesSince returns changed files since a given SHA
func GetChangedFilesSince(sha string) ([]FileChange, error) {
	output, err := runGit("diff", "--stat", sha+"..HEAD")
	if err != nil {
		return nil, err
	}

	return parseStatOutput(output), nil
}

// FileChange represents changes to a file
type FileChange struct {
	Path      string
	Additions int
	Deletions int
	IsNew     bool
}

func parseStatOutput(output string) []FileChange {
	var changes []FileChange
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, " ") && strings.Contains(line, "changed") {
			continue
		}

		// Parse format: file.go | 10 ++++----
		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			continue
		}

		path := strings.TrimSpace(parts[0])
		stats := strings.TrimSpace(parts[1])

		change := FileChange{Path: path}

		// Count + and -
		for _, c := range stats {
			switch c {
			case '+':
				change.Additions++
			case '-':
				change.Deletions++
			}
		}

		changes = append(changes, change)
	}

	return changes
}

// DiffStats summarizes git diff statistics
type DiffStats struct {
	FilesChanged int
	Additions    int
	Deletions    int
}

// GetDiffStatsSince returns diff statistics since a given SHA
func GetDiffStatsSince(sha string) (*DiffStats, error) {
	return getDiffStatsSince(context.Background(), sha)
}

func getDiffStatsSince(ctx context.Context, sha string) (*DiffStats, error) {
	output, err := runGitContext(ctx, "diff", "--no-ext-diff", "--no-textconv", "--shortstat", sha+"..HEAD")
	if err != nil {
		return nil, err
	}

	stats := &DiffStats{}

	// Parse format: "3 files changed, 45 insertions(+), 12 deletions(-)"
	output = strings.TrimSpace(output)
	if output == "" {
		return stats, nil
	}

	parts := strings.Split(output, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		fields := strings.Fields(part)
		if len(fields) < 2 {
			continue
		}

		count, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		switch {
		case strings.Contains(part, "file"):
			stats.FilesChanged = count
		case strings.Contains(part, "insertion"):
			stats.Additions = count
		case strings.Contains(part, "deletion"):
			stats.Deletions = count
		}
	}

	return stats, nil
}

// Summary is the current repository state and changes since an issue started.
type Summary struct {
	State             *State
	CommitsSinceStart int
	DiffStats         *DiffStats
}

// GetSummarySince gathers a complete summary within the caller's shared budget.
// A failed or timed-out query returns no summary, so unknown counts never appear as zero.
func GetSummarySince(ctx context.Context, sha string) (*Summary, error) {
	state, err := getState(ctx)
	if err != nil {
		return nil, err
	}
	commits, err := getCommitsSince(ctx, sha)
	if err != nil {
		return nil, err
	}
	stats, err := getDiffStatsSince(ctx, sha)
	if err != nil {
		return nil, err
	}
	return &Summary{State: state, CommitsSinceStart: commits, DiffStats: stats}, nil
}

// IsRepo checks if we're in a git repository
func IsRepo() bool {
	_, err := runGit("rev-parse", "--git-dir")
	return err == nil
}

// GetRootDir returns the git repository root directory
func GetRootDir() (string, error) {
	output, err := runGit("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func runGit(args ...string) (string, error) {
	return runGitContext(context.Background(), args...)
}

func runGitContext(ctx context.Context, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	// Bound pipe cleanup even if an external git helper leaves inherited pipes open.
	cmd.WaitDelay = 100 * time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
