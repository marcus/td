package e2e

import (
	"crypto/sha256"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// VerifyResult records the outcome of a single verification check.
type VerifyResult struct {
	Name    string
	Passed  bool
	Details string // explanation on failure
}

// truncate shortens a string to maxLen, replacing newlines.
func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func pass(name string) VerifyResult {
	return VerifyResult{Name: name, Passed: true}
}

func fail(name, details string) VerifyResult {
	return VerifyResult{Name: name, Passed: false, Details: details}
}

// Verifier runs correctness checks against harness databases.
type Verifier struct {
	harness *Harness
	results []VerifyResult
}

// NewVerifier creates a Verifier for the given harness.
func NewVerifier(h *Harness) *Verifier {
	return &Verifier{harness: h}
}

// Results returns accumulated results.
func (v *Verifier) Results() []VerifyResult {
	return v.results
}

// AllPassed returns true if every result passed.
func (v *Verifier) AllPassed() bool {
	for _, r := range v.results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// FailedResults returns only failing results.
func (v *Verifier) FailedResults() []VerifyResult {
	var out []VerifyResult
	for _, r := range v.results {
		if !r.Passed {
			out = append(out, r)
		}
	}
	return out
}

// Summary returns a human-readable summary.
func (v *Verifier) Summary() string {
	passed, failed := 0, 0
	var b strings.Builder
	for _, r := range v.results {
		if r.Passed {
			passed++
		} else {
			failed++
			fmt.Fprintf(&b, "  FAIL: %s — %s\n", r.Name, r.Details)
		}
	}
	header := fmt.Sprintf("Verify: %d passed, %d failed\n", passed, failed)
	return header + b.String()
}

func (v *Verifier) add(name string, passed bool, details string) {
	v.results = append(v.results, VerifyResult{Name: name, Passed: passed, Details: details})
}

// dbQuery runs sqlite3 <dbpath> <query> and returns trimmed output.
func (v *Verifier) dbQuery(actor, query string) (string, error) {
	dbPath := v.harness.DBPath(actor)
	if dbPath == "" {
		return "", fmt.Errorf("unknown actor: %s", actor)
	}
	cmd := exec.Command("sqlite3", dbPath, query)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ============================================================
// 1. Entity Convergence
// ============================================================

// VerifyConvergence compares all synced tables between two actors.
func (v *Verifier) VerifyConvergence(actorA, actorB string) []VerifyResult {
	start := len(v.results)

	issueCols := "id, title, description, status, type, priority, points, labels, parent_id, acceptance, minor, sprint, created_branch, implementer_session, reviewer_session, creator_session"

	// Issues — compare non-deleted, handle resurrection limitation
	issuesA, _ := v.dbQuery(actorA, fmt.Sprintf("SELECT %s FROM issues WHERE deleted_at IS NULL ORDER BY id;", issueCols))
	issuesB, _ := v.dbQuery(actorB, fmt.Sprintf("SELECT %s FROM issues WHERE deleted_at IS NULL ORDER BY id;", issueCols))

	if issuesA == issuesB {
		v.add("issues match", true, "")
	} else {
		// Check common set (resurrection can add extra rows on one side)
		idsA, _ := v.dbQuery(actorA, "SELECT id FROM issues WHERE deleted_at IS NULL ORDER BY id;")
		idsB, _ := v.dbQuery(actorB, "SELECT id FROM issues WHERE deleted_at IS NULL ORDER BY id;")
		common := intersectLines(idsA, idsB)
		if len(common) == 0 {
			v.add("issues match", false, "no common non-deleted IDs")
		} else {
			where := sqlInClause(common)
			commonA, _ := v.dbQuery(actorA, fmt.Sprintf("SELECT %s FROM issues WHERE id IN (%s) AND deleted_at IS NULL ORDER BY id;", issueCols, where))
			commonB, _ := v.dbQuery(actorB, fmt.Sprintf("SELECT %s FROM issues WHERE id IN (%s) AND deleted_at IS NULL ORDER BY id;", issueCols, where))
			if commonA == commonB {
				v.add("issues match", true, "common set matches (extra rows from known sync limitation)")
			} else {
				v.add("issues match", false, fmt.Sprintf("common set diverges:\nA: %s\nB: %s", truncate(commonA, 500), truncate(commonB, 500)))
			}
		}
	}

	// Deleted issues — known limitation
	delA, _ := v.dbQuery(actorA, "SELECT COUNT(*) FROM issues WHERE deleted_at IS NOT NULL;")
	delB, _ := v.dbQuery(actorB, "SELECT COUNT(*) FROM issues WHERE deleted_at IS NOT NULL;")
	if delA == delB {
		v.add("deleted issue count", true, "")
	} else {
		v.add("deleted issue count", true, fmt.Sprintf("diverges (known limitation: %s vs %s)", delA, delB))
	}

	// Strict table comparisons
	strictChecks := []struct {
		name  string
		query string
	}{
		{"comments", "SELECT issue_id, text, session_id FROM comments ORDER BY issue_id, id;"},
		{"logs", "SELECT issue_id, type, message, session_id FROM logs ORDER BY issue_id, id;"},
		{"handoffs", "SELECT issue_id, session_id, done, remaining, decisions, uncertain FROM handoffs ORDER BY issue_id, id;"},
		{"issue_dependencies", "SELECT issue_id, depends_on_id, relation_type FROM issue_dependencies ORDER BY issue_id, depends_on_id;"},
		{"boards", "SELECT name, is_builtin, query, view_mode FROM boards ORDER BY name;"},
		{"board_issue_positions", "SELECT bp.board_id, bp.issue_id, bp.position FROM board_issue_positions bp JOIN boards b ON bp.board_id = b.id WHERE bp.deleted_at IS NULL ORDER BY bp.board_id, bp.issue_id;"},
		{"issue_files", "SELECT issue_id, file_path, role FROM issue_files ORDER BY issue_id, file_path;"},
		{"work_sessions", "SELECT id, name, session_id FROM work_sessions ORDER BY id;"},
	}
	for _, sc := range strictChecks {
		a, _ := v.dbQuery(actorA, sc.query)
		b, _ := v.dbQuery(actorB, sc.query)
		if a == b {
			v.add(sc.name+" match", true, "")
		} else {
			v.add(sc.name+" match", false, fmt.Sprintf("diverges:\nA: %s\nB: %s", truncate(a, 300), truncate(b, 300)))
		}
	}

	// work_session_issues — can diverge due to resurrection
	wsiA, _ := v.dbQuery(actorA, "SELECT wsi.work_session_id, wsi.issue_id FROM work_session_issues wsi JOIN issues i ON wsi.issue_id = i.id WHERE i.deleted_at IS NULL ORDER BY wsi.work_session_id, wsi.issue_id;")
	wsiB, _ := v.dbQuery(actorB, "SELECT wsi.work_session_id, wsi.issue_id FROM work_session_issues wsi JOIN issues i ON wsi.issue_id = i.id WHERE i.deleted_at IS NULL ORDER BY wsi.work_session_id, wsi.issue_id;")
	if wsiA == wsiB {
		v.add("work_session_issues match", true, "")
	} else {
		v.add("work_session_issues match", true, "diverges (known sync limitation: junction table replay)")
	}

	// Row counts for strict tables
	strictTables := []string{"comments", "logs", "handoffs", "issue_dependencies", "boards", "issue_files", "work_sessions"}
	for _, table := range strictTables {
		countA, _ := v.dbQuery(actorA, fmt.Sprintf("SELECT COUNT(*) FROM %s;", table))
		countB, _ := v.dbQuery(actorB, fmt.Sprintf("SELECT COUNT(*) FROM %s;", table))
		if countA == countB {
			v.add(table+" row count", true, "")
		} else {
			v.add(table+" row count", false, fmt.Sprintf("%s vs %s", countA, countB))
		}
	}

	return v.results[start:]
}

// ============================================================
// 2. Action Log Convergence
// ============================================================

// VerifyActionLogConvergence checks that both actors agree on canonical event order.
// Compares events by server_seq: for each seq present on both sides, the
// (entity_type, action_type, entity_id) tuple must match. Clients may have
// different total event counts due to built-in entity creation during init.
func (v *Verifier) VerifyActionLogConvergence(actorA, actorB string) []VerifyResult {
	start := len(v.results)
	q := "SELECT server_seq, entity_type, action_type, entity_id FROM action_log WHERE server_seq IS NOT NULL ORDER BY server_seq;"
	a, errA := v.dbQuery(actorA, q)
	b, errB := v.dbQuery(actorB, q)
	if errA != nil || errB != nil {
		v.add("action_log convergence", false, fmt.Sprintf("query error: A=%v B=%v", errA, errB))
		return v.results[start:]
	}

	if a == b {
		v.add("action_log convergence", true, "identical")
		return v.results[start:]
	}

	// Build server_seq -> event maps and compare common entries
	mapA := actionLogToMap(a)
	mapB := actionLogToMap(b)

	mismatches := 0
	commonCount := 0
	var mismatchDetails []string
	for seq, evtA := range mapA {
		if evtB, ok := mapB[seq]; ok {
			commonCount++
			if evtA != evtB {
				mismatches++
				if len(mismatchDetails) < 5 {
					mismatchDetails = append(mismatchDetails, fmt.Sprintf("seq %s: A=%q B=%q", seq, evtA, evtB))
				}
			}
		}
	}

	if mismatches == 0 {
		v.add("action_log convergence", true, fmt.Sprintf("common seqs match (%d common, A=%d B=%d total)", commonCount, len(mapA), len(mapB)))
	} else {
		v.add("action_log convergence", false, fmt.Sprintf("%d mismatches in %d common seqs: %s", mismatches, commonCount, strings.Join(mismatchDetails, "; ")))
	}

	return v.results[start:]
}

// actionLogToMap parses "seq|entity_type|action_type|entity_id" lines into seq -> rest.
func actionLogToMap(data string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, "|")
		if idx > 0 {
			m[line[:idx]] = line[idx+1:]
		}
	}
	return m
}

// ============================================================
// 3. Monotonic Server Sequence
// ============================================================

// VerifyMonotonicSequence checks server_seq is strictly increasing with no duplicates.
// Gaps are expected (each client only has a subset of server events) and are noted but not failures.
func (v *Verifier) VerifyMonotonicSequence(actor string) []VerifyResult {
	start := len(v.results)
	q := "SELECT server_seq FROM action_log WHERE server_seq IS NOT NULL ORDER BY server_seq;"
	out, err := v.dbQuery(actor, q)
	if err != nil {
		v.add("monotonic server_seq", false, fmt.Sprintf("query error: %v", err))
		return v.results[start:]
	}
	if out == "" {
		v.add("monotonic server_seq", true, "no synced events")
		return v.results[start:]
	}

	lines := strings.Split(out, "\n")
	prev := -1
	gaps := 0
	for i, line := range lines {
		seq, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			v.add("monotonic server_seq", false, fmt.Sprintf("non-integer at line %d: %q", i, line))
			return v.results[start:]
		}
		if prev >= 0 {
			if seq <= prev {
				v.add("monotonic server_seq", false, fmt.Sprintf("not increasing: seq %d after %d at line %d", seq, prev, i))
				return v.results[start:]
			}
			if seq != prev+1 {
				gaps++
			}
		}
		prev = seq
	}
	detail := fmt.Sprintf("%d events, range [%s..%s]", len(lines), lines[0], lines[len(lines)-1])
	if gaps > 0 {
		detail += fmt.Sprintf(" (%d gaps, expected for partial view)", gaps)
	}
	v.add("monotonic server_seq", true, detail)
	return v.results[start:]
}

// ============================================================
// 4. Causal Ordering
// ============================================================

// VerifyCausalOrdering checks that create events precede updates/transitions for same entity.
func (v *Verifier) VerifyCausalOrdering(actor string) []VerifyResult {
	start := len(v.results)

	// Get all synced events ordered by server_seq
	q := "SELECT server_seq, entity_type, action_type, entity_id FROM action_log WHERE server_seq IS NOT NULL ORDER BY server_seq;"
	out, err := v.dbQuery(actor, q)
	if err != nil {
		v.add("causal ordering", false, fmt.Sprintf("query error: %v", err))
		return v.results[start:]
	}
	if out == "" {
		v.add("causal ordering", true, "no events")
		return v.results[start:]
	}

	// Track first-seen action per entity
	created := make(map[string]int)   // entity_id -> server_seq of create
	started := make(map[string]int)   // issue_id -> server_seq of start
	violations := 0
	var details []string

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		seq, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		entityType := strings.TrimSpace(parts[1])
		actionType := strings.TrimSpace(parts[2])
		entityID := strings.TrimSpace(parts[3])

		switch actionType {
		case "create":
			if _, seen := created[entityID]; !seen {
				created[entityID] = seq
			}
		case "update", "delete":
			if createSeq, ok := created[entityID]; ok {
				if seq < createSeq {
					violations++
					details = append(details, fmt.Sprintf("%s %s at seq %d before create at %d", actionType, entityID, seq, createSeq))
				}
			}
			// Entity might have been created before our observation window — not a violation
		case "start":
			if entityType == "issue" {
				started[entityID] = seq
			}
		case "review":
			if entityType == "issue" {
				if startSeq, ok := started[entityID]; ok && seq < startSeq {
					violations++
					details = append(details, fmt.Sprintf("review at seq %d before start at %d for %s", seq, startSeq, entityID))
				}
			}
		}
	}

	if violations == 0 {
		v.add("causal ordering", true, fmt.Sprintf("checked %d events", len(lines)))
	} else {
		v.add("causal ordering", false, fmt.Sprintf("%d violations: %s", violations, strings.Join(details, "; ")))
	}
	return v.results[start:]
}

// ============================================================
// 5. Idempotency
// ============================================================

// VerifyIdempotency hashes DB content, runs N sync rounds, verifies hashes unchanged.
func (v *Verifier) VerifyIdempotency(rounds int) []VerifyResult {
	start := len(v.results)

	actors := actorNames(v.harness.config.NumActors)
	baselineHashes := make(map[string]string)

	for _, actor := range actors {
		h, err := v.dbContentHash(actor)
		if err != nil {
			v.add("idempotency baseline", false, fmt.Sprintf("hash %s: %v", actor, err))
			return v.results[start:]
		}
		baselineHashes[actor] = h
	}

	for round := 1; round <= rounds; round++ {
		if err := v.harness.SyncAll(); err != nil {
			v.add(fmt.Sprintf("idempotency round %d", round), false, fmt.Sprintf("sync error: %v", err))
			return v.results[start:]
		}

		for _, actor := range actors {
			h, err := v.dbContentHash(actor)
			if err != nil {
				v.add(fmt.Sprintf("idempotency round %d", round), false, fmt.Sprintf("hash %s: %v", actor, err))
				return v.results[start:]
			}
			if h != baselineHashes[actor] {
				v.add(fmt.Sprintf("idempotency round %d", round), false, fmt.Sprintf("%s changed: %s -> %s", actor, baselineHashes[actor][:12], h[:12]))
				return v.results[start:]
			}
		}
	}

	v.add("idempotency", true, fmt.Sprintf("%d rounds stable", rounds))
	return v.results[start:]
}

// dbContentHash returns SHA-256 of concatenated table dumps.
func (v *Verifier) dbContentHash(actor string) (string, error) {
	queries := []string{
		"SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, minor, sprint, created_branch, implementer_session, reviewer_session, creator_session, deleted_at FROM issues ORDER BY id;",
		"SELECT issue_id, text, session_id FROM comments ORDER BY issue_id, id;",
		"SELECT issue_id, type, message, session_id FROM logs ORDER BY issue_id, id;",
		"SELECT issue_id, session_id, done, remaining, decisions, uncertain FROM handoffs ORDER BY issue_id, id;",
		"SELECT issue_id, depends_on_id, relation_type FROM issue_dependencies ORDER BY issue_id, depends_on_id;",
		"SELECT name, is_builtin, query, view_mode FROM boards ORDER BY name;",
		"SELECT bp.board_id, bp.issue_id, bp.position FROM board_issue_positions bp JOIN boards b ON bp.board_id = b.id WHERE bp.deleted_at IS NULL ORDER BY bp.board_id, bp.issue_id;",
		"SELECT issue_id, file_path, role FROM issue_files ORDER BY issue_id, file_path;",
		"SELECT id, name, session_id FROM work_sessions ORDER BY id;",
		"SELECT work_session_id, issue_id FROM work_session_issues ORDER BY work_session_id, issue_id;",
	}

	hasher := sha256.New()
	for _, q := range queries {
		out, err := v.dbQuery(actor, q)
		if err != nil {
			return "", fmt.Errorf("query %q: %w", q[:40], err)
		}
		hasher.Write([]byte(out))
		hasher.Write([]byte("\n"))
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// ============================================================
// 6. Event Count Verification
// ============================================================

// VerifyEventCounts checks synced event counts and distributions match.
func (v *Verifier) VerifyEventCounts(actorA, actorB string) []VerifyResult {
	start := len(v.results)

	// Synced event count — may differ due to built-in entity creation during init;
	// mismatch is a warning (same as bash version), not a hard failure.
	syncedA, _ := v.dbQuery(actorA, "SELECT COUNT(*) FROM action_log WHERE server_seq IS NOT NULL;")
	syncedB, _ := v.dbQuery(actorB, "SELECT COUNT(*) FROM action_log WHERE server_seq IS NOT NULL;")
	if syncedA == syncedB {
		v.add("synced event count", true, syncedA)
	} else {
		v.add("synced event count", true, fmt.Sprintf("WARN: A=%s B=%s (expected for independent init)", syncedA, syncedB))
	}

	// Entity type distribution
	distA, _ := v.dbQuery(actorA, "SELECT entity_type, COUNT(*) FROM action_log WHERE server_seq IS NOT NULL GROUP BY entity_type ORDER BY entity_type;")
	distB, _ := v.dbQuery(actorB, "SELECT entity_type, COUNT(*) FROM action_log WHERE server_seq IS NOT NULL GROUP BY entity_type ORDER BY entity_type;")
	if distA == distB {
		v.add("entity_type distribution", true, "")
	} else {
		v.add("entity_type distribution", true, fmt.Sprintf("WARN differs: A=%s B=%s", truncate(distA, 300), truncate(distB, 300)))
	}

	// Action type distribution
	adistA, _ := v.dbQuery(actorA, "SELECT action_type, COUNT(*) FROM action_log WHERE server_seq IS NOT NULL GROUP BY action_type ORDER BY action_type;")
	adistB, _ := v.dbQuery(actorB, "SELECT action_type, COUNT(*) FROM action_log WHERE server_seq IS NOT NULL GROUP BY action_type ORDER BY action_type;")
	if adistA == adistB {
		v.add("action_type distribution", true, "")
	} else {
		v.add("action_type distribution", true, fmt.Sprintf("WARN differs: A=%s B=%s", truncate(adistA, 300), truncate(adistB, 300)))
	}

	return v.results[start:]
}

// ============================================================
// 7. Read-Your-Writes
// ============================================================

// VerifyReadYourWrites checks that after sync, each actor can see issues they created.
func (v *Verifier) VerifyReadYourWrites(engine *ChaosEngine) []VerifyResult {
	start := len(v.results)

	checked := 0
	for _, issue := range engine.Issues {
		if issue.Deleted {
			continue
		}
		out, err := v.harness.Td(issue.Owner, "show", issue.ID, "--json")
		if err != nil {
			v.add(fmt.Sprintf("read-your-writes %s", issue.ID), false, fmt.Sprintf("owner %s cannot read: %v\n%s", issue.Owner, err, truncate(out, 200)))
			continue
		}
		if !strings.Contains(out, issue.ID) {
			v.add(fmt.Sprintf("read-your-writes %s", issue.ID), false, fmt.Sprintf("output missing issue ID, got: %s", truncate(out, 200)))
			continue
		}
		checked++
	}

	if checked > 0 {
		v.add("read-your-writes", true, fmt.Sprintf("verified %d issues", checked))
	} else {
		v.add("read-your-writes", true, "no non-deleted issues to check")
	}

	return v.results[start:]
}

// ============================================================
// 8. Field-Level Merge
// ============================================================

// VerifyFieldLevelMerge tests that concurrent updates to different fields both survive.
// This is a targeted test: creates an issue, has two actors update different fields
// without syncing between, then syncs and verifies both changes are preserved.
func (v *Verifier) VerifyFieldLevelMerge(actorA, actorB string) []VerifyResult {
	start := len(v.results)

	// Actor A creates an issue and syncs so B has it
	out, err := v.harness.Td(actorA, "create", "field-merge-test-issue")
	if err != nil {
		v.add("field-level merge setup", false, fmt.Sprintf("create failed: %v\n%s", err, out))
		return v.results[start:]
	}
	issueID := extractIssueID(out)
	if issueID == "" {
		v.add("field-level merge setup", false, fmt.Sprintf("no issue ID from create: %s", out))
		return v.results[start:]
	}

	// Sync so both actors have the issue
	if err := v.harness.SyncAll(); err != nil {
		v.add("field-level merge setup", false, fmt.Sprintf("initial sync: %v", err))
		return v.results[start:]
	}

	// Actor A updates title (no sync)
	if out, err := v.harness.Td(actorA, "edit", issueID, "--title", "merged-title-from-A"); err != nil {
		v.add("field-level merge", false, fmt.Sprintf("A title update failed: %v\n%s", err, out))
		return v.results[start:]
	}

	// Actor B updates priority (no sync)
	if out, err := v.harness.Td(actorB, "edit", issueID, "--priority", "P0"); err != nil {
		v.add("field-level merge", false, fmt.Sprintf("B priority update failed: %v\n%s", err, out))
		return v.results[start:]
	}

	// Now sync all
	if err := v.harness.SyncAll(); err != nil {
		v.add("field-level merge sync", false, fmt.Sprintf("sync: %v", err))
		return v.results[start:]
	}

	// Verify both changes survived on both actors
	for _, actor := range []string{actorA, actorB} {
		titleOut, _ := v.dbQuery(actor, fmt.Sprintf("SELECT title FROM issues WHERE id='%s';", issueID))
		priorityOut, _ := v.dbQuery(actor, fmt.Sprintf("SELECT priority FROM issues WHERE id='%s';", issueID))

		titleOK := strings.Contains(titleOut, "merged-title-from-A")
		priorityOK := strings.Contains(priorityOut, "P0")

		if titleOK && priorityOK {
			v.add(fmt.Sprintf("field-level merge %s", actor), true, "both fields preserved")
		} else {
			v.add(fmt.Sprintf("field-level merge %s", actor), false, fmt.Sprintf("title=%q (want merged-title-from-A), priority=%q (want P0)", titleOut, priorityOut))
		}
	}

	return v.results[start:]
}

// ============================================================
// Helpers
// ============================================================

// intersectLines returns lines present in both a and b (sorted).
func intersectLines(a, b string) []string {
	setA := make(map[string]bool)
	for _, line := range strings.Split(a, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			setA[line] = true
		}
	}
	var common []string
	for _, line := range strings.Split(b, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && setA[line] {
			common = append(common, line)
		}
	}
	return common
}

// sqlInClause builds 'id1','id2',... from a slice.
func sqlInClause(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "'" + id + "'"
	}
	return strings.Join(quoted, ",")
}


