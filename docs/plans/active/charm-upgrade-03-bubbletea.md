# Bubble Tea v1 → v2 Upgrade Plan (td)

> Status: **PLAN / not started** · Part of the Phase 1 atomic stack (see [overview](charm-upgrade-00-overview.md)).
> Do this **after** [lipgloss](charm-upgrade-02-lipgloss.md). The highest-risk file, and the one that defines the sidecar boundary.

## Versions

| | Current | Target |
|---|---------|--------|
| Module path | `github.com/charmbracelet/bubbletea` | **`charm.land/bubbletea/v2`** |
| Version | `v1.3.10` | **`v2.0.7`** (re-verify) |

Use `charm.land/bubbletea/v2` (NOT `github.com/charmbracelet/bubbletea/v2`, beta-frozen).

## The architectural changes (recap)

1. **`View() string` → `View() tea.View`** on the **program-root** model.
2. **Terminal features declarative** (alt-screen/mouse/etc. are `tea.View` fields, not `NewProgram` options/commands). Mouse is **off by default**.
3. **`tea.KeyMsg` is an interface** — match `tea.KeyPressMsg`; `msg.Type`→`msg.Code`, `msg.Runes`→`msg.Text` (string), `msg.Alt`→`msg.Mod.Contains(tea.ModAlt)`.
4. **`tea.MouseMsg` is an interface** — `MouseClickMsg`/`MouseReleaseMsg`/`MouseWheelMsg`/`MouseMotionMsg`; coords via `msg.Mouse()`; `MouseButtonLeft`→`MouseLeft`.

Unchanged: `Init() tea.Cmd`, `tea.Batch`, `tea.Sequence`, `tea.Tick`, `tea.Quit`, `tea.ExecProcess`, `tea.WindowSizeMsg`, `tea.QuitMsg`.

## ⭐ The `monitor.View()` decision (defines the sidecar boundary)

`monitor.Model` plays two roles:
- **td standalone** (`cmd/monitor.go:97`): it is the **program root** → in v2 its `View()` must return `tea.View`.
- **sidecar embed**: sidecar calls `p.model.View()` expecting a **string** to compose into a height-constrained pane (`internal/plugins/tdmonitor/plugin.go:333-340`).

These conflict. **Recommended approach (Option A):**

> Make `monitor.Model` a proper v2 `tea.Model`: `View() tea.View`. In `cmd/monitor.go`, pass it straight to `tea.NewProgram`. For embedding, sidecar reads the rendered string from the returned view's `Content` field (a **one-line** change on the sidecar side).

```go
// pkg/monitor/model.go
func (m Model) View() tea.View {
    s := m.renderString()          // rename the old View()'s body to a helper returning string
    v := tea.NewView(s)
    v.AltScreen = true             // monitor's standalone terminal needs (ignored when embedded)
    v.MouseMode = tea.MouseModeAllMotion
    return v
}

// Keep a string accessor for embedders so sidecar doesn't depend on tea.View internals:
func (m Model) ViewString() string { return m.renderString() }
```
Then in sidecar: `p.model.View()` → `p.model.ViewString()` (clean) **or** `p.model.View().Content`. Document this single-line change in sidecar's Phase 1.

**Option B** (keep `View() string`, wrap a root adapter in `cmd/monitor.go`, and change `Update` to return the concrete `(Model, tea.Cmd)` so the model needn't satisfy `tea.Model`) is also valid but changes `Update`'s return type — a wider boundary change. Prefer Option A unless you have reason otherwise.

`monitor.Model.Update(msg tea.Msg) (tea.Model, tea.Cmd)` and `Init() tea.Cmd` keep their signatures under Option A (the model stays a `tea.Model`). sidecar's `p.model.Update(msg)` reassign + type-assert path is unchanged.

## The work, grounded in the codebase

### Step 1 — Import path (27 files)

```bash
cd ~/code/td
grep -rl '"github.com/charmbracelet/bubbletea"' --include='*.go' . \
  | xargs sed -i '' 's#github.com/charmbracelet/bubbletea#charm.land/bubbletea/v2#g'
```

### Step 2 — Program construction (`cmd/monitor.go:97`)

```go
// BEFORE
p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion())
// AFTER (options move into monitor.View(); see the decision above)
p := tea.NewProgram(model)
```

### Step 3 — `monitor.View()` (Step "decision" above)

Rename the existing `func (m Model) View() string` body to `renderString()`, add `View() tea.View` + `ViewString()`. (`pkg/monitor/model.go:924`.)

### Step 4 — Key messages: `tea.KeyMsg` → `tea.KeyPressMsg`

Rename every `case tea.KeyMsg:` to `case tea.KeyPressMsg:`. Field renames where the body inspects keys:

- `pkg/monitor/model.go:568` — the main `Update` switch (`tea.KeyMsg`, `tea.WindowSizeMsg`, `tea.MouseMsg` arms). `tea.WindowSizeMsg{Width,Height}` is unchanged.
- `pkg/monitor/commands.go:102-295` — `handleFormUpdate` matches `tea.KeyMsg` then uses `keyMsg.Type == tea.KeyCtrlS`, `case tea.KeyUp/KeyDown/KeyEnter/KeyTab/KeyShiftTab:`, `keyMsg.Type == tea.KeyEsc`. Convert:
  - `keyMsg.Type` → `keyMsg.Code` for the special keys that still exist as `Code` constants (`tea.KeyUp`, `tea.KeyDown`, `tea.KeyEnter`, `tea.KeyTab`, `tea.KeyEsc`).
  - `tea.KeyCtrlS`, `tea.KeyShiftTab` and other `KeyCtrl*`/shift combos are **removed** as constants — match via `keyMsg.String()` (`"ctrl+s"`, `"shift+tab"`) or `Code`+`Mod`.
- `pkg/monitor/input.go` — search-input key handling; `pkg/monitor/keymap/` — the keymap registry matches keys (check how it compares — if via `msg.String()`, it mostly survives; `" "` → `"space"`).
- `pkg/monitor/modal/modal.go:57` — `HandleKey(msg tea.KeyMsg)`: **keep the signature** (`tea.KeyMsg` is the v2 interface), but the body's key inspection changes per the above. This preserves the sidecar boundary.

`msg.Runes` → `msg.Text` (string); `msg.Type == tea.KeyRunes` → `len(msg.Text) > 0`. `case " "` → `case "space"`.

### Step 5 — Mouse: interface + struct literals

- `pkg/monitor/mouse/mouse.go:175` — `HandleMouse(msg tea.MouseMsg) MouseAction`: **keep the signature** (`tea.MouseMsg` is the v2 interface). The body inspects `msg.Action`/`msg.Button` → rewrite to a type switch on `MouseClickMsg`/`MouseReleaseMsg`/`MouseWheelMsg`/`MouseMotionMsg`, reading `msg.Mouse().X/Y/Button`. Button renames `MouseButtonLeft`→`MouseLeft`, `MouseButtonWheelUp`→`MouseWheelUp`, etc. This is the central mouse routing — fix here first.
- `pkg/monitor/input.go:820+` — `handleMouse(msg tea.MouseMsg)` inspecting `tea.MouseActionPress/Release/Motion` and `tea.MouseButtonWheelUp` → same type-switch conversion.
- `pkg/monitor/model.go:584` — the `tea.MouseMsg` arm forwards to `handleMouse`.
- **`tea.MouseMsg{...}` struct literals (interface in v2 — won't compile):** heavy in tests — `pkg/monitor/model_test.go:1612+`, `pkg/monitor/input_test.go:743+`. Rewrite to the concrete type, e.g.:
  ```go
  // BEFORE
  tea.MouseMsg{X:10, Y:5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
  // AFTER
  tea.MouseClickMsg{Mouse: tea.Mouse{X:10, Y:5, Button: tea.MouseLeft}}
  ```
  (Verify the exact `MouseClickMsg` shape against the v2 source.)

### Step 6 — Commands

- `tea.WithAltScreen()`/`tea.WithMouseAllMotion()` removed from `NewProgram` (Step 2) → View fields.
- `tea.Batch`/`tea.Tick`/`tea.ExecProcess` unchanged: `pkg/monitor/actions.go` (Batch, Tick), `pkg/monitor/form_operations.go:297` (`tea.ExecProcess` for `$EDITOR`). The ExecProcess callback signature is unchanged.
- If any `tea.EnableMouseAllMotion()`/`tea.EnterAltScreen` commands exist, replace with `view.MouseMode`/`view.AltScreen` (grep to confirm; td's monitor likely sets these only at program start).
- `tea.QuitMsg` — unchanged (sidecar intercepts it on the monitor's returned cmd).

### Step 7 — Key/mouse struct literals in tests (large)

`pkg/monitor/*_test.go` and `pkg/monitor/keymap/registry_test.go` contain **50+** `tea.KeyMsg{Type:…, Runes:…}` and **15+** `tea.MouseMsg{…}` literals. Rewrite:
```go
// BEFORE                                          // AFTER
tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}} tea.KeyPressMsg{Code: 'j', Text: "j"}
tea.KeyMsg{Type: tea.KeyEsc}                        tea.KeyPressMsg{Code: tea.KeyEsc}
tea.KeyMsg{Type: tea.KeyTab}                        tea.KeyPressMsg{Code: tea.KeyTab}
tea.KeyMsg{Type: tea.KeyShiftTab}                   tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
```
This is the bulk of the test churn. Budget real time.

## Ordered checklist

1. [ ] `go get charm.land/bubbletea/v2@v2.0.7`
2. [ ] Import path rewrite (27 files)
3. [ ] `cmd/monitor.go` `NewProgram` options removed
4. [ ] `monitor.View()` → `tea.View` + `ViewString()` helper (the boundary decision)
5. [ ] `tea.KeyMsg` → `tea.KeyPressMsg`; field renames in commands.go/input.go/keymap (Step 4)
6. [ ] Mouse interface conversion in mouse.go/input.go; keep `HandleKey`/`HandleMouse` signatures (Step 5)
7. [ ] Test constructor rewrites (Step 7)
8. [ ] `go build ./...` — green only once bubbles + huh are done
9. [ ] Note the one-line sidecar change (`View()` → `ViewString()`/`.Content`) for sidecar's Phase 1

## Gotchas

- **Mouse silently dead** if `view.MouseMode` is unset — opt-in now. The monitor relies on click/drag/scroll.
- **Keep `modal.HandleKey(tea.KeyMsg)` / `mouse.HandleMouse(tea.MouseMsg)` signatures** — they take the v2 interface; changing them would needlessly churn the sidecar boundary.
- **`shift+tab` / `ctrl+s`** lose their `KeyCtrl*`/`KeyShiftTab` constants — match via `String()` or `Code`+`Mod`.
- Tests won't compile until the `tea.KeyMsg{}`/`tea.MouseMsg{}` literals are migrated.
