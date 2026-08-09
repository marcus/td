# Glamour v0.10 → v2 Upgrade Plan (td)

> Status: **PLAN / not started** · **Phase 1** (move with the trio, since glamour v2 needs lipgloss v2 and td's glamour use is intertwined with the monitor migration).
> td uses glamour more deeply than sidecar: the root package **plus** `glamour/ansi` (custom Chroma styles) and `glamour/styles`.

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/glamour` (+ `/ansi`, `/styles`) | **`charm.land/glamour/v2`** (+ `/v2/ansi`, `/v2/styles`) |
| Version | `v0.10.0` | **`v2.0.0`** (re-verify) |

Glamour v2 depends on **lipgloss v2** (do it with/after [02-lipgloss](charm-upgrade-02-lipgloss.md)). It does not need bubbletea/bubbles/huh.

## Codebase usage

- `internal/output/markdown.go` — CLI markdown output. Uses `glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(width))`.
- `pkg/monitor/markdown.go` — monitor markdown rendering. `getGlamourOptions(width)` / `getGlamourOptionsWithTheme(width, theme)` return `[]glamour.TermRendererOption` using `glamour.WithWordWrap(...)` and **custom Chroma syntax styling** built via `glamour/ansi` (`ansi.Chroma`, `ansi.StylePrimitive`) and referencing `glamour/styles`.
- `pkg/monitor/modal.go:348` — `glamour.NewTermRenderer(getGlamourOptionsWithTheme(...)...)`.
- `pkg/monitor/markdown_test.go` — test renders.

## The work

### Step 1 — Import paths (root + subpackages)

```bash
cd ~/code/td
grep -rl '"github.com/charmbracelet/glamour' --include='*.go' . \
  | xargs sed -i '' \
      -e 's#github.com/charmbracelet/glamour/ansi#charm.land/glamour/v2/ansi#g' \
      -e 's#github.com/charmbracelet/glamour/styles#charm.land/glamour/v2/styles#g' \
      -e 's#github.com/charmbracelet/glamour#charm.land/glamour/v2#g'
```
(Subpaths rewritten before the root path — the `-e` rules apply left-to-right per line.)

### Step 2 — `WithAutoStyle()` removed (`internal/output/markdown.go:51`)

`glamour.WithAutoStyle()` is **removed** in v2 (the default style is now `"dark"`). Replace:
```go
// BEFORE
r, _ := glamour.NewTermRenderer(
    glamour.WithAutoStyle(),
    glamour.WithWordWrap(width),
)
// AFTER — pick an explicit style; "auto"/"dark" via WithStandardStyle, or detect:
r, _ := glamour.NewTermRenderer(
    glamour.WithStandardStyle("dark"),   // or styles.DarkStyle / detect light/dark
    glamour.WithWordWrap(width),
)
```
If the CLI output needs to honor a light terminal, detect with `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)` and choose `"dark"`/`"light"`. (`glamour.WithColorProfile()` is also removed — td does not use it, confirmed.)

### Step 3 — Custom Chroma styles via `glamour/ansi` (`pkg/monitor/markdown.go`)

td builds `*ansi.Chroma` with `ansi.StylePrimitive{Color: ptrString(...), Bold: ...}` and feeds it into a renderer option. In v2:
- The `glamour/ansi` types (`ansi.Chroma`, `ansi.StylePrimitive`, `ansi.StyleConfig`) are **retained** but live under `charm.land/glamour/v2/ansi`. Field shapes are stable (string-pointer colors, bool-pointer flags).
- Verify the option used to inject the custom style still exists (e.g. `glamour.WithStyles(ansi.StyleConfig)` or a style-config option). If td currently passes the chroma via a specific option, re-check its name/signature against the v2 `glamour` package. The `Overlined` style field was removed in v2 — if any `ansi.StylePrimitive` or style config sets it, drop it.
- `WithWordWrap` is unchanged.

### Step 4 — Output path

Rendered markdown is composed into the monitor's string `View()` (and printed for CLI output). Under the Bubble Tea v2 program the runtime downsamples color; for the CLI path, glamour v2 emits truecolor and downsampling happens at the terminal/lipgloss writer. Verify CLI markdown still colorizes on a non-truecolor terminal; if it renders raw truecolor, route the print through `lipgloss.Print`/the colorprofile writer.

## Ordered checklist

1. [ ] `go get charm.land/glamour/v2@v2.0.0`
2. [ ] Import paths incl. `/ansi`, `/styles` (Step 1)
3. [ ] Replace `WithAutoStyle()` in `internal/output/markdown.go` (Step 2)
4. [ ] Verify custom Chroma injection option + drop `Overlined` if set (Step 3)
5. [ ] `go build ./... && go test ./...` (incl. `pkg/monitor/markdown_test.go`)
6. [ ] Manual: render a markdown-heavy issue in `td monitor` + `td <cmd>` CLI output; check code-block syntax colors and word-wrap

## Gotchas

- `WithAutoStyle()` removal is the one guaranteed compile break (`undefined: glamour.WithAutoStyle`).
- Word-wrap was rewritten on `lipgloss.Wrap` in v2 (better CJK/emoji) — long lines may wrap slightly differently; confirm preview panes still fit their width.
- Wrong path (`github.com/charmbracelet/glamour/v2`) → stale beta. Use `charm.land/glamour/v2`.
- v2 adds OSC 8 clickable links — a nice bonus to verify in the monitor preview.
