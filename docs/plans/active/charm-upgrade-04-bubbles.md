# Bubbles v1 → v2 Upgrade Plan (td)

> Status: **PLAN / not started** · Part of the Phase 1 atomic stack (see [overview](charm-upgrade-00-overview.md)).
> Do this **after** [lipgloss](charm-upgrade-02-lipgloss.md) and [bubbletea](charm-upgrade-03-bubbletea.md).

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/bubbles` | **`charm.land/bubbles/v2`** |
| Version | `v0.21.x` | **`v2.1.0`** (re-verify) |

`bubbles/v2` hard-requires `bubbletea/v2` + `lipgloss/v2` — part of why the stack moves together.

## Scope — two subpackages

td uses **only** `bubbles/textinput` (8 files) and `bubbles/textarea` (4 files). No `key`, `viewport`, `list`, `spinner`, etc. from bubbles.

## Step 1 — Import paths

```bash
cd ~/code/td
grep -rl '"github.com/charmbracelet/bubbles/' --include='*.go' . \
  | xargs sed -i '' 's#github.com/charmbracelet/bubbles#charm.land/bubbles/v2#g'
```

## Step 2 — `textinput`: `.Width` field → `SetWidth()`

`Width` became a method pair (`SetWidth(int)` / `Width() int`). Change assignments:

| Site |
|------|
| `pkg/monitor/model.go:252` — `searchInput.Width = 50` → `searchInput.SetWidth(50)` |
| `pkg/monitor/modal/input.go:91` — `s.model.Width = inputInnerWidth` → `s.model.SetWidth(inputInnerWidth)` |

Any **read** of `.Width` becomes `.Width()`. Grep: `grep -rn '\.Width\b' --include='*.go' pkg/monitor | grep -iE 'input|searchInput|s\.model'`.

Unchanged fields/methods td uses (no edits): `.Placeholder`, `.Prompt`, `.CharLimit`, `.Focus()`, `.Blur()`, `.Value()`, `.SetValue()`, `.Update()`, `textinput.New()`.

> **Exported boundary:** `modal.Input(id string, model *textinput.Model, ...)` and `modal.InputWithLabel(...)` (`pkg/monitor/modal/input.go:27,40`) take `*textinput.Model`. After the import rewrite this is `*charm.land/bubbles/v2/textinput.Model`. sidecar does **not** construct these directly (it uses the higher-level `modal.New(...).AddSection(...)` builders — confirmed), so this boundary type change is internal to td. Verify no sidecar call passes a textinput.Model across.

td does **not** use `textinput.DefaultKeyMap` (which became a function in v2) or set `PromptStyle/TextStyle/PlaceholderStyle` on textinput — confirm with a grep after the import rewrite.

## Step 3 — `textarea`

td uses textarea in `pkg/monitor/model.go:175` (`BoardEditorQueryInput *textarea.Model`), `pkg/monitor/board_editor.go`, `pkg/monitor/notes_modal.go`, `pkg/monitor/modal/input.go`.

Apply the v2 textarea changes **only where td touches the changed surface**:
- If td sets `textarea.Style{}` literals or `.FocusedStyle`/`.BlurredStyle` → rename type to `textarea.StyleState` and move to `.Styles.Focused`/`.Styles.Blurred`. (Grep `grep -rn 'textarea.Style\|FocusedStyle\|BlurredStyle' --include='*.go' pkg/monitor` — sidecar's notes plugin did this; td may or may not.)
- If td calls `.SetCursor(col)` → `.SetCursorColumn(col)`. (Grep `SetCursor(`.)
- Unchanged: `textarea.New()`, `.Update()`, `.Value()`, `.SetValue()`, `.Focus()`, `.Blur()`, `.SetWidth()`, `.SetHeight()`.

## Step 4 — `DefaultKeyMap` sweep

In v2, `textinput.DefaultKeyMap`/`textarea.DefaultKeyMap` are functions. Re-grep `grep -rn 'DefaultKeyMap' --include='*.go' .` after the import rewrite; add `()` to any survivors. (None expected from the current inventory.)

## Ordered checklist

1. [ ] `go get charm.land/bubbles/v2@v2.1.0` (pulls bubbletea/lipgloss v2)
2. [ ] Import path rewrite (Step 1)
3. [ ] textinput `.Width =` → `.SetWidth()`, reads → `.Width()` (Step 2)
4. [ ] textarea style/cursor changes where present (Step 3)
5. [ ] `DefaultKeyMap` sweep (Step 4)
6. [ ] `go build ./...` — green once [huh](charm-upgrade-05-huh.md) + glamour are also done

## Gotchas

- `textarea.Style` → `textarea.StyleState` rename surfaces as `undefined: textarea.Style`.
- textinput `.Width` read sites compile as a method value if you forget `()` — rely on the build to flag.
- Don't migrate bubbles before bubbletea/lipgloss — it pulls the whole v2 stack.
