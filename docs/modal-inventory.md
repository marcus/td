# Modal Inventory and Compliance

This document provides a comprehensive inventory of all modals in the TD monitor.

**New Implementation**: Use the declarative modal library documented in [declarative-modal-guide.md](guides/declarative-modal-guide.md).

**Legacy Reference**: The original guide is at [modal-system-guide.md](guides/deprecated/modal-system-guide.md) (deprecated).

## Compliance Features

| Feature | Description |
|---------|-------------|
| ModalStack | Uses stack-based architecture for nested modals |
| OverlayModal | Uses dimmed background overlay via OverlayModal() |
| Depth Colors | Border color changes by depth (purple/cyan/orange) |
| Keyboard Nav | Tab, Shift+Tab, arrow keys, Enter, Esc support |
| Mouse Click | Left-click handler for interactions |
| Mouse Hover | Hover state tracking and visual feedback |
| Mouse Scroll | Mouse wheel scroll wheel support |
| Interactive Buttons | Uses styled buttons instead of key hints |
| Context | Dedicated keymap context for keybindings |
| Commands | Handles commands in executeCommand() |
| Scrollable | Can scroll if content exceeds available height |
| Help Text | Shows keybindings/help in footer |
| Declarative | Uses declarative modal library (modal.New()) |

## Modal Compliance Matrix

| Modal | Purpose | Declarative | OverlayModal | Keyboard Nav | Mouse Click | Mouse Hover | Mouse Scroll | Interactive Buttons | Scrollable | Help Text |
|-------|---------|:-----------:|:------------:|:-------------:|:----------:|:----------:|:----------:|:------------------:|:----------:|:---------:|
| Issue Details | View/interact with issue, navigate dependencies | NO | YES | YES | YES | YES | YES | YES | YES | YES |
| Statistics | Show project stats, status/type/priority breakdown | **YES** | YES | YES | YES | YES | YES | YES | YES | YES |
| Handoffs | View recent handoffs, open issue from list | **YES** | YES | YES | YES | YES | YES | YES | YES | YES |
| Form Modal | Create/edit issues, inline form with fields | NO | YES | YES | YES | YES | NO | YES | NO | YES |
| Delete Confirmation | Confirm destructive delete action | **YES** | YES | YES | YES | YES | NO | YES | NO | YES |
| Close Confirmation | Confirm close with optional reason text | **YES** | YES | YES | YES | YES | NO | YES | NO | YES |
| Board Picker | Select board for new issue | **YES** | YES | YES | YES | YES | YES | YES | YES | YES |
| Help Modal | Show keybindings and navigation help | NO | YES | YES | NO | NO | YES | NO | YES | N/A |
| TDQ Help | Show query language syntax | **YES** | YES | YES | YES | YES | NO | YES | NO | YES |

## Migration Status

The following modals have been migrated to the declarative modal library:

| Modal | Migration Status | Commit |
|-------|-----------------|--------|
| Statistics | ✅ Migrated | Uses modal.Custom() for stats content, modal.Buttons() for Close |
| Handoffs | ✅ Migrated | Uses modal.List() for handoff items, modal.Buttons() for Open/Close |
| Board Picker | ✅ Migrated | Uses modal.List() for board items, modal.Buttons() for Select/Cancel |
| Delete Confirmation | ✅ Migrated | Uses modal.Text() + modal.Buttons() with BtnDanger() |
| Close Confirmation | ✅ Migrated | Uses modal.InputWithLabel() + modal.Buttons() |
| TDQ Help | ✅ Migrated | Uses modal.Custom() for help content, modal.Buttons() for Close |

**Not migrating** (per original epic scope):
- Issue Details Modal - too complex, already fully compliant, keep as-is
- Form Modal - uses huh library, keep as-is
- Help Modal - full-screen overlay, not a standard modal

## Detailed Compliance Analysis

### Issue Details Modal (FULLY COMPLIANT)
**Location**: `pkg/monitor/view.go`, `renderModal()`

**Status**: Exceeds guide requirements (not using declarative library - too complex)
- Uses full ModalStack architecture with depth-aware styling
- All keyboard navigation (↑↓ scroll, Tab focus, Enter select, Esc close)
- Complete mouse support (click, hover, scroll)
- Interactive breadcrumb and section headers
- Multiple focusable sections (main content, task list, blocked-by, blocks)
- Proper context detection for keybindings
- Full command handling for all interactions
- Footer help text with keybindings

**Non-Conformances**: None

---

### Statistics Modal (FULLY COMPLIANT - DECLARATIVE)
**Location**: `pkg/monitor/modal.go`, `createStatsModal()`

**Status**: Migrated to declarative modal library
- Uses `modal.New()` with `VariantDefault`
- `modal.Custom()` section for scrollable stats content (bar charts, breakdowns)
- `modal.Buttons()` with Close button
- Automatic keyboard navigation via `HandleKey()`
- Automatic mouse support via `HandleMouse()`
- Scrollable content with scroll clamping
- Footer help text

**Non-Conformances**: None

---

### Handoffs Modal (FULLY COMPLIANT - DECLARATIVE)
**Location**: `pkg/monitor/modal.go`, `createHandoffsModal()`

**Status**: Migrated to declarative modal library
- Uses `modal.New()` with `VariantDefault`
- `modal.List()` section for handoff items with cursor navigation
- `modal.Buttons()` with Open Issue and Close buttons
- Automatic keyboard navigation (↑↓/j/k select, Enter open, Esc close)
- Full mouse support (click, hover, scroll)
- Footer help text

**Non-Conformances**: None

---

### Form Modal (COMPLIANT)
**Location**: `pkg/monitor/view.go`, `renderFormModal()`

**Status**: Compliant with appropriate adaptations (uses huh library, not declarative)
- Uses OverlayModal with dimmed background
- Keyboard navigation: Tab/Shift+Tab between fields, Ctrl+S submit, Esc cancel
- Mouse support: via huh library (click, focus management)
- Interactive Submit/Cancel buttons
- Custom context ("FormInput" fields in form library)
- Command handling for Ctrl+S, Ctrl+X, Esc
- Footer help text
- Fixed border color (cyan) appropriate for form type

**Non-Conformances**: None - form library provides interactive UI

---

### Delete Confirmation Modal (FULLY COMPLIANT - DECLARATIVE)
**Location**: `pkg/monitor/modal.go`, `createDeleteConfirmModal()`

**Status**: Migrated to declarative modal library
- Uses `modal.New()` with `VariantDanger` (red border)
- `modal.Text()` for issue title display
- `modal.Buttons()` with Yes (BtnDanger) and No buttons
- Y/N quick keys for fast confirmation
- Automatic keyboard navigation via `HandleKey()`
- Full mouse support (click, hover)
- Footer help text showing available keys

**Non-Conformances**: None

---

### Close Confirmation Modal (FULLY COMPLIANT - DECLARATIVE)
**Location**: `pkg/monitor/modal.go`, `createCloseConfirmModal()`

**Status**: Migrated to declarative modal library
- Uses `modal.New()` with `VariantDanger` (red border)
- `modal.Text()` for issue title display
- `modal.InputWithLabel()` for optional reason text input
- `modal.Buttons()` with Confirm and Cancel buttons
- Tab cycles between input and buttons automatically
- Automatic keyboard navigation via `HandleKey()`
- Full mouse support (click, hover)
- Footer help text showing available keys

**Non-Conformances**: None

---

### Board Picker Modal (FULLY COMPLIANT - DECLARATIVE)
**Location**: `pkg/monitor/modal.go`, `createBoardPickerModal()`

**Status**: Migrated to declarative modal library
- Uses `modal.New()` with `VariantDefault` (purple border)
- `modal.List()` section for board items with cursor navigation
- `modal.Buttons()` with Select and Cancel buttons
- Automatic keyboard navigation (↑↓/j/k select, Enter select, Esc cancel)
- Full mouse support (click, hover, scroll)
- Footer help text

**Non-Conformances**: None

---

### Help Modal (N/A - SPECIAL CASE)
**Location**: `pkg/monitor/view.go`, `renderHelp()`

**Status**: Not subject to standard compliance - full-screen overlay
- Full terminal overlay (not centered modal)
- Keyboard navigation: j/k line scroll, Ctrl+d/u half-page, G/gg jump to ends, Page Up/Down
- Mouse scroll wheel support
- Scrollable content with scroll indicators (▲/▼)
- Border styling (purple)
- Scroll hints in footer

**Notes**: Help modal is full-screen by design. OverlayModal used with full background content.

---

### TDQ Help Modal (FULLY COMPLIANT - DECLARATIVE)
**Location**: `pkg/monitor/modal.go`, `createTDQHelpModal()`

**Status**: Migrated to declarative modal library
- Uses `modal.New()` with `VariantInfo` (cyan border)
- `modal.Custom()` section for TDQ query syntax help text
- `modal.Buttons()` with Close button
- Automatic keyboard navigation via `HandleKey()`
- Full mouse support via `HandleMouse()`
- Footer help text

**Non-Conformances**: None

---

## Patterns and Anti-Patterns

### Good Patterns Observed

1. **Declarative Modal Library Usage** (6/9 modals)
   - Statistics, Handoffs, Board Picker, Delete Confirmation, Close Confirmation, TDQ Help
   - Consistent API: `modal.New()` → `AddSection()` → `Render()` / `HandleKey()` / `HandleMouse()`
   - Automatic hit region management eliminates off-by-one bugs

2. **Consistent OverlayModal Usage** (8/9 modals)
   - All primary modals use OverlayModal for dimmed background overlay
   - Provides visual focus and context preservation

3. **Comprehensive Keyboard Navigation**
   - All modals support Tab/Shift+Tab for focus cycling
   - Esc consistently closes
   - Enter triggers focused action

4. **Full Mouse Support**
   - All declarative modals have click and hover support
   - Scroll wheel support for scrollable content

5. **Interactive Buttons**
   - All modals now use styled button pairs instead of text hints
   - Confirmation modals use danger styling for destructive actions

### Legacy Patterns (Non-Declarative Modals)

1. **Issue Details Modal**
   - Too complex for declarative library (nested navigation, multiple focus sections)
   - Manual hit region calculation justified by complexity

2. **Form Modal**
   - Uses huh library which provides its own declarative UI
   - Appropriate choice for form handling

---

## Testing Checklist

Current modal implementations should verify:

- [ ] Issue Details: All keyboard navigation, depth colors change correctly, mouse click on sections
- [x] Statistics: Scroll clamping, mouse wheel, ESC closes, Close button works
- [x] Handoffs: Cursor navigation, mouse click/hover, Enter opens issue, Open Issue button works
- [ ] Form: All field types interactive, Tab cycles focus, Ctrl+S submits
- [x] Delete Confirmation: Tab cycles buttons, Y/N quick keys, hover states, danger styling
- [x] Close Confirmation: Input field focus, button cycling, reason text preserved
- [x] Board Picker: Mouse hover tracking, scroll, click selects, Select button works
- [ ] Help: Scroll boundaries (G/gg), scroll indicator display
- [x] TDQ Help: Close button, hover states, Esc closes, Tab cycles focus

---

## File Locations

All implementations located in `pkg/monitor/`:

**Declarative Modal Library**:
- **Modal library**: `modal/modal.go`, `modal/section.go`, `modal/input.go`, `modal/list.go`
- **Mouse handling**: `mouse/mouse.go`

**Modal State and Functions**:
- **Modal state**: `types.go` (ModalEntry struct, Model fields)
- **Stack management**: `modal.go` (push/pop/navigate functions, declarative modal creation)
- **Rendering**: `view.go` (all render functions)
- **Keyboard/Mouse**: `input.go`, `commands.go` (all handlers)
- **Form dimensions**: `form_modal.go`
- **Overlay compositing**: `overlay.go`
- **Keybindings**: `keymap/registry.go`, `keymap/bindings.go`
