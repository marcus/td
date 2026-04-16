package git

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/marcus/td/internal/models"
)

// CommitType is the normalized conventional commit type accepted by td.
type CommitType string

const (
	CommitTypeFeat  CommitType = "feat"
	CommitTypeFix   CommitType = "fix"
	CommitTypeDocs  CommitType = "docs"
	CommitTypeTest  CommitType = "test"
	CommitTypeChore CommitType = "chore"
	CommitTypeCI    CommitType = "ci"
)

// CommitMessageOptions control how commit subjects are normalized.
type CommitMessageOptions struct {
	IssueID   string
	IssueType models.Type
	Type      CommitType
}

var (
	commitSubjectPrefixPattern   = regexp.MustCompile(`^\s*([A-Za-z]+)\s*(?:\(\s*([^)]+?)\s*\))?\s*!?\s*:\s*(.*)$`)
	commitSubjectIDSuffixPattern = regexp.MustCompile(`(?i)\s*\(\s*(td-[0-9a-f]{4,8})\s*\)\s*$`)
	commitSubjectAnyIDSuffix     = regexp.MustCompile(`(?i)\s*\(\s*(td-[^)\s]+)\s*\)\s*$`)
	validCommitIssueIDPattern    = regexp.MustCompile(`(?i)^td-[0-9a-f]{4,8}$`)
	bareCommitIssueIDPattern     = regexp.MustCompile(`(?i)^[0-9a-f]{4,8}$`)
	autosquashSubjectPattern     = regexp.MustCompile(`^(fixup|squash|amend)!\s+`)
	mergeSubjectPattern          = regexp.MustCompile(`^Merge (?:(?:branch|branches)|(?:remote-tracking branch|remote-tracking branches)|tag|commit) '`)
	revertSubjectPattern         = regexp.MustCompile(`^Revert\s+"`)
)

var commitTypeAliases = map[string]CommitType{
	"feat":    CommitTypeFeat,
	"feature": CommitTypeFeat,
	"fix":     CommitTypeFix,
	"bug":     CommitTypeFix,
	"bugfix":  CommitTypeFix,
	"docs":    CommitTypeDocs,
	"doc":     CommitTypeDocs,
	"test":    CommitTypeTest,
	"tests":   CommitTypeTest,
	"chore":   CommitTypeChore,
	"ci":      CommitTypeCI,
}

type parsedCommitSubject struct {
	Type    CommitType
	Scope   string
	Summary string
	IssueID string
}

// NormalizeCommitType returns a canonical lowercase commit type.
func NormalizeCommitType(raw string) (CommitType, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return "", nil
	}

	if canonical, ok := commitTypeAliases[normalized]; ok {
		return canonical, nil
	}

	return "", fmt.Errorf("unsupported commit type %q: use %s", strings.TrimSpace(raw), supportedCommitTypes())
}

// CommitTypeAllowsNoIssue reports whether a commit type may omit a td issue suffix.
func CommitTypeAllowsNoIssue(commitType CommitType) bool {
	switch commitType {
	case CommitTypeDocs, CommitTypeTest, CommitTypeChore, CommitTypeCI:
		return true
	default:
		return false
	}
}

// DefaultCommitType maps td issue types to the closest supported commit type.
func DefaultCommitType(issueType models.Type) (CommitType, error) {
	switch issueType {
	case models.TypeFeature:
		return CommitTypeFeat, nil
	case models.TypeBug:
		return CommitTypeFix, nil
	case models.TypeTask, models.TypeChore, models.TypeEpic:
		return CommitTypeChore, nil
	default:
		return "", fmt.Errorf("cannot infer commit type from issue type %q: use --type %s", issueType, supportedCommitTypes())
	}
}

// NormalizeCommitIssueID returns a canonical lowercase td issue ID.
func NormalizeCommitIssueID(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}

	if bareCommitIssueIDPattern.MatchString(trimmed) {
		trimmed = "td-" + trimmed
	}

	trimmed = strings.ToLower(trimmed)
	if !validCommitIssueIDPattern.MatchString(trimmed) {
		return "", fmt.Errorf("invalid issue ID %q: expected td-<hex>", raw)
	}

	return trimmed, nil
}

// ExtractCommitIssueID returns the trailing td issue ID referenced by a subject.
func ExtractCommitIssueID(subject string) (string, error) {
	parsed, err := parseCommitSubject(subject)
	if err != nil {
		return "", err
	}
	return parsed.IssueID, nil
}

// ShouldSkipCommitMessageNormalization reports whether a subject should be left
// alone because Git generated it for a special flow.
func ShouldSkipCommitMessageNormalization(subject string) bool {
	trimmed := strings.TrimSpace(subject)
	if trimmed == "" {
		return false
	}

	return autosquashSubjectPattern.MatchString(trimmed) ||
		mergeSubjectPattern.MatchString(trimmed) ||
		revertSubjectPattern.MatchString(trimmed)
}

// NormalizeCommitSubject rewrites a subject into a canonical conventional
// commit line. When an issue ID is available, it appends (td-<id>).
func NormalizeCommitSubject(subject string, opts CommitMessageOptions) (string, error) {
	parsed, err := parseCommitSubject(subject)
	if err != nil {
		return "", err
	}

	issueID, err := resolveCommitIssueID(parsed.IssueID, opts.IssueID)
	if err != nil {
		return "", err
	}

	commitType, err := resolveCommitType(parsed.Type, opts)
	if err != nil {
		return "", err
	}
	if issueID == "" && !CommitTypeAllowsNoIssue(commitType) {
		return "", fmt.Errorf("commit type %q requires a td issue: use --issue, add (td-<id>), or choose docs|test|chore|ci for no-issue commits", commitType)
	}

	return formatCommitSubject(commitType, parsed.Scope, parsed.Summary, issueID), nil
}

// NormalizeCommitMessage rewrites only the first line of a full commit message.
func NormalizeCommitMessage(message string, opts CommitMessageOptions) (string, error) {
	subject, remainder := splitCommitMessage(message)
	if ShouldSkipCommitMessageNormalization(subject) {
		return message, nil
	}

	normalizedSubject, err := NormalizeCommitSubject(subject, opts)
	if err != nil {
		return "", err
	}

	return normalizedSubject + remainder, nil
}

// RewriteCommitMessageFile normalizes the first line of a commit message file.
func RewriteCommitMessageFile(path string, opts CommitMessageOptions) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	normalized, err := NormalizeCommitMessage(string(data), opts)
	if err != nil {
		return err
	}

	if normalized == string(data) {
		return nil
	}

	return os.WriteFile(path, []byte(normalized), info.Mode())
}

func resolveCommitType(parsedType CommitType, opts CommitMessageOptions) (CommitType, error) {
	if opts.Type != "" {
		return NormalizeCommitType(string(opts.Type))
	}
	if parsedType != "" {
		return parsedType, nil
	}
	if opts.IssueType == "" {
		return "", fmt.Errorf("missing commit type: use a conventional subject, pass --type %s, or focus an issue", supportedCommitTypes())
	}
	return DefaultCommitType(opts.IssueType)
}

func resolveCommitIssueID(parsedIssueID, optionIssueID string) (string, error) {
	normalizedOptionID, err := NormalizeCommitIssueID(optionIssueID)
	if err != nil {
		return "", err
	}

	if normalizedOptionID == "" {
		return parsedIssueID, nil
	}
	if parsedIssueID == "" || parsedIssueID == normalizedOptionID {
		return normalizedOptionID, nil
	}

	return "", fmt.Errorf("commit subject references %s but resolved issue is %s", parsedIssueID, normalizedOptionID)
}

func parseCommitSubject(subject string) (parsedCommitSubject, error) {
	remaining := strings.TrimSpace(subject)
	if remaining == "" {
		return parsedCommitSubject{}, fmt.Errorf("missing commit subject")
	}

	issueID, stripped, err := stripTrailingIssueIDs(remaining)
	if err != nil {
		return parsedCommitSubject{}, err
	}
	remaining = stripped

	commitType := CommitType("")
	scope := ""
	if matches := commitSubjectPrefixPattern.FindStringSubmatch(remaining); matches != nil {
		commitType, err = NormalizeCommitType(matches[1])
		if err != nil {
			return parsedCommitSubject{}, err
		}
		scope = cleanCommitScope(matches[2])
		remaining = matches[3]
	}

	summary := cleanCommitSummary(remaining)
	if summary == "" {
		return parsedCommitSubject{}, fmt.Errorf("missing commit summary")
	}

	return parsedCommitSubject{
		Type:    commitType,
		Scope:   scope,
		Summary: summary,
		IssueID: issueID,
	}, nil
}

func stripTrailingIssueIDs(subject string) (string, string, error) {
	var ids []string
	remaining := strings.TrimSpace(subject)

	for {
		if matches := commitSubjectIDSuffixPattern.FindStringSubmatchIndex(remaining); matches != nil {
			id, err := NormalizeCommitIssueID(remaining[matches[2]:matches[3]])
			if err != nil {
				return "", "", err
			}

			ids = append(ids, id)
			remaining = strings.TrimSpace(remaining[:matches[0]])
			continue
		}

		if invalidMatches := commitSubjectAnyIDSuffix.FindStringSubmatchIndex(remaining); invalidMatches != nil {
			_, err := NormalizeCommitIssueID(remaining[invalidMatches[2]:invalidMatches[3]])
			if err != nil {
				return "", "", err
			}
		}

		break
	}

	issueID, err := dedupeCommitIssueIDs(ids)
	if err != nil {
		return "", "", err
	}

	return issueID, remaining, nil
}

func dedupeCommitIssueIDs(ids []string) (string, error) {
	if len(ids) == 0 {
		return "", nil
	}

	var first string
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if first == "" {
			first = id
		}
		seen[id] = struct{}{}
	}

	if len(seen) > 1 {
		ordered := make([]string, 0, len(seen))
		for _, id := range ids {
			if len(ordered) > 0 && ordered[len(ordered)-1] == id {
				continue
			}
			if containsString(ordered, id) {
				continue
			}
			ordered = append(ordered, id)
		}
		return "", fmt.Errorf("commit subject references multiple issue IDs: %s", strings.Join(ordered, ", "))
	}

	return first, nil
}

func formatCommitSubject(commitType CommitType, scope, summary, issueID string) string {
	var subject strings.Builder
	subject.WriteString(string(commitType))
	if scope != "" {
		subject.WriteString("(")
		subject.WriteString(scope)
		subject.WriteString(")")
	}
	subject.WriteString(": ")
	subject.WriteString(summary)
	if issueID != "" {
		subject.WriteString(" (")
		subject.WriteString(issueID)
		subject.WriteString(")")
	}
	return subject.String()
}

func cleanCommitSummary(summary string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(summary)), " ")
}

func cleanCommitScope(scope string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(scope)), " ")
}

func splitCommitMessage(message string) (string, string) {
	idx := strings.Index(message, "\n")
	if idx == -1 {
		return message, ""
	}

	lineEnd := idx
	if idx > 0 && message[idx-1] == '\r' {
		lineEnd = idx - 1
	}

	return message[:lineEnd], message[lineEnd:]
}

func supportedCommitTypes() string {
	return "feat|fix|docs|test|chore|ci"
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
