---
sidebar_position: 9
---

# Sync & Collaboration

Use sync when you want one local `td` project to stay aligned across machines or teammates. This guide covers command enablement, authentication, project linking, routine sync commands, background sync, and common failure modes.

## Enable Sync Commands

The sync surface is feature-gated at command registration time. If `td sync`, `td auth`, `td config`, `td doctor`, or `td sync-project` shows as an unknown command, enable `sync_cli` for the current process before starting `td`.

For an interactive shell session:

```bash
export TD_ENABLE_FEATURE=sync_cli
```

For a single command:

```bash
TD_FEATURE_SYNC_CLI=true td sync --help
```

Add the export to your shell profile if you use sync regularly.

> **Important**: `td feature set sync_cli true` writes project config, and `td feature list` may report `sync_cli=on (source=config)`, but that still does not expose the hidden commands. The CLI registers these commands before project config is loaded, so you still need the environment override shown above.

## Quick Setup

If you want the shortest path from a fresh machine:

```bash
export TD_ENABLE_FEATURE=sync_cli
td config set sync.url https://sync.example.com   # optional if you are not using localhost:8080
td auth login
td sync init
td sync
```

`td sync init` is a guided setup for server confirmation and project linking. It does **not** perform login for you. If you run it before `td auth login`, it stops and asks you to authenticate first.

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

`td auth login` starts the device login flow. `td auth status` is the fastest way to confirm that the current machine still has a saved credential.

### 3. Create Or Join A Remote Project

Project owners usually create a remote project:

```bash
td sync-project create "team-project" --description "Shared task tracking"
```

`create` also links the current local project to the new remote project.

Teammates usually join an existing project:

```bash
td sync-project join
td sync-project join "team-project"
td sync-project join "<project-id>"
```

Use `join` when possible. With no argument it opens a numbered picker. With one argument it matches by name first, then by project ID.

Use `link` when you already know the exact remote project ID or you want a more scriptable flow:

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

`td sync` pushes local events first, then pulls remote events. `td sync --status` shows both the local sync checkpoint and the current server event counts.

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
- `reader`: read-only project access

## Background Sync

Background sync behavior uses a runtime feature flag plus global config values:

```bash
td feature set sync_autosync true
td config set sync.auto.enabled true
td config set sync.auto.debounce 5s
td config set sync.auto.interval 5m
td config set sync.auto.pull true
td config set sync.auto.on_start true
```

`sync_autosync` is resolved at runtime from project config, so `td feature set sync_autosync true` does take effect. The `sync.auto.*` values live in global config and control whether background hooks run, whether they pull after pushing, and how often they fire.

Other supported sync config keys:

- `sync.url`
- `sync.enabled`
- `sync.snapshot_threshold`

Use `td config get <key>` to inspect one value, or `td config list` for the full config document.

## Notes In Shared Projects

Notes sync by default. To keep notes local in a shared repo, disable note syncing in that project:

```bash
td feature set sync_notes false
```

Unlike `sync_cli`, `sync_notes` is checked at runtime from project config, so this feature flag works without an environment override.

## Diagnostics And Activity

Use these commands when setup looks right but behavior does not:

```bash
td doctor
td sync conflicts
td sync conflicts --since 24h
td sync tail
td sync tail -f
```

`td doctor` checks auth config, server reachability, auth validity, local database access, project linkage, and pending local events. `td sync conflicts` shows recent overwrite events. `td sync tail` is useful when you want to watch push and pull activity live.

## Troubleshooting

**`unknown command "sync"` or `unknown command "auth"`**

Enable `sync_cli` with `export TD_ENABLE_FEATURE=sync_cli` or prefix the command with `TD_FEATURE_SYNC_CLI=true`. Project config alone does not expose hidden commands. See [Enable Sync Commands](#enable-sync-commands).

**`td feature list` says `sync_cli` is on, but commands are still hidden**

That means the feature is enabled in project config, not for the current process. Add the environment override and rerun the command.

**`not logged in (run: td auth login)`**

Your local auth config is missing or expired. Run `td auth login` again, then confirm with `td auth status`.

**`td sync init` says you are not authenticated**

That is expected. `td sync init` does not run the login flow for you. Run `td auth login` first, then re-run `td sync init`.

**`project not linked`**

Your local project has not been connected to a remote project yet. Run `td sync-project join` or `td sync-project link <project-id>`.

**`no projects found` during `td sync-project join`**

Your account does not currently have any project invites. Ask a project owner to run `td sync-project invite <your-email>`.

**`unauthorized` from `td sync --status` or `td doctor`**

Your API key is no longer valid. Re-run `td auth login`.

**404s after using `td sync-project link <project-id>`**

The local project may be linked to a stale or mistyped ID. Run `td sync-project list` to verify the remote project exists, then use `td sync-project join` when possible.

**You are not sure whether the server is healthy**

Run `td doctor` and `td sync --status`. They give better signal than a plain `td sync` run with no pending local events.
