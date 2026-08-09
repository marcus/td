# Design Spec: `td delegate` — Explicit Implementer/Reviewer Roles

Status: design only — no implementation in epic `td-124499`. This note defines the
canonical identity model and the migration story from context_id keying.
Created: 2026-06-20
Task: `td-d7fd80` (parent epic `td-124499`)
Related:
- `docs/plans/session-worktree-flow-recommendations.md` (source plan; "Controls That
  Have Aged → Explicit Delegation Over Inferred Identity", Failure Modes #5/#6, Decision Log)
- `docs/plans/review-policy-trusted-mode-plan.md`
- `docs/plans/orchestrator-review-closure-plan.md`

## Planning decision this spec implements (recorded faithfully)

From the `td-124499` Decision Log (2026-06-20):

> **Identity model = both, sequenced.** context_id keying is the shipping *mechanism*
> now; `td delegate` (explicit role declaration) is the eventual *canonical* model.
> `td delegate` is spec-only in this epic so the two don't become two half-supported paths.

context_id keying **shipped** in `td-64dc09`. This spec defines the canonical model
(`td delegate`) and — critically — the precedence rule and migration path so that
declared roles and session inference are **one layered system**, not two competing ones.

The headline rule: **declared role wins; fall back to session inference when no
declaration exists.** Delegation is *additive*. We never delete `issue_session_history`
or stop recording session involvement — we add a higher-precedence, explicit signal
on top of it.

---

## 1. Problem: review independence is inferred from session-identity accidents

Today, "were these the same actor?" is reverse-engineered from session identity, which
is itself derived from branch + agent fingerprint + (now) context_id. Two concrete
failure shapes:

### Too strict — sub-agents collapse (Failure Mode #6)

Before `td-64dc09`, every sub-agent spawned in the same checkout/branch resolved to the
**same** session, because the agent fingerprint keys off inherited terminal env markers
(`getContextID()` in `internal/session/session.go:128-140` checks `TERM_SESSION_ID`,
`TMUX_PANE`, etc. before ppid, and children inherit those). The orchestrator's
implementer sub-agent and reviewer sub-agent shared one `session_id`, so
`EvaluateReviewerEligibility` saw the reviewer as `SessionIsImplementer == true` and
blocked the close as a self-review (`internal/reviewpolicy/policy.go:244-249`,
`:261-281`). The orchestrator was forced into blanket `--self-review`, which then
*understates* the independence that actually occurred.

`td-64dc09` partially fixed this by promoting `TD_CONTEXT_ID` into the lookup key
(`matchContextID()` at `session.go:105-107`, `GetSessionByIdentity` at
`internal/db/sessions.go:51-57`, key = `branch + agent_type + agent_pid +
match_context_id`). When the harness sets a distinct `TD_CONTEXT_ID` per sub-agent, the
implementer and reviewer now get distinct sessions and independence is representable.

### Too loose — context rotation fabricates independence (Failure Mode #5)

The inverse weakness still exists. `td usage --new-session` / `ForceNewSession`
(`session.go:227-239`) mints a brand-new `session_id` and only records
`previous_session_id` for display. Policy checks ask "did *this exact session_id* touch
the issue?" via `WasSessionImplementationInvolved` (`internal/db/issue_relations.go:753-762`)
and `issue.ImplementerSession == sessionID` (`cmd/review_policy.go:151`). So an agent
that implements an issue, then `/clear`s or rotates its context, comes back as a
*different* session and can approve its own work with **no** `--self-review`
acknowledgement — independence that did not actually happen.

### Root cause

Both failures share one root cause: **independence is inferred from an identity
primitive that was never designed to carry that meaning.** `context_id` keying makes the
inference *more accurate* when the harness cooperates, but it is still inference. It
depends on env-var hygiene the orchestrator may or may not get right, and it cannot
distinguish "deliberately delegated to an independent reviewer" from "two processes
happened to differ in `TD_CONTEXT_ID`."

The fix is to stop inferring and let the orchestrator **declare** who is doing what.

---

## 2. Proposed model: `td delegate`

### Concept

A *declared role* is an explicit statement, recorded against an issue, that a named
actor is responsible for a role (implementer or reviewer). It is distinct from a
*session involvement row*:

| | Session involvement (`issue_session_history`) | Declared role (`issue_role_delegations`) |
|---|---|---|
| Source | Side effect of `td start` / `td review` / etc. | Explicit `td delegate` command |
| Identity unit | Exact `session_id` | A **delegation label** (logical actor), resolvable to a session |
| Meaning | "this session performed this action" | "this actor is *assigned* this role" |
| Mutability | Append-only audit trail | Latest declaration per (issue, role) wins; supersedable |
| Precedence in policy | Fallback when no declaration exists | Authoritative when present |

The key shift: eligibility is evaluated against the **declared implementer actor**, not
the guessed session match.

### Command surface

```
td delegate <id> --implementer <label>   # declare who implements
td delegate <id> --reviewer <label>      # declare who reviews
td delegate <id> --show                  # print current declared roles for the issue
td delegate <id> --clear-implementer     # remove a declaration (revert to inference)
td delegate <id> --clear-reviewer
```

`<label>` is a stable, orchestrator-chosen string identifying a logical actor —
typically the same value the harness puts in `TD_CONTEXT_ID` for that sub-agent (e.g.
`impl-auth`, `reviewer-1`). The recommended convention is **label == the sub-agent's
`TD_CONTEXT_ID`**, which is what makes the migration in §4 seamless: a declared label
and an inferred session's `match_context_id` describe the same actor.

Flags:
- `--label-source session` (default `context-id`): when omitted, `<label>` defaults to
  the calling session's `match_context_id` if set, else its `session_id`. Lets a
  sub-agent self-declare ("I am the implementer") without the orchestrator knowing its
  label in advance.
- `--reason "..."`: optional free-text rationale, recorded for audit.

#### Who may delegate

To keep delegation from becoming a self-approval bypass, declaration of the
**implementer** role is *open* (anyone may claim implementer — claiming work is not a
privilege), but declaration of the **reviewer** role for a non-minor issue follows the
same independence intuition: the recorded implementer actor should not be able to also
declare itself reviewer without the `--self-review` acknowledgement landing downstream
at approve time (see §3). `td delegate --reviewer` itself only *records intent*; the
actual gate stays at `td approve`, so delegation never widens what approve allows — it
only makes the approve-time check evaluate against a declared actor.

### What gets recorded — proposed schema

A new table (additive; does **not** replace `issue_session_history`):

```sql
CREATE TABLE issue_role_delegations (
    id              TEXT PRIMARY KEY,
    issue_id        TEXT NOT NULL,
    role            TEXT NOT NULL,          -- 'implementer' | 'reviewer'
    actor_label     TEXT NOT NULL,          -- orchestrator-chosen logical actor id
    declared_by_session TEXT NOT NULL,      -- session_id that ran `td delegate`
    resolved_session_id TEXT DEFAULT '',    -- session bound to actor_label once observed (nullable until bound)
    reason          TEXT DEFAULT '',
    superseded      INTEGER NOT NULL DEFAULT 0,  -- 0 = active, 1 = replaced by a later declaration
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_role_deleg_issue_role
    ON issue_role_delegations (issue_id, role, superseded);
```

Semantics:
- "Active declared implementer" for an issue = the row with
  `role='implementer' AND superseded=0` (at most one; re-declaring supersedes the prior).
- `actor_label` is the logical identity. `resolved_session_id` is bound the first time a
  session whose `match_context_id` (or `session_id`) equals `actor_label` performs a
  recorded action on the issue, letting policy translate between the declared label and
  concrete session-history rows.
- `declared_by_session` is pure audit ("orchestrator O assigned this role").

**Alternative considered — columns on `issues` instead of a table.** The `issues` row
already has `implementer_session` / `reviewer_session` (`internal/models/models.go:96-99`).
We could add `declared_implementer_label` / `declared_reviewer_label` columns. Rejected
as the primary store because: (a) it loses the supersession audit trail (who reassigned,
when, why), and (b) the `*_session` columns are the *resolved/observed* actor for
display and sync, and conflating "assigned" with "observed" is exactly the ambiguity
this design removes. We *do* keep the `issues.*_session` columns as the resolved view
(see §3) but treat the delegation table as the source of declared intent.

### Example CLI flows

**Orchestrator delegating implement + review to two sub-agents:**

```bash
# Orchestrator creates work and declares roles up front
td add "Refactor auth" --type feature                 # -> td-a1b2
td delegate td-a1b2 --implementer impl-auth
td delegate td-a1b2 --reviewer reviewer-1

# Implementer sub-agent runs with TD_CONTEXT_ID=impl-auth
TD_CONTEXT_ID=impl-auth td start td-a1b2
TD_CONTEXT_ID=impl-auth td log "implemented auth refactor"
TD_CONTEXT_ID=impl-auth td review td-a1b2

# Reviewer sub-agent runs with TD_CONTEXT_ID=reviewer-1.
# Policy checks against the DECLARED implementer (impl-auth), not a session guess.
# reviewer-1 != impl-auth -> independent -> no --self-review needed.
TD_CONTEXT_ID=reviewer-1 td approve td-a1b2 --record-only --reason "diff + tests pass"

# Any session closes using the recorded approval.
td approve td-a1b2 --reason "closing after recorded independent approval"
```

**Sub-agent self-declaring (label defaults from its own context):**

```bash
TD_CONTEXT_ID=impl-auth td delegate td-a1b2 --implementer   # label defaults to "impl-auth"
```

**Self-review made meaningful again (same declared actor does both):**

```bash
TD_CONTEXT_ID=solo td delegate td-a1b2 --implementer solo
TD_CONTEXT_ID=solo td review td-a1b2
# reviewer actor == declared implementer "solo" -> genuine self-review -> flag required
TD_CONTEXT_ID=solo td approve td-a1b2 --self-review --reason "reviewed my own diff"
```

---

## 3. Eligibility semantics

The policy package stays pure (data in, decision out — `internal/reviewpolicy/policy.go:11-15`).
The change is in **how callers populate the input booleans** plus a small set of new
input fields. The decision functions themselves change minimally.

### What changes in `reviewpolicy`

Add **declared-actor** facts to `ReviewerEligibilityInput` and `CloseEligibilityInput`,
parallel to the existing session-inference fields:

```go
type ReviewerEligibilityInput struct {
    // ... existing fields ...

    // Declared-role facts. When HasDeclaredImplementer is true, these REPLACE
    // the session-inference signals (SessionIsImplementer / HasImplementationHistory)
    // for the self-review decision. When false, the existing inference path runs
    // unchanged (full backward compatibility).
    HasDeclaredImplementer bool   // an active implementer delegation exists for the issue
    CallerIsDeclaredImplementer bool // caller's actor_label == active declared implementer label
}
```

(Same two fields added to `CloseEligibilityInput`.)

### The precedence rule, in code

The reviewer/close predicates gain one guard at the top of the trusted/delegated paths.
Conceptually, replace the trigger condition

```go
// today (session inference):
isSelfReview := in.SessionIsImplementer || in.HasImplementationHistory
```

with

```go
// proposed (declared role wins; fall back to inference):
var isSelfReview bool
if in.HasDeclaredImplementer {
    isSelfReview = in.CallerIsDeclaredImplementer        // declared actor identity
} else {
    isSelfReview = in.SessionIsImplementer || in.HasImplementationHistory  // legacy inference
}
```

This single substitution flows through `evaluateReviewerTrusted`
(`policy.go:261-281`), `evaluateReviewerDelegated` (`policy.go:234-250`), and the
Case-2 direct-close paths (`evaluateCloseDelegated` `policy.go:399-406`,
`evaluateCloseTrusted` `policy.go:442-450`) because they all share the same
`SessionIsImplementer || HasImplementationHistory` trigger. Centralize it in a helper —
e.g. `func (in ReviewerEligibilityInput) isImplementerSelfReview() bool` — so all four
sites compute it identically.

Effect on the two failure modes:
- **FM#6 (too strict):** when the orchestrator declared `impl-auth` and `reviewer-1`,
  the reviewer's `CallerIsDeclaredImplementer` is false even if a fingerprint accident
  collapsed their sessions. `isSelfReview == false` → independent review allowed, no
  flag. Independence is represented because it was *declared*.
- **FM#5 (too loose):** when `solo` implemented and then rotated context, the new
  session still resolves to `actor_label == solo`, so
  `CallerIsDeclaredImplementer == true` → `isSelfReview == true` → `--self-review`
  required. Context rotation no longer fabricates independence.

### When `--self-review` should fire

`--self-review` is required **iff the caller's declared actor equals the active declared
implementer actor** (and the issue is non-minor — minor still short-circuits at
`policy.go:176-178`/`:325-327`). It does **not** fire merely because two sessions share
a process, nor is it skipped merely because a context rotated. The acknowledgement
becomes a statement about *declared actors*, which is what the orchestrator actually
controls.

When no delegation exists, `--self-review` fires under exactly today's rules — zero
behavior change for projects that never call `td delegate`.

### How callers populate the new fields

Callers (e.g. `cmd/review_policy.go:147-156`, `internal/serve/handlers_transitions.go`,
`pkg/monitor/`) gain:
- `db.GetActiveDeclaredImplementer(issueID) (label string, ok bool, err error)` →
  populates `HasDeclaredImplementer`.
- Resolve the caller's own actor label: `session.MatchContextID` if set, else
  `session.ID`. Compare to the active label → `CallerIsDeclaredImplementer`.

`issues.implementer_session` continues to be set by `td start` as the *observed*
implementer and remains the resolved/display/sync value. Delegation does not change what
`td start` writes; it adds a higher-precedence signal that the policy layer consults
first.

---

## 4. Migration story: context_id keying → declared roles

Delegation is **additive and precedence-based**, never a replacement event. Three
coexisting layers, highest precedence first:

1. **Declared role** (`issue_role_delegations`) — authoritative when an active row exists.
2. **context_id-keyed session inference** — the current shipped mechanism; used when no
   declaration exists. `match_context_id` already gives accurate per-sub-agent sessions
   when the harness cooperates.
3. **Legacy session inference** (`session_id` exact match) — the floor; unchanged for
   interactive single-agent use where neither delegation nor `TD_CONTEXT_ID` is set.

### Precedence rule (canonical)

> For the self-review / independence decision: if an active declared implementer exists
> for the issue, evaluate against the declared actor label. Otherwise, evaluate against
> session involvement (which already benefits from context_id keying). Never both.

This guarantees there are not "two half-supported paths": there is *one* path with a
deterministic precedence. Declaration is opt-in per issue; absent it, nothing changes.

### What happens to `issue_session_history`

Nothing is removed. It stays the append-only audit trail of "which session did what."
Delegation adds the `resolved_session_id` binding so policy can still join declared
labels to history rows when it needs concrete session facts (e.g. for display, or to
confirm the declared implementer actually ran `td start`). Recommended invariant:
a `td review` by a session whose actor label is the declared implementer should *warn*
if no `started`/`unstarted` history row exists for that actor — catches "declared but
never actually implemented" drift without blocking.

### Sequencing (deliberate, not simultaneous)

- **Now (shipped):** context_id keying. Orchestrators get independence by setting
  `TD_CONTEXT_ID` per sub-agent.
- **Next (this spec → implementation tasks):** add `td delegate` + the delegation table
  + the precedence guard in `reviewpolicy`. Because the recommended label == the
  sub-agent's `TD_CONTEXT_ID`, an orchestrator already setting `TD_CONTEXT_ID` can adopt
  delegation incrementally, issue by issue, with no flag-day cutover.
- **Eventually (canonical):** declaration becomes the documented, preferred model for
  multi-agent orchestration. context_id keying does not go away — it remains the
  zero-config mechanism and the binding bridge (`match_context_id` → `actor_label`) —
  but the *audit story* shifts from "we inferred independence from session identity" to
  "the orchestrator declared who did what." No deprecation of context_id keying is
  proposed or needed.

### Sync boundary

`issue_role_delegations` is issue-scoped declared state (not per-machine working
context), so unlike the deferred `session_state` table it is a **candidate to sync**
with the issue. Flag for a product decision (see §6): syncing delegations means a remote
session's declared role is visible to all peers, which is desirable for distributed
multi-agent work but adds a sync surface. Recommendation: sync it, treating
`resolved_session_id` as advisory (machine-local sessions don't sync, so the *label* is
the portable identity and `resolved_session_id` may be empty on peers).

---

## 5. Relationship to deferred lineage work (Phase 4)

The source plan's Phase 4 proposes `lineage_id` on sessions plus
`WasLineageImplementationInvolved`, so that `td usage --new-session` rotation is
recognized as the *same actor* (`session-worktree-flow-recommendations.md:208-229`,
`:304-312`). That is the *inference-side* fix for FM#5: make the inferred identity
durable across context resets.

Delegation and lineage are **complementary, not redundant**, but delegation makes
lineage **lower priority**:

- Delegation solves FM#5 *when a role was declared* — the declared `actor_label` is
  already durable across rotation, so the rotated session still resolves to the same
  declared actor. No lineage needed in that case.
- Lineage solves FM#5 *for the inference fallback* — i.e. when no delegation exists and
  the agent rotated context. This is the un-declared, single-agent path.

Recommendation: ship delegation first (it is the canonical model and covers the
orchestrated multi-agent case, which is the painful one). Treat `lineage_id` as an
*optional later refinement* to harden the inference fallback for un-declared rotation.
If delegation adoption is high, lineage may never be worth the schema cost — keep it
deferred and timeboxed exactly as the parent epic recorded. Where both exist, precedence
is: declared role > lineage inference > exact-session inference.

---

## 6. Open questions / risks → need maintainer decision

1. **Should `issue_role_delegations` sync?** (§4) Recommendation: yes, with
   `resolved_session_id` advisory. **Product decision — affects the sync schema/contract.**
2. **Label namespace collisions.** Two unrelated issues can both use `actor_label=reviewer-1`.
   That is fine (delegation is per-issue), but `td delegate --show` and any cross-issue
   "what is reviewer-1 working on" view must scope by issue. Low risk; note in docs.
3. **Open implementer-claim vs. abuse.** Implementer declaration is open (§2). A
   malicious/confused agent could declare a *different* actor as implementer to make its
   own later review look independent. Mitigation: the `td review` warning when the
   declared implementer has no `started` history row; optionally require that the
   declared implementer actor has actually run `td start` before an independent review
   counts. **Decision: enforce or warn-only?** Recommend warn-only in trusted mode
   (consistent with trusted's "rich record, fewer gates" posture).
4. **Reviewer declaration vs. approve-time gate.** This spec keeps the real gate at
   `td approve` and treats `--reviewer` as intent-recording only, so delegation never
   widens approve. Confirm that is the desired split (vs. letting `--reviewer` pre-authorize).
5. **Interaction with `--minor`.** Minor issues bypass all self-review checks today
   (`policy.go:176-178`). Delegation should respect that short-circuit unchanged.
6. **Default label source.** `--label-source` defaulting to `match_context_id` then
   `session_id` (§2) — confirm precedence. Risk: if an orchestrator forgets
   `TD_CONTEXT_ID`, the label silently becomes a raw `session_id`, which is fine but less
   readable in audit output.

### Honest scope notes

- This is the *canonical-model design*, not a committed build plan. The schema and
  command surface are concrete proposals; the maintainer decisions in items 1, 3, 4 are
  genuine forks that change the surface.
- No production code or migration is written here. The `reviewpolicy` change is small
  and well-localized (one shared helper + two input fields), which is the main signal
  that delegation slots cleanly onto the existing trusted-mode machinery rather than
  rewriting it.

---

## 7. Phased implementation breakdown (follow-up tasks)

Proposed phases, each a self-contained task. These are created under `td-124499` for
now and may be regrouped into a dedicated delegation epic once the maintainer signs off
on the schema (open question §6.1).

- **D1 — Schema + DB layer.** Add `issue_role_delegations` table + migration; DB helpers
  `RecordDelegation`, `GetActiveDeclaredImplementer`, `GetActiveDeclaredReviewer`,
  `BindDelegationSession` (resolve `actor_label` → `resolved_session_id`). No CLI yet.
- **D2 — `td delegate` command.** CLI surface from §2 (`--implementer`, `--reviewer`,
  `--show`, `--clear-*`, `--label-source`, `--reason`), label defaulting from
  `match_context_id`/`session_id`, supersession on re-declare.
- **D3 — Policy precedence guard.** Add `HasDeclaredImplementer` /
  `CallerIsDeclaredImplementer` to `ReviewerEligibilityInput` + `CloseEligibilityInput`;
  introduce the shared `isImplementerSelfReview()` helper; route all four predicate
  sites through it. Pure-package tests for FM#5 and FM#6 scenarios.
- **D4 — Wire callers.** Populate the new inputs in `cmd/review_policy.go`,
  `internal/serve/handlers_transitions.go`, `pkg/monitor/`. Parity tests across surfaces.
- **D5 — Audit + display.** `td delegate --show`, surface declared roles in `td show`
  and the monitor; the "declared-but-never-started" warning at `td review`.
- **D6 — Sync (gated on §6.1 decision).** Sync `issue_role_delegations` with the issue;
  treat `resolved_session_id` as advisory on peers. Only if maintainer approves syncing.
- **D7 — Docs + adoption.** Update orchestrator guidance (CLAUDE.md / sync-agent-guide)
  to present delegation as the canonical multi-agent model with `TD_CONTEXT_ID` as the
  binding bridge; document precedence and the interim context_id-only path.

Lineage (`lineage_id`, source-plan Phase 4) stays deferred and is explicitly *not* in
this breakdown — see §5.

### Created follow-up tasks

Stubbed under `td-124499` (regroup into a dedicated delegation epic pending §6.1):

- `td-50e97b` — D1: schema + DB layer
- `td-df7524` — D2: `td delegate` command surface
- `td-7c0329` — D3: declared-role precedence in `reviewpolicy`
- `td-af7805` — D4: wire delegation inputs into policy callers
- `td-434692` — D5: audit + display + drift warning
- `td-ca4d95` — D6: sync delegations (gated on §6.1)
- `td-f3d976` — D7: docs + orchestrator adoption
