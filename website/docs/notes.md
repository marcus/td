---
sidebar_position: 8
---

# Notes

`td note` stores freeform project memory that is useful across sessions but does not need an issue lifecycle. Use notes for standing context, decision records, operating reminders, and background material that agents should be able to find later.

Use issues when work needs status, ownership, review, dependencies, or a handoff. Use notes when the information is durable context rather than a task to complete.

## Create Notes

```bash
td note add "Architecture decisions" --content "Use SQLite WAL for local writes."
td note add "Research links"
```

If `--content` is omitted, td opens your editor for the note body.

## Find Notes

```bash
td note list
td note list --search "sync"
td note list --pinned
td note list --archived
td note list --all
td note list --json
```

By default, `td note list` shows non-archived notes. Use `--all` to include archived notes or `--archived` to show only archived notes.

## Read And Edit

```bash
td note show nt-a1b2c3
td note edit nt-a1b2c3 --title "Sync setup notes"
td note edit nt-a1b2c3 --content "Updated operating notes."
td note edit nt-a1b2c3
```

Running `td note edit <id>` without `--title` or `--content` opens the note in your editor.

## Pinning

Pinned notes stay easy to discover for agents and operators.

```bash
td note pin nt-a1b2c3
td note list --pinned
td note unpin nt-a1b2c3
```

Pin notes that should remain visible during routine orientation, such as project conventions, local setup warnings, or active architectural decisions.

## Archive And Delete

Archive notes that should remain searchable but no longer belong in the default list.

```bash
td note archive nt-a1b2c3
td note unarchive nt-a1b2c3
td note delete nt-a1b2c3
```

`td note delete` soft-deletes the note. Prefer archive for old-but-useful context; delete for duplicates, mistakes, or content that should no longer be part of project memory.

## Agent Guidance

Agents should use notes for information that helps future sessions but does not need review:

- Stable project facts: repository layout, local services, or deployment caveats.
- Decisions that explain why a path was chosen.
- Research summaries that may inform later issues.
- Operator reminders that should survive context windows.

Do not use notes as a substitute for `td log`, `td handoff`, or `td review`. Progress on a specific issue belongs on that issue so reviewers and future implementers can see the full chain of work.
