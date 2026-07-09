---
sidebar_position: 17
---

# Troubleshooting

Start with the command that matches the failure mode:

```bash
td doctor
td errors
td stats errors
td security
td last
```

## Sync Diagnostics

Run the sync doctor:

```bash
td doctor
```

It checks authentication config, server reachability, API key validity, local database access, project linking, and pending events.

If sync commands are unavailable, enable the gated sync CLI first:

```bash
export TD_ENABLE_FEATURE=sync_cli
td doctor
```

For database integrity checks, `td doctor fk` reports orphaned foreign-key relations without modifying data:

```bash
td doctor fk
```

## Command Error Logs

td records failed command attempts so agent runs can be debugged later.

```bash
td errors
td errors --since 24h
td errors --session ses_abc123
td errors --limit 50
td errors --json
td errors --count
td errors --clear
```

`td stats errors` is an alias-style stats view for the same class of failures.

## Security Exceptions

Review workflow exceptions are written to the security log.

```bash
td security
td security --json
td security --clear
td stats security
```

Use this when a task was closed through a creator exception, an administrative close, or another review-policy exception that should be audited.

## Undo and Recovery

Show recent actions in the current session:

```bash
td undo --list
td last
td last -n 5
```

Undo the most recent reversible action:

```bash
td undo
```

Undo supports common issue, dependency, file-link, board, handoff, and review-state actions. It is session-scoped, so it only sees actions from the current td session.

## Common Sync Failures

### `unknown command "sync"` or `unknown command "auth"`

Enable the sync CLI:

```bash
export TD_ENABLE_FEATURE=sync_cli
```

Add it to your shell profile if you use sync regularly.

### `not logged in`

Credentials are missing or expired:

```bash
td auth login
td auth status
```

For CI, set `TD_AUTH_KEY`.

### `project not linked`

The local database is not connected to a remote project:

```bash
td sync-project join
td sync --status
```

Use `td sync-project list` to confirm you have access to the project.

### `unauthorized`

Your API key is invalid, expired, or revoked:

```bash
td auth logout
td auth login
td sync --status
```

### 404 or missing remote project

The local project may be linked to an old or mistyped project ID:

```bash
td sync-project list
td sync-project join
```

Prefer `join` over raw `link` for interactive setup because it validates the project against the server.

### Local changes are not appearing remotely

Check pending events and push state:

```bash
td sync --status
td sync --push
td sync tail
```

`Nothing to push` means the local action log has no unsynced events. It does not prove the server is healthy; use `td sync --status` or `td doctor` for that.

### Remote changes overwrite local edits

Review conflict records:

```bash
td sync conflicts
td sync conflicts --since 24h
```

td uses last-write-wins conflict handling. Reapply the desired local change after reviewing the conflict.

## Config Problems

Show active config:

```bash
td config list
td feature list
```

Check the most common overrides:

```bash
echo "$TD_WORK_DIR"
echo "$TD_ENABLE_FEATURE"
echo "$TD_DISABLE_FEATURE"
echo "$TD_SYNC_URL"
echo "$TD_AUTH_KEY"
```

If td is reading the wrong project, use a work directory or association:

```bash
TD_WORK_DIR=/path/to/project td list
td config associate /path/to/current/dir /path/to/project
td config associations
```

## Database and Snapshot Recovery

Sync snapshot bootstrap creates a backup before replacing the local database:

```bash
.todos/issues.db.pre-snapshot-backup
```

If a snapshot bootstrap produced an unexpected local state, stop running mutating td commands, copy the current `.todos/issues.db` somewhere safe, and restore the pre-snapshot backup manually.

For routine issue mistakes, use `td undo` before editing database files directly.
