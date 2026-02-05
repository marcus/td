// Package workdir resolves the td database root directory, supporting git
// worktree redirection via .td-root files.
package workdir

import (
	"os"
	"path/filepath"
	"strings"
)

const tdRootFile = ".td-root"

// ResolveBaseDir checks for a .td-root file in the given directory.
// If found, it returns the path contained in that file (pointing to the main
// worktree's root). Otherwise, returns the original baseDir unchanged.
// This enables git worktrees to share a single td database with the main repo.
func ResolveBaseDir(baseDir string) string {
	tdRootPath := filepath.Join(baseDir, tdRootFile)
	content, err := os.ReadFile(tdRootPath)
	if err != nil {
		return baseDir
	}
	resolved := strings.TrimSpace(string(content))
	if resolved == "" {
		return baseDir
	}
	return resolved
}
