package workflow

import (
	"github.com/marcus/td/internal/models"
)

// AllTransitions returns all valid status transitions
// This defines the complete workflow state machine
func AllTransitions() []*Transition {
	return []*Transition{
		// From open
		{From: models.StatusOpen, To: models.StatusInProgress, Guards: nil},
		{From: models.StatusOpen, To: models.StatusBlocked, Guards: nil},
		{From: models.StatusOpen, To: models.StatusInReview, Guards: nil},
		{From: models.StatusOpen, To: models.StatusClosed, Guards: nil},

		// From in_progress
		{From: models.StatusInProgress, To: models.StatusOpen, Guards: nil},
		{From: models.StatusInProgress, To: models.StatusBlocked, Guards: nil},
		{From: models.StatusInProgress, To: models.StatusInReview, Guards: nil},
		{From: models.StatusInProgress, To: models.StatusClosed, Guards: nil},

		// From blocked
		{From: models.StatusBlocked, To: models.StatusOpen, Guards: nil},
		{From: models.StatusBlocked, To: models.StatusInProgress, Guards: []Guard{&BlockedGuard{}}},
		{From: models.StatusBlocked, To: models.StatusClosed, Guards: nil},

		// From in_review
		{From: models.StatusInReview, To: models.StatusOpen, Guards: nil},
		{From: models.StatusInReview, To: models.StatusInProgress, Guards: nil},
		{From: models.StatusInReview, To: models.StatusClosed, Guards: []Guard{&DifferentReviewerGuard{}}},

		// From closed
		{From: models.StatusClosed, To: models.StatusOpen, Guards: nil},
	}
}

// TransitionName returns a human-readable name for the transition
func TransitionName(from, to models.Status) string {
	switch {
	case from == models.StatusOpen && to == models.StatusInProgress:
		return "start"
	case from == models.StatusInProgress && to == models.StatusOpen:
		return "unstart"
	case to == models.StatusBlocked:
		return "block"
	case from == models.StatusBlocked && to == models.StatusOpen:
		return "unblock"
	case to == models.StatusInReview:
		return "review"
	case from == models.StatusInReview && to == models.StatusOpen:
		return "reject"
	case from == models.StatusInReview && to == models.StatusClosed:
		return "approve"
	case to == models.StatusClosed:
		return "close"
	case from == models.StatusClosed && to == models.StatusOpen:
		return "reopen"
	default:
		return string(from) + " → " + string(to)
	}
}

// GetTransitionsFrom returns all possible transitions from a given status
func GetTransitionsFrom(status models.Status) []models.Status {
	var targets []models.Status
	for _, t := range AllTransitions() {
		if t.From == status {
			targets = append(targets, t.To)
		}
	}
	return targets
}

// GetTransitionsTo returns all statuses that can transition to the given status
func GetTransitionsTo(status models.Status) []models.Status {
	var sources []models.Status
	for _, t := range AllTransitions() {
		if t.To == status {
			sources = append(sources, t.From)
		}
	}
	return sources
}

// AllStatuses returns all valid statuses in workflow order
func AllStatuses() []models.Status {
	return []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
		models.StatusBlocked,
		models.StatusInReview,
		models.StatusClosed,
	}
}
