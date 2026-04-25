---
sidebar_position: 13
---

# System and Diagnostics

This page collects operational commands for configuration, diagnostics, exports, migration checks, and agent safety hooks.

## Configuration

```bash
td config list
td config get <key>
td config set <key> <value>
td config associate [dir] <target>
td config associations
td config dissociate [dir]
```

Use directory associations when a command needs to resolve the right td project from outside the project root. With one argument, `td config associate` treats that argument as the target and uses the current directory as the source.

## Feature Flags

```bash
td feature list
td feature get <flag>
td feature set <flag> <value>
td feature unset <flag>
```

Feature flags resolve from known defaults plus local project overrides. Use `td feature list` before changing a flag so the current state is visible.

## Error and Security Logs

```bash
td errors
td errors --limit 50 --since 24h
td errors --session ses_abc123
td errors --json
td errors --count
td security
td security --json
```

`td errors` records failed td invocations for troubleshooting agent behavior and workflow friction. `td security` records review and close workflow exceptions such as creator approvals or self-close exceptions.

Both commands support `--clear` when you intentionally want to reset the local log.

## Sync Diagnostics

```bash
td doctor
td auth status
td sync --status
```

`td doctor` runs sync setup checks. It currently accepts no operational flags beyond `--help`, so combine it with `td auth status` and `td sync --status` for more context.

## Workflow Inspection

```bash
td workflow
td workflow --mermaid
td workflow --dot
```

Use `td workflow` to inspect valid issue status transitions. The diagram flags are useful when documenting or reviewing state-machine changes.

## Export and Import

```bash
td export --format json --output td-export.json
td export --format md --render-markdown --output td-export.md
td import td-export.json --dry-run
td import td-export.json --force
```

Use `--dry-run` before imports that may overwrite existing issue data. Use `--all` on export when closed or deleted issues should be included.

## Version, Migration, and Project Info

```bash
td version
td version --short
td version --check=false
td upgrade
td info
td info --json
td stats analytics
td stats security
td stats errors
td last
td debug-stats
```

`td upgrade` runs pending database migrations. `td info --json` is the machine-readable project overview; `td stats analytics`, `td stats security`, and `td stats errors` are the usage and diagnostic views.

## Agent Exit Safety

```bash
td check-handoff
td check-handoff --quiet
td check-handoff --json
```

Agents should use `td check-handoff` before stopping work or ending a context window. It returns exit code 0 when no handoff is needed and exit code 1 when in-progress work needs a `td handoff`.
