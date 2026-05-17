package semdiff

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Category describes the semantic flavor of a single change.
type Category string

const (
	CatFileAdded       Category = "file-added"
	CatFileRemoved     Category = "file-removed"
	CatFuncAdded       Category = "function-added"
	CatFuncRemoved     Category = "function-removed"
	CatSignatureChange Category = "signature-change"
	CatCommentOnly     Category = "comment-only"
	CatTestChange      Category = "test-change"
	CatDependency      Category = "dependency-change"
	CatImport          Category = "import-change"
	CatConfig          Category = "config-change"
	CatFormatting      Category = "formatting-only"
	CatControlFlow     Category = "control-flow"
	CatStructChange    Category = "struct-change"
	CatGeneric         Category = "code-change"
)

// Change is a single classified observation within a file.
type Change struct {
	Category Category `json:"category"`
	Detail   string   `json:"detail,omitempty"`
}

// FileSummary is the classifier's output for one file.
type FileSummary struct {
	Path     string   `json:"path"`
	Language string   `json:"language,omitempty"`
	IsNew    bool     `json:"is_new,omitempty"`
	IsDel    bool     `json:"is_deleted,omitempty"`
	Changes  []Change `json:"changes"`
}

// Summary aggregates all files and provides a top-line headline.
type Summary struct {
	Headline string        `json:"headline"`
	Files    []FileSummary `json:"files"`
}

var (
	goFuncRe        = regexp.MustCompile(`^func\s+(\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	goCommentRe     = regexp.MustCompile(`^\s*(//|/\*|\*|\*/)`)
	goImportLineRe  = regexp.MustCompile(`^\s*(import\b|"[^"]+")`)
	goStructFieldRe = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_]*\s+[A-Za-z_*\[\]]`)
	goControlRe     = regexp.MustCompile(`^\s*(if|for|switch|select|return|else|case)\b`)
)

// Classify walks each file diff and produces a Summary.
func Classify(files []FileDiff) Summary {
	out := Summary{}
	for _, f := range files {
		fs := classifyFile(f)
		out.Files = append(out.Files, fs)
	}
	out.Headline = buildHeadline(out.Files)
	return out
}

func classifyFile(f FileDiff) FileSummary {
	fs := FileSummary{
		Path:     f.Path(),
		Language: detectLanguage(f.Path()),
		IsNew:    f.IsNew,
		IsDel:    f.IsDel,
	}

	if f.IsNew {
		fs.Changes = append(fs.Changes, Change{Category: CatFileAdded, Detail: "new file"})
	}
	if f.IsDel {
		fs.Changes = append(fs.Changes, Change{Category: CatFileRemoved, Detail: "file deleted"})
	}

	// File-level shortcuts: dependency / config files are uninteresting at the
	// hunk level — the fact that they changed is itself the signal.
	if isDependencyFile(fs.Path) {
		if hasContent(f) {
			fs.Changes = append(fs.Changes, Change{Category: CatDependency, Detail: filepath.Base(fs.Path) + " updated"})
		}
		return fs
	}
	if isConfigFile(fs.Path) {
		if hasContent(f) {
			fs.Changes = append(fs.Changes, Change{Category: CatConfig, Detail: filepath.Base(fs.Path) + " updated"})
		}
		return fs
	}

	isTest := isTestFile(fs.Path)
	isGo := fs.Language == "go"

	for _, h := range f.Hunks {
		cats := classifyHunk(h, isGo)
		if isTest {
			// Promote a copy of the change so test edits are easy to spot,
			// while preserving the underlying category for filtering.
			for _, c := range cats {
				c.Detail = strings.TrimSpace("test: " + c.Detail)
				fs.Changes = append(fs.Changes, Change{Category: CatTestChange, Detail: c.Detail})
			}
			continue
		}
		fs.Changes = append(fs.Changes, cats...)
	}

	fs.Changes = dedupeChanges(fs.Changes)
	return fs
}

func classifyHunk(h Hunk, isGo bool) []Change {
	if len(h.Added) == 0 && len(h.Removed) == 0 {
		return nil
	}

	// Comment-only: every changed line (added+removed) looks like a comment.
	if isGo && allComments(h.Added) && allComments(h.Removed) {
		return []Change{{Category: CatCommentOnly, Detail: "comment/doc updated"}}
	}

	// Formatting-only: the *content* of added and removed lines (sans
	// whitespace) is identical.
	if formattingOnly(h.Added, h.Removed) {
		return []Change{{Category: CatFormatting, Detail: "whitespace/format only"}}
	}

	var changes []Change

	if isGo {
		changes = append(changes, goFunctionChanges(h)...)
		changes = append(changes, goImportChanges(h)...)
		changes = append(changes, goStructChanges(h)...)
		changes = append(changes, goControlFlowChanges(h)...)
	}

	if len(changes) == 0 {
		changes = append(changes, Change{Category: CatGeneric, Detail: hunkSizeDetail(h)})
	}
	return changes
}

func goFunctionChanges(h Hunk) []Change {
	added := collectFuncs(h.Added)
	removed := collectFuncs(h.Removed)

	var changes []Change
	// Signature change: a func name appears in both added and removed but the
	// full signature line differs.
	for name, sigAdd := range added {
		if sigRem, ok := removed[name]; ok {
			if normalize(sigAdd) != normalize(sigRem) {
				changes = append(changes, Change{
					Category: CatSignatureChange,
					Detail:   "signature changed: " + name,
				})
			}
			delete(added, name)
			delete(removed, name)
		}
	}
	for name := range added {
		changes = append(changes, Change{Category: CatFuncAdded, Detail: "added func " + name})
	}
	for name := range removed {
		changes = append(changes, Change{Category: CatFuncRemoved, Detail: "removed func " + name})
	}
	return changes
}

func goImportChanges(h Hunk) []Change {
	inImportBlock := false
	for _, l := range h.Context {
		if strings.Contains(l, "import (") || strings.TrimSpace(l) == "import (" {
			inImportBlock = true
			break
		}
	}
	if !inImportBlock && !onlyImportLines(h.Added) && !onlyImportLines(h.Removed) {
		return nil
	}
	if hasImportLine(h.Added) || hasImportLine(h.Removed) {
		return []Change{{Category: CatImport, Detail: "import block updated"}}
	}
	return nil
}

func hasImportLine(lines []string) bool {
	for _, l := range lines {
		ts := strings.TrimSpace(l)
		if ts == "" {
			continue
		}
		if strings.HasPrefix(ts, "import ") {
			return true
		}
		if strings.HasPrefix(ts, "\"") && strings.HasSuffix(ts, "\"") {
			return true
		}
	}
	return false
}

func goStructChanges(h Hunk) []Change {
	addedFields := 0
	removedFields := 0
	for _, l := range h.Added {
		if goStructFieldRe.MatchString(l) && !strings.Contains(l, "func ") {
			addedFields++
		}
	}
	for _, l := range h.Removed {
		if goStructFieldRe.MatchString(l) && !strings.Contains(l, "func ") {
			removedFields++
		}
	}
	if !strings.Contains(strings.Join(h.Context, "\n"), "struct {") {
		return nil
	}
	if addedFields == 0 && removedFields == 0 {
		return nil
	}
	return []Change{{Category: CatStructChange, Detail: "struct fields modified"}}
}

func goControlFlowChanges(h Hunk) []Change {
	for _, l := range append(append([]string{}, h.Added...), h.Removed...) {
		if goControlRe.MatchString(l) {
			return []Change{{Category: CatControlFlow, Detail: "control flow updated"}}
		}
	}
	return nil
}

func collectFuncs(lines []string) map[string]string {
	out := map[string]string{}
	for _, l := range lines {
		m := goFuncRe.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		out[m[2]] = strings.TrimSpace(l)
	}
	return out
}

func allComments(lines []string) bool {
	if len(lines) == 0 {
		return true
	}
	for _, l := range lines {
		ts := strings.TrimSpace(l)
		if ts == "" {
			continue
		}
		if !goCommentRe.MatchString(l) {
			return false
		}
	}
	return true
}

func formattingOnly(added, removed []string) bool {
	if len(added) == 0 || len(removed) == 0 {
		return false
	}
	addJoined := normalize(strings.Join(added, ""))
	remJoined := normalize(strings.Join(removed, ""))
	return addJoined == remJoined
}

func onlyImportLines(lines []string) bool {
	any := false
	for _, l := range lines {
		ts := strings.TrimSpace(l)
		if ts == "" || ts == "(" || ts == ")" {
			continue
		}
		if !goImportLineRe.MatchString(ts) {
			return false
		}
		any = true
	}
	return any
}

func dedupeChanges(in []Change) []Change {
	if len(in) <= 1 {
		return in
	}
	seen := map[string]bool{}
	var out []Change
	for _, c := range in {
		k := string(c.Category) + "|" + c.Detail
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, c)
	}
	return out
}

func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func hasContent(f FileDiff) bool {
	for _, h := range f.Hunks {
		if len(h.Added) > 0 || len(h.Removed) > 0 {
			return true
		}
	}
	return false
}

func hunkSizeDetail(h Hunk) string {
	return joinCount(len(h.Added), len(h.Removed))
}

func joinCount(add, rem int) string {
	b := strings.Builder{}
	b.WriteString("modified (")
	if add > 0 {
		b.WriteString("+")
		b.WriteString(itoa(add))
	}
	if add > 0 && rem > 0 {
		b.WriteString(" / ")
	}
	if rem > 0 {
		b.WriteString("-")
		b.WriteString(itoa(rem))
	}
	b.WriteString(" lines)")
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func detectLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".rs":
		return "rust"
	case ".md":
		return "markdown"
	}
	return ""
}

func isTestFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	if strings.Contains(base, ".test.") {
		return true
	}
	if strings.Contains(base, "spec.") {
		return true
	}
	return false
}

func isDependencyFile(path string) bool {
	base := filepath.Base(path)
	switch base {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "yarn.lock",
		"Cargo.toml", "Cargo.lock", "requirements.txt", "Pipfile", "Pipfile.lock",
		"pyproject.toml":
		return true
	}
	return false
}

func isConfigFile(path string) bool {
	base := filepath.Base(path)
	switch base {
	case ".golangci.yml", ".golangci.yaml", ".editorconfig", ".gitignore",
		".dockerignore", "Dockerfile", "docker-compose.yml", "docker-compose.yaml",
		"Makefile":
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".yml", ".yaml", ".toml", ".ini":
		return true
	}
	return false
}

func buildHeadline(files []FileSummary) string {
	if len(files) == 0 {
		return "No changes detected."
	}
	var added, removed, modified int
	for _, f := range files {
		switch {
		case f.IsNew:
			added++
		case f.IsDel:
			removed++
		default:
			modified++
		}
	}
	parts := []string{}
	if added > 0 {
		parts = append(parts, plural(added, "new file", "new files"))
	}
	if modified > 0 {
		parts = append(parts, plural(modified, "modified file", "modified files"))
	}
	if removed > 0 {
		parts = append(parts, plural(removed, "removed file", "removed files"))
	}
	return strings.Join(parts, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

// SortFiles sorts FileSummary slices alphabetically for stable output.
func SortFiles(files []FileSummary) {
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}
