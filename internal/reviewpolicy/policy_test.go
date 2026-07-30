package reviewpolicy

import (
	"strings"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestParseMode(t *testing.T) {
	t.Run("round trip for each known mode", func(t *testing.T) {
		cases := []Mode{ModeStrict, ModeBalanced, ModeDelegated, ModeTrusted}
		for _, want := range cases {
			got, err := ParseMode(string(want))
			if err != nil {
				t.Errorf("ParseMode(%q) returned error: %v", want, err)
				continue
			}
			if got != want {
				t.Errorf("ParseMode(%q) = %q, want %q", want, got, want)
			}
		}
	})

	t.Run("empty string is rejected", func(t *testing.T) {
		if _, err := ParseMode(""); err == nil {
			t.Error("ParseMode(\"\") should error")
		}
	})

	t.Run("unknown value is rejected", func(t *testing.T) {
		if _, err := ParseMode("laissez-faire"); err == nil {
			t.Error("ParseMode of unknown value should error")
		}
	})
}

// inReview returns a non-minor in_review issue for reviewer-eligibility tests.
// ImplementerSession is populated so the "different implementer" balanced
// branch can exercise its creator-exception path.
func inReview(creator, implementer string) *models.Issue {
	return &models.Issue{
		ID:                 "td-test1",
		Title:              "test",
		Status:             models.StatusInReview,
		Type:               models.TypeTask,
		Priority:           models.PriorityP2,
		Minor:              false,
		CreatorSession:     creator,
		ImplementerSession: implementer,
	}
}

func minorIssue() *models.Issue {
	is := inReview("ses-creator", "ses-impl")
	is.Minor = true
	return is
}

func TestEvaluateReviewerEligibility_NilIssue(t *testing.T) {
	got := EvaluateReviewerEligibility(ReviewerEligibilityInput{Mode: ModeStrict, Issue: nil})
	if got.Allowed {
		t.Error("nil issue must not be approvable")
	}
	if got.RejectionMessage == "" {
		t.Error("nil issue should produce a rejection message")
	}
}

func TestEvaluateReviewerEligibility_MinorBypass(t *testing.T) {
	for _, mode := range []Mode{ModeStrict, ModeBalanced, ModeDelegated, ModeTrusted} {
		in := ReviewerEligibilityInput{
			Mode:                     mode,
			Issue:                    minorIssue(),
			SessionID:                "ses-impl",
			SessionIsImplementer:     true,
			HasImplementationHistory: true,
			WasAnyInvolved:           true,
		}
		got := EvaluateReviewerEligibility(in)
		if !got.Allowed {
			t.Errorf("mode %s: minor issue should bypass to Allowed, got %+v", mode, got)
		}
		if got.RequiresReason {
			t.Errorf("mode %s: minor bypass should not require reason", mode)
		}
	}
}

func TestEvaluateReviewerEligibility_Strict(t *testing.T) {
	issue := inReview("ses-creator", "ses-impl")

	cases := []struct {
		name                     string
		sessionID                string
		sessionIsImplementer     bool
		sessionIsCreator         bool
		hasImplementationHistory bool
		wasAnyInvolved           bool
		wantAllowed              bool
	}{
		{"implementer blocked", "ses-impl", true, false, true, true, false},
		{"creator blocked", "ses-creator", false, true, false, true, false},
		{"prior reviewer (any involvement) blocked", "ses-prev", false, false, false, true, false},
		{"uninvolved session allowed", "ses-fresh", false, false, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := ReviewerEligibilityInput{
				Mode:                     ModeStrict,
				Issue:                    issue,
				SessionID:                c.sessionID,
				SessionIsImplementer:     c.sessionIsImplementer,
				SessionIsCreator:         c.sessionIsCreator,
				HasImplementationHistory: c.hasImplementationHistory,
				WasAnyInvolved:           c.wasAnyInvolved,
			}
			got := EvaluateReviewerEligibility(in)
			if got.Allowed != c.wantAllowed {
				t.Errorf("Allowed: got %v, want %v (%+v)", got.Allowed, c.wantAllowed, got)
			}
			if !c.wantAllowed && got.RejectionMessage == "" {
				t.Error("rejection should carry a message")
			}
		})
	}
}

func TestEvaluateReviewerEligibility_Balanced(t *testing.T) {
	cases := []struct {
		name                     string
		issue                    *models.Issue
		sessionID                string
		sessionIsImplementer     bool
		sessionIsCreator         bool
		hasImplementationHistory bool
		wasAnyInvolved           bool
		wantAllowed              bool
		wantCreatorException     bool
		wantRequiresReason       bool
	}{
		{
			name:                 "implementer blocked",
			issue:                inReview("ses-creator", "ses-impl"),
			sessionID:            "ses-impl",
			sessionIsImplementer: true,
			wantAllowed:          false,
		},
		{
			name:                     "impl history blocked even if not current implementer",
			issue:                    inReview("ses-creator", "ses-impl"),
			sessionID:                "ses-prev-impl",
			hasImplementationHistory: true,
			wasAnyInvolved:           true,
			wantAllowed:              false,
		},
		{
			name:                 "creator with different implementer allowed with exception + reason",
			issue:                inReview("ses-creator", "ses-impl"),
			sessionID:            "ses-creator",
			sessionIsCreator:     true,
			wasAnyInvolved:       true,
			wantAllowed:          true,
			wantCreatorException: true,
			wantRequiresReason:   true,
		},
		{
			name:             "creator with same session as implementer blocked",
			issue:            inReview("ses-creator", "ses-creator"),
			sessionID:        "ses-creator",
			sessionIsCreator: true,
			// SessionIsImplementer true means no creator-exception applies.
			sessionIsImplementer: true,
			wantAllowed:          false,
		},
		{
			name:           "non-creator with prior involvement blocked",
			issue:          inReview("ses-creator", "ses-impl"),
			sessionID:      "ses-prev",
			wasAnyInvolved: true,
			wantAllowed:    false,
		},
		{
			name:        "uninvolved session allowed",
			issue:       inReview("ses-creator", "ses-impl"),
			sessionID:   "ses-fresh",
			wantAllowed: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := ReviewerEligibilityInput{
				Mode:                     ModeBalanced,
				Issue:                    c.issue,
				SessionID:                c.sessionID,
				SessionIsImplementer:     c.sessionIsImplementer,
				SessionIsCreator:         c.sessionIsCreator,
				HasImplementationHistory: c.hasImplementationHistory,
				WasAnyInvolved:           c.wasAnyInvolved,
			}
			got := EvaluateReviewerEligibility(in)
			if got.Allowed != c.wantAllowed {
				t.Errorf("Allowed: got %v, want %v (msg=%q)", got.Allowed, c.wantAllowed, got.RejectionMessage)
			}
			if got.CreatorException != c.wantCreatorException {
				t.Errorf("CreatorException: got %v, want %v", got.CreatorException, c.wantCreatorException)
			}
			if got.RequiresReason != c.wantRequiresReason {
				t.Errorf("RequiresReason: got %v, want %v", got.RequiresReason, c.wantRequiresReason)
			}
		})
	}
}

func TestEvaluateReviewerEligibility_Delegated(t *testing.T) {
	issue := inReview("ses-creator", "ses-impl")

	cases := []struct {
		name                     string
		sessionID                string
		sessionIsImplementer     bool
		sessionIsCreator         bool
		hasImplementationHistory bool
		wasAnyInvolved           bool
		hasActiveApproval        bool
		wantAllowed              bool
	}{
		{"implementer blocked", "ses-impl", true, false, true, true, false, false},
		{"impl history blocked even if not current implementer", "ses-prev-impl", false, false, true, true, false, false},
		{"creator who never implemented allowed", "ses-creator", false, true, false, true, false, true},
		{"uninvolved session allowed", "ses-fresh", false, false, false, false, false, true},
		{"prior reviewer (no impl) still allowed (repeat review cycle)", "ses-prev-reviewer", false, false, false, true, false, true},
		// In 1b delegated-mode reviewer eligibility does NOT branch on
		// HasActiveApproval — Step 2 routes callers through the
		// close-after-review path when that is true.
		{"has active approval does not block (still allowed)", "ses-fresh", false, false, false, false, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := ReviewerEligibilityInput{
				Mode:                     ModeDelegated,
				Issue:                    issue,
				SessionID:                c.sessionID,
				SessionIsImplementer:     c.sessionIsImplementer,
				SessionIsCreator:         c.sessionIsCreator,
				HasImplementationHistory: c.hasImplementationHistory,
				WasAnyInvolved:           c.wasAnyInvolved,
				HasActiveApproval:        c.hasActiveApproval,
			}
			got := EvaluateReviewerEligibility(in)
			if got.Allowed != c.wantAllowed {
				t.Errorf("Allowed: got %v, want %v (msg=%q)", got.Allowed, c.wantAllowed, got.RejectionMessage)
			}
		})
	}
}

func TestEvaluateCloseEligibility_NilIssue(t *testing.T) {
	got := EvaluateCloseEligibility(CloseEligibilityInput{Mode: ModeStrict, Issue: nil})
	if got.Allowed {
		t.Error("nil issue must not be closable")
	}
}

func TestEvaluateCloseEligibility_MinorBypass(t *testing.T) {
	for _, mode := range []Mode{ModeStrict, ModeBalanced, ModeDelegated, ModeTrusted} {
		in := CloseEligibilityInput{
			Mode:                 mode,
			Issue:                minorIssue(),
			SessionID:            "ses-impl",
			SessionIsImplementer: true,
		}
		got := EvaluateCloseEligibility(in)
		if !got.Allowed {
			t.Errorf("mode %s: minor issue should bypass to Allowed, got %+v", mode, got)
		}
	}
}

func TestEvaluateCloseEligibility_StrictBalanced(t *testing.T) {
	openIssue := func(creator, implementer string) *models.Issue {
		is := inReview(creator, implementer)
		is.Status = models.StatusOpen
		return is
	}

	cases := []struct {
		name                     string
		mode                     Mode
		issue                    *models.Issue
		sessionID                string
		sessionIsImplementer     bool
		sessionIsCreator         bool
		hasImplementationHistory bool
		wasAnyInvolved           bool
		wantAllowed              bool
		wantCreatorOpenBypass    bool
	}{
		{
			name:                  "creator-open-bypass: self-created open with no impl",
			mode:                  ModeStrict,
			issue:                 openIssue("ses-c", ""),
			sessionID:             "ses-c",
			sessionIsCreator:      true,
			wantAllowed:           true,
			wantCreatorOpenBypass: true,
		},
		{
			name:                     "creator with impl history on open issue requires review",
			mode:                     ModeStrict,
			issue:                    openIssue("ses-c", "ses-impl"),
			sessionID:                "ses-c",
			sessionIsCreator:         true,
			hasImplementationHistory: true,
			wantAllowed:              false,
		},
		{
			name:                 "implementer blocked",
			mode:                 ModeStrict,
			issue:                inReview("ses-c", "ses-impl"),
			sessionID:            "ses-impl",
			sessionIsImplementer: true,
			wantAllowed:          false,
		},
		{
			name:                     "prior implementation-history blocked",
			mode:                     ModeBalanced,
			issue:                    inReview("ses-c", "ses-impl"),
			sessionID:                "ses-prev-impl",
			hasImplementationHistory: true,
			wantAllowed:              false,
		},
		{
			name:             "creator closing in_review issue blocked",
			mode:             ModeBalanced,
			issue:            inReview("ses-c", "ses-impl"),
			sessionID:        "ses-c",
			sessionIsCreator: true,
			wantAllowed:      false,
		},
		{
			name:           "wasAnyInvolved non-creator blocked",
			mode:           ModeBalanced,
			issue:          inReview("ses-c", "ses-impl"),
			sessionID:      "ses-prev",
			wasAnyInvolved: true,
			wantAllowed:    false,
		},
		{
			name:        "uninvolved session allowed",
			mode:        ModeStrict,
			issue:       inReview("ses-c", "ses-impl"),
			sessionID:   "ses-fresh",
			wantAllowed: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := CloseEligibilityInput{
				Mode:                     c.mode,
				Issue:                    c.issue,
				SessionID:                c.sessionID,
				SessionIsImplementer:     c.sessionIsImplementer,
				SessionIsCreator:         c.sessionIsCreator,
				HasImplementationHistory: c.hasImplementationHistory,
				WasAnyInvolved:           c.wasAnyInvolved,
			}
			got := EvaluateCloseEligibility(in)
			if got.Allowed != c.wantAllowed {
				t.Errorf("Allowed: got %v, want %v (msg=%q)", got.Allowed, c.wantAllowed, got.RejectionMessage)
			}
			if got.CreatorOpenBypass != c.wantCreatorOpenBypass {
				t.Errorf("CreatorOpenBypass: got %v, want %v", got.CreatorOpenBypass, c.wantCreatorOpenBypass)
			}
		})
	}
}

func TestEvaluateCloseEligibility_Delegated_WithActiveApproval(t *testing.T) {
	issue := inReview("ses-creator", "ses-impl")

	cases := []struct {
		name                      string
		sessionIsImplementer      bool
		sessionIsCreator          bool
		sessionIsReviewerOfRecord bool
		sessionIsReviewRequester  bool
		hasImplementationHistory  bool
		wantAllowed               bool
	}{
		{"creator allowed", false, true, false, false, false, true},
		{"implementer allowed", true, false, false, false, true, true},
		{"reviewer-of-record allowed", false, false, true, false, false, true},
		{"review-requester allowed", false, false, false, true, false, true},
		{"arbitrary session allowed", false, false, false, false, false, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := CloseEligibilityInput{
				Mode:                      ModeDelegated,
				Issue:                     issue,
				SessionID:                 "ses-x",
				SessionIsImplementer:      c.sessionIsImplementer,
				SessionIsCreator:          c.sessionIsCreator,
				SessionIsReviewerOfRecord: c.sessionIsReviewerOfRecord,
				SessionIsReviewRequester:  c.sessionIsReviewRequester,
				HasImplementationHistory:  c.hasImplementationHistory,
				HasActiveApproval:         true,
			}
			got := EvaluateCloseEligibility(in)
			if got.Allowed != c.wantAllowed {
				t.Errorf("Allowed: got %v, want %v (msg=%q)", got.Allowed, c.wantAllowed, got.RejectionMessage)
			}
		})
	}
}

func TestEvaluateCloseEligibility_Delegated_NoActiveApproval(t *testing.T) {
	// Without an active approval the delegated close path reduces to the
	// direct reviewer-eligibility check (review + close in one action).
	issue := inReview("ses-creator", "ses-impl")

	cases := []struct {
		name                     string
		sessionIsImplementer     bool
		sessionIsCreator         bool
		hasImplementationHistory bool
		wantAllowed              bool
	}{
		{"implementer blocked", true, false, true, false},
		{"creator-who-never-implemented allowed (direct review+close)", false, true, false, true},
		{"fresh session allowed (direct review+close)", false, false, false, true},
		{"prior impl history blocked", false, false, true, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := CloseEligibilityInput{
				Mode:                     ModeDelegated,
				Issue:                    issue,
				SessionID:                "ses-x",
				SessionIsImplementer:     c.sessionIsImplementer,
				SessionIsCreator:         c.sessionIsCreator,
				HasImplementationHistory: c.hasImplementationHistory,
				HasActiveApproval:        false,
			}
			got := EvaluateCloseEligibility(in)
			if got.Allowed != c.wantAllowed {
				t.Errorf("Allowed: got %v, want %v (msg=%q)", got.Allowed, c.wantAllowed, got.RejectionMessage)
			}
		})
	}
}

// TestEvaluateCloseEligibility_Delegated_NonInReviewIssue covers the
// close-with-open-issue gap flagged by the 1b reviewer. In delegated mode,
// EvaluateCloseEligibility must NOT fall through to reviewer-eligibility when
// the issue is still open/in_progress/blocked — that would let an uninvolved
// session close a still-open issue. Non-in_review non-minor issues should be
// gated by the same rules as strict/balanced mode.
func TestEvaluateCloseEligibility_Delegated_NonInReviewIssue(t *testing.T) {
	openIssue := func(creator, implementer string) *models.Issue {
		is := inReview(creator, implementer)
		is.Status = models.StatusOpen
		return is
	}

	cases := []struct {
		name                     string
		issue                    *models.Issue
		sessionID                string
		sessionIsImplementer     bool
		sessionIsCreator         bool
		hasImplementationHistory bool
		wasAnyInvolved           bool
		wantAllowed              bool
		wantCreatorOpenBypass    bool
	}{
		{
			name:                  "creator-open-bypass: self-created open with no impl",
			issue:                 openIssue("ses-c", ""),
			sessionID:             "ses-c",
			sessionIsCreator:      true,
			wantAllowed:           true,
			wantCreatorOpenBypass: true,
		},
		{
			name:                     "uninvolved session must NOT close a still-open issue in delegated mode",
			issue:                    openIssue("ses-c", "ses-impl"),
			sessionID:                "ses-fresh",
			hasImplementationHistory: true,
			wantAllowed:              false,
		},
		{
			name:                 "implementer blocked on open issue",
			issue:                openIssue("ses-c", "ses-impl"),
			sessionID:            "ses-impl",
			sessionIsImplementer: true,
			wantAllowed:          false,
		},
		{
			name:                     "previously-involved non-creator blocked",
			issue:                    openIssue("ses-c", "ses-impl"),
			sessionID:                "ses-prev",
			wasAnyInvolved:           true,
			hasImplementationHistory: true,
			wantAllowed:              false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := CloseEligibilityInput{
				Mode:                     ModeDelegated,
				Issue:                    c.issue,
				SessionID:                c.sessionID,
				SessionIsImplementer:     c.sessionIsImplementer,
				SessionIsCreator:         c.sessionIsCreator,
				HasImplementationHistory: c.hasImplementationHistory,
				WasAnyInvolved:           c.wasAnyInvolved,
				HasActiveApproval:        false,
			}
			got := EvaluateCloseEligibility(in)
			if got.Allowed != c.wantAllowed {
				t.Errorf("Allowed: got %v, want %v (msg=%q)", got.Allowed, c.wantAllowed, got.RejectionMessage)
			}
			if got.CreatorOpenBypass != c.wantCreatorOpenBypass {
				t.Errorf("CreatorOpenBypass: got %v, want %v", got.CreatorOpenBypass, c.wantCreatorOpenBypass)
			}
		})
	}
}

func TestEvaluateReviewerEligibility_Trusted(t *testing.T) {
	issue := inReview("ses-creator", "ses-impl")

	cases := []struct {
		name                     string
		sessionID                string
		sessionIsImplementer     bool
		sessionIsCreator         bool
		hasImplementationHistory bool
		selfReviewAcknowledged   bool
		wantAllowed              bool
		wantSelfReview           bool
		wantRequiresReason       bool
	}{
		{
			name:        "independent session approves (no flag, not self-review)",
			sessionID:   "ses-fresh",
			wantAllowed: true,
		},
		{
			name:             "creator-only no history approves without flag",
			sessionID:        "ses-creator",
			sessionIsCreator: true,
			wantAllowed:      true,
		},
		{
			name:                 "implementer without flag rejected",
			sessionID:            "ses-impl",
			sessionIsImplementer: true,
			wantAllowed:          false,
		},
		{
			name:                     "impl history without flag rejected",
			sessionID:                "ses-prev-impl",
			hasImplementationHistory: true,
			wantAllowed:              false,
		},
		{
			name:                   "implementer with flag allowed as self-review",
			sessionID:              "ses-impl",
			sessionIsImplementer:   true,
			selfReviewAcknowledged: true,
			wantAllowed:            true,
			wantSelfReview:         true,
			wantRequiresReason:     true,
		},
		{
			name:                     "impl history with flag allowed as self-review",
			sessionID:                "ses-prev-impl",
			hasImplementationHistory: true,
			selfReviewAcknowledged:   true,
			wantAllowed:              true,
			wantSelfReview:           true,
			wantRequiresReason:       true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := ReviewerEligibilityInput{
				Mode:                     ModeTrusted,
				Issue:                    issue,
				SessionID:                c.sessionID,
				SessionIsImplementer:     c.sessionIsImplementer,
				SessionIsCreator:         c.sessionIsCreator,
				HasImplementationHistory: c.hasImplementationHistory,
				SelfReviewAcknowledged:   c.selfReviewAcknowledged,
			}
			got := EvaluateReviewerEligibility(in)
			if got.Allowed != c.wantAllowed {
				t.Errorf("Allowed: got %v, want %v (msg=%q)", got.Allowed, c.wantAllowed, got.RejectionMessage)
			}
			if got.SelfReview != c.wantSelfReview {
				t.Errorf("SelfReview: got %v, want %v", got.SelfReview, c.wantSelfReview)
			}
			if got.RequiresReason != c.wantRequiresReason {
				t.Errorf("RequiresReason: got %v, want %v", got.RequiresReason, c.wantRequiresReason)
			}
		})
	}
}

// TestEvaluateReviewerEligibility_Trusted_TeachingMessage asserts the rejection
// for an unattested approval names BOTH acknowledgement paths plus the issue
// ID. --reviewed-by must come first: it is the expected path for orchestrated
// work, and an agent that reads only the start of the message should reach for
// attribution rather than claiming a self-review it did not perform.
func TestEvaluateReviewerEligibility_Trusted_TeachingMessage(t *testing.T) {
	issue := inReview("ses-creator", "ses-impl")
	got := EvaluateReviewerEligibility(ReviewerEligibilityInput{
		Mode:                 ModeTrusted,
		Issue:                issue,
		SessionID:            "ses-impl",
		SessionIsImplementer: true,
	})
	if got.Allowed {
		t.Fatal("implementer without an attestation should be rejected")
	}
	msg := got.RejectionMessage
	for _, want := range []string{"--reviewed-by", "--self-review", "--reason", issue.ID} {
		if !strings.Contains(msg, want) {
			t.Errorf("teaching message missing %q: %s", want, msg)
		}
	}
	if strings.Index(msg, "--reviewed-by") > strings.Index(msg, "--self-review") {
		t.Errorf("--reviewed-by must be offered before --self-review: %s", msg)
	}
}

// TestEvaluateReviewerEligibility_Trusted_Attribution covers the --reviewed-by
// path: an implementation-involved session may approve when it names who
// reviewed the work. The row records BOTH facts — an involved session wrote it
// (SelfReview) and the review is credited elsewhere (AttributedTo) — so neither
// field is a lie. No reason is required; the attribution is the substance.
func TestEvaluateReviewerEligibility_Trusted_Attribution(t *testing.T) {
	issue := inReview("ses-creator", "ses-impl")

	got := EvaluateReviewerEligibility(ReviewerEligibilityInput{
		Mode:                 ModeTrusted,
		Issue:                issue,
		SessionID:            "ses-impl",
		SessionIsImplementer: true,
		AttributedTo:         "code-reviewer sub-agent",
	})
	if !got.Allowed {
		t.Fatalf("attributed approval should be allowed: %s", got.RejectionMessage)
	}
	if !got.SelfReview {
		t.Error("an involved session's row must still be stamped SelfReview for audit")
	}
	if got.AttributedTo != "code-reviewer sub-agent" {
		t.Errorf("AttributedTo = %q, want the attribution echoed back", got.AttributedTo)
	}
	if got.RequiresReason {
		t.Error("attributed approval must not require a reason")
	}

	// Implementation history without being the implementer-of-record takes the
	// same path.
	got = EvaluateReviewerEligibility(ReviewerEligibilityInput{
		Mode:                     ModeTrusted,
		Issue:                    issue,
		SessionID:                "ses-helper",
		HasImplementationHistory: true,
		AttributedTo:             "reviewer-2",
	})
	if !got.Allowed || !got.SelfReview || got.AttributedTo != "reviewer-2" {
		t.Errorf("impl-history session with attribution: got %+v", got)
	}
}

// TestEvaluateReviewerEligibility_AttributionNeverGrants is the security
// property of the whole design: --reviewed-by is an audit record, not a
// permission. Only trusted mode treats it as an acknowledgement; the modes that
// exist to enforce a mechanical independence boundary must ignore it entirely.
func TestEvaluateReviewerEligibility_AttributionNeverGrants(t *testing.T) {
	issue := inReview("ses-creator", "ses-impl")

	for _, mode := range []Mode{ModeStrict, ModeBalanced, ModeDelegated, Mode("bogus")} {
		got := EvaluateReviewerEligibility(ReviewerEligibilityInput{
			Mode:                 mode,
			Issue:                issue,
			SessionID:            "ses-impl",
			SessionIsImplementer: true,
			WasAnyInvolved:       true,
			AttributedTo:         "some sub-agent",
		})
		if got.Allowed {
			t.Errorf("mode %s: attribution must not grant approval to the implementer", mode)
		}
	}
}

// TestEvaluateReviewerEligibility_AttributionEchoedWhenIndependent covers an
// independent session crediting someone else — allowed in every mode, and
// never mistaken for a self-review.
func TestEvaluateReviewerEligibility_AttributionEchoedWhenIndependent(t *testing.T) {
	issue := inReview("ses-creator", "ses-impl")

	for _, mode := range []Mode{ModeDelegated, ModeTrusted} {
		got := EvaluateReviewerEligibility(ReviewerEligibilityInput{
			Mode:         mode,
			Issue:        issue,
			SessionID:    "ses-outsider",
			AttributedTo: "human reviewer",
		})
		if !got.Allowed {
			t.Errorf("mode %s: independent session should be allowed: %s", mode, got.RejectionMessage)
		}
		if got.SelfReview {
			t.Errorf("mode %s: independent session's row must not be stamped SelfReview", mode)
		}
		if got.AttributedTo != "human reviewer" {
			t.Errorf("mode %s: AttributedTo = %q, want it echoed", mode, got.AttributedTo)
		}
	}
}

// TestEvaluateCloseEligibility_Trusted_Attribution covers the direct
// review+close fast path (Case 2): attribution satisfies the involved-session
// acknowledgement for closing too, and does not force a reason the way an
// unattributed self-review does.
func TestEvaluateCloseEligibility_Trusted_Attribution(t *testing.T) {
	issue := inReview("ses-creator", "ses-impl")

	got := EvaluateCloseEligibility(CloseEligibilityInput{
		Mode:                 ModeTrusted,
		Issue:                issue,
		SessionID:            "ses-impl",
		SessionIsImplementer: true,
		AttributedTo:         "code-reviewer sub-agent",
	})
	if !got.Allowed {
		t.Fatalf("attributed close should be allowed: %s", got.RejectionMessage)
	}
	if got.RequiresReason {
		t.Error("attributed close must not require a reason")
	}

	// Same session, no attestation at all -> still rejected.
	got = EvaluateCloseEligibility(CloseEligibilityInput{
		Mode:                 ModeTrusted,
		Issue:                issue,
		SessionID:            "ses-impl",
		SessionIsImplementer: true,
	})
	if got.Allowed {
		t.Error("implementer with no attestation must not close")
	}

	// Unattributed self-review still requires a reason.
	got = EvaluateCloseEligibility(CloseEligibilityInput{
		Mode:                   ModeTrusted,
		Issue:                  issue,
		SessionID:              "ses-impl",
		SessionIsImplementer:   true,
		SelfReviewAcknowledged: true,
	})
	if !got.Allowed || !got.RequiresReason {
		t.Errorf("acknowledged self-review close: got %+v, want allowed with RequiresReason", got)
	}
}

func TestEvaluateReviewerEligibility_Trusted_UnknownModeFailsClosed(t *testing.T) {
	// A misconfigured mode must fall through to strict, not trusted's flag path.
	issue := inReview("ses-creator", "ses-impl")
	got := EvaluateReviewerEligibility(ReviewerEligibilityInput{
		Mode:                   Mode("bogus"),
		Issue:                  issue,
		SessionID:              "ses-impl",
		SessionIsImplementer:   true,
		SelfReviewAcknowledged: true, // flag must NOT help under unknown mode
	})
	if got.Allowed {
		t.Errorf("unknown mode must fail closed to strict, got allowed (msg=%q)", got.RejectionMessage)
	}
}

func TestEvaluateCloseEligibility_Trusted(t *testing.T) {
	inReviewIssue := inReview("ses-creator", "ses-impl")
	openIssue := func() *models.Issue {
		is := inReview("ses-creator", "ses-impl")
		is.Status = models.StatusOpen
		return is
	}

	cases := []struct {
		name                     string
		issue                    *models.Issue
		sessionID                string
		sessionIsImplementer     bool
		sessionIsCreator         bool
		hasImplementationHistory bool
		hasActiveApproval        bool
		selfReviewAcknowledged   bool
		wantAllowed              bool
		wantRequiresReason       bool
	}{
		{
			name:                 "case1: in_review with active approval, implementer may close",
			issue:                inReviewIssue,
			sessionID:            "ses-impl",
			sessionIsImplementer: true,
			hasActiveApproval:    true,
			wantAllowed:          true,
		},
		{
			name:        "case2: in_review no approval, independent session direct close",
			issue:       inReviewIssue,
			sessionID:   "ses-fresh",
			wantAllowed: true,
		},
		{
			name:                 "case2: in_review no approval, implementer without flag blocked",
			issue:                inReviewIssue,
			sessionID:            "ses-impl",
			sessionIsImplementer: true,
			wantAllowed:          false,
		},
		{
			name:                   "case2: in_review no approval, implementer with flag direct closes (requires reason)",
			issue:                  inReviewIssue,
			sessionID:              "ses-impl",
			sessionIsImplementer:   true,
			selfReviewAcknowledged: true,
			wantAllowed:            true,
			wantRequiresReason:     true,
		},
		{
			name:                 "case3: not in_review, implementer blocked even with flag",
			issue:                openIssue(),
			sessionID:            "ses-impl",
			sessionIsImplementer: true,
			// flag does not relax the non-in_review gate (Case 3 = strict/balanced)
			selfReviewAcknowledged: true,
			wantAllowed:            false,
		},
		{
			name:             "case3: creator-open-bypass on never-implemented self-created open issue",
			issue:            func() *models.Issue { is := openIssue(); is.ImplementerSession = ""; return is }(),
			sessionID:        "ses-creator",
			sessionIsCreator: true,
			wantAllowed:      true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := CloseEligibilityInput{
				Mode:                     ModeTrusted,
				Issue:                    c.issue,
				SessionID:                c.sessionID,
				SessionIsImplementer:     c.sessionIsImplementer,
				SessionIsCreator:         c.sessionIsCreator,
				HasImplementationHistory: c.hasImplementationHistory,
				HasActiveApproval:        c.hasActiveApproval,
				SelfReviewAcknowledged:   c.selfReviewAcknowledged,
			}
			got := EvaluateCloseEligibility(in)
			if got.Allowed != c.wantAllowed {
				t.Errorf("Allowed: got %v, want %v (msg=%q)", got.Allowed, c.wantAllowed, got.RejectionMessage)
			}
			if got.RequiresReason != c.wantRequiresReason {
				t.Errorf("RequiresReason: got %v, want %v", got.RequiresReason, c.wantRequiresReason)
			}
		})
	}
}

func TestIsReviewInvalidatingMutation(t *testing.T) {
	// Zero value must not invalidate.
	if IsReviewInvalidatingMutation(IssueMutation{}) {
		t.Error("zero-value mutation should not invalidate")
	}

	// Each flag individually must invalidate.
	setters := []struct {
		name string
		set  func(*IssueMutation)
	}{
		{"description", func(m *IssueMutation) { m.DescriptionChanged = true }},
		{"title", func(m *IssueMutation) { m.TitleChanged = true }},
		{"type", func(m *IssueMutation) { m.TypeChanged = true }},
		{"priority", func(m *IssueMutation) { m.PriorityChanged = true }},
		{"minor", func(m *IssueMutation) { m.MinorChanged = true }},
		{"parent_id", func(m *IssueMutation) { m.ParentIDChanged = true }},
		{"status_from_review_not_closing", func(m *IssueMutation) { m.StatusChangedFromReviewNotClosing = true }},
		{"linked_files", func(m *IssueMutation) { m.LinkedFilesChanged = true }},
		{"dependencies", func(m *IssueMutation) { m.DependenciesChanged = true }},
		{"work_session_tags", func(m *IssueMutation) { m.WorkSessionTagsChanged = true }},
		{"reparent_cascade", func(m *IssueMutation) { m.ReparentCascade = true }},
	}

	for _, s := range setters {
		t.Run("single/"+s.name, func(t *testing.T) {
			var m IssueMutation
			s.set(&m)
			if !IsReviewInvalidatingMutation(m) {
				t.Errorf("flag %s should invalidate", s.name)
			}
		})
	}

	// Combination: all flags set must still be true.
	t.Run("all_combined", func(t *testing.T) {
		var m IssueMutation
		for _, s := range setters {
			s.set(&m)
		}
		if !IsReviewInvalidatingMutation(m) {
			t.Error("all flags set should invalidate")
		}
	})
}

// Guard: rejection-reason constants stay present and nonempty. If any caller
// collapses these to literal strings the sharing contract breaks silently.
func TestRejectionReasonConstantsNonEmpty(t *testing.T) {
	reasons := map[string]string{
		"ReasonImplementerCannotReview": ReasonImplementerCannotReview,
		"ReasonPriorInvolvement":        ReasonPriorInvolvement,
		"ReasonIssueNotInReview":        ReasonIssueNotInReview,
		"ReasonNoActiveReview":          ReasonNoActiveReview,
		"ReasonNotAllowedCloser":        ReasonNotAllowedCloser,
		"ReasonIssueNotFound":           ReasonIssueNotFound,
	}
	for name, v := range reasons {
		if strings.TrimSpace(v) == "" {
			t.Errorf("constant %s is empty", name)
		}
	}
}

// TestEvaluateCloseEligibility_AttributionNeverGrants is the close-path twin of
// TestEvaluateReviewerEligibility_AttributionNeverGrants. An independent review
// of td-478958 mutated the close predicates to honor AttributedTo and the whole
// suite still passed — the reviewer-path test alone does not cover close.
//
// The property: attribution is an audit record, not a permission. Only trusted
// mode treats it as an acknowledgement. Strict, balanced, delegated, and any
// misconfigured mode must ignore it entirely on the close path too, or a
// project that pinned a mode specifically for a mechanical independence
// boundary loses that boundary to a free-text string.
func TestEvaluateCloseEligibility_AttributionNeverGrants(t *testing.T) {
	for _, mode := range []Mode{ModeStrict, ModeBalanced, ModeDelegated, Mode("bogus")} {
		// in_review, no recorded approval: the implementer must not be able to
		// close by naming a reviewer.
		got := EvaluateCloseEligibility(CloseEligibilityInput{
			Mode:                 mode,
			Issue:                inReview("ses-creator", "ses-impl"),
			SessionID:            "ses-impl",
			SessionIsImplementer: true,
			WasAnyInvolved:       true,
			AttributedTo:         "some sub-agent",
		})
		if got.Allowed {
			t.Errorf("mode %s (in_review): attribution must not grant close to the implementer", mode)
		}

		// Still open / never submitted for review: attribution must not
		// short-circuit the non-in_review gate either.
		openIssue := inReview("ses-creator", "ses-impl")
		openIssue.Status = models.StatusInProgress
		got = EvaluateCloseEligibility(CloseEligibilityInput{
			Mode:                     mode,
			Issue:                    openIssue,
			SessionID:                "ses-impl",
			SessionIsImplementer:     true,
			HasImplementationHistory: true,
			WasAnyInvolved:           true,
			AttributedTo:             "some sub-agent",
		})
		if got.Allowed {
			t.Errorf("mode %s (in_progress): attribution must not grant close outside in_review", mode)
		}
	}
}

// TestEvaluateCloseEligibility_Trusted_AttributionScope pins where attribution
// DOES apply in trusted mode: the in_review direct review+close fast path, and
// nowhere else. An in_progress issue is not closeable by naming a reviewer —
// the work was never submitted for review at all.
func TestEvaluateCloseEligibility_Trusted_AttributionScope(t *testing.T) {
	inProgress := inReview("ses-creator", "ses-impl")
	inProgress.Status = models.StatusInProgress

	got := EvaluateCloseEligibility(CloseEligibilityInput{
		Mode:                     ModeTrusted,
		Issue:                    inProgress,
		SessionID:                "ses-impl",
		SessionIsImplementer:     true,
		HasImplementationHistory: true,
		AttributedTo:             "code-reviewer sub-agent",
	})
	if got.Allowed {
		t.Error("trusted: attribution must not close an issue that was never submitted for review")
	}
}
