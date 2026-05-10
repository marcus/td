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
)

var (
	// ErrNotRepository indicates the target directory is not inside a git repo.
	ErrNotRepository = errors.New("not a git repository")
	// ErrNoSemverTag indicates no reachable semver tag was found.
	ErrNoSemverTag = errors.New("no reachable semver tag found")

	semverTagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

// State represents the current git state.
type State struct {
	CommitSHA  string
	Branch     string
	IsClean    bool
	Modified   int
	Untracked  int
	DirtyFiles int
}

// FileChange represents changes to a file.
type FileChange struct {
	Path      string
	Additions int
	Deletions int
	IsNew     bool
}

// DiffStats summarizes git diff statistics.
type DiffStats struct {
	FilesChanged int
	Additions    int
	Deletions    int
}

// Commit captures the metadata needed for release-note drafting.
type Commit struct {
	SHA     string
	Subject string
	Files   []string
}

// GetState returns the current git state.
func GetState() (*State, error) {
	state := &State{}

	sha, err := runGit("rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("not a git repository")
	}
	state.CommitSHA = strings.TrimSpace(sha)

	branch, err := runGit("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		branch = "HEAD"
	}
	state.Branch = strings.TrimSpace(branch)

	status, _ := runGit("status", "--porcelain")
	lines := strings.Split(strings.TrimSpace(status), "\n")

	if status == "" || (len(lines) == 1 && lines[0] == "") {
		state.IsClean = true
	} else {
		for _, line := range lines {
			if len(line) < 2 {
				continue
			}
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

// GetCommitsSince returns the number of commits since a given SHA.
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

// GetChangedFilesSince returns changed files since a given SHA.
func GetChangedFilesSince(sha string) ([]FileChange, error) {
	output, err := runGit("diff", "--stat", sha+"..HEAD")
	if err != nil {
		return nil, err
	}

	return parseStatOutput(output), nil
}

func parseStatOutput(output string) []FileChange {
	var changes []FileChange
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, " ") && strings.Contains(line, "changed") {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			continue
		}

		path := strings.TrimSpace(parts[0])
		stats := strings.TrimSpace(parts[1])

		change := FileChange{Path: path}
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

// GetDiffStatsSince returns diff statistics since a given SHA.
func GetDiffStatsSince(sha string) (*DiffStats, error) {
	output, err := runGit("diff", "--shortstat", sha+"..HEAD")
	if err != nil {
		return nil, err
	}

	stats := &DiffStats{}
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

// IsRepo checks if we're in a git repository.
func IsRepo() bool {
	return IsRepoAt("")
}

// GetRootDir returns the git repository root directory.
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
		return "", fmt.Errorf("%w", ErrNotRepository)
	}
	return strings.TrimSpace(output), nil
}

// GetLatestSemverTag returns the latest reachable semver tag for ref.
func GetLatestSemverTag(dir, ref string) (string, error) {
	root, err := GetRootDirFrom(dir)
	if err != nil {
		return "", err
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "HEAD"
	}

	if err := verifyCommitRef(root, ref); err != nil {
		return "", err
	}

	tags, err := listSemverTagsMerged(root, ref)
	if err != nil {
		return "", err
	}

	for _, tag := range tags {
		return tag, nil
	}

	return "", ErrNoSemverTag
}

// GetPreviousSemverTag returns the latest reachable semver tag before ref.
// Any semver tags pointing at ref itself are skipped.
func GetPreviousSemverTag(dir, ref string) (string, error) {
	root, err := GetRootDirFrom(dir)
	if err != nil {
		return "", err
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "HEAD"
	}

	targetCommit, err := resolveCommitRef(root, ref)
	if err != nil {
		return "", err
	}

	tags, err := listSemverTagsMerged(root, ref)
	if err != nil {
		return "", err
	}

	for _, tag := range tags {
		tagCommit, err := resolveCommitRef(root, tag)
		if err != nil {
			return "", err
		}
		if tagCommit == targetCommit {
			continue
		}
		return tag, nil
	}

	return "", ErrNoSemverTag
}

// RefPointsToSemverTag reports whether ref resolves to a commit with at least
// one semver tag pointing at it.
func RefPointsToSemverTag(dir, ref string) (bool, error) {
	root, err := GetRootDirFrom(dir)
	if err != nil {
		return false, err
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "HEAD"
	}

	if _, err := resolveCommitRef(root, ref); err != nil {
		return false, err
	}

	output, err := runGitInDir(root, "tag", "--points-at", ref, "--sort=-version:refname")
	if err != nil {
		return false, err
	}

	for _, line := range strings.Split(output, "\n") {
		tag := strings.TrimSpace(line)
		if semverTagPattern.MatchString(tag) {
			return true, nil
		}
	}

	return false, nil
}

// ListCommitsInRange returns non-merge commits in chronological order with
// each commit's touched files.
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
		toRef = "HEAD"
	}

	if err := verifyCommitRef(root, fromRef); err != nil {
		return nil, err
	}
	if err := verifyCommitRef(root, toRef); err != nil {
		return nil, err
	}

	output, err := runGitInDir(root, "log", "--no-merges", "--reverse", "--format=%H%x00%s", fromRef+".."+toRef)
	if err != nil {
		return nil, err
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return []Commit{}, nil
	}

	lines := strings.Split(output, "\n")
	commits := make([]Commit, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\x00", 2)
		if len(parts) != 2 {
			continue
		}

		sha := strings.TrimSpace(parts[0])
		files, err := listFilesForCommit(root, sha)
		if err != nil {
			return nil, err
		}

		commits = append(commits, Commit{
			SHA:     sha,
			Subject: strings.TrimSpace(parts[1]),
			Files:   files,
		})
	}

	return commits, nil
}

func verifyCommitRef(dir, ref string) error {
	_, err := resolveCommitRef(dir, ref)
	return err
}

func listFilesForCommit(dir, sha string) ([]string, error) {
	output, err := runGitInDir(dir, "show", "--pretty=format:", "--name-only", "--diff-filter=ACDMRT", sha)
	if err != nil {
		return nil, err
	}
	return splitUniqueLines(output), nil
}

func splitUniqueLines(output string) []string {
	seen := make(map[string]struct{})
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return lines
}

func listSemverTagsMerged(dir, ref string) ([]string, error) {
	output, err := runGitInDir(dir, "tag", "--merged", ref, "--sort=-version:refname")
	if err != nil {
		return nil, err
	}

	tags := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		tag := strings.TrimSpace(line)
		if tag == "" || !semverTagPattern.MatchString(tag) {
			continue
		}
		tags = append(tags, tag)
	}

	return tags, nil
}

func resolveCommitRef(dir, ref string) (string, error) {
	output, err := runGitInDir(dir, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("invalid git ref %q: %w", ref, err)
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
		return "", fmt.Errorf("%s: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
