// a11y-lint scans the td TUI/CLI codebase for accessibility smells.
//
// Heuristics (non-exhaustive):
//   - hardcoded ANSI escape sequences without NO_COLOR awareness
//   - lipgloss color usage without a high-contrast / adaptive variant
//   - emoji or icon-only labels lacking accompanying text
//   - cobra commands missing Short/Long help text
//
// Output: prose findings to stdout (or to -out file). Non-zero exit only on
// internal error; findings themselves are advisory.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Severity string

const (
	SevHigh   Severity = "high"
	SevMedium Severity = "medium"
	SevLow    Severity = "low"
)

type Finding struct {
	File           string
	Line           int
	Severity       Severity
	Category       string
	Snippet        string
	Recommendation string
}

var (
	reANSI       = regexp.MustCompile(`\\x1b\[[0-9;]+m|\\033\[[0-9;]+m`)
	reLipColor   = regexp.MustCompile(`lipgloss\.Color\(`)
	reAdaptive   = regexp.MustCompile(`lipgloss\.AdaptiveColor`)
	reEmoji      = regexp.MustCompile(`[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}]`)
	reCobraShort = regexp.MustCompile(`Short:\s*"`)
	reCobraCmd   = regexp.MustCompile(`&cobra\.Command\{`)
)

func main() {
	out := flag.String("out", "", "write report to this file (default stdout)")
	roots := flag.String("roots", "pkg/monitor,cmd", "comma-separated roots to scan")
	flag.Parse()

	var findings []Finding
	hasNoColorCheck := false

	for _, r := range strings.Split(*roots, ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		_ = filepath.WalkDir(r, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if strings.Contains(string(data), "NO_COLOR") {
				hasNoColorCheck = true
			}
			scanFile(path, string(data), &findings)
			return nil
		})
	}

	report := renderReport(findings, hasNoColorCheck)

	if *out == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %d findings to %s\n", len(findings), *out)
}

func scanFile(path, content string, out *[]Finding) {
	lines := strings.Split(content, "\n")
	inCobraBlock := false
	cobraStart := 0
	for i, line := range lines {
		ln := i + 1

		if reANSI.MatchString(line) {
			*out = append(*out, Finding{
				File: path, Line: ln, Severity: SevHigh,
				Category:       "color/no-color",
				Snippet:        strings.TrimSpace(line),
				Recommendation: "Hardcoded ANSI escape; route color through lipgloss + honor NO_COLOR (https://no-color.org).",
			})
		}

		if reLipColor.MatchString(line) && !reAdaptive.MatchString(line) {
			// flag plain Color(...) usages — they don't adapt to light terminals
			*out = append(*out, Finding{
				File: path, Line: ln, Severity: SevMedium,
				Category:       "contrast/adaptive",
				Snippet:        strings.TrimSpace(line),
				Recommendation: "Prefer lipgloss.AdaptiveColor{Light, Dark} so contrast holds on both light and dark terminals.",
			})
		}

		if reEmoji.MatchString(line) && looksLikeIconOnlyLabel(line) {
			*out = append(*out, Finding{
				File: path, Line: ln, Severity: SevMedium,
				Category:       "icon-only label",
				Snippet:        strings.TrimSpace(line),
				Recommendation: "Pair the icon with a short text label so screen readers / NO_COLOR users get the meaning.",
			})
		}

		if reCobraCmd.MatchString(line) {
			inCobraBlock = true
			cobraStart = ln
			continue
		}
		if inCobraBlock {
			if strings.TrimSpace(line) == "}" {
				inCobraBlock = false
			} else if reCobraShort.MatchString(line) {
				inCobraBlock = false // satisfied
			} else if ln-cobraStart > 40 {
				*out = append(*out, Finding{
					File: path, Line: cobraStart, Severity: SevLow,
					Category:       "cli help",
					Snippet:        "cobra.Command{...} block",
					Recommendation: "Cobra command appears to lack a Short: help string within ~40 lines; --help output suffers.",
				})
				inCobraBlock = false
			}
		}
	}
}

func looksLikeIconOnlyLabel(line string) bool {
	// crude: a quoted string that is just an emoji + maybe whitespace
	q := regexp.MustCompile(`"([^"]{1,8})"`)
	for _, m := range q.FindAllStringSubmatch(line, -1) {
		s := strings.TrimSpace(m[1])
		if s == "" {
			continue
		}
		stripped := reEmoji.ReplaceAllString(s, "")
		if strings.TrimSpace(stripped) == "" {
			return true
		}
	}
	return false
}

func renderReport(findings []Finding, hasNoColor bool) string {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return sevRank(findings[i].Severity) < sevRank(findings[j].Severity)
		}
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	byCat := map[string]int{}
	for _, f := range findings {
		byCat[f.Category]++
	}

	var b strings.Builder
	fmt.Fprintln(&b, "# Accessibility Report — td")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "This is a narrative analysis of TUI/CLI accessibility heuristics. It is")
	fmt.Fprintln(&b, "intentionally not a checkbox list — each finding includes context and a")
	fmt.Fprintln(&b, "recommendation. Treat severities as guidance, not gates.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Environment signals")
	fmt.Fprintln(&b)
	if hasNoColor {
		fmt.Fprintln(&b, "- NO_COLOR is referenced somewhere in the scanned tree — good. Verify the")
		fmt.Fprintln(&b, "  check is honored at every render path, not just startup.")
	} else {
		fmt.Fprintln(&b, "- NO_COLOR is **not** referenced in the scanned tree. Users who set")
		fmt.Fprintln(&b, "  `NO_COLOR=1` (per https://no-color.org) expect colored output to be")
		fmt.Fprintln(&b, "  suppressed. Add a single check at TUI bootstrap and gate lipgloss styles.")
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Summary by category")
	fmt.Fprintln(&b)
	cats := make([]string, 0, len(byCat))
	for c := range byCat {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	for _, c := range cats {
		fmt.Fprintf(&b, "- %s: %d finding(s)\n", c, byCat[c])
	}
	if len(findings) == 0 {
		fmt.Fprintln(&b, "- No automated findings. Manual review still recommended.")
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Findings")
	fmt.Fprintln(&b)
	if len(findings) == 0 {
		fmt.Fprintln(&b, "No findings to report.")
		return b.String()
	}
	for _, f := range findings {
		fmt.Fprintf(&b, "### [%s] %s — %s:%d\n\n", strings.ToUpper(string(f.Severity)), f.Category, f.File, f.Line)
		fmt.Fprintf(&b, "    %s\n\n", truncate(f.Snippet, 160))
		fmt.Fprintf(&b, "%s\n\n", f.Recommendation)
	}
	return b.String()
}

func sevRank(s Severity) int {
	switch s {
	case SevHigh:
		return 0
	case SevMedium:
		return 1
	case SevLow:
		return 2
	}
	return 3
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
