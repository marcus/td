---
sidebar_position: 7
---

# Multi-Issue Work Sessions

Work sessions group related issues when an agent tackles them together. Logs and handoffs fan out to all tagged issues, keeping everything in sync without repeating commands.

## Starting a Work Session

```bash
td ws start "Auth implementation"    # Start named session
```

This creates a work session container that can hold multiple issues.

## Tagging Issues

```bash
td ws tag td-a1b2 td-c3d4            # Associate issues (auto-starts open ones)
td ws tag --no-start td-e5f6         # Associate without starting
```

Tagged issues automatically transition to `in_progress` unless `--no-start` is passed.

## Logging Progress

```bash
td ws log "Shared token storage implemented"    # Fans out to ALL tagged issues
td ws log --only td-a1b2 "Specific to this issue"  # Log to one issue only
```

By default, logs apply to every tagged issue. Use `--only` to target a single issue when the update is narrowly scoped.

## Viewing Session State

```bash
td ws current                        # Show current session, tagged issues, logs
```

Displays the active work session name, all tagged issues with their statuses, and recent log entries.

## Handoffs

```bash
td ws handoff                        # Capture state for all tagged issues, end session
```

This creates handoff entries for each tagged issue and closes the work session. The next agent (or session) picks up exactly where you left off.

## When to Use Work Sessions

- Implementing related features together
- Bug fixes that span multiple issues
- Refactoring touching multiple tracked components

## Sessions vs Work Sessions

| | Session | Work Session |
|---|---|---|
| **What** | Identity | Work container |
| **Lifecycle** | Automatic (terminal/agent context) | Created explicitly with `td ws start` |
| **Requirement** | Always exists | Optional grouping |
| **Purpose** | Tracks who is working | Groups what is being worked on |
