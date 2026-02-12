# td Primer

A beginner-friendly guide to how td works, where it stores data, and how to think about organizing your projects.

## What is td?

td is a command-line task manager designed for AI-assisted development. When an AI coding agent (Claude Code, Cursor, Copilot, etc.) finishes a session, it has no memory of what happened. td solves this by providing structured handoffs — the outgoing session records what's done, what remains, and what decisions were made. The next session picks up from accurate state instead of guessing.

## Where td Stores Data

All td data lives in a `.todos/` directory at the root of your project:

```
your-project/
├── .todos/
│   ├── issues.db      # SQLite database (issues, logs, handoffs, sessions, boards, etc.)
│   └── config.json    # Monitor UI settings (pane heights, filters)
├── src/
├── package.json
└── ...
```

There is no external server, no account, and no cloud dependency for basic usage. The database is a single SQLite file. You initialize it with `td init` and everything stays local.

## Core Concepts

### Projects

A "project" in td is simply a directory where you ran `td init`. There is no explicit project model or configuration — the `.todos/` directory *is* your project. When you run any `td` command, it walks up from your current directory looking for a `.todos/` folder (or a git root that contains one).

### Issues

Issues are the central object in td. Every task, bug, feature, or piece of work is an issue.

Each issue has:
- **ID** — a short identifier like `td-a1b2`
- **Title** — what needs to be done
- **Status** — where it is in the workflow (see below)
- **Type** — `bug`, `feature`, `task`, `chore`, or `epic`
- **Priority** — `P0` (critical) through `P4` (none), defaulting to `P2`
- **Labels** — freeform tags for categorization

### Issue Lifecycle

Issues move through a state machine:

```
open → in_progress → in_review → closed
             |
             v
          blocked
```

- `open` — created but not started
- `in_progress` — someone (or some agent) is actively working on it
- `blocked` — work is stuck on something
- `in_review` — work is done, waiting for a different session to review
- `closed` — reviewed and approved

An important rule: the session that implemented an issue cannot approve it. A different session must review the work. This prevents an AI agent from silently approving its own output.

### Sessions

A session represents a single agent or terminal context. Sessions are created automatically — each terminal window or AI agent context gets a unique ID like `claude-7f3a`.

Sessions matter because td tracks *who* did *what*. The implementer session, creator session, and reviewer session are all recorded on each issue. This is what enforces the rule that you can't review your own work.

### Epics

Epics are just issues with `type = epic`. They act as parents — you link child issues to an epic using `td tree add-child <epic-id> <child-id>`. This creates a tree you can visualize with `td tree <epic-id>`.

### Boards

Boards are saved views into your issues, defined by a query. For example:

```bash
td board create "Sprint 1" --query "priority <= P1"
td board create "Bug Triage" --query "type = bug AND status = open"
```

Boards don't *contain* issues — they filter them dynamically. You can also manually position issues within a board for priority ordering.

### Handoffs

Handoffs are structured snapshots that capture the state of work when a session ends:

```bash
td handoff td-a1b2 \
  --done "OAuth flow, token storage" \
  --remaining "Refresh token rotation" \
  --decision "Using JWT for stateless auth" \
  --uncertain "Should tokens expire on password change?"
```

The next session runs `td usage` and sees exactly where things stand — no guessing required.

### Logs

Logs are freeform notes attached to an issue during work. They come in several types:

- **progress** — general updates (`td log "endpoint returning correct data"`)
- **decision** — architectural choices (`td log --decision "using Redis for caching"`)
- **blocker** — things you're stuck on (`td log --blocker "API rate limit unclear"`)

### Dependencies

Issues can depend on other issues:

```bash
td dep add td-abc td-xyz   # td-abc depends on td-xyz (td-abc is blocked until td-xyz is done)
```

The `td critical-path` command analyzes the dependency graph and tells you the optimal order to work on things — prioritizing tasks that unblock the most downstream work.

## Syncing Across Machines

By default, td is entirely local. But if you want multiple computers to share task state, td includes a sync server.

### How Sync Works

Each machine keeps its own local `.todos/issues.db` and syncs via an event-based protocol:

1. Every mutation (create, update, delete) is recorded in an `action_log` table
2. The sync engine pushes new events to the server and pulls events from other clients
3. Conflicts are resolved using Last-Write-Wins (LWW)
4. New clients can bootstrap from a snapshot instead of replaying the entire event history

### Setting Up Sync

You need a server running somewhere accessible (a VPS, EC2 instance, homelab machine, etc.):

```bash
# On the server: run the sync server
td-sync serve

# On each client: authenticate and link
td auth login
td sync-project create "my-project"   # first client creates the project
td sync-project join "my-project"     # other clients join it
td sync                                # push/pull events
```

The `deploy/` directory in the td repo contains Docker and docker-compose configurations for running the sync server.

### Remote Access

The sync server exposes a REST API with authentication, CORS support, and rate limiting. While td itself is a CLI/TUI with no web interface, the API could be used to build a mobile-friendly web app or integrate with other tools.

## Project Organization

### One Repo = One td Project

td ties a project to a single git repository. When you run `td init` in a repo, that repo gets its own `.todos/` database. This means:

- All tasks, boards, sessions, and handoffs are scoped to that one repo
- Running `td` commands automatically finds the right database by walking up to the git root
- There is no built-in way to see tasks across multiple repos in one view

### Multi-Component Products

If your product spans multiple repositories (e.g., an API repo, a UI library repo, and a frontend app repo), you have a few options:

**Monorepo (simplest)** — Put everything in one repo. One `.todos/` database manages all tasks. Use labels to organize by component:

```bash
td create "Add auth endpoint" --labels api
td create "Button component" --labels ui-library
td create "Login page" --labels frontend
td board create "API Work" --query "labels ~ api"
td board create "UI Library" --query "labels ~ ui-library"
```

**Separate repos with shared database** — Use the `.td-root` mechanism to point multiple repos at the same database. Create a file named `.td-root` in each repo root containing the path to a shared location:

```bash
# Create a shared location
mkdir -p ~/Source/MyProduct/.todos-shared
cd ~/Source/MyProduct/.todos-shared && td init

# Point each repo at it
echo "$HOME/Source/MyProduct/.todos-shared" > ~/Source/MyProduct/api/.td-root
echo "$HOME/Source/MyProduct/.todos-shared" > ~/Source/MyProduct/ui-lib/.td-root
echo "$HOME/Source/MyProduct/.todos-shared" > ~/Source/MyProduct/frontend/.td-root
```

Now running `td` in any of those repos hits the same database. Note: `.td-root` was designed for git worktrees, so this is a workaround rather than an officially designed workflow.

**Separate repos, separate databases** — Each repo manages its own tasks independently. This is the simplest setup but provides no cross-repo visibility. With sync enabled, all the data ends up on the server, but there is no built-in cross-project query tool.

### Recommendation

If your components are tightly coupled and worked on by the same team or agents, a monorepo with label-based boards is the path of least resistance. If the repos genuinely represent independent products with separate release cycles, keeping them as separate td projects is fine — just know that each one is its own island.

## Quick Start

```bash
# Initialize td in your project
cd /path/to/your/project
td init

# Create your first issue
td create "Add user authentication" --type feature --priority P1

# See what to work on
td usage

# Start work on an issue
td start td-a1b2

# Log progress as you go
td log "OAuth callback implemented"

# Hand off when done
td handoff td-a1b2 --done "OAuth flow" --remaining "Token refresh"

# Submit for review
td review td-a1b2

# Open the live dashboard in a separate terminal
td monitor
```

## Further Reading

- [Getting Started](https://marcus.github.io/td/docs/intro) — full onboarding guide
- [Core Workflow](https://marcus.github.io/td/docs/core-workflow) — detailed workflow documentation
- [Boards](https://marcus.github.io/td/docs/boards) — organizing work with query-based boards
- [Dependencies & Critical Path](https://marcus.github.io/td/docs/dependencies) — modeling and visualizing dependencies
- [TDQ Query Language](https://marcus.github.io/td/docs/query-language) — powerful issue filtering
- [Live Monitor](https://marcus.github.io/td/docs/monitor) — TUI dashboard
