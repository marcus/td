---
sidebar_position: 10
---

# Sync And Collaboration

Sync connects a local `.todos` database to a remote td project so teammates can share issues, handoffs, comments, notes, and workflow history.

Most collaborators follow this path:

```bash
td auth login
td sync-project join
td sync --status
td sync
```

Project owners usually create or link the shared project first:

```bash
td auth login
td sync-project create "Product CLI"
td sync-project link <project-id>
td sync-project invite teammate@example.com editor
td sync
```

## Authentication

| Command | Use |
|---------|-----|
| `td auth login` | Authenticate this machine with the sync server |
| `td auth status` | Confirm whether credentials are available |
| `td auth logout` | Remove local sync credentials |

Run `td auth status` before debugging project membership. A valid login is required before project and sync commands can call the server.

## Project Setup

| Command | Use |
|---------|-----|
| `td sync init` | Guided setup for authentication and project linking |
| `td sync-project create <name>` | Create a remote project |
| `td sync-project link <project-id>` | Attach the current local project to a remote project |
| `td sync-project join [name-or-id]` | Join and link to a remote project |
| `td sync-project list` | Show remote projects available to you |
| `td sync-project unlink` | Disconnect the local project from remote sync |

Use `--force` with `link` or `unlink` only when you intentionally want to skip confirmation prompts.

## Member Management

| Command | Use |
|---------|-----|
| `td sync-project members` | List members for the linked project |
| `td sync-project invite <email> [role]` | Invite a collaborator |
| `td sync-project kick <user-id>` | Remove a collaborator |
| `td sync-project role <user-id> <role>` | Change a collaborator role |

Roles are project policy. Use the role names configured by your td server.

## Sync Operations

| Command | Use |
|---------|-----|
| `td sync` | Push local changes and pull remote changes |
| `td sync --push` | Push local changes only |
| `td sync --pull` | Pull remote changes only |
| `td sync --status` | Show sync state without changing data |
| `td sync tail` | Show recent sync activity |
| `td sync tail -f` | Follow sync activity in real time |
| `td sync conflicts` | Show recent sync conflicts |

When a new collaborator joins, they should run `td sync --status` first to confirm the link, then `td sync` to pull current project state.

## Diagnostics

Run diagnostics when setup or sync behavior looks wrong:

```bash
td auth status
td sync --status
td doctor
td sync conflicts --since 24h
td sync tail -n 50
```

`td doctor` focuses on sync setup checks. `td sync conflicts` and `td sync tail` help explain what happened after setup succeeded.
