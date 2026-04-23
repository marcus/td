package e2e

import (
	"fmt"
	"math/rand"
	"strings"
)

// Edge-case strings for adversarial testing.
var edgeStrings = []string{
	"",                                // empty
	"x",                               // single char
	strings.Repeat("A", 1200),         // very long
	"\xf0\x9f\x94\xa5\xf0\x9f\x90\x9b\xe2\x9c\x85\xf0\x9f\x9a\x80\xf0\x9f\x92\x80\xf0\x9f\x8e\x89", // emoji
	"\u6d4b\u8bd5\u4e2d\u6587\u6570\u636e\u5904\u7406",                                                  // CJK
	"\u0645\u0631\u062d\u0628\u0627 \u0628\u0627\u0644\u0639\u0627\u0644\u0645",                          // RTL Arabic
	"line one\nline two\nline three",
	"it's a test with 'single quotes'",
	`she said "hello world"`,
	`path\\to\\file and \\n not a newline`,
	"'; DROP TABLE issues; --",
	`Robert"); DELETE FROM sync_events WHERE ("1"="1`,
	"emoji\xf0\x9f\x94\xa5 with 'quotes' and \"doubles\" and \\backslash",
	"\ttabs\tand   spaces   ",
	"%s %d %x %n %%",
	"<script>alert('xss')</script>",
	`{"key": "value", "nested": {"a": 1}}`,
}

var chaosPrefixes = []string{
	"Fix", "Add", "Refactor", "Update", "Implement", "Remove", "Optimize", "Debug", "Test", "Document",
	"Investigate", "Redesign", "Migrate", "Configure", "Automate", "Validate", "Extend", "Simplify",
	"Extract", "Consolidate",
}

var chaosSubjects = []string{
	"login flow", "database queries", "API endpoint", "error handling", "caching layer",
	"auth middleware", "build pipeline", "test suite", "monitoring", "rate limiter",
	"search index", "file uploads", "notification system", "user preferences", "audit log",
	"session management", "data export", "webhook handler", "retry logic", "config loader",
}

var chaosSuffixes = []string{
	"for production", "across services", "in staging", "with fallback", "using new API",
	"per requirements", "after migration", "before release", "with tests", "for scale",
	"on timeout", "under load", "with retry", "for compliance", "in background",
}

var chaosSentences = []string{
	"This needs careful attention.",
	"The current implementation has edge cases.",
	"Performance benchmarks show room for improvement.",
	"Users have reported intermittent failures.",
	"The design doc covers the approach in detail.",
	"We should consider backward compatibility.",
	"This blocks the upcoming release.",
	"Unit tests should cover the critical paths.",
	"The root cause appears to be a race condition.",
	"Integration tests pass but manual testing reveals issues.",
	"The feature flag should gate the rollout.",
	"Monitoring shows increased latency after deploy.",
	"Code review feedback has been addressed.",
	"The dependency upgrade introduces breaking changes.",
	"This aligns with the Q3 roadmap.",
}

var chaosLabels = []string{
	"bug", "feature", "enhancement", "refactor", "docs", "testing", "infra", "security",
	"performance", "ux", "tech-debt", "ci", "backend", "frontend", "api", "urgent", "blocked",
	"needs-design", "needs-review", "good-first-issue",
}

var chaosWords = []string{
	"the", "system", "should", "handle", "errors", "gracefully", "when", "processing",
	"large", "batches", "of", "data", "from", "upstream", "services", "that", "may", "timeout",
	"or", "return", "unexpected", "results", "during", "peak", "traffic", "hours", "while",
	"maintaining", "consistency", "across", "all", "replicas", "in", "the", "cluster",
	"additionally", "we", "need", "to", "ensure", "proper", "logging", "and", "alerting",
	"for", "any", "anomalies", "detected", "by", "the", "monitoring", "pipeline",
	"this", "requires", "careful", "coordination", "between", "teams", "and", "thorough",
	"testing", "before", "deployment", "to", "production", "environments",
}

var handoffItems = []string{
	"Implemented core logic", "Added error handling", "Wrote unit tests",
	"Updated config", "Fixed edge case", "Refactored helper", "Added logging",
	"Reviewed upstream changes", "Verified in staging", "Updated dependencies",
	"Need to add integration tests", "Config needs review", "Edge case unhandled",
	"Performance untested", "Docs incomplete",
}

var acceptanceCriteria = []string{
	"All tests pass", "No regressions in CI", "Code review approved",
	"Documentation updated", "Performance meets SLA", "Security scan clean",
	"Feature flag works", "Rollback tested", "Monitoring configured", "Load test passed",
}

// maybeEdgeData returns an edge-case string ~15% of the time, or empty string.
// Returns (edgeString, used).
func maybeEdgeData(rng *rand.Rand) (string, bool) {
	if rng.Intn(100) < 15 {
		return edgeStrings[rng.Intn(len(edgeStrings))], true
	}
	return "", false
}

// randTitle generates a random issue title.
func randTitle(rng *rand.Rand, maxLen int) (string, bool) {
	if s, used := maybeEdgeData(rng); used {
		if s == "" {
			s = fmt.Sprintf("edge-case-empty-%d", rng.Intn(1000))
		}
		if len(s) > maxLen {
			s = s[:maxLen]
		}
		return s, true
	}
	prefix := chaosPrefixes[rng.Intn(len(chaosPrefixes))]
	subject := chaosSubjects[rng.Intn(len(chaosSubjects))]
	suffix := chaosSuffixes[rng.Intn(len(chaosSuffixes))]
	title := fmt.Sprintf("%s %s %s", prefix, subject, suffix)
	// Pad to at least 30 chars
	for len(title) < 30 {
		title += fmt.Sprintf(" %08x", rng.Uint32())
	}
	if len(title) > maxLen {
		title = title[:maxLen]
	}
	return title, false
}

// randDescription generates a random description with paragraphs.
func randDescription(rng *rand.Rand, paragraphs int) (string, bool) {
	if s, used := maybeEdgeData(rng); used {
		return s, true
	}
	if paragraphs <= 0 {
		paragraphs = 1 + rng.Intn(5)
	}
	var paras []string
	for range paragraphs {
		nSentences := 2 + rng.Intn(4)
		var sents []string
		for range nSentences {
			sents = append(sents, chaosSentences[rng.Intn(len(chaosSentences))])
		}
		paras = append(paras, strings.Join(sents, " "))
	}
	return strings.Join(paras, "\n\n"), false
}

// randLabels generates a comma-separated list of random unique labels.
func randLabels(rng *rand.Rand, count int) string {
	if count <= 0 {
		count = 1 + rng.Intn(8)
	}
	seen := make(map[string]bool)
	var result []string
	for range count {
		label := chaosLabels[rng.Intn(len(chaosLabels))]
		if !seen[label] {
			seen[label] = true
			result = append(result, label)
		}
	}
	return strings.Join(result, ",")
}

// randComment generates random text from word pool.
func randComment(rng *rand.Rand, minWords, maxWords int) (string, bool) {
	if s, used := maybeEdgeData(rng); used {
		return s, true
	}
	count := minWords + rng.Intn(maxWords-minWords+1)
	words := make([]string, count)
	for i := range words {
		words[i] = chaosWords[rng.Intn(len(chaosWords))]
	}
	return strings.Join(words, " "), false
}

// randAcceptance generates acceptance criteria items.
func randAcceptance(rng *rand.Rand, items int) (string, bool) {
	if s, used := maybeEdgeData(rng); used {
		return s, true
	}
	if items <= 0 {
		items = 1 + rng.Intn(5)
	}
	var lines []string
	for range items {
		lines = append(lines, "- "+acceptanceCriteria[rng.Intn(len(acceptanceCriteria))])
	}
	return strings.Join(lines, "\n"), false
}

// randHandoffItems generates comma-separated handoff items.
func randHandoffItems(rng *rand.Rand, count int) (string, bool) {
	if s, used := maybeEdgeData(rng); used {
		return s, true
	}
	if count <= 0 {
		count = 1 + rng.Intn(5)
	}
	result := make([]string, count)
	for i := range result {
		result[i] = handoffItems[rng.Intn(len(handoffItems))]
	}
	return strings.Join(result, ","), false
}

// randChoice picks a random element from choices.
func randChoice(rng *rand.Rand, choices ...string) string {
	return choices[rng.Intn(len(choices))]
}

// randWords generates n random words joined by spaces.
func randWords(rng *rand.Rand, n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = chaosWords[rng.Intn(len(chaosWords))]
	}
	return strings.Join(words, " ")
}
