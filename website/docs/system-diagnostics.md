---
sidebar_position: 12
---

# System And Diagnostics

These commands help inspect local state, manage configuration, diagnose sync problems, and support agent workflows.

## Configuration

Project configuration is managed through `td config`:

```bash
td config list
td config get review_policy_mode
td config set review_policy_mode delegated
```

Directory associations let a directory resolve to another td project without placing `.td-root` files everywhere:

```bash
td config associate /path/to/repo /path/to/project
td config associations
td config dissociate /path/to/repo
```

## Feature Flags

Experimental behavior can be inspected and overridden locally:

```bash
td feature list
td feature get review_policy_mode
td feature set review_policy_mode delegated
td feature unset review_policy_mode
```

Use feature flags sparingly in shared projects. Record any workflow-affecting change in a note or issue comment so other sessions understand the environment.

## Health Checks

Run `doctor` for sync setup diagnostics:

```bash
td doctor
```

For database relation checks:

```bash
td doctor fk
```

For version and migrations:

```bash
td version
td version --short
td upgrade
```

## Logs And Audit Trails

`td` records command analytics, failed invocations, and workflow exceptions locally.

```bash
td stats analytics
td errors
td errors --since 24h --limit 50
td errors --json
td security
td security --json
```

`td security` shows review and close exceptions such as creator approval or self-close audit events. The same views are available through `td stats security` and `td stats errors`.

## Import, Export, And Project Info

Use export and import for backups, migration, and human-readable snapshots:

```bash
td export --format json --output issues.json
td export --format md --render-markdown --output issues.md
td import issues.json --dry-run
td import issues.json
```

Inspect project state:

```bash
td info
td info --json
```

## Workflow Diagrams

Show the issue lifecycle in terminal, GraphViz, or Mermaid format:

```bash
td workflow
td workflow --dot
td workflow --mermaid
```

## Agent Handoff Checks

Agents should run `td check-handoff` before exiting a session or handing control back to a human:

```bash
td check-handoff
td check-handoff --quiet
td check-handoff --json
```

Exit code `0` means no handoff is needed. Exit code `1` means the current session has in-progress work that should be captured with `td handoff` or `td ws handoff`.

## Undo And Last Action

When a supported local action was a mistake:

```bash
td undo last
td undo
```

Review the last action before undoing if the session has been busy.
