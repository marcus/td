---
sidebar_position: 6
---

# Epics & Trees

Epics track large initiatives spanning multiple issues. Use tree visualization to see parent-child relationships.

## Creating Epics

```bash
td epic create "Multi-user support" --priority P0
td epic create "API v2" --priority P1 --description "Complete API redesign"
```

## Listing Epics

```bash
td epic list
```

## Adding Children

```bash
td tree add-child epic-id child-issue-id
# Or use --parent flag when creating
td create "Auth endpoint" --type task --parent epic-id
```

## Viewing Trees

```bash
td tree epic-id
```

Shows hierarchical tree:

```
td-0ee243 "Multi-user support" [open] epic
  ├── td-f0c994 "User registration" [in_progress] task
  ├── td-198595 "Role-based access" [open] feature
  └── td-ea570f "Admin dashboard" [open] task
```

## Epic Progress

Epics show progress based on child issue statuses. Use `td show epic-id` to see all children and their statuses.

## Listing Issues in an Epic

```bash
td list --epic epic-id
```
