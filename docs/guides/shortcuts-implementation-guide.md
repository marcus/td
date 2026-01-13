# Shortcuts Implementation Guide

This guide covers how to implement new commands in `td`, with emphasis on the shortcuts system.

## Architecture Overview

Commands are built on [Cobra](https://github.com/spf13/cobra). Key files:

| Location | Purpose |
|----------|---------|
| `cmd/root.go` | Root command, groups, custom help template |
| `cmd/*.go` | Individual command implementations |
| `main.go` | Entry point calling `cmd.Execute()` |

## Command Groups

Commands are organized into groups (defined in `root.go:112-120`):

| GroupID | Purpose | Examples |
|---------|---------|----------|
| `core` | Basic CRUD operations | `create`, `list`, `show`, `update`, `delete` |
| `workflow` | Issue lifecycle | `start`, `review`, `approve`, `handoff` |
| `query` | Data analysis | `tree`, `dependencies`, `critical-path` |
| `shortcuts` | Quick-access filtered lists | `ready`, `next`, `blocked`, `in-review` |
| `session` | Session management | `status`, `usage`, `focus`, `ws` |
| `files` | File linking | `link`, `unlink`, `files` |
| `system` | Tooling | `version`, `info`, `export`, `import` |

## Implementing a New Command

### Basic Command Structure

```go
package cmd

import (
    "fmt"

    "github.com/marcus/td/internal/db"
    "github.com/marcus/td/internal/output"
    "github.com/spf13/cobra"
)

var myCmd = &cobra.Command{
    Use:     "mycommand [args]",
    Aliases: []string{"mc", "mycmd"},  // Optional short names
    Short:   "One-line description",
    Long:    `Detailed description with examples.`,
    GroupID: "shortcuts",  // Assigns to a help section
    Args:    cobra.MinimumNArgs(1),  // Argument validation
    RunE: func(cmd *cobra.Command, args []string) error {
        // Implementation here
        return nil
    },
}

func init() {
    rootCmd.AddCommand(myCmd)

    // Flag registration
    myCmd.Flags().Bool("json", false, "JSON output")
    myCmd.Flags().StringP("filter", "f", "", "Filter value")
}
```

### Standard Setup Pattern

Most commands follow this initialization pattern:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    baseDir := getBaseDir()  // Get project root

    database, err := db.Open(baseDir)
    if err != nil {
        output.Error("%v", err)
        return err
    }
    defer database.Close()

    // For session-aware commands:
    sess, err := session.GetOrCreate(baseDir)
    if err != nil {
        output.Error("%v", err)
        return err
    }

    // Command logic...

    return nil
}
```

## Implementing Shortcuts

Shortcuts are specialized list commands in the `shortcuts` group. They use a shared helper:

### The runListShortcut Pattern

```go
// Shared core for list shortcuts (list.go:194-211)
func runListShortcut(opts db.ListIssuesOptions) (*listShortcutResult, error) {
    baseDir := getBaseDir()

    database, err := db.Open(baseDir)
    if err != nil {
        output.Error("%v", err)
        return nil, err
    }
    defer database.Close()

    issues, err := database.ListIssues(opts)
    if err != nil {
        output.Error("failed to list issues: %v", err)
        return nil, err
    }

    return &listShortcutResult{issues: issues}, nil
}
```

### Example: Simple Shortcut

```go
var readyCmd = &cobra.Command{
    Use:     "ready",
    Short:   "List open issues sorted by priority",
    GroupID: "shortcuts",
    RunE: func(cmd *cobra.Command, args []string) error {
        result, err := runListShortcut(db.ListIssuesOptions{
            Status: []models.Status{models.StatusOpen},
            SortBy: "priority",
        })
        if err != nil {
            return err
        }

        for _, issue := range result.issues {
            fmt.Println(output.FormatIssueShort(&issue))
        }

        if len(result.issues) == 0 {
            fmt.Println("No open issues")
        }
        return nil
    },
}
```

### Example: Session-Aware Shortcut

```go
var reviewableCmd = &cobra.Command{
    Use:     "reviewable",
    Short:   "Show issues awaiting review that you can review",
    GroupID: "shortcuts",
    RunE: func(cmd *cobra.Command, args []string) error {
        baseDir := getBaseDir()
        sess, err := session.GetOrCreate(baseDir)
        if err != nil {
            output.Error("%v", err)
            return err
        }

        result, err := runListShortcut(db.ListIssuesOptions{
            ReviewableBy: sess.ID,
        })
        if err != nil {
            return err
        }

        for _, issue := range result.issues {
            fmt.Printf("%s  (impl: %s)\n",
                output.FormatIssueShort(&issue),
                issue.ImplementerSession)
        }

        if len(result.issues) == 0 {
            fmt.Println("No issues awaiting your review")
        }
        return nil
    },
}
```

### Registering Shortcuts

In `init()`:

```go
func init() {
    rootCmd.AddCommand(listCmd)
    rootCmd.AddCommand(reviewableCmd)
    rootCmd.AddCommand(blockedListCmd)
    rootCmd.AddCommand(readyCmd)
    // ... other shortcuts
}
```

## Flag Patterns

### Standard Flag Types

```go
// Boolean flags
cmd.Flags().Bool("json", false, "JSON output")
cmd.Flags().BoolP("verbose", "v", false, "Verbose output")

// String flags with short form
cmd.Flags().StringP("filter", "f", "", "Filter value")

// String array (multiple values)
cmd.Flags().StringArray("status", nil, "Status filter (repeatable)")

// Integer flags
cmd.Flags().IntP("limit", "n", 50, "Result limit")
```

### Flag Aliases Pattern

For user convenience, support multiple names for the same concept:

```go
// In init():
createCmd.Flags().StringP("labels", "l", "", "Comma-separated labels")
createCmd.Flags().String("label", "", "Alias for --labels")
createCmd.Flags().String("tags", "", "Alias for --labels")
createCmd.Flags().String("tag", "", "Alias for --labels")

// In RunE, resolve in priority order:
labelsStr, _ := cmd.Flags().GetString("labels")
if labelsStr == "" {
    if s, _ := cmd.Flags().GetString("label"); s != "" {
        labelsStr = s
    }
}
if labelsStr == "" {
    if s, _ := cmd.Flags().GetString("tags"); s != "" {
        labelsStr = s
    }
}
// ...
```

### Common Flags Across Commands

These flags appear frequently and should be supported consistently:

| Flag | Purpose | Commands |
|------|---------|----------|
| `--json` | JSON output | All list/show commands |
| `--quiet/-q` | Suppress output | Mutation commands |
| `--force` | Override safety checks | `start`, `delete`, etc. |
| `--reason` | Action justification | `start`, `block`, etc. |
| `--limit/-n` | Result limit | List commands |
| `--sort` | Sort field | List commands |
| `--reverse/-r` | Reverse sort | List commands |

## Subcommand Pattern

For related commands, use a parent with subcommands (see `ws.go`):

```go
var wsCmd = &cobra.Command{
    Use:     "ws",
    Aliases: []string{"worksession"},
    Short:   "Work session commands",
    GroupID: "session",
}

var wsStartCmd = &cobra.Command{
    Use:   "start [name]",
    Short: "Start a named work session",
    Args:  cobra.ExactArgs(1),
    RunE:  func(cmd *cobra.Command, args []string) error { ... },
}

var wsTagCmd = &cobra.Command{
    Use:   "tag [issue-ids...]",
    Short: "Associate issues with the current work session",
    Args:  cobra.MinimumNArgs(1),
    RunE:  func(cmd *cobra.Command, args []string) error { ... },
}

func init() {
    rootCmd.AddCommand(wsCmd)
    wsCmd.AddCommand(wsStartCmd)
    wsCmd.AddCommand(wsTagCmd)
    // ...
}
```

## Output Patterns

### Use the output package

```go
import "github.com/marcus/td/internal/output"

// Errors (logs and returns)
output.Error("failed to load: %v", err)

// Warnings (non-fatal)
output.Warning("issue is blocked: %s", id)

// Standard output
fmt.Println(output.FormatIssueShort(&issue))
fmt.Print(output.FormatIssueLong(&issue, logs, handoff))
```

### JSON Output Support

All list/show commands should support JSON:

```go
if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
    return output.JSON(result)
}
```

### Action Confirmation Format

Use consistent uppercase for action confirmations:

```go
fmt.Printf("CREATED %s\n", issue.ID)
fmt.Printf("STARTED %s (session: %s)\n", issueID, sess.ID)
fmt.Printf("WORK SESSION ENDED\n")
```

## Undo Support

Log actions for undo capability:

```go
import "github.com/marcus/td/internal/models"

// Capture state before mutation
prevData, _ := json.Marshal(issue)

// Perform mutation
issue.Status = models.StatusInProgress
database.UpdateIssue(issue)

// Log for undo
newData, _ := json.Marshal(issue)
database.LogAction(&models.ActionLog{
    SessionID:    sess.ID,
    ActionType:   models.ActionStart,
    EntityType:   "issue",
    EntityID:     issue.ID,
    PreviousData: string(prevData),
    NewData:      string(newData),
})
```

## Validation Patterns

### Model Validation

Use model validators for enum types:

```go
if t != "" {
    typ := models.Type(t)
    if !models.IsValidType(typ) {
        output.Error("invalid type: %s (valid: bug, feature, task, epic, chore)", t)
        return fmt.Errorf("invalid type: %s", t)
    }
    issue.Type = typ
}
```

### Argument Validation

Use Cobra's built-in validators:

```go
Args: cobra.ExactArgs(1),      // Exactly 1 arg
Args: cobra.MinimumNArgs(1),   // At least 1 arg
Args: cobra.MaximumNArgs(2),   // At most 2 args
Args: cobra.RangeArgs(1, 3),   // Between 1 and 3 args
```

## Workflow Hints

For common mistakes, add workflow hints in `root.go`:

```go
func handleWorkflowHint(cmd string) bool {
    switch cmd {
    case "done", "complete", "submit":
        showWorkflowHint(cmd, "review",
            "Use 'td close --self-close-exception \"reason\"' only for admin closures.")
        return true
    }
    return false
}
```

## Code Review Checklist

When implementing new commands:

1. **Group assignment**: Use appropriate `GroupID`
2. **Aliases**: Add intuitive short forms where helpful
3. **Error handling**: Use `output.Error()` before returning errors
4. **Database cleanup**: Always `defer database.Close()`
5. **JSON support**: Add `--json` flag for list/show commands
6. **Empty state**: Handle and message empty results gracefully
7. **Undo logging**: Log mutations for undo support
8. **Session awareness**: Get session when needed for context
9. **Consistent output**: Use UPPERCASE for action confirmations
10. **Help text**: Include `Short` and optional `Long` with examples

## Common Pitfalls

### Duplicate Database Opens

**Wrong:**
```go
// Session-aware shortcut that opens DB twice
sess, err := session.GetOrCreate(baseDir)  // Opens DB internally
result, err := runListShortcut(opts)       // Opens DB again
```

**Right:**
```go
// For session-aware shortcuts, open DB once
baseDir := getBaseDir()
database, err := db.Open(baseDir)
if err != nil { ... }
defer database.Close()

sess, err := session.GetOrCreate(baseDir)
// Use database directly instead of runListShortcut
```

### Missing Empty State Messages

**Wrong:**
```go
for _, issue := range result.issues {
    fmt.Println(output.FormatIssueShort(&issue))
}
// No output if empty
```

**Right:**
```go
for _, issue := range result.issues {
    fmt.Println(output.FormatIssueShort(&issue))
}
if len(result.issues) == 0 {
    fmt.Println("No issues found")
}
```

### Inconsistent Flag Handling

**Wrong:**
```go
// Ignoring error (which is fine) but not checking value
json, _ := cmd.Flags().GetBool("json")
```

**Right:**
```go
// Explicitly document that error is intentionally ignored
// (Cobra flags don't error for defined flags)
if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
    return output.JSON(result)
}
```

## Testing Commands

Commands can be tested by setting `baseDirOverride`:

```go
func TestMyCommand(t *testing.T) {
    tempDir := t.TempDir()
    baseDirOverride = &tempDir
    defer func() { baseDirOverride = nil }()

    // Initialize database
    database, _ := db.Open(tempDir)
    defer database.Close()

    // Test command execution
    // ...
}
```

## File Organization

When adding new commands, follow these conventions:

- **Single command**: Add to existing file if related (e.g., new list shortcut goes in `list.go`)
- **Command group**: Create new file (e.g., `ws.go` for work session commands)
- **Naming**: Use command name as file name (e.g., `start.go`, `review.go`)

---

# Monitor TUI Shortcuts

This section covers implementing keyboard shortcuts in the `td` monitor TUI (bubbletea-based).

## Architecture Overview

Key files for monitor shortcuts:

| Location | Purpose |
|----------|---------|
| `pkg/monitor/keymap/registry.go` | Command constants and registry |
| `pkg/monitor/keymap/bindings.go` | Key-to-command mappings by context |
| `pkg/monitor/keymap/help.go` | Help text and descriptions |
| `pkg/monitor/keymap/export.go` | Command metadata for UI |
| `pkg/monitor/model.go` | Command handlers |

## Context System

Shortcuts are context-aware. Each binding specifies which context it applies to:

| Context | When Active |
|---------|-------------|
| `ContextGlobal` | Always (unless overridden) |
| `ContextMain` | Main list view, no modal open |
| `ContextModal` | Issue detail modal open |
| `ContextStats` | Statistics modal open |
| `ContextSearch` | Search input focused |
| `ContextConfirm` | Confirmation dialog shown |
| `ContextEpicTasks` | Epic modal with task list focused |
| `ContextParentEpicFocused` | Parent epic row focused in modal |
| `ContextHandoffs` | Handoffs modal open |
| `ContextForm` | Form modal (create/edit) open |

## Implementing a New Shortcut

### Step 1: Add Command Constant

In `pkg/monitor/keymap/registry.go`:

```go
const (
    // ... existing commands ...

    // Clipboard
    CmdCopyToClipboard   Command = "copy-to-clipboard"
    CmdCopyIDToClipboard Command = "copy-id-to-clipboard"  // NEW
)
```

### Step 2: Add Key Bindings

In `pkg/monitor/keymap/bindings.go`, add bindings for each context:

```go
// In MAIN PANEL BINDINGS section:
{Key: "y", Command: CmdCopyToClipboard, Context: ContextMain, Description: "Copy to clipboard"},
{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextMain, Description: "Copy issue ID"},

// In MODAL BINDINGS section:
{Key: "y", Command: CmdCopyToClipboard, Context: ContextModal, Description: "Copy to clipboard"},
{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextModal, Description: "Copy issue ID"},
```

### Step 3: Add Handler Method

In `pkg/monitor/model.go`, add the handler method:

```go
// copyIssueIDToClipboard copies just the issue ID to clipboard.
func (m Model) copyIssueIDToClipboard() (tea.Model, tea.Cmd) {
    var issueID string

    // Try modal first, then fall back to list selection
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

### Step 4: Wire Up Command Handling

In `pkg/monitor/model.go`, add cases to the appropriate handlers:

```go
// In handleMainCommand() for list view:
case keymap.CmdCopyIDToClipboard:
    return m.copyIssueIDToClipboard()

// In handleModalCommand() for modal view:
case keymap.CmdCopyIDToClipboard:
    return m.copyIssueIDToClipboard()
```

### Step 5: Update Help Text

In `pkg/monitor/keymap/help.go`:

```go
// In CommandDescription():
case CmdCopyIDToClipboard:
    return "Copy issue ID to clipboard"

// In AllCommands() list:
CmdCopyIDToClipboard,
```

In `pkg/monitor/keymap/export.go`:

```go
// In CommandMeta map:
CmdCopyIDToClipboard: {"Copy ID", "Copy issue ID to clipboard", 3},
```

## Accessing Selected Issues

### From List View (Main Context)

Use cursor position to find the selected issue:

```go
// Get issue ID from any panel
issueID := m.SelectedIssueID(m.ActivePanel)

// Get full issue object (if available)
issue := m.getSelectedIssueForPanel(m.ActivePanel)
```

Panel-specific access:
- `PanelCurrentWork`: `m.FocusedIssue` or `m.InProgress[]` (matched by `m.CurrentWorkRows`)
- `PanelTaskList`: `m.TaskListRows[cursor].Issue`
- `PanelActivity`: Only has `IssueID`, not full issue

### From Modal View

```go
modal := m.CurrentModal()
if modal != nil && modal.Issue != nil {
    issue := modal.Issue
    // ...
}
```

## Status Messages

Use temporary status messages for user feedback:

```go
m.StatusMessage = "Copied to clipboard"
return m, m.clearStatusAfterDelay()
```

The `clearStatusAfterDelay()` returns a command that clears the status after 2 seconds.

## Clipboard Operations

Use the helpers in `pkg/monitor/clipboard.go`:

```go
// Copy text to clipboard
if err := copyToClipboard(text); err != nil {
    m.StatusMessage = "Copy failed: " + err.Error()
}

// Format issue as markdown
markdown := formatIssueAsMarkdown(issue)

// Format epic with children
markdown := formatEpicAsMarkdown(epic, children)
```

## Testing Shortcuts

In `pkg/monitor/model_test.go`:

```go
func TestCopyIssueIDToClipboard(t *testing.T) {
    m := newTestModel()
    // Setup model with issues...

    // Simulate key press
    updated, _ := m.handleKey(tea.KeyMsg{
        Type:  tea.KeyRunes,
        Runes: []rune{'Y'},
    })

    m2 := updated.(Model)
    if m2.StatusMessage != "Copied ID: td-abc123" {
        t.Errorf("expected status message, got %q", m2.StatusMessage)
    }
}
```

## Common Pitfalls

### Not Handling All Contexts

If a shortcut should work in multiple contexts, add bindings for each:

```go
// Wrong: Only works in modal
{Key: "y", Command: CmdCopy, Context: ContextModal, ...}

// Right: Works in both list and modal
{Key: "y", Command: CmdCopy, Context: ContextMain, ...},
{Key: "y", Command: CmdCopy, Context: ContextModal, ...},
```

### Forgetting to Clear Status

Always clear status messages:

```go
// Wrong: Status stays forever
m.StatusMessage = "Done"
return m, nil

// Right: Status clears after delay
m.StatusMessage = "Done"
return m, m.clearStatusAfterDelay()
```

### Not Checking for nil Issue

Always check if an issue exists before accessing it:

```go
// Wrong: May panic
issueID := m.TaskListRows[cursor].Issue.ID

// Right: Check bounds first
if cursor < len(m.TaskListRows) {
    issueID = m.TaskListRows[cursor].Issue.ID
}
```

---

## Exporting Shortcuts to Sidecar

The monitor TUI can export its keyboard shortcuts to external tools (like sidecar) for command palette integration and context-aware keybindings.

### Export Architecture

Key files:
| Location | Purpose |
|----------|---------|
| `pkg/monitor/keymap/export.go` | Export functions and metadata |
| `pkg/monitor/keymap/registry.go` | Command constants |
| `pkg/monitor/keymap/bindings.go` | Key-to-command mappings |

### Adding a Command to Sidecar Export

When creating new shortcuts, ensure they're visible to sidecar by following these steps:

#### Step 1: Add to commandMetadata

In `pkg/monitor/keymap/export.go`, add an entry to the `commandMetadata` map:

```go
var commandMetadata = map[Command]struct {
    Name        string
    Description string
    Priority    int
}{
    // Priority levels:
    // 1-3 = footer visible (shown in UI footer)
    // 4+  = palette only (command palette access)

    // High priority (P1) - always in footer
    CmdOpenDetails:   {"Details", "Open issue details", 1},

    // Medium priority (P2) - footer when space allows
    CmdToggleBoardView: {"View", "Toggle swimlanes/backlog view", 2},

    // Lower priority (P3) - palette only
    CmdToggleHelp: {"Help", "Toggle help overlay", 3},

    // Navigation (P4-5) - usually palette only
    CmdCursorDown: {"Down", "Move cursor down", 5},
}
```

#### Step 2: Add Context Mapping (if needed)

If your command uses a new context, add it to `contextToSidecar`:

```go
var contextToSidecar = map[Context]string{
    ContextMain:   "td-monitor",
    ContextModal:  "td-modal",
    ContextBoard:  "td-board",  // Board-specific context
    // Add new contexts here
}
```

### Priority Guidelines

| Priority | Usage | Visibility |
|----------|-------|------------|
| 1 | Core actions (open, approve, search) | Always in footer |
| 2 | Common operations (sort, filter, toggle) | Footer when space allows |
| 3 | Utility commands (help, copy, quit) | Palette only |
| 4-5 | Navigation, context-specific | Palette only |

### Using the Export Functions

The Registry provides two export functions:

```go
// Get all key bindings for sidecar
bindings := registry.ExportBindings()
// Returns: []ExportedBinding{Key, Command, Context}

// Get command metadata for command palette
commands := registry.ExportCommands()
// Returns: []ExportedCommand{ID, Name, Description, Context, Priority}
```

### Checklist for Sidecar Visibility

When adding a new shortcut:

1. **Add to bindings.go**: Define the key binding with context
2. **Add to registry.go**: Define the command constant
3. **Add to export.go commandMetadata**: Set name, description, priority
4. **Add context to contextToSidecar** (if using new context)
5. **Verify**: Run `td monitor` and check sidecar receives the binding
