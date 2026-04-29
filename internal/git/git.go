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

// Commit represents a commit selected from local git history.
type Commit struct {
	SHA      string
	ShortSHA string
	Subject  string
	Body     string
	Date     string
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
	return GetRootDirInDir("")
}

// GetRootDirInDir returns the git repository root directory for dir.
func GetRootDirInDir(dir string) (string, error) {
	output, err := runGitInDir(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// ResolveRef resolves ref to a full commit SHA in repoDir.
func ResolveRef(repoDir, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("ref is required")
	}
	output, err := runGitInDir(repoDir, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("invalid git ref %q: %w", ref, err)
	}
	return strings.TrimSpace(output), nil
}

// NearestSemverTag returns the nearest reachable semver tag from ref.
func NearestSemverTag(repoDir, ref string) (string, error) {
	toSHA, err := ResolveRef(repoDir, ref)
	if err != nil {
		return "", err
	}

	output, err := runGitInDir(repoDir, "tag", "--merged", toSHA, "--list")
	if err != nil {
		return "", err
	}

	tags := strings.Fields(output)
	var bestTag string
	bestDistance := -1
	for _, tag := range tags {
		if !isSemverTag(tag) {
			continue
		}
		countOutput, err := runGitInDir(repoDir, "rev-list", "--count", tag+".."+toSHA)
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
		return "", fmt.Errorf("no reachable semver tag found from %s", ref)
	}
	return bestTag, nil
}

// ListCommits returns commits in from..to in oldest-first order.
func ListCommits(repoDir, from, to string) ([]Commit, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return nil, fmt.Errorf("from and to refs are required")
	}

	format := "%H%x1f%h%x1f%ad%x1f%s%x1f%b%x1e"
	output, err := runGitInDir(repoDir, "log", "--reverse", "--date=short", "--format="+format, from+".."+to)
	if err != nil {
		return nil, err
	}

	var commits []Commit
	for _, record := range strings.Split(output, "\x1e") {
		record = strings.Trim(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		parts := strings.SplitN(record, "\x1f", 5)
		if len(parts) != 5 {
			continue
		}
		commits = append(commits, Commit{
			SHA:      strings.TrimSpace(parts[0]),
			ShortSHA: strings.TrimSpace(parts[1]),
			Date:     strings.TrimSpace(parts[2]),
			Subject:  strings.TrimSpace(parts[3]),
			Body:     strings.TrimSpace(parts[4]),
		})
	}
	return commits, nil
}

func isSemverTag(tag string) bool {
	return semverTagPattern.MatchString(tag)
}

var semverTagPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)

func runGit(args ...string) (string, error) {
	return runGitInDir("", args...)
}

func runGitInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
