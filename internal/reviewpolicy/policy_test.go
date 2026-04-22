package reviewpolicy

import (
	"strings"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestParseMode(t *testing.T) {
	t.Run("round trip for each known mode", func(t *testing.T) {
		cases := []Mode{ModeStrict, ModeBalanced, ModeDelegated}
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
	for _, mode := range []Mode{ModeStrict, ModeBalanced, ModeDelegated} {
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
	for _, mode := range []Mode{ModeStrict, ModeBalanced, ModeDelegated} {
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
		{"arbitrary session rejected", false, false, false, false, false, false},
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
