# Changelog

All notable changes to td are documented in this file.

## [Unreleased]

## [v0.63.0] - 2026-08-22

### Dependencies

- **td now builds with Go 1.27** (up from 1.25.8). The main consequence is that `encoding/json/v2` is the default JSON implementation, which td leans on for sync payloads, the serve API, JSONL logs, and SQLite JSON columns; all of those round-trip unchanged, and `GOEXPERIMENT=nojsonv2` remains as an escape hatch. Note that Go 1.26 raised the floor for darwin builds to macOS 13+ — Homebrew users are unaffected in practice.

### Tests

- **The shell e2e sync suite runs again** (td-cb8ba9). All 16 `scripts/e2e` tests had been failing in shared setup since 2026-06-19: `scripts/e2e/harness.sh` still authenticated through `/v1/auth/login/start` + `/auth/verify`, which now answer 410 Gone, so every test died before its first assertion. `curl -sf` under `set -e` made that exit silent — no error, no diagnostic — which is why it read as a broken suite rather than a moved endpoint. The harness now drives the same device PKCE flow as `td auth login` and the two already-migrated harnesses, provisions users up front because `device/start` is non-enumerating, and reports a reason on every failure path. Sync behavior itself was never at fault.

## [v0.62.0] - 2026-08-21

### Monitor

- **Empty panes name the next step** (td-70d392, td-219916). Current Work, Board, and Activity Log leave a blank line under the header and indent empty copy to the title. Current Work tells you to start a task with td. Board distinguishes a database with no issues from a filter that matches nothing. Embedded Sidecar adds `Next: Press [3] for Workspaces…` only when there are no tasks yet; standalone `td monitor` never mentions Sidecar tabs.
- **Getting Started title and subtitle sit together as one heading** (td-1495cc). Install guidance sits above the buttons; `?` / `H` hints sit below.
- **Note create/update events log as "created note" / "updated note"** (td-545473) instead of "created issue" / "updated issue".

### Bug Fixes

- **The issue modal no longer re-queries the database and rebuilds its layout every frame**, which spiked CPU in embedded hosts while a modal was open.
- **Modal markdown re-renders only when its wrap width changes** (td-fcb03a) instead of on every render pass.

## [v0.61.0] - 2026-08-20

### Monitor

- **Embedded hosts can compose the monitor's three root panels into their own focus ring** (td-8b34d8). `pkg/monitor` now exposes stable `current-work`, `task-list`, and `activity` focus stops with their current rendered bounds, reports the current stop, focuses a stop directly while preserving cursor clamping and scroll visibility, and reports when an input or overlay must retain Tab. Standalone Tab and Shift+Tab use the same direct focus path, so embedding does not introduce a second navigation model. Compact and error replacement views expose no stale panel stops, and self-review confirmation, record-review, and activity-detail overlays correctly retain Tab.

### Documentation

- **Agents are prompted to check for Sidecar capabilities before working in td.** The README now points Sidecar-hosted sessions to `sidecar --agents` so they can discover host-provided navigation and presentation tools.

## [v0.60.0] - 2026-08-18

### Features

- **The monitor's list, board, and swimlane panes share one row layout.** Rows are built from a single column spec — `<gutter><key> <type> <priority> <title>` — so the issue key and every column after it line up within a pane instead of drifting per view. The gutter is a fixed two columns wide, so selecting a row draws the caret without shifting the rest of the row, and rows sit visually indented beneath their flush-left section headers. The key column sizes itself to the widest key on screen within a 9–16 column band, and the title keeps a minimum width so a long key can never squeeze it away.

### Bug Fixes

- **One unappliable remote event no longer wedges a peer's sync permanently** (td-8fe2bc). A pull batch containing an event that could never apply rolled back with the sync cursor deliberately preserved, so every later pull refetched the same batch, failed on the same event, and rolled back again — the peer stopped converging for good, and not just for the row involved but for every event behind it in the stream. Two fixes: (1) a per-event failure is now classified, and only a **transient** one (lock contention, disk I/O, timeout) rolls the batch back for retry; a **permanent** one (constraint violation, unknown entity or action type, undecodable payload) is quarantined, so the cursor advances and the rest of the stream flows. Unrecognized errors default to transient, so a peer stalls loudly rather than silently skipping something. (2) A create whose `ON DELETE CASCADE` parent no longer exists is now recognized before the insert and deliberately dropped — cascade means the schema itself says the child must not outlive the parent, so dropping it is what makes peers agree rather than diverge. Nothing is discarded silently: every skipped event is recorded in the new `sync_skipped_events` table with its `server_seq`, payload, and error, and surfaced by `td sync status` (text and `--json`). FK enforcement is unchanged — the parent check runs *before* the insert and only for cascade FKs, so SQLite still rejects everything it did before. The root cause was a peer replaying its **own** events from a cursor suffix, not the cross-peer delete race originally suspected.
- **Wrapped modal text is no longer jagged.** Lip Gloss v2 counts border and padding inside `Width()`, but the legacy monitor modals still budgeted content for the v1 rule (`width-4`). Every wrapped log line, description, and acceptance block ran two cells past the box, and Lip Gloss re-wrapped the overrun onto its own unindented line. Content is now built for `width-6`, and standalone and embedded modals agree on one outer box: a host chrome renderer receives the same outer dimensions the standalone box occupies and its two-cell-thinner chrome exposes a slightly wider interior, which the surface fill covers. Form modal mouse hit-testing followed the same corrected geometry.

## [v0.59.0] - 2026-08-18

### Features

- **The embedded monitor takes its theme from the host** (td-2930ee). A host that embeds `pkg/monitor` — Sidecar's td tab — can now hand it a model-scoped theme, and every surface honors it: the core list, kanban and swimlane renderers, both the declarative and legacy modals, the notes and form paths, markdown, and the Huh select filters. Rethemes apply live without losing selection, scroll position, filter text, or in-flight form state, and a regression guard fails the build if a renderer reaches past the theme contract for a hardcoded color.
- **`td note restore`, `list --deleted`, and `show --include-deleted`** bring soft-deleted notes to the CLI. This is what let Sidecar's Notes plugin drop its in-process database access (see the corruption fix below).

### Bug Fixes

- **td no longer corrupts `.todos/issues.db` under mixed embedded and CLI use.** modernc's WAL shared-memory coordination destroyed the database five times in two days whenever a long-lived embedded connection (Sidecar's monitor and notes store) ran alongside bursts of short-lived CLI processes — a 0-byte `-wal`, a populated `-shm`, and `database disk image is malformed`. td now uses TRUNCATE rollback-journal mode, which has no `-shm`/`-wal` state to race on. Slower per-write journaling means bursts queue past the old 500ms write lock, so the lock timeout now matches the 5s SQLite busy_timeout (td-adbf16).
- **Themed modals are no longer splotchy.** Lip Gloss emits a block background once per line, so any nested style's reset left the rest of that line on the terminal's default background, and host chrome renderers padded short lines with unstyled spaces of their own. `pkg/monitor/ansifill` re-applies the surface after every SGR that would drop back to the default, leaves genuine nested backgrounds (a selected row, a badge) intact, and pads to width when the host owns the chrome.

## [v0.58.0] - 2026-08-16

### Features

- **Public `github.com/marcus/td/pkg/notes` API** (td-ac3876). In-process clients (Sidecar's Notes plugin) can open a project database and create, list, update, delete, restore, pin, and archive notes through the same `withWriteLock` + `action_log` path as `td note`. `List` with `Limit<=0` returns every row. Pin and archive now write `action_log` so those toggles sync. `RestoreNote` undeletes a soft-deleted note.

### Bug Fixes

- **Historical `notes.deleted_at` values are recognized as deleted.** Older Sidecar rows used `time.Time.String()` or a space instead of `T`. RFC3339-only parsing left `DeletedAt` nil, so those notes disappeared from every list. Any non-empty `deleted_at` now counts as deleted.

## [v0.57.0] - 2026-08-13

### Bug Fixes

- **Review buckets are painted, and reopening an issue supersedes the previous approval** (td-245126). Ready-to-close and pending-other rows were classified but never rendered, so they appeared as a blank gap with no status tag. Separately, reopening a closed issue left the prior close's approval active, so a resubmitted issue looked closable when it was actually awaiting review. Those buckets now render on swimlanes, the task list, and kanban, and `closed → open` is treated as a new review epoch on the shared write path. The status and review state machines are now documented in `docs/status-and-review.md`.
- **Undoing a close no longer discards the review it just restored.** The reopen rule above matched every transition out of `closed`, including the `closed → in_review` that undoing an approve or close-after-review performs, so the undo path cleared `superseded_at` on the prior review and then immediately set it again. Reopen is now specifically `closed → open`, the one edge out of `closed` in the state machine.
- **The monitor's kanban hides empty columns instead of crushing card width.** Every swimlane category used to get a full-width column, so usually-empty buckets like Ready to Close squeezed the occupied ones; occupied columns now share the space and `h`/`l` skip the holes.
- **The kanban overlay keeps a stable three-column skeleton and its closing rule.** Review / WIP / Ready always stay so a new board has a shape, grid lines no longer wrap (width had ignored border and padding, letting min-width overflow), and the last row is no longer clipped by `MaxHeight`.

### Developer

- **td now has Go CI on push and PR to main.** Previously only a release-tag workflow existed, so a broken build or test regression could sit on main undetected until the next release. The workflow is test-only for now: golangci-lint is deliberately excluded because td has ~2100 pre-existing findings that need their own remediation pass.
- **`make release` fails closed on a missing changelog entry or a non-green Go CI** for the commit being released. Both were previously only things to remember.
- Fixed two failures that only appear on a clean CI runner: `internal/workdir`'s worktree tests shell out to `git commit`, which needs an identity that GitHub Actions images do not set globally; and `TestChaosSync` now recognizes `invalid transition from` as expected state drift, which is the shared refusal string across `start`, `unstart`, `block`, `review`, and `ws`.

## [v0.56.0] - 2026-08-08

### Bug Fixes

- **Review stamps cleared by `supersedeApprovalIfLinked` now sync to peers.** Superseding a linked review used to clear `issues.reviewer_session` / `reviewed_at` with a bare `UPDATE`, with no matching `action_log` entry. The sync engine derives every outbound event from `action_log`, so the clearing client kept the change forever and a peer that had already pulled once could never receive it — the row's own `create` event gave `BackfillOrphanEntities` nothing to rescue. `TestChaosSync` had been reporting this as `issues match — common set diverges`. Fixed by routing the clear through the same logged update path other mutations use; `TestActionLogReconstructsEveryIssue` now globally guards against a mutation reaching the `issues` table without a corresponding `action_log` entry, rather than relying on catching each call site by hand.
- **The monitor now displays every timestamp and due/defer date in local time,** and day-boundary math (today/tomorrow labeling) uses civil-day diffs instead of raw duration arithmetic, so a DST transition can no longer mislabel tomorrow as today. Formatting is centralized on a single `formatLocalTime` helper instead of being reimplemented at each call site (td-3e362c).

### Developer

- **`make install-local` / `make use-homebrew` / `make install-status`** bring the machine-wide dev-install switching pattern from sidecar and tasks to td, so the machine-wide `td` binary can be swapped between a local build and the installed Homebrew release without hand-editing symlinks. `install-local` and `install-worktree` refuse feature branches/linked worktrees where that would be a mistake, and refuse to replace a `td` that isn't either a managed build or a Homebrew link. Plain `make install` remains an unmanaged `go install` into `GOBIN` (td-83f436).
- gofmt applied across the codebase to clear formatting drift accumulated across recent commits.

## [v0.55.0] - 2026-08-01

### Breaking: diagnostics moved to stderr

- **`ERROR:` and `Warning:` lines now go to stderr, not stdout** (td-181f4c). Human diagnostics printed to stdout, so any command that reported an error and then emitted a JSON envelope left stdout unparseable — `td list --json` in an uninitialized project produced an `ERROR:` line followed by valid JSON, and `json.load` failed on the first byte. The same corruption td-d762a5 fixed for `show`, reached by a different route and present at roughly 68 call sites. Fixed centrally in `internal/output` rather than by sweeping the call sites: stdout is now the command's **result**, stderr is **everything said about the command**. `td tree <missing-id> --json`, `td next --json`, and `td reviewable --json` were affected identically.
- **This changes the contract for existing scripts.** A caller doing `td <cmd> 2>&1 | grep ERROR:` or capturing stdout to detect failure needs updating: read stderr, or use the exit code and the `--json` error envelope, which is what these codes were made correct for in td-be12a0 and td-d762a5. Concretely, an idempotent no-op now writes nothing to stdout at all:

  ```
  $ td unstart <already-open-issue>      # exit 0
  stdout: (empty)                        # was: "Warning: already unstarted td-xxxxx"
  stderr: Warning: already unstarted td-xxxxx
  ```

  So `out=$(td unstart X); [ -n "$out" ]` no longer detects a successful no-op. `td reject` and `td unblock` behave the same way. **Do not merge the streams with `2>&1` before parsing `--json`** — that reintroduces exactly the corruption this fixes.
- `output.Success` and `output.Info` deliberately **stay on stdout**: their content is the command's answer, not commentary on it. `Info("No boards found")` is the literal result of `td board list`, and moving it would leave stdout empty for a command that succeeded. `WarningErr`, which existed only because `Warning` was unsafe, is gone.

### Sync convergence

- **Log rows produced as side effects never synced to other machines** (td-018ee1, td-8aa916). The sync engine derives events exclusively from `action_log`. Two code paths wrote rows into `logs` without a matching `action_log` entry, so those rows were invisible to every peer — permanently, since `BackfillOrphanEntities` stops rescuing orphans once a client has pulled even once. Affected: the progress log `td unstart` writes when releasing a claim (regressed in v0.54.0 by td-c45f99, which replaced an `AddLog` call that emitted the event with a `ReleaseClaims` call that did not), and the auto-cascade / auto-unblock notes (`Auto-cascaded to <id>`, `Auto-unblocked (dependency <id> closed)`) written by the dependency engine. Because cascades are computed locally and never recomputed by the receiver, the peer had no way to reconstruct them. Both now write the `create`/`logs` entry inside the same transaction as the row itself.
- **The class is now guarded globally, not per site.** It had already been fixed once and recurred at a second producer, so a new invariant test asserts that **every** row in each of the 12 syncable tables has a corresponding create-style `action_log` entry, after driving a broad stream of ordinary mutations — a per-site test only ever catches the sites someone thought of. A companion guard fails if `internal/sync`'s syncable-table list drifts out of step with it.
- **This was hiding behind a test that reported it as a flake.** `TestPartitionRecovery` had been failing deterministically (10/10, always the same row) while its assertion printed only `alice has 24 lines, bob has 25 lines`, which reads as noise and was dismissed as such across three sessions. Convergence failures now print the symmetric difference of the actual rows, for issues as well as logs, so the failing row names itself.

### Stale-claim reclamation

- **`td unstart --session <id>` releases every claim held by one named session** (td-c45f99). This is the reaper a fleet supervisor actually needs: when a tick is killed by a usage limit or a wall-clock timeout, the supervisor knows exactly which session died. `--stale` cannot provide that safely — reaping by idle time in an `-n 4` fleet releases every other quiet slot's claim, which is the two-agents-one-issue failure the design exists to prevent. `--session` is exact and uses no liveness heuristic at all. It previews by default and mutates with `--force`, emits the same `--json` shape as `--stale`, and treats a session id that names nothing and holds nothing as an error rather than an empty success.
- **`td unstart --stale <duration>` releases claims whose holder is gone** (td-9b2e4a). An agent killed mid-work — by a subscription usage limit, a wall-clock timeout, a crash — never hands off and never unstarts, so its `in_progress` claim leaks: the work never returns to the ready queue and a supervisor watching for ready work reads a broken fleet as an idle one. td already tracks each session's last activity, so td is the right owner of "this claim's holder is gone" rather than every harness that drives it.
- It **previews by default and mutates only with `--force`**, copying `td session cleanup`, and reuses the same duration parser (`2h`, `90m`, `1d`). `--json` lists each claim with its issue id, the holding session, `idle_seconds`, its last activity, and which signal that activity came from (one exact rendering of the idle time, not a truncated string beside an exact number); the released issue records `claim released: implementer session ses_x idle 3h0m0s (threshold 2h)` in its history.
- **There is deliberately no default threshold**, and liveness is no longer read off the holder's session row alone. A claim's liveness is the most recent of the holder session's last activity, the issue's own `updated_at`, and the newest history entry on that issue by any session. Session identity is `branch + agent fingerprint + match context + worktree`, so an agent that runs `git checkout -b` — or works in a per-slot worktree — mints a **new** session row and stops heartbeating the one recorded in `implementer_session`; measuring only that row read a live agent as hours dead and released its claim. The self-claim guarantee is likewise resolved against the caller's identity lineage — its `previous_session_id` chain, followed in both directions — rather than one exact id. Lineage is the only kinship relation: `agent_pid` is not used to widen it, because sessions sync between machines and a pid from another host means nothing locally, so a ghost row wearing the sweeper's fingerprint would be unreclaimable at every threshold. A lineage-held claim past the threshold is never released but always **reported** under `unresolved`, with `td unstart --session <holder> --force` as the remedy; dropping it silently made the sweep answer "no claims idle longer than 2h" while three sat 72 hours dead. The threshold must still exceed the longest a healthy agent can run without **writing** to td — read-only commands do not move the signal, which `docs/multi-agent-sessions.md` now states precisely and by name.
- **The sweep is atomic and guarded.** Releases run in one transaction with one write-lock acquisition for the whole batch, and each release carries a `WHERE id = ? AND status = ? AND implementer_session = ?` guard; a claim that moved between selection and the write is reported as skipped and excluded from the released count. Previously the sweep took the lock roughly four times per issue — an 800-claim sweep held it for 11 seconds and made a concurrent `td update` fail with `write lock timeout after 500ms`, and a live agent's `td start` mid-sweep had its brand-new claim silently revoked. The same 800-claim sweep now takes 0.3s, and the release writes only the claim columns, so a concurrent edit is no longer reverted.
- **Unmeasurable liveness fails closed.** A timestamp an older td build wrote unparseably reads back as the zero time, whose idle duration exceeds every threshold — so "td cannot measure this holder" meant "release it", under any `--stale` value. Such claims are now reported under `unresolved`, and the `--json` `action` gains a `_with_unresolved` suffix so a caller that reads only `action` still sees that something was left behind.
- **`td session cleanup` no longer strands the claims `--stale` exists to reclaim.** It selected rows by the same idleness predicate, so on a cron it deleted the holder row first and the issue leaked forever with exit 0. Cleanup now keeps any session still holding an `in_progress` claim, names it under `held` with the command that clears it, and deletes exactly the rows it previewed. In the other direction, `--stale` falls back to the work recorded on the issue when the holder's row is already gone.

### Fixed

- **A transition to `open` always releases the implementer claim, on every surface** (td-cdbbbe). `td update <id> --status open`, the API's unblock and reopen handlers, and two monitor paths (reopen, and the edit form's status field) all set the status and left `implementer_session` populated, so an issue could read `open` while still claimed. Neither the reopen fix (td-c45f99) nor the unblock fix (td-d2e612) had ever been applied to the API, which meant the same transition left different state depending on which surface performed it. The release now happens in the single funnel every logged issue mutation passes through, rather than at each call site — patching sites individually is how the bug survived three rounds, with reopen fixed, unblock missed, and the API and monitor missed twice. Sweeps already reclaimed these rows, so the damage was a wrong-looking claim until a sweep ran rather than a permanent leak.
- **`td check-handoff --json` and `td approve`/`close --json` always emit their list keys** (td-6bb403), completing td-9ff2fe and td-1e090e. `in_progress_issues`, `cascaded_parents`, and `unblocked_dependents` were omitted entirely when empty, so a caller got `KeyError` in Python or `undefined` in JavaScript for the ordinary case of nothing to report. They are now always present as `[]`. A sweep of the remaining `--json` sites found no further list-valued key with this defect; the singular-object analogue in `td status --json` is tracked separately.
- **`TestSyncStatusReachableWhenSyncCLIOff` no longer inherits feature gates from the developer's shell** (td-6fda71). It read the ambient `TD_FEATURE_SYNC_CLI` / `TD_FEATURE_SYNC_AUTOSYNC`, which `make test` scrubs and an interactive or agent shell does not — so it failed for everyone running `go test` directly and passed in CI, the inversion that costs the most trust. Three separate sessions hit it and two misattributed it to their own changes. `t.Setenv` could not fix it: the command tree is wired in `init()`, before any test runs. The gate is now a parameter to an extracted `wireSyncCommands`, so both the on and off branches are tested directly and neither depends on the environment.
- **The chaos suite names what failed instead of counting it.** `TestChaosSync` reported `N unexpected action failures` with no indication of which command refused or why. Every such failure turned out to be a legitimate state-drift refusal — the engine selects a target from its own local model and a sync lands a real change before the command runs — which the classifier did not recognize. It now matches on the precise evidence of drift (a refusal naming the concrete status it found) rather than another vague keyword, and unclassified failures print the action, actor, target, output, and reproducing seed.

- **`td unstart` no longer reports success while the claim is still held** (td-c45f99). Its idempotent-retry branch tested `status == open` alone, but `td reopen` cleared `closed_at` and the reviewer and left `implementer_session` set — so an issue could be open AND claimed. `td unstart` printed "already unstarted" and exited 0 with the claim untouched, and `--stale` could not see it either because it lists `in_progress` only. The no-op now requires an unclaimed open issue, `td reopen` clears the implementer alongside `closed_at`, and an open-but-held issue is released.
- **Out-of-range durations are rejected instead of wrapping** (td-c45f99). `--stale`/`--older-than` accept a `d` suffix that td multiplies into nanoseconds itself; unchecked, `213504d` overflowed int64 and landed back on **25m26s**, turning an operator's intended "never" into a 25-minute reaper.
- **`td session list --json` reports `current` correctly** (td-c45f99). It compared the stored `agent_type` against the bare agent *type*, but sessions store the full fingerprint (`claude-code_94806`), so `current` was false for the real current session and true for any row that happened to store a bare type; the `*` marker in human output had the same bug. Both now ask the session layer which row this shell would use, applying the whole identity key. `td whoami` also reports `branch` and `agent`, without which a JSON caller cannot correlate itself against `session list` at all.
- **`td whoami`'s human `STARTED` line is UTC** (td-c45f99). It formatted local time with a hardcoded literal `Z`, a seven-hour lie sitting next to the correct value in the same command's `--json`.
- **Usage output is back for genuine usage errors** (td-c45f99). `start`, `unstart`, `block`, `unblock`, `reopen`, `approve`, `reject` and `close` set `SilenceUsage` at registration time to keep Cobra from appending usage after a per-issue failure they had already reported — which also suppressed it for wrong arg counts and unparseable flags, where usage is the whole point. It is now set inside `RunE`, after arguments validate.
- **The bulk `unstart`/`reopen`/`unblock` summary distinguishes unchanged from failed** rather than summing both into "skipped", a distinction the loop already computed and then discarded.
- **`td unstart`, `block`, `reopen`, and `unblock` report rejections through their exit code** (td-34c833), completing the work started in td-b76671 and td-f9cfab. All four incremented a `skipped` counter for missing ids, invalid transitions, and update failures and then returned `nil`, so a command that performed no requested mutation still reported shell success. The semantics match the commands already fixed: a batch exits non-zero when nothing mutated and at least one target genuinely failed; a documented idempotent retry (unstarting an already-open issue, blocking an already-blocked one) stays exit 0; a mixed batch with at least one real mutation stays exit 0, so callers relying on partial success are unaffected. `--json` callers get exactly one error envelope naming the target that failed.
- **`td whoami`, `td session list`, and `td session cleanup` honor `--json`** (td-ca69a1). `--json` is a global persistent flag, so it was advertised on all three and ignored by all three: they printed human tables regardless. A flag that silently does nothing is worse than an absent one — an agent caller cannot tell "no JSON support" from "no results". `whoami` emits session id, ISO-8601 start time, and issues touched; `session list` emits a row per session with `last_activity` as ISO-8601 (not the human `17m0s`) and `age_seconds`; `session cleanup` emits what would be deleted in preview and what was deleted under `--force`. Empty results are `[]` / `count: 0`, never a human "none found" line. Human output is unchanged.

## [v0.54.0] - 2026-07-30

### Review attribution

- **`td approve --reviewed-by "<who>"` records who actually performed a review** (td-0bc752). `issue_reviews.reviewer_session` conflated two different facts — who reviewed the work, and which session wrote the row down. Those were the same thing when one session meant one agent; they diverge as soon as an orchestrator records a review its sub-agent performed, and `--self-review` then forces that orchestrator to write a record that is false. Good models balk at that, correctly, and the work stalls. Attribution is stored in a new `issue_reviews.reviewed_by` column and requires no `--reason`; the attribution is the substance. Available on the CLI, the API (`reviewed_by` on `/approve` and `/reviews`), and the monitor.
- **It never grants permission by itself.** Only the default `trusted` mode treats attribution as an acknowledgement. `delegated` and `strict` record it and still refuse an involved session's approval, so a project that pinned a mode for a mechanical independence boundary keeps it. td cannot verify the name — this is deliberately an honesty guardrail, and naming a reviewer who did not review is worse than an honest `--self-review` because it reads as independent in the audit trail.
- **`self_review` keeps its meaning**: "recorded by an implementation-involved session". It is set for both acknowledgement paths, and `reviewed_by` is what distinguishes them. Every historical row was already written that way, so no backfill and no reinterpretation of existing audit data.
- Attributed approvals get a plain progress log naming the reviewer rather than a `SECURITY`-tagged one — this is now the normal orchestrated path. The `.todos/security_events.jsonl` entry remains, and is written whenever the recording session was implementation-involved, on the direct approve path and the `--record-only` path alike, on both CLI and API. An independent session's approval is not audited: that file exists to surface approvals that lack mechanical independence, and filling it with routine ones makes it useless for the case it is for.

### Fixed

- **`--record-only` now works in the default `trusted` mode** (td-1748a6). It was rejected outside `delegated`, while the close-after-recorded-approval path already accepted `trusted` — so the default mode could close on an attestation it was not allowed to create. The API's close-after-recorded-approval path was delegated-only in the same way, leaving a recorded approval unusable through `/approve`.
- **Review rows now sync between machines** (td-02ae9f). `issue_reviews` had a fully built apply path but nothing that emitted the events, so reviews never left the machine that created them and close-after-recorded-approval silently did not work across two hosts — the exact topology this release recommends. Creates, supersedes, and undo now emit atomic lifecycle events, with cross-peer coverage in the sync harness.
- **The monitor no longer silently discards typed text** (td-5d602a). Modal inputs were captured on a by-value receiver's copy, so typed text reached the modal but not the code that read it, and the first characters were swallowed before the input took focus. A close reason typed into the monitor was discarded outright and record-review did not work at all. Initial typing, bracketed paste, and Ctrl+V are all fixed.
- **The monitor writes `security_events.jsonl` entries** for involved-session approvals (td-b718ba), matching the CLI and API. It was the one surface with no audit-file coverage.
- **`td approve`, `start`, `reject`, and `close` report rejections through their exit code** (td-f9cfab, td-b76671). A rejected command printed to stderr and exited 0, so `td approve <id> || handle_failure` reported success for a command that did nothing — backwards for a tool whose primary user reads exit codes. Idempotent no-ops (re-approving an already-closed issue) deliberately still exit 0.
- **Free text can no longer forge log entries in terminal output** (td-1ea790). Descriptions, acceptance criteria, handoff items, review summaries, and log messages are sanitized before rendering in `td show` and `td list --long`: escape sequences are stripped (an `ESC[E` moves the cursor with no newline in the data), carriage returns are normalized, and multi-line text is marked as a continuation of its entry rather than reading as a new one. This matters more than it used to now that an unverifiable attestation is what stands in for mechanical review independence — a reader who cannot trust the log cannot audit the attestation.
- The monitor's ready-to-close bucket, its record-review action, and `td reviewable`'s SQL now agree with the policy layer under `trusted` instead of silently diverging from it.

### Known limitations

- **Issue titles are not sanitized** for terminal rendering (td-c0e73c). Description, acceptance, handoff, review-summary and log text all are, but a title carrying an escape sequence can still forge a line at the top of `td show` and in plain `td list`. Pre-existing and unchanged by this release.
- **The monitor's activity feed and `td query` output are not sanitized** (td-c0e73c). `td query` renders short-form only, so it never emits the prose blocks that carry the vector.
- **`td unstart`, `block`, `reopen`, and `unblock` still exit 0 on rejection** (td-34c833). The commands fixed above cover the main workflow paths.

## [v0.53.0] - 2026-07-25

### Query

- **Fixed: `td query` date comparisons returned wrong answers, silently** (td-951238). Every comparison against `created`, `updated` or `closed` was wrong: `created < 2026-05-09` matched **nothing** and `created > 2026-05-09` matched **everything**, and neither reported a problem. Root cause: the in-memory matcher (the only path `Execute` actually uses) fell through to a numeric comparison that converted the timestamp to Unix seconds and the date literal to `0` — so every `<` was false and every `>` was true regardless of the dates involved. Equality was broken the same way, comparing `"2026-05-09 12:00:00 -0700 PDT"` against `"2026-05-09"` as strings. Verified against export ground truth on a 905-issue workspace: `created < 2026-05-09` now returns 441 and `created >= 2026-05-09` returns 464, matching the database exactly (both previously returned 0 and "everything"). The same helpers back `td note` queries, which were broken identically and are also fixed.
- **Date comparisons now have defined granularity.** A bare date names a whole calendar day in your local timezone, so `created <= 2026-05-09` includes the 9th and `created > 2026-05-09` excludes it — `<=` and `<` are genuinely different operators. An hour offset (`-6h`) compares to the instant instead. Timestamps written under different UTC offsets still compare by the wall-clock day you saw.
- **A timestamp that was never set matches no ordering comparison.** `closed < <date>` no longer treats an open issue's missing close date as the epoch, matching SQL's NULL semantics.
- **Unusable predicates now error instead of returning an empty result set.** A silent zero cannot be told apart from "no matches", which is what made the bug above invisible. `created < banana`, `created ~ 2026`, a malformed date, and ordering operators on cross-entity fields (`log.timestamp < ...`) now fail with a message naming the problem.
- **Result limits are stated, never silent.** `td query` still defaults to 50 results, but says `showing 50 of 441 matches` on stderr when the cap drops matches; `-n 0` returns everything. `-o count` now reports the true match count rather than the truncated one. The pre-filter scan cap is reported the same way and is adjustable with the new `--max-scan`.

### Search

- **Fixed: `td search` now searches what its help claims** (td-406d65). The help advertised logs and handoff content, but the SQL only matched id, title and description — so searching for a term an agent had written into a handoff returned nothing, with no signal that the material was never in scope. Search now also covers log messages and all four handoff fields (done, remaining, decisions, uncertain). Matches found only in that activity rank below matches in the issue's own title or description, and `--show-score` names the field that matched.
- **Fixed: handoff content was unsearchable in SQL at all.** Handoff fields hold marshalled JSON written as `[]byte`, so SQLite stores them with BLOB affinity and a bare `LIKE` never matches them. The search SQL now casts to text.
- **An empty search states its scope**, so "no results" is not read as "this text exists nowhere in td". Comments remain out of scope; the message points at `td query "comment.text ~ ..."`.
- `td list --search` and every other caller keep the narrow issue-fields-only scope; only the `td search` path widened.

## [v0.52.0] - 2026-07-24

### Sync
- **Autosync is now enabled per project, not via a feature flag.** Setting a project up for sync (`td login` + `td sync init`/`link`, giving it a usable `sync_state`) is all it takes for autosync to run — there is no longer a flag to flip to turn sync on. The `sync_autosync` feature flag and the new `config.json` `sync.autosync` field are optional **overrides / a global kill-switch**. Gate precedence: global kill-switch (explicit `false`) → explicit `sync_autosync` override → per-project configured (default). `td sync disable` / `td sync enable` write the tri-state global switch (shell-independent). `td sync status` is always available — run it first when sync seems stuck; it reports gate state + source, configured/authenticated, pending events, and last sync. Documented the per-project model, override precedence, the `~/.zshenv` vs `.zshrc` caveat for `TD_FEATURE_SYNC_AUTOSYNC`, and the upgrade path (legacy `sync.enabled: false` is ignored by the gate; already-authenticated projects are auto-configured with no re-login). See `docs/sync-client-guide.md` and `CLAUDE.md`.
- **Sync now fails closed on SQLite corruption.** Explicit and automatic sync run an integrity check before reading or uploading local state, remote pull batches roll back if any event fails, and snapshot replacement validates the staged database before installation. Replacement also coordinates through the maintenance lock and refuses to overwrite a live SQLite generation with active WAL/SHM sidecars.

### Project Members
- Member listings now include email addresses, and owner-role updates refuse to demote the final project owner.

### Agent Onboarding
- Replaced the old mandatory agent instructions with compact, autonomy-respecting guidance that defaults to `td usage --new-session -q`, explains the normal tracked workflow, links to on-demand help, and accurately distinguishes trusted self-review from delegated review.
- Installed guidance now uses a versioned td-owned block. Older marked blocks can be updated safely, while guidance written by a newer td release is detected and left untouched.

### Release Process
- `make test` now removes ambient sync feature overrides and disables workspace inheritance so release validation is reproducible.
- `make release` validates strict semantic-version syntax, a clean `main`, and that `HEAD` matches `origin/main` before creating an annotated tag. It also runs the full release-safe test suite before pushing the tag.


## [v0.51.2] - 2026-07-18

### Bug Fixes
- **Fixed timestamp corruption that could break session lookup** (`sql: Scan error on column ... "last_activity": unsupported Scan, storing driver.Value type string into type *time.Time`). Root cause: td opened SQLite without a `_time_format` DSN param, so modernc's default writer serialized every `time.Time` with `time.Time.String()` — emitting a monotonic-clock suffix (`m=+…`) and a zone *name* (`PDT`) that the driver cannot reliably parse back. This corrupted timestamps DB-wide but only surfaced on the strict-scan session-lookup path. The fix has four parts:
  - **Prevent:** open the DB with `_time_format=sqlite`, so all writes use the canonical `2006-01-02 15:04:05.999999999-07:00` layout that round-trips reliably.
  - **Tolerate:** session scans now use a lenient timestamp scanner that degrades gracefully (falls back to `started_at`/zero) instead of failing the whole lookup when a value is malformed.
  - **Repair:** a new schema migration (v36) normalizes already-corrupted timestamps across every table to the canonical layout; idempotent and safe to re-run. The repair also reaches server-side `project.db` files on open.
  - Regression and round-trip tests included.

### Internal
- Feature-flag tests are now isolated from the developer's shell environment (`TD_FEATURE_*` / `TD_ENABLE|DISABLE_*`), so exported flags no longer cause spurious test failures.

## [v0.51.1] - 2026-07-18

### Bug Fixes
- **`td handoff` no longer consumes stdin when explicit input is given.** When any of `--done`/`--remaining`/`--decision`/`--uncertain`/`--note`/`--message` (or a message arg) is supplied, handoff no longer also tries to read piped stdin, which could block or misparse the handoff. Regression test included.

## [v0.51.0] - 2026-06-20

### Sessions and worktrees
- **Current focus and active work sessions are now session/worktree scoped.** CLI and serve-mode current-state paths use the local-only `session_state` table instead of shared config-file focus, so separate agents and worktrees can keep independent current work without clobbering each other. This includes the CLI focus/log/handoff/review/work-session paths and the serve API used by embedded clients.
- **Session identity is safer for sub-agents and alternate worktrees.** Sessions now carry worktree metadata and context-aware identity so independent agent contexts can be tracked separately while local-only fields are kept out of sync payloads.
- **`td context`/resume reliability improved.** Resume now correctly calls the show path so context lookup returns the expected issue details.

### Sync
- **Auto-sync failures are bounded and visible.** Push retries now use a bounded backoff and warn when local changes remain pending instead of silently stranding them.
- **`td handoff` no longer waits on a startup pull and avoids redundant pulls after failed pushes.** Handoff still records local progress and attempts the post-mutation push, but an unhealthy sync endpoint now exits after the bounded push window with a pending-sync warning instead of paying multiple network timeouts.
- **Local-only worktree/session metadata is scrubbed from sync events and conflicts.** Worktree identity remains useful locally without leaking or conflicting across devices.

### Workflow
- **Auto-review handoffs can be synthesized from session logs.** Review submission can preserve a useful handoff trail even when an explicit handoff was missing.

## [v0.50.1] - 2026-06-20

### Sync
- **Fixed: `td sync` no longer warns about phantom conflicts from your own events.** A sync deliberately re-pulls the client's own just-pushed events to keep sequence numbers convergent, and replaying them in `server_seq` order can transiently overwrite newer local state (identical-payload log entries; an issue's creation event landing on top of a later local close) before a subsequent event in the same batch restores it. These self-replays were being flagged as overwrites, producing a spurious `Warning: N local records overwritten by remote changes:` and writing phantom rows to `sync_conflicts`, even on a single device with no other writers. `ApplyRemoteEvents` now gates conflict detection on `ev.DeviceID != myDeviceID` (wiring up a parameter that was already passed but unused), so only changes authored by *another* device are reported. Genuine cross-device conflicts still warn exactly as before. Regression test included.

## [v0.50.0] - 2026-06-20

### Sync server
- **First-class project slugs.** Projects now have a stored, globally-unique `slug` (migration v6: `slug` column + unique index). Slugs are generated on create from the project name (`slugify`, falling back to the project id for names that produce an empty slug, with `-2`/`-3` suffixes on collision) and an idempotent startup backfill assigns slugs to pre-existing projects, ordered deterministically by `created_at`. Soft-deleted projects are skipped so they don't consume slug namespace. The slug is exposed as the `slug` JSON field on the user project API (`GET /v1/projects`, `GET /v1/projects/{id}`) and the admin project API. Stored slugs are stable canonical identifiers — they are intentionally **not** updated on rename. This enables clean, deep-linkable, guessable `/projects/<slug>` URLs in td-watch (with the opaque `p_…` id still resolving and redirecting to the canonical slug).
- **Project invitations (backend).** Invite users to a project by email with a role; accept/decline flow with token-hashed invitations, plus invited-user signup support.
- **Web signup via magic links.** Email magic-link signup, including for invited users.
- **Sync-scope enforcement on project routes.** Project routes now require the `sync` scope while preserving the admin proxy path; added a `HasAnyScope` helper and a scope-enforcement test matrix covering admin proxy paths.

### CLI / JSON output
- **Consistent `--json` across every command, including all mutating ones.** `--json` is now a global (persistent) flag registered on the root command, so it works uniformly on reads *and* mutations (`create`/`add`, `update`, `start`, `unstart`, `log`, `handoff`, `defer`, `block`/`unblock`/`reopen`, `link`, `note add`/`edit`/`delete`, `approve`, `review`, `reject`, `close`). Previously many mutating commands had no JSON mode or registered their own ad-hoc local `--json` flag.
- **Shared success envelopes.** Issue-affecting commands emit `{"id","status","action","issue":{...full issue...}}` (plus command-specific extras like `session`, `reason`, or cascade counts); non-issue mutations emit `{"action", ...}` (e.g. `log` -> `{"action":"logged","id","log":{...}}`, `handoff` -> `{"action":"handoff_recorded","id","handoff":{...}}`). Produced by the new `output.EmitIssue` / `output.EmitResult` helpers. Bulk operations emit one JSON object per id (NDJSON).
- **`td add --json` now returns the new issue id** (and full issue), making `id=$(td add "..." --json | jq -r .id)` a reliable scripting idiom.
- **Structured JSON error envelopes.** Errors in JSON mode emit `{"error":{"code":"...","message":"..."}}` on stdout with exit code 1, via `output.JSONError`. `JSONError` now encodes through the `json` package so messages containing quotes, backslashes, or newlines remain valid, parseable single-line JSON.
- **Fixed: `td epic create` was broken** and now correctly delegates to the create path (emitting an `epic`-typed issue, including under `--json`).
- Exceptions documented: `td query` continues to use `--output table|json|ids|count`; the JSONL commands (`errors`, `security`, and the stats error/security views) emit their own line-delimited JSON; `show` additionally supports the legacy `--format json`.

### Documentation
- New "JSON output (`--json`)" section in `docs/guides/cli-commands-guide.md` documenting the global flag, both success envelopes, the error envelope, NDJSON bulk output, the exceptions, and a scripting tip, with real captured examples. Added a JSON output pointer to `README.md`.

## [v0.47.2] - 2026-06-17

### Bug Fixes
- **Fixed: `td handoff`, `td review`, and `td ws tag` failed with `FOREIGN KEY constraint failed (787)` when given a bare issue id (e.g. `94e0fd` instead of `td-94e0fd`).** These write paths persisted the raw argument as `issue_id`; since issue PKs always carry the `td-` prefix, a bare id never matched `issues(id)` and the constraint fired once foreign-key enforcement was enabled in v0.44.0. The lookup itself normalized internally, so `td start`/`td show` worked on the bare id while the audit-trail write failed — making the failure look like a session/sync problem (it was not; nothing references the `sessions` table by FK). `AddHandoff`, `AddComment`, `AddGitSnapshot`, and `TagIssueToWorkSession`/`UntagIssueFromWorkSession` now normalize `issue_id` before writing, mirroring how `GetIssue` and `CreateIssueReview` already canonicalize ids. Normalizing tag and untag together also closes a latent mismatch where the two computed different deterministic row ids from differing id forms. A regression test exercises bare-id writes under FK enforcement.

## [v0.47.1] - 2026-06-17

### Monitor
- **Fixed: capital-letter keyboard shortcuts stopped working after the v0.45.0 Charmbracelet v2 migration.** `Y` (copy issue ID), and every other shift-bound shortcut (`C`, `F`, `G`, `H`, `I`, `J`, `K`, `N`, `O`, `R`, `S`, `T`, `V`, `W`), silently did nothing — most visibly in sidecar's embedded monitor. In Bubble Tea v2 a shifted printable key arrives as the unshifted code plus a shift modifier (e.g. `shift+y` => code `y` + `ModShift`), so the keymap's `KeyToString` rendered it as `"shift+y"` and never matched bindings written in textual form (`"Y"`). It now uses `Key.String()`, which returns the printable text (`"Y"`, `"?"`) and falls back to the keystroke form for special keys (`"tab"`, `"ctrl+c"`, `"shift+tab"`). Regression tests now exercise realistic shifted-key input.

## [v0.47.0] - 2026-06-15

### Monitor
- **Getting Started modal now shows only on the first open of a project**, not on every launch. Previously the "Welcome to td!" modal reappeared each time the monitor opened in any project that had not installed td agent instructions, which nagged repeatedly when agents declined the install. A per-project `getting_started_seen` flag is now stored in `.todos/config.json` and stamped the moment the modal is first displayed; the modal shows only when the project has no instructions **and** has not been seen before. The `H`-key manual reopen path is unchanged.

### Documentation
- Release guide rewritten to be agent-friendly: documents the `make release` workflow, clarifies that the Homebrew tap is bumped by the dedicated `update-homebrew-tap` GitHub Actions job (building from source, updating URL + tarball sha256) rather than by GoReleaser, distinguishes the hand-written `CHANGELOG.md` from GoReleaser's auto-generated GitHub release notes, and adds an agent-oriented non-interactive runbook. Corrected the stale "manually maintained" comment in `.goreleaser.yml`.

## [v0.46.0] - 2026-06-11

### Review policy
- **`trusted` is now the default `review_policy_mode`.** A fresh install with no explicit configuration resolves to `trusted`, which keeps the delegated review-attestation model (prefer an independent reviewer; any session may close once an approval is recorded) and adds a flag-gated, audited self-review escape hatch. When you have reviewed your own diff and delegation is impractical, approve+close with `td approve <id> --self-review --reason "..."` (stamps `self_review` on the review row for audit). Explicit `review_policy_mode` settings, the `TD_FEATURE_REVIEW_POLICY_MODE` env override, and the legacy `balanced_review_policy` mapping are all unchanged — only the unconfigured default flips. Pin `review_policy_mode=delegated|strict` to keep the hard no-self-review wall.
- **`td feature {get,set,unset,list}` now manage the string-valued `review_policy_mode`** (e.g. `td feature set review_policy_mode trusted`), validating values via `reviewpolicy.ParseMode`. Previously the command only accepted boolean flags and rejected `review_policy_mode` as unknown.

## [v0.45.0] - 2026-06-08

### Dependencies
- **Charmbracelet v2 migration**: the monitor TUI and all Charm dependencies moved to the `charm.land` v2 stack — lipgloss v2.0.3, bubbletea v2.0.7, bubbles v2.1.0, glamour v2.0.0, huh v2.0.3 — plus x/ansi v0.11.7 and x/cellbuf v0.0.15. Bubble Tea v2 ships a faster renderer; glamour v2 renders OSC 8 clickable links in markdown.
- Key and mouse handling migrated to the v2 message model (`tea.KeyPressMsg`, `tea.MouseClickMsg`/`MouseWheelMsg`, etc.); colors moved to `image/color.Color`.

### API
- `monitor.Model.View()` now returns `tea.View` (Bubble Tea v2). A `monitor.Model.ViewString()` accessor is preserved so embedders (sidecar's tdmonitor plugin) can render the monitor as a plain string. The exported `modal`/`mouse` handler signatures (`HandleKey`, `HandleMouse`) are unchanged — they take the v2 message interfaces.

## [v0.44.0] - 2026-04-18

### Features
- `td doctor fk`: hidden diagnostic that reports orphan-row counts across every declared foreign-key relation (gated behind `TD_FEATURE_SYNC_CLI=1`)
- Monitor DB-pool diagnostics: set `TD_MONITOR_DBPOOL_DEBUG=1` to trace `getSharedDB`/`releaseSharedDB` with refcounts and caller — helps detect connection leaks in embedded monitors

### Database
- **Foreign-key enforcement enabled** on the CLI `issues.db` (previously off despite the schema declaring ~12 FK relationships)
- Migration 30 cleans up pre-existing orphan rows and adds schema-level `ON DELETE CASCADE` to child relations (handoffs, git_snapshots, issue_dependencies, issue_files, work_session_issues, comments, issue_session_history, board_issue_positions)
- Centralized SQLite connection opener (`internal/db/conn.go` — `OpenSQLite`) applies uniform pragmas (WAL, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`, `MaxOpenConns=1`) across the CLI, API server, per-project event DB, and snapshot DB paths
- WAL checkpoint on `Close` switched from `TRUNCATE` to `PASSIVE` to avoid blocking concurrent readers (the snapshot-build path still uses `TRUNCATE` before file copy, as intended)
- Manual cascade emulation in `internal/sync/events.go` removed where schema cascades now handle it; runtime `parent_id` cleanup retained (no FK on `issues.parent_id` due to `''` sentinel semantics)

### Improvements
- `withWriteLock` scope documented explicitly in source (serializes CLI writes only; does not coordinate with the API server's separate DBs)
- `td import` tolerates forward-referencing dependencies (issue A depending on issue B that appears later in the JSON) by disabling FK enforcement inside the import transaction; run `td doctor fk` after a large import to surface any remaining orphans

## [v0.43.0] - 2026-03-24

### Bug Fixes
- **Atomic lossless import** — `td import --json` now imports all associated data (logs, handoffs, dependencies, files) in a single transaction; backward-compat `UnmarshalJSON` handles old `handoff` singular / `[]string` deps format; `GetHandoffs` and `GetIssueDependencyRelations` added to DB layer (#65)
- **`UpdateIssue` missing fields** — `creator_session`, `minor`, and `created_branch` were not updated by `UpdateIssue` / `updateIssueAndLog`; patches silently dropped these fields (#70)
- **Timezone-aware defer/due filtering** — temporal queries used `date('now')` (UTC) instead of `date('now','localtime')`; deferred/overdue/due-soon filters returned wrong results in non-UTC zones (#70)
- **`RemoveDependencyLogged` wrong depID** — hardcoded `"depends_on"` in `DependencyID` call even for `"blocks"` relations; undo data was corrupted for non-`depends_on` relations (#70)
- **`DeleteBoardLogged` not atomic** — position updates, action_log inserts, and board delete ran outside a transaction; partial failure left inconsistent state (#70)
- **RateLimiter goroutine leak** — background cleanup goroutine used `time.Sleep` loop with no cancellation; `Stop()` added, called in `Server.Shutdown()` (#70)
- **CORS missing methods** — PATCH, PUT, DELETE not in `Access-Control-Allow-Methods`; browser pre-flight checks failed for mutating requests (#70)
- **Snapshot stat error ignored** — `f.Stat()` error swallowed; now returns 500 with proper error message (#70)
- **DB connection leak on init failure** — `Open` and `Initialize` did not close `conn` on migration or schema errors (#70)
- **Form scroll over-run** — `FormScrollOffset` could exceed content height; now clamped to `formScrollToBottom()` on wheel-down (#70)
- **Modal click detection** — section line bounds (`BlockedBy*`, `Blocks*`) computed during render (wrong: built incrementally); extracted to `computeModalSectionLines()`, called before click handling (#70)
- **In-progress panel header count** — used `len(m.InProgress)` including focused duplicate; now counts `inProgressVisible` to avoid spurious header when all items are hidden (#70)
- **RFC3339Nano timestamp parsing** — sync pull events with sub-second precision failed with strict `RFC3339`; now tries `RFC3339Nano` first with `RFC3339` fallback in both `autoSyncPull` and `runPull` (#69)
- **`sess != nil` guard in delete/restore** — `DeleteIssueLogged` / `RestoreIssueLogged` called with `sess.ID` without nil check; now uses empty string fallback (#69)
- **`escapeJSON` incomplete escaping** — manual string replacement missed `\r`, `\b`, `\f`, NUL, and other control characters; replaced with `json.Marshal` (#69)
- **Stdin pipe read without size check** — `stat.Size() > 0` guard on piped stdin in `log` and `handoff` commands silently dropped content from pipes that report 0 size; guard removed (#69)
- **Trusted proxy XFF spoofing** — `clientIP` trusted `X-Forwarded-For` unconditionally; attackers could spoof client IP for rate limit bypass; now only trusts XFF from configured `TrustedProxies` (#69)
- **CreateUser admin TOCTOU race** — `SELECT COUNT(*)` + `INSERT` without transaction allowed concurrent requests to both become admin; wrapped in a transaction (#69)
- **Backfill `anyEventSetsStatus` false positive** — LIKE pre-filter on `"status":"open"` matched nested fields and similar-named statuses (`"reopened"`); added `statusMatches` post-filter; extracted `checkCreateEventStatus` so `rows.Close()` fires before next query (#69)
- **Autosync pull transaction leak** — `defer tx.Rollback()` accumulated across loop iterations; extracted to `autoSyncApplyPullBatch` so defer fires per batch (#69)
- **Singleflight snapshot dedup** — concurrent snapshot requests for same project triggered redundant builds; now deduplicated with `singleflight.Group` (#69)

## [v0.42.2] - 2026-03-21

### Bug Fixes
- **SSE nil-validator panic** — `ApplyRemoteEvents` was called with `nil` validator, causing guaranteed panic on any non-empty SSE event batch; now passes an allow-all validator (#68)
- **`work_session_issues` never synced** — missing from `baseSyncableEntities`, silently dropping events on push/pull; added to sync entity map (#68)
- **Non-atomic undo of delete** — `RestoreIssue` + `LogAction` as separate locked operations had a crash window; replaced with atomic `RestoreIssueLogged` (#68)
- **Timestamp parse mismatch** — `GetRecentConflicts` used rigid format that failed on Go `time.Time.String()` output, breaking `td sync conflicts`; now uses flexible `parseTimestamp` with monotonic clock stripping (#68)
- **`rows.Err()` unchecked** — ~30 query functions returned silent partial results on driver errors; all now check and propagate `rows.Err()` (#68)
- **Non-transactional migration** — `migrateFilePathsToRelative` crash left partial data; now runs inside a transaction with proper rollback (#68)
- **Snapshot serve race** — only copy of snapshot was deleted on cache rename failure; `servePath` now updated immediately before second rename (#68)
- **StatusFilter data race** — map reference captured in goroutine shared underlying data; now deep-copied before capture (#68)
- **Board editor data race** — `BoardEditorBoard` pointer mutated from save goroutine while Update loop may read it; now copies struct before mutation (#68)
- **Stale syncState** — push updates `last_sync_at` in DB but in-memory struct was stale; pull now reloads syncState after push for correct conflict detection (#68)
- **CLI reject from wrong states** — rejected issues in `in_progress`/`blocked`/`closed`; now restricted to `in_review` only, matching HTTP API behavior (#68)
- **HelpFilter backspace UTF-8** — byte slicing split multi-byte runes; now uses `[]rune` conversion (#68)
- **Board editor preview count** — showed capped "6" instead of "5+"; uses sentinel `-1` to signal overflow (#68)
- **`copyFile` sync durability** — backup file not flushed to disk; now calls `out.Sync()` after copy (#68)

## [v0.42.1] - 2026-03-20

### Bug Fixes
- fix: `td import` now restores all issue fields and associated data (logs, handoffs, dependencies, files) — lossless round-trip (#64)

## [v0.42.0] - 2026-03-09

### Bug Fixes
- Fix closed_at timestamp to use current time on approve/close (#55)
- Fix mobile navbar sidebar hidden behind secondary panel (#54)

## [v0.41.0] - 2026-03-01

### Bug Fixes
- Fix premature title truncation in task list panel: overhead calculation in `formatIssueShort` was overestimating by 3 chars due to phantom leading spaces in tag width and a hardcoded type icon width. Task titles now display 3 more characters before truncating, giving more readable output in both `td monitor` and sidecar's embedded td view (sidecar#215)

## [v0.40.0] - 2026-02-27

### Features
- Add search/filter to help modal (press `/` to filter) (#25)
- Add scroll support to form modal
- Add `balanced_review_policy` feature flag (default on)
  - Allows creator-only approvals when a different session implemented the issue
  - Requires `--reason` for creator-exception approvals and logs them to security audit
  - Keeps implementer/self-approval blocked for non-minor issues

### Improvements
- Align `reviewable`/`in-review`/`status` reviewability hints with actual policy check

### Documentation
- Document balanced review policy in core workflow and references

## [v0.39.0] - 2026-02-26

### Features
- `td serve`: HTTP API server for programmatic access to td projects
  - Full CRUD for issues, comments, dependencies, boards, and focus
  - Status transition endpoints (start, review, approve, reject, close, reopen)
  - SSE event stream for real-time updates
  - Port file management and session bootstrap
  - Response envelope, DTOs, and validation helpers

### Fixes
- Support full agent file family (GEMINI.md, CLAUDE.local.md, etc) (#49)
- `td reject` resets issues to open instead of in_progress (#45, #47)
- Normalize action_log timestamp writes to RFC3339Nano UTC (#43)
- Exclude tasks with open dependencies from ready/next (#34)
- Prevent dependency divergence from phantom deletes and double normalization

### Documentation
- HTTP API documentation for `td serve`
- Improved sync setup guides based on user feedback (#39)
- Mention 100 character limit in title flag help text

## [v0.38.0] - 2026-02-19

### Fixes
- Fix approveIssue action in board/swimlanes view (#35)

## [v0.35.0] - 2026-02-14

### Features
- GTD-style deferral system: `td defer` and `td due` commands for managing temporal visibility
- `--defer` and `--due` flags on `td create` and `td update` for inline date assignment
- List temporal filters: `--deferred`, `--overdue`, `--surfacing`, `--due-soon` for focused views
- Monitor TUI modal displays defer/due dates with smart relative formatting
- Natural date parsing: `+7d`, `+2w`, `monday`, `tomorrow`, `next-week`, and more

### Documentation
- New deferral docs page covering GTD deferral concepts and usage
- Updated command reference with defer/due flags and temporal filters
- Updated monitor docs with defer/due date display

## [v0.34.0] - 2026-02-10

### Features
- `--work-dir` / `-w` global flag and `TD_WORK_DIR` env var for pointing td at a different project directory
  - Integrates with `.td-root` and git worktree resolution (unlike bypassing it)
  - Priority: `--work-dir` flag > `TD_WORK_DIR` env > cwd
  - Accepts path to project dir or directly to `.todos` dir
- Event taxonomy normalizer: centralized validation and normalization of entity and action types
  - Backward-compatible: accepts both singular/plural entity names and legacy action types
  - Comprehensive validation for all entity+action combinations in the sync/API layer

## [v0.33.0] - 2026-02-09

### Features
- Notes CLI: full CRUD via `td note` (add, list, show, edit, delete, pin, unpin, archive, unarchive)
- Notes CRUD database layer with soft-delete, undo support, and list filtering
- TDQ note query support: `note.` cross-entity fields (title, content, created, updated, pinned, archived)

### Bug Fixes
- Remove accidentally committed test artifacts
- Fix time parsing for TEXT timestamp columns in notes DB methods

## [v0.32.0] - 2026-02-08

### Features
- Admin API: server overview, config, and rate-limit-violations endpoints
- Admin API: user/auth endpoints — users list, detail, keys, auth events
- Admin API: project, events, and snapshots endpoints
- TDQ-powered snapshot query endpoint with server-side execution
- Integration test harness with fluent builder and assertion helpers
- Error code constants for consistent API responses

### Improvements
- Homebrew formula now builds from source (avoids macOS Gatekeeper warnings)

## [v0.31.0] - 2026-02-07

### Features
- Complete regression seed suite with verified seeds and runner integration
- Enable notes entity sync by default with feature flag

### Bug Fixes
- Resolve .todos in main repo for external git worktrees (gh pr checkout, Claude Code)
- Fix .todos lookup when td/sidecar launched from non-project-root directory
- Add sync feature flags to bash e2e harness (matching Go harness)
- Remove redundant notes schema from e2e test (latent schema mismatch)

## [v0.30.0] - 2026-02-06

### Features
- Sync engine: full multi-client sync with auto-sync, snapshot bootstrap, field-level merge, and conflict recording
- Sync CLI: `td sync init` guided setup wizard, `td sync tail` live activity view, `td config set/get/list`
- Notes entity support in sync
- Sync feature-flag framework with gated entity rollout
- Chaos sync test oracle with weighted random actions, convergence verification, and CLI runner
- Sparse board positioning with `ComputeInsertPosition` and automatic re-spacing
- Logged mutation layer (`*Logged` variants) for full undo/sync coverage
- Sync history tracking and pruning
- Multi-environment deployment system
- Nightshift added to sister projects

### Bug Fixes
- Field-level LWW merge prevents cross-field divergence
- Soft-delete board positions to prevent sync resurrection
- Cascade board position soft-deletes in sync receiver
- Map issue delete to soft_delete in sync protocol
- Prevent NULL points from sync partial update
- Handle NULL session columns after sync
- Backfill stale issues and handle undone creates in sync
- Detect dependency cycles during sync event application
- Drop UNIQUE(name) on boards to prevent sync data loss
- File locking and atomic writes for config
- Monitor periodic sync uses independent goroutine instead of BubbleTea Cmd

### Testing
- Comprehensive e2e sync test suite: chaos, convergence, clock skew, network partition, server restart, late-joiner, soak mode
- Syncharness test infrastructure for board delete cascades, server migration, and real-data scenarios
- Unit tests for sparse positioning and all logged mutation variants

### Documentation
- Sync setup and client guides
- Package-level godoc comments across 15 packages

## [v0.29.0] - 2026-02-02

### Bug Fixes
- Fix form width for text wrapping in issue modal
- Fix cross-entity query OR logic and blocks() wrong DB call
- Stop clipboard tests from clobbering system clipboard

## [v0.28.1] - 2026-01-31

### Bug Fixes
- Fix scan error on databases with unmigrated integer primary keys (CAST id AS TEXT in all SELECT queries)

## [v0.28.0] - 2026-01-30

### Features
- Primary key migration to enable future sync support
- GoReleaser binary releases and Homebrew formula

### Improvements
- Accessibility improvements
- Minor fixes from code review
- Transactional PK migration for safety

### Documentation
- Release guide wording fixes (cask → tap)

## [v0.27.0] - 2026-01-30

### Features
- GoReleaser binary releases and Homebrew formula
- Session migration to database

### Bug Fixes
- Revert URI DSN for modernc.org/sqlite, extract openConn helper
- Repair sessions table for DBs where v13 migration didn't apply

## [v0.26.0] - 2026-01-29

### Features
- Case-insensitive enum values in TDQ query language
- Much improved board editor modal
- ContextForm added to sidecar context map

### Bug Fixes
- Epic field query matched all issues instead of descendants
- Query language bug fixes
- Code review bug fix

## [v0.25.0] - 2026-01-28

### Features
- Exported `OpenIssueByIDMsg` for embedding contexts to programmatically open issue detail modals by ID

## [v0.24.0] - 2026-01-28

### Features
- Auto-unblock dependents when blocker is approved/closed
- OG image for rich link previews
- Redesigned marketing site hero and workflow sections

### Bug Fixes
- TUI actions now capture PreviousData/NewData for undo support
- TUI markForReview sets ImplementerSession when empty (matching CLI)
- TUI reopenIssue clears ReviewerSession (matching CLI)

## [v0.23.0] - 2026-01-26

### Features
- Group view controls in footer (view mode, show closed toggle, sort order)
- Split docs for easier navigation

### Bug Fixes
- Better focus handling for edit modal

## [v0.22.2] - 2026-01-26

### Bug Fixes
- Make list section a single tab stop instead of per-item (Tab now cycles list → buttons, not item1 → item2 → ... → buttons)

## [v0.22.1] - 2026-01-26

### Bug Fixes
- Fix board picker and handoffs modal navigation (j/k/up/down) not updating cursor due to value receiver semantics with declarative modal list pointers

## [v0.22.0] - 2026-01-25

### Features
- Migrate multiple modals to declarative library (Statistics, Handoffs, Board Picker, Delete/Close Confirmation)
- Add Getting Started modal for new users
- Improve monitor screenshot and workflow section styling
- Update marketing copy and redesign workflow sections
- Add Fraunces serif font for section headers

### Documentation
- Update modal inventory after declarative modal migrations

## [v0.21.0] - 2026-01-23

### Features
- Improve agent DX based on error pattern analysis

### Bug Fixes
- Fix agent fingerprint cache to only cache expensive process tree walk
- Add indices to schema for frequent queries to improve performance
- Fix critical path queries

### Documentation
- Update docs structure and marketing site

## [v0.20.0] - 2026-01-21

### Features
- Shorten issue IDs from 8 to 6 hex characters for easier typing
- Add collision retry logic for ID generation

## [v0.19.0] - 2026-01-21

### Features
- Include full task markdown when yanking epics (copies epic + all child stories)

### Bug Fixes
- Fix database connection leak in embedded monitor (connection pool singleton prevents FD accumulation)

## [v0.18.0] - 2026-01-20

### Features
- Add configurable title length limits via config (TitleMinLength, TitleMaxLength)
- Default max title length of 100 chars prevents description-as-title abuse

## [v0.17.0] - 2026-01-19

### Bug Fixes
- Add missing ESCAPE clause to label() SQL query for proper wildcard escaping
- Add error handling for is_ready()/has_open_deps() pre-fetch queries

## [v0.16.0] - 2026-01-19

### Features
- Add `epic.labels` field for query expressions
- Add `is_ready()` query function to find issues with no open dependencies
- Add `has_open_deps()` query function to check dependency status

### Bug Fixes
- Fix board refresh when query functions change
- Fix monitor panel header styling and row alignment
- Stabilize activity table column widths
- Fix activity table scrolling

## [v0.15.1] - 2026-01-19

### Bug Fixes
- Fix `--filter` flag validation: error if provided but empty
- Escape SQL wildcards in label() queries to prevent injection
- Use actual function name in label()/labels() error messages

## [v0.15.0] - 2026-01-17

### Bug Fixes
- Make SyntaxTheme actually apply Chroma themes in sidecar

## [v0.14.0] - 2026-01-17

### Features
- Add markdown theme support with custom chroma style builder
- Support hex color palettes and syntax themes for markdown rendering in monitor
- Allow embedders (sidecar) to customize theme via MarkdownThemeConfig

## [v0.13.0] - 2026-01-17

### Features
- Add send-to-worktree command for sidecar integration
- Add ctrl+K/ctrl+J shortcuts for move to top/bottom in board mode

### Bug Fixes
- ws handoff --review now uses proper review flow

## [v0.12.3] - 2026-01-14

### Features
- Persist filter state across sessions (search query, sort mode, type filter, include closed)
- Active search query now highlighted in orange for better visibility

### Documentation
- Add remote sync options research spec (Turso, rqlite, CR-SQLite analysis)

## [v0.12.2] - 2026-01-14

### Features
- Title validation for issue creation (min 20 chars, rejects generic titles)
- Cascade status changes to descendant issues (review, close, approve now cascade down)
- Epic task keybindings (O/R/C) in modal task section
- Created/closed timestamps shown in modal view
- Focus TaskList panel with cursor on first result after search

### Bug Fixes
- Modal actions (review, close, reopen) now work on focused epic tasks
- Modal refresh behavior instead of auto-close after status changes

## [v0.12.1] - 2026-01-14

### Bug Fixes
- Fix off-by-one mouse click bug in Current Work panel when no focused issue

## [v0.12.0] - 2026-01-14

### Features
- Sidecar worktree integration
- Mouse support for board picker
- CLI interface improvements

### Bug Fixes
- Fix modals when embedded in Sidecar
- Add panel checks to cursor commands when board mode active
- Fix for opening issues in top panel of td monitor
- Epic list consistency improvements

### Refactoring
- Split db.go into smaller files for maintainability

## [v0.11.0] - 2026-01-13

### Features
- Add gradient borders to sidecar panel

### Bug Fixes
- Fix session action recording and file locking for analytics
- Apply type filter correctly in board/backlog view
- Fix gradient border rendering issues

## [v0.10.0] - 2026-01-13

### Features
- Add board view with swimlanes in `td monitor`
  - New `td board` command for board operations
  - Toggle between swimlanes and backlog views
  - Keyboard navigation for board mode
  - Status-based swimlane organization
- Configurable keymap bindings system
- Improved blocked issue calculation and display

### Bug Fixes
- Fix line truncation issue in monitor view
- Fix mode switching in td monitor
- Respect sort order in swimlanes view
- Fix board movement issues
- Fix keyboard shortcuts in center panel

### Documentation
- Add board swimlanes and issue boards v2 specifications

## [v0.9.0] - 2026-01-10

### Features
- Add `rework()` query function for finding rejected issues awaiting rework
  - Query with `td query "rework()"` to find issues needing fixes
  - Efficient caching - fetches rework IDs once before filtering
- Show full log text in monitor task modal
  - No more truncation - long messages wrap properly
  - Uses cellbuf.Wrap for correct display-width handling
- Add Submit and Cancel buttons to form modal
  - Tab/Shift+Tab navigation between form fields and buttons
  - Mouse hover and click support for buttons

## [v0.8.0] - 2026-01-10

### Features
- Add issue state machine with workflow guards
  - Formal state transitions (open → in_progress → in_review → closed)
  - Validation guards prevent invalid state changes
  - New `td workflow` command for state diagnostics
- Add "needs rework" indicator for rejected in_progress issues
- Improved modal system documentation

### Bug Fixes
- Consolidate analytics logging to avoid double logging
- Add safe fallback for rejected issue detection errors

## [v0.7.0] - 2026-01-08

### Features
- Add local CLI analytics tracking (`td stats analytics`)
  - Track command usage, flags, duration, success/failure
  - Bar charts for most used commands and flags
  - List of least used and never used commands
  - Daily activity visualization
  - Session activity tracking
  - Toggle with `TD_ANALYTICS=false` env var
- Add unified `td stats` command with subcommands:
  - `td stats analytics` - Command usage statistics
  - `td stats security` - Security exception audit log
  - `td stats errors` - Failed command attempts

## [v0.6.0] - 2026-01-07

### Features
- Auto-handoff when submitting issues for review

### Bug Fixes
- Fix mouse offset issue when filtering or sorting in td monitor
- Remove self-close from close guidance

### Tests
- Additional test coverage

## [v0.5.0] - 2026-01-07

### Features
- Improved shortcuts panel for standalone `td` command
- Search field improvements
- Add `td security` command for viewing self-close exception audit logs

### Tests
- Add comprehensive modal scroll boundary tests
- Add comprehensive editor integration tests
- Add security command and review tests

## [v0.4.26] - 2026-01-06

### Bug Fixes
- ReviewableBy query now properly excludes issues where session is creator or in session history (not just implementer)
- Session migration now cleans up old session files after successful migration to agent-scoped format

### Tests
- Added `TestReviewableByFilter` with comprehensive scenarios covering creator, implementer, and session history bypass prevention
- Added tests for `ExplicitID` in agent fingerprint `String()` method

### Documentation
- Added release guide at `docs/guides/releasing-new-version.md` with step-by-step instructions
- Moved completed feature specifications to `docs/implemented/`

## [v0.4.25] - 2025-12-20

### Bug Fixes
- Epic create command now correctly sets issue type to epic

## [v0.4.24] - 2025-12-20

### Documentation
- Added warnings in developer guides about not starting new sessions mid-work (bypasses review)

## [v0.4.23] - 2025-12-19

### Bug Fixes
- Fixed mouse scroll and click offset issues in monitor TaskList

## [v0.4.22] - 2025-12-19

### Bug Fixes
- Removed dead code related to self-close enforcement

### Documentation
- Updated docs for self-close exception workflow

## [v0.4.21] - 2025-12-18

### Changed
- Updated review workflow process

## [v0.4.20] - 2025-12-17

### Features
- Improved agent-friendly interface with better CLI messages

### UI
- Enhanced td monitor modal styling and interactions

---

## Release Process

When releasing a new version:

1. **Update CHANGELOG.md** with new version at the top
2. **Follow semver** (Major.Minor.Patch):
   - Major: Breaking changes
   - Minor: New features (backward compatible)
   - Patch: Bug fixes only
3. **Create annotated git tag**: `git tag -a vX.Y.Z -m "Release vX.Y.Z: description"`
4. **Push commits and tag**: `git push origin main && git push origin vX.Y.Z`
5. **Create GitHub release** with release notes (can auto-generate from commits)
6. **Install with version**: `go install -ldflags "-X main.Version=vX.Y.Z" ./...`

See `docs/guides/releasing-new-version.md` for detailed instructions.
