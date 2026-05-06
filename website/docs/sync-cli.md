---
sidebar_position: 11
---

# Sync CLI

td sync connects a local `.todos` database to a remote sync project. This page covers the user-facing CLI flow. For server endpoints and integration details, see the [HTTP API docs](./http-api/overview.md).

## Enable The Sync Commands

Some builds hide sync commands until the rollout flag is enabled:

```bash
td feature set sync_cli true
```

Then confirm the commands are available:

```bash
td auth status
td sync --help
```

## Configure The Server

Set the server URL explicitly instead of relying on private defaults:

```bash
td config set sync.url http://localhost:8080
td config get sync.url
```

For one command, use an environment override:

```bash
TD_SYNC_URL=http://localhost:8080 td auth status
```

## Authenticate

```bash
td auth login
td auth status
td auth logout
```

`td auth login` starts a device login flow. It prompts for an email address, prints a verification URL and code, then stores credentials in `~/.config/td/auth.json` after the server completes the login.

For automation, an API key can be supplied with `TD_AUTH_KEY`.

## Link A Project

Create a remote project and link the current local project:

```bash
td sync-project create "Website rebuild"
```

Or link to an existing project:

```bash
td sync-project list
td sync-project link prj_abc123
td sync-project join "Website rebuild"
```

Useful project commands:

| Command | Purpose |
|---------|---------|
| `td sync-project create <name>` | Create a remote project and try to link the current local database. |
| `td sync-project link <project-id>` | Link the current local database to an existing remote project. |
| `td sync-project unlink` | Remove the local link. |
| `td sync-project list` | List remote projects visible to the authenticated user. |
| `td sync-project members` | List members of the linked project. |
| `td sync-project invite <email> [role]` | Invite a user as `owner`, `writer`, or `reader`. |
| `td sync-project role <user-id> <role>` | Change a member role. |
| `td sync-project kick <user-id>` | Remove a member. |

## Guided Setup

`td sync init` walks through server URL, authentication, and project linking:

```bash
td sync init
```

Run `td auth login` first if the setup reports that you are not authenticated.

## Push, Pull, And Status

```bash
td sync
td sync --status
td sync --push
td sync --pull
```

`td sync` pushes local action-log events and pulls remote events. `--status` prints the linked project ID, local push/pull positions, pending local events, and server event state.

## Activity And Conflicts

```bash
td sync tail
td sync tail -f
td sync tail -n 50
td sync conflicts
td sync conflicts --since 24h --limit 50
```

Use `td sync tail` when checking whether sync is moving. Use `td sync conflicts` when a sync run reports conflicting changes.

## Diagnostics

```bash
td doctor
```

`td doctor` runs read-only checks for sync setup. It is the first command to run when authentication, project linking, or sync state does not look right.

## Minimal Setup Checklist

```bash
td feature set sync_cli true
td config set sync.url http://localhost:8080
td auth login
td sync-project create "My project"
td sync --status
td sync
```

Keep sync settings explicit in shared setup docs so agents and operators know which server a project is meant to use.
