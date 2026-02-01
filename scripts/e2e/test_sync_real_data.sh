#!/usr/bin/env bash
#
# One-off: sync using a copy of a real project database.
#
# Copies ~/code/td/.todos/issues.db into both clients, then tests that new
# mutations on top of real data sync correctly between alice and bob.
#
# Usage:
#   bash scripts/e2e/test_sync_real_data.sh
#   bash scripts/e2e/test_sync_real_data.sh /path/to/other/issues.db
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"

DB_SOURCE="${1:-$HOME/code/td/.todos/issues.db}"

[ -f "$DB_SOURCE" ] || _fatal "DB not found: $DB_SOURCE"
DB_SIZE=$(du -h "$DB_SOURCE" | cut -f1)
ISSUE_COUNT=$(sqlite3 "$DB_SOURCE" 'SELECT COUNT(*) FROM issues' 2>/dev/null)
_step "Source: $DB_SOURCE ($DB_SIZE, $ISSUE_COUNT issues)"

setup

# --- Seed both clients with the real DB ---
_step "Copying DB into both clients"
sqlite3 "$DB_SOURCE" 'PRAGMA wal_checkpoint(TRUNCATE);' 2>/dev/null || true

for CLIENT_DIR in "$CLIENT_A_DIR" "$CLIENT_B_DIR"; do
    cp "$DB_SOURCE" "$CLIENT_DIR/.todos/issues.db"
    sqlite3 "$CLIENT_DIR/.todos/issues.db" <<'SQL'
DELETE FROM sync_state;
UPDATE action_log SET synced_at = datetime('now'), server_seq = 0 WHERE synced_at IS NULL;
SQL
done

# Create sessions and re-link to the test project
td_a status >/dev/null 2>&1 || true
td_b status >/dev/null 2>&1 || true
td_a sync-project link "$PROJECT_ID" >/dev/null
td_b sync-project link "$PROJECT_ID" >/dev/null

# Initial sync to establish baseline
td_a sync >/dev/null 2>&1
td_b sync >/dev/null 2>&1

ALICE_COUNT=$(td_a list --json --status all -n 10000 2>/dev/null | jq 'length')
BOB_COUNT=$(td_b list --json --status all -n 10000 2>/dev/null | jq 'length')
_ok "Both seeded (alice=$ALICE_COUNT, bob=$BOB_COUNT issues)"

# --- Alice creates a new issue on top of real data ---
_step "Alice creates issue on top of real data"
CREATE_OUT=$(td_a create "New issue on top of $ISSUE_COUNT existing" 2>&1)
ISSUE_ID=$(echo "$CREATE_OUT" | grep -oE 'td-[0-9a-f]+')
[ -n "$ISSUE_ID" ] || _fatal "Create failed: $CREATE_OUT"
_ok "Created $ISSUE_ID"

# Push alice's new issue
td_a sync >/dev/null 2>&1

# Pull on bob
td_b sync >/dev/null 2>&1

# --- Verify bob got the new issue ---
_step "Verify bob got alice's new issue"
BOB_NEW=$(td_b show "$ISSUE_ID" --json 2>/dev/null)
assert_json_field "bob has new issue" "$BOB_NEW" '.id' "$ISSUE_ID"
assert_contains "title matches" "$(echo "$BOB_NEW" | jq -r '.title')" "New issue on top of"

# --- Bob creates an issue ---
_step "Bob creates issue"
sleep 1
BOB_CREATE=$(td_b create "Bob's issue alongside real data" 2>&1)
BOB_ISSUE=$(echo "$BOB_CREATE" | grep -oE 'td-[0-9a-f]+')
[ -n "$BOB_ISSUE" ] || _fatal "Bob create failed: $BOB_CREATE"
_ok "Created $BOB_ISSUE"

td_b sync >/dev/null 2>&1
td_a sync >/dev/null 2>&1

ALICE_GOT=$(td_a show "$BOB_ISSUE" --json 2>/dev/null | jq -r '.id')
assert_eq "alice got bob's issue" "$ALICE_GOT" "$BOB_ISSUE"

# --- Verify existing data is intact + new issues counted ---
_step "Verify data integrity"
ALICE_TOTAL=$(td_a list --json --status all -n 10000 2>/dev/null | jq 'length')
BOB_TOTAL=$(td_b list --json --status all -n 10000 2>/dev/null | jq 'length')

EXPECTED=$((ALICE_COUNT + 2))  # baseline + alice's new + bob's new
assert_eq "alice and bob agree on count" "$ALICE_TOTAL" "$BOB_TOTAL"
assert_ge "total includes new issues" "$ALICE_TOTAL" "$EXPECTED"

# --- Spot-check random existing issues ---
_step "Spot-checking existing issues"
SAMPLE_IDS=$(td_a list --json --status all -n 10000 2>/dev/null | jq -r '.[].id' | shuf | head -5)
for id in $SAMPLE_IDS; do
    ALICE_TITLE=$(td_a show "$id" --json 2>/dev/null | jq -r '.title')
    BOB_TITLE=$(td_b show "$id" --json 2>/dev/null | jq -r '.title')
    assert_eq "issue $id title matches" "$BOB_TITLE" "$ALICE_TITLE"
done

# --- Status transition on existing issue ---
_step "Alice starts + reviews her new issue"
sleep 1
td_a start "$ISSUE_ID" >/dev/null
sleep 2
td_a review "$ISSUE_ID" >/dev/null
td_a sync >/dev/null 2>&1
td_b sync >/dev/null 2>&1

BOB_STATUS=$(td_b show "$ISSUE_ID" --json 2>/dev/null | jq -r '.status')
assert_eq "bob sees in_review" "$BOB_STATUS" "in_review"

report
