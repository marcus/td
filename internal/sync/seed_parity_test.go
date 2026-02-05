package sync

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestSeedSyncStatusParity verifies that after syncing a seeded database,
// both alice and bob have the same number of issues in each status.
// This test catches backfill and event application bugs that lose data.
func TestSeedSyncStatusParity(t *testing.T) {
	seedPath := os.Getenv("SEED_DB")
	if seedPath == "" {
		t.Skip("SEED_DB not set")
	}

	if _, err := os.Stat(seedPath); os.IsNotExist(err) {
		t.Fatalf("seed DB not found: %s", seedPath)
	}

	// Create temp dir for test databases
	tmpDir := t.TempDir()
	bobPath := filepath.Join(tmpDir, "bob.db")
	alicePath := filepath.Join(tmpDir, "alice.db")

	// Copy seed DB to bob
	if err := copyFile(seedPath, bobPath); err != nil {
		t.Fatalf("copy seed DB: %v", err)
	}

	// Open bob DB and reset sync state
	bobDB, err := sql.Open("sqlite3", bobPath)
	if err != nil {
		t.Fatalf("open bob: %v", err)
	}
	defer bobDB.Close()

	// Clear sync_state and reset action_log (matches e2e-sync-test.sh seed cleanup)
	for _, q := range []string{
		`DELETE FROM sync_state`,
		`DELETE FROM action_log WHERE id IS NULL OR entity_id IS NULL OR entity_id = ''`,
		`UPDATE action_log SET synced_at = NULL, server_seq = NULL`,
	} {
		if _, err := bobDB.Exec(q); err != nil {
			t.Fatalf("seed cleanup %q: %v", q, err)
		}
	}

	// Get bob's status counts before sync
	bobCounts := getStatusCounts(t, bobDB)
	t.Logf("Bob status counts: %v", bobCounts)
	totalBob := 0
	for _, c := range bobCounts {
		totalBob += c
	}
	t.Logf("Bob total issues: %d", totalBob)

	// Get pending events from bob (runs backfill)
	bobTx, err := bobDB.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	sessionID := fmt.Sprintf("test-session-%d", time.Now().Unix())
	events, err := GetPendingEvents(bobTx, sessionID, "")
	if err != nil {
		bobTx.Rollback()
		t.Fatalf("get pending events: %v", err)
	}

	if err := bobTx.Commit(); err != nil {
		t.Fatalf("commit bob tx: %v", err)
	}

	t.Logf("Got %d pending events from bob", len(events))

	// Count action types
	actionCounts := make(map[string]int)
	for _, e := range events {
		actionCounts[e.ActionType]++
	}
	t.Logf("Event action types: %v", actionCounts)

	// Create alice DB with fresh schema
	aliceDB, err := sql.Open("sqlite3", alicePath)
	if err != nil {
		t.Fatalf("open alice: %v", err)
	}
	defer aliceDB.Close()

	// Copy schema from bob to alice
	schema := extractSchema(t, bobDB)
	for _, stmt := range schema {
		if _, err := aliceDB.Exec(stmt); err != nil {
			t.Fatalf("create alice schema: %v (stmt: %s)", err, stmt)
		}
	}

	// Apply all events to alice
	aliceTx, err := aliceDB.Begin()
	if err != nil {
		t.Fatalf("begin alice tx: %v", err)
	}

	result, err := ApplyRemoteEvents(aliceTx, events, "alice-device", allowAll, nil)
	if err != nil {
		aliceTx.Rollback()
		t.Fatalf("apply events to alice: %v", err)
	}

	if err := aliceTx.Commit(); err != nil {
		t.Fatalf("commit alice tx: %v", err)
	}

	t.Logf("Applied %d events to alice (%d failed)", result.Applied, len(result.Failed))
	if len(result.Failed) > 0 {
		for i, f := range result.Failed {
			if i < 10 {
				t.Logf("  Failed seq %d: %v", f.ServerSeq, f.Error)
			}
		}
	}

	// Get alice's status counts after sync
	aliceCounts := getStatusCounts(t, aliceDB)
	t.Logf("Alice status counts: %v", aliceCounts)
	totalAlice := 0
	for _, c := range aliceCounts {
		totalAlice += c
	}
	t.Logf("Alice total issues: %d", totalAlice)

	// Compare status counts
	mismatches := []string{}
	allStatuses := make(map[string]bool)
	for s := range bobCounts {
		allStatuses[s] = true
	}
	for s := range aliceCounts {
		allStatuses[s] = true
	}

	for status := range allStatuses {
		bobCount := bobCounts[status]
		aliceCount := aliceCounts[status]
		if bobCount != aliceCount {
			mismatches = append(mismatches, fmt.Sprintf("%s: bob=%d alice=%d (diff=%d)",
				status, bobCount, aliceCount, bobCount-aliceCount))
		}
	}

	if len(mismatches) > 0 {
		t.Errorf("Status count mismatches:\n  %s", joinLines(mismatches))

		// Report missing/extra issues
		bobIssues := getIssueIDsByStatus(t, bobDB)
		aliceIssues := getIssueIDsByStatus(t, aliceDB)

		for status := range allStatuses {
			bobSet := makeSet(bobIssues[status])
			aliceSet := makeSet(aliceIssues[status])

			missing := setDiff(bobSet, aliceSet)
			extra := setDiff(aliceSet, bobSet)

			if len(missing) > 0 {
				t.Logf("Missing on alice (status=%s, count=%d): %v", status, len(missing), limitList(missing, 10))
			}
			if len(extra) > 0 {
				t.Logf("Extra on alice (status=%s, count=%d): %v", status, len(extra), limitList(extra, 10))
			}
		}
	}

	if totalBob != totalAlice {
		t.Errorf("Total issue count mismatch: bob=%d alice=%d (diff=%d)",
			totalBob, totalAlice, totalBob-totalAlice)
	}
}

func getStatusCounts(t *testing.T, db *sql.DB) map[string]int {
	rows, err := db.Query(`SELECT status, COUNT(*) FROM issues GROUP BY status`)
	if err != nil {
		t.Fatalf("query status counts: %v", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			t.Fatalf("scan status count: %v", err)
		}
		counts[status] = count
	}
	return counts
}

func getIssueIDsByStatus(t *testing.T, db *sql.DB) map[string][]string {
	rows, err := db.Query(`SELECT id, status FROM issues ORDER BY id`)
	if err != nil {
		t.Fatalf("query issues: %v", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scan issue: %v", err)
		}
		result[status] = append(result[status], id)
	}
	return result
}

func extractSchema(t *testing.T, db *sql.DB) []string {
	rows, err := db.Query(`
		SELECT sql FROM sqlite_master
		WHERE sql IS NOT NULL AND type IN ('table', 'index')
		AND name NOT LIKE 'sqlite_%'
		ORDER BY type DESC, name
	`)
	if err != nil {
		t.Fatalf("query schema: %v", err)
	}
	defer rows.Close()

	var statements []string
	for rows.Next() {
		var sql string
		if err := rows.Scan(&sql); err != nil {
			t.Fatalf("scan schema: %v", err)
		}
		statements = append(statements, sql)
	}
	return statements
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func makeSet(items []string) map[string]bool {
	set := make(map[string]bool)
	for _, item := range items {
		set[item] = true
	}
	return set
}

func setDiff(a, b map[string]bool) []string {
	var result []string
	for item := range a {
		if !b[item] {
			result = append(result, item)
		}
	}
	return result
}

func joinLines(items []string) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += "\n  "
		}
		result += item
	}
	return result
}

func limitList(items []string, max int) []string {
	if len(items) <= max {
		return items
	}
	return items[:max]
}

func allowAll(entityType string) bool {
	return true
}
