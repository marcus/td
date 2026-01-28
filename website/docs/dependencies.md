---
sidebar_position: 4
---

# Dependencies & Critical Path

Model dependencies between issues. Find bottlenecks. Optimize work order with critical-path analysis.

## Adding Dependencies

```bash
td dep add td-abc td-xyz    # td-abc depends on td-xyz (td-xyz must be done first)
```

This means `td-xyz` must be resolved before `td-abc` can proceed.

## Viewing Dependencies

```bash
td dep td-abc               # What does td-abc depend on?
td dep td-abc --blocking    # What depends on td-abc? (what it blocks)
td blocked-by td-xyz        # Show all issues blocked by td-xyz
```

## Critical Path

```bash
td critical-path            # Optimal sequence to unblock the most work
```

Uses topological sorting weighted by how many issues each task unblocks. Start with high-impact work first.

Example output:

```
CRITICAL PATH SEQUENCE (resolve in order):
  1. td-f0c994  Scaffold project  [open]
     └─▶ unblocks 22
  2. td-a9fbdf  Add dependency  [open]
     └─▶ unblocks 17

START NOW (no blockers, unblocks others):
  ▶ td-f0c994  Scaffold project  (unblocks 22)

BOTTLENECKS (blocking most issues):
  td-f0c994: 22 issues waiting
```

## Blocking Status

When an issue's dependency isn't resolved, mark it accordingly:

```bash
td block td-abc              # Mark as blocked
td unblock td-abc            # Unblock back to open
```

## Auto-Unblocking

When a blocking issue is approved or closed, td automatically unblocks any dependents whose dependencies are now all resolved. This works in both the CLI and TUI:

```
td approve td-xyz
# APPROVED td-xyz (reviewer: ses_abc123)
#   ↓ Dependent td-abc auto-unblocked
```

A dependent transitions from `blocked` → `open` only when **all** of its dependencies are closed. If it has multiple blockers, it stays blocked until the last one is resolved.

Auto-unblocking also cascades through epic hierarchies. When closing the last child of an epic causes the epic to auto-close, any issues blocked by that epic are unblocked too.
