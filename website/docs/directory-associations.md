---
sidebar_position: 9
---

# Directory Associations

Directory associations let td treat one directory as part of another td project. This is useful for sibling worktrees, nested checkouts, generated sidecar repos, or tools that run outside the directory containing `.todos`.

Associations are stored in `~/.config/td/associations.json`.

## Link The Current Directory

From the directory you want to associate:

```bash
cd /Users/alex/code/project-worktree
td config associate /Users/alex/code/project
```

With one argument, `td config associate` uses the current directory as the source and the argument as the target td project.

## Link A Specific Directory

```bash
td config associate /Users/alex/code/project-feature /Users/alex/code/project
```

After this, td commands run from `/Users/alex/code/project-feature` resolve to the canonical td project at `/Users/alex/code/project`.

## List Associations

```bash
td config associations
td config assoc
```

`assoc` is an alias for `associations`.

## Remove An Association

```bash
td config dissociate
td config dissociate /Users/alex/code/project-feature
```

With no argument, `td config dissociate` removes the association for the current directory.

## When To Use Associations

Use associations when multiple directories should share one task database:

- A feature worktree should use the main repo's `.todos` database.
- A nested app directory should resolve to the parent project.
- A generated or temporary checkout should report work against the canonical project.

Do not use associations to merge unrelated projects. Each real project should keep its own `.todos` database unless the directories are intentionally part of the same work stream.
