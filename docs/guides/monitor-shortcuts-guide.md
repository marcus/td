# Monitor TUI Shortcuts Guide

How to implement keyboard shortcuts in the `td monitor` TUI. For CLI commands, see `cli-commands-guide.md`.

## Quick checklist
- Command constant in `registry.go`
- Key binding in `bindings.go` (with correct context)
- Handler case in `model.go`
- Export metadata in `export.go` (for sidecar visibility)
- Status message cleanup with `clearStatusAfterDelay()`
- Nil checks before accessing issues

## Four things must match

1. **Command constant** in `registry.go` → e.g., `CmdCopyIDToClipboard`
2. **Key binding** in `bindings.go` → e.g., `{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextMain}`
3. **Handler case** in `model.go` → e.g., `case keymap.CmdCopyIDToClipboard:`
4. **Export metadata** in `export.go` → `CmdCopyIDToClipboard: {"Copy ID", "Copy issue ID", 3}`

## Architecture

| File | Purpose |
|------|---------|
| `pkg/monitor/keymap/registry.go` | Command constants |
| `pkg/monitor/keymap/bindings.go` | Key→command mappings by context |
| `pkg/monitor/keymap/export.go` | Metadata for sidecar export |
| `pkg/monitor/keymap/help.go` | Help text |
| `pkg/monitor/model.go` | Command handlers |

## Contexts (quick reference)

| Context | View |
|---------|------|
| `ContextMain` | Main list (root) |
| `ContextModal` | Issue detail modal |
| `ContextBoard` | Swimlanes/backlog view |
| `ContextBoardPicker` | Board selection modal |
| `ContextStats` | Statistics modal |
| `ContextSearch` | Search input |
| `ContextConfirm` | Confirmation dialog |
| `ContextForm` | Create/edit form |
| `ContextEpicTasks` | Epic task list focused |
| `ContextParentEpicFocused` | Parent epic row focused |
| `ContextHandoffs` | Handoffs modal |
| `ContextGlobal` | Always active (unless overridden) |

## Implementing a Shortcut

### Step 1: Add command constant

`pkg/monitor/keymap/registry.go`:
```go
const (
    CmdCopyIDToClipboard Command = "copy-id-to-clipboard"
)
```

### Step 2: Add key bindings

`pkg/monitor/keymap/bindings.go`:
```go
// Add for each context where it should work
{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextMain, Description: "Copy issue ID"},
{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextModal, Description: "Copy issue ID"},
```

**Key pattern:** `ctrl+<key>` extends base command to extreme (e.g., `J` moves down, `ctrl+J` moves to bottom).

### Step 3: Add handler

`pkg/monitor/model.go`:
```go
func (m Model) copyIssueIDToClipboard() (tea.Model, tea.Cmd) {
    var issueID string

    if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
        issueID = modal.Issue.ID
    } else {
        issueID = m.SelectedIssueID(m.ActivePanel)
    }

    if issueID == "" {
        m.StatusMessage = "No issue selected"
        return m, m.clearStatusAfterDelay()
    }

    if err := copyToClipboard(issueID); err != nil {
        m.StatusMessage = "Copy failed: " + err.Error()
    } else {
        m.StatusMessage = "Copied ID: " + issueID
    }
    return m, m.clearStatusAfterDelay()
}
```

### Step 4: Wire up command

`pkg/monitor/model.go`:
```go
// In handleMainCommand():
case keymap.CmdCopyIDToClipboard:
    return m.copyIssueIDToClipboard()

// In handleModalCommand():
case keymap.CmdCopyIDToClipboard:
    return m.copyIssueIDToClipboard()
```

### Step 5: Add export metadata

`pkg/monitor/keymap/export.go`:
```go
var commandMetadata = map[Command]struct {
    Name        string
    Description string
    Priority    int
}{
    CmdCopyIDToClipboard: {"Copy ID", "Copy issue ID to clipboard", 3},
}
```

`pkg/monitor/keymap/help.go`:
```go
case CmdCopyIDToClipboard:
    return "Copy issue ID to clipboard"
```

## Priority Guidelines

| Priority | Usage | Visibility |
|----------|-------|------------|
| 1 | Core actions (open, approve, search) | Always in footer |
| 2 | Common operations (sort, filter, toggle) | Footer when space allows |
| 3 | Utility commands (help, copy, quit) | Palette only |
| 4-5 | Navigation, context-specific | Palette only |

## Accessing Issues

### From list view
```go
issueID := m.SelectedIssueID(m.ActivePanel)
issue := m.getSelectedIssueForPanel(m.ActivePanel)
```

### From modal
```go
if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
    issue := modal.Issue
}
```

### Panel-specific
- `PanelCurrentWork`: `m.FocusedIssue` or `m.InProgress[]`
- `PanelTaskList`: `m.TaskListRows[cursor].Issue`
- `PanelActivity`: Only has `IssueID`

## Status Messages

```go
m.StatusMessage = "Copied to clipboard"
return m, m.clearStatusAfterDelay()  // Clears after 2s
```

## Clipboard Operations

```go
copyToClipboard(text)
formatIssueAsMarkdown(issue)
formatEpicAsMarkdown(epic, children)
```

## Common Mistakes

| Symptom | Fix |
|---------|-----|
| Shortcut works in list but not modal | Add binding for both `ContextMain` and `ContextModal` |
| Status message stays forever | Return `m.clearStatusAfterDelay()` |
| Panic on empty list | Check `cursor < len(rows)` before access |
| Shortcut missing from sidecar | Add entry to `commandMetadata` in `export.go` |
| Handler never called | Ensure command constant matches in binding and case |

## Sidecar Integration

TD exports shortcuts to sidecar for command palette integration.

### Export functions
```go
bindings := registry.ExportBindings()   // []ExportedBinding
commands := registry.ExportCommands()   // []ExportedCommand
```

### Context mapping to sidecar

| TD Context | Sidecar Context |
|------------|-----------------|
| `ContextMain` | `td-monitor` |
| `ContextModal` | `td-modal` |
| `ContextBoard` | `td-board` |
| `ContextStats` | `td-stats` |
| `ContextSearch` | `td-search` |
| `ContextConfirm` | `td-confirm` |
| `ContextForm` | `td-form` |

For sidecar's side of the integration, see the Sidecar repository documentation (the keyboard shortcuts reference is maintained there, not in this repo).

### Checklist for sidecar visibility
1. Binding in `bindings.go` with context
2. Command constant in `registry.go`
3. Entry in `commandMetadata` (export.go) with name, description, priority
4. Context in `contextToSidecar` map (if new context)

## Testing

```go
func TestCopyIssueIDToClipboard(t *testing.T) {
    m := newTestModel()
    // Setup...

    updated, _ := m.handleKey(tea.KeyMsg{
        Type:  tea.KeyRunes,
        Runes: []rune{'Y'},
    })

    m2 := updated.(Model)
    if m2.StatusMessage != "Copied ID: td-abc123" {
        t.Errorf("got %q", m2.StatusMessage)
    }
}
```
