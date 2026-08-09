# Lip Gloss v1 → v2 Upgrade Plan (td)

> Status: **PLAN / not started** · Part of the Phase 1 atomic stack (see [overview](charm-upgrade-00-overview.md)).
> Do this **first** within Phase 1 — it is the foundation everything else builds on.

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/lipgloss` (+ `/table`) | **`charm.land/lipgloss/v2`** (+ `/v2/table`) |
| Version | `v1.1.1-0.2025…` | **`v2.0.3`** (re-verify) |

**Do NOT use `github.com/charmbracelet/lipgloss/v2`** (beta-frozen). Use `charm.land/lipgloss/v2`.

## What changed upstream (the parts that matter for td)

1. **`lipgloss.Color` is now a function, not a type** (`func Color(s string) color.Color`). `lipgloss.Color("212")` calls still work; `lipgloss.Color` used as a *type* breaks.
2. **Renderer model removed** (`NewRenderer`/`DefaultRenderer`/`SetColorProfile`). td never used these — confirmed zero matches. Nothing to remove.
3. **`AdaptiveColor`/`CompleteColor`/`TerminalColor` removed** from root. td never used them — confirmed zero matches.
4. **`color.Color` (stdlib `image/color`) is the universal color type.** `GetForeground()/GetBackground()` return `color.Color`.
5. **Layout/border/Style methods unchanged**: `JoinVertical`, `JoinHorizontal`, `Width()`, `Height()`, `MaxWidth()`, `RoundedBorder()`, `NormalBorder()`, `HiddenBorder()`, `Border`, `BorderForeground`, `Foreground`, `Background`, `Bold`, `Render`, etc.
6. **Downsampling moves to output**; under a Bubble Tea v2 program (td's monitor) the runtime handles it — nothing to do.

## The work, grounded in the codebase

### Step 1 — Import path (17 files, incl. table)

```bash
cd ~/code/td
grep -rl '"github.com/charmbracelet/lipgloss' --include='*.go' . \
  | xargs sed -i '' \
      -e 's#github.com/charmbracelet/lipgloss/table#charm.land/lipgloss/v2/table#g' \
      -e 's#github.com/charmbracelet/lipgloss#charm.land/lipgloss/v2#g'
```
(Order matters in the sed: rewrite the `/table` subpath before the root path so the root rule doesn't half-match. The two `-e` rules above are applied left-to-right per line, so `/table` is handled first.)

### Step 2 — The ONE `lipgloss.Color`-as-type site

td uses `lipgloss.Color` as a type in exactly one place (confirmed by grep — far less than sidecar):

`pkg/monitor/kanban.go:50`:
```go
// BEFORE
func kanbanColumnColor(cat TaskListCategory) lipgloss.Color {
    ...
    return lipgloss.Color("183")   // these CALLS stay as-is
}
// AFTER
import "image/color"
func kanbanColumnColor(cat TaskListCategory) color.Color {
    ...
    return lipgloss.Color("183")   // unchanged
}
```
Then check every **caller** of `kanbanColumnColor` — wherever its result is stored in a variable typed `lipgloss.Color` or passed to something expecting `lipgloss.Color`, that must become `color.Color` too. `Foreground()/Background()/BorderForeground()` already accept `color.Color`, so passing the result straight into a style needs no change.

Confirm with: `grep -rnE 'lipgloss\.Color\b[^(]' --include='*.go' .` returning nothing (every survivor should be a `lipgloss.Color(` call). There are **no** `.(lipgloss.Color)` type assertions in td (confirmed) — so the assertion churn that hit sidecar does not apply here.

### Step 3 — Package-level color vars (no edit needed)

`pkg/monitor/styles.go` and `pkg/monitor/modal/styles.go` declare color vars via `var primaryColor = lipgloss.Color("212")` etc. These use **type inference**, so they become `color.Color` automatically in v2. No edits — unless a var is *explicitly* annotated `lipgloss.Color` (grep from Step 2 will catch it; none expected).

### Step 4 — `lipgloss/table` (used in `pkg/monitor/view.go`)

The `table` subpackage API is **stable** in v2 — only the import path changes (Step 1). td's usage (`StyleFunc func(row, col int) lipgloss.Style`, `Border(lipgloss.HiddenBorder())`, `Width`/`Height`) keeps the same signatures. `StyleFunc` still returns `lipgloss.Style`. No code change beyond the path.

### Step 5 — Downsampling / dark background

No action. The monitor renders through the Bubble Tea v2 program, which downsamples. td's colors are hardcoded ANSI-256 strings (no adaptive detection), so there's no light/dark handshake to wire.

## Ordered checklist

1. [ ] `go get charm.land/lipgloss/v2@v2.0.3` (re-verify version)
2. [ ] Import path rewrite incl. `/table` (Step 1)
3. [ ] `kanbanColumnColor` return type → `color.Color` + audit callers (Step 2)
4. [ ] `grep -rnE 'lipgloss\.Color\b[^(]'` returns nothing
5. [ ] `grep -rn 'lipgloss.TerminalColor\|lipgloss.AdaptiveColor\|lipgloss.NewRenderer\|SetColorProfile'` returns nothing
6. [ ] `go build ./...` — **stays red** until bubbletea/bubbles/huh are migrated. Proceed to [03-bubbletea](charm-upgrade-03-bubbletea.md).

## Common compile errors

- `lipgloss.Color (type) is not an expression` / `cannot use "183" as lipgloss.Color` → a `lipgloss.Color` used as a type; change to `color.Color` and keep `lipgloss.Color("…")` as the constructor.
- `undefined: lipgloss.TerminalColor` → replace with `color.Color`.
