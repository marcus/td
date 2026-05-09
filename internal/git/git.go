// Package git provides utilities for reading git repository state (branch,
// commit, dirty status).
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrNotRepository indicates the target directory is not inside a git repo.
	ErrNotRepository = errors.New("not a git repository")
	// ErrNoSemverTag indicates no reachable semver tag was found.
	ErrNoSemverTag = errors.New("no reachable semver tag found")

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

// Commit captures the git commit fields needed for changelog generation.
type Commit struct {
	SHA      string
	ShortSHA string
	Subject  string
	Body     string
	Date     time.Time
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
	return IsRepoAt("")
}

// GetRootDir returns the git repository root directory
func GetRootDir() (string, error) {
	return GetRootDirFrom("")
}

// IsRepoAt checks whether dir is inside a git repository.
func IsRepoAt(dir string) bool {
	_, err := runGitInDir(dir, "rev-parse", "--git-dir")
	return err == nil
}

// GetRootDirFrom returns the git repository root for dir.
func GetRootDirFrom(dir string) (string, error) {
	output, err := runGitInDir(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", ErrNotRepository
	}
	return strings.TrimSpace(output), nil
}

// ResolveRef resolves ref to a commit SHA and rejects empty or invalid refs.
func ResolveRef(dir, ref string) (string, error) {
	root, err := GetRootDirFrom(dir)
	if err != nil {
		return "", err
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("git ref is required")
	}

	output, err := runGitInDir(root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("invalid git ref %q: %w", ref, err)
	}
	return strings.TrimSpace(output), nil
}

// NearestReachableSemverTag returns the nearest semver tag reachable from ref.
func NearestReachableSemverTag(dir, ref string) (string, error) {
	root, err := GetRootDirFrom(dir)
	if err != nil {
		return "", err
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "HEAD"
	}

	targetSHA, err := ResolveRef(root, ref)
	if err != nil {
		return "", err
	}

	output, err := runGitInDir(root, "tag", "--merged", targetSHA, "--sort=-version:refname")
	if err != nil {
		return "", err
	}

	var tags []string
	for _, line := range strings.Split(output, "\n") {
		tag := strings.TrimSpace(line)
		if tag != "" && semverTagPattern.MatchString(tag) {
			tags = append(tags, tag)
		}
	}
	if len(tags) == 0 {
		return "", ErrNoSemverTag
	}

	type candidate struct {
		tag      string
		distance int
	}
	candidates := make([]candidate, 0, len(tags))
	for _, tag := range tags {
		countOutput, err := runGitInDir(root, "rev-list", "--count", tag+".."+targetSHA)
		if err != nil {
			return "", err
		}
		count, err := strconv.Atoi(strings.TrimSpace(countOutput))
		if err != nil {
			return "", err
		}
		candidates = append(candidates, candidate{tag: tag, distance: count})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].distance < candidates[j].distance
	})
	return candidates[0].tag, nil
}

// ListCommitsInRange returns commits in oldest-first order for fromRef..toRef.
func ListCommitsInRange(dir, fromRef, toRef string) ([]Commit, error) {
	root, err := GetRootDirFrom(dir)
	if err != nil {
		return nil, err
	}

	fromRef = strings.TrimSpace(fromRef)
	if fromRef == "" {
		return nil, fmt.Errorf("start git ref is required")
	}
	toRef = strings.TrimSpace(toRef)
	if toRef == "" {
		return nil, fmt.Errorf("end git ref is required")
	}

	fromSHA, err := ResolveRef(root, fromRef)
	if err != nil {
		return nil, err
	}
	toSHA, err := ResolveRef(root, toRef)
	if err != nil {
		return nil, err
	}

	output, err := runGitInDir(root, "log", "-z", "--reverse", "--format=%H%x00%h%x00%aI%x00%s%x00%b", fromSHA+".."+toSHA)
	if err != nil {
		return nil, err
	}
	if output == "" {
		return []Commit{}, nil
	}

	fields := strings.Split(output, "\x00")
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%5 != 0 {
		return nil, fmt.Errorf("unexpected git log output for range %s..%s", fromRef, toRef)
	}

	commits := make([]Commit, 0, len(fields)/5)
	for i := 0; i < len(fields); i += 5 {
		date, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[i+2]))
		if err != nil {
			return nil, fmt.Errorf("parse commit date for %s: %w", fields[i], err)
		}
		commits = append(commits, Commit{
			SHA:      strings.TrimSpace(fields[i]),
			ShortSHA: strings.TrimSpace(fields[i+1]),
			Date:     date,
			Subject:  strings.TrimSpace(fields[i+3]),
			Body:     strings.TrimSpace(fields[i+4]),
		})
	}

	return commits, nil
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
		return "", fmt.Errorf("%s: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
