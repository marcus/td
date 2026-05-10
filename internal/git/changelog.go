package git

import (
	"regexp"
	"strings"
)

// CommitInfo holds parsed information about a single git commit.
type CommitInfo struct {
	Hash       string
	Subject    string
	Body       string
	Type       string // feat, fix, chore, etc.
	Scope      string
	IsBreaking bool
}

// conventionalRe matches "type(scope)!: description" or "type!: description" or "type: description"
var conventionalRe = regexp.MustCompile(`^(\w+)(?:\(([^)]*)\))?(!)?\s*:\s*(.*)$`)

// ParseConventionalCommit extracts type, scope, and breaking flag from a
// conventional commit subject line. Non-conventional commits get Type="other".
func ParseConventionalCommit(subject, body string) CommitInfo {
	ci := CommitInfo{Subject: subject, Body: body}

	m := conventionalRe.FindStringSubmatch(subject)
	if m == nil {
		ci.Type = "other"
		return ci
	}

	ci.Type = strings.ToLower(m[1])
	ci.Scope = m[2]
	if m[3] == "!" {
		ci.IsBreaking = true
	}
	// Rewrite subject to the description part only
	ci.Subject = m[4]

	// BREAKING CHANGE trailer in body
	if !ci.IsBreaking && strings.Contains(body, "BREAKING CHANGE") {
		ci.IsBreaking = true
	}

	return ci
}

// GetLatestTag returns the most recent reachable tag, or "" if none exist.
func GetLatestTag() (string, error) {
	out, err := runGit("describe", "--tags", "--abbrev=0")
	if err != nil {
		// No tags — not an error for our purposes
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// GetCommitLog returns commits between fromRef and toRef (exclusive..inclusive).
// If fromRef is empty, returns all commits up to toRef.
func GetCommitLog(fromRef, toRef string) ([]CommitInfo, error) {
	// Format: hash<SEP>subject<SEP>body (body uses %x00 as record separator)
	const sep = "<SEP>"
	format := "%H" + sep + "%s" + sep + "%b%x00"

	var rangeSpec string
	if fromRef == "" {
		rangeSpec = toRef
	} else {
		rangeSpec = fromRef + ".." + toRef
	}

	out, err := runGit("log", "--format="+format, rangeSpec)
	if err != nil {
		return nil, err
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}

	records := strings.Split(out, "\x00")
	var commits []CommitInfo

	for _, rec := range records {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, sep, 3)
		if len(parts) < 2 {
			continue
		}

		hash := parts[0]
		subject := parts[1]
		var body string
		if len(parts) == 3 {
			body = strings.TrimSpace(parts[2])
		}

		ci := ParseConventionalCommit(subject, body)
		ci.Hash = hash
		commits = append(commits, ci)
	}

	return commits, nil
}

// typeTitles maps commit types to human-readable section headers.
var typeTitles = map[string]string{
	"feat":     "Features",
	"fix":      "Bug Fixes",
	"perf":     "Performance",
	"refactor": "Improvements",
	"docs":     "Documentation",
	"test":     "Tests",
	"chore":    "Chores",
	"ci":       "CI",
	"build":    "Build",
	"other":    "Other",
}

// GroupCommitsByType buckets commits by their conventional type.
func GroupCommitsByType(commits []CommitInfo) map[string][]CommitInfo {
	grouped := make(map[string][]CommitInfo)
	for _, c := range commits {
		t := c.Type
		if _, ok := typeTitles[t]; !ok {
			t = "other"
		}
		grouped[t] = append(grouped[t], c)
	}
	return grouped
}
