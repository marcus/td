// Package git provides utilities for reading git repository state (branch,
// commit, dirty status).
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
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

// Commit represents a git commit plus the files it changed.
type Commit struct {
	SHA         string
	ShortSHA    string
	Subject     string
	Body        string
	AuthorName  string
	AuthorEmail string
	CommittedAt time.Time
	Files       []string
}

// Repo provides git operations scoped to a specific working directory.
type Repo struct {
	Dir string
}

// RevisionRange represents a git revision span.
type RevisionRange struct {
	From string
	To   string
	Expr string
}

var ErrNoTagsFound = errors.New("no tags found")

// NewRepo returns a git helper rooted at dir.
func NewRepo(dir string) *Repo {
	return &Repo{Dir: dir}
}

// CurrentRepo returns a git helper for the current working directory.
func CurrentRepo() *Repo {
	return &Repo{}
}

// GetState returns the current git state
func GetState() (*State, error) {
	return CurrentRepo().GetState()
}

// GetState returns the current git state for the repo.
func (r *Repo) GetState() (*State, error) {
	state := &State{}

	// Get current commit SHA
	sha, err := r.run("rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("not a git repository")
	}
	state.CommitSHA = strings.TrimSpace(sha)

	// Get current branch
	branch, err := r.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		branch = "HEAD"
	}
	state.Branch = strings.TrimSpace(branch)

	// Get status
	status, _ := r.run("status", "--porcelain")
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
	return CurrentRepo().GetCommitsSince(sha)
}

// GetCommitsSince returns the number of commits since a given SHA.
func (r *Repo) GetCommitsSince(sha string) (int, error) {
	output, err := r.run("rev-list", "--count", sha+"..HEAD")
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
	return CurrentRepo().GetChangedFilesSince(sha)
}

// FileChange represents changes to a file
type FileChange struct {
	Path      string
	Additions int
	Deletions int
	IsNew     bool
	IsDeleted bool
	IsRenamed bool
	OldPath   string
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
	return CurrentRepo().GetDiffStatsSince(sha)
}

// GetDiffStatsSince returns diff statistics since a given SHA.
func (r *Repo) GetDiffStatsSince(sha string) (*DiffStats, error) {
	return r.GetDiffStats(sha + "..HEAD")
}

// GetChangedFilesSince returns changed files since a given SHA.
func (r *Repo) GetChangedFilesSince(sha string) ([]FileChange, error) {
	return r.GetChangedFiles(sha + "..HEAD")
}

// GetChangedFiles returns changed files in a revision range.
func (r *Repo) GetChangedFiles(revisionRange string) ([]FileChange, error) {
	numstatOutput, err := r.run("diff", "--find-renames", "--numstat", revisionRange)
	if err != nil {
		return nil, err
	}

	statusOutput, err := r.run("diff", "--find-renames", "--name-status", revisionRange)
	if err != nil {
		return nil, err
	}

	changes := parseNumstatOutput(numstatOutput)
	applyNameStatus(changes, statusOutput)
	return flattenFileChanges(changes), nil
}

// GetDiffStats returns diff statistics for a revision range.
func (r *Repo) GetDiffStats(revisionRange string) (*DiffStats, error) {
	output, err := r.run("diff", "--shortstat", revisionRange)
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
	return CurrentRepo().IsRepo()
}

// IsRepo checks if the configured directory is in a git repository.
func (r *Repo) IsRepo() bool {
	_, err := r.run("rev-parse", "--git-dir")
	return err == nil
}

// GetRootDir returns the git repository root directory
func GetRootDir() (string, error) {
	return CurrentRepo().GetRootDir()
}

// GetRootDir returns the git repository root directory.
func (r *Repo) GetRootDir() (string, error) {
	output, err := r.run("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// LatestTag returns the most recent reachable tag.
func LatestTag() (string, error) {
	return CurrentRepo().LatestTag()
}

// LatestTag returns the most recent reachable tag.
func (r *Repo) LatestTag() (string, error) {
	output, err := r.run("describe", "--tags", "--abbrev=0")
	if err != nil {
		return "", ErrNoTagsFound
	}
	return strings.TrimSpace(output), nil
}

// ResolveRevisionRange validates a revision range and returns its normalized form.
func ResolveRevisionRange(from, to, revisionRange string) (RevisionRange, error) {
	return CurrentRepo().ResolveRevisionRange(from, to, revisionRange)
}

// ResolveRevisionRange validates a revision range and returns its normalized form.
func (r *Repo) ResolveRevisionRange(from, to, revisionRange string) (RevisionRange, error) {
	if strings.TrimSpace(revisionRange) != "" && (strings.TrimSpace(from) != "" || strings.TrimSpace(to) != "") {
		return RevisionRange{}, fmt.Errorf("use either --range or --from/--to")
	}

	if strings.TrimSpace(revisionRange) != "" {
		if err := r.validateRevisionRange(revisionRange); err != nil {
			return RevisionRange{}, err
		}
		return RevisionRange{Expr: revisionRange}, nil
	}

	resolvedTo := strings.TrimSpace(to)
	if resolvedTo == "" {
		resolvedTo = "HEAD"
	}
	if _, err := r.ResolveRevision(resolvedTo); err != nil {
		return RevisionRange{}, err
	}

	resolvedFrom := strings.TrimSpace(from)
	if resolvedFrom == "" {
		tag, err := r.LatestTag()
		if err != nil {
			if errors.Is(err, ErrNoTagsFound) {
				return RevisionRange{}, ErrNoTagsFound
			}
			return RevisionRange{}, err
		}
		resolvedFrom = tag
	} else {
		if _, err := r.ResolveRevision(resolvedFrom); err != nil {
			return RevisionRange{}, err
		}
	}

	expr := resolvedFrom + ".." + resolvedTo
	if err := r.validateRevisionRange(expr); err != nil {
		return RevisionRange{}, err
	}
	return RevisionRange{From: resolvedFrom, To: resolvedTo, Expr: expr}, nil
}

// ResolveRevision validates and normalizes a revision.
func (r *Repo) ResolveRevision(revision string) (string, error) {
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return "", fmt.Errorf("revision cannot be empty")
	}

	output, err := r.run("rev-parse", "--verify", revision)
	if err != nil {
		return "", fmt.Errorf("invalid revision %q", revision)
	}
	return strings.TrimSpace(output), nil
}

// ListCommits returns commits in a revision range, oldest first.
func ListCommits(revisionRange string) ([]Commit, error) {
	return CurrentRepo().ListCommits(revisionRange)
}

// ListCommits returns commits in a revision range, oldest first.
func (r *Repo) ListCommits(revisionRange string) ([]Commit, error) {
	if err := r.validateRevisionRange(revisionRange); err != nil {
		return nil, err
	}

	output, err := r.run("log", "--reverse", "--format=%H%x1f%h%x1f%s%x1f%B%x1f%an%x1f%ae%x1f%cI%x1e", revisionRange)
	if err != nil {
		return nil, err
	}

	var commits []Commit
	for _, record := range strings.Split(output, "\x1e") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}

		fields := strings.Split(record, "\x1f")
		if len(fields) < 7 {
			continue
		}

		committedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[6]))
		if err != nil {
			return nil, fmt.Errorf("parse commit time: %w", err)
		}

		commit := Commit{
			SHA:         strings.TrimSpace(fields[0]),
			ShortSHA:    strings.TrimSpace(fields[1]),
			Subject:     strings.TrimSpace(fields[2]),
			Body:        strings.TrimSpace(fields[3]),
			AuthorName:  strings.TrimSpace(fields[4]),
			AuthorEmail: strings.TrimSpace(fields[5]),
			CommittedAt: committedAt,
		}

		files, err := r.commitFiles(commit.SHA)
		if err != nil {
			return nil, err
		}
		commit.Files = files
		commits = append(commits, commit)
	}

	return commits, nil
}

func (r *Repo) commitFiles(sha string) ([]string, error) {
	output, err := r.run("show", "--find-renames", "--pretty=format:", "--name-only", sha)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var files []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		files = append(files, line)
	}
	return files, nil
}

func (r *Repo) validateRevisionRange(revisionRange string) error {
	if strings.TrimSpace(revisionRange) == "" {
		return fmt.Errorf("revision range cannot be empty")
	}
	if _, err := r.run("rev-list", "--count", revisionRange); err != nil {
		return fmt.Errorf("invalid revision range %q", revisionRange)
	}
	return nil
}

func runGit(args ...string) (string, error) {
	return CurrentRepo().run(args...)
}

func (r *Repo) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if r != nil && strings.TrimSpace(r.Dir) != "" {
		cmd.Dir = r.Dir
	} else if cwd, err := filepath.Abs("."); err == nil {
		cmd.Dir = cwd
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

func parseNumstatOutput(output string) map[string]*FileChange {
	changes := make(map[string]*FileChange)

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}

		path := fields[2]
		oldPath := ""
		if len(fields) >= 4 {
			oldPath = fields[2]
			path = fields[3]
		}

		change := &FileChange{
			Path:    path,
			OldPath: oldPath,
		}
		if n, err := strconv.Atoi(fields[0]); err == nil {
			change.Additions = n
		}
		if n, err := strconv.Atoi(fields[1]); err == nil {
			change.Deletions = n
		}
		changes[path] = change
	}

	return changes
}

func applyNameStatus(changes map[string]*FileChange, output string) {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}

		status := fields[0]
		path := fields[len(fields)-1]
		change, ok := changes[path]
		if !ok {
			change = &FileChange{Path: path}
			changes[path] = change
		}

		switch {
		case strings.HasPrefix(status, "A"):
			change.IsNew = true
		case strings.HasPrefix(status, "D"):
			change.IsDeleted = true
		case strings.HasPrefix(status, "R"):
			change.IsRenamed = true
			if len(fields) >= 3 {
				change.OldPath = fields[1]
			}
		}
	}
}

func flattenFileChanges(changes map[string]*FileChange) []FileChange {
	if len(changes) == 0 {
		return nil
	}

	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sortStrings(paths)

	result := make([]FileChange, 0, len(paths))
	for _, path := range paths {
		result = append(result, *changes[path])
	}
	return result
}

func sortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
