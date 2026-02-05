package e2e

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

// issueIDRe matches td-<hex> issue IDs in command output.
var issueIDRe = regexp.MustCompile(`td-[0-9a-f]+`)

// ActionResult records the outcome of a single chaos action.
type ActionResult struct {
	Action  string
	Actor   string
	Target  string // issue ID or other target
	OK      bool
	ExpFail bool // expected failure (self-review, cycle, etc.)
	Skipped bool // no suitable target found
	Output  string
}

// IssueState tracks the engine's view of an issue.
type IssueState struct {
	ID      string
	Status  string // open, in_progress, in_review, closed, blocked
	Owner   string // actor who created it
	Minor   bool
	Deleted bool
}

// ActionStats tracks per-action-type outcomes.
type ActionStats struct {
	OK      int
	ExpFail int
	UnexpFail int
}

// ChaosStats aggregates counters across all actions.
type ChaosStats struct {
	ActionCount        int
	SyncCount          int
	Skipped            int
	ExpectedFailures   int
	UnexpectedFailures int
	FieldCollisions    int
	DeleteMutate       int
	BurstCount         int
	BurstActions       int
	EdgeDataUsed       int
	PerAction          map[string]*ActionStats
}

// ChaosEngine drives random mutations against a Harness.
type ChaosEngine struct {
	Harness   *Harness
	Rng       *rand.Rand
	NumActors int

	// State tracking
	Issues      map[string]*IssueState // id -> state
	IssueOrder  []string               // ordered list of all created issue IDs
	Boards      []string
	DepPairs    map[string]bool   // "from_to" -> true
	ParentChild map[string]string // childID -> parentID
	IssueFiles  map[string]string // "issueID~filePath" -> role
	ActiveWS    map[string]string // actor -> ws name
	WSTagged    map[string]map[string]bool // actor -> set of tagged issue IDs

	Stats ChaosStats
}

// NewChaosEngine creates a ChaosEngine with deterministic seed.
func NewChaosEngine(h *Harness, seed int64, numActors int) *ChaosEngine {
	if numActors < 2 {
		numActors = 2
	}
	return &ChaosEngine{
		Harness:     h,
		Rng:         rand.New(rand.NewSource(seed)),
		NumActors:   numActors,
		Issues:      make(map[string]*IssueState),
		DepPairs:    make(map[string]bool),
		ParentChild: make(map[string]string),
		IssueFiles:  make(map[string]string),
		ActiveWS:    make(map[string]string),
		WSTagged:    make(map[string]map[string]bool),
		Stats: ChaosStats{
			PerAction: make(map[string]*ActionStats),
		},
	}
}

// randActor picks a random actor name.
func (e *ChaosEngine) randActor() string {
	names := actorNames(e.NumActors)
	return names[e.Rng.Intn(len(names))]
}

// otherActor returns a different actor from the given one.
func (e *ChaosEngine) otherActor(actor string) string {
	names := actorNames(e.NumActors)
	for range 10 {
		other := names[e.Rng.Intn(len(names))]
		if other != actor {
			return other
		}
	}
	// fallback
	if actor == "alice" {
		return "bob"
	}
	return "alice"
}

// selectIssue picks a random issue matching the filter.
func (e *ChaosEngine) selectIssue(filter string) string {
	var candidates []string
	for _, id := range e.IssueOrder {
		st, ok := e.Issues[id]
		if !ok {
			continue
		}
		switch filter {
		case "not_deleted":
			if !st.Deleted {
				candidates = append(candidates, id)
			}
		case "deleted":
			if st.Deleted {
				candidates = append(candidates, id)
			}
		case "open":
			if !st.Deleted && st.Status == "open" {
				candidates = append(candidates, id)
			}
		case "in_progress":
			if !st.Deleted && st.Status == "in_progress" {
				candidates = append(candidates, id)
			}
		case "in_review":
			if !st.Deleted && st.Status == "in_review" {
				candidates = append(candidates, id)
			}
		case "closed":
			if !st.Deleted && st.Status == "closed" {
				candidates = append(candidates, id)
			}
		case "blocked":
			if !st.Deleted && st.Status == "blocked" {
				candidates = append(candidates, id)
			}
		case "any":
			candidates = append(candidates, id)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[e.Rng.Intn(len(candidates))]
}

// extractIssueID finds the first td-<hex> in output.
func extractIssueID(output string) string {
	m := issueIDRe.FindString(output)
	return m
}

// ExtractIssueID is the exported version for test use.
func ExtractIssueID(output string) string {
	return extractIssueID(output)
}

// TrackCreatedIssue manually adds an issue to the engine's state tracking.
func (e *ChaosEngine) TrackCreatedIssue(id, status, owner string) {
	e.Issues[id] = &IssueState{ID: id, Status: status, Owner: owner}
	e.IssueOrder = append(e.IssueOrder, id)
}

// isExpectedFailure checks if output contains known rejection patterns.
func isExpectedFailure(output string) bool {
	lower := strings.ToLower(output)
	patterns := []string{
		"cannot approve your own",
		"self-review",
		"self-close",
		"cycle",
		"circular",
		"not in expected status",
		"invalid status",
		"cannot transition",
		"not found",
		"no such",
		"already",
		"blocked",
		"no issues",
		"does not exist",
		"cannot close",
		"cannot block",
		"no active",
		"no work session",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	// "session"..."not found"
	if strings.Contains(lower, "session") && strings.Contains(lower, "not found") {
		return true
	}
	return false
}

// recordResult updates stats from an action result.
func (e *ChaosEngine) recordResult(r ActionResult) {
	e.Stats.ActionCount++
	if _, ok := e.Stats.PerAction[r.Action]; !ok {
		e.Stats.PerAction[r.Action] = &ActionStats{}
	}
	pa := e.Stats.PerAction[r.Action]
	switch {
	case r.Skipped:
		// No suitable target; not a failure
		return
	case r.OK:
		pa.OK++
	case r.ExpFail:
		pa.ExpFail++
		e.Stats.ExpectedFailures++
	default:
		pa.UnexpFail++
		e.Stats.UnexpectedFailures++
	}
}

// RunAction selects a weighted random action and executes it.
// Returns the result and whether it was an unexpected failure.
func (e *ChaosEngine) RunAction() ActionResult {
	actor := e.randActor()
	def := SelectAction(e.Rng)
	r := def.Exec(e, actor)
	r.Action = def.Name
	r.Actor = actor
	e.recordResult(r)
	return r
}

// RunN executes n random actions and returns all results.
func (e *ChaosEngine) RunN(n int) []ActionResult {
	results := make([]ActionResult, 0, n)
	for range n {
		r := e.RunAction()
		results = append(results, r)
	}
	return results
}

// Summary returns a human-readable stats summary.
func (e *ChaosEngine) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Actions: %d | ExpFail: %d | UnexpFail: %d | Skipped: %d | Edge: %d\n",
		e.Stats.ActionCount, e.Stats.ExpectedFailures, e.Stats.UnexpectedFailures,
		e.Stats.Skipped, e.Stats.EdgeDataUsed)
	for name, pa := range e.Stats.PerAction {
		fmt.Fprintf(&b, "  %-20s ok=%d exp=%d unexp=%d\n", name, pa.OK, pa.ExpFail, pa.UnexpFail)
	}
	return b.String()
}
