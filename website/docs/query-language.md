---
sidebar_position: 5
---

# TDQ Query Language

TDQ (td Query) is a powerful expression language for filtering issues. It is used by the `td query` command and in board definitions to select issues matching specific criteria.

## Basic Queries

```bash
td query "status = in_progress"
td query "priority <= P1"
td query "type = bug"
td query "labels ~ auth"
```

## Operators

| Operator | Meaning | Example |
|----------|---------|---------|
| `=` | Equals | `status = open` |
| `!=` | Not equals | `status != closed` |
| `~` | Contains | `labels ~ frontend` |
| `!~` | Not contains | `title !~ WIP` |
| `<` | Less than | `priority < P2` |
| `>` | Greater than | `priority > P3` |
| `<=` | Less than or equal | `priority <= P1` |
| `>=` | Greater than or equal | `priority >= P2` |

## Boolean Operators

Combine expressions with `AND`, `OR`, and `NOT`. Use parentheses to control precedence.

```bash
td query "status = in_progress AND priority <= P1"
td query "type = bug OR type = feature"
td query "priority <= P1 AND NOT labels ~ frontend"
td query "(type = bug OR type = feature) AND status != closed"
```

## Available Fields

| Field | Description |
|-------|-------------|
| `status` | Issue status: `open`, `in_progress`, `in_review`, `closed`, `blocked` |
| `type` | Issue type: `bug`, `feature`, `task`, etc. |
| `priority` | Priority level: `P0`, `P1`, `P2`, `P3` |
| `points` | Story points (numeric) |
| `labels` | Comma-separated label list |
| `title` | Issue title text |
| `description` | Issue description text |
| `created` | Creation timestamp |
| `updated` | Last updated timestamp |
| `closed` | Closed timestamp |
| `implementer` | Assigned implementer |
| `reviewer` | Assigned reviewer |
| `parent` | Parent issue ID |
| `epic` | Epic issue ID |

## Date Queries

Use relative date expressions with duration suffixes:

```bash
td query "created >= -7d"        # Created in last 7 days
td query "updated >= -24h"       # Updated in last 24 hours
```

## Query Functions

Built-in functions provide common filter patterns:

```bash
td query "rework()"              # Issues rejected and needing fixes
td query "stale(14)"             # Issues not updated in 14 days
```

## Using with Boards

Define boards with persistent query filters:

```bash
td board create "Urgent Bugs" --query "type = bug AND priority <= P1"
```

Issues matching the query will appear on the board automatically. See [Boards](./boards.md) for more on board configuration.
