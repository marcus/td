// Package git provides utilities for reading git repository state (branch,
// commit, dirty status).
package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
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

// Commit represents a git commit in a changelog-friendly form.
type Commit struct {
	SHA     string
	Subject string
	Body    string
	Date    time.Time
}

// GetState returns the current git state
func GetState() (*State, error) {
	state := &State{}

	// Get current commit SHA
	sha, err := runGit("rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("not a git repository")
	}
	state.CommitSHA = strings.TrimSpace(sha)

	// Get current branch
	branch, err := runGit("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		branch = "HEAD"
	}
	state.Branch = strings.TrimSpace(branch)

	// Get status
	status, _ := runGit("status", "--porcelain")
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
	output, err := runGit("rev-list", "--count", sha+"..HEAD")
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
			if c == '+' {
				change.Additions++
			} else if c == '-' {
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
	output, err := runGit("diff", "--shortstat", sha+"..HEAD")
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

// ResolveRef resolves a revision to its commit SHA.
func ResolveRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("empty git ref")
	}

	output, err := runGit("rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("invalid git ref %q: %w", ref, err)
	}
	return strings.TrimSpace(output), nil
}

// NearestReachableSemverTag returns the nearest semver tag reachable from ref.
func NearestReachableSemverTag(ref string) (string, error) {
	if _, err := ResolveRef(ref); err != nil {
		return "", err
	}

	output, err := runGit("tag", "--merged", ref, "--list")
	if err != nil {
		return "", err
	}

	var bestTag string
	var bestDistance int
	for _, tag := range strings.Split(output, "\n") {
		tag = strings.TrimSpace(tag)
		if tag == "" || !isSemverTag(tag) {
			continue
		}

		countOutput, err := runGit("rev-list", "--count", tag+".."+ref)
		if err != nil {
			continue
		}
		distance, err := strconv.Atoi(strings.TrimSpace(countOutput))
		if err != nil {
			continue
		}
		if bestTag == "" || distance < bestDistance || distance == bestDistance && tag > bestTag {
			bestTag = tag
			bestDistance = distance
		}
	}

	if bestTag == "" {
		return "", fmt.Errorf("no reachable semver tag found from %q", ref)
	}
	return bestTag, nil
}

var semverTagPattern = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func isSemverTag(tag string) bool {
	return semverTagPattern.MatchString(tag)
}

// ListCommits returns commits in from..to order, oldest first.
func ListCommits(from, to string) ([]Commit, error) {
	if _, err := ResolveRef(from); err != nil {
		return nil, err
	}
	if _, err := ResolveRef(to); err != nil {
		return nil, err
	}

	output, err := runGit("log", "--reverse", "--format=%H%x1f%cI%x1f%s%x1f%b%x1e", from+".."+to)
	if err != nil {
		return nil, err
	}

	return parseCommitLog(output)
}

func parseCommitLog(output string) ([]Commit, error) {
	output = strings.TrimSuffix(output, "\x1e\n")
	output = strings.TrimSuffix(output, "\x1e")
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}

	records := strings.Split(output, "\x1e\n")
	commits := make([]Commit, 0, len(records))
	for _, record := range records {
		record = strings.TrimSuffix(record, "\x1e")
		record = strings.TrimPrefix(record, "\n")
		if record == "" {
			continue
		}

		parts := strings.SplitN(record, "\x1f", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("unexpected git log record format")
		}
		date, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("parse commit date: %w", err)
		}
		commits = append(commits, Commit{
			SHA:     strings.TrimSpace(parts[0]),
			Date:    date,
			Subject: strings.TrimSpace(parts[2]),
			Body:    strings.TrimSpace(parts[3]),
		})
	}

	return commits, nil
}

func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
