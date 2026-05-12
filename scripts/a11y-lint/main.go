// Command a11y-lint scans the td TUI/CLI codebase for accessibility smells.
//
// This is a narrative analyzer, not a checkbox linter. It produces severity-
// tagged findings about color usage, keybinding discoverability, icon-only
// labels, and NO_COLOR support. Output is a markdown report to stdout or to
// the path given by -o.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type finding struct {
	severity string // info | warn | high
	file     string
	line     int
	category string
	message  string
}

var (
	reHexColor   = regexp.MustCompile(`lipgloss\.Color\("#[0-9a-fA-F]{3,8}"\)`)
	reANSIRaw    = regexp.MustCompile(`"\\x1b\[[0-9;]*m"`)
	reEmojiOnly  = regexp.MustCompile(`"[\x{2300}-\x{27BF}\x{1F300}-\x{1FAFF}\x{2600}-\x{26FF}]+\s*"`)
	reNoColorEnv = regexp.MustCompile(`NO_COLOR`)
	reKeyBinding = regexp.MustCompile(`key\.NewBinding\(`)
	reKeyHelp    = regexp.MustCompile(`key\.WithHelp\(`)
)

func main() {
	out := flag.String("o", "", "write report to file instead of stdout")
	root := flag.String("root", ".", "repo root")
	flag.Parse()

	var findings []finding
	hasNoColorSupport := false

	walkErr := filepath.Walk(*root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == "vendor" || name == ".git" || name == "node_modules" || strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(*root, path)
		if !(strings.HasPrefix(rel, "pkg/monitor") || strings.HasPrefix(rel, "cmd/") || strings.HasPrefix(rel, "internal/")) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := string(data)
		if reNoColorEnv.MatchString(text) {
			hasNoColorSupport = true
		}
		lines := strings.Split(text, "\n")
		bindings := 0
		helps := 0
		for i, ln := range lines {
			if reHexColor.MatchString(ln) {
				findings = append(findings, finding{"warn", rel, i + 1, "color-contrast",
					"Hardcoded hex color via lipgloss.Color. Ensure pair has a documented contrast ratio and a high-contrast fallback for low-vision users."})
			}
			if reANSIRaw.MatchString(ln) {
				findings = append(findings, finding{"high", rel, i + 1, "raw-ansi",
					"Raw ANSI escape sequence. Bypasses lipgloss/NO_COLOR handling and breaks screen readers."})
			}
			if reEmojiOnly.MatchString(ln) {
				findings = append(findings, finding{"warn", rel, i + 1, "icon-only-label",
					"String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers."})
			}
			if reKeyBinding.MatchString(ln) {
				bindings++
			}
			if reKeyHelp.MatchString(ln) {
				helps++
			}
		}
		if bindings > 0 && helps < bindings {
			findings = append(findings, finding{"warn", rel, 0, "keybinding-help",
				fmt.Sprintf("%d key.NewBinding calls but only %d WithHelp entries. Undocumented bindings hurt keyboard discoverability.", bindings, helps)})
		}
		return nil
	})
	if walkErr != nil {
		fmt.Fprintln(os.Stderr, "walk error:", walkErr)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].severity != findings[j].severity {
			rank := map[string]int{"high": 0, "warn": 1, "info": 2}
			return rank[findings[i].severity] < rank[findings[j].severity]
		}
		if findings[i].file != findings[j].file {
			return findings[i].file < findings[j].file
		}
		return findings[i].line < findings[j].line
	})

	var b strings.Builder
	b.WriteString("# Accessibility Report — td TUI/CLI\n\n")
	b.WriteString("This report is a narrative accessibility review of the td codebase. ")
	b.WriteString("td is a terminal-first application; the surfaces that matter for a11y are the Bubble Tea monitor in `pkg/monitor` and the Cobra CLI in `cmd/`. ")
	b.WriteString("Web/HTML a11y heuristics (ARIA, alt text, semantic landmarks) do not apply here, so this analyzer targets TUI-equivalents: color contrast and theming, NO_COLOR support, keybinding discoverability, and icon-only labels.\n\n")

	b.WriteString("## NO_COLOR Environment Support\n\n")
	if hasNoColorSupport {
		b.WriteString("The codebase references `NO_COLOR`, which is the standard low-vision / screen-reader-friendly opt-out. Verify it is honored at the lipgloss/render layer, not just read into a flag.\n\n")
	} else {
		b.WriteString("**HIGH**: No reference to `NO_COLOR` was found. Low-vision users and screen-reader pipelines rely on this env var (https://no-color.org) to suppress ANSI styling. Add a check at startup that disables lipgloss color rendering when `NO_COLOR` is set.\n\n")
	}

	b.WriteString("## Findings\n\n")
	if len(findings) == 0 {
		b.WriteString("No automated findings. A manual review of color palette contrast and focus indicators is still recommended.\n\n")
	} else {
		cur := ""
		for _, f := range findings {
			if f.category != cur {
				fmt.Fprintf(&b, "\n### %s\n\n", f.category)
				cur = f.category
			}
			if f.line > 0 {
				fmt.Fprintf(&b, "- **%s** `%s:%d` — %s\n", strings.ToUpper(f.severity), f.file, f.line, f.message)
			} else {
				fmt.Fprintf(&b, "- **%s** `%s` — %s\n", strings.ToUpper(f.severity), f.file, f.message)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## Recommendations\n\n")
	b.WriteString("1. Centralize color tokens in a single theme package and define a high-contrast variant selectable via env or config.\n")
	b.WriteString("2. Honor `NO_COLOR` at the lipgloss renderer level (`lipgloss.SetColorProfile(termenv.Ascii)` when set).\n")
	b.WriteString("3. Every `key.NewBinding` should carry a matching `key.WithHelp` so the help pane is complete.\n")
	b.WriteString("4. Never use emoji or unicode symbols as the sole indicator of state — pair with a short text label.\n")
	b.WriteString("5. Avoid raw ANSI escape sequences in source; route all styling through lipgloss so it can be globally disabled.\n")

	report := b.String()
	if *out != "" {
		if err := os.WriteFile(*out, []byte(report), 0644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d findings)\n", *out, len(findings))
		return
	}
	fmt.Print(report)
}
