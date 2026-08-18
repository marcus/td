package git

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// AuthorLines tracks how many lines an author owns in a file or directory.
type AuthorLines struct {
	Author string
	Lines  int
}

// FileOwnership describes ownership of a single file.
type FileOwnership struct {
	Path         string
	TotalLines   int
	Authors      []AuthorLines // sorted by lines descending
	BusFactor    int           // min people owning >50%
	TopOwnerPct  float64
	Contributors int
}

// DirOwnership describes aggregated ownership of a directory.
type DirOwnership struct {
	Path         string
	TotalLines   int
	Authors      []AuthorLines // sorted by lines descending
	BusFactor    int
	TopOwnerPct  float64
	Contributors int
	FileCount    int
}

// BusFactorResult is the full analysis result.
type BusFactorResult struct {
	Dirs  []DirOwnership
	Files []FileOwnership
}

// BusFactorOptions configures the analysis.
type BusFactorOptions struct {
	Path  string // subdirectory scope (relative to repo root)
	Depth int    // directory aggregation depth
}

// AnalyzeBusFactor runs the full bus-factor analysis on the repository.
// It must be called from within a git repository.
func AnalyzeBusFactor(opts BusFactorOptions) (*BusFactorResult, error) {
	// Get tracked files
	args := []string{"ls-files"}
	if opts.Path != "" {
		args = append(args, opts.Path)
	}
	output, err := runGit(args...)
	if err != nil {
		return nil, fmt.Errorf("listing files: %w", err)
	}

	files := strings.Split(strings.TrimSpace(output), "\n")
	if len(files) == 0 || (len(files) == 1 && files[0] == "") {
		return nil, fmt.Errorf("no tracked files found")
	}

	// Collect per-file ownership via git blame
	fileOwnerships := make([]FileOwnership, 0, len(files))
	dirAgg := make(map[string]map[string]int) // dir -> author -> lines

	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}

		authors, total, err := blameFile(f)
		if err != nil {
			// Skip binary or unblameble files
			continue
		}
		if total == 0 {
			continue
		}

		fo := buildFileOwnership(f, authors, total)
		fileOwnerships = append(fileOwnerships, fo)

		// Aggregate into directory buckets
		dir := dirAtDepth(f, opts.Depth)
		if dirAgg[dir] == nil {
			dirAgg[dir] = make(map[string]int)
		}
		for author, lines := range authors {
			dirAgg[dir][author] += lines
		}
	}

	// Build directory results
	dirs := make([]DirOwnership, 0, len(dirAgg))
	for dir, authors := range dirAgg {
		total := 0
		for _, lines := range authors {
			total += lines
		}
		sorted := sortAuthorMap(authors)
		dirs = append(dirs, DirOwnership{
			Path:         dir,
			TotalLines:   total,
			Authors:      sorted,
			BusFactor:    computeBusFactor(sorted, total),
			TopOwnerPct:  topOwnerPct(sorted, total),
			Contributors: len(sorted),
			FileCount:    countFilesInDir(fileOwnerships, dir, opts.Depth),
		})
	}

	// Sort dirs by bus factor ascending (riskiest first), then by top owner pct descending
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].BusFactor != dirs[j].BusFactor {
			return dirs[i].BusFactor < dirs[j].BusFactor
		}
		return dirs[i].TopOwnerPct > dirs[j].TopOwnerPct
	})

	return &BusFactorResult{
		Dirs:  dirs,
		Files: fileOwnerships,
	}, nil
}

// blameFile runs git blame --line-porcelain on a file and returns author -> line count.
func blameFile(path string) (map[string]int, int, error) {
	cmd := exec.Command("git", "blame", "--line-porcelain", path)
	out, err := cmd.Output()
	if err != nil {
		return nil, 0, err
	}
	return parseBlameOutput(string(out))
}

// parseBlameOutput parses git blame --line-porcelain output.
// Returns author -> line count and total lines.
func parseBlameOutput(output string) (map[string]int, int, error) {
	authors := make(map[string]int)
	total := 0

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if author, ok := strings.CutPrefix(line, "author "); ok {
			if author != "Not Committed Yet" {
				authors[author]++
				total++
			}
		}
	}

	return authors, total, scanner.Err()
}

// computeBusFactor returns the minimum number of people who collectively own >50% of the code.
func computeBusFactor(sorted []AuthorLines, total int) int {
	if total == 0 {
		return 0
	}
	threshold := total / 2
	accum := 0
	for i, a := range sorted {
		accum += a.Lines
		if accum > threshold {
			return i + 1
		}
	}
	return len(sorted)
}

func topOwnerPct(sorted []AuthorLines, total int) float64 {
	if total == 0 || len(sorted) == 0 {
		return 0
	}
	return float64(sorted[0].Lines) / float64(total) * 100
}

func sortAuthorMap(m map[string]int) []AuthorLines {
	result := make([]AuthorLines, 0, len(m))
	for author, lines := range m {
		result = append(result, AuthorLines{Author: author, Lines: lines})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Lines > result[j].Lines
	})
	return result
}

func buildFileOwnership(path string, authors map[string]int, total int) FileOwnership {
	sorted := sortAuthorMap(authors)
	return FileOwnership{
		Path:         path,
		TotalLines:   total,
		Authors:      sorted,
		BusFactor:    computeBusFactor(sorted, total),
		TopOwnerPct:  topOwnerPct(sorted, total),
		Contributors: len(sorted),
	}
}

// dirAtDepth returns the directory prefix at the given depth.
// depth=1: "cmd", depth=2: "cmd/stats", etc.
// Files at root get ".".
func dirAtDepth(filePath string, depth int) string {
	dir := filepath.Dir(filePath)
	if dir == "." {
		return "."
	}

	parts := strings.Split(filepath.ToSlash(dir), "/")
	if len(parts) > depth {
		parts = parts[:depth]
	}
	return strings.Join(parts, "/")
}

func countFilesInDir(files []FileOwnership, dir string, depth int) int {
	count := 0
	for _, f := range files {
		if dirAtDepth(f.Path, depth) == dir {
			count++
		}
	}
	return count
}
