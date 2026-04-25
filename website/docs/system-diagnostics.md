---
sidebar_position: 12
---

# System and Diagnostics

td keeps most state in the local project database, with small amounts of configuration in project config and `~/.config/td`. These commands help inspect configuration, feature flags, health, audit logs, and import/export state.

## Configuration

```bash
td config list
td config get <key>
td config set <key> <value>
```

Directory associations let you run td from one directory while using another directory's td project:

```bash
td config associate /path/to/worktree /path/to/project
td config associate /path/to/project
td config associations
td config dissociate /path/to/worktree
```

Associations are stored in `~/.config/td/associations.json`. If only one argument is passed to `config associate`, td treats it as the target project and uses the current directory as the source.

## Feature Flags

```bash
td feature list
td feature get sync_cli
td feature set sync_cli true
td feature unset sync_cli
```

Feature flags resolve from environment overrides, then local project config, then defaults. Common sync-related flags include `sync_cli`, `sync_autosync`, `sync_monitor_prompt`, and `sync_notes`.

## Health Checks

```bash
td doctor
td upgrade
td version
td info
td workflow
```

`td doctor` runs sync setup diagnostics. `td upgrade` applies pending database migrations. `td version` checks the installed version, `td info` prints project/database statistics, and `td workflow` displays the issue status state machine. `td workflow --dot` and `td workflow --mermaid` export graph formats.

## Agent Exit Checks

```bash
td check-handoff
td check-handoff --quiet
td check-handoff --json
```

Agents should run `td check-handoff` before exiting or ending a context window. It returns exit code 0 when no handoff is needed and exit code 1 when the current session has in-progress work that should be handed off.

## Error and Audit Logs

```bash
td errors
td errors --since 24h --limit 50
td errors --session ses_abc123
td errors --json
td errors --count

td security
td security --json
```

`td errors` records failed td invocations so you can spot CLI friction or repeated agent mistakes. `td security` shows review and close workflow exceptions such as creator-approval and self-close events.

The same surfaces are available under `td stats`:

```bash
td stats analytics
td stats errors
td stats security
```

`td stats analytics` reports command usage analytics. Set `TD_ANALYTICS=false` to disable analytics collection.

## Runtime Diagnostics

```bash
td debug-stats
td last
td last -n 10
```

`td debug-stats` emits Go runtime memory and goroutine statistics as JSON for soak or endurance testing. `td last` shows the last recorded action, or the last `n` actions with `-n`.

## Import and Export

```bash
td export --format json --output td-export.json
td export --format md --render-markdown --output td-export.md
td import td-export.json --dry-run
td import td-export.json --force
```

Use `--all` on export to include closed and deleted issues. Use `--dry-run` on import before applying changes to a live project.

## Local API Server

```bash
td serve --port 0
td serve --addr localhost --port 8080 --token secret --cors http://localhost:3000
```

`td serve` starts the local HTTP API. When `--port 0` is used, td chooses an available port and writes it to `.todos/serve-port`.
