---
sidebar_position: 9
---

# Sync & Collaboration

Use td sync when you want one local project database to stay in sync across machines or teammates. This guide covers the feature gate, auth, project linking, routine sync commands, and common failure modes.

## Enable Sync Commands

The sync surface is feature-gated. If `td sync` or `td auth` shows as an unknown command, enable `sync_cli` first.

Recommended for immediate use in a shell:

```bash
export TD_ENABLE_FEATURE=sync_cli
```

Project-level alternative:

```bash
td feature set sync_cli true
```

`td feature set` writes the flag to project config, but command registration happens when the process starts, so restart your shell after setting it.

## Quick Setup

If you want the shortest path, run:

```bash
td sync init
```

The guided flow walks through server selection, auth, and linking to a remote project.

## Manual Setup

### 1. Configure The Server

The default server is `http://localhost:8080`. Set a different URL when you are using a hosted or self-hosted sync server:

```bash
td config set sync.url https://sync.example.com
```

Useful config helpers:

```bash
td config get sync.url
td config list
```

### 2. Authenticate

```bash
td auth login
td auth status
```

`td auth login` starts the device auth flow. `td auth status` is the fastest way to confirm whether the current machine still has a valid credential.

### 3. Create Or Join A Remote Project

Project owners usually create a remote project:

```bash
td sync-project create "team-project" --description "Shared task tracking"
```

Teammates usually join an existing project:

```bash
td sync-project join
td sync-project join "team-project"
```

Use `join` when possible. It validates what you pick from the server. `link` is better for scripts and known IDs:

```bash
td sync-project link <project-id>
td sync-project unlink
td sync-project list
```

### 4. Sync

```bash
td sync
td sync --push
td sync --pull
td sync --status
```

`td sync` pushes local events first, then pulls remote events. `td sync --status` shows both the local sync checkpoint and current server event counts.

## Collaboration Commands

Once a project is linked, owners can invite and manage teammates:

```bash
td sync-project invite alice@example.com
td sync-project invite bob@example.com reader

td sync-project members
td sync-project role <user-id> writer
td sync-project kick <user-id>
```

Roles:

- `owner`: full access, including invites and role changes
- `writer`: read/write project access
- `reader`: read-only access

## Diagnostics And Activity

Use these commands when setup looks right but behavior does not:

```bash
td doctor
td sync conflicts
td sync conflicts --since 24h
td sync tail
td sync tail -f
```

`td doctor` checks auth, server reachability, and local project linkage. `td sync conflicts` shows recent overwrite events. `td sync tail` is useful when you want to watch push and pull activity live.

## Autosync Configuration

You can keep sync manual, or enable background sync behavior:

```bash
td config set sync.auto.enabled true
td config set sync.auto.debounce 5s
td config set sync.auto.interval 5m
td config set sync.auto.pull true
td config set sync.auto.on_start true
```

Other supported sync config keys:

- `sync.enabled`
- `sync.snapshot_threshold`

Use `td config get <key>` to inspect one value, or `td config list` for the full config document.

## Notes In Shared Projects

Notes sync with the rest of the project by default. If you want notes to stay local in a shared repo, disable note syncing in that project:

```bash
td feature set sync_notes false
```

## Troubleshooting

**`unknown command "sync"` or `unknown command "auth"`**

Enable `sync_cli` first. See [Enable Sync Commands](#enable-sync-commands).

**`not logged in (run: td auth login)`**

Your local auth config is missing or expired. Run `td auth login` again, then confirm with `td auth status`.

**`project not linked`**

Your local project has not been connected to a remote project yet. Run `td sync-project join` or `td sync-project link <project-id>`.

**`no projects found` during `td sync-project join`**

Your account does not currently have any project invites. Ask a project owner to run `td sync-project invite <your-email>`.

**`unauthorized` from `td sync --status` or `td doctor`**

Your API key is no longer valid. Re-run `td auth login`.

**404s after using `td sync-project link <project-id>`**

The local project may be linked to a stale or mistyped ID. Run `td sync-project list` to verify the remote project exists, then use `td sync-project join` when possible.

**You are not sure whether the server is healthy**

Run `td doctor` and `td sync --status`. They check more than a plain `td sync` run with no pending local events.
