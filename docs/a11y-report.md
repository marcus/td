# Accessibility Report — td TUI/CLI

This report is a narrative accessibility review of the td codebase. td is a terminal-first application; the surfaces that matter for a11y are the Bubble Tea monitor in `pkg/monitor` and the Cobra CLI in `cmd/`. Web/HTML a11y heuristics (ARIA, alt text, semantic landmarks) do not apply here, so this analyzer targets TUI-equivalents: color contrast and theming, NO_COLOR support, keybinding discoverability, and icon-only labels.

## NO_COLOR Environment Support

**HIGH**: No reference to `NO_COLOR` was found. Low-vision users and screen-reader pipelines rely on this env var (https://no-color.org) to suppress ANSI styling. Add a check at startup that disables lipgloss color rendering when `NO_COLOR` is set.

## Findings


### raw-ansi

- **HIGH** `pkg/monitor/overlay_test.go:82` — Raw ANSI escape sequence. Bypasses lipgloss/NO_COLOR handling and breaks screen readers.
- **HIGH** `pkg/monitor/overlay_test.go:239` — Raw ANSI escape sequence. Bypasses lipgloss/NO_COLOR handling and breaks screen readers.
- **HIGH** `pkg/monitor/overlay_test.go:242` — Raw ANSI escape sequence. Bypasses lipgloss/NO_COLOR handling and breaks screen readers.
- **HIGH** `pkg/monitor/overlay_test.go:245` — Raw ANSI escape sequence. Bypasses lipgloss/NO_COLOR handling and breaks screen readers.
- **HIGH** `pkg/monitor/styles.go:360` — Raw ANSI escape sequence. Bypasses lipgloss/NO_COLOR handling and breaks screen readers.
- **HIGH** `pkg/monitor/styles.go:361` — Raw ANSI escape sequence. Bypasses lipgloss/NO_COLOR handling and breaks screen readers.

### icon-only-label

- **WARN** `cmd/board.go:446` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `cmd/board.go:448` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `cmd/board.go:450` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `cmd/board.go:452` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `cmd/board.go:454` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `cmd/board.go:456` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `cmd/stats_analytics.go:26` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `cmd/stats_analytics.go:27` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `cmd/stats_analytics.go:28` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output.go:337` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output.go:338` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output.go:339` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output.go:340` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output.go:341` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output_test.go:578` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output_test.go:579` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output_test.go:580` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output_test.go:581` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output_test.go:582` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output_test.go:712` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output_test.go:726` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/output_test.go:740` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/tree_test.go:28` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/tree_test.go:57` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/tree_test.go:62` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/tree_test.go:67` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/tree_test.go:222` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/tree_test.go:233` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `internal/output/tree_test.go:238` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/kanban.go:331` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/kanban.go:337` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/kanban.go:453` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/kanban.go:462` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/kanban.go:464` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/markdown.go:177` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/markdown.go:295` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/markdown.go:296` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/markdown.go:297` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/markdown.go:319` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/markdown.go:437` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/markdown.go:438` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/markdown.go:439` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/styles.go:103` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/styles.go:104` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/styles.go:172` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/styles.go:173` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/styles.go:174` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/styles.go:175` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/styles.go:176` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/view.go:1788` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/view.go:1789` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/view.go:2201` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/view.go:2205` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.
- **WARN** `pkg/monitor/view.go:2355` — String appears to contain only emoji/symbol characters. Pair icons with text labels for screen readers.

## Recommendations

1. Centralize color tokens in a single theme package and define a high-contrast variant selectable via env or config.
2. Honor `NO_COLOR` at the lipgloss renderer level (`lipgloss.SetColorProfile(termenv.Ascii)` when set).
3. Every `key.NewBinding` should carry a matching `key.WithHelp` so the help pane is complete.
4. Never use emoji or unicode symbols as the sole indicator of state — pair with a short text label.
5. Avoid raw ANSI escape sequences in source; route all styling through lipgloss so it can be globally disabled.
