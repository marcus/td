package db

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// ToRepoRelative converts an absolute file path to a repo-relative path
// using forward slashes for cross-platform stability.
// Returns an error if the path is outside the repo root.
func ToRepoRelative(absPath, repoRoot string) (string, error) {
	// Clean both paths
	absPath = filepath.Clean(absPath)
	repoRoot = filepath.Clean(repoRoot)

	rel, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("cannot compute relative path: %w", err)
	}

	// Check if the path escapes the repo root
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("file %s is outside repo root %s", absPath, repoRoot)
	}

	// Normalize to forward slashes for cross-platform consistency
	return filepath.ToSlash(rel), nil
}

// IsAbsolutePath returns true if the path looks like an absolute path
// (starts with / on Unix or a drive letter on Windows).
func IsAbsolutePath(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	// Windows drive letter: e.g. C:\, D:/
	if runtime.GOOS == "windows" && len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return true
	}
	return false
}

// NormalizeFilePathForID normalizes a file path to forward slashes
// for use in deterministic ID generation. This ensures the same ID
// is generated regardless of OS path separators.
func NormalizeFilePathForID(p string) string {
	return filepath.ToSlash(filepath.Clean(p))
}
