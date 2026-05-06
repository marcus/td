---
sidebar_position: 4
---

# Configuration

td has two user-facing configuration surfaces:

- `td config` manages sync settings stored in `~/.config/td/config.json`.
- `td feature` manages project-local feature flags and string feature settings.

Some sync commands are rollout-gated. If `td auth`, `td config`, or `td sync` is unavailable in your install, enable the CLI surface from the project:

```bash
td feature set sync_cli true
```

## Sync Config

Inspect current sync configuration:

```bash
td config list
td config get sync.url
td config get sync.enabled
td config get sync.auto.interval
```

Set supported keys with `td config set <key> <value>`:

```bash
td config set sync.url http://localhost:8080
td config set sync.enabled true
td config set sync.auto.enabled false
td config set sync.auto.interval 10m
td config set sync.snapshot_threshold 250
```

Supported keys:

| Key | Value | Notes |
|-----|-------|-------|
| `sync.url` | URL | Sync server base URL. |
| `sync.enabled` | boolean | Enables sync behavior for the project. |
| `sync.auto.enabled` | boolean | Enables or disables autosync. |
| `sync.auto.debounce` | duration | Debounce after local mutations, such as `3s`. |
| `sync.auto.interval` | duration | Periodic autosync interval, such as `5m`. |
| `sync.auto.pull` | boolean | Include pull during autosync. |
| `sync.auto.on_start` | boolean | Sync when td starts. |
| `sync.snapshot_threshold` | integer | Minimum server event count before snapshot bootstrap is considered. |

`td config` does not currently have an `unset` subcommand. To return optional sync settings to their built-in defaults, remove the key from `~/.config/td/config.json` or set the default-equivalent value explicitly.

## Environment Overrides

Environment variables are useful for one-off commands and CI jobs:

```bash
TD_SYNC_URL=http://localhost:8080 td auth status
TD_AUTH_KEY=td_api_key_xxx td sync --status
TD_SYNC_AUTO=false td list
TD_SYNC_SNAPSHOT_THRESHOLD=0 td sync
```

Unset shell overrides with your shell's normal syntax:

```bash
unset TD_SYNC_URL
unset TD_AUTH_KEY
```

## Feature Flags

List feature flags and their resolved source:

```bash
td feature list
td feature get sync_cli
```

Set and unset project-local overrides:

```bash
td feature set sync_cli true
td feature set sync_autosync false
td feature unset sync_autosync
```

Known boolean flags include:

| Flag | Purpose |
|------|---------|
| `sync_cli` | Enables end-user sync/auth/config commands. |
| `sync_autosync` | Enables background autosync hooks. |
| `sync_monitor_prompt` | Enables monitor sync setup prompts. |
| `sync_notes` | Enables notes entity sync. |
| `balanced_review_policy` | Deprecated compatibility flag; prefer `review_policy_mode=balanced`. |

## Review Policy Mode

`review_policy_mode` selects review behavior:

| Mode | Behavior |
|------|----------|
| `delegated` | Default for new installs. Review must be independent; any session may close after an approval is recorded. |
| `strict` | The reviewer must have no prior involvement. |
| `balanced` | Legacy mode with a creator-approval exception when a reason is supplied. |

Use a one-off environment override when testing policy behavior:

```bash
TD_FEATURE_REVIEW_POLICY_MODE=strict td approve td-a1b2
```

For a persistent project setting, store `review_policy_mode` in `.todos/config.json` under `feature_string_flags`:

```json
{
  "feature_string_flags": {
    "review_policy_mode": "delegated"
  }
}
```

Remove the key to return to the built-in default. The current `td feature` command manages boolean feature flags; it does not set or unset string-valued features.

Prefer `review_policy_mode` over the deprecated `balanced_review_policy` flag.
