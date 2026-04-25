---
sidebar_position: 9
---

# Notes

Notes are freeform project memory. Use them for durable context that does not belong to one issue: architecture decisions, recurring commands, release observations, meeting notes, or research that several issues may need.

Issue comments stay attached to a specific issue timeline. Notes stand on their own, can be pinned, archived, searched, and shown as JSON for scripts.

## Create Notes

Create a note with inline content:

```bash
td note add "Sync rollout checklist" --content "Verify auth, link project, run td sync --status."
```

If `--content` is omitted, `td` opens your editor:

```bash
td note add "API review notes"
```

## List And Filter

By default, `td note list` shows non-archived notes, sorted with pinned notes first.

```bash
td note list
td note list --pinned
td note list --archived
td note list --all
td note list --search "sync"
td note list --limit 10
```

The list command supports table and JSON output:

```bash
td note list --output json
td note list --json
```

## Show And Edit

Show one note:

```bash
td note show nt-abc123
td note show nt-abc123 --json
```

Edit title, content, or both:

```bash
td note edit nt-abc123 --title "New title"
td note edit nt-abc123 --content "Updated content"
td note edit nt-abc123
```

When no edit flags are passed, `td` opens your editor.

## Pin, Archive, And Delete

Pinned notes sort ahead of normal notes:

```bash
td note pin nt-abc123
td note unpin nt-abc123
```

Archive notes when they should stay searchable but leave the default list:

```bash
td note archive nt-abc123
td note unarchive nt-abc123
```

Delete performs a soft delete:

```bash
td note delete nt-abc123
```

## Notes Vs Comments

Use a note when the information should outlive a single issue or apply across issues.

Use a comment when the information explains one issue's discussion, review, or decision history:

```bash
td comment td-a1b2 "Reviewer asked for an integration test."
td comments td-a1b2
```
