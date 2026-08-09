# huh v0.8 → v2 Upgrade Plan (td)

> Status: **PLAN / not started** · Part of the Phase 1 atomic stack (see [overview](charm-upgrade-00-overview.md)).
> Do this **with** bubbletea/lipgloss/bubbles v2 — huh is hard-coupled to the v2 trio.
> sidecar has **no direct huh dependency** — huh is internal to td's monitor, so this file is td-only.

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/huh` | **`charm.land/huh/v2`** |
| Version | `v0.8.0` | **`v2.0.3`** (re-verify) |

`huh/v2`'s go.mod requires `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2`. Go MVS will select your higher trio versions (`v2.0.7 / v2.0.3 / v2.1.0`) since they share the v2 major — compatible. **Do not** use the `v1.0.0` huh tag: it is the *v1* stack (still `github.com/charmbracelet/...`), not the v2 line.

> Toolchain: huh v2.0.3's go.mod declares `go 1.25.8`. If `go mod tidy` complains, bump td's toolchain.

## Scope — small

td uses huh in **one** place: `pkg/monitor/form.go` (the create/edit-issue form). `FormState.Form *huh.Form`, built with `huh.NewForm`/`NewGroup`/`NewInput`/`NewSelect[string]`/`NewText`/`NewOption`, themed with `huh.ThemeDracula()`, embedded in the monitor model.

**Good news from the inventory:**
- td does **not** import `github.com/charmbracelet/huh/accessibility` and does **not** call field-level `.WithAccessible(...)` — so the v2 accessibility breakage does **not** apply.
- `NewForm`, `NewGroup`, `NewInput`, `NewSelect[T]`, `NewText`, `NewOption`, `.Value()`, `.Title()`, `.Options()`, `.Validate()`, `.Key()`, `.Placeholder()`, `.Lines()` are **unchanged in v2**. td's `buildForm()` body needs no structural change.

## The work

### Step 1 — Import path (`pkg/monitor/form.go`, and any test)

```go
// BEFORE
import "github.com/charmbracelet/huh"
// AFTER
import "charm.land/huh/v2"     // package name is still `huh`
```
```bash
cd ~/code/td
grep -rl '"github.com/charmbracelet/huh' --include='*.go' . \
  | xargs sed -i '' 's#github.com/charmbracelet/huh#charm.land/huh/v2#g'
```

### Step 2 — Theme call gains `isDark` (`pkg/monitor/form.go:253`)

```go
// BEFORE
fs.Form.WithTheme(huh.ThemeDracula())
// AFTER
isDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)   // lipgloss v2 detection
fs.Form.WithTheme(huh.ThemeDracula(isDark))
```
All built-in theme constructors now take `isDark bool` and return `*huh.Styles`. Compute `isDark` once (you can cache it on the model / `FormState` rather than re-detecting per form build). If the monitor already knows its background (or you want to keep it simple), `isDark := true` is an acceptable starting value for the Dracula (dark) theme — but the `HasDarkBackground` call is the correct one.

### Step 3 — Form embedding in the monitor (bubbletea v2 host)

The huh form is embedded in `monitor.Model`, not run standalone. Key facts:
- `huh.Form.Update(msg tea.Msg) (huh.Model, tea.Cmd)` returns `huh.Model` (a v1-shaped model whose `View()` returns **string**). The `.(*huh.Form)` assertion still works.
- `huh.Form.View()` still returns **string** — so where the monitor composes the form into its own (string) render, **nothing changes**.
- The bubbletea-v2 host concerns (`View() → tea.View`, `tea.KeyPressMsg`) are handled at the `monitor.Model` level in the [bubbletea plan](charm-upgrade-03-bubbletea.md), not here. huh matches v2 key messages internally.

So in `pkg/monitor/`, where the form is updated:
```go
// pattern (unchanged in shape)
f, cmd := fs.Form.Update(msg)
if hf, ok := f.(*huh.Form); ok { fs.Form = hf }
```
and where it's rendered, `fs.Form.View()` (string) flows into the monitor's `renderString()` as before.

### Step 4 — Form state checks

Where td checks form completion (`huh.Form` state, e.g. `form.State == huh.StateCompleted`) — those enums/fields are unchanged in v2. Verify `commands.go:287` / `model.go:287` type-assertions (`form.(*huh.Form)`) still compile (they do; type just resolves to v2).

## Ordered checklist

1. [ ] `go get charm.land/huh/v2@v2.0.3`
2. [ ] Import path rewrite (Step 1)
3. [ ] `ThemeDracula()` → `ThemeDracula(isDark)` (Step 2)
4. [ ] Confirm form `Update`/`View` embedding compiles (Step 3)
5. [ ] `go build ./...` — green once the whole stack is migrated
6. [ ] Manual: open the create-issue and edit-issue forms in `td monitor`; tab through fields, validate, submit

## Gotchas

- **`huh.ThemeDracula()` — not enough arguments** after the import swap: add `isDark`.
- **Two bubbleteas in the graph:** if `go list -m all | grep -E 'charmbracelet/bubbletea|charm.land/bubbletea'` shows both, a v1 holdout (often a stray huh v0.8 or unrelated widget) remains — find and remove it. huh v0.8 pulls v1 bubbletea, so leaving huh behind while the rest is v2 is the classic dual-stack trap.
- td uses no custom huh theme and no accessibility — so the `*huh.Styles` custom-theme rewrite and `accessibility` package deletion (common v2 chores) **do not apply** here.
