package workflow

import (
	"github.com/marcus/td/internal/models"
)

// TransitionMode controls how guards are applied
type TransitionMode int

const (
	// ModeLiberal disables all guard checks (default, preserves existing behavior)
	ModeLiberal TransitionMode = iota
	// ModeAdvisory runs guards but only returns warnings, allows transition
	ModeAdvisory
	// ModeStrict blocks transitions when guards fail
	ModeStrict
)

// ActionContext identifies the source of the transition request.
// Used by guards to apply context-specific rules (e.g., admin bypass).
// Currently only ContextAdmin is checked by DifferentReviewerGuard.
type ActionContext string

const (
	// ContextCLI indicates transition from CLI commands
	ContextCLI ActionContext = "cli"
	// ContextMonitor indicates transition from TUI monitor
	ContextMonitor ActionContext = "monitor"
	// ContextWorkSession indicates transition from work session commands
	ContextWorkSession ActionContext = "worksession"
	// ContextAdmin indicates administrative bypass (allows self-approval)
	ContextAdmin ActionContext = "admin"
)

// GuardResult represents the outcome of a guard check
type GuardResult struct {
	Passed  bool
	Message string
	Guard   string
}

// Guard checks whether a transition should be allowed
type Guard interface {
	Name() string
	Check(ctx *TransitionContext) GuardResult
}

// TransitionContext provides context for a status transition
type TransitionContext struct {
	Issue       *models.Issue
	FromStatus  models.Status
	ToStatus    models.Status
	SessionID   string
	Force       bool
	Minor       bool
	Context     ActionContext
	WasInvolved bool // Whether current session was involved with issue
}

// Transition defines a valid status transition with optional guards
type Transition struct {
	From   models.Status
	To     models.Status
	Guards []Guard
}

// StateMachine manages issue status transitions
type StateMachine struct {
	transitions map[models.Status]map[models.Status]*Transition
	mode        TransitionMode
}

// New creates a new StateMachine with the given mode
func New(mode TransitionMode) *StateMachine {
	sm := &StateMachine{
		transitions: make(map[models.Status]map[models.Status]*Transition),
		mode:        mode,
	}
	sm.registerTransitions()
	return sm
}

// DefaultMachine returns a state machine with liberal mode (existing behavior)
func DefaultMachine() *StateMachine {
	return New(ModeLiberal)
}

// AdvisoryMachine returns a state machine that warns but allows transitions
func AdvisoryMachine() *StateMachine {
	return New(ModeAdvisory)
}

// StrictMachine returns a state machine that blocks invalid transitions
func StrictMachine() *StateMachine {
	return New(ModeStrict)
}

// Mode returns the current transition mode
func (sm *StateMachine) Mode() TransitionMode {
	return sm.mode
}

// SetMode changes the transition mode
func (sm *StateMachine) SetMode(mode TransitionMode) {
	sm.mode = mode
}

// registerTransitions sets up all valid status transitions
func (sm *StateMachine) registerTransitions() {
	// Define all transitions - see transitions.go for full definitions
	for _, t := range AllTransitions() {
		sm.addTransition(t)
	}
}

// addTransition registers a transition in the state machine
func (sm *StateMachine) addTransition(t *Transition) {
	if sm.transitions[t.From] == nil {
		sm.transitions[t.From] = make(map[models.Status]*Transition)
	}
	sm.transitions[t.From][t.To] = t
}

// IsValidTransition checks if a transition exists in the state machine
func (sm *StateMachine) IsValidTransition(from, to models.Status) bool {
	if toMap, ok := sm.transitions[from]; ok {
		_, exists := toMap[to]
		return exists
	}
	return false
}

// GetTransition returns the transition definition if it exists
func (sm *StateMachine) GetTransition(from, to models.Status) *Transition {
	if toMap, ok := sm.transitions[from]; ok {
		return toMap[to]
	}
	return nil
}

// Validate checks if a transition is allowed and returns any guard results
func (sm *StateMachine) Validate(ctx *TransitionContext) ([]GuardResult, error) {
	// Validate context
	if ctx == nil {
		return nil, &TransitionError{Reason: "nil context"}
	}
	if ctx.Issue == nil {
		return nil, &TransitionError{
			From:   ctx.FromStatus,
			To:     ctx.ToStatus,
			Reason: "nil issue in context",
		}
	}

	// First check if the transition path exists
	transition := sm.GetTransition(ctx.FromStatus, ctx.ToStatus)
	if transition == nil {
		return nil, &TransitionError{
			From:    ctx.FromStatus,
			To:      ctx.ToStatus,
			IssueID: ctx.Issue.ID,
			Reason:  "transition not allowed",
		}
	}

	// In liberal mode, skip all guard checks
	if sm.mode == ModeLiberal {
		return nil, nil
	}

	// Run guards
	var results []GuardResult
	var validationErr ValidationError

	for _, guard := range transition.Guards {
		result := guard.Check(ctx)
		result.Guard = guard.Name()
		results = append(results, result)

		if !result.Passed {
			validationErr.Add(&GuardError{
				GuardName: guard.Name(),
				Reason:    result.Message,
				IssueID:   ctx.Issue.ID,
			})
		}
	}

	// In advisory mode, return warnings but no error
	if sm.mode == ModeAdvisory {
		return results, nil
	}

	// In strict mode, return error if any guard failed
	if validationErr.HasErrors() {
		return results, &validationErr
	}

	return results, nil
}

// CanTransition checks if a transition can be performed (convenience method)
func (sm *StateMachine) CanTransition(ctx *TransitionContext) (bool, []GuardResult) {
	results, err := sm.Validate(ctx)
	return err == nil, results
}

// GetAllowedTransitions returns all valid target statuses from a given status
func (sm *StateMachine) GetAllowedTransitions(from models.Status) []models.Status {
	var allowed []models.Status
	if toMap, ok := sm.transitions[from]; ok {
		for to := range toMap {
			allowed = append(allowed, to)
		}
	}
	return allowed
}

// GetAllTransitions returns all registered transitions
func (sm *StateMachine) GetAllTransitions() []*Transition {
	var all []*Transition
	for _, toMap := range sm.transitions {
		for _, t := range toMap {
			all = append(all, t)
		}
	}
	return all
}
