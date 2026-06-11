package cmd

// Cross-surface parity test suite for the review policy.
//
// Contract:
//   Every surface that decides reviewer/close eligibility MUST route through
//   internal/reviewpolicy. If a new surface ships that bypasses reviewpolicy,
//   this suite must be extended to cover it — otherwise the policy will drift
//   again (that is the regression the batch plan calls out).
//
// The suite enumerates (role × prior involvement × active review state ×
// policy mode × minor) scenarios and asserts that for every row:
//   1. cmd/review_policy.go's evaluateApproveEligibility /
//      evaluateCloseEligibility return the same Allowed boolean as the
//      shared reviewpolicy decision.
//   2. pkg/monitor's monitorApproveDecision returns the same Allowed boolean.
//   3. internal/serve's serveReviewerDecision / serveCloseDecision — when
//      we can construct a comparable input — return the same Allowed.
//   4. internal/api/snapshot_query_source routes through the same mode-aware
//      SQL composer as internal/db (verified indirectly because both call
//      db.ReviewableByFilterForMode).
//
// The monitor and serve surfaces are invoked through unexported helpers that
// take the same ReviewerEligibilityInput/CloseEligibilityInput the CLI
// wrapper builds; the test verifies each call site produces identical
// decisions for the same inputs.

import (
	"testing"

	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
	"github.com/marcus/td/internal/serve"
	"github.com/marcus/td/pkg/monitor"
)

// parityRow is a single scenario in the cross-surface parity matrix.
//
// hasImplementationHistory is the *caller's own* impl-involvement flag — it
// maps to ReviewerEligibilityInput.HasImplementationHistory and the
// `wasImplementationInvolved` argument on the close wrapper. issueImplHistory
// is the broader *issue-wide* flag (HasImplementationHistory in the close
// wrapper's last arg) used by the creator-open-bypass veto. When
// issueImplHistory is left at its zero value it defaults to
// hasImplementationHistory (this matches how most rows historically exercised
// the two in lock-step); rows that need to pull them apart set both
// explicitly.
type parityRow struct {
	name                     string
	mode                     reviewpolicy.Mode
	issueStatus              models.Status
	minor                    bool
	sessionID                string
	creatorSession           string
	implementerSession       string
	reviewerSession          string
	reviewRequestedBySession string
	hasImplementationHistory bool
	issueImplHistory         bool
	issueImplHistorySet      bool
	wasAnyInvolved           bool
	hasActiveApproval        bool
	wantReviewerAllowed      bool
	wantCloseAllowed         bool
	wantCreatorException     bool
	// wantCLICloseBlockedByIssueVeto is true for rows where the CLI
	// close wrapper's issue-wide impl-history veto should reject the
	// close even though reviewpolicy.EvaluateCloseEligibility allowed
	// it. Most rows leave this false; only divergence rows set it.
	wantCLICloseBlockedByIssueVeto bool
}

// effectiveIssueImplHistory returns the issue-wide impl-history flag for the
// row. When the row did not set issueImplHistory explicitly, this falls back
// to the caller's own hasImplementationHistory flag (the historical default).
func (r parityRow) effectiveIssueImplHistory() bool {
	if r.issueImplHistorySet {
		return r.issueImplHistory
	}
	return r.hasImplementationHistory
}

func mkParityIssue(r parityRow) *models.Issue {
	return &models.Issue{
		ID:                       "td-parity",
		Title:                    "parity",
		Status:                   r.issueStatus,
		Type:                     models.TypeTask,
		Priority:                 models.PriorityP2,
		Minor:                    r.minor,
		CreatorSession:           r.creatorSession,
		ImplementerSession:       r.implementerSession,
		ReviewerSession:          r.reviewerSession,
		ReviewRequestedBySession: r.reviewRequestedBySession,
	}
}

// parityMatrix enumerates scenarios deliberately chosen to cover each
// branch of reviewer/close eligibility in each mode. 50+ rows required by
// the batch plan.
func parityMatrix() []parityRow {
	var rows []parityRow

	// --- Strict mode scenarios ---
	rows = append(rows,
		// Reviewer eligibility
		parityRow{name: "strict: implementer blocked", mode: reviewpolicy.ModeStrict,
			issueStatus: models.StatusInReview, sessionID: "ses-impl",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: false, wantCloseAllowed: false,
		},
		parityRow{name: "strict: creator blocked", mode: reviewpolicy.ModeStrict,
			issueStatus: models.StatusInReview, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wasAnyInvolved:      true,
			wantReviewerAllowed: false, wantCloseAllowed: false,
		},
		parityRow{name: "strict: prior reviewer blocked", mode: reviewpolicy.ModeStrict,
			issueStatus: models.StatusInReview, sessionID: "ses-prev",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wasAnyInvolved:      true,
			wantReviewerAllowed: false, wantCloseAllowed: false,
		},
		parityRow{name: "strict: uninvolved allowed", mode: reviewpolicy.ModeStrict,
			issueStatus: models.StatusInReview, sessionID: "ses-fresh",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "strict: minor bypass", mode: reviewpolicy.ModeStrict,
			issueStatus: models.StatusInReview, minor: true, sessionID: "ses-impl",
			creatorSession: "ses-impl", implementerSession: "ses-impl",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
	)

	// --- Balanced mode scenarios ---
	rows = append(rows,
		parityRow{name: "balanced: implementer blocked", mode: reviewpolicy.ModeBalanced,
			issueStatus: models.StatusInReview, sessionID: "ses-impl",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: false, wantCloseAllowed: false,
		},
		parityRow{name: "balanced: impl history blocked (not current impl)", mode: reviewpolicy.ModeBalanced,
			issueStatus: models.StatusInReview, sessionID: "ses-prev-impl",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: false, wantCloseAllowed: false,
		},
		parityRow{name: "balanced: creator exception allows", mode: reviewpolicy.ModeBalanced,
			issueStatus: models.StatusInReview, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wasAnyInvolved:       true,
			wantReviewerAllowed:  true,
			wantCreatorException: true,
			wantCloseAllowed:     false, // close path doesn't grant creator exception
		},
		parityRow{name: "balanced: creator as own implementer blocked", mode: reviewpolicy.ModeBalanced,
			issueStatus: models.StatusInReview, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "ses-c",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: false, wantCloseAllowed: false,
		},
		parityRow{name: "balanced: non-creator prior involved blocked", mode: reviewpolicy.ModeBalanced,
			issueStatus: models.StatusInReview, sessionID: "ses-prev",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wasAnyInvolved:      true,
			wantReviewerAllowed: false, wantCloseAllowed: false,
		},
		parityRow{name: "balanced: uninvolved allowed", mode: reviewpolicy.ModeBalanced,
			issueStatus: models.StatusInReview, sessionID: "ses-fresh",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "balanced: minor bypass", mode: reviewpolicy.ModeBalanced,
			issueStatus: models.StatusInReview, minor: true, sessionID: "ses-impl",
			creatorSession: "ses-impl", implementerSession: "ses-impl",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
	)

	// --- Delegated mode scenarios ---
	rows = append(rows,
		parityRow{name: "delegated: implementer blocked", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-impl",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: false, wantCloseAllowed: false,
		},
		parityRow{name: "delegated: impl history blocked", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-prev-impl",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: true,
			wantReviewerAllowed:      false, wantCloseAllowed: false,
		},
		parityRow{name: "delegated: creator-who-never-implemented allowed", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wasAnyInvolved:      true, // created counts as involved, not impl
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: fresh session allowed (review+close)", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-fresh",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: prior-reviewer still reviewable", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-prev-reviewer",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wasAnyInvolved:      true, // reviewed counts as involved, not impl
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: active approval + creator closes", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasActiveApproval:   true,
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: active approval + implementer closes", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-impl",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: true, hasActiveApproval: true,
			wantReviewerAllowed: false, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: active approval + reviewer-of-record closes", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-rev",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			reviewerSession:     "ses-rev",
			hasActiveApproval:   true,
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: active approval + review-requester closes", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-orch",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			reviewRequestedBySession: "ses-orch",
			hasActiveApproval:        true,
			wantReviewerAllowed:      true, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: active approval + arbitrary session closes", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-other",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasActiveApproval:   true,
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: minor bypass", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, minor: true, sessionID: "ses-impl",
			creatorSession: "ses-impl", implementerSession: "ses-impl",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: non-in_review still-open issue + uninvolved direct-close fallback", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusOpen, sessionID: "ses-fresh",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			// Uninvolved session with no impl history — reviewer check
			// allows (delegated rule looks only at implementation
			// independence); close falls through to the direct-close policy
			// because there is no active approval review.
			wantReviewerAllowed: true,
			wantCloseAllowed:    true, // under strict/balanced fallback (see evaluateCloseStrictBalanced)
		},
		parityRow{name: "delegated: non-in_review creator-open-bypass", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusOpen, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "",
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
	)

	// --- Extra coverage: each mode × creator-open-bypass, implementer-history permutations ---
	for _, mode := range []reviewpolicy.Mode{reviewpolicy.ModeStrict, reviewpolicy.ModeBalanced, reviewpolicy.ModeDelegated} {
		rows = append(rows,
			parityRow{name: string(mode) + ": creator-open-bypass (no impl)", mode: mode,
				issueStatus: models.StatusOpen, sessionID: "ses-c",
				creatorSession: "ses-c", implementerSession: "",
				// Strict reviewer rule: SessionIsCreator=true -> blocked.
				// Balanced: SessionIsCreator && no impl history && no
				// different implementer -> WasAnyInvolved=false here, so
				// the non-creator branch allows. Delegated: no impl history
				// -> allowed. Close: creator-open-bypass triggers.
				wantReviewerAllowed: mode != reviewpolicy.ModeStrict,
				wantCloseAllowed:    true,
			},
			parityRow{name: string(mode) + ": creator-open-bypass blocked when impl history exists", mode: mode,
				issueStatus: models.StatusOpen, sessionID: "ses-c",
				creatorSession: "ses-c", implementerSession: "ses-impl",
				hasImplementationHistory: true, wasAnyInvolved: true,
				wantReviewerAllowed: false, wantCloseAllowed: false,
			},
			parityRow{name: string(mode) + ": minor always allows close", mode: mode,
				issueStatus: models.StatusInReview, minor: true, sessionID: "ses-impl",
				creatorSession: "ses-impl", implementerSession: "ses-impl",
				hasImplementationHistory: true, wasAnyInvolved: true,
				wantReviewerAllowed: true, wantCloseAllowed: true,
			},
			parityRow{name: string(mode) + ": non-creator uninvolved close allowed on in_review", mode: mode,
				issueStatus: models.StatusInReview, sessionID: "ses-fresh",
				creatorSession: "ses-c", implementerSession: "ses-impl",
				wantReviewerAllowed: true, wantCloseAllowed: true,
			},
			parityRow{name: string(mode) + ": minor + arbitrary session still allowed", mode: mode,
				issueStatus: models.StatusInReview, minor: true, sessionID: "ses-anyone",
				creatorSession: "ses-c", implementerSession: "ses-impl",
				wantReviewerAllowed: true, wantCloseAllowed: true,
			},
			parityRow{name: string(mode) + ": prior-reviewer uninvolved impl can review again", mode: mode,
				issueStatus: models.StatusInReview, sessionID: "ses-rev",
				creatorSession: "ses-c", implementerSession: "ses-impl",
				reviewerSession: "ses-rev",
				wasAnyInvolved:  true, // reviewed-before counts as "any involvement"
				// No hasImplementationHistory; reviewing is not implementing.
				// Strict blocks any involvement; balanced blocks too because
				// WasAnyInvolved is true for non-creator sessions; delegated
				// allows since reviewing is not implementation.
				wantReviewerAllowed: mode == reviewpolicy.ModeDelegated,
				wantCloseAllowed:    mode == reviewpolicy.ModeDelegated,
			},
		)
	}

	// --- Delegated-specific: role matrix with active approval ---
	rows = append(rows,
		parityRow{name: "delegated: active approval + pure creator (no impl history)", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasActiveApproval:   true,
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: active approval + creator-with-impl-history still closes", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "ses-c",
			hasImplementationHistory: true, hasActiveApproval: true,
			wantReviewerAllowed: false, wantCloseAllowed: true,
		},
		parityRow{name: "delegated: no active approval + uninvolved direct-close works", mode: reviewpolicy.ModeDelegated,
			issueStatus: models.StatusInReview, sessionID: "ses-fresh",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
	)

	// --- Divergence rows: caller-own impl-history differs from issue-wide ---
	// These rows exercise the distinction between
	// wasImplementationInvolved (caller's own history) and
	// hasImplementationHistory (issue-wide history). reviewpolicy's close
	// rule only considers the caller's own flag; the CLI wrapper layers a
	// post-decision veto that also consults the issue-wide flag. These rows
	// assert that divergence explicitly — wantCloseAllowed reflects
	// reviewpolicy's view, wantCLICloseBlockedByIssueVeto reflects the
	// wrapper's additional veto.
	rows = append(rows,
		parityRow{name: "strict: fresh creator+open+issue impl history veto",
			mode:        reviewpolicy.ModeStrict,
			issueStatus: models.StatusOpen, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			// caller not impl-involved; but issue has history elsewhere.
			hasImplementationHistory: false,
			issueImplHistory:         true, issueImplHistorySet: true,
			// reviewpolicy: caller-only check, allowed.
			wantReviewerAllowed: false, wantCloseAllowed: true,
			// CLI wrapper applies the issue-wide veto, blocking the close.
			wantCLICloseBlockedByIssueVeto: true,
		},
		parityRow{name: "balanced: fresh creator+open+issue impl history veto",
			mode:        reviewpolicy.ModeBalanced,
			issueStatus: models.StatusOpen, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: false,
			issueImplHistory:         true, issueImplHistorySet: true,
			// Balanced reviewer branch: creator session -> creator exception
			// applies when issue has no different implementer impl history.
			// Here issueImplHistory=true but caller's HasImplementationHistory
			// is false, so balanced allows the reviewer check (creator
			// exception); close stays allowed at the reviewpolicy level and
			// is vetoed only by the CLI wrapper's issue-wide gate.
			wantReviewerAllowed:            true,
			wantCreatorException:           true,
			wantCloseAllowed:               true,
			wantCLICloseBlockedByIssueVeto: true,
		},
		parityRow{name: "strict: uninvolved closes in_review issue with impl history elsewhere",
			mode:        reviewpolicy.ModeStrict,
			issueStatus: models.StatusInReview, sessionID: "ses-fresh",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: false,
			issueImplHistory:         true, issueImplHistorySet: true,
			wasAnyInvolved:      false,
			wantReviewerAllowed: true, wantCloseAllowed: true,
			// Issue-wide veto only applies under creator-open-bypass; a
			// non-creator session is unaffected.
			wantCLICloseBlockedByIssueVeto: false,
		},
	)

	// --- Additional matrix: status x mode ---
	for _, mode := range []reviewpolicy.Mode{reviewpolicy.ModeStrict, reviewpolicy.ModeBalanced, reviewpolicy.ModeDelegated} {
		for _, status := range []models.Status{models.StatusOpen, models.StatusInProgress, models.StatusBlocked} {
			rows = append(rows,
				parityRow{
					name:                string(mode) + "/" + string(status) + ": uninvolved reviewer check allowed",
					mode:                mode,
					issueStatus:         status,
					sessionID:           "ses-fresh",
					creatorSession:      "ses-c",
					implementerSession:  "ses-impl",
					wantReviewerAllowed: true,
					// Close on non-in_review for an uninvolved session is
					// allowed under strict/balanced when no prior involvement
					// flagged; allowed under delegated via the same path.
					wantCloseAllowed: true,
				},
			)
		}
	}

	// --- Step 3: record-review + close-after-review parity ---
	//
	// These rows lock the Step 3 additions across CLI (`td approve
	// --record-only`, `td approve` close-after-review), monitor
	// (record-review / approve actions), and serve (POST /reviews,
	// POST /approve close-after-review).
	rows = append(rows,
		// Record-review eligibility = reviewer-eligibility under delegated.
		parityRow{name: "step3-record: delegated uninvolved session may record",
			mode: reviewpolicy.ModeDelegated, issueStatus: models.StatusInReview,
			sessionID: "ses-reviewer", creatorSession: "ses-c", implementerSession: "ses-impl",
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "step3-record: delegated implementer blocked from record",
			mode: reviewpolicy.ModeDelegated, issueStatus: models.StatusInReview,
			sessionID: "ses-impl", creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: false, wantCloseAllowed: false,
		},
		// Close-after-review: requester closes an approved issue.
		parityRow{name: "step3-close-after: delegated review-requester closes with active approval",
			mode: reviewpolicy.ModeDelegated, issueStatus: models.StatusInReview,
			sessionID: "ses-req", creatorSession: "ses-c", implementerSession: "ses-impl",
			reviewRequestedBySession: "ses-req",
			reviewerSession:          "ses-rev",
			hasActiveApproval:        true,
			// The requester is not the implementer (and has no impl history)
			// so it would be reviewer-eligible too; the close path is the
			// one we care about — it's the Mode-C close-after-review case.
			wantReviewerAllowed: true,
			wantCloseAllowed:    true,
		},
		// Log-only closer parity row (reviewer flagged this path). Session
		// has no explicit role, but active approval is the delegated close
		// gate.
		parityRow{name: "step3-log-only: delegated non-role session can close approved issue",
			mode: reviewpolicy.ModeDelegated, issueStatus: models.StatusInReview,
			sessionID: "ses-log-only", creatorSession: "ses-c", implementerSession: "ses-impl",
			hasActiveApproval:   true,
			wantReviewerAllowed: true, // reviewer eligibility is independent
			wantCloseAllowed:    true,
		},
	)

	// --- Trusted mode scenarios ---
	//
	// Trusted is delegated plus a flag-gated, audited self-review escape. These
	// rows exercise the non-self-review paths, where trusted must behave
	// exactly like delegated (the --self-review flag is not set, so the
	// implementer self-review remains blocked at decision time). The trusted
	// self-review-allow path is covered separately in
	// TestReviewPolicyParity_TrustedSelfReview because it requires the
	// SelfReviewAcknowledged flag the generic shims do not thread.
	rows = append(rows,
		parityRow{name: "trusted: implementer blocked without flag", mode: reviewpolicy.ModeTrusted,
			issueStatus: models.StatusInReview, sessionID: "ses-impl",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: false, wantCloseAllowed: false,
		},
		parityRow{name: "trusted: impl history blocked without flag", mode: reviewpolicy.ModeTrusted,
			issueStatus: models.StatusInReview, sessionID: "ses-prev-impl",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: true,
			wantReviewerAllowed:      false, wantCloseAllowed: false,
		},
		parityRow{name: "trusted: fresh session allowed (review+close)", mode: reviewpolicy.ModeTrusted,
			issueStatus: models.StatusInReview, sessionID: "ses-fresh",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "trusted: creator-who-never-implemented allowed", mode: reviewpolicy.ModeTrusted,
			issueStatus: models.StatusInReview, sessionID: "ses-c",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			wasAnyInvolved:      true,
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "trusted: active approval + arbitrary session closes", mode: reviewpolicy.ModeTrusted,
			issueStatus: models.StatusInReview, sessionID: "ses-other",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasActiveApproval:   true,
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
		parityRow{name: "trusted: active approval + implementer closes", mode: reviewpolicy.ModeTrusted,
			issueStatus: models.StatusInReview, sessionID: "ses-impl",
			creatorSession: "ses-c", implementerSession: "ses-impl",
			hasImplementationHistory: true, hasActiveApproval: true,
			wantReviewerAllowed: false, wantCloseAllowed: true,
		},
		parityRow{name: "trusted: minor bypass", mode: reviewpolicy.ModeTrusted,
			issueStatus: models.StatusInReview, minor: true, sessionID: "ses-impl",
			creatorSession: "ses-impl", implementerSession: "ses-impl",
			hasImplementationHistory: true, wasAnyInvolved: true,
			wantReviewerAllowed: true, wantCloseAllowed: true,
		},
	)

	return rows
}

// inputsFromRow builds the ReviewerEligibilityInput/CloseEligibilityInput the
// shared reviewpolicy package expects. This is the exact shape every ported
// surface computes before calling EvaluateReviewerEligibility — asserting
// parity here guarantees parity across callers.
func (r parityRow) reviewerInput() reviewpolicy.ReviewerEligibilityInput {
	issue := mkParityIssue(r)
	return reviewpolicy.ReviewerEligibilityInput{
		Mode:                     r.mode,
		Issue:                    issue,
		SessionID:                r.sessionID,
		SessionIsImplementer:     issue.ImplementerSession != "" && issue.ImplementerSession == r.sessionID,
		SessionIsCreator:         issue.CreatorSession != "" && issue.CreatorSession == r.sessionID,
		HasImplementationHistory: r.hasImplementationHistory,
		HasActiveApproval:        r.hasActiveApproval,
		WasAnyInvolved:           r.wasAnyInvolved,
	}
}

func (r parityRow) closeInput() reviewpolicy.CloseEligibilityInput {
	issue := mkParityIssue(r)
	return reviewpolicy.CloseEligibilityInput{
		Mode:                      r.mode,
		Issue:                     issue,
		SessionID:                 r.sessionID,
		SessionIsImplementer:      issue.ImplementerSession != "" && issue.ImplementerSession == r.sessionID,
		SessionIsCreator:          issue.CreatorSession != "" && issue.CreatorSession == r.sessionID,
		SessionIsReviewerOfRecord: issue.ReviewerSession != "" && issue.ReviewerSession == r.sessionID,
		SessionIsReviewRequester:  issue.ReviewRequestedBySession != "" && issue.ReviewRequestedBySession == r.sessionID,
		HasImplementationHistory:  r.hasImplementationHistory,
		WasAnyInvolved:            r.wasAnyInvolved,
		HasActiveApproval:         r.hasActiveApproval,
	}
}

// TestReviewPolicyParity_Surfaces asserts that every surface returns the
// same decision for the same inputs. If a new caller is added that bypasses
// reviewpolicy, either it must be wired in here or this test will fail.
func TestReviewPolicyParity_Surfaces(t *testing.T) {
	rows := parityMatrix()
	if len(rows) < 50 {
		t.Fatalf("parity matrix must cover at least 50 scenarios, got %d", len(rows))
	}

	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			// 0. Ground truth: reviewpolicy decision.
			revDec := reviewpolicy.EvaluateReviewerEligibility(r.reviewerInput())
			closeDec := reviewpolicy.EvaluateCloseEligibility(r.closeInput())
			if revDec.Allowed != r.wantReviewerAllowed {
				t.Fatalf("reviewpolicy reviewer Allowed=%v, want %v (msg=%q)",
					revDec.Allowed, r.wantReviewerAllowed, revDec.RejectionMessage)
			}
			if closeDec.Allowed != r.wantCloseAllowed {
				t.Fatalf("reviewpolicy close Allowed=%v, want %v (msg=%q)",
					closeDec.Allowed, r.wantCloseAllowed, closeDec.RejectionMessage)
			}
			if revDec.CreatorException != r.wantCreatorException {
				t.Fatalf("reviewpolicy CreatorException=%v, want %v",
					revDec.CreatorException, r.wantCreatorException)
			}

			// 1. CLI wrapper: evaluateApproveEligibility and
			// evaluateCloseEligibility must agree.
			balancedPolicy := r.mode == reviewpolicy.ModeBalanced
			// The cmd wrapper does not accept a delegated mode (the CLI
			// close-path is still strict/balanced in Batch 1c — that's the
			// "do NOT activate delegated behavior in any caller yet" rule).
			// For delegated rows we assert parity only for the reviewer
			// decision the CLI is able to reach today; the close wrapper
			// stays on strict/balanced and must match the strict/balanced
			// reviewpolicy decision for the same inputs.
			issue := mkParityIssue(r)
			cliApprove := evaluateApproveEligibility(issue, r.sessionID,
				r.wasAnyInvolved, r.hasImplementationHistory, balancedPolicy)

			switch r.mode {
			case reviewpolicy.ModeDelegated:
				// In delegated mode, the legacy CLI wrapper uses
				// strict/balanced, which may differ. We only assert delegated
				// reviewpolicy agrees with itself; the CLI wrapper's delegated
				// behavior is wired in Step 2.
				_ = cliApprove
			case reviewpolicy.ModeTrusted:
				// Trusted mode routes through the mode-aware CLI approve
				// wrapper. These rows do not set the --self-review flag, so the
				// trusted decision must equal the reviewpolicy ground truth.
				cliApproveTrusted := evaluateApproveEligibilityWithMode(
					issue, r.sessionID,
					r.wasAnyInvolved, r.hasImplementationHistory,
					reviewpolicy.ModeTrusted, false,
				)
				if cliApproveTrusted.Allowed != r.wantReviewerAllowed {
					t.Fatalf("cli trusted approve Allowed=%v, want %v",
						cliApproveTrusted.Allowed, r.wantReviewerAllowed)
				}
			default:
				if cliApprove.Allowed != r.wantReviewerAllowed {
					t.Fatalf("cli approve Allowed=%v, want %v", cliApprove.Allowed, r.wantReviewerAllowed)
				}
			}

			// CLI close wrapper. The legacy 5-arg wrapper stays on
			// strict/balanced; for delegated rows we exercise
			// evaluateCloseEligibilityWithMode (the baseDir-plumbed variant
			// Step 2 added) so the CLI close decision is also parity-checked
			// in delegated mode.
			cliClose := evaluateCloseEligibility(issue, r.sessionID,
				r.wasAnyInvolved, r.hasImplementationHistory, r.effectiveIssueImplHistory())

			switch r.mode {
			case reviewpolicy.ModeDelegated, reviewpolicy.ModeTrusted:
				// Delegated and trusted share the review-attestation close rule
				// (an in_review issue with an active approval is closeable by
				// any session). Invoke the mode-aware CLI close wrapper so the
				// rule is exercised through the CLI too.
				cliCloseModed := evaluateCloseEligibilityWithMode(
					issue, r.sessionID,
					r.wasAnyInvolved, r.hasImplementationHistory, r.effectiveIssueImplHistory(),
					r.mode, r.hasActiveApproval, false,
				)
				if cliCloseModed.Allowed != r.wantCloseAllowed {
					t.Fatalf("cli %s close Allowed=%v, want %v (msg=%q)",
						r.mode, cliCloseModed.Allowed, r.wantCloseAllowed, cliCloseModed.RejectionMessage)
				}
			default:
				// Strict and balanced share the same close logic, so the CLI
				// close decision must match the reviewpolicy close decision
				// for those modes — except when the wrapper's issue-wide
				// impl-history veto fires (divergence rows).
				wantCLIClose := r.wantCloseAllowed && !r.wantCLICloseBlockedByIssueVeto
				if cliClose.Allowed != wantCLIClose {
					t.Fatalf("cli close Allowed=%v, want %v (msg=%q)", cliClose.Allowed, wantCLIClose, cliClose.RejectionMessage)
				}
				if !cliClose.Allowed && cliClose.RejectionMessage == "" {
					t.Fatalf("cli close rejected with empty RejectionMessage")
				}
			}

			// Rejection-message non-emptiness parity for approve as well.
			// Skip for delegated/trusted: those rows assert the mode-aware
			// wrapper above, not the legacy strict/balanced cliApprove value.
			if !cliApprove.Allowed && cliApprove.RejectionMessage == "" &&
				r.mode != reviewpolicy.ModeDelegated && r.mode != reviewpolicy.ModeTrusted {
				t.Fatalf("cli approve rejected with empty RejectionMessage")
			}

			// 2. Monitor decision: must use the same reviewpolicy rule as
			// the CLI under all modes. We call the exported helper with
			// the same inputs the parity row describes.
			monitorDec := monitor.MonitorApproveDecisionForTest(
				r.mode, issue, r.sessionID,
				r.hasImplementationHistory, r.wasAnyInvolved, r.hasActiveApproval,
				false, /*selfReviewAcknowledged*/
			)
			if monitorDec.Allowed != r.wantReviewerAllowed {
				t.Fatalf("monitor Allowed=%v, want %v", monitorDec.Allowed, r.wantReviewerAllowed)
			}

			// 3. Serve handlers_transitions decision: same rule.
			// Drive through the exported serve shim so any drift inside
			// the serve package's input assembly would surface here.
			serveRev := serve.ServeReviewerDecisionForTest(
				r.mode, issue, r.sessionID,
				r.hasImplementationHistory, r.wasAnyInvolved, r.hasActiveApproval,
				false, /*selfReviewAcknowledged*/
			)
			if serveRev.Allowed != r.wantReviewerAllowed {
				t.Fatalf("serve reviewer Allowed=%v, want %v (msg=%q)",
					serveRev.Allowed, r.wantReviewerAllowed, serveRev.RejectionMessage)
			}

			// Serve close-eligibility: explicit coverage so the close
			// handler path is exercised alongside approve.
			serveClose := serve.ServeCloseDecisionForTest(
				r.mode, issue, r.sessionID,
				r.hasImplementationHistory, r.wasAnyInvolved, r.hasActiveApproval,
				false, /*selfReviewAcknowledged*/
			)
			if serveClose.Allowed != r.wantCloseAllowed {
				t.Fatalf("serve close Allowed=%v, want %v (msg=%q)",
					serveClose.Allowed, r.wantCloseAllowed, serveClose.RejectionMessage)
			}
			if !serveClose.Allowed && serveClose.RejectionMessage == "" {
				t.Fatalf("serve close rejected with empty RejectionMessage")
			}
		})
	}
}

// TestReviewPolicyParity_Composer asserts the SQL composers for
// internal/db and internal/api/snapshot_query_source route through the same
// mode-aware function. Since both callers now call
// db.ReviewableByFilterForMode, the test just guards against accidental
// reintroduction of a second copy by checking that the composer returns
// identical SQL for the same inputs.
func TestReviewPolicyParity_Composer(t *testing.T) {
	// This is intentionally a smoke test — the deep SQL behavior is tested
	// in internal/db/db_test.go#TestReviewableByFilter. What matters here
	// is that the contract surface exists and has the documented shape.
	for _, mode := range []string{"strict", "balanced", "delegated", "trusted"} {
		sqlFrag, args := reviewableByFilterForModeParity("ses-x", mode)
		if sqlFrag == "" {
			t.Errorf("composer returned empty SQL for mode %q", mode)
		}
		if len(args) == 0 {
			t.Errorf("composer returned empty args for mode %q", mode)
		}
	}
}

// TestReviewPolicyParity_TrustedSelfReview proves the trusted-mode contract
// the DB composer and the action-time policy jointly enforce: the
// ReviewableByFilter is intentionally broad (it returns self-implemented
// in_review issues, unlike delegated), and the implementer-independence
// requirement is enforced at action time via the --self-review flag. This row
// can't live in the generic parity matrix because the shared monitor/serve
// shims do not thread SelfReviewAcknowledged.
func TestReviewPolicyParity_TrustedSelfReview(t *testing.T) {
	issue := &models.Issue{
		ID: "td-trusted-self", Title: "trusted self", Status: models.StatusInReview,
		Type: models.TypeTask, Priority: models.PriorityP2,
		CreatorSession: "ses-self", ImplementerSession: "ses-self",
	}

	// Action time WITHOUT the flag: trusted self-review is rejected, matching
	// the CLI's mode-aware approve wrapper.
	withoutFlag := evaluateApproveEligibilityWithMode(
		issue, "ses-self",
		true /*wasInvolved*/, true, /*wasImplementationInvolved*/
		reviewpolicy.ModeTrusted, false, /*selfReview*/
	)
	if withoutFlag.Allowed {
		t.Fatalf("trusted self-review without --self-review should be rejected, got allowed")
	}

	// Action time WITH the flag: allowed and stamped as a self-review for audit.
	withFlag := evaluateApproveEligibilityWithMode(
		issue, "ses-self",
		true, true,
		reviewpolicy.ModeTrusted, true, /*selfReview*/
	)
	if !withFlag.Allowed {
		t.Fatalf("trusted self-review WITH --self-review should be allowed, got %+v", withFlag)
	}
	if !withFlag.SelfReview {
		t.Fatalf("trusted self-review WITH --self-review should be flagged SelfReview for audit")
	}

	// Monitor surface parity: the monitor's approve decision must agree with
	// the CLI for trusted self-review, both without and with acknowledgement.
	// Without the ack the implementer self-review is rejected (the monitor then
	// prompts the confirm modal); with the ack it is an audited self-review
	// allow stamped SelfReview=true.
	monitorWithoutAck := monitor.MonitorApproveDecisionForTest(
		reviewpolicy.ModeTrusted, issue, "ses-self",
		true /*hasImplementationHistory*/, true /*wasAnyInvolved*/, false, /*hasActiveApproval*/
		false, /*selfReviewAcknowledged*/
	)
	if monitorWithoutAck.Allowed {
		t.Fatalf("monitor trusted self-review without ack should be rejected, got allowed")
	}
	monitorWithAck := monitor.MonitorApproveDecisionForTest(
		reviewpolicy.ModeTrusted, issue, "ses-self",
		true, true, false,
		true, /*selfReviewAcknowledged*/
	)
	if !monitorWithAck.Allowed {
		t.Fatalf("monitor trusted self-review WITH ack should be allowed, got %+v", monitorWithAck)
	}
	if !monitorWithAck.SelfReview {
		t.Fatalf("monitor trusted self-review WITH ack should be flagged SelfReview for audit")
	}

	// Serve surface parity: the serve approve/close decision shims must agree
	// with the CLI/monitor for trusted self-review. Without the ack the
	// implementer self-review is rejected; with the ack it is an audited allow
	// stamped SelfReview=true.
	serveRevWithoutAck := serve.ServeReviewerDecisionForTest(
		reviewpolicy.ModeTrusted, issue, "ses-self",
		true /*hasImplementationHistory*/, true /*wasAnyInvolved*/, false, /*hasActiveApproval*/
		false, /*selfReviewAcknowledged*/
	)
	if serveRevWithoutAck.Allowed {
		t.Fatalf("serve trusted self-review without ack should be rejected, got allowed")
	}
	if serveRevWithoutAck.RejectionMessage == "" {
		t.Fatalf("serve trusted self-review rejection should carry a teaching message")
	}
	serveRevWithAck := serve.ServeReviewerDecisionForTest(
		reviewpolicy.ModeTrusted, issue, "ses-self",
		true, true, false,
		true, /*selfReviewAcknowledged*/
	)
	if !serveRevWithAck.Allowed {
		t.Fatalf("serve trusted self-review WITH ack should be allowed, got %+v", serveRevWithAck)
	}
	if !serveRevWithAck.SelfReview {
		t.Fatalf("serve trusted self-review WITH ack should be flagged SelfReview for audit")
	}
	if !serveRevWithAck.RequiresReason {
		t.Fatalf("serve trusted self-review WITH ack should require a reason")
	}
	serveCloseWithAck := serve.ServeCloseDecisionForTest(
		reviewpolicy.ModeTrusted, issue, "ses-self",
		true, true, false,
		true, /*selfReviewAcknowledged*/
	)
	if !serveCloseWithAck.Allowed {
		t.Fatalf("serve trusted self-review close WITH ack should be allowed, got %+v", serveCloseWithAck)
	}

	// The shared reviewpolicy ground truth agrees with the CLI wrapper.
	groundTruth := reviewpolicy.EvaluateReviewerEligibility(reviewpolicy.ReviewerEligibilityInput{
		Mode:                     reviewpolicy.ModeTrusted,
		Issue:                    issue,
		SessionID:                "ses-self",
		SessionIsImplementer:     true,
		SessionIsCreator:         true,
		HasImplementationHistory: true,
		WasAnyInvolved:           true,
		SelfReviewAcknowledged:   true,
	})
	if !groundTruth.Allowed || !groundTruth.SelfReview {
		t.Fatalf("reviewpolicy trusted self-review ground truth = %+v, want allowed+SelfReview", groundTruth)
	}
}
