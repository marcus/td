package api

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tddb "github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/serve"
)

// openEventsDB opens the on-disk events.db for the harness's project so tests
// can read promoted rows directly. Read-only to avoid contending with the
// live pool's writer connection on the same file.
func openEventsDBRO(t *testing.T, dataDir, projectID string) *sql.DB {
	t.Helper()
	path := filepath.Join(dataDir, projectID, "events.db")
	conn, err := tddb.OpenSQLite(path, tddb.OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("open events.db readonly: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

type promotedEventRow struct {
	ServerSeq      int64
	DeviceID       string
	SessionID      string
	ClientActionID int64
	ActionType     string
	EntityType     string
	EntityID       string
}

func readPromotedEvents(t *testing.T, db *sql.DB) []promotedEventRow {
	t.Helper()
	rows, err := db.Query(`SELECT server_seq, device_id, session_id, client_action_id, action_type, entity_type, entity_id FROM events ORDER BY server_seq ASC`)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()
	var out []promotedEventRow
	for rows.Next() {
		var r promotedEventRow
		if err := rows.Scan(&r.ServerSeq, &r.DeviceID, &r.SessionID, &r.ClientActionID, &r.ActionType, &r.EntityType, &r.EntityID); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		out = append(out, r)
	}
	return out
}

// --- TestPromote_RoundTrip ------------------------------------------------

func TestPromote_RoundTrip(t *testing.T) {
	h := newProjectRoutesHarness(t)

	body := serve.IssueCreateBody{
		Title:    "Issue created via REST for promotion round-trip",
		Type:     "task",
		Priority: "P1",
	}
	resp := h.do(t, "POST", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
		h.ownerTok, body, map[string]string{HeaderTdWatchSession: "alice"})
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("POST /issues: status=%d body=%s", resp.StatusCode, respBody)
	}
	var created struct {
		Issue serve.IssueDTO `json:"issue"`
	}
	readEnvelope(t, resp, &created)

	// events.db must exist and have the promoted event.
	eventsDB := openEventsDBRO(t, h.dataDir, h.pid)
	events := readPromotedEvents(t, eventsDB)
	if len(events) == 0 {
		t.Fatal("expected at least one promoted event, got 0")
	}

	// Find the create_issue event for this issue.
	var found *promotedEventRow
	for i := range events {
		if events[i].EntityID == created.Issue.ID {
			found = &events[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no event found for entity_id=%s; events=%+v", created.Issue.ID, events)
	}
	if found.DeviceID != TdWatchServerDeviceID {
		t.Errorf("device_id = %q, want %q", found.DeviceID, TdWatchServerDeviceID)
	}
	if !strings.HasPrefix(found.SessionID, "twu_") {
		t.Errorf("session_id = %q, want twu_* prefix", found.SessionID)
	}
	if !strings.Contains(found.SessionID, "alice") {
		t.Errorf("session_id = %q, want to contain 'alice'", found.SessionID)
	}
	// Action type for create issue normalises to "create".
	if found.ActionType != "create" {
		t.Errorf("action_type = %q, want create", found.ActionType)
	}
	if found.EntityType != "issues" {
		t.Errorf("entity_type = %q, want issues", found.EntityType)
	}

	// action_log row(s) for this issue must be marked synced_at.
	projDB := h.openProjectDB(t)
	var unsynced int
	if err := projDB.Conn().QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND synced_at IS NULL`,
		created.Issue.ID,
	).Scan(&unsynced); err != nil {
		t.Fatalf("count unsynced: %v", err)
	}
	if unsynced != 0 {
		t.Errorf("unsynced action_log rows for %s: got %d, want 0", created.Issue.ID, unsynced)
	}
}

// --- TestPromote_PreservesSessionPerRow -----------------------------------

func TestPromote_PreservesSessionPerRow(t *testing.T) {
	srv, store := newTestServer(t)
	owner, _ := createTestUser(t, store, "owner@test.com")
	proj, err := store.CreateProject("p", "", owner)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID

	// Acquire the project DB and pre-seed action_log with two distinct
	// session_ids (no http traffic — we want to test the inner promotion
	// behaviour, not handler integration).
	liveDB, err := srv.projectLivePool.Acquire(pid)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer srv.projectLivePool.Release(pid)

	// Seed: two issues, two action_log rows, two session_ids.
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	insertIssueAndAction := func(issueID, sessionID string) {
		t.Helper()
		if _, err := liveDB.Conn().Exec(
			`INSERT INTO issues (id, title, status, priority) VALUES (?, ?, 'open', 'P1')`,
			issueID, "Title for "+issueID,
		); err != nil {
			t.Fatalf("insert issue: %v", err)
		}
		newData := fmt.Sprintf(`{"id":"%s","title":"Title for %s","status":"open","priority":"P1"}`, issueID, issueID)
		if _, err := liveDB.Conn().Exec(
			`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
			 VALUES (?, ?, 'create', 'issue', ?, '', ?, ?, 0)`,
			fmt.Sprintf("al-%s", issueID), sessionID, issueID, newData, now,
		); err != nil {
			t.Fatalf("insert action_log: %v", err)
		}
	}
	insertIssueAndAction("i1", "twu_alice")
	insertIssueAndAction("i2", "twa_bob_as_carol")

	// Run promotion.
	n, err := srv.promoteActionLog(pid, liveDB)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if n != 2 {
		t.Fatalf("promoted = %d, want 2", n)
	}

	// Verify events.db has both rows with the original session_ids.
	eventsDB := openEventsDBRO(t, srv.config.ProjectDataDir, pid)
	events := readPromotedEvents(t, eventsDB)
	if len(events) != 2 {
		t.Fatalf("events count = %d, want 2; events=%+v", len(events), events)
	}
	bySession := map[string]promotedEventRow{}
	for _, e := range events {
		bySession[e.SessionID] = e
	}
	if e, ok := bySession["twu_alice"]; !ok {
		t.Errorf("missing event for session twu_alice; events=%+v", events)
	} else if e.EntityID != "i1" {
		t.Errorf("twu_alice event entity_id=%q, want i1", e.EntityID)
	}
	if e, ok := bySession["twa_bob_as_carol"]; !ok {
		t.Errorf("missing event for session twa_bob_as_carol; events=%+v", events)
	} else if e.EntityID != "i2" {
		t.Errorf("twa_bob_as_carol event entity_id=%q, want i2", e.EntityID)
	}

	// Both rows must have device_id == td_watch_server.
	for _, e := range events {
		if e.DeviceID != TdWatchServerDeviceID {
			t.Errorf("event %d device_id=%q, want %q", e.ServerSeq, e.DeviceID, TdWatchServerDeviceID)
		}
	}

	// Re-running promotion is a no-op (synced_at gate).
	n2, err := srv.promoteActionLog(pid, liveDB)
	if err != nil {
		t.Fatalf("promote rerun: %v", err)
	}
	if n2 != 0 {
		t.Errorf("rerun promoted = %d, want 0 (synced_at gate)", n2)
	}
}

// --- TestPromote_NoMutation_NoOp ------------------------------------------

func TestPromote_NoMutation_NoOp(t *testing.T) {
	h := newProjectRoutesHarness(t)

	// Plain GET — must not produce events.db rows. (The handler still
	// passes through wrapServeHandler; shouldPromote returns false for GET.)
	resp := h.do(t, "GET", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
		h.ownerTok, nil, map[string]string{HeaderTdWatchSession: "alice"})
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("GET /issues: status=%d body=%s", resp.StatusCode, respBody)
	}
	resp.Body.Close()

	eventsPath := filepath.Join(h.dataDir, h.pid, "events.db")
	if _, err := os.Stat(eventsPath); err == nil {
		// events.db can still be missing — the only way it would exist after
		// pure GETs is if promoteActionLog ran. Confirm zero rows if it did.
		eventsDB := openEventsDBRO(t, h.dataDir, h.pid)
		evs := readPromotedEvents(t, eventsDB)
		if len(evs) != 0 {
			t.Errorf("GET produced %d events; want 0", len(evs))
		}
	}

	// Direct unit-level call: promoteActionLog with no pending rows is a no-op.
	srv := h.srv
	liveDB, err := srv.projectLivePool.Acquire(h.pid)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer srv.projectLivePool.Release(h.pid)

	n, err := srv.promoteActionLog(h.pid, liveDB)
	if err != nil {
		t.Fatalf("promote no-op: %v", err)
	}
	if n != 0 {
		t.Errorf("no-op promote returned n=%d, want 0", n)
	}
}

// --- TestPromote_AttachIsolation ------------------------------------------

func TestPromote_AttachIsolation(t *testing.T) {
	// Two concurrent REST POSTs to the same project. SQLite's single-writer
	// semantics serialize the two transactions; what we want to verify here
	// is that we don't double-promote any single action_log row (the
	// synced_at gate ensures each row is processed exactly once).
	h := newProjectRoutesHarness(t)

	const writers = 4
	const perWriter = 3
	var wg sync.WaitGroup
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				body := serve.IssueCreateBody{
					Title: fmt.Sprintf("issue from writer %d round %d", idx, j),
					Type:  "task",
				}
				resp := h.do(t, "POST", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
					h.ownerTok, body, map[string]string{
						HeaderTdWatchSession: fmt.Sprintf("w%d", idx),
					})
				if resp.StatusCode != http.StatusCreated {
					respBody, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					t.Errorf("writer %d round %d: status=%d body=%s", idx, j, resp.StatusCode, respBody)
					return
				}
				resp.Body.Close()
			}
		}(w)
	}
	wg.Wait()

	eventsDB := openEventsDBRO(t, h.dataDir, h.pid)
	events := readPromotedEvents(t, eventsDB)

	// We expect exactly writers*perWriter creates (no duplicates from
	// concurrent promotion runs).
	wantCreates := writers * perWriter
	got := 0
	seen := map[string]int{}
	for _, e := range events {
		if e.ActionType == "create" && e.EntityType == "issues" {
			got++
		}
		key := fmt.Sprintf("%s|%s|%d", e.DeviceID, e.SessionID, e.ClientActionID)
		seen[key]++
	}
	if got != wantCreates {
		t.Errorf("create-issue events = %d, want %d", got, wantCreates)
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("duplicate dedupe key %q: count=%d", k, n)
		}
	}

	// All action_log rows in project.db must be marked synced.
	projDB := h.openProjectDB(t)
	var unsynced int
	if err := projDB.Conn().QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0`,
	).Scan(&unsynced); err != nil {
		t.Fatalf("count unsynced: %v", err)
	}
	if unsynced != 0 {
		t.Errorf("unsynced action_log rows after concurrent promotion: %d, want 0", unsynced)
	}
}

// --- TestPromote_RecoveryAfterError ---------------------------------------

func TestPromote_RecoveryAfterError(t *testing.T) {
	// Simulate a mid-batch insert failure by making events.db read-only AFTER
	// schema init, then verify:
	//   1. promoteActionLog returns an error.
	//   2. action_log.synced_at remains NULL on the un-promoted row.
	//   3. After we restore write access, a second promoteActionLog call
	//      successfully promotes the row.
	srv, store := newTestServer(t)
	owner, _ := createTestUser(t, store, "owner@test.com")
	proj, err := store.CreateProject("p", "", owner)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID

	liveDB, err := srv.projectLivePool.Acquire(pid)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer srv.projectLivePool.Release(pid)

	// Seed one row.
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if _, err := liveDB.Conn().Exec(
		`INSERT INTO issues (id, title, status, priority) VALUES ('i1','t','open','P1')`,
	); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := liveDB.Conn().Exec(
		`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
		 VALUES ('al-1','twu_alice','create','issue','i1','','{"id":"i1","title":"t","status":"open","priority":"P1"}',?,0)`,
		now,
	); err != nil {
		t.Fatalf("insert action_log: %v", err)
	}

	// Pre-create events.db with the schema, then chmod 0o400 so the ATTACH
	// can succeed but INSERT will fail with "attempt to write a readonly
	// database".
	eventsPath := filepath.Join(srv.config.ProjectDataDir, pid, "events.db")
	if err := ensureEventsDBSchema(eventsPath); err != nil {
		t.Fatalf("ensure events schema: %v", err)
	}
	// Lock down the file (and the WAL/SHM siblings if present) so writes
	// fail. SQLite in WAL mode also writes to events.db-wal / events.db-shm,
	// so locking those down too forces the writer path to error out.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := eventsPath + suffix
		if _, err := os.Stat(p); err == nil {
			if err := os.Chmod(p, 0o400); err != nil {
				t.Fatalf("chmod %s: %v", p, err)
			}
			defer os.Chmod(p, 0o600) //nolint:errcheck // best-effort cleanup
		}
	}
	// Also lock the project directory to block creation of new -wal/-shm.
	projDir := filepath.Dir(eventsPath)
	if err := os.Chmod(projDir, 0o500); err != nil {
		t.Fatalf("chmod projDir: %v", err)
	}
	defer os.Chmod(projDir, 0o755) //nolint:errcheck

	_, err = srv.promoteActionLog(pid, liveDB)
	if err == nil {
		t.Fatal("expected promoteActionLog to fail with read-only events.db, got nil")
	}

	// Verify action_log.synced_at is still NULL — the recovery valve.
	var syncedAt sql.NullString
	if err := liveDB.Conn().QueryRow(
		`SELECT synced_at FROM action_log WHERE id = 'al-1'`,
	).Scan(&syncedAt); err != nil {
		t.Fatalf("query synced_at: %v", err)
	}
	if syncedAt.Valid {
		t.Errorf("synced_at = %q after failed promotion; want NULL (recovery valve)", syncedAt.String)
	}

	// Restore write access and retry.
	if err := os.Chmod(projDir, 0o755); err != nil {
		t.Fatalf("restore projDir: %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := eventsPath + suffix
		if _, err := os.Stat(p); err == nil {
			if err := os.Chmod(p, 0o600); err != nil {
				t.Fatalf("restore chmod %s: %v", p, err)
			}
		}
	}

	n, err := srv.promoteActionLog(pid, liveDB)
	if err != nil {
		t.Fatalf("retry promote: %v", err)
	}
	if n != 1 {
		t.Errorf("retry promoted = %d, want 1", n)
	}

	// Now the action_log row is synced.
	if err := liveDB.Conn().QueryRow(
		`SELECT synced_at FROM action_log WHERE id = 'al-1'`,
	).Scan(&syncedAt); err != nil {
		t.Fatalf("query synced_at post-retry: %v", err)
	}
	if !syncedAt.Valid {
		t.Errorf("synced_at still NULL after successful retry")
	}

	// And events.db has the row.
	eventsDB := openEventsDBRO(t, srv.config.ProjectDataDir, pid)
	events := readPromotedEvents(t, eventsDB)
	if len(events) != 1 {
		t.Errorf("events count = %d, want 1", len(events))
	}
}

