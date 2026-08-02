package db

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/marcus/td/internal/models"
)

// ============================================================================
// The sync-event invariant
//
// The sync engine derives its outbound events EXCLUSIVELY from action_log
// (internal/sync/client.go GetPendingEvents:
// `SELECT ... FROM action_log WHERE synced_at IS NULL`). So a row written into
// a syncable table WITHOUT a matching create-style action_log entry exists on
// the writing client and nowhere else, permanently — sync.BackfillOrphanEntities
// cannot rescue it, because it bails out once last_pulled_server_seq > 0, i.e.
// after the client's very first pull.
//
// That defect has now shipped twice, at two different producers:
//
//	1. db.recordClaimReleaseHistory (internal/db/claims.go) — the `td unstart`
//	   progress log. Regressed by ebc5d59.
//	2. db.addLogEntry (internal/db/issues_logged.go) — the "Auto-cascaded to X"
//	   and "Auto-unblocked (dependency X closed)" rows written from
//	   internal/db/issue_relations.go.
//
// Per-site regression tests only cover the sites somebody thought of, and this
// class is defined by the site nobody thought of. So the guard here is global:
// exercise a broad stream of ordinary local mutations, then assert that EVERY
// row in EVERY syncable table has a create event. A new producer that forgets
// its action_log write fails this test the moment any test-exercised path
// reaches it, without anyone having to anticipate it.
// ============================================================================

// syncableTableUnderTest mirrors one entry of internal/sync's syncableTables.
// That list is unexported, so it cannot be imported; TestSyncableTableListHasNotDrifted
// below reads internal/sync/backfill.go and fails if the two sets diverge.
type syncableTableUnderTest struct {
	table       string
	aliases     []string
	createTypes []string
	softDelete  bool
}

var syncableTablesUnderTest = []syncableTableUnderTest{
	{"issues", []string{"issue", "issues"}, []string{"create"}, false},
	{"logs", []string{"log", "logs"}, []string{"create"}, false},
	{"comments", []string{"comment", "comments"}, []string{"create"}, false},
	{"handoffs", []string{"handoff", "handoffs"}, []string{"handoff"}, false},
	{"boards", []string{"board", "boards"}, []string{"board_create"}, false},
	{"work_sessions", []string{"work_session", "work_sessions"}, []string{"create"}, false},
	{"board_issue_positions", []string{"board_position", "board_issue_positions"}, []string{"board_set_position", "board_add_issue"}, true},
	{"issue_dependencies", []string{"dependency", "issue_dependencies"}, []string{"add_dependency"}, false},
	{"issue_files", []string{"file_link", "issue_files"}, []string{"link_file"}, false},
	{"work_session_issues", []string{"work_session_issue", "work_session_issues"}, []string{"work_session_tag"}, false},
	{"issue_reviews", []string{"issue_review", "issue_reviews"}, []string{"create"}, false},
	{"notes", []string{"note", "notes"}, []string{"create"}, true},
}

// findRowsWithoutSyncEvents returns the ids of rows in one syncable table that
// have no create-style action_log entry — i.e. rows no other client will ever
// see. The query mirrors sync.backfillTable's orphan query, including its
// exclusion of builtin boards (seeded by schema.go, deliberately never synced).
func findRowsWithoutSyncEvents(t *testing.T, database *DB, st syncableTableUnderTest) []string {
	t.Helper()

	var exists int
	if err := database.conn.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, st.table,
	).Scan(&exists); err != nil || exists == 0 {
		return nil
	}

	args := make([]any, 0, len(st.aliases)+len(st.createTypes))
	aliasPH := make([]string, len(st.aliases))
	for i, a := range st.aliases {
		aliasPH[i] = "?"
		args = append(args, a)
	}
	createPH := make([]string, len(st.createTypes))
	for i, a := range st.createTypes {
		createPH[i] = "?"
		args = append(args, a)
	}

	extra := ""
	if st.softDelete {
		extra += " AND t.deleted_at IS NULL"
	}
	if st.table == "boards" {
		extra += " AND t.is_builtin = 0"
	}

	query := fmt.Sprintf(`SELECT CAST(t.id AS TEXT) FROM %s t WHERE NOT EXISTS (
			SELECT 1 FROM action_log al
			WHERE al.entity_id = t.id
			AND al.entity_type IN (%s)
			AND al.action_type IN (%s)
			AND al.undone = 0
		)%s`, st.table, strings.Join(aliasPH, ","), strings.Join(createPH, ","), extra)

	rows, err := database.conn.Query(query, args...)
	if err != nil {
		t.Fatalf("orphan query for %s: %v", st.table, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan orphan id for %s: %v", st.table, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate orphans for %s: %v", st.table, err)
	}
	return ids
}

// assertEverySyncableRowHasASyncEvent is the invariant assertion itself.
func assertEverySyncableRowHasASyncEvent(t *testing.T, database *DB) {
	t.Helper()
	for _, st := range syncableTablesUnderTest {
		if orphans := findRowsWithoutSyncEvents(t, database, st); len(orphans) > 0 {
			t.Errorf("%s: %d row(s) written with no create action_log entry, so no other "+
				"client will ever see them: %v", st.table, len(orphans), orphans)
		}
	}
}

// TestEverySyncableRowGetsASyncEvent drives a broad stream of ordinary local
// mutations — the kind a session produces in an afternoon — and then asserts
// the global invariant. It deliberately uses the *logged* API variants
// throughout: the unlogged twins (AddDependency, CreateBoard, ...) exist for
// the sync receiver applying remote events, where a local action_log entry
// would be wrong, and using one here would be testing the wrong contract.
func TestEverySyncableRowGetsASyncEvent(t *testing.T) {
	database := claimTestDB(t)
	const session = "ses_invariant"

	// --- issues, including a parent/child tree and a blocked dependent ------
	epic := &models.Issue{Title: "Invariant epic", Type: models.TypeEpic, Status: models.StatusOpen}
	if err := database.CreateIssueLogged(epic, session); err != nil {
		t.Fatalf("CreateIssueLogged epic: %v", err)
	}
	child := &models.Issue{Title: "Invariant child", Type: models.TypeTask,
		ParentID: epic.ID, Status: models.StatusOpen}
	if err := database.CreateIssueLogged(child, session); err != nil {
		t.Fatalf("CreateIssueLogged child: %v", err)
	}
	dependent := &models.Issue{Title: "Invariant dependent", Type: models.TypeTask, Status: models.StatusOpen}
	if err := database.CreateIssueLogged(dependent, session); err != nil {
		t.Fatalf("CreateIssueLogged dependent: %v", err)
	}

	// --- logs, comments, handoffs ------------------------------------------
	if err := database.AddLog(&models.Log{IssueID: child.ID, SessionID: session,
		Message: "manual progress note", Type: models.LogTypeProgress}); err != nil {
		t.Fatalf("AddLog: %v", err)
	}
	if err := database.AddComment(&models.Comment{IssueID: child.ID, SessionID: session,
		Text: "a comment"}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := database.AddHandoff(&models.Handoff{IssueID: child.ID, SessionID: session,
		Done: []string{"some work"}, Remaining: []string{"more work"}}); err != nil {
		t.Fatalf("AddHandoff: %v", err)
	}

	// --- dependencies and file links ---------------------------------------
	if err := database.AddDependencyLogged(dependent.ID, child.ID, "depends_on", session); err != nil {
		t.Fatalf("AddDependencyLogged: %v", err)
	}
	if err := database.LinkFileLogged(child.ID, "internal/db/issues_logged.go",
		models.FileRoleImplementation, "", session); err != nil {
		t.Fatalf("LinkFileLogged: %v", err)
	}

	// --- boards and board positions ----------------------------------------
	board, err := database.CreateBoardLogged("Invariant board", "", session)
	if err != nil {
		t.Fatalf("CreateBoardLogged: %v", err)
	}
	if err := database.SetIssuePositionLogged(board.ID, child.ID, 1, session); err != nil {
		t.Fatalf("SetIssuePositionLogged: %v", err)
	}

	// --- work sessions and their issue tags ---------------------------------
	ws := &models.WorkSession{Name: "invariant work session", SessionID: session}
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession: %v", err)
	}
	if err := database.TagIssueToWorkSession(ws.ID, child.ID, session); err != nil {
		t.Fatalf("TagIssueToWorkSession: %v", err)
	}

	// --- notes --------------------------------------------------------------
	if _, err := database.CreateNote("invariant note", "body"); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	// --- the claim-release path (producer #1) -------------------------------
	claimed := &models.Issue{Title: "invariant claimed", Type: models.TypeTask, Status: models.StatusOpen}
	if err := database.CreateIssueLogged(claimed, session); err != nil {
		t.Fatalf("CreateIssueLogged claimed: %v", err)
	}
	if claimed, err = database.GetIssue(claimed.ID); err != nil {
		t.Fatalf("GetIssue claimed: %v", err)
	}
	claimed.Status = models.StatusInProgress
	claimed.ImplementerSession = "ses_dead"
	if err := database.UpdateIssueLogged(claimed, "ses_dead", models.ActionStart); err != nil {
		t.Fatalf("claim issue: %v", err)
	}
	if _, err := database.ReleaseClaims(
		[]ClaimRelease{{IssueID: claimed.ID, LogMessage: "released by invariant sweep"}},
		session); err != nil {
		t.Fatalf("ReleaseClaims: %v", err)
	}

	// --- the auto-cascade path (producer #2, site A) ------------------------
	// Every child of the epic reaches closed, so the epic cascades and
	// db.addLogEntry writes "Auto-cascaded to closed (all children complete)".
	child.Status = models.StatusClosed
	if err := database.UpdateIssueLogged(child, session, models.ActionClose); err != nil {
		t.Fatalf("close child: %v", err)
	}
	if count, _ := database.CascadeUpParentStatus(child.ID, models.StatusClosed, session); count != 1 {
		t.Fatalf("expected the epic to cascade to closed, cascaded %d", count)
	}

	// --- the auto-unblock path (producer #2, site B) ------------------------
	// The dependent's only dependency is now closed, so it unblocks and
	// db.addLogEntry writes "Auto-unblocked (dependency X closed)".
	dependent, err = database.GetIssue(dependent.ID)
	if err != nil {
		t.Fatalf("GetIssue dependent: %v", err)
	}
	dependent.Status = models.StatusBlocked
	if err := database.UpdateIssueLogged(dependent, session, models.ActionBlock); err != nil {
		t.Fatalf("block dependent: %v", err)
	}
	if count, _ := database.CascadeUnblockDependents(child.ID, session); count != 1 {
		t.Fatalf("expected the dependent to auto-unblock, unblocked %d", count)
	}

	// Sanity: the two cascade logs really were produced, so a future refactor
	// that quietly stops writing them cannot make this test vacuously pass.
	for _, want := range []string{"Auto-cascaded to closed", "Auto-unblocked (dependency"} {
		var n int
		if err := database.conn.QueryRow(
			`SELECT COUNT(*) FROM logs WHERE message LIKE ?`, want+"%").Scan(&n); err != nil {
			t.Fatalf("count %q logs: %v", want, err)
		}
		if n == 0 {
			t.Fatalf("no %q log row was produced — the invariant assertion below "+
				"would pass without ever exercising db.addLogEntry", want)
		}
	}

	assertEverySyncableRowHasASyncEvent(t, database)
}

// assertLogRowHasSyncEvent finds the logs row whose message starts with prefix
// and asserts it carries a create/logs action_log entry naming it.
func assertLogRowHasSyncEvent(t *testing.T, database *DB, prefix string) {
	t.Helper()

	var logID, message string
	if err := database.conn.QueryRow(
		`SELECT CAST(id AS TEXT), message FROM logs WHERE message LIKE ?`, prefix+"%",
	).Scan(&logID, &message); err != nil {
		t.Fatalf("no logs row matching %q: %v", prefix, err)
	}

	var newData string
	if err := database.conn.QueryRow(
		`SELECT new_data FROM action_log
		 WHERE entity_type = 'logs' AND entity_id = ? AND action_type = 'create'`,
		logID).Scan(&newData); err != nil {
		t.Fatalf("no sync event for log %s (%q): %v — this row exists on this "+
			"client and nowhere else, permanently", logID, message, err)
	}
	if !strings.Contains(newData, prefix) {
		t.Fatalf("sync event payload for %s does not carry the message: %s", logID, newData)
	}
}

// TestAutoCascadeLogEmitsASyncEvent is the per-site guard for the
// "Auto-cascaded to X (all children complete)" row: a cascade is computed
// locally from local state, so the peer never recomputes it and, without a
// create/logs event, never receives it either.
func TestAutoCascadeLogEmitsASyncEvent(t *testing.T) {
	database := claimTestDB(t)
	const session = "ses_cascade"

	epic := &models.Issue{Title: "Cascade epic", Type: models.TypeEpic, Status: models.StatusOpen}
	if err := database.CreateIssueLogged(epic, session); err != nil {
		t.Fatalf("CreateIssueLogged epic: %v", err)
	}
	child := &models.Issue{Title: "Cascade child", Type: models.TypeTask,
		ParentID: epic.ID, Status: models.StatusClosed}
	if err := database.CreateIssueLogged(child, session); err != nil {
		t.Fatalf("CreateIssueLogged child: %v", err)
	}
	if child, err := database.GetIssue(child.ID); err == nil {
		child.Status = models.StatusClosed
		if err := database.UpdateIssueLogged(child, session, models.ActionClose); err != nil {
			t.Fatalf("close child: %v", err)
		}
	}

	if count, _ := database.CascadeUpParentStatus(child.ID, models.StatusClosed, session); count != 1 {
		t.Fatalf("expected 1 cascade, got %d", count)
	}

	assertLogRowHasSyncEvent(t, database, "Auto-cascaded to closed")
}

// TestAutoUnblockLogEmitsASyncEvent is the per-site guard for the
// "Auto-unblocked (dependency X closed)" row. Same mechanism as the cascade.
func TestAutoUnblockLogEmitsASyncEvent(t *testing.T) {
	database := claimTestDB(t)
	const session = "ses_unblock"

	blocker := &models.Issue{Title: "Blocker", Type: models.TypeTask, Status: models.StatusClosed}
	if err := database.CreateIssueLogged(blocker, session); err != nil {
		t.Fatalf("CreateIssueLogged blocker: %v", err)
	}
	dependent := &models.Issue{Title: "Dependent", Type: models.TypeTask, Status: models.StatusBlocked}
	if err := database.CreateIssueLogged(dependent, session); err != nil {
		t.Fatalf("CreateIssueLogged dependent: %v", err)
	}
	if err := database.AddDependencyLogged(dependent.ID, blocker.ID, "depends_on", session); err != nil {
		t.Fatalf("AddDependencyLogged: %v", err)
	}

	if count, _ := database.CascadeUnblockDependents(blocker.ID, session); count != 1 {
		t.Fatalf("expected 1 unblock, got %d", count)
	}

	assertLogRowHasSyncEvent(t, database, "Auto-unblocked (dependency")
}

// TestSyncableTableListHasNotDrifted keeps syncableTablesUnderTest honest.
// internal/sync's syncableTables is unexported, so the invariant test cannot
// import it; without this guard a table added to the sync engine would silently
// escape the invariant check above. Reading the source is cheap and the failure
// message says exactly what to do.
func TestSyncableTableListHasNotDrifted(t *testing.T) {
	src, err := os.ReadFile("../sync/backfill.go")
	if err != nil {
		t.Fatalf("read internal/sync/backfill.go: %v", err)
	}

	body := string(src)
	start := strings.Index(body, "var syncableTables = []syncableTable{")
	if start < 0 {
		t.Fatal("could not find syncableTables in internal/sync/backfill.go; " +
			"if it moved or was renamed, update this guard and syncableTablesUnderTest")
	}
	end := strings.Index(body[start:], "\n}")
	if end < 0 {
		t.Fatal("could not find the end of the syncableTables literal")
	}

	// Match only the first field of each top-level entry (`\n\t{"issues", ...`);
	// the alias slices nested inside use the same quoting but never start a line.
	entry := regexp.MustCompile(`\n\t\{"([a-z_]+)",`)
	var declared []string
	for _, m := range entry.FindAllStringSubmatch(body[start:start+end], -1) {
		declared = append(declared, m[1])
	}
	if len(declared) == 0 {
		t.Fatal("parsed zero tables out of syncableTables; the guard needs updating")
	}

	var covered []string
	for _, st := range syncableTablesUnderTest {
		covered = append(covered, st.table)
	}
	sort.Strings(declared)
	sort.Strings(covered)

	if strings.Join(declared, ",") != strings.Join(covered, ",") {
		t.Errorf("syncableTablesUnderTest has drifted from internal/sync's syncableTables.\n"+
			"  sync declares: %v\n  invariant test covers: %v\n"+
			"Add the missing table(s) to syncableTablesUnderTest, and make sure "+
			"TestEverySyncableRowGetsASyncEvent exercises a write to them.", declared, covered)
	}
}
