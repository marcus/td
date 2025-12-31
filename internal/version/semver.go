package version

import (
	"strconv"
	"strings"
)

// parseSemver parses a version string into [major, minor, patch].
// Handles "v" prefix and pre-release suffixes (e.g., -beta, -rc1).
func parseSemver(v string) [3]int {
	// Strip "v" prefix
	v = strings.TrimPrefix(v, "v")

	// Strip pre-release suffix (everything after - or +)
	if idx := strings.IndexAny(v, "-+"); idx != -1 {
		v = v[:idx]
	}

	parts := strings.Split(v, ".")
	var result [3]int

	for i := 0; i < 3 && i < len(parts); i++ {
		if n, err := strconv.Atoi(parts[i]); err == nil {
			result[i] = n
		}
	}

	return result
}

// isNewer returns true if latest version is newer than current version.
func isNewer(latest, current string) bool {
	l := parseSemver(latest)
	c := parseSemver(current)

	for i := 0; i < 3; i++ {
		if l[i] > c[i] {
			return true
		}
		if l[i] < c[i] {
			return false
		}
	}

	return false // equal
}
