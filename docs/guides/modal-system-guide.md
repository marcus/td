# Modal System Architecture

Guide for developers adding modal-related features to the TD monitor.

## Core Concepts

### Modal Stack

Modals use a stack architecture (`ModalStack []ModalEntry`) allowing nested navigation:

```go
type ModalEntry struct {
    IssueID     string
    SourcePanel Panel      // Only meaningful for base modal (depth 1)
    Scroll      int

    // Async data
    Loading, Error, Issue, Handoff, Logs, BlockedBy, Blocks
    DescRender, AcceptRender  // Pre-rendered markdown

    // Epic-specific
    EpicTasks          []models.Issue
    EpicTasksCursor    int
    TaskSectionFocused bool
}
```

### Helper Methods

```go
m.ModalOpen()       // bool - any modal open?
m.ModalDepth()      // int - stack depth (0 = none)
m.CurrentModal()    // *ModalEntry - top of stack (nil if empty)
m.ModalSourcePanel() // Panel - base modal's source panel
m.ModalBreadcrumb()  // string - "epic: td-001 > task: td-002"
```

## Overlay Implementation

Modals dim the background to:
- Draw user focus to the modal content
- Provide visual separation between modal and underlying content
- Show context from the underlying panels while focusing attention on the modal

### Dimmed Background Overlay

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

### Alternative: Solid Black Overlay

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

## Adding a New Modal Feature

### 1. Add Fields to ModalEntry

```go
// In model.go, add to ModalEntry struct:
type ModalEntry struct {
    // ... existing fields ...

    // Your feature
    MyFeatureData    []SomeType
    MyFeatureCursor  int
    MyFeatureFocused bool
}
```

### 2. Fetch Data

Update `fetchIssueDetails()` in model.go:

```go
func (m Model) fetchIssueDetails(issueID string) tea.Cmd {
    return func() tea.Msg {
        msg := IssueDetailsMsg{IssueID: issueID}
        // ... existing fetches ...

        // Your feature
        if someCondition {
            msg.MyFeatureData, _ = m.DB.GetMyFeatureData(issueID)
        }
        return msg
    }
}
```

Add field to `IssueDetailsMsg` and handle in `Update()`.

### 3. Add Keymap Context (if needed)

In `keymap/registry.go`:
```go
const (
    ContextMyFeature Context = "my-feature"
)

const (
    CmdMyFeatureAction Command = "my-feature-action"
)
```

In `keymap/bindings.go`:
```go
{Key: "enter", Command: CmdMyFeatureAction, Context: ContextMyFeature},
```

### 4. Update Context Detection

In `currentContext()`:
```go
if m.ModalOpen() {
    if modal := m.CurrentModal(); modal != nil {
        if modal.MyFeatureFocused {
            return keymap.ContextMyFeature
        }
        if modal.TaskSectionFocused {
            return keymap.ContextEpicTasks
        }
    }
    return keymap.ContextModal
}
```

### 5. Handle Commands

In `executeCommand()`:
```go
case keymap.CmdMyFeatureAction:
    if modal := m.CurrentModal(); modal != nil && modal.MyFeatureFocused {
        // Handle action
    }
    return m, nil
```

### 6. Render UI

In `renderModal()`:
```go
// Add section when condition is met
if someCondition && len(modal.MyFeatureData) > 0 {
    header := "MY FEATURE SECTION"
    if modal.MyFeatureFocused {
        header = focusedStyle.Render(header)
    }
    lines = append(lines, header)

    for i, item := range modal.MyFeatureData {
        line := formatItem(item)
        if modal.MyFeatureFocused && i == modal.MyFeatureCursor {
            line = selectedStyle.Render("> " + line)
        }
        lines = append(lines, line)
    }
}
```

## Interactive Modal Buttons

All modals with user actions should use interactive buttons instead of key hints like `[Enter] Confirm [Esc] Cancel`.

### Button Rendering Pattern

```go
// In model struct, add:
buttonFocus int // 0=input, 1=confirm, 2=cancel

// In modal render function:
confirmStyle := styles.Button
cancelStyle := styles.Button
if m.buttonFocus == 1 {
    confirmStyle = styles.ButtonFocused
}
if m.buttonFocus == 2 {
    cancelStyle = styles.ButtonFocused
}

sb.WriteString("\n\n")
sb.WriteString(confirmStyle.Render(" Confirm "))
sb.WriteString("  ")
sb.WriteString(cancelStyle.Render(" Cancel "))
```

### Keyboard Navigation

- **Tab**: Cycle focus between input field and buttons (input → confirm → cancel → input)
- **Shift+Tab**: Reverse cycle
- **Enter**: Execute focused button (or confirm from input)
- **Esc**: Always cancels (global shortcut)

```go
case "tab":
    m.buttonFocus = (m.buttonFocus + 1) % 3
    if m.buttonFocus == 0 {
        m.textInput.Focus()
    } else {
        m.textInput.Blur()
    }
    return m, nil
```

### Mouse Support

TD uses inline bounds calculation in mouse handlers rather than a hit region registry. Add mouse support by:

1. **Add hover state field** to Model:
```go
// In model.go
MyModalButtonHover int // 0=none, 1=confirm, 2=cancel
```

2. **Create click handler** following the confirmation dialog pattern:
```go
// In input.go
func (m Model) handleMyModalClick(x, y int) (Model, tea.Cmd) {
    // Calculate modal dimensions (match your render function)
    modalWidth := 40
    modalHeight := 10

    // Center modal
    modalX := (m.Width - modalWidth) / 2
    modalY := (m.Height - modalHeight) / 2

    // Check if click is inside modal
    if x < modalX || x >= modalX+modalWidth || y < modalY || y >= modalY+modalHeight {
        // Click outside - close modal
        m.MyModalOpen = false
        return m, nil
    }

    // Calculate content bounds (account for border + padding)
    contentStartY := modalY + 2  // border(1) + padding(1)
    buttonY := contentStartY + 5 // lines before buttons

    // Check button row
    if y == buttonY {
        confirmX := modalX + 3
        cancelX := confirmX + 12
        if x >= confirmX && x < confirmX+10 {
            return m.executeConfirm()
        }
        if x >= cancelX && x < cancelX+10 {
            return m.executeCancel()
        }
    }

    return m, nil
}
```

3. **Create hover handler**:
```go
func (m Model) handleMyModalHover(x, y int) (Model, tea.Cmd) {
    // Same bounds calculation as click handler
    modalWidth, modalHeight := 40, 10
    modalX := (m.Width - modalWidth) / 2
    modalY := (m.Height - modalHeight) / 2

    contentStartY := modalY + 2
    buttonY := contentStartY + 5

    m.MyModalButtonHover = 0
    if y == buttonY {
        confirmX := modalX + 3
        cancelX := confirmX + 12
        if x >= confirmX && x < confirmX+10 {
            m.MyModalButtonHover = 1
        } else if x >= cancelX && x < cancelX+10 {
            m.MyModalButtonHover = 2
        }
    }
    return m, nil
}
```

4. **Integrate into handleMouse()** in input.go:
```go
// In handleMouse(), add after other modal handlers:

// My modal click
if m.MyModalOpen && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
    return m.handleMyModalClick(msg.X, msg.Y)
}

// My modal hover
if m.MyModalOpen && msg.Action == tea.MouseActionMotion {
    return m.handleMyModalHover(msg.X, msg.Y)
}

// Add to the "ignore all events" condition:
if m.ModalOpen() || m.FormOpen || m.ConfirmOpen || m.MyModalOpen || ... {
    return m, nil
}
```

### Hover State Rendering

Apply hover styling in render (focus takes precedence over hover):

```go
// In modal render function:
confirmStyle := buttonStyle
cancelStyle := buttonStyle

if m.buttonFocus == 1 {
    confirmStyle = buttonFocusedStyle
} else if m.MyModalButtonHover == 1 {
    confirmStyle = buttonHoverStyle
}

if m.buttonFocus == 2 {
    cancelStyle = buttonFocusedStyle
} else if m.MyModalButtonHover == 2 {
    cancelStyle = buttonHoverStyle
}
```

### List Item Click Support

For modals with clickable list items (like board picker):

```go
func (m Model) handleListModalClick(x, y int) (Model, tea.Cmd) {
    // Calculate modal bounds
    modalWidth := m.Width * 60 / 100
    modalHeight := m.Height * 60 / 100
    modalX := (m.Width - modalWidth) / 2
    modalY := (m.Height - modalHeight) / 2

    // Click outside closes
    if x < modalX || x >= modalX+modalWidth || y < modalY || y >= modalY+modalHeight {
        m.ListModalOpen = false
        return m, nil
    }

    // Calculate item bounds
    contentStartY := modalY + 2  // border + padding
    headerLines := 2             // title + blank line
    itemStartY := contentStartY + headerLines

    // Convert click to item index
    clickedIdx := y - itemStartY
    if clickedIdx >= 0 && clickedIdx < len(m.ListItems) {
        m.ListCursor = clickedIdx
        return m.selectListItem()
    }

    return m, nil
}
```

### Scroll Wheel Support

Add scroll wheel handling in handleMouse():

```go
// In handleMouse(), scroll handling section:
if m.ListModalOpen {
    delta := -3
    if msg.Button == tea.MouseButtonWheelDown {
        delta = 3
    }
    newCursor := m.ListCursor + delta
    m.ListCursor = clamp(newCursor, 0, len(m.ListItems)-1)
    return m, nil
}
```

### Mouse Support Checklist

When adding mouse support to a modal:

1. **Add hover state field** to Model (e.g., `MyModalButtonHover int`)
2. **Create click handler** with bounds calculation matching render
3. **Create hover handler** using same bounds logic
4. **Integrate into handleMouse()** for click, hover, and scroll
5. **Add to ignore condition** to block panel clicks when modal is open
6. **Update render** to apply hover styles (focus > hover > normal)
7. **Initialize hover state** when opening modal (set to 0 or -1)

### Custom Renderer Vertical Padding

**Critical for embedded mode:** When using a custom `ModalRenderer` (e.g., in Sidecar), you must manually add vertical padding to match lipgloss `Padding(1, 2)` behavior.

The custom renderer only handles horizontal padding. Without vertical padding, mouse click/hover coordinates will be off by 1 row when embedded.

```go
// In your modal wrapper function:
if m.ModalRenderer != nil {
    // Add vertical padding (blank lines) for top/bottom padding
    // Custom renderer only handles horizontal padding
    paddedInner := "\n" + inner + "\n"
    return m.ModalRenderer(paddedInner, width+2, height+2, ModalType, depth)
}

// Default lipgloss rendering handles padding automatically
modalStyle := lipgloss.NewStyle().
    Padding(1, 2).  // 1 line top/bottom, 2 char left/right
    ...
```

**Why this matters for mouse support:**
- Lipgloss: content starts at row 2 (border + padding)
- Custom renderer without vertical padding: content starts at row 1 (border only)
- This 1-row difference causes mouse Y calculations to be off

**Symptoms of missing vertical padding:**
- Mouse hover highlights the row below the cursor
- Clicks activate the item below where clicked
- Issue only appears in embedded mode (custom renderer), works in standalone

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

## Visual Indicators

### Border Colors by Depth

| Depth | Color | Code |
|-------|-------|------|
| 1 | Purple/Magenta | `primaryColor` (212) |
| 2 | Cyan | 45 |
| 3+ | Orange | 214 |

### Style Constants

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

// Styles (styles.go)
epicTasksFocusedStyle  // Cyan, bold - focused section header
epicTaskSelectedStyle  // Inverted - selected item
breadcrumbStyle        // Gray, italic - navigation path
```

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

## Testing

Add tests in `model_test.go`:

```go
func TestMyFeature(t *testing.T) {
    m := Model{
        Keymap: newTestKeymap(),
        ModalStack: []ModalEntry{{
            IssueID: "td-001",
            MyFeatureData: []SomeType{{}, {}},
            MyFeatureFocused: true,
        }},
    }

    // Test cursor movement, actions, etc.
}
```

## Key Patterns

1. **Always use `CurrentModal()`** - never index `ModalStack` directly
2. **Check `modal != nil`** before accessing fields
3. **Reset focus state** when pushing/popping modals
4. **Update footer help text** in `wrapModalWithDepth()` for new contexts
5. **Add to help.go** for user-visible documentation
