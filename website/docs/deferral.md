---
sidebar_position: 8
---

# Deferral & Due Dates

td supports GTD-style deferral (tickler) and due dates (deadlines) — two distinct concepts that work together to keep your backlog focused on what's actionable *right now*.

## Why Two Dates?

**`defer_until`** is a *snooze*. It hides a task from your default list until a future date. The task isn't actionable yet — maybe you're waiting on something, or it's scheduled for next sprint. When the date arrives, the task resurfaces automatically.

**`due_date`** is a *deadline*. The task stays visible regardless, but now it has a hard date. Overdue tasks get flagged.

You can use both on the same issue: defer it until next Monday, with a due date of Friday. It'll appear Monday and show as due-soon immediately.

## Deferring Tasks

```bash
td defer td-a1b2 +7d          # Defer for 7 days
td defer td-a1b2 +2w          # Defer for 2 weeks
td defer td-a1b2 monday       # Next Monday
td defer td-a1b2 tomorrow     # Tomorrow
td defer td-a1b2 next-week    # Next Monday
td defer td-a1b2 next-month   # 1st of next month
td defer td-a1b2 2026-03-15   # Exact date
td defer td-a1b2 --clear      # Remove deferral, make immediately actionable
```

When you re-defer a task to a *later* date, td increments a **defer count**. This tracks how many times a task has been pushed back — a useful signal that something might need to be rethought, broken down, or dropped entirely.

## Setting Due Dates

```bash
td due td-a1b2 +3d            # Due in 3 days
td due td-a1b2 friday         # Due next Friday
td due td-a1b2 2026-04-01     # Due on a specific date
td due td-a1b2 --clear        # Remove due date
```

Same date formats as `td defer`.

## Date Formats

All date arguments accept:

| Format | Example | Meaning |
|--------|---------|---------|
| `+Nd` | `+7d` | N days from now |
| `+Nw` | `+2w` | N weeks from now |
| `+Nm` | `+1m` | N months from now |
| Day name | `monday`, `friday` | Next occurrence of that weekday |
| `tomorrow` | | Tomorrow |
| `today` | | Today |
| `next-week` | | Next Monday |
| `next-month` | | 1st of next month |
| `YYYY-MM-DD` | `2026-03-15` | Exact date |

## Setting Dates at Creation

Use `--defer` and `--due` flags on `td create`:

```bash
td create "Write quarterly report" --defer next-week --due 2026-03-31
td create "Review PR when CI passes" --defer tomorrow
```

## Updating Dates

The same flags work on `td update`:

```bash
td update td-a1b2 --defer +3d
td update td-a1b2 --due friday
td update td-a1b2 --defer "" --due ""   # Clear both
```

Passing an empty string clears the date.

## List Behavior

By default, `td list` **hides deferred tasks** — tasks whose `defer_until` is in the future. This keeps your list focused on what's actionable today.

```bash
td list                  # Actionable tasks only (deferred hidden)
td list --all            # Everything, including deferred
td list --deferred       # Only deferred tasks
td list --surfacing      # Tasks whose deferral expires today
td list --overdue        # Tasks past their due date
td list --due-soon       # Tasks due within 3 days
```

These filters are mutually exclusive — use one at a time.

## Monitor Display

In `td monitor`, the task detail modal shows defer and due dates when set:

- **Deferred until** — the date with relative context (e.g., "2026-02-21 (in 7 days)")
- **Due date** — with warning styling for due-soon and error styling for overdue
- **Defer count** — shown when greater than 0, indicating how many times the task has been re-deferred
