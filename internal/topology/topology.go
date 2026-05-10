// Package topology scans git-tracked files and builds a hierarchical tree
// with optional git activity stats and td issue linkage.
package topology

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Node represents a file or directory in the repository tree.
type Node struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	IsDir    bool    `json:"is_dir"`
	Children []*Node `json:"children,omitempty"`

	// Git stats (populated when stats are requested)
	Commits      int    `json:"commits,omitempty"`
	LastModified string `json:"last_modified,omitempty"`

	// Issue linkage (populated when issues are requested)
	LinkedIssues []string `json:"linked_issues,omitempty"`

	// Aggregated stats for directories
	FileCount int `json:"file_count,omitempty"`
}

// ScanOptions configures how the repository is scanned.
type ScanOptions struct {
	RootDir   string
	MaxDepth  int    // 0 = unlimited
	Filter    string // glob pattern to include
	WithStats bool
}

// Scan reads git-tracked files and builds a tree rooted at the repo root.
func Scan(opts ScanOptions) (*Node, error) {
	files, err := listGitFiles(opts.RootDir)
	if err != nil {
		return nil, fmt.Errorf("list git files: %w", err)
	}

	if opts.Filter != "" {
		files = filterFiles(files, opts.Filter)
	}

	root := buildTree(files, opts.MaxDepth)

	if opts.WithStats {
		if err := populateStats(root, opts.RootDir); err != nil {
			return nil, fmt.Errorf("populate stats: %w", err)
		}
	}

	computeFileCounts(root)

	return root, nil
}

// listGitFiles returns all git-tracked files relative to the repo root.
func listGitFiles(rootDir string) ([]string, error) {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = rootDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %s", err, stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// filterFiles returns only files matching the glob pattern.
func filterFiles(files []string, pattern string) []string {
	var matched []string
	for _, f := range files {
		if ok, _ := filepath.Match(pattern, filepath.Base(f)); ok {
			matched = append(matched, f)
		}
		// Also try matching against the full path
		if ok, _ := filepath.Match(pattern, f); ok && !slices.Contains(matched, f) {
			matched = append(matched, f)
		}
	}
	return matched
}

// buildTree constructs a Node tree from a list of file paths.
func buildTree(files []string, maxDepth int) *Node {
	root := &Node{
		Name:  ".",
		Path:  ".",
		IsDir: true,
	}

	for _, file := range files {
		parts := strings.Split(file, "/")

		// Apply depth limit: skip files deeper than maxDepth
		if maxDepth > 0 && len(parts) > maxDepth {
			continue
		}

		current := root
		for i, part := range parts {
			isFile := i == len(parts)-1
			path := strings.Join(parts[:i+1], "/")

			child := findChild(current, part)
			if child == nil {
				child = &Node{
					Name:  part,
					Path:  path,
					IsDir: !isFile,
				}
				current.Children = append(current.Children, child)
			}
			current = child
		}
	}

	sortTree(root)
	return root
}

// findChild finds a child node by name.
func findChild(parent *Node, name string) *Node {
	for _, c := range parent.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// sortTree sorts children: directories first, then files, alphabetically within each group.
func sortTree(node *Node) {
	sort.Slice(node.Children, func(i, j int) bool {
		if node.Children[i].IsDir != node.Children[j].IsDir {
			return node.Children[i].IsDir
		}
		return node.Children[i].Name < node.Children[j].Name
	})
	for _, child := range node.Children {
		if child.IsDir {
			sortTree(child)
		}
	}
}

// populateStats adds git commit count and last modified time to file nodes.
func populateStats(node *Node, rootDir string) error {
	if !node.IsDir {
		count, err := gitCommitCount(rootDir, node.Path)
		if err == nil {
			node.Commits = count
		}
		modified, err := gitLastModified(rootDir, node.Path)
		if err == nil {
			node.LastModified = modified
		}
		return nil
	}
	for _, child := range node.Children {
		if err := populateStats(child, rootDir); err != nil {
			return err
		}
	}
	return nil
}

// computeFileCounts sets FileCount on directory nodes.
func computeFileCounts(node *Node) int {
	if !node.IsDir {
		return 1
	}
	count := 0
	for _, child := range node.Children {
		count += computeFileCounts(child)
	}
	node.FileCount = count
	return count
}

// gitCommitCount returns the number of commits touching a file.
func gitCommitCount(rootDir, filePath string) (int, error) {
	cmd := exec.Command("git", "rev-list", "--count", "HEAD", "--", filePath)
	cmd.Dir = rootDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(stdout.String()))
}

// gitLastModified returns the last modified date of a file.
func gitLastModified(rootDir, filePath string) (string, error) {
	cmd := exec.Command("git", "log", "-1", "--format=%ci", "--", filePath)
	cmd.Dir = rootDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	result := strings.TrimSpace(stdout.String())
	if result == "" {
		return "", fmt.Errorf("no commits for %s", filePath)
	}
	// Return just the date portion
	if len(result) >= 10 {
		return result[:10], nil
	}
	return result, nil
}

// AnnotateIssues marks nodes that have linked td issues.
// fileIssues maps relative file paths to lists of issue IDs.
func AnnotateIssues(node *Node, fileIssues map[string][]string) {
	if !node.IsDir {
		if issues, ok := fileIssues[node.Path]; ok {
			node.LinkedIssues = issues
		}
		return
	}
	for _, child := range node.Children {
		AnnotateIssues(child, fileIssues)
	}
}
