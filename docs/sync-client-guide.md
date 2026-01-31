# Sync Client Guide

How to configure, authenticate, and sync your local `td` project with a remote sync server.

## Overview

Sync replicates your local td issues, logs, comments, handoffs, and boards to a remote server. Other team members linked to the same project receive those changes on pull. Sync is manual -- you decide when to push and pull.

### What syncs

| Entity | Synced |
|---|---|
| Issues | Yes |
| Logs | Yes |
| Comments | Yes |
| Handoffs | Yes |
| Boards | Yes |
| Board issues | Yes |
| Board positions | Yes |
| Dependencies | Yes |
| File links | Yes |
| Work sessions | Yes |
| Sessions | No (local only) |

### Board positions, dependencies, and file links

These entities use deterministic IDs derived from their composite keys via SHA-256:

| Entity | Table | ID prefix | Derived from |
|---|---|---|---|
| Board position | `board_issue_positions` | `bip_` | `board_id \| issue_id` |
| Dependency | `issue_dependencies` | `dep_` | `issue_id \| depends_on_id \| relation_type` |
| File link | `issue_files` | `ifl_` | `issue_id \| file_path` |

Deterministic IDs ensure the same logical entity produces the same ID across clients, preventing duplicates during sync.

**File path handling:** File paths in `issue_files` are stored repo-relative with forward slashes (e.g., `src/main.go` rather than an absolute path). Files outside the repository root are skipped during sync. Path separators are normalized to forward slashes before ID computation, so the same file produces the same ID on any OS.

**Entity type normalization:** The action_log may record these entities with short names (`board_position`, `dependency`, `file_link`). The sync engine normalizes them to their canonical table names (`board_issue_positions`, `issue_dependencies`, `issue_files`) before pushing. Unrecognized entity types are skipped with a warning.

### What's automatic vs. manual

| Action | Automatic? |
|---|---|
| Recording local changes to the action log | Automatic (every create/update/delete is logged) |
| Pushing changes to server | Manual (`td sync` or `td sync --push`) |
| Pulling changes from server | Manual (`td sync` or `td sync --pull`) |
| Conflict detection | Automatic during pull |
| Conflict resolution | Automatic (last-write-wins; both versions preserved) |
| Authentication token refresh | Not automatic (re-login when expired) |

## Setup

### 1. Configure the server URL

By default, `td` connects to `http://localhost:8080`. To point at a different server:

**Option A: Environment variable** (per-session or in shell profile)

```bash
export TD_SYNC_URL=https://sync.example.com
```

**Option B: Config file** (`~/.config/td/config.json`)

```json
{
  "sync": {
    "url": "https://sync.example.com",
    "enabled": true
  }
}
```

Priority: `TD_SYNC_URL` env > `config.json` > `http://localhost:8080`

### 2. Authenticate

```bash
td auth login
```

This starts the device auth flow:

1. You enter your email
2. The CLI requests a device code from the server
3. A verification URL and 6-character code are displayed
4. Open the URL in a browser and enter the code
5. The CLI polls until verification completes
6. An API key is saved to `~/.config/td/auth.json` (file permissions: 0600)

If the server has `SYNC_ALLOW_SIGNUP=true`, new users are created automatically on first login.

**Check auth status:**

```bash
td auth status
# Email:  you@example.com
# Server: https://sync.example.com
# Key:    abc123def456...
```

**Log out:**

```bash
td auth logout
```

This deletes the local credentials. The API key remains valid on the server until it expires (1 year).

**Override API key via environment:**

```bash
export TD_AUTH_KEY=your-api-key-here
```

### 3. Create or join a project

**Create a new remote project:**

```bash
td sync-project create "my-team-project"
# ✓ Created project my-team-project (a1b2c3d4-...)

td sync-project create "my-project" --description "Sprint tracker"
```

**List your projects:**

```bash
td sync-project list
# ID                                    NAME                  CREATED
# a1b2c3d4-e5f6-...                     my-team-project       2026-01-31T10:00:00Z
```

### 4. Link your local project

```bash
td sync-project link <project-id>
# ✓ Linked to project a1b2c3d4-...
```

This writes the project ID to your local database's `sync_state` table. Each local td project can be linked to one remote project.

**Unlink:**

```bash
td sync-project unlink
```

## Syncing

### Full sync (push then pull)

```bash
td sync
```

This pushes local changes first, then pulls remote changes. Default behavior when no flags are passed.

### Push only

```bash
td sync --push
```

Sends all unsynced local actions to the server. Events are read from the local `action_log` table where `synced_at IS NULL`. On success, each event is marked with its `server_seq` and `synced_at` timestamp.

### Pull only

```bash
td sync --pull
```

Fetches remote events since your last pull. Events are fetched in batches of 1000. Your own events (matching your device ID) are excluded via the `exclude_client` parameter to avoid applying your own changes back.

Each batch is applied in a transaction:
1. Remote events are written to local entity tables (INSERT OR REPLACE)
2. Conflicts are recorded if a local row was overwritten
3. `last_pulled_server_seq` is updated
4. Transaction commits

### Check status

```bash
td sync --status
```

Shows local and server state:

```
Project:     a1b2c3d4-...
Last pushed: action 42
Last pulled: seq 100
Pending:     3 events
Last sync:   2026-01-31T10:15:00Z

Server:
  Events:    150
  Last seq:  150
  Last event: 2026-01-31T10:20:00Z
```

**Key fields:**
- **Pending** -- local events not yet pushed
- **Last pushed** -- highest action_log rowid sent to server
- **Last pulled** -- highest server_seq received
- Gap between local "Last pulled" and server "Last seq" means there are remote changes to pull

## Team Management

All member commands operate on the currently linked project.

**Invite a member** (owner only):

```bash
td sync-project invite alice@example.com          # defaults to writer role
td sync-project invite bob@example.com reader      # read-only access
```

The invited user must have an account on the server (created via `td auth login`).

**List members:**

```bash
td sync-project members
```

**Change a member's role:**

```bash
td sync-project role <user-id> writer
```

**Remove a member:**

```bash
td sync-project kick <user-id>
```

### Roles

| Role | Push | Pull | Manage members | Delete project |
|---|---|---|---|---|
| owner | Yes | Yes | Yes | Yes |
| writer | Yes | Yes | No | No |
| reader | No | Yes | No | No |

## Conflict Resolution

Sync uses **last-write-wins**. When a pull overwrites a local record that was modified since the last sync, both versions are preserved in the `sync_conflicts` table.

### View conflicts

```bash
td sync conflicts
# Recent sync conflicts:
#   TIME                  TYPE      ENTITY     SEQ
#   2026-01-31 10:15:00   issues    abc123     105

td sync conflicts --limit 50
td sync conflicts --since 24h
td sync conflicts --since 1h30m
```

### What's stored

Each conflict record contains:
- `entity_type` and `entity_id` -- which record was overwritten
- `server_seq` -- the remote event's sequence number
- `local_data` -- JSON snapshot of the local version before overwrite
- `remote_data` -- JSON snapshot of the incoming remote version
- `overwritten_at` -- when the overwrite happened

### Recovery

If a conflict overwrote data you need, the `local_data` field in `sync_conflicts` contains the pre-overwrite state. You can query it directly:

```bash
sqlite3 .todos/issues.db "SELECT local_data FROM sync_conflicts WHERE entity_id='abc123'"
```

## Observability

### Client-side indicators

The sync commands provide direct feedback:

```
Pushed 5 events.                          # successful push
Nothing to push.                          # no pending changes
Pulled 12 events (12 applied).            # successful pull
Nothing to pull.                          # already up to date
⚠ 2 local records overwritten by remote changes:    # conflicts detected
  issues/abc123 (seq 105)
  comments/def456 (seq 108)
```

### Auth errors

```
✗ unauthorized - re-login may be needed
```

This means your API key is expired or revoked. Run `td auth login` again.

### Sync state inspection

Check the raw sync state in your local database:

```bash
sqlite3 .todos/issues.db "SELECT * FROM sync_state"
```

Fields: `project_id`, `last_pushed_action_id`, `last_pulled_server_seq`, `last_sync_at`, `sync_disabled`

### Pending event count

```bash
sqlite3 .todos/issues.db "SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0"
```

Or use `td sync --status` which shows this as the "Pending" count.

## Sync Lifecycle in Detail

### How local changes become sync events

1. You run any `td` command that modifies data (create issue, add log, etc.)
2. The `action_log` table records the action with: action type, entity type, entity ID, full-row payload (`new_data` and `previous_data` as JSON), timestamp
3. The entry starts with `synced_at = NULL`
4. Entity types are normalized on push (e.g., `board_position` becomes `board_issue_positions`); unrecognized types are skipped

### Push flow

1. `td sync --push` reads all `action_log` rows where `synced_at IS NULL AND undone = 0`
2. Each row is wrapped into an event with `schema_version: 1`, `new_data`, and `previous_data`
3. Events are POSTed to `/v1/projects/{id}/sync/push` in a single batch (max 1000)
4. Server assigns each event a `server_seq` and returns acks
5. Client marks each acked event with `synced_at` and `server_seq` in the action_log
6. `last_pushed_action_id` is updated in sync_state
7. All updates happen in a single transaction

### Pull flow

1. `td sync --pull` GETs `/v1/projects/{id}/sync/pull?after_server_seq=<last>&exclude_client=<device_id>`
2. Events from other devices are returned (your own are excluded)
3. For each event:
   - If `action_type` is `create` or `update`: INSERT OR REPLACE into the entity table
   - If `action_type` is `delete`: hard delete from entity table
   - If `action_type` is `soft_delete`: set `deleted_at` timestamp
4. If an existing row was overwritten, a conflict record is created
5. `last_pulled_server_seq` is updated
6. Pagination continues until `has_more` is false

### Duplicate protection

Events include `(device_id, session_id, client_action_id)` as a unique key on the server. If you push the same events twice (e.g., due to a network error before the response arrived), the server silently deduplicates them.

### Undo interaction

Undone actions (`undone = 1` in action_log) are excluded from push. If you undo a change before syncing, it won't be sent to the server. Once an action has been pushed, undoing it locally does not propagate the undo to other clients.

## Configuration Reference

### Files

| File | Purpose | Permissions |
|---|---|---|
| `~/.config/td/config.json` | Server URL, sync settings | 0644 |
| `~/.config/td/auth.json` | API key, email, device ID | 0600 |
| `.todos/issues.db` | Local database (includes sync_state, sync_conflicts, action_log) | project-local |

### Environment variables

| Variable | Description |
|---|---|
| `TD_SYNC_URL` | Override server URL |
| `TD_AUTH_KEY` | Override API key |

### Device ID

A unique device identifier is generated automatically on first use and stored in `auth.json`. It identifies your machine in the sync system and is used to:

- Tag events you push (so they can be attributed to your device)
- Exclude your own events on pull (so you don't re-apply your own changes)

## Command Reference

```
td auth login              # Start device auth flow
td auth logout             # Clear local credentials
td auth status             # Show current auth state

td sync-project create     # Create remote project
td sync-project list       # List your projects
td sync-project link       # Link local project to remote
td sync-project unlink     # Unlink from remote project
td sync-project members    # List project members
td sync-project invite     # Add member by email
td sync-project kick       # Remove member
td sync-project role       # Change member role

td sync                    # Push then pull
td sync --push             # Push only
td sync --pull             # Pull only
td sync --status           # Show sync state
td sync conflicts          # List recent conflicts
td sync conflicts --limit  # Limit results (default 20, max 1000)
td sync conflicts --since  # Filter by duration (e.g. 24h, 1h30m)
```
