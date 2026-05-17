// Package semdiff parses unified-diff output and classifies hunks by their
// semantic meaning. It is intentionally heuristic and dependency-free so the
// classifier can run offline and remain deterministic for tests.
package semdiff

import (
	"bufio"
	"io"
	"strings"
)

// FileDiff represents a single file's changes in a unified diff.
type FileDiff struct {
	OldPath string
	NewPath string
	IsNew   bool
	IsDel   bool
	Hunks   []Hunk
}

// Path returns the most-relevant path for the file diff.
func (f FileDiff) Path() string {
	if f.NewPath != "" && f.NewPath != "/dev/null" {
		return f.NewPath
	}
	return f.OldPath
}

// Hunk is a single @@ ... @@ block.
type Hunk struct {
	Header   string
	Added    []string
	Removed  []string
	Context  []string
	RawLines []string
}

// Parse reads a unified diff (as produced by `git diff` with default options)
// from r and returns one FileDiff per file.
func Parse(r io.Reader) ([]FileDiff, error) {
	scanner := bufio.NewScanner(r)
	// Allow large lines (some generated files have very long lines).
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var files []FileDiff
	var cur *FileDiff
	var curHunk *Hunk

	flushHunk := func() {
		if cur != nil && curHunk != nil {
			cur.Hunks = append(cur.Hunks, *curHunk)
			curHunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			old, new := parseDiffHeader(line)
			cur = &FileDiff{OldPath: old, NewPath: new}
		case strings.HasPrefix(line, "new file mode"):
			if cur != nil {
				cur.IsNew = true
			}
		case strings.HasPrefix(line, "deleted file mode"):
			if cur != nil {
				cur.IsDel = true
			}
		case strings.HasPrefix(line, "--- "):
			if cur != nil {
				cur.OldPath = stripPathPrefix(strings.TrimPrefix(line, "--- "))
			}
		case strings.HasPrefix(line, "+++ "):
			if cur != nil {
				cur.NewPath = stripPathPrefix(strings.TrimPrefix(line, "+++ "))
			}
		case strings.HasPrefix(line, "@@"):
			flushHunk()
			curHunk = &Hunk{Header: line}
		default:
			if curHunk == nil || cur == nil {
				continue
			}
			curHunk.RawLines = append(curHunk.RawLines, line)
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				curHunk.Added = append(curHunk.Added, line[1:])
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				curHunk.Removed = append(curHunk.Removed, line[1:])
			} else if strings.HasPrefix(line, " ") {
				curHunk.Context = append(curHunk.Context, line[1:])
			}
		}
	}
	flushFile()
	if err := scanner.Err(); err != nil {
		return files, err
	}
	return files, nil
}

func parseDiffHeader(line string) (string, string) {
	// Format: diff --git a/path b/path
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		return "", ""
	}
	return stripPathPrefix(parts[2]), stripPathPrefix(parts[3])
}

func stripPathPrefix(p string) string {
	p = strings.TrimSpace(p)
	switch {
	case strings.HasPrefix(p, "a/"):
		return p[2:]
	case strings.HasPrefix(p, "b/"):
		return p[2:]
	case p == "/dev/null":
		return p
	}
	return p
}
