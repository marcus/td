package git

import (
	"fmt"
	"strings"
	"unicode"
)

// ApprovedTypes lists the conventional commit types this project accepts.
var ApprovedTypes = []string{
	"feat", "fix", "docs", "style", "refactor",
	"perf", "test", "build", "ci", "chore", "revert",
}

// CommitMessage is a parsed conventional commit.
type CommitMessage struct {
	Type        string // e.g. "feat"
	Scope       string // e.g. "parser", may be empty
	Breaking    bool   // trailing "!" on type/scope
	Description string // first-line description after ": "
	Body        string // optional body paragraphs
	Trailers    []Trailer
}

// Trailer is a git trailer (key: value).
type Trailer struct {
	Key   string
	Value string
}

// MaxSubjectLen is the maximum allowed length for the subject line.
const MaxSubjectLen = 72

// ValidationError describes why a commit message is invalid.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ParseCommitMessage parses a raw commit message string into a CommitMessage.
// It returns an error only if the message is completely unparseable (empty).
func ParseCommitMessage(raw string) (*CommitMessage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty commit message")
	}

	lines := strings.Split(raw, "\n")
	subject := lines[0]

	cm := &CommitMessage{}

	// Parse subject: type[(scope)][!]: description
	colonIdx := strings.Index(subject, ": ")
	if colonIdx < 0 {
		// Not a conventional commit — store entire subject as description
		cm.Description = subject
	} else {
		prefix := subject[:colonIdx]
		cm.Description = subject[colonIdx+2:]

		// Check for breaking indicator
		if strings.HasSuffix(prefix, "!") {
			cm.Breaking = true
			prefix = prefix[:len(prefix)-1]
		}

		// Check for scope
		if lp := strings.Index(prefix, "("); lp >= 0 {
			rp := strings.Index(prefix, ")")
			if rp > lp {
				cm.Type = prefix[:lp]
				cm.Scope = prefix[lp+1 : rp]
			} else {
				cm.Type = prefix
			}
		} else {
			cm.Type = prefix
		}
	}

	// Separate body and trailers from remaining lines
	if len(lines) > 1 {
		rest := lines[1:]
		bodyLines, trailers := splitBodyTrailers(rest)
		cm.Body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
		cm.Trailers = trailers
	}

	return cm, nil
}

// splitBodyTrailers separates body text from trailing key: value trailers.
// Trailers are contiguous key: value lines at the end, preceded by a blank line.
func splitBodyTrailers(lines []string) (body []string, trailers []Trailer) {
	// Find the last contiguous block of trailer-like lines at the end
	trailerStart := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			// blank line before trailers — this is the separator
			break
		}
		if isTrailerLine(line) {
			trailerStart = i
		} else {
			// Non-trailer, non-blank line — no trailers block
			trailerStart = len(lines)
			break
		}
	}

	// Need a blank line separator before trailers (unless trailers start right after subject)
	if trailerStart < len(lines) && trailerStart > 0 {
		prev := strings.TrimSpace(lines[trailerStart-1])
		if prev != "" {
			// No blank separator — treat as body
			return lines, nil
		}
	}

	for i := trailerStart; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if k, v, ok := parseTrailer(line); ok {
			trailers = append(trailers, Trailer{Key: k, Value: v})
		}
	}

	body = lines[:trailerStart]
	return body, trailers
}

func isTrailerLine(line string) bool {
	_, _, ok := parseTrailer(line)
	return ok
}

func parseTrailer(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ": ")
	if idx <= 0 {
		return "", "", false
	}
	key = line[:idx]
	// Trailer keys must be word chars and hyphens
	for _, r := range key {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
			return "", "", false
		}
	}
	value = strings.TrimSpace(line[idx+2:])
	return key, value, true
}

// Validate checks whether a CommitMessage conforms to project rules.
// Returns a slice of errors (empty means valid).
func Validate(cm *CommitMessage) []ValidationError {
	var errs []ValidationError

	if cm.Type == "" {
		errs = append(errs, ValidationError{Field: "type", Message: "type is required"})
	} else if !isApprovedType(strings.ToLower(cm.Type)) {
		errs = append(errs, ValidationError{Field: "type", Message: fmt.Sprintf("unknown type %q; approved: %s", cm.Type, strings.Join(ApprovedTypes, ", "))})
	}

	if strings.TrimSpace(cm.Description) == "" {
		errs = append(errs, ValidationError{Field: "description", Message: "description is required"})
	}

	subject := FormatSubject(cm)
	if len(subject) > MaxSubjectLen {
		errs = append(errs, ValidationError{Field: "subject", Message: fmt.Sprintf("subject is %d chars, max %d", len(subject), MaxSubjectLen)})
	}

	return errs
}

// Normalize applies auto-fixes to a CommitMessage in place:
//   - lowercase the type
//   - lowercase first char of description
//   - strip trailing period from description
//   - normalize trailer casing (Co-Authored-By, Signed-Off-By)
func Normalize(cm *CommitMessage) {
	cm.Type = strings.ToLower(cm.Type)
	cm.Description = strings.TrimSpace(cm.Description)

	// Strip trailing period
	cm.Description = strings.TrimRight(cm.Description, ".")

	// Lowercase first character of description
	if len(cm.Description) > 0 {
		runes := []rune(cm.Description)
		runes[0] = unicode.ToLower(runes[0])
		cm.Description = string(runes)
	}

	// Normalize well-known trailer keys
	for i := range cm.Trailers {
		cm.Trailers[i].Key = normalizeTrailerKey(cm.Trailers[i].Key)
	}
}

// normalizeTrailerKey canonicalizes well-known trailer keys.
var knownTrailerKeys = map[string]string{
	"co-authored-by": "Co-Authored-By",
	"signed-off-by":  "Signed-Off-By",
}

func normalizeTrailerKey(key string) string {
	if canonical, ok := knownTrailerKeys[strings.ToLower(key)]; ok {
		return canonical
	}
	return key
}

func isApprovedType(t string) bool {
	for _, a := range ApprovedTypes {
		if a == t {
			return true
		}
	}
	return false
}

// FormatSubject renders just the subject line.
func FormatSubject(cm *CommitMessage) string {
	var sb strings.Builder
	sb.WriteString(cm.Type)
	if cm.Scope != "" {
		sb.WriteByte('(')
		sb.WriteString(cm.Scope)
		sb.WriteByte(')')
	}
	if cm.Breaking {
		sb.WriteByte('!')
	}
	sb.WriteString(": ")
	sb.WriteString(cm.Description)
	return sb.String()
}

// FormatCommitMessage renders a full commit message string.
func FormatCommitMessage(cm *CommitMessage) string {
	var sb strings.Builder
	sb.WriteString(FormatSubject(cm))

	if cm.Body != "" {
		sb.WriteString("\n\n")
		sb.WriteString(cm.Body)
	}

	if len(cm.Trailers) > 0 {
		sb.WriteString("\n\n")
		for i, t := range cm.Trailers {
			if i > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(t.Key)
			sb.WriteString(": ")
			sb.WriteString(t.Value)
		}
	}

	sb.WriteByte('\n')
	return sb.String()
}
