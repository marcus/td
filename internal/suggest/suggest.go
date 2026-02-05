// Package suggest provides fuzzy matching for CLI flag and command suggestions
// using Levenshtein distance.
package suggest

import (
	"strings"
)

// levenshtein calculates the edit distance between two strings
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Create matrix
	matrix := make([][]int, len(a)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(b)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	// Fill matrix
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(a)][len(b)]
}

// Flag finds similar flags from a list of valid flags
// Returns suggestions sorted by similarity (best first)
func Flag(unknown string, validFlags []string) []string {
	// Normalize: strip leading dashes
	unknown = strings.TrimLeft(unknown, "-")

	type scored struct {
		flag  string
		score int
	}
	var candidates []scored

	for _, valid := range validFlags {
		// Strip dashes from valid flag too
		normalized := strings.TrimLeft(valid, "-")

		dist := levenshtein(unknown, normalized)

		// Only suggest if reasonably close (within 3 edits or 50% of length)
		maxDist := max(3, len(unknown)/2)
		if dist <= maxDist {
			candidates = append(candidates, scored{valid, dist})
		}
	}

	// Sort by score (lower is better)
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score < candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Return top 3 suggestions
	var result []string
	for i := 0; i < len(candidates) && i < 3; i++ {
		result = append(result, candidates[i].flag)
	}
	return result
}

// CommonFlagAliases maps commonly attempted flags to their correct names
var CommonFlagAliases = map[string]string{
	// Description aliases
	"note":  "--description, -d",
	"notes": "--description, -d",
	"msg":   "--description, -d",

	// Comment/reason aliases
	"comment":  "--reason, -m",
	"comments": "--reason, -m",
	"message":  "--reason, -m",

	// Issue aliases
	"id":       "--issue",
	"issue-id": "--issue",
	"task":     "--issue",

	// Force/confirm aliases
	"force":   "(not supported - use confirmation prompt)",
	"confirm": "(not supported - use confirmation prompt)",
	"yes":     "(not supported - use confirmation prompt)",
	"y":       "(not supported - use confirmation prompt)",

	// Version
	"version": "use: td version",
	"v":       "use: td version",

	// Status aliases
	"state": "--status, -s",

	// Workflow hints
	"review": "use: td review <issue-id> (after td handoff)",
}

// GetFlagHint returns a hint for a commonly misused flag
func GetFlagHint(flag string) string {
	// Normalize
	flag = strings.TrimLeft(flag, "-")
	flag = strings.ToLower(flag)

	if hint, ok := CommonFlagAliases[flag]; ok {
		return hint
	}
	return ""
}
