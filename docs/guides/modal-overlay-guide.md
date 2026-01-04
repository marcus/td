# Modal Overlay Implementation Guide

Guide for implementing modals with dimmed background overlays in the TD monitor.

## Overview

Modals dim the background to:
- Draw user focus to the modal content
- Provide visual separation between modal and underlying content
- Show context from the underlying panels while focusing attention on the modal

## Current Implementation: Dimmed Background Overlay

All modals use `OverlayModal()` from `pkg/monitor/overlay.go` to show **dimmed background content** behind the modal:

```go
// In renderView():
if m.FormOpen && m.FormState != nil {
    background := m.renderBaseView()
    form := m.renderFormModal()
    return OverlayModal(background, form, m.Width, m.Height)
}
```

**How `OverlayModal()` works:**
1. Renders the base view (panels + footer) via `renderBaseView()`
2. Strips ANSI codes from background and applies dim gray styling (color 242)
3. Calculates modal position (centered horizontally and vertically)
4. Composites each row: `dimmed-left + modal + dimmed-right`
5. Shows dimmed background on all four sides of the modal

**Visual result:**
```
╔════════════════════════════════════════════════╗
║  [dimmed gray background text]                 ║
║  [gray left]  ┌─Modal─┐  [gray right]          ║
║  [gray left]  │ text  │  [gray right]          ║
║  [gray left]  └───────┘  [gray right]          ║
║  [dimmed gray background text]                 ║
╚════════════════════════════════════════════════╝
```

**Note:** Background colors are not preserved because ANSI SGR 2 (faint) doesn't reliably combine with existing color codes in most terminals. The gray overlay provides consistent dimming.

## Alternative: Solid Black Overlay

Use `lipgloss.Place()` with whitespace options when you want to **completely hide** the background:

```go
func (m Model) renderMyModal(content string) string {
    modal := m.wrapModal(content, width, height)

    return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, modal,
        lipgloss.WithWhitespaceChars(" "),
        lipgloss.WithWhitespaceForeground(lipgloss.Color("0")))
}
```

**How it works:**
- `lipgloss.Place()` centers the modal and fills surrounding space with spaces
- The spaces use the terminal's default background color (black/0)
- Background content is **hidden**, not dimmed

**Note:** `WithWhitespaceForeground()` sets the foreground color of space characters, which are invisible. This does NOT create visible dimming.

## Implementation Checklist

When adding a new modal:

1. **Render the modal content** using appropriate wrapper (`wrapModal()`, `wrapModalWithDepth()`, or inline styling)

2. **Use the dimmed overlay pattern** (preferred):
   ```go
   background := m.renderBaseView()
   modalContent := m.renderMyModal()
   return OverlayModal(background, modalContent, m.Width, m.Height)
   ```

3. **Don't use `lipgloss.Place()` with `OverlayModal()`** - they both handle centering, which causes layout issues.

## Core Functions

### renderBaseView()

Renders the panels and footer without any modal overlay. This is the background content used for dimmed modal overlays.

```go
func (m Model) renderBaseView() string {
    // Renders: search bar + Current Work + Task List + Activity + footer
    // Returns the complete base view string
}
```

### OverlayModal()

Composites a modal on top of a dimmed background. Located in `pkg/monitor/overlay.go`.

```go
func OverlayModal(background, modal string, width, height int) string
```

## Style Constants

```go
// DimStyle applies dim gray color to background content (overlay.go)
var DimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

// Modal border colors by context (styles.go)
var (
    primaryColor = lipgloss.Color("212") // Purple/Magenta - depth 1 issue modals
    cyanColor    = lipgloss.Color("45")  // Cyan - depth 2+ or form modals
    orangeColor  = lipgloss.Color("214") // Orange - depth 3+
    greenColor   = lipgloss.Color("42")  // Green - handoffs modal
    errorColor   = lipgloss.Color("196") // Red - confirmation dialogs
)
```

## Common Pitfalls

1. **Don't use `lipgloss.Place()` with `OverlayModal()`** - they both handle centering, which causes layout issues.

2. **Pass the full background** - `OverlayModal()` needs the complete background content to composite correctly. Don't pre-truncate or pre-dim.

3. **Height constraints** - Ensure modal content respects available height to prevent overflow. Use `wrapModalWithDepth()` for consistent sizing.

4. **Stacked modals** - For stacked issue modals (depth 2+), the background is always the base view (panels), not the previous modal.

## File Locations

- Overlay helper: `pkg/monitor/overlay.go` (`OverlayModal()`, `DimStyle`)
- Base view: `pkg/monitor/view.go` (`renderBaseView()`)
- Modal rendering: `pkg/monitor/view.go` (`renderModal()`, `renderStatsModal()`, etc.)
- Modal logic: `pkg/monitor/modal.go` (stack management, navigation)
- Modal wrapper: `pkg/monitor/view.go` (`wrapModal()`, `wrapModalWithDepth()`)
- Modal styles: `pkg/monitor/styles.go` (border colors, text styles)
- Modal architecture guide: `docs/guides/modal-system-guide.md`

## Modal Inventory

| Modal | Border Color | Overlay Type | Render Function |
|-------|--------------|--------------|-----------------|
| Issue details | Purple (depth 1), Cyan (2), Orange (3+) | Dimmed | `renderModal()` |
| Stats | Purple | Dimmed | `renderStatsModal()` |
| Handoffs | Green | Dimmed | `renderHandoffsModal()` |
| Form | Cyan | Dimmed | `renderFormModal()` |
| Confirmation | Red | Dimmed | `renderConfirmation()` |
| Help | N/A (full screen) | N/A | `renderHelp()` |
| TDQ Help | N/A (full screen) | N/A | `renderTDQHelp()` |
