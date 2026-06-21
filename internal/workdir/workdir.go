// Package workdir resolves the td database root directory, supporting git
// worktree redirection via .td-root files.
package workdir

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	tdRootFile = ".td-root"
	todosDir   = ".todos"
)

// WorktreeInfo describes the current checkout and the shared td repo root.
type WorktreeInfo struct {
	RepoRoot     string
	WorktreeRoot string
	WorktreeID   string
}

// CurrentWorktree returns worktree metadata for the current directory.
func CurrentWorktree() (WorktreeInfo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return WorktreeInfo{}, err
	}
	return WorktreeForPath(cwd)
}

// WorktreeForPath returns worktree metadata for dir. RepoRoot is the resolved
// td base directory, while WorktreeRoot is the actual checkout root for dir.
func WorktreeForPath(dir string) (WorktreeInfo, error) {
	if dir == "" {
		return WorktreeInfo{}, nil
	}

	worktreeRoot, err := gitTopLevel(dir)
	if err != nil || worktreeRoot == "" {
		worktreeRoot = dir
	}

	worktreeRoot = canonicalPath(worktreeRoot)
	repoRoot := canonicalPath(ResolveBaseDir(worktreeRoot))

	return WorktreeInfo{
		RepoRoot:     repoRoot,
		WorktreeRoot: worktreeRoot,
		WorktreeID:   worktreeID(worktreeRoot),
	}, nil
}

// ResolveBaseDir resolves td's project root with conservative heuristics:
//  1. Honor .td-root in the current directory.
//  2. Use current directory if it already has a .todos directory.
//  3. If inside git, check git root for .td-root or .todos.
//
// If no td markers are found, it returns the original baseDir unchanged.
func ResolveBaseDir(baseDir string) string {
	if baseDir == "" {
		return baseDir
	}
	baseDir = filepath.Clean(baseDir)

	if resolved, ok := readTdRoot(baseDir); ok {
		return resolved
	}
	if hasTodosDir(baseDir) {
		return baseDir
	}

	// Check directory associations (from ~/.config/td/associations.json)
	if target, ok := LookupAssociation(baseDir); ok {
		return target
	}

	gitRoot, err := gitTopLevel(baseDir)
	if err != nil || gitRoot == "" {
		return baseDir
	}
	gitRoot = filepath.Clean(gitRoot)

	if resolved, ok := readTdRoot(gitRoot); ok {
		return resolved
	}
	if hasTodosDir(gitRoot) {
		return gitRoot
	}

	// Check directory associations for git root
	if target, ok := LookupAssociation(gitRoot); ok {
		return target
	}

	// Check main worktree (handles external worktrees without .td-root)
	mainRoot, err := gitMainWorktree(baseDir)
	if err == nil && mainRoot != "" && mainRoot != gitRoot {
		if resolved, ok := readTdRoot(mainRoot); ok {
			return resolved
		}
		if hasTodosDir(mainRoot) {
			return mainRoot
		}
	}

	return baseDir
}

func readTdRoot(dir string) (string, bool) {
	tdRootPath := filepath.Join(dir, tdRootFile)
	content, err := os.ReadFile(tdRootPath)
	if err != nil {
		return "", false
	}

	resolved := strings.TrimSpace(string(content))
	if resolved == "" {
		return "", false
	}
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(dir, resolved)
	}

	return filepath.Clean(resolved), true
}

func hasTodosDir(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, todosDir))
	return err == nil && fi.IsDir()
}

func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(resolved)
}

func worktreeID(canonicalWorktreePath string) string {
	sum := sha256.Sum256([]byte(canonicalWorktreePath))
	return "wt_" + hex.EncodeToString(sum[:])[:12]
}

func gitTopLevel(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitMainWorktree returns the root of the main worktree for external git
// worktrees. It returns ("", nil) when dir is already the main worktree.
func gitMainWorktree(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return "", err
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(dir, commonDir)
	}
	commonDir = filepath.Clean(commonDir)

	// The main worktree root is the parent of the common git dir.
	mainRoot := filepath.Dir(commonDir)

	// If the main root equals the current toplevel, we're already there.
	topLevel, err := gitTopLevel(dir)
	if err != nil {
		return "", err
	}
	if filepath.Clean(topLevel) == mainRoot {
		return "", nil
	}

	return mainRoot, nil
}
