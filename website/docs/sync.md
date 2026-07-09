---
sidebar_position: 12
---

# Sync and Collaboration

Sync shares a local td project with a remote sync server. It lets multiple people or agents push issues, logs, comments, handoffs, boards, dependencies, file links, work sessions, and notes to the same project.

Sessions stay local. Review history and issue data sync, but each machine keeps its own session identity.

:::info
Sync commands are gated by the `sync_cli` feature. If `td sync` or `td auth` is not available, enable the feature first:

```bash
export TD_ENABLE_FEATURE=sync_cli
```

You can also set it in project config with `td feature set sync_cli true`, but command registration happens when td starts, so environment variables are the most reliable way to expose gated commands.
:::

## Quick Setup

Run the guided setup:

```bash
td sync init
```

The wizard checks the server URL, verifies authentication, and helps create or join a remote project.

For manual setup:

```bash
td config set sync.url https://sync.example.com
td auth login
td sync-project create "backend-api"
td sync
```

Teammates usually join an existing project:

```bash
td auth login
td sync-project join
td sync
```

## Authentication

Log in with the device flow:

```bash
td auth login
```

td prompts for your email, prints a verification URL and code, then stores credentials in `~/.config/td/auth.json`.

Check or clear auth state:

```bash
td auth status
td auth logout
```

Use `TD_AUTH_KEY` for CI or short-lived automation:

```bash
TD_AUTH_KEY=td_live_xxx td sync --status
```

## Projects

Create and link a remote project in one step:

```bash
td sync-project create "launch-plan" --description "Shared release tracker"
```

List, join, link, or unlink projects:

```bash
td sync-project list
td sync-project join "launch-plan"
td sync-project link <project-id>
td sync-project unlink
```

Prefer `td sync-project join` for humans because it validates the project against the server. Use `td sync-project link <project-id>` for scripts that already have the exact project ID.

If you need to relink a database that already has synced events, use `--force` only after confirming you want to reset local sync state:

```bash
td sync-project link <project-id> --force
td sync-project unlink --force
```

## Push, Pull, and Status

Run a full bidirectional sync:

```bash
td sync
```

Push or pull only:

```bash
td sync --push
td sync --pull
```

Inspect local and server state:

```bash
td sync --status
td sync tail
td sync tail -f
```

On first pull, td may bootstrap from a server snapshot when the server has enough events. Before replacing the local database with a snapshot, td writes a `.todos/issues.db.pre-snapshot-backup` file.

## Auto-Sync

Auto-sync can push and pull on command startup, after mutating commands, and periodically in the monitor.

Auto-sync has two switches:

- `sync_autosync` is the feature flag that allows startup, post-mutation, and monitor hooks to run. It defaults to `false`.
- `sync.auto.enabled` is the user config switch for those hooks once the feature is enabled. It defaults to `true`.

Enable both when setting up automatic sync:

```bash
td feature set sync_autosync true
td config set sync.auto.enabled true
td config set sync.auto.on_start true
td config set sync.auto.debounce 3s
td config set sync.auto.interval 5m
td config set sync.auto.pull true
```

For one-off use, enable the feature through the environment before the command starts:

```bash
TD_ENABLE_FEATURE=sync_cli,sync_autosync td list
```

Environment variables override config:

| Variable | Purpose |
|----------|---------|
| `TD_FEATURE_SYNC_AUTOSYNC` | Enable or disable the auto-sync feature flag |
| `TD_ENABLE_FEATURE` | Enable gated features, such as `sync_cli,sync_autosync` |
| `TD_SYNC_AUTO` | Enable or disable auto-sync |
| `TD_SYNC_AUTO_START` | Enable or disable startup sync |
| `TD_SYNC_AUTO_DEBOUNCE` | Debounce post-mutation sync, such as `3s` |
| `TD_SYNC_AUTO_INTERVAL` | Periodic monitor sync interval, such as `5m` |
| `TD_SYNC_AUTO_PULL` | Include pull during auto-sync |

If `sync_autosync` is disabled, `sync.auto.*` values are still saved but no startup, post-mutation, or monitor auto-sync work runs.

Auto-sync is quiet by design. Use `td sync --status`, `td sync tail`, or `td doctor` when you need explicit feedback.

## Collaboration Roles

Project members can be owners, writers, or readers.

| Role | What it can do |
|------|----------------|
| `owner` | Read, write, invite members, remove members, and change roles |
| `writer` | Read and write project data |
| `reader` | Read project data without pushing local edits |

Manage members:

```bash
td sync-project members
td sync-project invite alice@example.com writer
td sync-project role <user-id> reader
td sync-project kick <user-id>
```

## Conflicts

Sync uses last-write-wins conflict handling. If remote changes overwrite local records during pull, td preserves conflict records for review.

```bash
td sync conflicts
td sync conflicts --limit 10
td sync conflicts --since 24h
```

Use the conflict list to identify what changed, then manually restore or reapply anything that should win.

## Common Recovery Commands

```bash
td doctor
td auth login
td sync-project join
td sync --status
td sync --pull
```

For broader diagnostics, see [Troubleshooting](./troubleshooting.md).
