---
sidebar_position: 12
---

# Analytics & Stats

td tracks usage patterns and system health locally. All data stays on your machine.

## Command Usage Statistics

```bash
td stats analytics
```

Shows which commands are used most, frequency patterns, and usage over time.

## Security Audit Log

```bash
td stats security
```

Shows review workflow exceptions, such as administrative closes or legacy creator-approval exceptions.

## Error Tracking

```bash
td stats errors
```

Shows failed command attempts - useful for debugging agent issues.

For filters and cleanup commands, see [Troubleshooting](./troubleshooting.md#command-error-logs).

## Monitor Stats

Press `s` in the monitor to view:

- Status breakdown bar chart
- Type and priority distributions
- Summary metrics (total, points, completion rate)
- Timeline data
- Activity stats (logs, handoffs, most active session)

## Disabling Analytics

```bash
export TD_ANALYTICS=false
```

Set this environment variable to disable local analytics collection.

## Data Storage

All analytics are stored in the local SQLite database (`.todos/issues.db`). Nothing leaves your machine.
