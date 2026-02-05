# Multi-User Collaboration Guide

How to share td projects with team members and handle concurrent edits.

## Getting Started

### Prerequisites

1. Authenticate with the sync server:
```bash
td auth login
```

For self-hosted servers:
```bash
td auth login --server https://your-server:8080
```

2. Create a remote project:
```bash
td sync-project create "Team Project" --description "Shared task tracking"
# Created project Team Project (abc-123-def)
```

3. Link your local project to the remote:
```bash
td sync-project link abc-123-def
# Linked to project abc-123-def
```

## Inviting Collaborators

### Add Members

Invite users by email:
```bash
td sync-project invite alice@example.com writer
# Invited alice@example.com as writer (user xyz-789)
```

Role defaults to `writer` if omitted:
```bash
td sync-project invite bob@example.com
# Invited bob@example.com as writer (user uvw-456)
```

### Roles and Permissions

| Role | Permissions |
|------|-------------|
| **owner** | All operations: read, write, invite, change roles, remove members |
| **writer** | Read and write issues, logs, handoffs, boards |
| **reader** | Read-only access to all project data |

Owners can:
- Create and delete issues
- Modify project structure
- Manage team members
- Change member roles

Writers can:
- Create, update, and close issues
- Add logs and comments
- Modify existing data

Readers can:
- View issues and project data
- Pull remote changes
- Cannot push local edits

## Managing Members

### List Current Members

```bash
td sync-project members
# USER ID                              ROLE       ADDED
# abc-123-def                          owner      2026-01-30T10:00:00Z
# xyz-789                              writer     2026-01-30T11:30:00Z
```

### Change a Member's Role

```bash
td sync-project role xyz-789 owner
# Updated xyz-789 to owner
```

### Remove a Member

```bash
td sync-project kick uvw-456
# Removed member uvw-456
```

## Syncing Changes

### Push and Pull

Push local changes to the server:
```bash
td sync --push
# Pushed 3 events.
```

Pull remote changes from other team members:
```bash
td sync --pull
# Pulled 5 events (5 applied).
```

Sync both directions (default):
```bash
td sync
# Pushed 3 events.
# Pulled 5 events (5 applied).
```

### Sync Status

Check sync state and pending changes:
```bash
td sync --status
# Project:     abc-123-def
# Last pushed: action 42
# Last pulled: seq 128
# Pending:     0 events
# Last sync:   2026-01-30T12:00:00Z
#
# Server:
#   Events:    128
#   Last seq:  128
#   Last event: 2026-01-30T12:00:00Z
```

## Conflict Behavior

td-sync uses **last-write-wins** conflict resolution. When multiple users edit the same entity concurrently:

1. Each user's edits are recorded as events with timestamps
2. During sync, the server applies events in timestamp order
3. The most recent edit overwrites earlier edits
4. The previous state is preserved in the conflict log

### What Happens on Conflict

When Alice and Bob both edit issue #42:

1. Alice changes title to "Fix bug" (timestamp: 12:00:00)
2. Bob changes title to "Resolve crash" (timestamp: 12:00:05)
3. Both sync their changes
4. Server applies both events in timestamp order
5. Final title: "Resolve crash" (Bob's edit is newer)
6. Alice's edit is preserved in the conflict log

### Viewing Conflicts

See recent overwrites:
```bash
td sync conflicts
# Recent sync conflicts:
#   TIME                  TYPE      ENTITY     SEQ
#   2026-01-30 12:00:05   issues    42         129
#   2026-01-30 11:45:12   issues    18         127
```

Limit results:
```bash
td sync conflicts --limit 10
```

Show conflicts from the last 24 hours:
```bash
td sync conflicts --since 24h
```

The conflict log stores:
- Entity type and ID
- Server sequence number
- Previous (local) data
- New (remote) data
- Overwrite timestamp

Use this to manually review and restore overwrites if needed.

## Self-Hosting for Teams

Run your own sync server with Docker:

```bash
# Clone the repository
git clone https://github.com/marcus/td
cd td/server

# Configure environment
cp .env.example .env
# Edit .env: set AUTH_SECRET, database paths, Litestream backup credentials

# Start server + Litestream backup
docker-compose up -d
```

Team members authenticate against your server:
```bash
td auth login --server https://your-server:8080
```

All functionality is identical to the hosted service. See [sync-plan-03-merged.md](../sync-plan-03-merged.md) for deployment details.
