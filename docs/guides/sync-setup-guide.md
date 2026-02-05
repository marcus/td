# Sync Setup Guide

How to get sync running for the first time — whether you're creating a new team project or joining an existing one.

## Quickest Path

### From the monitor (recommended)

Open `td monitor` in any directory. If it's a new project, the init modal creates your local database. If you're already authenticated to a sync server, a follow-up prompt asks whether to link this project:

- **Pick an existing remote project** from the list to sync with your team
- **Create a new project** if you're starting fresh
- **Skip** to set up sync later

That's it — one flow, no commands to remember.

### From the CLI

```bash
td sync init
```

This walks through each step interactively:

1. **Server URL** — shows the current server, lets you change it
2. **Authentication** — checks if you're logged in, tells you to run `td auth login` if not
3. **Project** — create a new remote project or pick from your existing ones
4. **Done** — prints a summary of what's configured

## Step-by-Step (manual)

If you prefer explicit commands over the guided flow:

### 1. Set the server URL

```bash
td config set sync.url https://sync.example.com
```

Or set the environment variable:
```bash
export TD_SYNC_URL=https://sync.example.com
```

Skip this step if running locally — the default is `http://localhost:8080`.

### 2. Authenticate

```bash
td auth login
```

Enter your email. A verification URL and 6-character code are displayed. Open the URL in a browser, enter the code, and the CLI saves your credentials.

Check auth status anytime:
```bash
td auth status
```

### 3. Create or join a project

**Creating a new project** (for project owners):

```bash
td sync-project create "my-project"
# ✓ Created and linked to project my-project (a1b2c3d4-...)
```

This creates the remote project and links your local database in one step.

**Joining an existing project** (for teammates):

```bash
td sync-project join
```

With no arguments, this lists all projects you've been invited to and lets you pick one:

```
Available projects:
  1) backend-api (a1b2c3d4-...)
  2) frontend-v2 (e5f6a7b8-...)
Select project number: 1
✓ Linked to project backend-api (a1b2c3d4-...)
```

You can also join by name directly:
```bash
td sync-project join "backend-api"
# ✓ Linked to project backend-api (a1b2c3d4-...)
```

### 4. Sync

```bash
td sync
```

Your local changes push to the server, and remote changes from teammates pull down.

## For Project Owners: Inviting Teammates

After creating a project, invite others by email:

```bash
td sync-project invite alice@example.com         # defaults to writer
td sync-project invite bob@example.com reader     # read-only
```

The invited user then authenticates (`td auth login`) and joins (`td sync-project join`).

See the [collaboration guide](collaboration.md) for roles, permissions, and member management.

## Auto-Sync

Once linked, you can enable automatic push/pull so you never have to run `td sync` manually:

```bash
td config set sync.auto.enabled true
```

With auto-sync enabled:
- **On startup**: push+pull runs when any `td` command starts
- **After mutations**: push+pull runs after commands that change data (debounced to 3s)

All auto-sync operations are silent and use a 5-second timeout.

Other auto-sync settings:
```bash
td config set sync.auto.debounce 5s        # wait between post-mutation syncs
td config set sync.auto.interval 5m         # periodic sync interval
td config set sync.auto.on_start true       # sync on command startup
td config set sync.auto.pull true           # include pull (false = push-only)
```

## Config Reference

View current config:
```bash
td config list                     # all settings as JSON
td config get sync.url             # single value
```

Set values:
```bash
td config set sync.url https://sync.example.com
td config set sync.auto.enabled true
td config set sync.snapshot_threshold 500
```

## What Happens When the Server Is Down

- **During monitor init prompt**: the sync prompt is silently skipped. No error shown.
- **During `td sync init`**: the wizard reports the error and exits. Re-run when the server is back.
- **During `td sync`**: the push or pull fails with an error message. Your local data is unaffected.
- **During auto-sync**: failures are logged at debug level. No user-visible error.

Your local database is always the source of truth. Sync is additive — a failed sync never corrupts or loses local data.

## Troubleshooting

**"not logged in (run: td auth login)"**
Your credentials are missing or expired. Run `td auth login` again.

**"project not linked"**
Your local project isn't connected to a remote. Run `td sync-project join` or `td sync-project link <id>`.

**"no projects found"**
You haven't been invited to any remote projects yet. Ask the project owner to run `td sync-project invite your@email.com`.

**"unauthorized"**
Your API key is expired or revoked. Run `td auth login` to get a new one.

## Related Docs

- [Collaboration guide](collaboration.md) — roles, permissions, conflict resolution, member management
- [Sync client guide](../sync-client-guide.md) — detailed client reference (all flags, env vars, internals)
- [Sync server ops guide](../sync-server-ops-guide.md) — running and deploying the server
