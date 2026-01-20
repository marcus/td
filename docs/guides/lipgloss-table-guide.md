# Lipgloss Table Implementation Guide

Guide for implementing scrollable, interactive tables using `github.com/charmbracelet/lipgloss/table` in td monitor panels.

## Quick Start

```go
import "github.com/charmbracelet/lipgloss/table"

t := table.New().
    Headers("Col1", "Col2", "Col3").
    Width(contentWidth).
    StyleFunc(myStyleFunc).
    Border(lipgloss.HiddenBorder()).
    BorderHeader(false).
    BorderRow(false).
    BorderColumn(false).
    BorderTop(false).
    BorderBottom(false).
    BorderLeft(false).
    BorderRight(false)

// Add only visible rows (avoids ellipsis overflow row)
visibleRows := layout.dataRowsVisible
startIdx := offset
endIdx := min(startIdx+visibleRows, len(data))
rows := make([][]string, endIdx-startIdx)
for i := startIdx; i < endIdx; i++ {
    rows[i-startIdx] = formatRow(data[i])
}
t.Rows(rows...)

return t.Render()
```

## Table Checklist (Save Future Pain)

- Render only visible rows; avoid `Height` + `Offset` (prevents ellipsis row).
- Disable all table borders when embedding (`BorderTop/Bottom/Left/Right(false)`).
- Fix column widths for stable alignment across pages (use `StyleFunc` + `Width`).
- Centralize layout math; reuse for render + hit testing + visible height.
- Clamp `offset <= len(rows)-dataRowsVisible` and ignore the scroll-indicator line.
- Keep cursor visible on scroll; otherwise clicks/selection desync.

## Critical Learnings

### 1. Offset Must Be Set After Rows (when using Offset)

The lipgloss table **ignores** offset set before rows are added. Always call `.Offset(n)` after `.Rows(...)`:

```go
// WRONG - offset will be ignored
t.Offset(offset).Rows(rows...)

// CORRECT
t.Rows(rows...)
t.Offset(offset)
```

### 1a. Avoid the Ellipsis Overflow Row

When you set `Height(...)` and `Offset(...)`, lipgloss/table renders the last
visible row as an overflow ellipsis when there are more rows. That row is not a
real data row and will throw off cursor math and hit testing.

**Fix**: Don’t use `Height`/`Offset` for scrolling. Instead, slice your rows to
the visible window and pass only those rows to the table.

### 1b. Centralize Layout Math (render + input must agree)

If you compute visible rows differently in rendering vs hit testing, the cursor
will drift and clicks will select the wrong row after scrolling. Use a shared
helper that derives:
- `dataRowsVisible` (header excluded)
- `tableHeight` (header + data)
- `scrollIndicatorRows` (if you reserve a line below the table)

Use that helper for:
- `visibleHeightForPanel()`
- `render...Panel()` slicing
- `hitTest...Row()` clamping and bounds checks

### 1c. Fix Column Widths When Slicing Rows

When you only render the visible rows, lipgloss/table will auto-size columns
based on the current window. If later pages have shorter values, columns shrink
and spacing shifts (e.g., extra space after the time column).

**Fix**: Set fixed widths for non-message columns via the table `StyleFunc`:
- Time, Session, Type, Issue columns should have fixed widths
- Leave the message column flexible to absorb remaining width

### 2. Table Header Takes 1 Line (only if borders are disabled)

When using `Border(lipgloss.HiddenBorder())` with `BorderHeader(false)` **and** all borders disabled (`BorderTop/Bottom/Left/Right(false)`), the table renders:
- Line 0: Header row ("Time", "Sess", etc.)
- Line 1+: Data rows (no separator line)

Account for this in:
- **Hit testing**: `tableHeaderRows = 1`
- **Height calculations**: `dataRowsVisible = tableHeight - 1`
- **Visible height**: See Height Calculation section below

**Note**: `HiddenBorder` still renders blank border lines unless you disable top/bottom.
If you keep those borders on, the header shifts down and all row math must add 1–2 extra lines.

### 2a. Panel Height Must Match renderView Rounding

**CRITICAL**: When calculating panel height for mouse/scroll operations, you MUST match the exact calculation from renderView(), including rounding behavior. Using a different formula causes cursor/scroll sync bugs.

```go
// WRONG - direct multiplication causes rounding mismatch
case PanelActivity:
    panelHeight = int(float64(availableHeight) * m.PaneHeights[2])

// CORRECT - match renderView() which absorbs rounding errors for last panel
panel0 := int(float64(availableHeight) * m.PaneHeights[0])
panel1 := int(float64(availableHeight) * m.PaneHeights[1])
case PanelActivity:
    panelHeight = availableHeight - panel0 - panel1  // Absorbs rounding
```

This is why arrow key scroll keeps cursor visible but mouse wheel doesn't - they use different height calculations.

### 3. ANSI Codes Affect Column Width

When cells contain styled content (ANSI escape codes), the table's width calculation can be off, causing columns to bleed together.

**Solutions**:
- Prefer fixed column widths via `StyleFunc` to stabilize layout.
- If you keep auto-sizing, add trailing spaces to styled cells:

```go
// WRONG - columns may bleed together
session := subtleStyle.Render(sessionID)
badge := formatBadge(typ)

// CORRECT - explicit spacing
session := subtleStyle.Render(sessionID) + " "
badge := formatBadge(typ) + " "
```

### 4. Hide Borders for Embedded Panels

When embedding a table inside a panel that already has borders, hide the table's borders:

```go
t := table.New().
    Border(lipgloss.HiddenBorder()).
    BorderHeader(false).
    BorderRow(false).
    BorderColumn(false).
    BorderTop(false).
    BorderBottom(false).
    BorderLeft(false).
    BorderRight(false)
```

### 5. StyleFunc for Row Selection

Use `StyleFunc` to highlight the selected row. The function receives:
- `row`: Row index (-1 for header, 0+ for data)
- `col`: Column index

```go
func (m Model) tableStyleFunc(visibleCursor int, isActive bool) table.StyleFunc {
    return func(row, col int) lipgloss.Style {
        // Header row
        if row == table.HeaderRow { // -1
            return headerStyle
        }
        // Selected row (only when panel active)
        if isActive && row == visibleCursor {
            return selectedStyle
        }
        return lipgloss.NewStyle()
    }
}
```

**Important**: Pass `cursor - offset` as `visibleCursor` since the StyleFunc operates on visible row indices, not data indices.

## Hit Testing for Mouse Support

### Height Calculation

With scroll indicator (recommended for consistent UX):
```
Panel Total: height
├─ Border Top: 1
├─ Title Row: 1
├─ Content Area: height - 3
│  ├─ Table Header: 1         (with hidden borders)
│  ├─ Data Rows: height - 5   (tableHeight - 1)
│  └─ Scroll Indicator: 1     ("↓ N more below")
└─ Border Bottom: 1

visibleHeightForPanel returns: panelHeight - 5
```

Without scroll indicator:
```
Panel Total: height
├─ Border Top: 1
├─ Title Row: 1
├─ Content Area: height - 3
│  ├─ Table Header: 1         (with hidden borders)
│  └─ Data Rows: height - 4   (tableHeight - 1)
└─ Border Bottom: 1

visibleHeightForPanel returns: panelHeight - 4
```

**Note**: If you slice rows manually (recommended), `visibleHeightForPanel` for
table panels should return `dataRowsVisible`, not `panelHeight - 5`.

### Hit Test Function

```go
func (m Model) hitTestTableRow(relY int) int {
    if len(m.Data) == 0 {
        return -1
    }

    layout := activityTableMetrics(panelHeight)
    dataRowsVisible := layout.dataRowsVisible

    offset := m.ScrollOffset[panel]
    maxOffset := len(m.Data) - dataRowsVisible
    if maxOffset < 0 {
        maxOffset = 0
    }
    if offset > maxOffset {
        offset = maxOffset
    }
    if offset < 0 {
        offset = 0
    }

    // Table header takes 1 line (with hidden borders)
    // Use 2 if you have visible borders
    const tableHeaderRows = 1

    if relY >= tableHeaderRows+dataRowsVisible {
        return -1 // Click on scroll indicator / padding line
    }
    if relY < tableHeaderRows {
        return -1 // Click on header area
    }

    // Convert to data row index
    dataRowY := relY - tableHeaderRows
    rowIdx := dataRowY + offset

    if rowIdx >= 0 && rowIdx < len(m.Data) {
        return rowIdx
    }
    return -1
}
```

### Visible Height Calculation

```go
func (m Model) visibleHeightForPanel(panel Panel) int {
    panelHeight := calculatePanelHeight()  // MUST match renderView() calculation!

    // With scroll indicator: title(1) + border(2) + table header(1) + indicator(1) = 5
    return panelHeight - 5

    // Without scroll indicator: title(1) + border(2) + table header(1) = 4
    // return panelHeight - 4
}
```

## Scroll Handling

### Keep Cursor Visible During Mouse Wheel Scroll

When users scroll with the mouse wheel, keep the cursor within the visible area. This prevents confusion where the highlighted row disappears and clicking doesn't work as expected.

```go
func (m Model) handleMouseWheel(x, y, delta int) (tea.Model, tea.Cmd) {
    // ... scroll offset update ...
    m.ScrollOffset[panel] = newOffset

    // Keep cursor visible when mouse scrolling
    if m.Cursor != nil {
        visibleHeight := m.visibleHeightForPanel(panel)
        cursor := m.Cursor[panel]

        // If cursor went above viewport, move it to first visible row
        if cursor < newOffset {
            m.Cursor[panel] = newOffset
        }
        // If cursor went below viewport, move it to last visible row
        if cursor >= newOffset+visibleHeight {
            m.Cursor[panel] = newOffset + visibleHeight - 1
            if m.Cursor[panel] >= count {
                m.Cursor[panel] = count - 1
            }
        }
    }
    return m, nil
}
```

**Why this matters**: If the cursor goes off-screen during scrolling:
1. No row appears highlighted (confusing UX)
2. Click selection may not work correctly due to cursor/offset mismatch
3. User loses track of their position in the list

### Ensure Cursor Visible (Keyboard Navigation)

For table-based panels, don't add extra offset for scroll indicators (tables don't have them):

```go
if cursor >= offset+effectiveHeight {
    newOffset := cursor - effectiveHeight + 1
    // Only add extra for panels with scroll indicators
    if panel != PanelWithTable && offset == 0 && newOffset > 0 {
        newOffset++ // Compensate for "more above" indicator
    }
    m.ScrollOffset[panel] = newOffset
}
```

### Position Counter in Title

Instead of scroll indicators, show position in the panel title:

```go
panelTitle := "MY TABLE"
if totalRows > dataRowsVisible {
    endPos := offset + dataRowsVisible
    if endPos > totalRows {
        endPos = totalRows
    }
    panelTitle = fmt.Sprintf("MY TABLE (%d-%d of %d)", offset+1, endPos, totalRows)
}
```

## Style Definitions

```go
// styles.go

// Column widths for fixed-width columns
const (
    colTimeWidth    = 5  // "15:04"
    colSessionWidth = 10 // truncated ID
    colTypeWidth    = 5  // "[LOG]"
    colIssueWidth   = 8  // Issue ID
)

// Table styles
var (
    tableHeaderStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("255"))

    tableSelectedStyle = lipgloss.NewStyle().
        Background(lipgloss.Color("237"))
)
```

## Complete Example

See `pkg/monitor/view.go`:
- `activityTableStyleFunc()` - StyleFunc implementation
- `formatActivityRow()` - Row formatting with spacing
- `renderActivityPanel()` - Complete table rendering

See `pkg/monitor/input.go`:
- `hitTestActivityRow()` - Mouse hit testing
- `visibleHeightForPanel()` - Height calculation
- `ensureCursorVisible()` - Scroll handling

## Checklist for New Table Implementations

- [ ] Import `github.com/charmbracelet/lipgloss/table`
- [ ] Define column width constants
- [ ] Define header and selected row styles
- [ ] Create StyleFunc that handles header (-1) and selection
- [ ] Create row formatting function with trailing spaces on styled cells
- [ ] Set table Width, Height, and hidden borders
- [ ] Call Offset() AFTER Rows()
- [ ] Update hit testing with `tableHeaderRows = 1` (with hidden borders) or `2` (with visible borders)
- [ ] Ensure visibleHeightForPanel matches renderView() panel height calculation (rounding!)
- [ ] Update visible height calculation (`panelHeight - 5` with scroll indicator, `-4` without)
- [ ] Update ensureCursorVisible to skip scroll indicator compensation
- [ ] Show position counter in panel title + "N more below" indicator
- [ ] Test: clicks select correct row after scrolling, mouse scroll keeps cursor visible, keyboard works
