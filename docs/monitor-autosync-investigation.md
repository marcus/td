# Fix: Monitor auto-sync doesn't push edits

## Repro

```bash
bash scripts/e2e-sync-test.sh --manual --auto-sync --seed ~/code/td/.todos/issues.db
```

In the tmux session (alice pane):
1. Source env, run `td monitor`
2. Edit an issue title (e.g., change to "BOOBIES")
3. Wait 15+ seconds
4. Exit monitor

In bob pane:
- `td show <id>` — title is NOT updated

Back in alice pane:
- Run `td show <id>` — this triggers `autoSyncOnStartup()` in PersistentPreRun, which pushes the edit

Back in bob pane:
- `td show <id>` — NOW the change appears

**Key observation**: `td show` (a read-only command) triggers sync via `PersistentPreRun` → `autoSyncOnStartup()`, proving auth/config/sync-state all work. The bug is that the monitor's periodic sync path either doesn't fire or fails silently, and exiting the monitor doesn't trigger a push either (since "monitor" is not in `mutatingCommands`).

## Problem

When Alice edits an issue inside `td monitor`, the change is never pushed to the server until she exits the monitor and runs another command (e.g., `td show`). The monitor's periodic auto-sync (every 10s via TickMsg) appears to either not fire or fail silently.

The `td show` command triggers a push via `autoSyncOnStartup()` in `PersistentPreRun`, proving that config, auth, sync state, and the push mechanism all work. The issue is specific to the monitor's periodic sync path.

## Root Cause Analysis

Three possible failure points, in order of likelihood:

1. **`autoSyncOnce()` fails silently** -- Many early-return paths have zero logging (CAS blocked, sync state nil, getBaseDir empty). If any condition fails, there's no trace.

2. **Monitor's `AutoSyncFunc` is nil** -- If `AutoSyncEnabled()`, `IsAuthenticated()`, or `GetSyncState()` returned false/nil at monitor startup (`cmd/monitor.go:64-69`), `AutoSyncFunc` was never set, and periodic sync never fires.

3. **CAS flag stuck** -- If the startup sync goroutine or a previous periodic sync goroutine hangs (e.g., HTTP timeout while holding the flag), `autoSyncInFlight` stays at 1 and all subsequent syncs silently skip via the CAS guard.

## Plan

### Step 1: Add diagnostic logging to `autoSyncOnce()`

**File: `cmd/autosync.go`**

Add `slog.Debug` at every silent return in `autoSyncOnce()`:
- CAS failure: `slog.Debug("autosync: skipped, in flight")`
- `!AutoSyncEnabled()`: `slog.Debug("autosync: disabled")`
- `!IsAuthenticated()`: `slog.Debug("autosync: not authenticated")`
- `dir == ""`: `slog.Debug("autosync: no base dir")`
- `syncState == nil`: `slog.Debug("autosync: no sync state")`
- `syncState.SyncDisabled`: `slog.Debug("autosync: sync disabled")`
- Success entry: `slog.Debug("autosync: starting push+pull")`

### Step 2: Add diagnostic logging to monitor's periodic tick

**File: `pkg/monitor/model.go`** (TickMsg handler around line 500)

Add a log when the periodic sync condition is checked and when it fires:
- When AutoSyncFunc is nil: `slog.Debug("monitor: autosync not configured")`
- When sync fires: `slog.Debug("monitor: triggering periodic sync")`

### Step 3: Add monitor exit sync

**File: `cmd/autosync.go`** -- Add `"monitor"` to `mutatingCommands` map.

This ensures `PersistentPostRun` triggers `autoSyncAfterMutation()` when the monitor exits, pushing any edits that weren't caught by periodic sync.

### Step 4: Run the e2e test and check logs

```bash
bash scripts/e2e-sync-test.sh --manual --auto-sync --seed ~/code/td/.todos/issues.db
```

In the tmux session:
1. Source env, open `td monitor`, edit a title, wait 15s, exit
2. Check `tail -f $WORKDIR/alice.log` for the diagnostic messages
3. This will reveal which return path is hit

### Step 5: Fix the root cause (based on logs)

If logs show a specific failure, fix it. Most likely fixes:
- If CAS is stuck: add a timeout/staleness check on the CAS flag
- If sync state is nil: investigate DB connection in autoSyncOnce
- If not authenticated: check HOME propagation to goroutine context

### Step 6: (Optional) Add post-edit immediate sync in monitor

**File: `pkg/monitor/form_operations.go`** and `pkg/monitor/actions.go`

After each mutation (submitForm, executeCloseWithReason, executeDelete, reopenIssue, etc.), trigger an immediate sync:
```go
if m.AutoSyncFunc != nil {
    return m, tea.Batch(m.fetchData(), func() tea.Msg {
        m.AutoSyncFunc()
        return nil
    })
}
```

This gives near-instant sync after monitor edits instead of waiting for the 10s interval.

## Files to modify

- `cmd/autosync.go` -- diagnostic logging + add "monitor" to mutatingCommands
- `pkg/monitor/model.go` -- diagnostic logging in TickMsg handler
- `pkg/monitor/form_operations.go` -- (optional) post-edit sync trigger
- `pkg/monitor/actions.go` -- (optional) post-edit sync trigger

## Verification

1. Run e2e test: `bash scripts/e2e-sync-test.sh --manual --auto-sync --seed ~/code/td/.todos/issues.db`
2. In alice's tmux pane: `td monitor`, edit a title, wait 15s, exit
3. Check `$WORKDIR/alice.log` for diagnostic messages
4. In bob's pane: `td show <id>` -- should see the change
5. Run full test suite: `go test ./...`
