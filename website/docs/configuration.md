---
sidebar_position: 16
---

# Configuration

td stores configuration in a few places because some settings are global to your user account, while others belong to the current project.

## Config Locations

| Location | Scope | Contains |
|----------|-------|----------|
| `~/.config/td/config.json` | User-global | Sync URL, sync enablement, auto-sync settings |
| `~/.config/td/auth.json` | User-global | Sync API key, email, server URL, device ID |
| `~/.config/td/associations.json` | User-global | Directory-to-project associations |
| `.todos/config.json` | Project-local | Focus, active work session, monitor filters, feature flags |
| `.todos/issues.db` | Project-local database | Issues, boards, notes, logs, handoffs, sync state, board positions |

Project-local config is safe to keep with a project. User-global auth files should not be committed.

## Sync Config

`td config` manages sync settings in `~/.config/td/config.json`.

```bash
td config list
td config get sync.url
td config set sync.url https://sync.example.com
```

Supported keys:

| Key | Default | Description |
|-----|---------|-------------|
| `sync.url` | `http://localhost:8080` | Sync server URL |
| `sync.enabled` | `false` when omitted | Stored sync enablement flag |
| `sync.auto.enabled` | `true` | Master switch for auto-sync hooks |
| `sync.auto.debounce` | `3s` | Minimum time between post-mutation syncs |
| `sync.auto.interval` | `5m` | Periodic sync interval used by monitor workflows |
| `sync.auto.pull` | `true` | Pull as part of auto-sync |
| `sync.auto.on_start` | `true` | Sync when td commands start |
| `sync.snapshot_threshold` | `100` | Server event count before snapshot bootstrap is attempted |

:::info
`td config` is registered with the sync CLI. If the command is unavailable, enable `sync_cli` with `TD_ENABLE_FEATURE=sync_cli`.
:::

## Feature Flags

Use `td feature` for project-local feature flags stored in `.todos/config.json`.

```bash
td feature list
td feature get sync_cli
td feature set sync_cli true
td feature unset sync_cli
```

Known flags include:

| Feature | Default | Purpose |
|---------|---------|---------|
| `balanced_review_policy` | `true` | Legacy creator-approval exception for externally implemented work |
| `sync_cli` | `false` | Registers sync, auth, project, doctor, and config commands |
| `sync_autosync` | `false` | Enables startup and post-mutation auto-sync hooks |
| `sync_monitor_prompt` | `false` | Enables monitor sync setup prompts |
| `sync_notes` | `true` | Allows notes to sync as an entity type |

Environment overrides win over project config:

```bash
TD_FEATURE_SYNC_CLI=true td sync --status
TD_ENABLE_FEATURE=sync_cli,sync_autosync td sync
TD_DISABLE_FEATURE=sync_autosync td list
TD_DISABLE_EXPERIMENTAL=1 td list
```

Feature override forms:

| Variable | Effect |
|----------|--------|
| `TD_FEATURE_<NAME>` | Set a specific feature, such as `TD_FEATURE_SYNC_CLI=true` |
| `TD_ENABLE_FEATURE` or `TD_ENABLE_FEATURES` | Comma-separated features to enable |
| `TD_DISABLE_FEATURE` or `TD_DISABLE_FEATURES` | Comma-separated features to disable |
| `TD_DISABLE_EXPERIMENTAL` | Disable all experimental features |

## Review Policy

The current public review-policy flag is `balanced_review_policy`.

```bash
td feature get balanced_review_policy
td feature set balanced_review_policy false
td feature unset balanced_review_policy
```

When enabled, a creator session can approve work implemented by a different session if it provides a reason. Implementation self-approval remains blocked. See [AI Agent Integration](./ai-integration.md#balanced-review-policy) for examples.

## Environment Variables

Common environment variables:

| Variable | Purpose |
|----------|---------|
| `TD_WORK_DIR` | Use a project directory without changing the shell cwd |
| `TD_SESSION_ID` | Pin the current agent/session identity |
| `TD_LOG_FILE` | Write td debug logs to a file |
| `TD_ANALYTICS` | Set to `false` to disable local analytics |
| `TD_SYNC_URL` | Override the sync server URL |
| `TD_AUTH_KEY` | Override sync API key |
| `TD_SYNC_SNAPSHOT_THRESHOLD` | Override snapshot bootstrap threshold |
| `TD_SYNC_AUTO` | Override auto-sync enablement |
| `TD_SYNC_AUTO_START` | Override startup auto-sync |
| `TD_SYNC_AUTO_DEBOUNCE` | Override post-mutation debounce |
| `TD_SYNC_AUTO_INTERVAL` | Override periodic auto-sync interval |
| `TD_SYNC_AUTO_PULL` | Override whether auto-sync pulls |

## Directory Associations

Associations let td use one project database while commands run from another directory. This is useful for monorepos, generated worktrees, or tools that launch from nested folders.

```bash
td config associate /path/to/project
td config associate /path/to/nested/dir /path/to/project
td config associations
td config dissociate /path/to/nested/dir
```

Associations are stored in `~/.config/td/associations.json`.
