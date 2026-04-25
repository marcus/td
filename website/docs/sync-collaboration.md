---
sidebar_position: 10
---

# Sync and Collaboration

td is local-first. Sync adds a remote project so multiple machines or collaborators can share the same issue history.

The collaboration role names accepted by the CLI are:

| Role | Intended access |
|------|-----------------|
| `owner` | Manage project membership |
| `writer` | Pull and push project events |
| `reader` | Pull project events |

The default invite role is `writer`.

## First-Time Setup

A typical owner setup:

```bash
td auth login
td auth status
td sync-project create "api-redesign" --description "Shared td project"
td sync
td doctor
```

`td sync-project create` creates the remote project and attempts to link the current local td project automatically.

A typical collaborator setup:

```bash
td init
td auth login
td sync-project join api-redesign
td sync
td doctor
```

If the collaborator already knows the project ID, they can link directly:

```bash
td sync-project link <project-id>
td sync --pull
```

Use `td sync-project link <project-id> --force` only when replacing an existing link and intentionally resetting local sync state.

## Authentication

```bash
td auth login
td auth status
td auth logout
```

Authentication stores local sync credentials. Use `td auth status` before debugging sync failures so you know whether the CLI can see a login.

## Project Management

```bash
td sync-project create "project-name" --description "Optional description"
td sync-project list
td sync-project link <project-id>
td sync-project join [name-or-id]
td sync-project unlink
```

`sync-project` has the alias `sp`, so `td sp list` and `td sync-project list` are equivalent.

## Members

```bash
td sync-project members
td sync-project invite alex@example.com
td sync-project invite alex@example.com reader
td sync-project role <user-id> writer
td sync-project kick <user-id>
```

Only `owner`, `writer`, and `reader` are valid roles. Owners manage membership; writers are the normal role for collaborators who should push work; readers are useful for read-only observers.

## Sync Operations

```bash
td sync
td sync --push
td sync --pull
td sync --status
td sync conflicts
td sync tail
```

Run plain `td sync` for normal bidirectional operation. Use `--push` or `--pull` for troubleshooting or controlled migration steps.

`td sync conflicts` shows recent sync conflicts. `td sync tail` shows recent sync activity.

## Diagnostics

```bash
td doctor
td auth status
td sync --status
td sync-project members
```

`td doctor` checks the sync setup. It does not accept JSON or repair flags; use the other status commands alongside it when you need more detail.
