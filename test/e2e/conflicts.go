package e2e

import (
	"database/sql"
	"fmt"
	"math/rand"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// queryDB runs a SQL query against an actor's database and returns rows as strings.
func queryDB(h *Harness, actor, query string) (string, error) {
	db, err := sql.Open("sqlite3", h.DBPath(actor)+"?mode=ro")
	if err != nil {
		return "", fmt.Errorf("open %s db: %w", actor, err)
	}
	defer db.Close()

	rows, err := db.Query(query)
	if err != nil {
		return "", fmt.Errorf("query %s: %w", actor, err)
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var lines []string
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		var parts []string
		for _, v := range vals {
			switch vv := v.(type) {
			case nil:
				parts = append(parts, "NULL")
			case []byte:
				parts = append(parts, string(vv))
			default:
				parts = append(parts, fmt.Sprintf("%v", vv))
			}
		}
		lines = append(lines, strings.Join(parts, "|"))
	}
	return strings.Join(lines, "\n"), nil
}

// verifyConvergence checks that two actors have matching non-deleted issues.
func verifyConvergence(h *Harness, actorA, actorB string) []VerifyResult {
	var results []VerifyResult

	issueCols := "id, title, description, status, type, priority, points, labels, parent_id, acceptance, minor, sprint"

	issuesA, errA := queryDB(h, actorA, fmt.Sprintf("SELECT %s FROM issues WHERE deleted_at IS NULL ORDER BY id", issueCols))
	issuesB, errB := queryDB(h, actorB, fmt.Sprintf("SELECT %s FROM issues WHERE deleted_at IS NULL ORDER BY id", issueCols))
	if errA != nil || errB != nil {
		return append(results, fail("issues query", fmt.Sprintf("errA=%v errB=%v", errA, errB)))
	}
	if issuesA == issuesB {
		results = append(results, pass("issues match"))
	} else {
		results = append(results, fail("issues match",
			fmt.Sprintf("%s has:\n%s\n\n%s has:\n%s", actorA, issuesA, actorB, issuesB)))
	}

	// Comments
	commentsA, _ := queryDB(h, actorA, "SELECT issue_id, text, session_id FROM comments ORDER BY issue_id, id")
	commentsB, _ := queryDB(h, actorB, "SELECT issue_id, text, session_id FROM comments ORDER BY issue_id, id")
	if commentsA == commentsB {
		results = append(results, pass("comments match"))
	} else {
		results = append(results, fail("comments match",
			fmt.Sprintf("%s has %d lines, %s has %d lines",
				actorA, len(strings.Split(commentsA, "\n")),
				actorB, len(strings.Split(commentsB, "\n")))))
	}

	// Logs
	logsA, _ := queryDB(h, actorA, "SELECT issue_id, type, message, session_id FROM logs ORDER BY issue_id, id")
	logsB, _ := queryDB(h, actorB, "SELECT issue_id, type, message, session_id FROM logs ORDER BY issue_id, id")
	if logsA == logsB {
		results = append(results, pass("logs match"))
	} else {
		results = append(results, fail("logs match",
			fmt.Sprintf("%s has %d lines, %s has %d lines",
				actorA, len(strings.Split(logsA, "\n")),
				actorB, len(strings.Split(logsB, "\n")))))
	}

	// Dependencies
	depsA, _ := queryDB(h, actorA, "SELECT issue_id, depends_on_id, relation_type FROM issue_dependencies ORDER BY issue_id, depends_on_id")
	depsB, _ := queryDB(h, actorB, "SELECT issue_id, depends_on_id, relation_type FROM issue_dependencies ORDER BY issue_id, depends_on_id")
	if depsA == depsB {
		results = append(results, pass("dependencies match"))
	} else {
		results = append(results, fail("dependencies match",
			fmt.Sprintf("%s: %s\n%s: %s", actorA, depsA, actorB, depsB)))
	}

	// Boards
	boardsA, _ := queryDB(h, actorA, "SELECT name, is_builtin, query, view_mode FROM boards ORDER BY name")
	boardsB, _ := queryDB(h, actorB, "SELECT name, is_builtin, query, view_mode FROM boards ORDER BY name")
	if boardsA == boardsB {
		results = append(results, pass("boards match"))
	} else {
		results = append(results, fail("boards match",
			fmt.Sprintf("%s: %s\n%s: %s", actorA, boardsA, actorB, boardsB)))
	}

	return results
}

// verifyIssueField checks a specific field on a specific issue for both actors.
func verifyIssueField(h *Harness, issueID, field, expected string) []VerifyResult {
	var results []VerifyResult
	for _, actor := range []string{"alice", "bob"} {
		val, err := queryDB(h, actor, fmt.Sprintf("SELECT %s FROM issues WHERE id='%s' AND deleted_at IS NULL", field, issueID))
		if err != nil {
			results = append(results, fail(fmt.Sprintf("%s.%s@%s", issueID, field, actor), err.Error()))
			continue
		}
		if strings.TrimSpace(val) == expected {
			results = append(results, pass(fmt.Sprintf("%s.%s@%s", issueID, field, actor)))
		} else {
			results = append(results, fail(fmt.Sprintf("%s.%s@%s", issueID, field, actor),
				fmt.Sprintf("expected %q, got %q", expected, strings.TrimSpace(val))))
		}
	}
	return results
}

// issueCount returns the number of non-deleted issues for an actor.
func issueCount(h *Harness, actor string) (int, error) {
	val, err := queryDB(h, actor, "SELECT COUNT(*) FROM issues WHERE deleted_at IS NULL")
	if err != nil {
		return 0, err
	}
	var count int
	fmt.Sscanf(val, "%d", &count)
	return count, nil
}

// ScenarioPartitionRecovery: both actors perform 50+ mutations without syncing,
// then sync and verify convergence.
func ScenarioPartitionRecovery(h *Harness, rng *rand.Rand) []VerifyResult {
	engineA := NewChaosEngine(h, rng.Int63(), 2)
	engineB := NewChaosEngine(h, rng.Int63(), 2)

	// Seed some initial issues on alice, sync so both have them
	for i := 0; i < 5; i++ {
		out, err := h.TdA("create", fmt.Sprintf("partition-seed-%d", i),
			"--type", "task", "--priority", "P1")
		if err == nil {
			id := extractIssueID(out)
			if id != "" {
				engineA.TrackCreatedIssue(id, "open", "alice")
				engineB.TrackCreatedIssue(id, "open", "alice")
			}
		}
	}
	if err := h.SyncAll(); err != nil {
		return []VerifyResult{fail("partition seed sync", err.Error())}
	}

	// Actor A: 50+ mutations without syncing
	for i := 0; i < 55; i++ {
		def := SelectAction(engineA.Rng)
		r := def.Exec(engineA, "alice")
		engineA.recordResult(ActionResult{Action: def.Name, Actor: "alice", Target: r.Target, OK: r.OK, ExpFail: r.ExpFail, Skipped: r.Skipped})
	}

	// Actor B: 50+ mutations without syncing
	for i := 0; i < 55; i++ {
		def := SelectAction(engineB.Rng)
		r := def.Exec(engineB, "bob")
		engineB.recordResult(ActionResult{Action: def.Name, Actor: "bob", Target: r.Target, OK: r.OK, ExpFail: r.ExpFail, Skipped: r.Skipped})
	}

	// Now sync for convergence
	if err := h.SyncAll(); err != nil {
		return []VerifyResult{fail("partition recovery sync", err.Error())}
	}

	return verifyConvergence(h, "alice", "bob")
}

// ScenarioUndoSync: actor A creates issue, syncs, actor B sees it,
// actor A undoes, syncs, verify convergence.
func ScenarioUndoSync(h *Harness) []VerifyResult {
	var results []VerifyResult

	// Alice creates an issue
	out, err := h.TdA("create", "undo-sync-test-issue", "--type", "task", "--priority", "P2")
	if err != nil {
		return []VerifyResult{fail("create", fmt.Sprintf("%v: %s", err, out))}
	}
	issueID := extractIssueID(out)
	if issueID == "" {
		return []VerifyResult{fail("create", "no issue ID in output")}
	}

	// Sync so bob sees it
	if err := h.SyncAll(); err != nil {
		return []VerifyResult{fail("initial sync", err.Error())}
	}

	// Verify bob has it
	bobOut, err := h.TdB("show", issueID)
	if err != nil {
		results = append(results, fail("bob sees issue pre-undo", fmt.Sprintf("%v: %s", err, bobOut)))
	} else {
		results = append(results, pass("bob sees issue pre-undo"))
	}

	// Alice undoes the create
	undoOut, err := h.TdA("undo")
	if err != nil {
		// Undo might not be supported for synced issues -- document behavior
		results = append(results, fail("alice undo", fmt.Sprintf("%v: %s", err, undoOut)))
		return results
	}
	results = append(results, pass("alice undo succeeded"))

	// Sync again
	if err := h.SyncAll(); err != nil {
		results = append(results, fail("post-undo sync", err.Error()))
		return results
	}

	// Check convergence -- the issue should be in same state on both sides
	convResults := verifyConvergence(h, "alice", "bob")
	results = append(results, convResults...)

	return results
}

// ScenarioMultiFieldCollision: both actors update different fields on the same
// issue concurrently. After sync, both changes should survive (field-level merge).
func ScenarioMultiFieldCollision(h *Harness) []VerifyResult {
	var results []VerifyResult

	// Define field collision pairs to test
	type fieldPair struct {
		nameA, flagA, valA string
		nameB, flagB, valB string
	}
	pairs := []fieldPair{
		{"title", "--title", "collision-title-from-alice", "priority", "--priority", "P0"},
		{"description", "--description", "collision-desc-from-alice", "labels", "--labels", "collision-label"},
		{"points", "--points", "13", "sprint", "--sprint", "sprint-collision"},
	}

	for _, pair := range pairs {
		// Create a shared issue and sync
		out, err := h.TdA("create", fmt.Sprintf("multi-field-test-%s-%s", pair.nameA, pair.nameB),
			"--type", "task", "--priority", "P1")
		if err != nil {
			results = append(results, fail(fmt.Sprintf("create for %s/%s", pair.nameA, pair.nameB), fmt.Sprintf("%v: %s", err, out)))
			continue
		}
		issueID := extractIssueID(out)
		if issueID == "" {
			results = append(results, fail(fmt.Sprintf("create for %s/%s", pair.nameA, pair.nameB), "no issue ID"))
			continue
		}

		if err := h.SyncAll(); err != nil {
			results = append(results, fail(fmt.Sprintf("sync for %s/%s", pair.nameA, pair.nameB), err.Error()))
			continue
		}

		// Alice updates field A (no sync)
		if _, err := h.TdA("update", issueID, pair.flagA, pair.valA); err != nil {
			results = append(results, fail(fmt.Sprintf("alice update %s", pair.nameA), err.Error()))
			continue
		}

		// Bob updates field B (no sync)
		if _, err := h.TdB("update", issueID, pair.flagB, pair.valB); err != nil {
			results = append(results, fail(fmt.Sprintf("bob update %s", pair.nameB), err.Error()))
			continue
		}

		// Sync for convergence
		if err := h.SyncAll(); err != nil {
			results = append(results, fail(fmt.Sprintf("convergence sync %s/%s", pair.nameA, pair.nameB), err.Error()))
			continue
		}

		// Verify alice's field survived on both sides
		results = append(results, verifyIssueField(h, issueID, pair.nameA, pair.valA)...)

		// Verify bob's field survived on both sides
		results = append(results, verifyIssueField(h, issueID, pair.nameB, pair.valB)...)
	}

	return results
}

// ScenarioRapidCreateDelete: create 10, sync, delete all, sync, restore 5, sync,
// delete 3, sync -- verify convergence at each stage.
func ScenarioRapidCreateDelete(h *Harness) []VerifyResult {
	var results []VerifyResult
	var issueIDs []string

	// Create 10 issues
	for i := 0; i < 10; i++ {
		out, err := h.TdA("create", fmt.Sprintf("rapid-create-delete-%d", i), "--type", "task", "--priority", "P2")
		if err != nil {
			results = append(results, fail(fmt.Sprintf("create %d", i), fmt.Sprintf("%v: %s", err, out)))
			continue
		}
		id := extractIssueID(out)
		if id != "" {
			issueIDs = append(issueIDs, id)
		}
	}

	if len(issueIDs) < 10 {
		results = append(results, fail("create phase", fmt.Sprintf("only created %d/10", len(issueIDs))))
		return results
	}

	// Sync after creates
	if err := h.SyncAll(); err != nil {
		return append(results, fail("sync after creates", err.Error()))
	}
	results = append(results, verifyConvergence(h, "alice", "bob")...)

	// Delete all 10
	for _, id := range issueIDs {
		h.TdA("delete", id)
	}

	// Sync after deletes
	if err := h.SyncAll(); err != nil {
		return append(results, fail("sync after deletes", err.Error()))
	}
	results = append(results, verifyConvergence(h, "alice", "bob")...)

	// Restore 5
	for i := 0; i < 5; i++ {
		h.TdA("restore", issueIDs[i])
	}

	// Sync after restores
	if err := h.SyncAll(); err != nil {
		return append(results, fail("sync after restores", err.Error()))
	}
	results = append(results, verifyConvergence(h, "alice", "bob")...)

	// Delete 3 of the restored
	for i := 0; i < 3; i++ {
		h.TdA("delete", issueIDs[i])
	}

	// Final sync
	if err := h.SyncAll(); err != nil {
		return append(results, fail("sync after re-deletes", err.Error()))
	}
	results = append(results, verifyConvergence(h, "alice", "bob")...)

	return results
}

// ScenarioCascadeConflict: parent + children in_progress, actor A moves parent
// to in_review (cascading children), actor B independently closes one child.
func ScenarioCascadeConflict(h *Harness) []VerifyResult {
	var results []VerifyResult

	// Create parent
	out, err := h.TdA("create", "cascade-conflict-parent-issue", "--type", "task", "--priority", "P1")
	if err != nil {
		return []VerifyResult{fail("create parent", fmt.Sprintf("%v: %s", err, out))}
	}
	parentID := extractIssueID(out)
	if parentID == "" {
		return []VerifyResult{fail("create parent", "no ID")}
	}

	// Create children
	var childIDs []string
	for i := 0; i < 3; i++ {
		out, err := h.TdA("create", fmt.Sprintf("cascade-conflict-child-%d", i),
			"--type", "task", "--priority", "P2", "--parent", parentID)
		if err != nil {
			results = append(results, fail(fmt.Sprintf("create child %d", i), fmt.Sprintf("%v: %s", err, out)))
			continue
		}
		id := extractIssueID(out)
		if id != "" {
			childIDs = append(childIDs, id)
		}
	}

	// Move all to in_progress
	h.TdA("start", parentID, "--reason", "cascade test")
	for _, cid := range childIDs {
		h.TdA("start", cid, "--reason", "cascade test")
	}

	// Sync so both sides have same state
	if err := h.SyncAll(); err != nil {
		return append(results, fail("cascade initial sync", err.Error()))
	}

	// Actor A: moves parent to in_review (should cascade children)
	h.TdA("review", parentID, "--reason", "cascade review test")

	// Actor B: independently closes one child
	if len(childIDs) > 0 {
		h.TdB("close", childIDs[0], "--reason", "cascade close test")
	}

	// Sync for convergence
	if err := h.SyncAll(); err != nil {
		return append(results, fail("cascade convergence sync", err.Error()))
	}

	results = append(results, verifyConvergence(h, "alice", "bob")...)
	return results
}

// ScenarioDependencyCycle: create issues A, B, C, sync, add deps A->B, B->C,
// then try C->A to create a cycle.
func ScenarioDependencyCycle(h *Harness) []VerifyResult {
	var results []VerifyResult

	// Create three issues
	var ids [3]string
	names := [3]string{"dependency-cycle-issue-A", "dependency-cycle-issue-B", "dependency-cycle-issue-C"}
	for i, name := range names {
		out, err := h.TdA("create", name, "--type", "task", "--priority", "P1")
		if err != nil {
			return []VerifyResult{fail(fmt.Sprintf("create %s", name), fmt.Sprintf("%v: %s", err, out))}
		}
		ids[i] = extractIssueID(out)
		if ids[i] == "" {
			return []VerifyResult{fail(fmt.Sprintf("create %s", name), "no ID")}
		}
	}

	// Sync
	if err := h.SyncAll(); err != nil {
		return []VerifyResult{fail("dep cycle sync 1", err.Error())}
	}

	// Actor A adds dep A->B
	out, err := h.TdA("dep", "add", ids[0], ids[1])
	if err != nil {
		results = append(results, fail("dep A->B", fmt.Sprintf("%v: %s", err, out)))
	} else {
		results = append(results, pass("dep A->B"))
	}

	// Actor B adds dep B->C
	out, err = h.TdB("dep", "add", ids[1], ids[2])
	if err != nil {
		results = append(results, fail("dep B->C", fmt.Sprintf("%v: %s", err, out)))
	} else {
		results = append(results, pass("dep B->C"))
	}

	// Sync
	if err := h.SyncAll(); err != nil {
		results = append(results, fail("dep cycle sync 2", err.Error()))
		return results
	}

	// Actor A tries to add dep C->A (should create cycle)
	out, err = h.TdA("dep", "add", ids[2], ids[0])
	if err != nil {
		// Expected: cycle detection should reject this
		lower := strings.ToLower(out)
		if strings.Contains(lower, "cycle") || strings.Contains(lower, "circular") {
			results = append(results, pass("cycle C->A rejected"))
		} else {
			results = append(results, fail("cycle C->A", fmt.Sprintf("rejected but not cycle error: %s", out)))
		}
	} else {
		// The dep was accepted -- this may or may not be correct depending on implementation
		results = append(results, fail("cycle C->A", "dep was accepted (cycle not detected)"))
	}

	// Sync and verify convergence
	if err := h.SyncAll(); err != nil {
		results = append(results, fail("dep cycle final sync", err.Error()))
	}
	results = append(results, verifyConvergence(h, "alice", "bob")...)

	return results
}

// ScenarioThunderingHerd: both actors make mutations, then both sync
// SIMULTANEOUSLY using goroutines, then verify convergence.
func ScenarioThunderingHerd(h *Harness) []VerifyResult {
	var results []VerifyResult

	// Both actors create issues
	for i := 0; i < 5; i++ {
		h.TdA("create", fmt.Sprintf("thundering-herd-alice-%d", i), "--type", "task", "--priority", "P1")
		h.TdB("create", fmt.Sprintf("thundering-herd-bob-%d", i), "--type", "task", "--priority", "P2")
	}

	// Both sync SIMULTANEOUSLY
	var wg sync.WaitGroup
	var errA, errB error
	var outA, outB string

	wg.Add(2)
	go func() {
		defer wg.Done()
		outA, errA = h.Td("alice", "sync")
	}()
	go func() {
		defer wg.Done()
		outB, errB = h.Td("bob", "sync")
	}()
	wg.Wait()

	if errA != nil {
		results = append(results, fail("concurrent sync alice", fmt.Sprintf("%v: %s", errA, outA)))
	} else {
		results = append(results, pass("concurrent sync alice"))
	}
	if errB != nil {
		results = append(results, fail("concurrent sync bob", fmt.Sprintf("%v: %s", errB, outB)))
	} else {
		results = append(results, pass("concurrent sync bob"))
	}

	// Now do convergence sync (sequential round-robin)
	if err := h.SyncAll(); err != nil {
		return append(results, fail("thundering herd convergence sync", err.Error()))
	}

	results = append(results, verifyConvergence(h, "alice", "bob")...)
	return results
}

// ScenarioBurstNoSync: actor A performs 20 sequential mutations on one issue
// without any sync, then syncs, and verifies actor B sees the final state.
func ScenarioBurstNoSync(h *Harness, rng *rand.Rand) []VerifyResult {
	var results []VerifyResult

	// Create an issue
	out, err := h.TdA("create", "burst-target-issue", "--type", "task", "--priority", "P1")
	if err != nil {
		return []VerifyResult{fail("burst create", fmt.Sprintf("%v: %s", err, out))}
	}
	issueID := extractIssueID(out)
	if issueID == "" {
		return []VerifyResult{fail("burst create", "no ID")}
	}

	// Sync so bob has the issue
	if err := h.SyncAll(); err != nil {
		return []VerifyResult{fail("burst initial sync", err.Error())}
	}

	// Now alice performs 20 sequential mutations WITHOUT syncing
	finalTitle := "burst-final-title"
	finalPriority := "P0"
	finalLabels := "burst-label,final"
	finalSprint := "sprint-burst"
	finalPoints := "8"

	// Mutations: various updates, comments, status changes, logs
	h.TdA("update", issueID, "--title", "burst-title-1")
	h.TdA("update", issueID, "--priority", "P3")
	h.TdA("comments", "add", issueID, "burst comment 1")
	h.TdA("update", issueID, "--labels", "burst-label")
	h.TdA("update", issueID, "--description", "burst description update 1")
	h.TdA("start", issueID, "--reason", "burst start")
	h.TdA("comments", "add", issueID, "burst comment 2")
	h.TdA("update", issueID, "--title", "burst-title-2")
	h.TdA("update", issueID, "--points", "5")
	h.TdA("log", "--issue", issueID, "burst log 1")
	h.TdA("update", issueID, "--sprint", "sprint-1")
	h.TdA("update", issueID, "--priority", "P2")
	h.TdA("comments", "add", issueID, "burst comment 3")
	h.TdA("update", issueID, "--description", "burst description final")
	h.TdA("update", issueID, "--title", "burst-title-3")
	h.TdA("log", "--issue", issueID, "burst log 2")
	h.TdA("update", issueID, "--labels", finalLabels)
	h.TdA("update", issueID, "--title", finalTitle)
	h.TdA("update", issueID, "--priority", finalPriority)
	h.TdA("update", issueID, "--points", finalPoints, "--sprint", finalSprint)

	// Now sync
	if err := h.SyncAll(); err != nil {
		return append(results, fail("burst convergence sync", err.Error()))
	}

	// Verify bob sees the final state
	results = append(results, verifyIssueField(h, issueID, "title", finalTitle)...)
	results = append(results, verifyIssueField(h, issueID, "priority", finalPriority)...)
	results = append(results, verifyIssueField(h, issueID, "labels", finalLabels)...)
	results = append(results, verifyIssueField(h, issueID, "sprint", finalSprint)...)
	results = append(results, verifyIssueField(h, issueID, "points", finalPoints)...)

	// Verify full convergence
	results = append(results, verifyConvergence(h, "alice", "bob")...)

	return results
}
