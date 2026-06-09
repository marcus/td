# Charmbracelet Library Upgrade (td) — Overview & Coordination

> Status: **PLAN / not started**
> Tracking task: `td-4386f9`
> Author research date: 2026-06-08 (re-verify versions at execution — see "Re-verify before starting")
> **This upgrade is a hard prerequisite for sidecar's Phase 1.** See "Cross-repo coordination" below.

This is the master document for upgrading td's Charmbracelet dependencies. It mirrors sidecar's plan set (`~/code/sidecar/docs/plans/active/charm-upgrade-*.md`) and adds **huh** (forms), **lipgloss/table**, and **glamour subpackages**, which td uses but sidecar does not. Read this first, then work the per-library files **in execution order**:

| # | Library | File | Current | Target | Phase | Risk |
|---|---------|------|---------|--------|-------|------|
| 01 | x/ansi | [charm-upgrade-01-xansi-warmup.md](charm-upgrade-01-xansi-warmup.md) | `v0.11.3` | `v0.11.7` | 0 (standalone) | Low |
| 02 | Lip Gloss (+table) | [charm-upgrade-02-lipgloss.md](charm-upgrade-02-lipgloss.md) | `v1.1.x` | `charm.land/lipgloss/v2 v2.0.3` | 1 (atomic) | Medium |
| 03 | Bubble Tea | [charm-upgrade-03-bubbletea.md](charm-upgrade-03-bubbletea.md) | `v1.3.10` | `charm.land/bubbletea/v2 v2.0.7` | 1 (atomic) | **High** (key/mouse/View + boundary) |
| 04 | Bubbles | [charm-upgrade-04-bubbles.md](charm-upgrade-04-bubbles.md) | `v0.21.x` | `charm.land/bubbles/v2 v2.1.0` | 1 (atomic) | Medium |
| 05 | huh | [charm-upgrade-05-huh.md](charm-upgrade-05-huh.md) | `v0.8.0` | `charm.land/huh/v2 v2.0.3` | 1 (atomic) | Medium |
| 06 | Glamour (+ansi/styles) | [charm-upgrade-06-glamour.md](charm-upgrade-06-glamour.md) | `v0.10.0` | `charm.land/glamour/v2 v2.0.0` | 1 (with trio) | Low–Medium |
| 07 | x/cellbuf + cleanup | [charm-upgrade-07-cellbuf.md](charm-upgrade-07-cellbuf.md) | `v0.0.14` | `v0.0.15` | 2 (post-trio) | Low |

## TL;DR — the things that make this hard

1. **Module paths move to `charm.land/`.** v2 of bubbletea/lipgloss/bubbles/glamour/**huh** are published under `charm.land/...`, NOT `github.com/charmbracelet/.../v2` (those are frozen at 2025 betas). The `x/*` utility libs keep their `github.com/charmbracelet/x/...` path.

2. **It is a five-library lockstep, not three.** `bubbles/v2`, `bubbletea/v2`, `lipgloss/v2`, **and `huh/v2`** all hard-require each other (huh v2's go.mod requires the v2 trio; bubbles requires bubbletea+lipgloss). There is **no compiling half-migrated state**. All five (plus glamour, which needs lipgloss v2) move in one atomic change.

3. **td's monitor is embedded inside sidecar's program** — this is the cross-repo constraint (next section).

## Cross-repo coordination — WHY this blocks sidecar Phase 1

sidecar imports `github.com/marcus/td/pkg/monitor`, `.../monitor/mouse`, `.../monitor/modal` and **embeds `*monitor.Model` as a live Bubble Tea sub-model** inside sidecar's single `tea.Program`. Verified in sidecar at `internal/plugins/tdmonitor/plugin.go`:

- `p.model.Init()`, `p.model.Update(msg)` (forwarding `tea.Msg`/`tea.WindowSizeMsg`/`tea.KeyMsg`/`tea.MouseMsg`), `p.model.View()` (a `lipgloss`-rendered string) — all called from sidecar's own plugin Update/View.
- `tea.Cmd` returned by the monitor is bubbled up to sidecar's program; `tea.QuitMsg` is intercepted.
- sidecar also drives `td/pkg/monitor/modal.Modal.HandleKey(tea.KeyMsg)` / `HandleMouse(tea.MouseMsg)` and `td/pkg/monitor/mouse.Handler`.

**The charm types that cross the boundary** (`tea.Msg`, `tea.Cmd`, `tea.Model`, `tea.WindowSizeMsg`, `tea.KeyMsg`/`KeyPressMsg`, `tea.MouseMsg`, `tea.QuitMsg`, `lipgloss.Style`, `lipgloss.Color`) **must be the same module major version on both sides.** A `tea.Cmd` from `github.com/charmbracelet/bubbletea` (v1) is a *different type* from a `tea.Cmd` in `charm.land/bubbletea/v2` — they will not unify, and sidecar's program cannot drive a v1 monitor sub-model from a v2 host.

### The required sequence (do not skip)

```
1. td   Phase 0:  x/ansi warm-up (optional, standalone)
2. td   Phase 1:  migrate monitor + all charm libs to v2 (this plan), keeping the
                   public boundary signatures stable where possible (see below)
3. td   RELEASE:  cut a new td version (e.g. v0.45.0) built on charm.land v2
4. sidecar Phase 1, step 0:  bump `github.com/marcus/td` to that new release FIRST,
                   THEN migrate sidecar's own lipgloss/bubbletea/bubbles to v2.
                   Now both import the SAME charm.land v2 modules → the embedded
                   monitor sub-model's types unify.
```

> sidecar's plan ([~/code/sidecar/docs/plans/active/charm-upgrade-00-overview.md](../../../../sidecar/docs/plans/active/charm-upgrade-00-overview.md)) should gain a "Prerequisite: td v2 release" note at the top of its Phase 1.

### Keep the boundary signatures stable

Most of the exported `pkg/monitor*` API that sidecar calls takes/returns **interface** charm types whose *signatures stay textually identical* across v1→v2 — only the underlying module changes:
- `modal.Modal.HandleKey(msg tea.KeyMsg)` — `tea.KeyMsg` is an *interface* in v2 (satisfied by `KeyPressMsg`); signature unchanged.
- `modal.Modal.HandleMouse(msg tea.MouseMsg, ...)` — `tea.MouseMsg` is an interface in v2; signature unchanged.
- `monitor.Model.Update(msg tea.Msg) (...)`, `Init() tea.Cmd` — unchanged.

The **one** boundary signature that must change is `monitor.Model.View()`, because in v2 the program-root model must return `tea.View` (see [03-bubbletea](charm-upgrade-03-bubbletea.md), "The monitor.View() decision"). Plan to keep sidecar's embedding change to a single line (`.Content` extraction or a `ViewString()` helper).

## Scope (grounded in the td codebase)

Counted via `grep -rhoE '"github.com/charmbracelet/[^"]+"' --include="*.go"`:

| Import | Files | Notes |
|--------|-------|-------|
| `bubbletea` | 27 | 1 `tea.NewProgram` (`cmd/monitor.go:97`); monitor is the program root |
| `lipgloss` | 17 | + `lipgloss/table` (1, `pkg/monitor/view.go`) |
| `bubbles/textinput` | 8 | incl. exported `modal.Input(*textinput.Model)` |
| `x/ansi` | 7 | `Strip`, `StringWidth`, `Truncate`, **`Cut`** (td uses Cut; sidecar didn't) |
| `glamour` | 4 | + `glamour/ansi` (1, `ansi.Chroma`) + `glamour/styles` (1) |
| `bubbles/textarea` | 4 | |
| `huh` | 2 | `pkg/monitor/form.go` — `FormState.Form *huh.Form` |
| `x/cellbuf` | 1 | `pkg/monitor/view.go` |

The heavy, public-API package is `pkg/monitor` (kanban, board editor, forms, modals, markdown, activity table) plus its subpackages `keymap`, `modal`, `mouse`.

## Recommended sequencing

### Phase 0 — x/ansi warm-up (file 01, standalone)
Bump `x/ansi` → `v0.11.7` while still on v1. Safe, isolated. See [01](charm-upgrade-01-xansi-warmup.md).

### Phase 1 — the v2 stack (files 02–06, ONE atomic change)
Migrate in dependency order: **lipgloss → bubbletea → bubbles → huh → glamour**. `go build ./...` stays red until all are done. colorprofile/x/term/x/exp resolve via `go mod tidy`.

### Phase 2 — cellbuf + cleanup (file 07)
`go mod tidy`, bump cellbuf if still direct, review the dep graph. See [07](charm-upgrade-07-cellbuf.md).

### Then: cut a td release, and only then start sidecar Phase 1.

## Re-verify before starting (versions move)

```bash
go list -m -versions charm.land/lipgloss/v2
go list -m -versions charm.land/bubbletea/v2
go list -m -versions charm.land/bubbles/v2
go list -m -versions charm.land/huh/v2
go list -m -versions charm.land/glamour/v2
go list -m -versions github.com/charmbracelet/x/ansi
```

Upgrade guides (source of truth):
- bubbletea/lipgloss/bubbles: see links in sidecar's plan
- huh: https://github.com/charmbracelet/huh/blob/main/UPGRADE_GUIDE_V2.md

## Prerequisites

- **Go 1.25+** (v2 modules declare `go 1.25.x`; huh v2.0.3 declares `go 1.25.8`). td is on `go 1.25.5` — bump the toolchain if `go mod tidy` complains about huh's `1.25.8`.

## Target version cheat-sheet (for go.mod)

```
// v2 stack — must move together, vanity paths (Phase 1):
charm.land/lipgloss/v2          v2.0.3
charm.land/bubbletea/v2         v2.0.7
charm.land/bubbles/v2           v2.1.0
charm.land/huh/v2               v2.0.3   // requires the v2 trio; MVS selects the above
charm.land/glamour/v2           v2.0.0   // needs lipgloss v2

// standalone warm-up, same path, no /v2 (Phase 0):
github.com/charmbracelet/x/ansi         v0.11.7

// float automatically with the v2 bump:
github.com/charmbracelet/colorprofile   v0.4.3
github.com/charmbracelet/x/term         v0.2.2

// bump explicitly only if still direct after tidy (Phase 2):
github.com/charmbracelet/x/cellbuf      v0.0.15
```

## Risk register

| Risk | Where | Mitigation |
|------|-------|------------|
| Boundary type mismatch with sidecar | `pkg/monitor*` exported API | Keep signatures stable (interfaces); coordinate the td release → sidecar bump (see above) |
| `monitor.View()` signature change | `pkg/monitor/model.go`, `cmd/monitor.go`, sidecar embed | Decide string-vs-tea.View early ([03](charm-upgrade-03-bubbletea.md)); keep sidecar change to one line |
| Mouse stops working | monitor TUI | Mouse opt-in via `view.MouseMode` in v2; test click/scroll/drag in kanban + board editor |
| Test suite breakage | `pkg/monitor/*_test.go` (50+ `tea.KeyMsg{}`, 15+ `tea.MouseMsg{}` literals) | Rewrite constructors to v2 (`KeyPressMsg{Code,Text}`, `MouseClickMsg{Mouse:...}`) |
| huh theme/form regressions | `pkg/monitor/form.go` | `ThemeDracula(isDark)`; verify create/edit issue forms render + submit |
| glamour CLI output style | `internal/output/markdown.go` | `WithAutoStyle()` removed in v2 — replace (see [06](charm-upgrade-06-glamour.md)) |
| Color rendering | monitor styles | Mostly hardcoded ANSI-256 strings; low adaptive risk. Visual QA kanban colors |

## Definition of done

- `go build ./...`, `go vet ./...` clean; `go test ./...` green (after test-constructor rewrites).
- Manual: run `td monitor` standalone — kanban nav, board editor, create/edit issue (huh form), markdown preview, mouse click/scroll, modals.
- **Cross-check with sidecar:** build sidecar against the new td (via a local `replace` directive) and confirm the embedded monitor renders + responds before cutting the real td release.
