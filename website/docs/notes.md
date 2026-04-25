---
sidebar_position: 9
---

# Notes

Notes are project-scoped, freeform markdown records for information that should live near the work but is not itself an issue. Use them for decisions, investigation scratchpads, meeting notes, release prep, or durable context that multiple future sessions may need.

Notes are separate from issue comments:

| Use | Best command |
|-----|--------------|
| Progress or discussion tied to one issue | `td comment <issue-id> "text"` |
| Durable context that applies across issues or the whole project | `td note ...` |
| Agent handoff for active implementation | `td handoff <issue-id> ...` |

## Create Notes

```bash
td note add "Release checklist" --content "Verify docs build before tagging."
td note add "Architecture decisions"
```

When `--content` is omitted, td opens `$EDITOR` for note content. If `$EDITOR` is unset, it falls back to `vi`.

## List and Filter

```bash
td note list
td note list --pinned
td note list --archived
td note list --all
td note list --search "sync"
td note list --limit 10
```

By default, `td note list` shows non-archived notes. Pinned notes are useful for context that should remain easy to find during repeated agent sessions.

For scripts, use JSON output:

```bash
td note list --json
td note list --output json
```

## Show Notes

```bash
td note show nt-abc123
td note show nt-abc123 --json
```

Use `--json` when another tool needs stable fields rather than terminal formatting.

## Edit Notes

```bash
td note edit nt-abc123 --title "Updated release checklist"
td note edit nt-abc123 --content "Updated content"
td note edit nt-abc123
```

When neither `--title` nor `--content` is provided, td opens `$EDITOR` with the current content.

## Pin, Archive, and Delete

```bash
td note pin nt-abc123
td note unpin nt-abc123
td note archive nt-abc123
td note unarchive nt-abc123
td note delete nt-abc123
```

Archive notes that are still useful historically but no longer belong in the default list. Delete notes that should be hidden from normal note workflows.
