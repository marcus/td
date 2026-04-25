---
sidebar_position: 8
---

# Notes

Notes are freeform project memory that does not belong to a specific issue. Use them for durable context such as design decisions, meeting notes, investigation scratchpads, rollout checklists, or links an agent should keep nearby.

Issue comments are different: comments are attached to one issue and travel with that issue's activity history. Notes are standalone records with their own lifecycle, pin/archive state, and note IDs.

## Create Notes

```bash
td note add "Architecture decisions"
td note add "Meeting notes" --content "Discussed API design"
```

If `--content` is omitted, td opens `$EDITOR` and saves the edited buffer as the note content.

## Find Notes

```bash
td note list
td note list --pinned
td note list --archived
td note list --all
td note list --search "api"
td note list --limit 100
```

By default, `td note list` hides archived notes. Use `--all` to include archived notes or `--archived` to show only archived notes.

## View and Export Notes

```bash
td note show nt-abc123
td note show nt-abc123 --json
td note list --json
td note list --output json
```

JSON output is supported by `note list` and `note show`. Create, edit, pin, unpin, archive, unarchive, and delete print concise status lines instead.

## Edit Notes

```bash
td note edit nt-abc123 --title "New title"
td note edit nt-abc123 --content "Updated content"
td note edit nt-abc123
```

With no `--title` or `--content` flag, td opens `$EDITOR` with the current note content.

## Organize Notes

```bash
td note pin nt-abc123
td note unpin nt-abc123
td note archive nt-abc123
td note unarchive nt-abc123
td note delete nt-abc123
```

Pinned notes appear with a `*` marker in table output. Archived notes stay out of the default list without being deleted. `note delete` is a soft delete.

## When to Use Notes vs. Comments

Use notes for project-wide context, recurring reminders, and working memory that multiple issues may need.

Use comments when the information is part of one issue's record, review trail, or implementation discussion:

```bash
td comment td-a1b2 "Validated the migration path on a copy of production data."
td comments td-a1b2
```
