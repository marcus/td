package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// fibPoints are valid Fibonacci story point values.
var fibPoints = []int{0, 1, 2, 3, 5, 8, 13, 21}

// skip returns a skipped result (no suitable target).
func skip(action string) ActionResult {
	return ActionResult{Action: action, Skipped: true}
}

// --- CRUD ---

func execCreate(e *ChaosEngine, actor string) ActionResult {
	title, edgeUsed := randTitle(e.Rng, 200)
	if edgeUsed {
		e.Stats.EdgeDataUsed++
	}
	typeVal := randChoice(e.Rng, "task", "bug", "feature", "chore")
	priority := randChoice(e.Rng, "P0", "P1", "P2", "P3")
	points := fmt.Sprintf("%d", fibPoints[e.Rng.Intn(len(fibPoints))])
	labels := randLabels(e.Rng, 0)
	desc, edgeUsed2 := randDescription(e.Rng, 1)
	if edgeUsed2 {
		e.Stats.EdgeDataUsed++
	}
	acceptance, edgeUsed3 := randAcceptance(e.Rng, 0)
	if edgeUsed3 {
		e.Stats.EdgeDataUsed++
	}

	args := []string{"create", title, "--type", typeVal, "--priority", priority, "--points", points, "--labels", labels}

	// 40% chance of parent
	var parent string
	if e.Rng.Intn(5) < 2 && len(e.IssueOrder) > 0 {
		parent = e.selectIssue("not_deleted")
		if parent != "" {
			args = append(args, "--parent", parent)
		}
	}

	out, err := e.Harness.Td(actor, args...)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "create", Actor: actor, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "create", Actor: actor, Output: out}
	}

	id := extractIssueID(out)
	if id == "" {
		return ActionResult{Action: "create", Actor: actor, Output: out}
	}

	e.Issues[id] = &IssueState{ID: id, Status: "open", Owner: actor}
	e.IssueOrder = append(e.IssueOrder, id)

	if parent != "" {
		e.ParentChild[id] = parent
	}

	// Set description and acceptance separately
	e.Harness.Td(actor, "update", id, "--description", desc)
	e.Harness.Td(actor, "update", id, "--acceptance", acceptance)

	// 30% chance of minor
	if e.Rng.Intn(10) < 3 {
		e.Harness.Td(actor, "update", id, "--minor")
		e.Issues[id].Minor = true
	}

	return ActionResult{Action: "create", Actor: actor, Target: id, OK: true, Output: out}
}

func execUpdate(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("update")
	}

	nFields := 1 + e.Rng.Intn(3)
	args := []string{"update", id}
	for range nFields {
		switch e.Rng.Intn(8) {
		case 0:
			t, edg := randTitle(e.Rng, 100)
			if edg {
				e.Stats.EdgeDataUsed++
			}
			args = append(args, "--title", t)
		case 1:
			d, edg := randDescription(e.Rng, 1)
			if edg {
				e.Stats.EdgeDataUsed++
			}
			args = append(args, "--description", d)
		case 2:
			args = append(args, "--type", randChoice(e.Rng, "task", "bug", "feature", "chore"))
		case 3:
			args = append(args, "--priority", randChoice(e.Rng, "P0", "P1", "P2", "P3"))
		case 4:
			args = append(args, "--points", fmt.Sprintf("%d", fibPoints[e.Rng.Intn(len(fibPoints))]))
		case 5:
			args = append(args, "--labels", randLabels(e.Rng, 0))
		case 6:
			a, edg := randAcceptance(e.Rng, 0)
			if edg {
				e.Stats.EdgeDataUsed++
			}
			args = append(args, "--acceptance", a)
		case 7:
			args = append(args, "--sprint", randChoice(e.Rng, "sprint-1", "sprint-2", "sprint-3", ""))
		}
	}

	out, err := e.Harness.Td(actor, args...)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "update", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "update", Actor: actor, Target: id, Output: out}
	}
	return ActionResult{Action: "update", Actor: actor, Target: id, OK: true, Output: out}
}

func execUpdateAppend(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("update_append")
	}

	fieldFlag := "--description"
	if e.Rng.Intn(2) == 1 {
		fieldFlag = "--acceptance"
	}

	nWords := 1 + e.Rng.Intn(3)
	appendText := randWords(e.Rng, nWords)

	out, err := e.Harness.Td(actor, "update", id, "--append", fieldFlag, appendText)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "update_append", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "update_append", Actor: actor, Target: id, Output: out}
	}
	return ActionResult{Action: "update_append", Actor: actor, Target: id, OK: true, Output: out}
}

func execUpdateBulk(e *ChaosEngine, actor string) ActionResult {
	count := 2 + e.Rng.Intn(2)
	seen := make(map[string]bool)
	var ids []string
	for range count {
		id := e.selectIssue("not_deleted")
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) < 2 {
		e.Stats.Skipped++
		return skip("update_bulk")
	}

	args := append([]string{"update"}, ids...)
	switch e.Rng.Intn(5) {
	case 0:
		args = append(args, "--priority", randChoice(e.Rng, "P0", "P1", "P2", "P3"))
	case 1:
		args = append(args, "--type", randChoice(e.Rng, "task", "bug", "feature", "chore"))
	case 2:
		args = append(args, "--points", fmt.Sprintf("%d", fibPoints[e.Rng.Intn(len(fibPoints))]))
	case 3:
		args = append(args, "--labels", randLabels(e.Rng, 0))
	case 4:
		args = append(args, "--sprint", randChoice(e.Rng, "sprint-1", "sprint-2", "sprint-3", ""))
	}

	out, _ := e.Harness.Td(actor, args...)
	return ActionResult{Action: "update_bulk", Actor: actor, Target: strings.Join(ids, ","), OK: true, Output: out}
}

func execDelete(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("delete")
	}

	out, err := e.Harness.Td(actor, "delete", id)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "delete", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "delete", Actor: actor, Target: id, Output: out}
	}
	if st, ok := e.Issues[id]; ok {
		st.Deleted = true
	}
	return ActionResult{Action: "delete", Actor: actor, Target: id, OK: true, Output: out}
}

func execRestore(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("restore")
	}

	out, err := e.Harness.Td(actor, "restore", id)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "restore", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "restore", Actor: actor, Target: id, Output: out}
	}
	if st, ok := e.Issues[id]; ok {
		st.Deleted = false
	}
	return ActionResult{Action: "restore", Actor: actor, Target: id, OK: true, Output: out}
}

// --- Status transitions ---

func execStart(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("open")
	if id == "" {
		e.Stats.Skipped++
		return skip("start")
	}

	out, err := e.Harness.Td(actor, "start", id, "--reason", "chaos start")
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "start", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "start", Actor: actor, Target: id, Output: out}
	}
	e.Issues[id].Status = "in_progress"
	return ActionResult{Action: "start", Actor: actor, Target: id, OK: true, Output: out}
}

func execUnstart(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("in_progress")
	if id == "" {
		e.Stats.Skipped++
		return skip("unstart")
	}

	out, err := e.Harness.Td(actor, "unstart", id, "--reason", "chaos unstart")
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "unstart", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "unstart", Actor: actor, Target: id, Output: out}
	}
	e.Issues[id].Status = "open"
	return ActionResult{Action: "unstart", Actor: actor, Target: id, OK: true, Output: out}
}

func execReview(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("in_progress")
	if id == "" {
		e.Stats.Skipped++
		return skip("review")
	}

	out, err := e.Harness.Td(actor, "review", id, "--reason", "chaos review")
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "review", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "review", Actor: actor, Target: id, Output: out}
	}
	e.Issues[id].Status = "in_review"
	return ActionResult{Action: "review", Actor: actor, Target: id, OK: true, Output: out}
}

func execApprove(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("in_review")
	if id == "" {
		e.Stats.Skipped++
		return skip("approve")
	}

	// Try to use a different actor to avoid self-approve on non-minor
	approver := actor
	st := e.Issues[id]
	if st.Owner == actor && !st.Minor {
		approver = e.otherActor(actor)
	}

	out, err := e.Harness.Td(approver, "approve", id, "--reason", "chaos approve")
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "approve", Actor: approver, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "approve", Actor: approver, Target: id, Output: out}
	}
	e.Issues[id].Status = "closed"
	return ActionResult{Action: "approve", Actor: approver, Target: id, OK: true, Output: out}
}

func execReject(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("in_review")
	if id == "" {
		e.Stats.Skipped++
		return skip("reject")
	}

	out, err := e.Harness.Td(actor, "reject", id, "--reason", "chaos reject")
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "reject", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "reject", Actor: actor, Target: id, Output: out}
	}
	e.Issues[id].Status = "in_progress"
	return ActionResult{Action: "reject", Actor: actor, Target: id, OK: true, Output: out}
}

func execClose(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("close")
	}
	if e.Issues[id].Status == "closed" {
		e.Stats.Skipped++
		return skip("close")
	}

	out, err := e.Harness.Td(actor, "close", id, "--reason", "chaos close")
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "close", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "close", Actor: actor, Target: id, Output: out}
	}
	e.Issues[id].Status = "closed"
	return ActionResult{Action: "close", Actor: actor, Target: id, OK: true, Output: out}
}

func execReopen(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("closed")
	if id == "" {
		e.Stats.Skipped++
		return skip("reopen")
	}

	out, err := e.Harness.Td(actor, "reopen", id, "--reason", "chaos reopen")
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "reopen", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "reopen", Actor: actor, Target: id, Output: out}
	}
	e.Issues[id].Status = "open"
	return ActionResult{Action: "reopen", Actor: actor, Target: id, OK: true, Output: out}
}

// --- Bulk status ---

func execBulkStart(e *ChaosEngine, actor string) ActionResult {
	count := 2 + e.Rng.Intn(3)
	seen := make(map[string]bool)
	var ids []string
	for range count {
		id := e.selectIssue("open")
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) < 2 {
		e.Stats.Skipped++
		return skip("bulk_start")
	}

	args := append([]string{"start"}, ids...)
	args = append(args, "--reason", "chaos bulk start")
	out, err := e.Harness.Td(actor, args...)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "bulk_start", Actor: actor, Target: strings.Join(ids, ","), ExpFail: true, Output: out}
		}
		return ActionResult{Action: "bulk_start", Actor: actor, Target: strings.Join(ids, ","), Output: out}
	}
	for _, id := range ids {
		e.Issues[id].Status = "in_progress"
	}
	return ActionResult{Action: "bulk_start", Actor: actor, Target: strings.Join(ids, ","), OK: true, Output: out}
}

func execBulkReview(e *ChaosEngine, actor string) ActionResult {
	count := 2 + e.Rng.Intn(3)
	seen := make(map[string]bool)
	var ids []string
	for range count {
		id := e.selectIssue("in_progress")
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) < 2 {
		e.Stats.Skipped++
		return skip("bulk_review")
	}

	args := append([]string{"review"}, ids...)
	args = append(args, "--reason", "chaos bulk review")
	out, err := e.Harness.Td(actor, args...)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "bulk_review", Actor: actor, Target: strings.Join(ids, ","), ExpFail: true, Output: out}
		}
		return ActionResult{Action: "bulk_review", Actor: actor, Target: strings.Join(ids, ","), Output: out}
	}
	for _, id := range ids {
		e.Issues[id].Status = "in_review"
	}
	return ActionResult{Action: "bulk_review", Actor: actor, Target: strings.Join(ids, ","), OK: true, Output: out}
}

func execBulkClose(e *ChaosEngine, actor string) ActionResult {
	count := 2 + e.Rng.Intn(3)
	seen := make(map[string]bool)
	var ids []string
	for range count {
		id := e.selectIssue("not_deleted")
		if id != "" && !seen[id] && e.Issues[id].Status != "closed" {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) < 2 {
		e.Stats.Skipped++
		return skip("bulk_close")
	}

	args := append([]string{"close"}, ids...)
	args = append(args, "--reason", "chaos bulk close")
	out, err := e.Harness.Td(actor, args...)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "bulk_close", Actor: actor, Target: strings.Join(ids, ","), ExpFail: true, Output: out}
		}
		return ActionResult{Action: "bulk_close", Actor: actor, Target: strings.Join(ids, ","), Output: out}
	}
	for _, id := range ids {
		e.Issues[id].Status = "closed"
	}
	return ActionResult{Action: "bulk_close", Actor: actor, Target: strings.Join(ids, ","), OK: true, Output: out}
}

// --- Block/Unblock ---

func execBlock(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("open")
	if id == "" {
		id = e.selectIssue("in_progress")
	}
	if id == "" {
		e.Stats.Skipped++
		return skip("block")
	}

	out, err := e.Harness.Td(actor, "block", id, "--reason", "chaos block")
	if err == nil && !isExpectedFailure(out) && !strings.Contains(strings.ToLower(out), "error") {
		e.Issues[id].Status = "blocked"
	}
	return ActionResult{Action: "block", Actor: actor, Target: id, OK: true, Output: out}
}

func execUnblock(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("blocked")
	if id == "" {
		e.Stats.Skipped++
		return skip("unblock")
	}

	out, err := e.Harness.Td(actor, "unblock", id, "--reason", "chaos unblock")
	if err == nil && !isExpectedFailure(out) && !strings.Contains(strings.ToLower(out), "error") {
		e.Issues[id].Status = "open"
	}
	return ActionResult{Action: "unblock", Actor: actor, Target: id, OK: true, Output: out}
}

// --- Comments & logs ---

func execComment(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("comment")
	}

	text, edg := randComment(e.Rng, 10, 100)
	if edg {
		e.Stats.EdgeDataUsed++
	}
	out, _ := e.Harness.Td(actor, "comments", "add", id, text)
	return ActionResult{Action: "comment", Actor: actor, Target: id, OK: true, Output: out}
}

func execLogProgress(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("log_progress")
	}

	msg, edg := randComment(e.Rng, 5, 30)
	if edg {
		e.Stats.EdgeDataUsed++
	}
	out, _ := e.Harness.Td(actor, "log", "--issue", id, msg)
	return ActionResult{Action: "log_progress", Actor: actor, Target: id, OK: true, Output: out}
}

func execLogBlocker(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("log_blocker")
	}

	msg, edg := randComment(e.Rng, 5, 20)
	if edg {
		e.Stats.EdgeDataUsed++
	}
	out, _ := e.Harness.Td(actor, "log", "--issue", id, "--blocker", msg)
	return ActionResult{Action: "log_blocker", Actor: actor, Target: id, OK: true, Output: out}
}

func execLogDecision(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("log_decision")
	}

	msg, edg := randComment(e.Rng, 5, 20)
	if edg {
		e.Stats.EdgeDataUsed++
	}
	out, _ := e.Harness.Td(actor, "log", "--issue", id, "--decision", msg)
	return ActionResult{Action: "log_decision", Actor: actor, Target: id, OK: true, Output: out}
}

func execLogHypothesis(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("log_hypothesis")
	}

	msg, edg := randComment(e.Rng, 5, 20)
	if edg {
		e.Stats.EdgeDataUsed++
	}
	out, _ := e.Harness.Td(actor, "log", "--issue", id, "--hypothesis", msg)
	return ActionResult{Action: "log_hypothesis", Actor: actor, Target: id, OK: true, Output: out}
}

func execLogResult(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("log_result")
	}

	msg, edg := randComment(e.Rng, 5, 20)
	if edg {
		e.Stats.EdgeDataUsed++
	}
	out, _ := e.Harness.Td(actor, "log", "--issue", id, "--result", msg)
	return ActionResult{Action: "log_result", Actor: actor, Target: id, OK: true, Output: out}
}

// --- Dependencies ---

func execDepAdd(e *ChaosEngine, actor string) ActionResult {
	id1 := e.selectIssue("not_deleted")
	if id1 == "" {
		e.Stats.Skipped++
		return skip("dep_add")
	}
	id2 := e.selectIssue("not_deleted")
	if id2 == "" || id2 == id1 {
		e.Stats.Skipped++
		return skip("dep_add")
	}

	depKey := id1 + "_" + id2
	if e.DepPairs[depKey] {
		e.Stats.Skipped++
		return skip("dep_add")
	}

	out, err := e.Harness.Td(actor, "dep", "add", id1, id2)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "dep_add", Actor: actor, Target: depKey, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "dep_add", Actor: actor, Target: depKey, Output: out}
	}
	e.DepPairs[depKey] = true
	return ActionResult{Action: "dep_add", Actor: actor, Target: depKey, OK: true, Output: out}
}

func execDepRm(e *ChaosEngine, actor string) ActionResult {
	if len(e.DepPairs) == 0 {
		e.Stats.Skipped++
		return skip("dep_rm")
	}

	// Collect keys
	var keys []string
	for k := range e.DepPairs {
		keys = append(keys, k)
	}
	pair := keys[e.Rng.Intn(len(keys))]

	// Split: "td-xxx_td-yyy" -- find the underscore between two td- IDs
	// The pair is "id1_id2" where id1=td-xxx and id2=td-yyy
	// We need to split on "_td-" to handle this correctly
	parts := strings.SplitN(pair, "_td-", 2)
	if len(parts) != 2 {
		e.Stats.Skipped++
		return skip("dep_rm")
	}
	id1 := parts[0]
	id2 := "td-" + parts[1]

	out, err := e.Harness.Td(actor, "dep", "rm", id1, id2)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "dep_rm", Actor: actor, Target: pair, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "dep_rm", Actor: actor, Target: pair, Output: out}
	}
	delete(e.DepPairs, pair)
	return ActionResult{Action: "dep_rm", Actor: actor, Target: pair, OK: true, Output: out}
}

// --- Boards ---

func execBoardCreate(e *ChaosEngine, actor string) ActionResult {
	name := fmt.Sprintf("chaos-board-%08x", e.Rng.Uint32())

	args := []string{"board", "create", name}
	// 50% chance of query
	if e.Rng.Intn(2) == 1 {
		q := randChoice(e.Rng, "status = open", "priority = P0", "type = bug", "status != closed", "labels ~ urgent")
		args = append(args, "-q", q)
	}

	out, err := e.Harness.Td(actor, args...)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "board_create", Actor: actor, Target: name, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "board_create", Actor: actor, Target: name, Output: out}
	}
	e.Boards = append(e.Boards, name)
	return ActionResult{Action: "board_create", Actor: actor, Target: name, OK: true, Output: out}
}

func execBoardEdit(e *ChaosEngine, actor string) ActionResult {
	if len(e.Boards) == 0 {
		e.Stats.Skipped++
		return skip("board_edit")
	}

	name := e.Boards[e.Rng.Intn(len(e.Boards))]
	q := randChoice(e.Rng, "status = open", "priority = P0", "type = bug", "status != closed", "labels ~ urgent", "points > 3")

	out, _ := e.Harness.Td(actor, "board", "edit", name, "-q", q)
	return ActionResult{Action: "board_edit", Actor: actor, Target: name, OK: true, Output: out}
}

func execBoardMove(e *ChaosEngine, actor string) ActionResult {
	if len(e.Boards) == 0 {
		e.Stats.Skipped++
		return skip("board_move")
	}

	name := e.Boards[e.Rng.Intn(len(e.Boards))]
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("board_move")
	}

	pos := fmt.Sprintf("%d", 1+e.Rng.Intn(100))
	out, err := e.Harness.Td(actor, "board", "move", name, id, pos)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "board_move", Actor: actor, Target: name + ":" + id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "board_move", Actor: actor, Target: name + ":" + id, Output: out}
	}
	return ActionResult{Action: "board_move", Actor: actor, Target: name + ":" + id, OK: true, Output: out}
}

func execBoardUnposition(e *ChaosEngine, actor string) ActionResult {
	if len(e.Boards) == 0 {
		e.Stats.Skipped++
		return skip("board_unposition")
	}

	name := e.Boards[e.Rng.Intn(len(e.Boards))]
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("board_unposition")
	}

	out, _ := e.Harness.Td(actor, "board", "unposition", name, id)
	return ActionResult{Action: "board_unposition", Actor: actor, Target: name + ":" + id, OK: true, Output: out}
}

func execBoardDelete(e *ChaosEngine, actor string) ActionResult {
	if len(e.Boards) == 0 {
		e.Stats.Skipped++
		return skip("board_delete")
	}

	idx := e.Rng.Intn(len(e.Boards))
	name := e.Boards[idx]

	out, err := e.Harness.Td(actor, "board", "delete", name)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "board_delete", Actor: actor, Target: name, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "board_delete", Actor: actor, Target: name, Output: out}
	}
	// Remove from slice
	e.Boards = append(e.Boards[:idx], e.Boards[idx+1:]...)
	return ActionResult{Action: "board_delete", Actor: actor, Target: name, OK: true, Output: out}
}

func execBoardViewMode(e *ChaosEngine, actor string) ActionResult {
	if len(e.Boards) == 0 {
		e.Stats.Skipped++
		return skip("board_view_mode")
	}

	name := e.Boards[e.Rng.Intn(len(e.Boards))]
	mode := randChoice(e.Rng, "swimlanes", "backlog")

	out, err := e.Harness.Td(actor, "board", "edit", name, "--view-mode", mode)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "board_view_mode", Actor: actor, Target: name, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "board_view_mode", Actor: actor, Target: name, Output: out}
	}
	return ActionResult{Action: "board_view_mode", Actor: actor, Target: name, OK: true, Output: out}
}

// --- Handoffs ---

func execHandoff(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("in_progress")
	if id == "" {
		e.Stats.Skipped++
		return skip("handoff")
	}

	doneItems, _ := randHandoffItems(e.Rng, 0)
	remainItems, _ := randHandoffItems(e.Rng, 0)
	decisionItems, _ := randHandoffItems(e.Rng, 2)
	uncertainItems, _ := randHandoffItems(e.Rng, 2)

	out, err := e.Harness.Td(actor, "handoff", id,
		"--done", doneItems,
		"--remaining", remainItems,
		"--decision", decisionItems,
		"--uncertain", uncertainItems)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "handoff", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "handoff", Actor: actor, Target: id, Output: out}
	}
	return ActionResult{Action: "handoff", Actor: actor, Target: id, OK: true, Output: out}
}

// --- Parent-child cascade ---

func execCreateChild(e *ChaosEngine, actor string) ActionResult {
	if len(e.IssueOrder) == 0 {
		e.Stats.Skipped++
		return skip("create_child")
	}

	parent := e.selectIssue("not_deleted")
	if parent == "" {
		e.Stats.Skipped++
		return skip("create_child")
	}

	title, edg := randTitle(e.Rng, 200)
	if edg {
		e.Stats.EdgeDataUsed++
	}
	typeVal := randChoice(e.Rng, "task", "bug", "feature", "chore")
	priority := randChoice(e.Rng, "P0", "P1", "P2", "P3")
	points := fmt.Sprintf("%d", fibPoints[e.Rng.Intn(len(fibPoints))])

	out, err := e.Harness.Td(actor, "create", title, "--type", typeVal, "--priority", priority, "--points", points, "--parent", parent)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "create_child", Actor: actor, Target: parent, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "create_child", Actor: actor, Target: parent, Output: out}
	}

	id := extractIssueID(out)
	if id == "" {
		return ActionResult{Action: "create_child", Actor: actor, Target: parent, Output: out}
	}

	e.Issues[id] = &IssueState{ID: id, Status: "open", Owner: actor}
	e.IssueOrder = append(e.IssueOrder, id)
	e.ParentChild[id] = parent
	return ActionResult{Action: "create_child", Actor: actor, Target: id, OK: true, Output: out}
}

func execCascadeHandoff(e *ChaosEngine, actor string) ActionResult {
	// Find a parent with in_progress status that has children
	var parentID string
	for childID, pID := range e.ParentChild {
		pSt, pOK := e.Issues[pID]
		cSt, cOK := e.Issues[childID]
		if pOK && cOK && !pSt.Deleted && !cSt.Deleted && pSt.Status == "in_progress" {
			parentID = pID
			break
		}
	}
	if parentID == "" {
		e.Stats.Skipped++
		return skip("cascade_handoff")
	}

	doneItems, _ := randHandoffItems(e.Rng, 0)
	remainItems, _ := randHandoffItems(e.Rng, 0)

	out, err := e.Harness.Td(actor, "handoff", parentID, "--done", doneItems, "--remaining", remainItems)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "cascade_handoff", Actor: actor, Target: parentID, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "cascade_handoff", Actor: actor, Target: parentID, Output: out}
	}
	return ActionResult{Action: "cascade_handoff", Actor: actor, Target: parentID, OK: true, Output: out}
}

func execCascadeReview(e *ChaosEngine, actor string) ActionResult {
	// Find parent with in_progress children
	var parentID string
	for childID, pID := range e.ParentChild {
		pSt, pOK := e.Issues[pID]
		cSt, cOK := e.Issues[childID]
		if pOK && cOK && !pSt.Deleted && !cSt.Deleted && pSt.Status == "in_progress" && cSt.Status == "in_progress" {
			parentID = pID
			break
		}
	}
	if parentID == "" {
		e.Stats.Skipped++
		return skip("cascade_review")
	}

	out, err := e.Harness.Td(actor, "review", parentID, "--reason", "cascade review test")
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "cascade_review", Actor: actor, Target: parentID, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "cascade_review", Actor: actor, Target: parentID, Output: out}
	}
	e.Issues[parentID].Status = "in_review"
	// Update children that were in_progress to in_review (best-effort tracker update)
	for childID, pID := range e.ParentChild {
		if pID == parentID {
			cSt := e.Issues[childID]
			if cSt != nil && !cSt.Deleted && (cSt.Status == "in_progress" || cSt.Status == "open") {
				cSt.Status = "in_review"
			}
		}
	}
	return ActionResult{Action: "cascade_review", Actor: actor, Target: parentID, OK: true, Output: out}
}

// --- File links ---

func execLink(e *ChaosEngine, actor string) ActionResult {
	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("link")
	}

	dirs := []string{"src", "tests", "docs", "internal", "cmd", "pkg"}
	exts := []string{"go", "md", "sh", "yaml", "json"}
	dir := dirs[e.Rng.Intn(len(dirs))]
	ext := exts[e.Rng.Intn(len(exts))]
	num := 1 + e.Rng.Intn(999)
	filePath := fmt.Sprintf("%s_file_%d.%s", dir, num, ext)

	role := randChoice(e.Rng, "implementation", "test", "reference", "config")

	// Create the file so td link can find it
	clientDir := e.Harness.ClientDir(actor)
	absPath := filepath.Join(clientDir, filePath)
	os.MkdirAll(filepath.Dir(absPath), 0755)
	os.WriteFile(absPath, []byte("chaos-generated\n"), 0644)

	out, err := e.Harness.Td(actor, "link", id, filePath, "--role", role)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "link", Actor: actor, Target: id + "~" + filePath, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "link", Actor: actor, Target: id + "~" + filePath, Output: out}
	}
	e.IssueFiles[id+"~"+filePath] = role
	return ActionResult{Action: "link", Actor: actor, Target: id + "~" + filePath, OK: true, Output: out}
}

func execUnlink(e *ChaosEngine, actor string) ActionResult {
	if len(e.IssueFiles) == 0 {
		e.Stats.Skipped++
		return skip("unlink")
	}

	var keys []string
	for k := range e.IssueFiles {
		keys = append(keys, k)
	}
	key := keys[e.Rng.Intn(len(keys))]

	parts := strings.SplitN(key, "~", 2)
	if len(parts) != 2 {
		e.Stats.Skipped++
		return skip("unlink")
	}
	issueID, filePath := parts[0], parts[1]

	out, err := e.Harness.Td(actor, "unlink", issueID, filePath)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "unlink", Actor: actor, Target: key, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "unlink", Actor: actor, Target: key, Output: out}
	}
	delete(e.IssueFiles, key)
	return ActionResult{Action: "unlink", Actor: actor, Target: key, OK: true, Output: out}
}

// --- Work sessions ---

func execWSStart(e *ChaosEngine, actor string) ActionResult {
	if e.ActiveWS[actor] != "" {
		e.Stats.Skipped++
		return skip("ws_start")
	}

	name := fmt.Sprintf("chaos-ws-%d", 1+e.Rng.Intn(999))
	out, err := e.Harness.Td(actor, "ws", "start", name)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "ws_start", Actor: actor, Target: name, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "ws_start", Actor: actor, Target: name, Output: out}
	}
	e.ActiveWS[actor] = name
	return ActionResult{Action: "ws_start", Actor: actor, Target: name, OK: true, Output: out}
}

func execWSTag(e *ChaosEngine, actor string) ActionResult {
	if e.ActiveWS[actor] == "" {
		e.Stats.Skipped++
		return skip("ws_tag")
	}

	id := e.selectIssue("not_deleted")
	if id == "" {
		e.Stats.Skipped++
		return skip("ws_tag")
	}

	out, err := e.Harness.Td(actor, "ws", "tag", id, "--no-start")
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "ws_tag", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "ws_tag", Actor: actor, Target: id, Output: out}
	}
	if e.WSTagged[actor] == nil {
		e.WSTagged[actor] = make(map[string]bool)
	}
	e.WSTagged[actor][id] = true
	return ActionResult{Action: "ws_tag", Actor: actor, Target: id, OK: true, Output: out}
}

func execWSUntag(e *ChaosEngine, actor string) ActionResult {
	if e.ActiveWS[actor] == "" || len(e.WSTagged[actor]) == 0 {
		e.Stats.Skipped++
		return skip("ws_untag")
	}

	var tagged []string
	for id := range e.WSTagged[actor] {
		tagged = append(tagged, id)
	}
	id := tagged[e.Rng.Intn(len(tagged))]

	out, err := e.Harness.Td(actor, "ws", "untag", id)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "ws_untag", Actor: actor, Target: id, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "ws_untag", Actor: actor, Target: id, Output: out}
	}
	delete(e.WSTagged[actor], id)
	return ActionResult{Action: "ws_untag", Actor: actor, Target: id, OK: true, Output: out}
}

func execWSEnd(e *ChaosEngine, actor string) ActionResult {
	if e.ActiveWS[actor] == "" {
		e.Stats.Skipped++
		return skip("ws_end")
	}

	out, err := e.Harness.Td(actor, "ws", "end")
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "ws_end", Actor: actor, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "ws_end", Actor: actor, Output: out}
	}
	e.ActiveWS[actor] = ""
	e.WSTagged[actor] = nil
	return ActionResult{Action: "ws_end", Actor: actor, OK: true, Output: out}
}

func execWSHandoff(e *ChaosEngine, actor string) ActionResult {
	if e.ActiveWS[actor] == "" {
		e.Stats.Skipped++
		return skip("ws_handoff")
	}

	doneItems, _ := randHandoffItems(e.Rng, 0)
	remainItems, _ := randHandoffItems(e.Rng, 0)
	decisionItems, _ := randHandoffItems(e.Rng, 2)
	uncertainItems, _ := randHandoffItems(e.Rng, 2)

	out, err := e.Harness.Td(actor, "ws", "handoff",
		"--done", doneItems,
		"--remaining", remainItems,
		"--decision", decisionItems,
		"--uncertain", uncertainItems)
	if err != nil {
		if isExpectedFailure(out) {
			return ActionResult{Action: "ws_handoff", Actor: actor, ExpFail: true, Output: out}
		}
		return ActionResult{Action: "ws_handoff", Actor: actor, Output: out}
	}
	e.ActiveWS[actor] = ""
	e.WSTagged[actor] = nil
	return ActionResult{Action: "ws_handoff", Actor: actor, OK: true, Output: out}
}
