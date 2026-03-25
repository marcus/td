// Command doc-drift scans markdown files for references to code paths and
// verifies they exist on disk. Exits non-zero when broken references are found.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ref represents a path reference found in a markdown file.
type ref struct {
	file string
	line int
	path string
	kind string // "link" or "backtick"
}

var (
	// Matches markdown links: [text](path)
	linkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)
	// Matches backtick-quoted paths like `cmd/foo.go` or `internal/db/`
	backtickRe = regexp.MustCompile("`([^`]+)`")
	// Heuristic: looks like a file/dir path (contains / or ends with known extension)
	pathLikeRe = regexp.MustCompile(`^[a-zA-Z0-9_.][a-zA-Z0-9_./\-]*(\.[a-zA-Z0-9]+|/)$`)
	// Known code file extensions
	codeExtensions = map[string]bool{
		".go": true, ".js": true, ".ts": true, ".py": true, ".rs": true,
		".sql": true, ".sh": true, ".yaml": true, ".yml": true, ".json": true,
		".toml": true, ".md": true, ".txt": true, ".mod": true, ".sum": true,
		".css": true, ".html": true, ".tsx": true, ".jsx": true, ".svelte": true,
	}
)

func main() {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	refs, err := scanMarkdownFiles(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	broken := validateRefs(root, refs)
	for _, b := range broken {
		fmt.Printf("%s:%d: broken %s reference: %s\n", b.file, b.line, b.kind, b.path)
	}
	if len(broken) > 0 {
		fmt.Printf("\n%d broken reference(s) found\n", len(broken))
		os.Exit(1)
	}
	fmt.Println("No broken references found")
}

// findRepoRoot walks up from cwd looking for .git.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside a git repository")
		}
		dir = parent
	}
}

// scanMarkdownFiles finds all .md files under root and extracts path references.
func scanMarkdownFiles(root string) ([]ref, error) {
	var refs []ref
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip hidden dirs, vendor, node_modules, .git
		if info.IsDir() {
			base := info.Name()
			if strings.HasPrefix(base, ".") || base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		fileRefs, err := extractRefs(root, path)
		if err != nil {
			return err
		}
		refs = append(refs, fileRefs...)
		return nil
	})
	return refs, err
}

// extractRefs parses a single markdown file for path references.
func extractRefs(root, path string) ([]ref, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	relPath, _ := filepath.Rel(root, path)
	var refs []ref
	scanner := bufio.NewScanner(f)
	lineNum := 0
	inCodeBlock := false

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Track fenced code blocks
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		// Extract markdown link targets
		for _, m := range linkRe.FindAllStringSubmatch(line, -1) {
			target := m[2]
			if isURL(target) || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			// Strip anchor fragment
			if idx := strings.Index(target, "#"); idx >= 0 {
				target = target[:idx]
			}
			if target == "" {
				continue
			}
			refs = append(refs, ref{file: relPath, line: lineNum, path: target, kind: "link"})
		}

		// Extract backtick-quoted paths
		for _, m := range backtickRe.FindAllStringSubmatch(line, -1) {
			candidate := m[1]
			if isPathLike(candidate) {
				refs = append(refs, ref{file: relPath, line: lineNum, path: candidate, kind: "backtick"})
			}
		}
	}
	return refs, scanner.Err()
}

// isURL returns true for http/https URLs.
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// isPathLike returns true if the string looks like a file or directory path.
func isPathLike(s string) bool {
	// Must contain a slash or end with a known extension
	if !strings.Contains(s, "/") && !strings.Contains(s, ".") {
		return false
	}
	// Skip things that look like code, flags, or config values
	if strings.ContainsAny(s, " =(){}[]<>$@!?&|;,\"'\\") {
		return false
	}
	// Strip line number references before further checks
	s = stripLineRef(s)
	// Skip URLs
	if isURL(s) {
		return false
	}
	// Must match our path pattern
	if !pathLikeRe.MatchString(s) {
		return false
	}
	// Backtick paths without a slash are bare filenames — too ambiguous to validate.
	// Only validate paths that contain directory structure.
	if !strings.Contains(s, "/") {
		return false
	}
	return true
}

// stripLineRef removes :NNN line number suffixes from a path (e.g., "foo.go:42" -> "foo.go").
func stripLineRef(p string) string {
	// Match trailing :digits pattern
	if idx := strings.LastIndex(p, ":"); idx > 0 {
		suffix := p[idx+1:]
		allDigits := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits && len(suffix) > 0 {
			return p[:idx]
		}
		// Also handle :digits-digits (line ranges)
		parts := strings.Split(suffix, "-")
		if len(parts) == 2 {
			allDigits1, allDigits2 := true, true
			for _, c := range parts[0] {
				if c < '0' || c > '9' {
					allDigits1 = false
				}
			}
			for _, c := range parts[1] {
				if c < '0' || c > '9' {
					allDigits2 = false
				}
			}
			if allDigits1 && allDigits2 {
				return p[:idx]
			}
		}
	}
	return p
}

// validateRefs checks each reference against the filesystem.
func validateRefs(root string, refs []ref) []ref {
	var broken []ref
	for _, r := range refs {
		cleanPath := stripLineRef(r.path)
		if pathExists(root, r.file, cleanPath) {
			continue
		}
		broken = append(broken, r)
	}
	return broken
}

// pathExists checks if a path resolves relative to the markdown file's dir or the repo root.
func pathExists(root, mdFile, target string) bool {
	// Try relative to the markdown file's directory
	mdDir := filepath.Dir(filepath.Join(root, mdFile))
	candidate := filepath.Join(mdDir, target)
	if _, err := os.Stat(candidate); err == nil {
		return true
	}

	// Try relative to repo root
	candidate = filepath.Join(root, target)
	if _, err := os.Stat(candidate); err == nil {
		return true
	}

	return false
}
