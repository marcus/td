---
sidebar_position: 7
---

# Notes

Use notes for information that should stay close to the project but does not belong in the issue lifecycle: architecture decisions, meeting notes, rough plans, runbooks, or research scraps you want future sessions to find quickly.

Unlike issues, notes do not move through statuses or review. They are just local, searchable documents stored in the same project data as the rest of `td`.

## Creating Notes

Create a note with a title and inline content:

```bash
td note add "Architecture decisions" --content "Use OAuth device flow for local development."
```

If you omit `--content`, `td` opens your default editor so you can write longer content comfortably:

```bash
td note add "Release checklist"
```

`td` uses `$EDITOR` when it is set, and falls back to `vi` otherwise.

## Listing and Searching

`td note list` shows unarchived notes by default. Results are ordered with pinned notes first, then by most recently updated note.

```bash
td note list
td note list --search "oauth"
td note list --pinned
td note list --archived
td note list --all
```

Use `--limit` to cap the number of results:

```bash
td note list --limit 10
```

For scripts and tooling, use JSON output:

```bash
td note list --json
td note list --output json
```

## Viewing a Note

Show the full contents of a single note:

```bash
td note show nt-abc123
```

Use JSON when you need structured output:

```bash
td note show nt-abc123 --json
```

## Editing Notes

Update the title, the content, or both:

```bash
td note edit nt-abc123 --title "ADR: OAuth device flow"
td note edit nt-abc123 --content "Updated implementation notes."
```

If you run `td note edit` with no flags, `td` opens the existing content in your editor and saves the edited result when the editor exits:

```bash
td note edit nt-abc123
```

## Pinning and Archiving

Pin notes you want to keep at the top of `td note list`:

```bash
td note pin nt-abc123
td note unpin nt-abc123
```

Archive notes when they should stay around but no longer appear in the default list:

```bash
td note archive nt-abc123
td note unarchive nt-abc123
```

Archived notes still appear in `td note list --archived` and `td note list --all`.

## Deleting Notes

Delete a note when you no longer want it in normal note views:

```bash
td note delete nt-abc123
```

This is a soft delete, so the note is removed from regular `td note` lookups without immediately erasing the underlying record.

## Command Summary

| Command | What it does |
|---------|---------------|
| `td note add "title" [--content "..."]` | Create a note |
| `td note list [flags]` | List notes with filters and search |
| `td note show <id> [--json]` | Show one note |
| `td note edit <id> [--title ...] [--content ...]` | Update a note |
| `td note delete <id>` | Soft-delete a note |
| `td note pin <id>` / `td note unpin <id>` | Pin or unpin a note |
| `td note archive <id>` / `td note unarchive <id>` | Archive or restore a note to the default list |
