package syncharness

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/marcus/td/internal/db"
	tdsync "github.com/marcus/td/internal/sync"
)

// syncExtensionSchema creates tables and columns not in the base schema
// that are needed for sync testing (from migrations v2, v6, v7, v9, v10, v11, v16, v17).
const syncExtensionSchema = `
-- action_log (migration v2) with sync columns (migration v16)
CREATE TABLE IF NOT EXISTS action_log (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    action_type TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    previous_data TEXT DEFAULT '',
    new_data TEXT DEFAULT '',
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    undone INTEGER DEFAULT 0,
    synced_at DATETIME,
    server_seq INTEGER
);
CREATE INDEX IF NOT EXISTS idx_action_log_session ON action_log(session_id);
CREATE INDEX IF NOT EXISTS idx_action_log_timestamp ON action_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_action_log_entity_type ON action_log(entity_id, action_type);

-- boards (migration v9/v10/v11)
CREATE TABLE IF NOT EXISTS boards (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL COLLATE NOCASE,
    last_viewed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    query TEXT NOT NULL DEFAULT '',
    is_builtin INTEGER NOT NULL DEFAULT 0,
    view_mode TEXT NOT NULL DEFAULT 'swimlanes'
);

-- board_issue_positions (migration v9, renamed in v10, soft delete v25)
CREATE TABLE IF NOT EXISTS board_issue_positions (
    id TEXT PRIMARY KEY,
    board_id TEXT NOT NULL,
    issue_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME,
    UNIQUE(board_id, issue_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_board_positions_position
    ON board_issue_positions(board_id, position);

-- issue_session_history (migration v7)
CREATE TABLE IF NOT EXISTS issue_session_history (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    action TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);
CREATE INDEX IF NOT EXISTS idx_ish_issue ON issue_session_history(issue_id);
CREATE INDEX IF NOT EXISTS idx_ish_session ON issue_session_history(session_id);

-- sync_state (migration v16)
CREATE TABLE IF NOT EXISTS sync_state (
    project_id TEXT PRIMARY KEY,
    last_pushed_action_id INTEGER DEFAULT 0,
    last_pulled_server_seq INTEGER DEFAULT 0,
    last_sync_at DATETIME,
    sync_disabled INTEGER DEFAULT 0
);

-- sync_conflicts (migration v17)
CREATE TABLE IF NOT EXISTS sync_conflicts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    server_seq INTEGER NOT NULL,
    local_data JSON,
    remote_data JSON,
    overwritten_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sync_conflicts_entity ON sync_conflicts(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_sync_conflicts_time ON sync_conflicts(overwritten_at);
CREATE INDEX IF NOT EXISTS idx_sync_conflicts_seq ON sync_conflicts(server_seq);
`

// migrationColumns are ALTER TABLE statements for columns added by migrations
// that aren't in the base schema (v6: creator_session, v10: sprint).
var migrationColumns = []string{
	"ALTER TABLE issues ADD COLUMN creator_session TEXT DEFAULT ''",
	"ALTER TABLE issues ADD COLUMN sprint TEXT DEFAULT ''",
}

// entityTables lists the tables that hold user data (not action_log or sync_state).
var entityTables = []string{"issues", "logs", "handoffs", "comments", "boards", "work_sessions", "board_issue_positions", "issue_dependencies", "issue_files", "work_session_issues"}

// validEntities is the set of entity types accepted by the validator.
var validEntities = map[string]bool{
	"issues":                true,
	"logs":                  true,
	"handoffs":              true,
	"comments":              true,
	"boards":                true,
	"work_sessions":         true,
	"board_issue_positions": true,
	"issue_dependencies":    true,
	"issue_files":           true,
	"work_session_issues":   true,
}

// SimulatedClient represents a single sync client with its own database.
type SimulatedClient struct {
	DeviceID         string
	SessionID        string
	DB               *sql.DB
	LastPushedAction int64
	LastPulledSeq    int64
}

// Harness orchestrates multi-client sync testing.
type Harness struct {
	t          *testing.T
	ProjectDBs map[string]*sql.DB
	Clients    map[string]*SimulatedClient
	clientKeys []string
	Validator  tdsync.EntityValidator
	actionSeq  atomic.Int64
	serverMu   sync.Mutex // serializes server DB writes (SQLite single-writer)
}

// NewHarness creates a test harness with numClients and one server DB for projectID.
func NewHarness(t *testing.T, numClients int, projectID string) *Harness {
	t.Helper()

	h := &Harness{
		t:          t,
		ProjectDBs: make(map[string]*sql.DB),
		Clients:    make(map[string]*SimulatedClient),
		Validator:  func(entityType string) bool { return validEntities[entityType] },
	}

	// Create server DB with shared cache + WAL mode for concurrent access
	serverDB, err := sql.Open("sqlite3", "file::memory:?mode=memory&cache=shared&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open server db: %v", err)
	}
	if err := tdsync.InitServerEventLog(serverDB); err != nil {
		t.Fatalf("init server event log: %v", err)
	}
	h.ProjectDBs[projectID] = serverDB
	t.Cleanup(func() { serverDB.Close() })

	// Create clients
	for i := 0; i < numClients; i++ {
		letter := string(rune('A' + i))
		clientID := "client-" + letter
		deviceID := fmt.Sprintf("device-%s-0000-0000-0000-%012d", letter, i+1)
		sessionID := fmt.Sprintf("session-%s-%04d", letter, i+1)

		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatalf("open client %s db: %v", clientID, err)
		}
		if err := initClientSchema(db); err != nil {
			t.Fatalf("create schema client %s: %v", clientID, err)
		}
		t.Cleanup(func() { db.Close() })

		h.Clients[clientID] = &SimulatedClient{
			DeviceID:  deviceID,
			SessionID: sessionID,
			DB:        db,
		}
		h.clientKeys = append(h.clientKeys, clientID)
	}

	return h
}

// initClientSchema sets up a client database with the real td schema plus sync extensions.
func initClientSchema(clientDB *sql.DB) error {
	// Base schema from internal/db
	if _, err := clientDB.Exec(db.BaseSchema()); err != nil {
		return fmt.Errorf("base schema: %w", err)
	}
	// Migration columns not in base schema
	for _, stmt := range migrationColumns {
		if _, err := clientDB.Exec(stmt); err != nil {
			return fmt.Errorf("migration column %q: %w", stmt, err)
		}
	}
	// Sync-specific tables and columns
	if _, err := clientDB.Exec(syncExtensionSchema); err != nil {
		return fmt.Errorf("sync extension schema: %w", err)
	}
	return nil
}

// Mutate performs a local mutation on a client's database and records it in action_log.
// For "delete" action: uses soft-delete on tables with deleted_at column (issues, board_issue_positions),
// hard delete on tables without it (issue_dependencies, issue_files, work_session_issues).
func (h *Harness) Mutate(clientID, actionType, entityType, entityID string, data map[string]any) error {
	c, ok := h.Clients[clientID]
	if !ok {
		return fmt.Errorf("unknown client: %s", clientID)
	}

	tx, err := c.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Read previous data
	prevData := readEntity(tx, entityType, entityID)

	// Determine effective action type for local mutation
	effectiveAction := actionType
	if actionType == "delete" && softDeleteTables[entityType] {
		// For tables with deleted_at, "delete" becomes soft_delete to match sync behavior
		effectiveAction = "soft_delete"
	}

	switch effectiveAction {
	case "create", "update":
		if err := upsertLocal(tx, entityType, entityID, data); err != nil {
			return fmt.Errorf("upsert: %w", err)
		}
	case "delete":
		// Hard delete for tables without deleted_at column
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE id = ?", entityType), entityID); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
	case "soft_delete":
		// Soft delete for tables with deleted_at column
		if _, err := tx.Exec(fmt.Sprintf("UPDATE %s SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?", entityType), entityID); err != nil {
			return fmt.Errorf("soft_delete: %w", err)
		}
	default:
		return fmt.Errorf("unknown action type: %s", actionType)
	}

	// Build JSON strings — new_data is the full row snapshot after the mutation
	// (mirrors real td behavior where new_data = full entity state after action)
	prevJSON, _ := json.Marshal(prevData)
	var newJSON []byte
	if actionType == "create" || actionType == "update" {
		postData := readEntity(tx, entityType, entityID)
		if postData != nil {
			newJSON, _ = json.Marshal(postData)
		} else if data != nil {
			newJSON, _ = json.Marshal(data)
		} else {
			newJSON = []byte("{}")
		}
	} else if data != nil {
		newJSON, _ = json.Marshal(data)
	} else {
		newJSON = []byte("{}")
	}

	seq := h.actionSeq.Add(1)
	alID := fmt.Sprintf("al-%08d", seq)

	// Determine the action type to log. For tables without deleted_at, map "delete"
	// to the specific action type that sync engine treats as hard delete.
	logActionType := actionType
	if actionType == "delete" && !softDeleteTables[entityType] {
		if hardAction, ok := hardDeleteActionTypes[entityType]; ok {
			logActionType = hardAction
		}
	}

	_, err = tx.Exec(
		`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, previous_data, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		alID, c.SessionID, logActionType, entityType, entityID, string(newJSON), string(prevJSON),
	)
	if err != nil {
		return fmt.Errorf("insert action_log: %w", err)
	}

	return tx.Commit()
}

// readEntity reads all columns for a given entity, returning a map.
func readEntity(tx *sql.Tx, entityType, entityID string) map[string]any {
	return readEntityFiltered(tx, entityType, entityID, false)
}

// readEntityFiltered reads all columns for a given entity, optionally filtering out soft-deleted rows.
func readEntityFiltered(tx *sql.Tx, entityType, entityID string, filterSoftDeleted bool) map[string]any {
	query := fmt.Sprintf("SELECT * FROM %s WHERE id = ?", entityType)
	if filterSoftDeleted {
		query = fmt.Sprintf("SELECT * FROM %s WHERE id = ? AND deleted_at IS NULL", entityType)
	}
	row, err := tx.Query(query, entityID)
	if err != nil {
		return nil
	}
	defer row.Close()

	if !row.Next() {
		return nil
	}

	cols, err := row.Columns()
	if err != nil {
		return nil
	}

	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := row.Scan(ptrs...); err != nil {
		return nil
	}

	result := make(map[string]any, len(cols))
	for i, col := range cols {
		result[col] = vals[i]
	}
	return result
}

// upsertLocal inserts or replaces a row in the entity table.
func upsertLocal(tx *sql.Tx, entityType, entityID string, data map[string]any) error {
	fields := make(map[string]any, len(data)+1)
	for k, v := range data {
		fields[k] = v
	}
	fields["id"] = entityID

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	placeholders := make([]string, len(keys))
	vals := make([]any, len(keys))
	for i, k := range keys {
		placeholders[i] = "?"
		vals[i] = fields[k]
	}

	query := fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)",
		entityType, strings.Join(keys, ", "), strings.Join(placeholders, ", "))

	_, err := tx.Exec(query, vals...)
	return err
}

// Push sends pending events from a client to the server.
func (h *Harness) Push(clientID, projectID string) (tdsync.PushResult, error) {
	c, ok := h.Clients[clientID]
	if !ok {
		return tdsync.PushResult{}, fmt.Errorf("unknown client: %s", clientID)
	}
	serverDB, ok := h.ProjectDBs[projectID]
	if !ok {
		return tdsync.PushResult{}, fmt.Errorf("unknown project: %s", projectID)
	}

	// Read pending events from client
	clientTx, err := c.DB.Begin()
	if err != nil {
		return tdsync.PushResult{}, fmt.Errorf("begin client tx: %w", err)
	}

	events, err := tdsync.GetPendingEvents(clientTx, c.DeviceID, c.SessionID)
	if err != nil {
		clientTx.Rollback()
		return tdsync.PushResult{}, fmt.Errorf("get pending: %w", err)
	}

	if len(events) == 0 {
		clientTx.Rollback()
		return tdsync.PushResult{}, nil
	}

	// Insert into server (mutex serializes concurrent writers for SQLite)
	h.serverMu.Lock()
	serverTx, err := serverDB.Begin()
	if err != nil {
		h.serverMu.Unlock()
		clientTx.Rollback()
		return tdsync.PushResult{}, fmt.Errorf("begin server tx: %w", err)
	}

	result, err := tdsync.InsertServerEvents(serverTx, events)
	if err != nil {
		serverTx.Rollback()
		h.serverMu.Unlock()
		clientTx.Rollback()
		return tdsync.PushResult{}, fmt.Errorf("insert server events: %w", err)
	}

	if err := serverTx.Commit(); err != nil {
		h.serverMu.Unlock()
		clientTx.Rollback()
		return tdsync.PushResult{}, fmt.Errorf("commit server tx: %w", err)
	}
	h.serverMu.Unlock()

	// Mark synced on client
	if err := tdsync.MarkEventsSynced(clientTx, result.Acks); err != nil {
		clientTx.Rollback()
		return tdsync.PushResult{}, fmt.Errorf("mark synced: %w", err)
	}

	if err := clientTx.Commit(); err != nil {
		return tdsync.PushResult{}, fmt.Errorf("commit client tx: %w", err)
	}

	// Update last pushed action
	if len(result.Acks) > 0 {
		c.LastPushedAction = result.Acks[len(result.Acks)-1].ClientActionID
	}

	return result, nil
}

// Pull fetches new events from the server and applies them to the client.
func (h *Harness) Pull(clientID, projectID string) (tdsync.PullResult, error) {
	c, ok := h.Clients[clientID]
	if !ok {
		return tdsync.PullResult{}, fmt.Errorf("unknown client: %s", clientID)
	}
	serverDB, ok := h.ProjectDBs[projectID]
	if !ok {
		return tdsync.PullResult{}, fmt.Errorf("unknown project: %s", projectID)
	}

	// Get events from server
	serverTx, err := serverDB.Begin()
	if err != nil {
		return tdsync.PullResult{}, fmt.Errorf("begin server tx: %w", err)
	}

	pullResult, err := tdsync.GetEventsSince(serverTx, c.LastPulledSeq, 10000, c.DeviceID)
	if err != nil {
		serverTx.Rollback()
		return tdsync.PullResult{}, fmt.Errorf("get events since: %w", err)
	}

	if err := serverTx.Commit(); err != nil {
		return tdsync.PullResult{}, fmt.Errorf("commit server tx: %w", err)
	}

	if len(pullResult.Events) == 0 {
		return pullResult, nil
	}

	// Apply to client
	clientTx, err := c.DB.Begin()
	if err != nil {
		return tdsync.PullResult{}, fmt.Errorf("begin client tx: %w", err)
	}

	applyResult, err := tdsync.ApplyRemoteEvents(clientTx, pullResult.Events, c.DeviceID, h.Validator, nil)
	if err != nil {
		clientTx.Rollback()
		return tdsync.PullResult{}, fmt.Errorf("apply remote events: %w", err)
	}

	if err := clientTx.Commit(); err != nil {
		return tdsync.PullResult{}, fmt.Errorf("commit client tx: %w", err)
	}

	// Update last pulled seq
	if applyResult.LastAppliedSeq > c.LastPulledSeq {
		c.LastPulledSeq = applyResult.LastAppliedSeq
	}
	if pullResult.LastServerSeq > c.LastPulledSeq {
		c.LastPulledSeq = pullResult.LastServerSeq
	}

	return pullResult, nil
}

// Sync pushes then pulls for a client.
func (h *Harness) Sync(clientID, projectID string) error {
	if _, err := h.Push(clientID, projectID); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	if _, err := h.Pull(clientID, projectID); err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	return nil
}

// AssertConverged verifies all clients have identical entity data.
func (h *Harness) AssertConverged(projectID string) {
	h.t.Helper()

	if len(h.clientKeys) < 2 {
		return
	}

	for _, table := range entityTables {
		var refRows string
		var refClient string
		for i, clientID := range h.clientKeys {
			rows := dumpTable(h.Clients[clientID].DB, table)
			if i == 0 {
				refRows = rows
				refClient = clientID
				continue
			}
			if rows != refRows {
				h.t.Fatalf("DIVERGENCE in table %q between %s and %s:\n--- %s ---\n%s\n--- %s ---\n%s",
					table, refClient, clientID, refClient, refRows, clientID, rows)
			}
		}
	}
}

// Diff returns a human-readable diff of entity tables between two clients.
func (h *Harness) Diff(clientA, clientB string) string {
	cA, okA := h.Clients[clientA]
	cB, okB := h.Clients[clientB]
	if !okA || !okB {
		return fmt.Sprintf("unknown client(s): %s, %s", clientA, clientB)
	}

	var sb strings.Builder
	for _, table := range entityTables {
		rowsA := dumpTable(cA.DB, table)
		rowsB := dumpTable(cB.DB, table)
		if rowsA != rowsB {
			sb.WriteString(fmt.Sprintf("=== %s ===\n", table))
			sb.WriteString(fmt.Sprintf("--- %s ---\n%s\n", clientA, rowsA))
			sb.WriteString(fmt.Sprintf("--- %s ---\n%s\n", clientB, rowsB))
		}
	}
	if sb.Len() == 0 {
		return "(identical)"
	}
	return sb.String()
}

// timestampCols are excluded from convergence checks because INSERT OR REPLACE
// sets DEFAULT CURRENT_TIMESTAMP independently on each client.
var timestampCols = map[string]bool{
	"created_at": true, "updated_at": true, "closed_at": true,
	"deleted_at": true, "timestamp": true, "started_at": true,
	"ended_at": true, "last_viewed_at": true, "linked_at": true,
	"tagged_at": true, "added_at": true,
}

// softDeleteTables lists tables that use soft-delete via deleted_at column.
var softDeleteTables = map[string]bool{
	"issues":                true,
	"board_issue_positions": true,
}

// hardDeleteActionTypes maps entity types without deleted_at to action types
// that mapActionType() converts to hard "delete" (not "soft_delete").
// Without this, "delete" action on these tables would become "soft_delete" and fail.
var hardDeleteActionTypes = map[string]string{
	"issue_dependencies":    "remove_dependency",
	"issue_files":           "unlink_file",
	"work_session_issues":   "work_session_untag",
	"boards":                "board_delete",
}

// dumpTable returns a deterministic string representation of all rows in a table.
// Timestamp columns are excluded from the dump to avoid false divergence.
// Soft-deleted rows are filtered out for tables that support soft-delete.
func dumpTable(db *sql.DB, table string) string {
	query := fmt.Sprintf("SELECT * FROM %s", table)
	if softDeleteTables[table] {
		query += " WHERE deleted_at IS NULL"
	}
	query += " ORDER BY id"
	rows, err := db.Query(query)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err)
	}

	var sb strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			sb.WriteString(fmt.Sprintf("SCAN ERROR: %v\n", err))
			continue
		}

		var parts []string
		for i, col := range cols {
			if timestampCols[col] {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%v", col, vals[i]))
		}
		sb.WriteString(strings.Join(parts, " | "))
		sb.WriteString("\n")
	}
	return sb.String()
}

// QueryEntity reads a single entity from a client's DB, returning nil if not found.
// For soft-delete tables (issues, board_issue_positions), filters out rows where deleted_at is set.
func (h *Harness) QueryEntity(clientID, entityType, entityID string) map[string]any {
	h.t.Helper()
	c, ok := h.Clients[clientID]
	if !ok {
		h.t.Fatalf("unknown client: %s", clientID)
	}

	tx, err := c.DB.Begin()
	if err != nil {
		h.t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	return readEntityFiltered(tx, entityType, entityID, softDeleteTables[entityType])
}

// QueryEntityRaw reads a single entity from a client's DB without soft-delete filtering.
// Use this when you need to verify soft-deleted rows exist (e.g., checking deleted_at is set).
func (h *Harness) QueryEntityRaw(clientID, entityType, entityID string) map[string]any {
	h.t.Helper()
	c, ok := h.Clients[clientID]
	if !ok {
		h.t.Fatalf("unknown client: %s", clientID)
	}

	tx, err := c.DB.Begin()
	if err != nil {
		h.t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	return readEntityFiltered(tx, entityType, entityID, false)
}

// CountEntities returns the number of rows in an entity table for a client.
func (h *Harness) CountEntities(clientID, entityType string) int {
	h.t.Helper()
	c, ok := h.Clients[clientID]
	if !ok {
		h.t.Fatalf("unknown client: %s", clientID)
	}

	var count int
	err := c.DB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", entityType)).Scan(&count)
	if err != nil {
		h.t.Fatalf("count %s: %v", entityType, err)
	}
	return count
}

// PushWithoutMark sends pending events to the server but skips MarkEventsSynced.
// This simulates a crash after the server accepts events but before the client records the acks.
func (h *Harness) PushWithoutMark(clientID, projectID string) (tdsync.PushResult, error) {
	c, ok := h.Clients[clientID]
	if !ok {
		return tdsync.PushResult{}, fmt.Errorf("unknown client: %s", clientID)
	}
	serverDB, ok := h.ProjectDBs[projectID]
	if !ok {
		return tdsync.PushResult{}, fmt.Errorf("unknown project: %s", projectID)
	}

	clientTx, err := c.DB.Begin()
	if err != nil {
		return tdsync.PushResult{}, fmt.Errorf("begin client tx: %w", err)
	}

	events, err := tdsync.GetPendingEvents(clientTx, c.DeviceID, c.SessionID)
	if err != nil {
		clientTx.Rollback()
		return tdsync.PushResult{}, fmt.Errorf("get pending: %w", err)
	}
	clientTx.Rollback() // read-only, don't mark anything

	if len(events) == 0 {
		return tdsync.PushResult{}, nil
	}

	h.serverMu.Lock()
	serverTx, err := serverDB.Begin()
	if err != nil {
		h.serverMu.Unlock()
		return tdsync.PushResult{}, fmt.Errorf("begin server tx: %w", err)
	}

	result, err := tdsync.InsertServerEvents(serverTx, events)
	if err != nil {
		serverTx.Rollback()
		h.serverMu.Unlock()
		return tdsync.PushResult{}, fmt.Errorf("insert server events: %w", err)
	}

	if err := serverTx.Commit(); err != nil {
		h.serverMu.Unlock()
		return tdsync.PushResult{}, fmt.Errorf("commit server tx: %w", err)
	}
	h.serverMu.Unlock()

	return result, nil
}

// PullAll fetches all new events from the server (including own device) and applies them.
// This ensures convergence by replaying events in server-seq order regardless of origin.
func (h *Harness) PullAll(clientID, projectID string) (tdsync.PullResult, error) {
	c, ok := h.Clients[clientID]
	if !ok {
		return tdsync.PullResult{}, fmt.Errorf("unknown client: %s", clientID)
	}
	serverDB, ok := h.ProjectDBs[projectID]
	if !ok {
		return tdsync.PullResult{}, fmt.Errorf("unknown project: %s", projectID)
	}

	serverTx, err := serverDB.Begin()
	if err != nil {
		return tdsync.PullResult{}, fmt.Errorf("begin server tx: %w", err)
	}

	// Empty excludeDevice = get all events including own
	pullResult, err := tdsync.GetEventsSince(serverTx, c.LastPulledSeq, 10000, "")
	if err != nil {
		serverTx.Rollback()
		return tdsync.PullResult{}, fmt.Errorf("get events since: %w", err)
	}

	if err := serverTx.Commit(); err != nil {
		return tdsync.PullResult{}, fmt.Errorf("commit server tx: %w", err)
	}

	if len(pullResult.Events) == 0 {
		return pullResult, nil
	}

	clientTx, err := c.DB.Begin()
	if err != nil {
		return tdsync.PullResult{}, fmt.Errorf("begin client tx: %w", err)
	}

	applyResult, err := tdsync.ApplyRemoteEvents(clientTx, pullResult.Events, c.DeviceID, h.Validator, nil)
	if err != nil {
		clientTx.Rollback()
		return tdsync.PullResult{}, fmt.Errorf("apply remote events: %w", err)
	}

	if err := clientTx.Commit(); err != nil {
		return tdsync.PullResult{}, fmt.Errorf("commit client tx: %w", err)
	}

	if applyResult.LastAppliedSeq > c.LastPulledSeq {
		c.LastPulledSeq = applyResult.LastAppliedSeq
	}
	if pullResult.LastServerSeq > c.LastPulledSeq {
		c.LastPulledSeq = pullResult.LastServerSeq
	}

	return pullResult, nil
}

// UndoLastAction simulates `td undo` for the last non-undone action on a client.
// It marks the action as undone=1 and inserts a compensating event:
//   - "create" action → soft_delete event
//   - "soft_delete" action → restore event (clears deleted_at)
//   - "update" action → update event with previous_data fields
//
// This mirrors the behavior of cmd/undo.go.
func (h *Harness) UndoLastAction(clientID string) error {
	c, ok := h.Clients[clientID]
	if !ok {
		return fmt.Errorf("unknown client: %s", clientID)
	}

	tx, err := c.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Find the last non-undone action (excluding backfill-generated entries)
	// Harness uses IDs like "al-00000001" (8 decimal digits)
	// Backfill uses IDs like "al-a1b2c3d4" (8 hex chars, may contain letters)
	// We filter by checking that the ID matches the harness pattern.
	var (
		actionID    string
		actionType  string
		entityType  string
		entityID    string
		prevDataStr sql.NullString
		newDataStr  sql.NullString
	)
	err = tx.QueryRow(`
		SELECT id, action_type, entity_type, entity_id, previous_data, new_data
		FROM action_log
		WHERE session_id = ? AND undone = 0
		  AND id GLOB 'al-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]'
		ORDER BY rowid DESC
		LIMIT 1
	`, c.SessionID).Scan(&actionID, &actionType, &entityType, &entityID, &prevDataStr, &newDataStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("no actions to undo for session %s", c.SessionID)
		}
		return fmt.Errorf("query last action: %w", err)
	}

	// Mark the action as undone
	_, err = tx.Exec(`UPDATE action_log SET undone = 1 WHERE id = ?`, actionID)
	if err != nil {
		return fmt.Errorf("mark undone: %w", err)
	}

	// Insert compensating event based on action type
	seq := h.actionSeq.Add(1)
	alID := fmt.Sprintf("al-%08d", seq)

	var compensatingAction string
	var compensatingNewData, compensatingPrevData []byte

	// For issues, use the mapped action type from mapActionType
	mappedAction := mapHarnessActionType(actionType)

	switch mappedAction {
	case "create":
		// Undo create → soft_delete
		compensatingAction = "soft_delete"
		// Soft-delete the entity locally
		if softDeleteTables[entityType] {
			_, err = tx.Exec(fmt.Sprintf("UPDATE %s SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?", entityType), entityID)
			if err != nil {
				return fmt.Errorf("soft_delete entity: %w", err)
			}
		}
		compensatingNewData = []byte("{}")
		if newDataStr.Valid && newDataStr.String != "" {
			compensatingPrevData = []byte(newDataStr.String)
		} else {
			compensatingPrevData = []byte("{}")
		}

	case "soft_delete":
		// Undo soft_delete → restore
		compensatingAction = "restore"
		// Restore the entity locally (clear deleted_at)
		if softDeleteTables[entityType] {
			_, err = tx.Exec(fmt.Sprintf("UPDATE %s SET deleted_at = NULL WHERE id = ?", entityType), entityID)
			if err != nil {
				return fmt.Errorf("restore entity: %w", err)
			}
		}
		// Read current entity state after restore
		restoredData := readEntity(tx, entityType, entityID)
		if restoredData != nil {
			compensatingNewData, _ = json.Marshal(restoredData)
		} else {
			compensatingNewData = []byte("{}")
		}
		if prevDataStr.Valid && prevDataStr.String != "" {
			compensatingPrevData = []byte(prevDataStr.String)
		} else {
			compensatingPrevData = []byte("{}")
		}

	case "update":
		// Undo update → update with previous_data
		compensatingAction = "update"
		// Restore previous state locally
		if prevDataStr.Valid && prevDataStr.String != "" {
			var prevFields map[string]any
			if err := json.Unmarshal([]byte(prevDataStr.String), &prevFields); err != nil {
				return fmt.Errorf("unmarshal previous_data: %w", err)
			}
			// Update entity with previous values
			if err := upsertLocal(tx, entityType, entityID, prevFields); err != nil {
				return fmt.Errorf("restore previous state: %w", err)
			}
			compensatingNewData = []byte(prevDataStr.String)
		} else {
			compensatingNewData = []byte("{}")
		}
		if newDataStr.Valid && newDataStr.String != "" {
			compensatingPrevData = []byte(newDataStr.String)
		} else {
			compensatingPrevData = []byte("{}")
		}

	default:
		return fmt.Errorf("cannot undo action type: %s (mapped: %s)", actionType, mappedAction)
	}

	// Insert compensating action into action_log
	_, err = tx.Exec(
		`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, previous_data, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		alID, c.SessionID, compensatingAction, entityType, entityID,
		string(compensatingNewData), string(compensatingPrevData),
	)
	if err != nil {
		return fmt.Errorf("insert compensating action: %w", err)
	}

	return tx.Commit()
}

// mapHarnessActionType converts action types to their sync-equivalent categories.
// This mirrors the logic in internal/sync/client.go mapActionType.
func mapHarnessActionType(actionType string) string {
	switch actionType {
	case "create", "handoff", "add_dependency", "link_file", "board_create", "board_update", "board_add_issue", "board_set_position", "work_session_tag":
		return "create"
	case "remove_dependency", "unlink_file", "board_delete", "work_session_untag":
		return "delete"
	case "delete", "board_unposition", "board_remove_issue", "soft_delete":
		return "soft_delete"
	case "restore":
		return "restore"
	default:
		return "update"
	}
}
