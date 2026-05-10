// Package diff provides unified diff parsing and semantic analysis of code changes.
package diff

import (
	"strings"
)

// FileDiff represents the diff for a single file.
type FileDiff struct {
	OldPath string
	NewPath string
	Status  string // "added", "deleted", "modified", "renamed"
	Hunks   []Hunk
}

// Hunk represents a single hunk within a file diff.
type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []DiffLine
}

// DiffLine represents a single line within a hunk.
type DiffLine struct {
	Kind    LineKind
	Content string
}

// LineKind indicates whether a diff line is context, added, or removed.
type LineKind int

const (
	LineContext LineKind = iota
	LineAdded
	LineRemoved
)

// Parse parses unified diff text into structured FileDiff values.
func Parse(diffText string) []FileDiff {
	if diffText == "" {
		return nil
	}

	var diffs []FileDiff
	lines := strings.Split(diffText, "\n")
	i := 0

	for i < len(lines) {
		// Look for "diff --git" header
		if !strings.HasPrefix(lines[i], "diff --git ") {
			i++
			continue
		}

		fd := FileDiff{}

		// Parse "diff --git a/path b/path"
		header := lines[i]
		parts := strings.SplitN(header, " b/", 2)
		if len(parts) == 2 {
			fd.NewPath = parts[1]
		}
		aPrefix := strings.TrimPrefix(header, "diff --git a/")
		if idx := strings.Index(aPrefix, " b/"); idx >= 0 {
			fd.OldPath = aPrefix[:idx]
		}

		i++

		// Parse extended headers (index, old mode, new mode, similarity, rename, new file, deleted file)
		for i < len(lines) {
			line := lines[i]
			switch {
			case strings.HasPrefix(line, "new file mode"):
				fd.Status = "added"
				i++
			case strings.HasPrefix(line, "deleted file mode"):
				fd.Status = "deleted"
				i++
			case strings.HasPrefix(line, "similarity index"):
				fd.Status = "renamed"
				i++
			case strings.HasPrefix(line, "rename from "):
				fd.OldPath = strings.TrimPrefix(line, "rename from ")
				i++
			case strings.HasPrefix(line, "rename to "):
				fd.NewPath = strings.TrimPrefix(line, "rename to ")
				i++
			case strings.HasPrefix(line, "index "),
				strings.HasPrefix(line, "old mode"),
				strings.HasPrefix(line, "new mode"):
				i++
			case strings.HasPrefix(line, "--- "):
				// Start of unified diff content
				goto parseContent
			case strings.HasPrefix(line, "Binary files"):
				i++
				goto done
			default:
				goto parseContent
			}
		}

	parseContent:
		// Parse --- and +++ lines
		if i < len(lines) && strings.HasPrefix(lines[i], "--- ") {
			i++
		}
		if i < len(lines) && strings.HasPrefix(lines[i], "+++ ") {
			i++
		}

		// Default status to modified if not set
		if fd.Status == "" {
			fd.Status = "modified"
		}

		// Parse hunks
		for i < len(lines) {
			if strings.HasPrefix(lines[i], "diff --git ") {
				break
			}
			if !strings.HasPrefix(lines[i], "@@ ") {
				i++
				continue
			}

			hunk, newI := parseHunk(lines, i)
			fd.Hunks = append(fd.Hunks, hunk)
			i = newI
		}

	done:
		// Default status to modified if still not set (e.g. binary)
		if fd.Status == "" {
			fd.Status = "modified"
		}
		diffs = append(diffs, fd)
	}

	return diffs
}

// parseHunk parses a single hunk starting at lines[i] which should be the @@ line.
func parseHunk(lines []string, i int) (Hunk, int) {
	hunk := Hunk{}

	// Parse @@ -old,count +new,count @@
	header := lines[i]
	parseHunkHeader(header, &hunk)
	i++

	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "@@ ") {
			break
		}

		if strings.HasPrefix(line, "+") {
			hunk.Lines = append(hunk.Lines, DiffLine{Kind: LineAdded, Content: line[1:]})
		} else if strings.HasPrefix(line, "-") {
			hunk.Lines = append(hunk.Lines, DiffLine{Kind: LineRemoved, Content: line[1:]})
		} else if strings.HasPrefix(line, " ") {
			hunk.Lines = append(hunk.Lines, DiffLine{Kind: LineContext, Content: line[1:]})
		} else if line == `\ No newline at end of file` {
			// skip
		}

		i++
	}

	return hunk, i
}

func parseHunkHeader(header string, hunk *Hunk) {
	// Format: @@ -start,count +start,count @@ optional section
	header = strings.TrimPrefix(header, "@@ ")
	endIdx := strings.Index(header, " @@")
	if endIdx < 0 {
		return
	}
	header = header[:endIdx]

	parts := strings.Fields(header)
	if len(parts) < 2 {
		return
	}

	// Parse -start,count
	old := strings.TrimPrefix(parts[0], "-")
	parseRange(old, &hunk.OldStart, &hunk.OldCount)

	// Parse +start,count
	neu := strings.TrimPrefix(parts[1], "+")
	parseRange(neu, &hunk.NewStart, &hunk.NewCount)
}

func parseRange(s string, start, count *int) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) == 1 {
		*start = atoi(parts[0])
		*count = 1
	} else {
		*start = atoi(parts[0])
		*count = atoi(parts[1])
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
