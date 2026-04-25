---
sidebar_position: 9
---

# Sync and Collaboration

td sync lets multiple devices or collaborators share a project through a remote sync server. The local SQLite database remains the working copy; `td sync` pushes local changes and pulls remote changes for the linked sync project.

Sync commands are behind the `sync_cli` feature flag. If the commands are not visible in `td --help`, start td with the process-level feature override:

```bash
export TD_FEATURE_SYNC_CLI=true
td sync --status
```

## Authentication

```bash
td auth login
td auth status
td auth logout
```

`td auth login` prompts for an email address, prints a verification URL and code, polls the server until the device flow completes, and stores credentials locally. `td auth status` shows the email, server URL, and API key prefix. `td auth logout` clears local credentials.

## Project Setup

For a new shared project:

```bash
td init
td auth login
td sync-project create "Backend API"
td sync --status
td sync
```

`td sync-project create` creates the remote project and attempts to link the local project automatically. If you already have a remote project ID, link it directly:

```bash
td sync-project link <project-id>
```

Use `--force` when relinking a project that already has synced events and you intentionally want to reset local sync state for the new remote project.

## Joining an Existing Project

After a collaborator has access:

```bash
td init
td auth login
td sync-project join
td sync
```

`td sync-project join` lists available projects interactively when no argument is provided. It also accepts an exact project name or project ID:

```bash
td sync-project join "Backend API"
td sync-project join <project-id>
```

## Collaboration Commands

```bash
td sync-project list
td sync-project members
td sync-project invite teammate@example.com
td sync-project invite teammate@example.com reader
td sync-project role <user-id> writer
td sync-project kick <user-id>
td sync-project unlink
```

Valid member roles are `owner`, `writer`, and `reader`. Invitations default to `writer` when the role is omitted.

`td sync-project members`, `invite`, `role`, and `kick` operate on the currently linked local project.

## Syncing

```bash
td sync
td sync --push
td sync --pull
td sync --status
```

The default `td sync` does both directions: push local pending events, then pull remote events. Use push-only or pull-only modes for troubleshooting or controlled handoffs.

`td sync --status` shows local cursors, pending event count, and server event position for the linked project.

## Troubleshooting Sync

```bash
td doctor
td sync conflicts
td sync conflicts --since 24h --limit 50
td sync tail
td sync tail -f
td sync tail -n 50
```

`td doctor` runs sync setup diagnostics. `td sync conflicts` shows recent conflict records. `td sync tail` shows sync history, and `-f` follows new sync activity.

## Unlinking

```bash
td sync-project unlink
td sync-project unlink --force
```

Unlinking removes the local project association. When there are synced events, td can clear local sync state so the events can be pushed to a different project later; `--force` skips prompts.
