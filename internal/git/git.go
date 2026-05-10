// Package git provides utilities for reading git repository state (branch,
// commit, dirty status).
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrNotRepository indicates the target directory is not a git repository.
	ErrNotRepository = errors.New("not a git repository")
	// ErrNoSemverTags indicates no semver-compatible tags were found.
	ErrNoSemverTags = errors.New("no semver tags found")

	semverTagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
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

// Commit represents structured metadata for a git commit.
type Commit struct {
	Hash       string
	Subject    string
	Body       string
	AuthorDate time.Time
}

// GetState returns the current git state
func GetState() (*State, error) {
	return GetStateInDir("")
}

// GetStateInDir returns the current git state for a specific repository
// directory. An empty dir uses the current working directory.
func GetStateInDir(dir string) (*State, error) {
	state := &State{}

	// Get current commit SHA
	sha, err := runGitInDir(dir, "rev-parse", "HEAD")
	if err != nil {
		return nil, ErrNotRepository
	}
	state.CommitSHA = strings.TrimSpace(sha)

	// Get current branch
	branch, err := runGitInDir(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		branch = "HEAD"
	}
	state.Branch = strings.TrimSpace(branch)

	// Get status
	status, _ := runGitInDir(dir, "status", "--porcelain")
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

// GetLatestSemverTag returns the latest semver-compatible tag reachable from HEAD
// in the current repository.
func GetLatestSemverTag() (string, error) {
	return GetLatestSemverTagInDir("")
}

// GetLatestSemverTagInDir returns the latest semver-compatible tag reachable
// from HEAD in the given repository.
func GetLatestSemverTagInDir(dir string) (string, error) {
	return GetLatestSemverTagReachableFromInDir(dir, "HEAD")
}

// GetLatestSemverTagReachableFromInDir returns the highest semver-compatible
// tag reachable from the given revision in the provided repository.
func GetLatestSemverTagReachableFromInDir(dir, rev string) (string, error) {
	rev = strings.TrimSpace(rev)
	if rev == "" {
		rev = "HEAD"
	}

	output, err := runGitInDir(dir, "tag", "--merged", rev, "--list", "--sort=-version:refname", "v*")
	if err != nil {
		if errors.Is(err, ErrNotRepository) {
			return "", err
		}
		return "", fmt.Errorf("list git tags reachable from %s: %w", rev, err)
	}

	for _, line := range strings.Split(output, "\n") {
		tag := strings.TrimSpace(line)
		if tag == "" {
			continue
		}
		if semverTagPattern.MatchString(tag) {
			return tag, nil
		}
	}

	return "", ErrNoSemverTags
}

// ListCommitsInRange returns structured commits for the given revision range in
// chronological order.
func ListCommitsInRange(from, to string) ([]Commit, error) {
	return ListCommitsInRangeInDir("", from, to)
}

// ListCommitsInRangeInDir returns structured commits for the given revision
// range in chronological order from the specified repository directory.
func ListCommitsInRangeInDir(dir, from, to string) ([]Commit, error) {
	rangeArg := revisionRange(from, to)
	output, err := runGitInDir(dir,
		"log",
		"--reverse",
		"--format=%H%x1f%s%x1f%b%x1f%aI%x1e",
		rangeArg,
	)
	if err != nil {
		if errors.Is(err, ErrNotRepository) {
			return nil, err
		}
		return nil, fmt.Errorf("list commits for %s: %w", rangeArg, err)
	}

	return parseCommitLog(output)
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

// GetRootDirInDir returns the git repository root directory for a specific directory.
func GetRootDirInDir(dir string) (string, error) {
	output, err := runGitInDir(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

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
		errText := strings.TrimSpace(stderr.String())
		if strings.Contains(errText, "not a git repository") {
			return "", ErrNotRepository
		}
		if errText == "" {
			errText = err.Error()
		}
		return "", fmt.Errorf("%s: %s", err, errText)
	}

	return stdout.String(), nil
}

func revisionRange(from, to string) string {
	switch {
	case from != "" && to != "":
		return from + ".." + to
	case from != "":
		return from + "..HEAD"
	case to != "":
		return to
	default:
		return "HEAD"
	}
}

func parseCommitLog(output string) ([]Commit, error) {
	output = strings.TrimSuffix(output, "\x1e")
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}

	records := strings.Split(output, "\x1e")
	commits := make([]Commit, 0, len(records))
	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}

		fields := strings.Split(record, "\x1f")
		if len(fields) != 4 {
			return nil, fmt.Errorf("unexpected git log record: %q", record)
		}

		authorDate, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[3]))
		if err != nil {
			return nil, fmt.Errorf("parse author date %q: %w", fields[3], err)
		}

		commits = append(commits, Commit{
			Hash:       strings.TrimSpace(fields[0]),
			Subject:    strings.TrimSpace(fields[1]),
			Body:       strings.TrimSpace(fields[2]),
			AuthorDate: authorDate,
		})
	}

	return commits, nil
}
