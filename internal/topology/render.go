package topology

import (
	"fmt"
	"strings"
)

// RenderOptions configures ASCII tree rendering.
type RenderOptions struct {
	ShowStats  bool
	ShowIssues bool
}

// Render returns the tree as an ASCII string.
func Render(root *Node, opts RenderOptions) string {
	var lines []string

	// Print root
	rootLabel := root.Name
	if root.IsDir && opts.ShowStats {
		rootLabel += fmt.Sprintf(" (%d files)", root.FileCount)
	}
	lines = append(lines, rootLabel)

	// Render children
	childLines := renderNodes(root.Children, "", opts)
	lines = append(lines, childLines...)

	return strings.Join(lines, "\n")
}

// renderNodes recursively renders tree nodes with box-drawing characters.
func renderNodes(nodes []*Node, prefix string, opts RenderOptions) []string {
	var lines []string

	for i, node := range nodes {
		isLast := i == len(nodes)-1

		connector := "\u251c\u2500\u2500 " // ├──
		if isLast {
			connector = "\u2514\u2500\u2500 " // └──
		}

		label := node.Name
		if node.IsDir {
			label += "/"
			if opts.ShowStats {
				label += fmt.Sprintf(" (%d files)", node.FileCount)
			}
		} else {
			var annotations []string
			if opts.ShowStats && node.Commits > 0 {
				annotations = append(annotations, fmt.Sprintf("%d commits", node.Commits))
				if node.LastModified != "" {
					annotations = append(annotations, node.LastModified)
				}
			}
			if opts.ShowIssues && len(node.LinkedIssues) > 0 {
				annotations = append(annotations, "\u2691 "+strings.Join(node.LinkedIssues, ","))
			}
			if len(annotations) > 0 {
				label += "  [" + strings.Join(annotations, " | ") + "]"
			}
		}

		lines = append(lines, prefix+connector+label)

		// Build prefix for children
		childPrefix := prefix
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "\u2502   " // │
		}

		if node.IsDir {
			childLines := renderNodes(node.Children, childPrefix, opts)
			lines = append(lines, childLines...)
		}
	}

	return lines
}
