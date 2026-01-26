# CLI Commands Implementation Guide

How to implement new CLI commands in `td`. For monitor TUI shortcuts, see `monitor-shortcuts-guide.md`.

## Quick checklist
- GroupID assigned (core, workflow, query, shortcuts, session, files, system)
- Aliases added for common short forms
- `--json` flag for list/show commands
- Empty state message when no results
- Undo logging for mutations
- `defer database.Close()` always

## Architecture

Commands use [Cobra](https://github.com/spf13/cobra). Key files:

| File | Purpose |
|------|---------|
| `cmd/root.go` | Root command, groups, help template |
| `cmd/*.go` | Individual commands |
| `main.go` | Entry point |

## Command Groups

| GroupID | Purpose | Examples |
|---------|---------|----------|
| `core` | CRUD operations | create, list, show, update, delete |
| `workflow` | Issue lifecycle | start, review, approve, handoff |
| `query` | Data analysis | tree, dependencies, critical-path |
| `shortcuts` | Filtered lists | ready, next, blocked, in-review |
| `session` | Session management | status, usage, focus, ws |
| `files` | File linking | link, unlink, files |
| `system` | Tooling | version, info, export, import |

## Basic Command Structure

```go
var myCmd = &cobra.Command{
    Use:     "mycommand [args]",
    Aliases: []string{"mc"},
    Short:   "One-line description",
    Long:    `Detailed description.`,
    GroupID: "shortcuts",
    Args:    cobra.MinimumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        baseDir := getBaseDir()
        database, err := db.Open(baseDir)
        if err != nil {
            output.Error("%v", err)
            return err
        }
        defer database.Close()

        // Command logic...
        return nil
    },
}

func init() {
    rootCmd.AddCommand(myCmd)
    myCmd.Flags().Bool("json", false, "JSON output")
}
```

## Shortcuts (Filtered Lists)

Shortcuts use a shared helper:

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

### Session-Aware Shortcuts

```go
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
    // ...
}
```

## Flag Patterns

### Standard Flags

```go
cmd.Flags().Bool("json", false, "JSON output")
cmd.Flags().BoolP("verbose", "v", false, "Verbose")
cmd.Flags().StringP("filter", "f", "", "Filter")
cmd.Flags().StringArray("status", nil, "Status (repeatable)")
cmd.Flags().IntP("limit", "n", 50, "Limit")
```

### Common Flags

| Flag | Purpose | Commands |
|------|---------|----------|
| `--json` | JSON output | list/show |
| `--quiet/-q` | Suppress output | mutations |
| `--force` | Override checks | start, delete |
| `--reason` | Justification | start, block |
| `--limit/-n` | Result limit | list |
| `--sort` | Sort field | list |

### Flag Aliases

```go
createCmd.Flags().StringP("labels", "l", "", "Labels")
createCmd.Flags().String("label", "", "Alias for --labels")
createCmd.Flags().String("tags", "", "Alias for --labels")

// Resolve in priority order
labelsStr, _ := cmd.Flags().GetString("labels")
if labelsStr == "" {
    if s, _ := cmd.Flags().GetString("label"); s != "" {
        labelsStr = s
    }
}
```

## Subcommand Pattern

```go
var wsCmd = &cobra.Command{
    Use:     "ws",
    Aliases: []string{"worksession"},
    Short:   "Work session commands",
    GroupID: "session",
}

var wsStartCmd = &cobra.Command{
    Use:   "start [name]",
    Short: "Start a work session",
    Args:  cobra.ExactArgs(1),
    RunE:  func(cmd *cobra.Command, args []string) error { ... },
}

func init() {
    rootCmd.AddCommand(wsCmd)
    wsCmd.AddCommand(wsStartCmd)
}
```

## Output Patterns

```go
// Errors
output.Error("failed: %v", err)

// Warnings
output.Warning("issue blocked: %s", id)

// Standard output
fmt.Println(output.FormatIssueShort(&issue))

// JSON
if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
    return output.JSON(result)
}

// Action confirmation (uppercase)
fmt.Printf("CREATED %s\n", issue.ID)
```

## Undo Support

```go
// Capture before mutation
prevData, _ := json.Marshal(issue)

// Mutate
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

## Validation

```go
// Model validation
if t != "" {
    typ := models.Type(t)
    if !models.IsValidType(typ) {
        output.Error("invalid type: %s", t)
        return fmt.Errorf("invalid type: %s", t)
    }
}

// Argument validation
Args: cobra.ExactArgs(1)
Args: cobra.MinimumNArgs(1)
Args: cobra.RangeArgs(1, 3)
```

## Common Mistakes

| Symptom | Fix |
|---------|-----|
| No output for empty results | Add `if len(results) == 0 { fmt.Println("No results") }` |
| Database connection leak | Add `defer database.Close()` |
| Duplicate DB opens in session-aware code | Open DB once, pass to session |
| Missing JSON support | Add `--json` flag and `output.JSON()` |

## Testing

```go
func TestMyCommand(t *testing.T) {
    tempDir := t.TempDir()
    baseDirOverride = &tempDir
    defer func() { baseDirOverride = nil }()

    database, _ := db.Open(tempDir)
    defer database.Close()
    // Test...
}
```

## Workflow Hints

For common mistakes, add hints in `root.go`:

```go
func handleWorkflowHint(cmd string) bool {
    switch cmd {
    case "done", "complete":
        showWorkflowHint(cmd, "review", "Use 'td close' for admin closures.")
        return true
    }
    return false
}
```
