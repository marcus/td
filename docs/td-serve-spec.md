# `td serve` HTTP API Specification

## Purpose

Run `td serve` as the canonical local HTTP boundary for td reads and writes.

Enforce this division of labor:
- Keep agent-heavy workflows in CLI + `td monitor`.
- Execute human backlog/board/detail CRUD through HTTP.

Execute web writes as atomic database mutations with action-log attribution.
Skip CLI pre/post hooks and per-human session identity in v1.

## Command

```bash
td serve [flags]

Flags:
  -p, --port int      Port to listen on (default: auto-assigned)
  -a, --addr string   Address to bind to (default: "localhost")
      --token string  Require bearer token for all requests (optional)
      --cors string   Allowed CORS origin for browser clients (optional)
      --interval dur  Poll interval for change-token checks (default: 2s)
```

Print startup info to stderr:

```text
td serve listening on http://localhost:54321
  base dir:   /Users/marcus/code/td
  database:   /Users/marcus/code/td/.todos/issues.db
  session:    ses_a1b2c3 (web)
  port file:  /Users/marcus/code/td/.todos/serve-port
```

Handle SIGINT/SIGTERM with graceful shutdown:
1. Stop accepting new requests.
2. Drain in-flight requests.
3. Close SSE clients.
4. Delete `.todos/serve-port`.
5. Force close after 10s timeout.

## Lifecycle and Discovery

Run one `td serve` process per td base directory.

### Port file contract

Write `.todos/serve-port` as JSON (not plain text):

```json
{
  "port": 54321,
  "pid": 91234,
  "started_at": "2026-02-27T05:10:11Z",
  "instance_id": "srv_8f3b2c"
}
```

Acquire an exclusive startup lock (`.todos/serve-port.lock`) before writing or replacing the port file.

Treat the file as stale when either condition is true:
- `pid` is not alive, or
- `GET /health` on the recorded port fails.

Replace stale metadata atomically.

### Consumer discovery flow

1. Read `.todos/serve-port`.
2. Parse JSON and validate required keys.
3. Call `GET /health`.
4. Reuse the process when healthy.
5. Start `td serve` when missing/unhealthy/stale.
6. Wait for a healthy port file before proxying traffic.

### Multi-project consumers

Run one process per td project and route by project key.
Do not coordinate multiple projects inside one `td serve` process.

## Session model (web attribution)

Use one service session row per project DB for write attribution.

Select/reuse session using:
- `agent_type = "web"`
- `agent_pid = 0`
- `branch = "default"`
- `name = "td-serve-web"`

Create that session if absent.
Use the service session ID for every write.
Bump `last_activity` on startup and periodically while serving.

## Response envelope

Success:

```json
{ "ok": true, "data": { "...": "..." } }
```

Error:

```json
{
  "ok": false,
  "error": {
    "code": "validation_error",
    "message": "request validation failed",
    "details": {
      "fields": [
        {
          "field": "title",
          "rule": "min_length",
          "value": 2,
          "expected": 3,
          "message": "title must be at least 3 characters"
        }
      ]
    }
  }
}
```

Standard `error.code` values:
- `validation_error` (HTTP 400)
- `not_found` (HTTP 404)
- `conflict` (HTTP 409)
- `unauthorized` (HTTP 401)
- `forbidden` (HTTP 403)
- `internal` (HTTP 500)

## JSON serialization policy

Publish explicit, stable DTOs.

Enforce these rules:
- Always include documented keys.
- Represent unset optional references/timestamps as `null`.
- Represent collections as arrays (`[]` when empty), never `null`.
- Represent freeform text fields as strings (empty string when unset).

For `issue` DTOs:
- Keep `description`, `acceptance`, `sprint` as strings.
- Keep `parent_id`, `implementer_session`, `creator_session`, `reviewer_session`, `created_branch`, `defer_until`, `due_date`, `closed_at`, `deleted_at` as `string | null`.

## Endpoints

### Health

#### `GET /health`

```json
{
  "ok": true,
  "data": {
    "status": "ok",
    "session_id": "ses_a1b2c3",
    "change_token": "1821"
  }
}
```

### Monitor

#### `GET /v1/monitor`

Use this payload as the primary board/list/activity snapshot.

Query params:
- `include_closed=true|false` (default `false`)
- `sort=priority|created|updated` (default `priority`)
- `search=<query>` (default empty)
- `search_mode=auto|text|tdq` (default `auto`)

Search behavior:
- `auto`: try TDQ first, fallback to plain-text search on parse/execute failure.
- `tdq`: require TDQ parse success; return `validation_error` on parse failure.
- `text`: execute plain-text search only.

Response shape:

```json
{
  "ok": true,
  "data": {
    "timestamp": "2026-02-27T04:20:00Z",
    "change_token": "1824",
    "session_id": "ses_a1b2c3",
    "focused_issue": null,
    "in_progress": [],
    "task_list": {
      "reviewable": [],
      "needs_rework": [],
      "in_progress": [],
      "ready": [],
      "pending_review": [],
      "blocked": [],
      "closed": []
    },
    "activity": [],
    "recent_handoffs": [],
    "active_sessions": []
  }
}
```

Monitor DTO contract:
- `activity[]` items expose `timestamp`, `session_id`, `type`, `issue_id`, `issue_title`, `message`, `log_type`, `action`, `entity_id`, `entity_type`, `previous_data`, `new_data`.
- `recent_handoffs[]` items expose `issue_id`, `session_id`, `timestamp`.
- `active_sessions[]` exposes session IDs.
- `focused_issue` exposes full issue DTO or `null`.

Marshal only dedicated snake_case DTOs.
Do not JSON-marshal monitor internal structs directly.

### Issues

#### `GET /v1/issues`

Query params:
- `status` (repeatable)
- `type` (repeatable)
- `priority` (repeatable)
- `search`
- `search_mode=auto|text|tdq` (default `auto`)
- `include_closed=true|false` (default `false`)
- `sort=priority|created|updated|id` (default `priority`)
- `order=asc|desc` (default: `asc` for `priority`/`id`, `desc` for `created`/`updated`)
- `limit` (default `200`, max `1000`)
- `offset` (default `0`)

Response:

```json
{
  "ok": true,
  "data": {
    "issues": [],
    "total": 42,
    "limit": 200,
    "offset": 0,
    "has_more": false
  }
}
```

#### `GET /v1/issues/{id}`

```json
{
  "ok": true,
  "data": {
    "issue": {},
    "logs": [],
    "comments": [],
    "latest_handoff": null,
    "dependencies": [
      {
        "dep_id": "dep_a1b2c3d4",
        "issue_id": "td-abc123",
        "depends_on_id": "td-111",
        "relation_type": "depends_on"
      }
    ],
    "blocked_by": [
      {
        "dep_id": "dep_z9y8x7w6",
        "issue_id": "td-333",
        "depends_on_id": "td-abc123",
        "relation_type": "depends_on"
      }
    ]
  }
}
```

Interpretation:
- `dependencies`: outgoing edges from `{id}` to blockers.
- `blocked_by`: incoming edges from dependents to `{id}`.

#### `POST /v1/issues`

Body:

```json
{
  "title": "Fix auth timeout",
  "type": "bug",
  "priority": "P1",
  "description": "",
  "acceptance": "",
  "points": 3,
  "labels": ["auth", "backend"],
  "parent_id": "td-epic-1",
  "sprint": "sprint-12",
  "minor": false,
  "defer_until": null,
  "due_date": null
}
```

Response:

```json
{ "ok": true, "data": { "issue": {} } }
```

#### `PATCH /v1/issues/{id}`

Apply partial update; keep omitted fields unchanged.

Body fields (all optional):
- `title`
- `description`
- `acceptance`
- `type`
- `priority`
- `points`
- `labels`
- `parent_id`
- `sprint`
- `minor`
- `defer_until` (`YYYY-MM-DD` or `null`)
- `due_date` (`YYYY-MM-DD` or `null`)

Response:

```json
{ "ok": true, "data": { "issue": {} } }
```

#### `DELETE /v1/issues/{id}`

Perform soft delete.

Response:

```json
{ "ok": true, "data": { "deleted": true } }
```

### Status transitions

Validate the current status for every transition endpoint.
Return `409 conflict` for invalid transitions.
Skip reviewer/self-close/session-history guardrails in v1.

Apply cascades in v1:
- On `review`: run parent cascade to `in_review` when all siblings qualify.
- On `approve` and `close`: run parent cascade to `closed` and dependency unblocking cascade.

Transition matrix:

| Endpoint | Valid From | To | Direct side effects |
|---|---|---|---|
| `POST /v1/issues/{id}/start` | `open` | `in_progress` | `implementer_session = web_session_id` |
| `POST /v1/issues/{id}/review` | `open`, `in_progress` | `in_review` | if `implementer_session` is null, set to web session |
| `POST /v1/issues/{id}/approve` | `in_review` | `closed` | `reviewer_session = web_session_id`, `closed_at = now` |
| `POST /v1/issues/{id}/reject` | `in_review` | `open` | `implementer_session = null`, `reviewer_session = null`, `closed_at = null` |
| `POST /v1/issues/{id}/block` | `open`, `in_progress` | `blocked` | none |
| `POST /v1/issues/{id}/unblock` | `blocked` | `open` | none |
| `POST /v1/issues/{id}/close` | `open`, `in_progress`, `blocked`, `in_review` | `closed` | `closed_at = now` |
| `POST /v1/issues/{id}/reopen` | `closed` | `open` | `reviewer_session = null`, `closed_at = null` |

Optional request body:

```json
{ "reason": "short note" }
```

Append `reason` as a progress log entry when provided.

### Review attribution on `approve`

`POST /v1/issues/{id}/approve` additionally accepts:

```json
{ "reviewed_by": "code-reviewer sub-agent", "self_review": true, "reason": "..." }
```

- `reviewed_by` names who actually performed the review when that is not the
  session recording it. Under `review_policy_mode=trusted` (the default) it lets
  an implementation-involved session approve without claiming a self-review it
  did not perform. It requires no `reason`.
- `self_review` acknowledges reviewing your own work. It requires a `reason`.
- The two are mutually exclusive (400). `reviewed_by` is rejected when blank,
  over 120 runes, or containing newlines/control characters, and is rejected
  entirely on the close-after-recorded-approval path, where no new review row is
  written.
- Neither field grants anything outside trusted mode. In `delegated` and
  `strict`, `reviewed_by` is recorded as metadata and an involved session is
  still refused.
- Both are stamped on the `issue_reviews` row from the POLICY DECISION, never
  from the request body, so a request cannot forge either field.

`POST /v1/issues/{id}/reviews` (record-only) accepts the same two fields.

Review rows expose `self_review` and `reviewed_by` on both `active_review` and
`GET /v1/issues/{id}?with=reviews`. Read them together: `self_review` means the
row was recorded by an implementation-involved session, which is a literal
self-review only when `reviewed_by` is empty.

Return transition response with cascade details:

```json
{
  "ok": true,
  "data": {
    "issue": {},
    "cascades": {
      "parent_status_updates": ["td-epic-1"],
      "auto_unblocked": ["td-xyz789"]
    }
  }
}
```

### Comments

#### `POST /v1/issues/{id}/comments`

Body:

```json
{ "text": "Needs a test for token refresh edge case." }
```

Response:

```json
{
  "ok": true,
  "data": {
    "comment": {
      "id": "cmt_123",
      "issue_id": "td-abc123",
      "session_id": "ses_a1b2c3",
      "text": "...",
      "created_at": "..."
    }
  }
}
```

#### `DELETE /v1/issues/{id}/comments/{comment_id}`

Enforce delete semantics:
- Match both `issue_id` and `comment_id`.
- Return `404 not_found` when no matching row exists.
- Hard-delete the comment row.
- Write action log row:
  - `action_type = "delete"`
  - `entity_type = "comments"`
  - `entity_id = {comment_id}`
  - `previous_data = full deleted comment JSON`
  - `new_data = ""`

Response:

```json
{ "ok": true, "data": { "deleted": true } }
```

### Dependencies

#### `POST /v1/issues/{id}/dependencies`

Body:

```json
{ "depends_on": "td-222" }
```

Response:

```json
{
  "ok": true,
  "data": {
    "dependency": {
      "dep_id": "dep_a1b2c3d4",
      "issue_id": "td-abc123",
      "depends_on_id": "td-222",
      "relation_type": "depends_on"
    }
  }
}
```

#### `DELETE /v1/issues/{id}/dependencies/{dep_id}`

Use `dep_id` as the canonical dependency delete key in v1.
Resolve `dep_id` to the underlying relation and ensure it belongs to `{id}`.
Return `404 not_found` when the relation does not exist or belongs to a different issue.

Response:

```json
{ "ok": true, "data": { "removed": true } }
```

### Focus

#### `PUT /v1/focus`

Set or clear focused issue in `.todos/config.json`.

Body:

```json
{ "issue_id": "td-abc123" }
```

or

```json
{ "issue_id": null }
```

Response:

```json
{ "ok": true, "data": { "focused_issue_id": "td-abc123" } }
```

Do not trigger sync for `/v1/focus`.

### Boards

#### `GET /v1/boards`

```json
{ "ok": true, "data": { "boards": [] } }
```

#### `GET /v1/boards/{id}`

Query params:
- `include_closed=true|false` (default `false`)

Response:

```json
{ "ok": true, "data": { "board": {}, "issues": [] } }
```

Board issue resolution contract:
1. Start with all issues (or filtered closed/open set).
2. When `board.query` is non-empty, execute TDQ first.
3. Apply board position overlays to reorder visible issues only.

Session-awareness policy for board queries:
- Do not let board queries depend on the service session identity.
- Normalize session-aware clauses (for example `@me`) to neutral no-op behavior before execution.
- Keep board membership deterministic across users and processes.

#### `POST /v1/boards`

Body:

```json
{ "name": "Sprint 12", "query": "sprint:sprint-12" }
```

Response:

```json
{ "ok": true, "data": { "board": {} } }
```

#### `PATCH /v1/boards/{id}`

Body (partial):

```json
{ "name": "Renamed board", "query": "type:bug priority<=P1" }
```

Response:

```json
{ "ok": true, "data": { "board": {} } }
```

#### `DELETE /v1/boards/{id}`

Response:

```json
{ "ok": true, "data": { "deleted": true } }
```

#### `POST /v1/boards/{id}/issues`

Set explicit board position overlay.

Body:

```json
{ "issue_id": "td-abc123", "position": 0 }
```

Response:

```json
{ "ok": true, "data": { "positioned": true } }
```

#### `DELETE /v1/boards/{id}/issues/{issue_id}`

Remove explicit board position overlay.

Response:

```json
{ "ok": true, "data": { "removed": true } }
```

### Sessions

#### `GET /v1/sessions`

```json
{
  "ok": true,
  "data": {
    "sessions": [
      {
        "id": "ses_a1b2c3",
        "name": "td-serve-web",
        "branch": "default",
        "agent_type": "web",
        "started_at": "2026-02-27T03:00:00Z",
        "last_activity": "2026-02-27T04:10:00Z"
      }
    ],
    "current_session_id": "ses_a1b2c3"
  }
}
```

### Stats

#### `GET /v1/stats`

```json
{
  "ok": true,
  "data": {
    "total": 142,
    "by_status": { "open": 34, "in_progress": 5, "blocked": 3, "in_review": 2, "closed": 98 },
    "by_type": { "bug": 12, "feature": 45, "task": 60, "epic": 8, "chore": 17 },
    "by_priority": { "P0": 2, "P1": 8, "P2": 55, "P3": 40, "P4": 37 },
    "created_today": 3,
    "created_this_week": 11,
    "total_points": 287,
    "completion_rate": 0.69,
    "total_logs": 534,
    "total_handoffs": 89
  }
}
```

### Real-time events (SSE)

#### `GET /v1/events`

Define change token as:
- `change_token = CAST(COALESCE(MAX(rowid), 0) AS TEXT) FROM action_log`

Emit SSE events with IDs:

```text
id: 1824
event: refresh
data: {"change_token":"1824","timestamp":"2026-02-27T04:20:07Z"}

id: 1824
event: ping
data: {"change_token":"1824"}
```

Behavior:
- Poll token every `--interval`.
- Emit `ping` every 30s.
- Broadcast `refresh` immediately after successful writes.

Reconnect behavior:
- Accept `Last-Event-ID`.
- Compare `Last-Event-ID` to current `change_token` on connect.
- Emit immediate `refresh` when current token is newer.
- Keep normal exponential backoff client policy (start 1s, cap 10s).

## Validation rules (HTTP 400)

### Issue create/update

- `title`:
  - default minimum length: **3**
  - default maximum length: **200**
  - keep configurability via `title_min_length` and `title_max_length`
- `type`: one of `bug|feature|task|epic|chore` (`story` alias to `feature`)
- `priority`: one of `P0|P1|P2|P3|P4` (numeric aliases `0..4` accepted)
- `points`: one of `1,2,3,5,8,13,21`
- `defer_until` / `due_date`: parseable date, stored as `YYYY-MM-DD`
- `parent_id` when provided: must reference an existing issue

### Query validation

- Board `query` must parse as TDQ; return `validation_error` on parse failure.
- `search_mode=tdq` must parse as TDQ; return `validation_error` on parse failure.
- `search_mode=auto` must fallback to plain-text search on parse/execute failure.

### Pagination

- `limit` must be integer in `[1, 1000]`.
- `offset` must be integer `>= 0`.

## Authentication

Run v1 in localhost tokenless mode by default.

Support future hardening with:
- `--token` for bearer auth on all endpoints.
- `--cors` for explicit browser origins.

Keep Perch v1 on same-origin localhost proxy without bearer injection.

## Sync

Trigger the same autosync mutation path used by CLI after successful writes (feature-gated, debounced).

Trigger sync for:
- Issues CRUD
- Status transitions
- Comments create/delete
- Dependencies add/remove
- Boards CRUD
- Board issue position set/remove

Do not trigger sync for:
- Health
- Monitor/reads
- Focus

## Future extension: undo

Keep v1 on standard error recovery.
Do not require undo in v1 clients.

Reserve future endpoints:
- `POST /v1/undo`
- session-scoped undo list/read APIs

## Implementation notes (`td` repo)

Create these files:

```text
cmd/serve.go
internal/serve/server.go
internal/serve/handlers_read.go
internal/serve/handlers_write.go
internal/serve/sse.go
internal/serve/response.go
internal/serve/session.go
internal/serve/portfile.go
```

Enforce these constraints:
- Build explicit serve DTOs with stable snake_case JSON.
- Call logged DB mutation methods for sync-compatible action log writes.
- Implement comment deletion path with action-log delete semantics.
- Use `dep_id` as the canonical dependency identifier for delete routes.
- Run board query resolution before `ApplyBoardPositions`.
- Use action-log rowid token for SSE change tracking.
- Keep focus handling local/config-based.
- Enforce startup lock and structured port-file lifecycle.
