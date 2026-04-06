---
sidebar_position: 3
---

# Notes

Use notes for durable project context that does not belong to a single issue. Notes live next to your `td` data, but they stay outside the issue workflow.

## When To Use Notes

Use notes when you want to capture:

- architecture decisions that span multiple issues
- meeting notes, research, and rough planning
- onboarding context for future agent or human sessions
- checklists or scratchpad content that should not change issue status

Use issues when the work needs ownership, status, logs, handoffs, review, or acceptance criteria.

| Use this | When you need |
|----------|---------------|
| `td note` | Durable context, references, brainstorms, or docs-in-progress |
| `td create` / `td task create` | Trackable work with workflow state, logs, handoffs, and review |

## Create A Note

Create a note with inline content:

```bash
td note add "Architecture decisions" --content "Use SQLite locally and sync events remotely."
```

If you omit `--content`, `td` opens your `$EDITOR` so you can write longer notes comfortably:

```bash
td note add "Sprint retro"
```

## List And Search Notes

```bash
td note list
td note list --pinned
td note list --all
td note list --archived
td note list --search "auth"
td note list --json
```

By default, `td note list` hides archived notes. Use `--archived` to show only archived notes, or `--all` to include both active and archived notes.

## Show And Edit Notes

```bash
td note show nt-abc123
td note show nt-abc123 --json

td note edit nt-abc123 --title "Auth architecture"
td note edit nt-abc123 --content "Updated content"
td note edit nt-abc123
```

Running `td note edit <id>` with no flags opens your editor with the current content loaded.

## Pin, Archive, And Delete

Pinned notes stay easy to find, and archived notes stay out of the default listing without being deleted.

```bash
td note pin nt-abc123
td note unpin nt-abc123

td note archive nt-abc123
td note unarchive nt-abc123
```

Use `delete` when you want to remove a note entirely:

```bash
td note delete nt-abc123
```

There is no `td note restore`, so archive is the safer choice when you only want a note out of the default list.

## Suggested Workflow

```bash
td note add "Release checklist"
td note pin nt-abc123
td note list --pinned
td note edit nt-abc123
td note archive nt-abc123
```

This works well for lightweight project docs, recurring checklists, and context you want available in future agent sessions without opening a new issue.

## Notes In Shared Projects

If you are using the sync workflow, notes can sync with the rest of the project data. To keep notes local-only in a shared project, disable note sync for that project:

```bash
td feature set sync_notes false
```

Unlike the `sync_cli` command gate, `sync_notes` is resolved from project config at runtime, so `td feature set` works for this flag.

Re-enable it later with:

```bash
td feature set sync_notes true
```

For remote setup, see [Sync & Collaboration](./sync-collaboration.md).
