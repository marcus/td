package workflow

import (
	"testing"

	"github.com/marcus/td/internal/models"
)

// TestIntegrationWorkflowScenarios tests common workflow scenarios
func TestIntegrationWorkflowScenarios(t *testing.T) {
	t.Run("typical issue lifecycle", func(t *testing.T) {
		sm := DefaultMachine()

		// open → in_progress (start)
		if !sm.IsValidTransition(models.StatusOpen, models.StatusInProgress) {
			t.Error("Should allow open → in_progress")
		}

		// in_progress → in_review (review)
		if !sm.IsValidTransition(models.StatusInProgress, models.StatusInReview) {
			t.Error("Should allow in_progress → in_review")
		}

		// in_review → closed (approve)
		if !sm.IsValidTransition(models.StatusInReview, models.StatusClosed) {
			t.Error("Should allow in_review → closed")
		}
	})

	t.Run("blocked workflow", func(t *testing.T) {
		sm := DefaultMachine()

		// open → blocked
		if !sm.IsValidTransition(models.StatusOpen, models.StatusBlocked) {
			t.Error("Should allow open → blocked")
		}

		// blocked → in_progress (with force)
		if !sm.IsValidTransition(models.StatusBlocked, models.StatusInProgress) {
			t.Error("Should allow blocked → in_progress")
		}

		// blocked → open (unblock)
		if !sm.IsValidTransition(models.StatusBlocked, models.StatusOpen) {
			t.Error("Should allow blocked → open")
		}
	})

	t.Run("rejection workflow", func(t *testing.T) {
		sm := DefaultMachine()

		// in_review → open (reject resets to open so td next picks it up)
		if !sm.IsValidTransition(models.StatusInReview, models.StatusOpen) {
			t.Error("Should allow in_review → open")
		}

		// in_review → in_progress (still valid transition)
		if !sm.IsValidTransition(models.StatusInReview, models.StatusInProgress) {
			t.Error("Should allow in_review → in_progress")
		}
	})

	t.Run("reopen workflow", func(t *testing.T) {
		sm := DefaultMachine()

		// closed → open (reopen)
		if !sm.IsValidTransition(models.StatusClosed, models.StatusOpen) {
			t.Error("Should allow closed → open")
		}

		// closed cannot go to anything else
		if sm.IsValidTransition(models.StatusClosed, models.StatusInProgress) {
			t.Error("Should not allow closed → in_progress directly")
		}
	})

	t.Run("direct close scenarios", func(t *testing.T) {
		sm := DefaultMachine()

		// open → closed (admin close)
		if !sm.IsValidTransition(models.StatusOpen, models.StatusClosed) {
			t.Error("Should allow open → closed")
		}

		// in_progress → closed (close without review)
		if !sm.IsValidTransition(models.StatusInProgress, models.StatusClosed) {
			t.Error("Should allow in_progress → closed")
		}

		// blocked → closed (close blocked issue)
		if !sm.IsValidTransition(models.StatusBlocked, models.StatusClosed) {
			t.Error("Should allow blocked → closed")
		}
	})
}

// TestIntegrationGuardBehavior tests guard behavior in different modes
func TestIntegrationGuardBehavior(t *testing.T) {
	t.Run("liberal mode ignores guards", func(t *testing.T) {
		sm := DefaultMachine()

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

		_, err := sm.Validate(ctx)
		if err != nil {
			t.Errorf("Liberal mode should allow all transitions, got: %v", err)
		}
	})

	t.Run("advisory mode returns warnings", func(t *testing.T) {
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
			t.Errorf("Advisory mode should allow transition, got: %v", err)
		}
		if len(results) == 0 || results[0].Passed {
			t.Error("Advisory mode should return failed guard results")
		}
	})

	t.Run("strict mode blocks on guard failure", func(t *testing.T) {
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
			t.Error("Strict mode should block transition when guard fails")
		}
	})
}

// TestIntegrationInvalidTransitions ensures invalid paths are blocked
func TestIntegrationInvalidTransitions(t *testing.T) {
	sm := DefaultMachine()

	invalidTransitions := []struct {
		from models.Status
		to   models.Status
	}{
		// blocked cannot go to in_review
		{models.StatusBlocked, models.StatusInReview},

		// in_review cannot go to blocked
		{models.StatusInReview, models.StatusBlocked},

		// closed can only go to open
		{models.StatusClosed, models.StatusInProgress},
		{models.StatusClosed, models.StatusBlocked},
		{models.StatusClosed, models.StatusInReview},
	}

	for _, tt := range invalidTransitions {
		t.Run(string(tt.from)+" → "+string(tt.to), func(t *testing.T) {
			if sm.IsValidTransition(tt.from, tt.to) {
				t.Errorf("Should not allow %s → %s", tt.from, tt.to)
			}
		})
	}
}
