# x/ansi Upgrade Plan (td) — Phase 0 Warm-up

> Status: **PLAN / not started** · **Phase 0** — standalone, ships before the v2 stack.
> The one charmbracelet bump safe to do while still on v1. Clean, isolated.

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/x/ansi` (**unchanged** — no vanity domain, no `/v2`) | same |
| Version | `v0.11.3` | **`v0.11.7`** (re-verify) |

## Why it's safe and independent

The 0.11.x line (`v0.11.4 → v0.11.7`) is width-calculation fixes + East-Asian-ambiguous-width options only — **no signature changes** to the functions td uses. v1 lipgloss/bubbletea tolerate it, so it neither requires nor conflicts with the v2 stack.

## Codebase usage (7 imports)

Functions in use:
- `ansi.Strip(s)` — e.g. `pkg/monitor/overlay.go:30,39`, `pkg/monitor/form_autofill.go:258`
- `ansi.StringWidth(s)` — `pkg/monitor/overlay.go:20,40,46`
- `ansi.Truncate(s, w, tail)` — `pkg/monitor/kanban.go:300,323,528,546`
- **`ansi.Cut(s, left, right)`** — `pkg/monitor/overlay.go:61` (td uses `Cut`; verify it's still present and stable in 0.11.7 — it is)

All signatures are stable across 0.11.x — no source edits anticipated.

## The work

```bash
cd ~/code/td
go get github.com/charmbracelet/x/ansi@v0.11.7
go mod tidy
go build ./...
go test ./...
```

## Verification

- `go build ./... && go test ./...` clean.
- Spot-check column alignment in glyph-heavy monitor views (kanban cards/columns, the activity table, markdown preview). Any one-cell shift is almost certainly an intended width correction, not a regression.

## Gotchas

- **Do NOT rewrite the import path.** `x/ansi` stays `github.com/charmbracelet/x/ansi`. Only the UI libraries (bubbletea/lipgloss/bubbles/glamour/huh) move to `charm.land`.
- Land + verify this before the [Phase 1 stack](charm-upgrade-02-lipgloss.md), so width shifts are isolated from the v2 changes.
