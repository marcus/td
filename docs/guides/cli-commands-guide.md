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

## JSON output (`--json`)

`--json` is a **global (persistent) flag** registered on the root command, so it
is available on **every** command — reads *and* mutations. In JSON mode a command
suppresses its human output and emits a single machine-readable envelope on
stdout instead. This is the contract scripts should rely on.

### The contract

Every mutating command emits one of two success envelopes, plus a shared error
envelope:

- **Issue-affecting commands** (`create`/`add`, `update`, `start`, `unstart`,
  `block`, `unblock`, `reopen`, `close`, `approve`, `review`, `reject`) emit:

  ```json
  {"id": "...", "status": "...", "action": "...", "issue": { ...full issue... }}
  ```

  `issue` is the complete issue record (the `models.Issue` JSON shape).
  Command-specific extras are merged in alongside — e.g. `start`/`close` add
  `session`, `block` adds `reason`, and cascade operations add count fields.

- **Non-issue mutations** emit `{"action": "...", ...}` with command-specific
  payload and (where one applies) an `id`:

  ```json
  {"action": "logged", "id": "...", "log": { ...full log... }}
  {"action": "handoff_recorded", "id": "...", "handoff": { ...full handoff... }}
  ```

  (`note add`/`edit`/`delete` emit `note_created`/`note_updated`/`note_deleted`,
  `link --depends-on` emits `dependency_added` with `from`/`to`/`type`, `defer`
  emits `deferred`/`deferral_cleared`, etc.)

The success envelopes are produced by the shared helpers in `internal/output`:
`output.EmitIssue(action, issue, extra)` and `output.EmitResult(action, extra)`.
Use those rather than hand-rolling a map so the contract stays consistent.

### Real examples

```console
$ td add "Investigate retry flake" --json
{
  "action": "created",
  "id": "td-b221e9",
  "issue": {
    "id": "td-b221e9",
    "title": "Investigate retry flake",
    "status": "open",
    "type": "task",
    "priority": "P2",
    ...
  },
  "status": "open"
}

$ td start td-b221e9 --json
{
  "action": "started",
  "id": "td-b221e9",
  "issue": { "id": "td-b221e9", "status": "in_progress", ... },
  "session": "ses_c22b59",
  "status": "in_progress"
}

$ td log td-b221e9 "investigated the flake" --json
{
  "action": "logged",
  "id": "td-b221e9",
  "log": {
    "id": "lg-bfbc1318",
    "issue_id": "td-b221e9",
    "message": "investigated the flake",
    "type": "progress",
    ...
  }
}
```

### Error envelope

On failure a JSON-mode command emits the error envelope on **stdout** and
exits non-zero (exit code 1):

```console
$ td add "short" --json
{"error":{"code":"invalid_input","message":"title too short (5 chars, need 15) ..."}}
$ echo $?
1
```

Produced by `output.JSONError(code, message)`. It is encoded via the `json`
package (HTML escaping disabled), so messages containing quotes, backslashes, or
newlines still yield valid, parseable JSON on a single line. Error codes are the
`output.ErrCode*` constants (`not_found`, `invalid_input`, `conflict`,
`cannot_self_approve`, `handoff_required`, `database_error`, `git_error`,
`no_active_session`).

### Bulk operations (NDJSON)

Commands that accept multiple ids (e.g. `td start id1 id2`) emit **one JSON
object per id**, newline-delimited (NDJSON), with no trailing human summary.
Parse it line-by-line / with a streaming decoder, not as a single document.

### Exceptions (so scripters aren't surprised)

- **`query`** does *not* use `--json`. It selects its format with
  `--output table|json|ids|count` instead.
- The **JSONL commands** — `errors`, `security`, and the error/security views of
  `stats` — emit line-delimited JSON through their own format, not the
  `EmitIssue`/`EmitResult` envelopes.
- **`show`** additionally honors a legacy `--format json` in addition to `--json`.

### Scripting tip

```bash
# Capture the new id from a create in JSON mode
id=$(td add "Wire up payment retry" --json | jq -r .id)
td start "$id" --json | jq -r .status   # -> in_progress
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
| Missing JSON support | `--json` is already a persistent root flag; gate on `jsonMode(cmd)` and emit via `output.EmitIssue`/`output.EmitResult` (see [JSON output](#json-output---json)) |

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
