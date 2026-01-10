package workflow

import (
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestIsValidTransition(t *testing.T) {
	sm := DefaultMachine()

	tests := []struct {
		name     string
		from     models.Status
		to       models.Status
		expected bool
	}{
		// Valid transitions from open
		{"open → in_progress", models.StatusOpen, models.StatusInProgress, true},
		{"open → blocked", models.StatusOpen, models.StatusBlocked, true},
		{"open → in_review", models.StatusOpen, models.StatusInReview, true},
		{"open → closed", models.StatusOpen, models.StatusClosed, true},

		// Valid transitions from in_progress
		{"in_progress → open", models.StatusInProgress, models.StatusOpen, true},
		{"in_progress → blocked", models.StatusInProgress, models.StatusBlocked, true},
		{"in_progress → in_review", models.StatusInProgress, models.StatusInReview, true},
		{"in_progress → closed", models.StatusInProgress, models.StatusClosed, true},

		// Valid transitions from blocked
		{"blocked → open", models.StatusBlocked, models.StatusOpen, true},
		{"blocked → in_progress", models.StatusBlocked, models.StatusInProgress, true},
		{"blocked → closed", models.StatusBlocked, models.StatusClosed, true},

		// Invalid: blocked cannot go to in_review
		{"blocked → in_review", models.StatusBlocked, models.StatusInReview, false},

		// Valid transitions from in_review
		{"in_review → open", models.StatusInReview, models.StatusOpen, true},
		{"in_review → in_progress", models.StatusInReview, models.StatusInProgress, true},
		{"in_review → closed", models.StatusInReview, models.StatusClosed, true},

		// Invalid: in_review cannot go to blocked
		{"in_review → blocked", models.StatusInReview, models.StatusBlocked, false},

		// Valid transitions from closed
		{"closed → open", models.StatusClosed, models.StatusOpen, true},

		// Invalid: closed cannot go anywhere else
		{"closed → in_progress", models.StatusClosed, models.StatusInProgress, false},
		{"closed → blocked", models.StatusClosed, models.StatusBlocked, false},
		{"closed → in_review", models.StatusClosed, models.StatusInReview, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sm.IsValidTransition(tt.from, tt.to)
			if result != tt.expected {
				t.Errorf("IsValidTransition(%s, %s) = %v, want %v", tt.from, tt.to, result, tt.expected)
			}
		})
	}
}

func TestLiberalModeAllowsAllTransitions(t *testing.T) {
	sm := DefaultMachine()

	// Even with guards, liberal mode should allow all valid transitions
	issue := &models.Issue{
		ID:                 "test-1",
		Status:             models.StatusBlocked,
		ImplementerSession: "session-1",
	}

	ctx := &TransitionContext{
		Issue:      issue,
		FromStatus: models.StatusBlocked,
		ToStatus:   models.StatusInProgress,
		SessionID:  "session-1",
		Force:      false, // Not forcing
	}

	results, err := sm.Validate(ctx)
	if err != nil {
		t.Errorf("Liberal mode should allow transition, got error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Liberal mode should skip guards, got %d results", len(results))
	}
}

func TestStrictModeBlocksGuardFailures(t *testing.T) {
	sm := StrictMachine()

	issue := &models.Issue{
		ID:                 "test-1",
		Status:             models.StatusBlocked,
		ImplementerSession: "session-1",
	}

	ctx := &TransitionContext{
		Issue:      issue,
		FromStatus: models.StatusBlocked,
		ToStatus:   models.StatusInProgress,
		SessionID:  "session-1",
		Force:      false,
	}

	_, err := sm.Validate(ctx)
	if err == nil {
		t.Error("Strict mode should block transition when BlockedGuard fails")
	}
}

func TestStrictModeAllowsWithForce(t *testing.T) {
	sm := StrictMachine()

	issue := &models.Issue{
		ID:                 "test-1",
		Status:             models.StatusBlocked,
		ImplementerSession: "session-1",
	}

	ctx := &TransitionContext{
		Issue:      issue,
		FromStatus: models.StatusBlocked,
		ToStatus:   models.StatusInProgress,
		SessionID:  "session-1",
		Force:      true, // Force flag set
	}

	_, err := sm.Validate(ctx)
	if err != nil {
		t.Errorf("Strict mode should allow transition with --force, got: %v", err)
	}
}

func TestAdvisoryModeReturnsWarnings(t *testing.T) {
	sm := AdvisoryMachine()

	issue := &models.Issue{
		ID:                 "test-1",
		Status:             models.StatusBlocked,
		ImplementerSession: "session-1",
	}

	ctx := &TransitionContext{
		Issue:      issue,
		FromStatus: models.StatusBlocked,
		ToStatus:   models.StatusInProgress,
		SessionID:  "session-1",
		Force:      false,
	}

	results, err := sm.Validate(ctx)
	if err != nil {
		t.Errorf("Advisory mode should allow transition, got error: %v", err)
	}
	if len(results) == 0 {
		t.Error("Advisory mode should return guard results")
	}
	if results[0].Passed {
		t.Error("Advisory mode should report guard failure in results")
	}
}

func TestDifferentReviewerGuard(t *testing.T) {
	sm := StrictMachine()

	tests := []struct {
		name        string
		implementer string
		reviewer    string
		minor       bool
		wasInvolved bool
		shouldPass  bool
	}{
		{"different reviewer", "session-1", "session-2", false, false, true},
		{"same reviewer (blocked)", "session-1", "session-1", false, true, false},
		{"same reviewer minor (allowed)", "session-1", "session-1", true, true, true},
		{"no implementer", "", "session-2", false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &models.Issue{
				ID:                 "test-1",
				Status:             models.StatusInReview,
				ImplementerSession: tt.implementer,
				Minor:              tt.minor,
			}

			ctx := &TransitionContext{
				Issue:       issue,
				FromStatus:  models.StatusInReview,
				ToStatus:    models.StatusClosed,
				SessionID:   tt.reviewer,
				Minor:       tt.minor,
				WasInvolved: tt.wasInvolved,
			}

			_, err := sm.Validate(ctx)
			passed := err == nil

			if passed != tt.shouldPass {
				t.Errorf("DifferentReviewerGuard: expected pass=%v, got pass=%v (err=%v)",
					tt.shouldPass, passed, err)
			}
		})
	}
}

func TestBlockedGuard(t *testing.T) {
	guard := &BlockedGuard{}

	tests := []struct {
		name       string
		fromStatus models.Status
		force      bool
		shouldPass bool
	}{
		{"blocked without force", models.StatusBlocked, false, false},
		{"blocked with force", models.StatusBlocked, true, true},
		{"open (not blocked)", models.StatusOpen, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TransitionContext{
				Issue:      &models.Issue{ID: "test-1", Status: tt.fromStatus},
				FromStatus: tt.fromStatus,
				ToStatus:   models.StatusInProgress,
				Force:      tt.force,
			}

			result := guard.Check(ctx)
			if result.Passed != tt.shouldPass {
				t.Errorf("BlockedGuard: expected pass=%v, got pass=%v", tt.shouldPass, result.Passed)
			}
		})
	}
}

func TestEpicChildrenGuard(t *testing.T) {
	tests := []struct {
		name           string
		issueType      models.Type
		toStatus       models.Status
		openChildCount int
		shouldPass     bool
	}{
		{"epic with open children", models.TypeEpic, models.StatusClosed, 3, false},
		{"epic with no open children", models.TypeEpic, models.StatusClosed, 0, true},
		{"task (not epic)", models.TypeTask, models.StatusClosed, 3, true},
		{"epic not closing", models.TypeEpic, models.StatusInProgress, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := &EpicChildrenGuard{OpenChildCount: tt.openChildCount}
			ctx := &TransitionContext{
				Issue:    &models.Issue{ID: "test-1", Type: tt.issueType},
				ToStatus: tt.toStatus,
			}

			result := guard.Check(ctx)
			if result.Passed != tt.shouldPass {
				t.Errorf("EpicChildrenGuard: expected pass=%v, got pass=%v", tt.shouldPass, result.Passed)
			}
		})
	}
}

func TestGetAllowedTransitions(t *testing.T) {
	sm := DefaultMachine()

	tests := []struct {
		from     models.Status
		expected int
	}{
		{models.StatusOpen, 4},       // in_progress, blocked, in_review, closed
		{models.StatusInProgress, 4}, // open, blocked, in_review, closed
		{models.StatusBlocked, 3},    // open, in_progress, closed
		{models.StatusInReview, 3},   // open, in_progress, closed
		{models.StatusClosed, 1},     // open only
	}

	for _, tt := range tests {
		t.Run(string(tt.from), func(t *testing.T) {
			allowed := sm.GetAllowedTransitions(tt.from)
			if len(allowed) != tt.expected {
				t.Errorf("GetAllowedTransitions(%s) = %d transitions, want %d", tt.from, len(allowed), tt.expected)
			}
		})
	}
}

func TestTransitionName(t *testing.T) {
	tests := []struct {
		from     models.Status
		to       models.Status
		expected string
	}{
		{models.StatusOpen, models.StatusInProgress, "start"},
		{models.StatusInProgress, models.StatusOpen, "unstart"},
		{models.StatusOpen, models.StatusBlocked, "block"},
		{models.StatusBlocked, models.StatusOpen, "unblock"},
		{models.StatusInProgress, models.StatusInReview, "review"},
		{models.StatusInReview, models.StatusInProgress, "reject"},
		{models.StatusInReview, models.StatusClosed, "approve"},
		{models.StatusInProgress, models.StatusClosed, "close"},
		{models.StatusClosed, models.StatusOpen, "reopen"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			name := TransitionName(tt.from, tt.to)
			if name != tt.expected {
				t.Errorf("TransitionName(%s, %s) = %q, want %q", tt.from, tt.to, name, tt.expected)
			}
		})
	}
}

func TestAllStatuses(t *testing.T) {
	statuses := AllStatuses()
	if len(statuses) != 5 {
		t.Errorf("AllStatuses() returned %d statuses, want 5", len(statuses))
	}

	expected := []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
		models.StatusBlocked,
		models.StatusInReview,
		models.StatusClosed,
	}

	for i, s := range expected {
		if statuses[i] != s {
			t.Errorf("AllStatuses()[%d] = %s, want %s", i, statuses[i], s)
		}
	}
}

func TestCanTransition(t *testing.T) {
	sm := DefaultMachine()

	issue := &models.Issue{
		ID:     "test-1",
		Status: models.StatusOpen,
	}

	ctx := &TransitionContext{
		Issue:      issue,
		FromStatus: models.StatusOpen,
		ToStatus:   models.StatusInProgress,
		SessionID:  "session-1",
	}

	can, _ := sm.CanTransition(ctx)
	if !can {
		t.Error("CanTransition should return true for valid transition")
	}
}

func TestTransitionError(t *testing.T) {
	err := &TransitionError{
		From:    models.StatusClosed,
		To:      models.StatusInProgress,
		IssueID: "test-123",
		Reason:  "transition not allowed",
	}

	msg := err.Error()
	if msg != "cannot transition test-123 from closed to in_progress: transition not allowed" {
		t.Errorf("TransitionError.Error() = %q", msg)
	}

	// Test without issue ID
	err.IssueID = ""
	msg = err.Error()
	if msg != "cannot transition from closed to in_progress: transition not allowed" {
		t.Errorf("TransitionError.Error() = %q", msg)
	}
}

func TestGuardError(t *testing.T) {
	err := &GuardError{
		GuardName: "BlockedGuard",
		IssueID:   "test-123",
		Reason:    "cannot start blocked issue",
	}

	msg := err.Error()
	if msg != "guard BlockedGuard failed for test-123: cannot start blocked issue" {
		t.Errorf("GuardError.Error() = %q", msg)
	}
}

func TestValidationError(t *testing.T) {
	ve := &ValidationError{}

	if ve.HasErrors() {
		t.Error("Empty ValidationError should not have errors")
	}

	ve.Add(&GuardError{GuardName: "G1", Reason: "failed"})
	if !ve.HasErrors() {
		t.Error("ValidationError with error should have errors")
	}
	if ve.Error() != "guard G1 failed: failed" {
		t.Errorf("Single error message: %q", ve.Error())
	}

	ve.Add(&GuardError{GuardName: "G2", Reason: "also failed"})
	if ve.Error() != "2 validation errors" {
		t.Errorf("Multiple errors message: %q", ve.Error())
	}
}

func TestValidateNilContext(t *testing.T) {
	sm := DefaultMachine()

	// Test nil context
	_, err := sm.Validate(nil)
	if err == nil {
		t.Error("Expected error for nil context")
	}
	if te, ok := err.(*TransitionError); !ok {
		t.Errorf("Expected TransitionError, got %T", err)
	} else if te.Reason != "nil context" {
		t.Errorf("Expected 'nil context' reason, got %q", te.Reason)
	}

	// Test nil issue in context
	ctx := &TransitionContext{
		Issue:      nil,
		FromStatus: models.StatusOpen,
		ToStatus:   models.StatusInProgress,
	}
	_, err = sm.Validate(ctx)
	if err == nil {
		t.Error("Expected error for nil issue")
	}
	if te, ok := err.(*TransitionError); !ok {
		t.Errorf("Expected TransitionError, got %T", err)
	} else if te.Reason != "nil issue in context" {
		t.Errorf("Expected 'nil issue in context' reason, got %q", te.Reason)
	}
}
