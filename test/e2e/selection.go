package e2e

import "math/rand"

// ActionDef defines a named action with weight and executor.
type ActionDef struct {
	Name   string
	Weight int
	Exec   func(engine *ChaosEngine, actor string) ActionResult
}

// actionDefs lists all chaos actions with their weights.
// Weights match the bash chaos_lib.sh.
var actionDefs = []ActionDef{
	// CRUD
	{Name: "create", Weight: 15, Exec: execCreate},
	{Name: "update", Weight: 10, Exec: execUpdate},
	{Name: "update_append", Weight: 2, Exec: execUpdateAppend},
	{Name: "delete", Weight: 2, Exec: execDelete},
	{Name: "restore", Weight: 1, Exec: execRestore},
	{Name: "update_bulk", Weight: 2, Exec: execUpdateBulk},
	// Status
	{Name: "start", Weight: 7, Exec: execStart},
	{Name: "unstart", Weight: 1, Exec: execUnstart},
	{Name: "review", Weight: 5, Exec: execReview},
	{Name: "approve", Weight: 5, Exec: execApprove},
	{Name: "reject", Weight: 2, Exec: execReject},
	{Name: "close", Weight: 2, Exec: execClose},
	{Name: "reopen", Weight: 2, Exec: execReopen},
	{Name: "bulk_start", Weight: 1, Exec: execBulkStart},
	{Name: "bulk_review", Weight: 1, Exec: execBulkReview},
	{Name: "bulk_close", Weight: 1, Exec: execBulkClose},
	{Name: "block", Weight: 1, Exec: execBlock},
	{Name: "unblock", Weight: 1, Exec: execUnblock},
	// Content
	{Name: "comment", Weight: 10, Exec: execComment},
	{Name: "log_progress", Weight: 4, Exec: execLogProgress},
	{Name: "log_blocker", Weight: 2, Exec: execLogBlocker},
	{Name: "log_decision", Weight: 2, Exec: execLogDecision},
	{Name: "log_hypothesis", Weight: 1, Exec: execLogHypothesis},
	{Name: "log_result", Weight: 1, Exec: execLogResult},
	// Dependencies
	{Name: "dep_add", Weight: 3, Exec: execDepAdd},
	{Name: "dep_rm", Weight: 2, Exec: execDepRm},
	// Boards
	{Name: "board_create", Weight: 2, Exec: execBoardCreate},
	{Name: "board_edit", Weight: 1, Exec: execBoardEdit},
	{Name: "board_move", Weight: 2, Exec: execBoardMove},
	{Name: "board_unposition", Weight: 1, Exec: execBoardUnposition},
	{Name: "board_delete", Weight: 1, Exec: execBoardDelete},
	{Name: "board_view_mode", Weight: 1, Exec: execBoardViewMode},
	// Handoffs
	{Name: "handoff", Weight: 3, Exec: execHandoff},
	// File links
	{Name: "link", Weight: 3, Exec: execLink},
	{Name: "unlink", Weight: 1, Exec: execUnlink},
	// Work sessions
	{Name: "ws_start", Weight: 2, Exec: execWSStart},
	{Name: "ws_tag", Weight: 3, Exec: execWSTag},
	{Name: "ws_untag", Weight: 1, Exec: execWSUntag},
	{Name: "ws_end", Weight: 1, Exec: execWSEnd},
	{Name: "ws_handoff", Weight: 1, Exec: execWSHandoff},
	// Parent-child
	{Name: "create_child", Weight: 4, Exec: execCreateChild},
	{Name: "cascade_handoff", Weight: 2, Exec: execCascadeHandoff},
	{Name: "cascade_review", Weight: 2, Exec: execCascadeReview},
}

// totalWeight is precomputed sum of all action weights.
var totalWeight int

func init() {
	for _, d := range actionDefs {
		totalWeight += d.Weight
	}
}

// SelectAction picks a weighted random action definition.
func SelectAction(rng *rand.Rand) ActionDef {
	roll := rng.Intn(totalWeight) + 1
	cumulative := 0
	for _, d := range actionDefs {
		cumulative += d.Weight
		if roll <= cumulative {
			return d
		}
	}
	return actionDefs[0] // fallback: create
}
