td Cloud Sync Server — Product & Technical Spec (v1)

1. Goals

Provide a self-hostable cloud service that enables automatic, low-friction synchronization of many small local td SQLite databases (“projects”) across multiple devices and (optionally) multiple users, while keeping the local SQLite file as the primary working copy.

The service must:
	•	Require near-zero configuration for end users once authenticated.
	•	Support offline-first behavior: local writes always succeed; sync is best-effort.
	•	Scale to many small projects per user with small teams (1–10 contributors per project).
	•	Be simple to operate by a solo maintainer on commodity infrastructure.
	•	Serve as the foundation for collaboration (invites, contributor roles) and published read-only views.

Non-goals:
	•	Replacing local SQLite with a remote database.
	•	Real-time co-editing guarantees beyond eventual consistency.
	•	Strong global serializability across concurrent writers.

2. Terminology
	•	Client: td CLI/TUI running on a device.
	•	Server: Cloud service providing auth, authorization, and sync APIs.
	•	Project: A single td database instance (a local SQLite file) representing a task space.
	•	Event: An immutable change record representing a mutation to project state.
	•	Action Log: Client-side append-only table that records events.
	•	Device: A specific client installation instance.
	•	Session: A sequence of operations performed by a device (used for idempotency).
	•	Sequence (server_seq): Monotonic per-project ordering assigned by the server.

3. Product Requirements

3.1 User experience
	•	If TD_AUTH_KEY is present (or an equivalent stored credential is available), td automatically syncs in the background.
	•	If no credential is present, td operates normally with local-only behavior.
	•	If credential is invalid/expired, td prompts the user to re-authenticate via an in-app flow.
	•	Sync is opt-out per project and globally.

3.2 Data model assumptions
	•	The local SQLite database contains an append-only event stream (the action log) sufficient to reconstruct state.
	•	Local state remains authoritative for user interactions; the server relays and stores events.

3.3 Collaboration
	•	Users can invite other users as contributors (writer) or readers to a project.
	•	Contributors can push events; readers can only pull.
	•	A project has an owner who can manage membership and keys.

3.4 Publishing
	•	A project can be published as a read-only view.
	•	Published views can be public or private.
	•	Private published views require authentication and authorization.

4. Architecture Overview

4.1 High-level design
	•	Client persists all state in local SQLite.
	•	Client appends an event to action_log for each mutation.
	•	Server stores events in an append-only log per project, assigning a monotonic server_seq.
	•	Client syncs using:
	•	Push: send unsynced local events.
	•	Pull: fetch remote events since the last received server_seq.
	•	Client applies remote events to local state deterministically.

4.2 Consistency model
	•	Eventual consistency per project.
	•	Per-project total order is defined by server_seq.
	•	Conflicts are allowed and must be surfaced/recorded without blocking sync.

5. Client Specification

5.1 Identity
	•	device_id: stable UUID generated on first run, stored locally.
	•	session_id: UUID generated per process start or per sync cycle.
	•	project_id: stable UUID stored in the local DB.

5.2 Required local tables

The local database MUST contain:
	•	action_log (append-only events), including:
	•	id (monotonic local id)
	•	timestamp
	•	action_type
	•	entity_type
	•	entity_id
	•	previous_data (JSON)
	•	new_data (JSON)

The local database MUST add:
	•	sync_state (one row per project):
	•	project_id (PK)
	•	last_pushed_action_id (integer)
	•	last_pulled_server_seq (bigint)
	•	last_sync_at (timestamp)
	•	sync_disabled (bool)
	•	conflict_count (integer)

Optional:
	•	conflicts table to record conflicts with enough detail for UI.

5.3 Event generation
	•	Every local mutation MUST produce exactly one event in action_log.
	•	Events MUST be appended within the same transaction as the mutation.
	•	Events MUST include enough data to apply the change on another replica.

5.4 Sync triggers
	•	On startup: attempt sync for all projects if authenticated.
	•	On a debounce after local writes.
	•	Periodically (e.g., 30–120s jittered) while td is running.
	•	Manual command: td sync.

5.5 Push protocol

Client sends events where action_log.id > last_pushed_action_id.

Client MUST include:
	•	project_id
	•	device_id
	•	session_id
	•	client_action_id (the local action_log.id)
	•	event_payload (JSON)
	•	event_timestamp

Client MUST retry transient failures with exponential backoff.

5.6 Pull protocol

Client requests:
	•	project_id
	•	after_server_seq = last_pulled_server_seq
	•	limit

Client applies returned events in ascending server_seq.

5.7 Applying remote events
	•	Remote event application MUST be deterministic and idempotent.
	•	Client MUST store the highest applied server_seq in sync_state.
	•	Client MUST ignore events already applied.

5.8 Conflict handling

The client MUST provide a non-blocking conflict strategy:
	•	If a remote event cannot be cleanly applied due to version mismatch or missing preconditions, the client:
	•	records a conflict entry with relevant details
	•	applies a deterministic fallback rule (e.g., last-write-wins by event timestamp or server_seq)
	•	continues syncing

The conflict record MUST include:
	•	server_seq
	•	entity_type, entity_id
	•	local snapshot of current entity
	•	remote event payload
	•	resolution rule applied

5.9 Authentication UX
	•	Credentials may be provided via TD_AUTH_KEY environment variable.
	•	Alternatively, td may store the key in OS keychain or a local config file.
	•	If the server returns 401/403 indicating invalid/expired key, td:
	•	enters an unauthenticated state for sync
	•	prompts user to re-authenticate via td auth login

6. Server Specification

6.1 Deployment
	•	Single binary Go service.
	•	Backing store: Postgres.
	•	Optional object store: S3-compatible for snapshots and published assets.

6.2 Authentication
	•	API key-based authentication.
	•	Keys are long-lived by default, revocable, rotatable.
	•	Keys are stored hashed (e.g., Argon2 or bcrypt) with an indexed key prefix for lookup.

6.3 Authorization
	•	Authorization is project-scoped.
	•	Roles:
	•	owner: manage project, members, publishing
	•	writer: push + pull events
	•	reader: pull only

6.4 API endpoints (v1)

All endpoints are JSON over HTTPS.

Auth
	•	POST /v1/auth/login/start
	•	Starts device authorization flow.
	•	Response: device_code, user_code, verification_uri, expires_in, interval.
	•	POST /v1/auth/login/poll
	•	Body: device_code.
	•	Response: api_key when authorized.
	•	POST /v1/auth/keys/revoke
	•	Revokes the calling key.

Projects
	•	POST /v1/projects
	•	Creates a project.
	•	Body: name, optional metadata.
	•	Response: project_id.
	•	GET /v1/projects
	•	Lists projects visible to user.
	•	GET /v1/projects/{project_id}
	•	Returns project metadata and membership role.

Membership
	•	POST /v1/projects/{project_id}/invites
	•	Body: email, role (writer|reader).
	•	POST /v1/invites/{invite_id}/accept
	•	DELETE /v1/projects/{project_id}/members/{user_id}
	•	PATCH /v1/projects/{project_id}/members/{user_id}
	•	Updates role.

Sync
	•	POST /v1/projects/{project_id}/events:push
	•	Body: { device_id, session_id, events: [ { client_action_id, event_timestamp, payload } ] }
	•	Response: { accepted: n, last_server_seq, acks: [ { client_action_id, server_seq } ], rejected: [ ... ] }
	•	GET /v1/projects/{project_id}/events:pull?after_server_seq={n}&limit={k}
	•	Response: { events: [ { server_seq, device_id, session_id, client_action_id, event_timestamp, payload } ], last_server_seq }
	•	POST /v1/projects/{project_id}/sync
	•	Convenience endpoint for combined push+pull (optional).

Publish
	•	POST /v1/projects/{project_id}/publish
	•	Body: { visibility: public|private, slug }
	•	Response: { publish_id, url }
	•	PATCH /v1/projects/{project_id}/publish
	•	Updates visibility/settings.
	•	GET /p/{slug}
	•	Serves read-only web UI.

6.5 Event storage and ordering
	•	Server MUST assign server_seq as a monotonically increasing sequence per project.
	•	Server MUST store events append-only.
	•	Server MUST ensure idempotency by rejecting duplicate (project_id, device_id, session_id, client_action_id) or returning the prior assignment.

6.6 Limits and quotas
	•	Maximum event payload size (e.g., 64KB).
	•	Maximum push batch size (e.g., 1,000 events).
	•	Rate limiting per key and per IP.

6.7 Snapshots (optional but recommended)

Purpose: accelerate onboarding and recovery.
	•	Server may store periodic snapshots:
	•	either a compressed SQLite file
	•	or a materialized logical snapshot
	•	Endpoint:
	•	GET /v1/projects/{project_id}/snapshot
	•	Returns latest snapshot metadata and download URL.

6.8 Published views data path
	•	Server maintains a materialized read model per published project by applying events in server_seq order.
	•	The read model may be:
	•	a SQLite database generated server-side
	•	or Postgres tables reflecting current state

The published web UI reads only from the read model.

7. Database Schema (Server)

Postgres tables (conceptual):
	•	users (id, email, created_at, ...)
	•	api_keys (id, user_id, key_prefix, key_hash, created_at, expires_at, revoked_at)
	•	projects (id, owner_user_id, name, created_at, ...)
	•	memberships (project_id, user_id, role, created_at)
	•	invites (id, project_id, email, role, token_hash, expires_at, accepted_at)
	•	events ( project_id, server_seq BIGSERIAL, device_id, session_id, client_action_id, event_timestamp, payload JSONB, received_at, PRIMARY KEY (project_id, server_seq) )
	•	Unique constraint for idempotency:
	•	(project_id, device_id, session_id, client_action_id)
	•	Optional:
	•	publish (project_id, slug, visibility, created_at, updated_at)
	•	snapshots (project_id, seq, storage_key, created_at)

8. Security
	•	HTTPS required.
	•	API keys treated as secrets.
	•	Keys stored hashed.
	•	Least-privilege authorization enforced on every endpoint.
	•	Audit logging for:
	•	login flows
	•	membership changes
	•	publish settings changes

9. Reliability and Operations

9.1 Observability
	•	Structured logs with request id, project id, user id (when known).
	•	Metrics:
	•	requests, error rates
	•	push/pull volume
	•	event lag per project
	•	DB latency

9.2 Backups
	•	Nightly Postgres backups.
	•	Optional snapshot storage in object store.

9.3 Failure modes
	•	If server is unreachable: client continues local-only and retries later.
	•	If push partially succeeds: server returns per-event acks; client advances last_pushed_action_id only for acked events.
	•	If pull fails: client does not advance last_pulled_server_seq.

10. Compatibility and Versioning
	•	API versioned under /v1.
	•	Event payload schema version included in each payload.
	•	Client and server must accept unknown fields.

11. Implementation Notes
	•	Client event payload SHOULD include:
	•	schema version
	•	action type
	•	entity type/id
	•	full new_data
	•	optional previous_data
	•	Server does not interpret payload for sync purposes.
	•	Materialization for publishing interprets payload according to schema version.

12. Open Decisions (must be resolved before implementation)
	•	Conflict resolution rule: last-write-wins vs optimistic versioning per entity.
	•	Snapshot strategy: server-side SQLite file vs logical materialization.
	•	Credential storage: env var only vs optional keychain integration.
	•	Whether to ship a combined /sync endpoint in v1.
