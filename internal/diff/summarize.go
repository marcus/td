package diff

import (
	"path/filepath"
	"strings"

	"github.com/marcus/td/internal/git"
)

// FileSummary contains the semantic summary for a single file's changes.
type FileSummary struct {
	Path     string   `json:"path"`
	Status   string   `json:"status"`
	Language string   `json:"language"`
	Changes  []Change `json:"changes,omitempty"`
	Category string   `json:"category,omitempty"` // For non-Go files
}

// Summarize takes parsed diffs and produces semantic summaries.
// ref is the git ref to retrieve old file content from (e.g. "HEAD~1", "HEAD").
func Summarize(diffs []FileDiff, ref string) []FileSummary {
	var summaries []FileSummary

	for _, fd := range diffs {
		summary := FileSummary{
			Path:   fd.NewPath,
			Status: fd.Status,
		}
		if fd.NewPath == "" || fd.NewPath == "/dev/null" {
			summary.Path = fd.OldPath
		}

		ext := strings.ToLower(filepath.Ext(summary.Path))

		if ext == ".go" {
			summary.Language = "go"
			summary.Changes = analyzeGoFileDiff(fd, ref)
		} else {
			summary.Language = languageFromExt(ext)
			summary.Category = categorizeFile(summary.Path, fd)
		}

		summaries = append(summaries, summary)
	}

	return summaries
}

func analyzeGoFileDiff(fd FileDiff, ref string) []Change {
	var oldSrc, newSrc []byte

	if fd.Status != "added" && fd.OldPath != "" {
		old, err := git.GetFileAtRef(ref, fd.OldPath)
		if err == nil {
			oldSrc = old
		}
	}

	if fd.Status != "deleted" && fd.NewPath != "" {
		// New content is the current working tree version; try HEAD first, then working tree
		new, err := git.GetFileAtRef("HEAD", fd.NewPath)
		if err == nil {
			newSrc = new
		}
	}

	// If we couldn't get either side, fall back to hunk-based analysis
	if oldSrc == nil && newSrc == nil {
		return hunkBasedChanges(fd)
	}

	return AnalyzeGoFile(oldSrc, newSrc)
}

// hunkBasedChanges provides a basic summary when git show fails.
func hunkBasedChanges(fd FileDiff) []Change {
	added, removed := 0, 0
	for _, h := range fd.Hunks {
		for _, l := range h.Lines {
			switch l.Kind {
			case LineAdded:
				added++
			case LineRemoved:
				removed++
			}
		}
	}

	var changes []Change
	if added > 0 && removed > 0 {
		changes = append(changes, Change{
			Kind:     ChangeModified,
			Symbol:   fd.NewPath,
			Category: CategoryFunction,
			Detail:   "content modified",
		})
	} else if added > 0 {
		changes = append(changes, Change{
			Kind:   ChangeAdded,
			Symbol: fd.NewPath,
		})
	} else if removed > 0 {
		changes = append(changes, Change{
			Kind:   ChangeRemoved,
			Symbol: fd.OldPath,
		})
	}
	return changes
}

func languageFromExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "c++"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".sql":
		return "sql"
	case ".sh", ".bash", ".zsh":
		return "shell"
	default:
		return ""
	}
}

func categorizeFile(path string, fd FileDiff) string {
	base := filepath.Base(path)
	dir := filepath.Dir(path)

	switch {
	case strings.HasSuffix(base, "_test.go") || strings.Contains(dir, "test"):
		return "test"
	case base == "go.mod" || base == "go.sum" || base == "package.json" || base == "Cargo.toml":
		return "dependency"
	case strings.HasSuffix(base, ".md"):
		return "documentation"
	case base == "Makefile" || base == "Dockerfile" || strings.Contains(base, ".yml") || strings.Contains(base, ".yaml"):
		return "configuration"
	case strings.Contains(dir, "cmd"):
		return "command"
	case strings.Contains(dir, "internal") || strings.Contains(dir, "pkg"):
		return "library"
	default:
		return "other"
	}
}
