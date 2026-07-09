---
sidebar_position: 8
---

# Notes

Notes are freeform project memory. Use them for context that does not fit a task lifecycle: architecture decisions, meeting notes, release checklists, debugging breadcrumbs, or "remember this later" material.

Notes live in the same local td database as issues, but they are not issues. They do not have status, priority, review state, or dependencies.

## Create Notes

Create a note with inline content:

```bash
td note add "Release checklist" --content "Tag, build, smoke test, publish."
```

Create a longer note in your editor:

```bash
td note add "Auth design notes"
```

When `--content` is omitted, td opens `$EDITOR` and saves the final buffer as the note content. If `$EDITOR` is not set, td falls back to `vi`.

## Find Notes

List active notes:

```bash
td note list
```

Filter the list:

```bash
td note list --search "sync"
td note list --pinned
td note list --archived
td note list --all
td note list --limit 50
```

Get machine-readable output:

```bash
td note list --json
td note list --output json
```

## Read and Edit

Show a note:

```bash
td note show nt-abc123
td note show nt-abc123 --json
```

Update the title or content directly:

```bash
td note edit nt-abc123 --title "Sync launch notes"
td note edit nt-abc123 --content "New content"
```

Open the existing content in your editor:

```bash
td note edit nt-abc123
```

## Pin, Archive, and Delete

Pin notes you want near the top of the list:

```bash
td note pin nt-abc123
td note unpin nt-abc123
```

Archive notes that should stay searchable but leave the default list:

```bash
td note archive nt-abc123
td note unarchive nt-abc123
```

Delete soft-removes a note:

```bash
td note delete nt-abc123
```

## Practical Workflows

Use notes for stable project context:

```bash
td note add "API invariants" --content "Project IDs are UUIDs. Issue IDs keep the td- prefix."
td note pin nt-abc123
```

Use notes to preserve investigation state outside a single issue:

```bash
td note add "SQLite lock investigation"
td note list --search "SQLite"
```

Use notes as lightweight runbooks:

```bash
td note add "Release runbook" --content "1. go test ./...\n2. build website\n3. tag release"
td note pin nt-release
```

For work that needs review, dependencies, ownership, or handoff state, create an issue instead. For durable background memory, use a note.
