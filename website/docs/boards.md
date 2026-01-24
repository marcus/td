---
sidebar_position: 3
---

# Boards

Boards are query-based views that organize issues into focused collections. They use TDQ (td query language) to filter issues and support manual positioning for custom ordering.

## Creating Boards

Create boards with a name and a TDQ query to define which issues appear:

```bash
td board create "Sprint 1" --query "priority <= P1"
td board create "Auth Work" --query "labels ~ auth"
td board create "Current" --query "status = in_progress OR status = in_review"
td board create "Bugs" --query "type = bug AND status != closed"
```

## Viewing Boards

```bash
td board list              # List all boards
td board show sprint-1     # Show board with issues
```

## Positioning Issues

Issues auto-match a board via its query. Manual positioning controls the ordering within the board:

```bash
td board move sprint-1 td-a1b2 1    # Move issue to position 1
```

## Monitor Integration

Boards display as swimlanes in the TUI monitor:

```bash
td monitor    # Press 'b' for board view with swimlanes
```

Issues are organized by status columns: open, in_progress, in_review, closed.

## Board Management

```bash
td board edit sprint-1 --name "Sprint 2" --query "priority <= P2"
td board delete sprint-1
```
