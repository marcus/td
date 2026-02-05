#!/usr/bin/env bash
#
# Sync test using a copy of a real project database.
#
# Seeds bob with a real DB (all events unsynced), pushes everything to the
# server (exercises push batching), then verifies alice pulls it all down.
# After that, tests new mutations on top of real data in both directions.
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

# =================================================================
# Part 1: Seed bob only, push everything up, alice pulls it down
# =================================================================
_step "Seeding bob with real DB (unsynced)"
sqlite3 "$DB_SOURCE" 'PRAGMA wal_checkpoint(TRUNCATE);' 2>/dev/null || true
cp "$DB_SOURCE" "$CLIENT_B_DIR/.todos/issues.db"

_step "Running migrations on copied DB"
if ! UPGRADE_OUT=$(td_b upgrade 2>&1); then
    _fatal "Migrations failed: $UPGRADE_OUT"
fi

sqlite3 "$CLIENT_B_DIR/.todos/issues.db" <<'SQL'
DELETE FROM sync_state;
DELETE FROM action_log WHERE id IS NULL OR entity_id IS NULL OR entity_id = '';
UPDATE action_log SET synced_at = NULL, server_seq = NULL;
SQL

# Re-create session and re-link after DB replacement
td_b status >/dev/null 2>&1 || true
td_b sync-project link "$PROJECT_ID" >/dev/null

BOB_PENDING=$(sqlite3 "$CLIENT_B_DIR/.todos/issues.db" \
    "SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0" 2>/dev/null)
_ok "Seeded bob ($ISSUE_COUNT issues, $BOB_PENDING pending events)"

# Bob pushes everything (tests push batching — server limit is 1000)
_step "Bob pushes all events"
td_b sync >/dev/null 2>&1
BOB_REMAINING=$(sqlite3 "$CLIENT_B_DIR/.todos/issues.db" \
    "SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0" 2>/dev/null)
assert_eq "bob has 0 unsynced events after push" "$BOB_REMAINING" "0"

BOB_COUNT=$(td_b list --json --status all -n 10000 2>/dev/null | jq 'length')
_ok "Bob has $BOB_COUNT issues locally"

# Alice pulls everything bob pushed
_step "Alice pulls bob's data"
td_a sync >/dev/null 2>&1
ALICE_COUNT=$(td_a list --json --status all -n 10000 2>/dev/null | jq 'length')
_ok "Alice pulled $ALICE_COUNT issues"

_step "Status parity check (bob vs alice)"
BOB_STATUS_FILE="$WORKDIR/status_bob.tsv"
ALICE_STATUS_FILE="$WORKDIR/status_alice.tsv"
STATUS_DIFF_FILE="$WORKDIR/status_diff.txt"

# Per-status counts for quick visibility
BOB_STATUS_COUNTS=$(sqlite3 "$CLIENT_B_DIR/.todos/issues.db" \
    "SELECT status, COUNT(*) FROM issues WHERE deleted_at IS NULL GROUP BY status ORDER BY status;" 2>/dev/null || true)
ALICE_STATUS_COUNTS=$(sqlite3 "$CLIENT_A_DIR/.todos/issues.db" \
    "SELECT status, COUNT(*) FROM issues WHERE deleted_at IS NULL GROUP BY status ORDER BY status;" 2>/dev/null || true)
echo "Bob status counts:"
echo "$BOB_STATUS_COUNTS"
echo "Alice status counts:"
echo "$ALICE_STATUS_COUNTS"

# Full diff by id + status (trim output to keep logs readable)
sqlite3 "$CLIENT_B_DIR/.todos/issues.db" \
    "SELECT id, status FROM issues WHERE deleted_at IS NULL ORDER BY id;" > "$BOB_STATUS_FILE"
sqlite3 "$CLIENT_A_DIR/.todos/issues.db" \
    "SELECT id, status FROM issues WHERE deleted_at IS NULL ORDER BY id;" > "$ALICE_STATUS_FILE"
diff -u "$BOB_STATUS_FILE" "$ALICE_STATUS_FILE" > "$STATUS_DIFF_FILE" || true
if [ -s "$STATUS_DIFF_FILE" ]; then
    _fail "status mismatch (see $STATUS_DIFF_FILE)"
    echo "Status diff (first 200 lines):"
    head -200 "$STATUS_DIFF_FILE"
else
    _ok "status parity"
fi

# Backfill ensures orphan entities get synthetic create events.
# Expect parity — alice should have at least bob's issues.
assert_ge "alice has at least bob's issues" "$ALICE_COUNT" "$BOB_COUNT"

# =================================================================
# Part 2: New mutations on top of real data
# =================================================================

# --- Alice creates a new issue ---
_step "Alice creates issue on top of real data"
CREATE_OUT=$(td_a create "New issue on top of $ISSUE_COUNT existing" 2>&1)
ISSUE_ID=$(echo "$CREATE_OUT" | grep -oE 'td-[0-9a-f]+')
[ -n "$ISSUE_ID" ] || _fatal "Create failed: $CREATE_OUT"
_ok "Created $ISSUE_ID"

td_a sync >/dev/null 2>&1
td_b sync >/dev/null 2>&1

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

# --- Verify total counts ---
_step "Verify data integrity"
ALICE_TOTAL=$(td_a list --json --status all -n 10000 2>/dev/null | jq 'length')
BOB_TOTAL=$(td_b list --json --status all -n 10000 2>/dev/null | jq 'length')

# Both should have gained 2 issues from the new mutations
assert_ge "bob gained new issues" "$BOB_TOTAL" "$((BOB_COUNT + 2))"
assert_ge "alice gained new issues" "$ALICE_TOTAL" "$((ALICE_COUNT + 2))"

# --- Spot-check random existing issues ---
_step "Spot-checking existing issues"
SAMPLE_IDS=$(td_a list --json --status all -n 10000 2>/dev/null | jq -r '.[].id' | shuf | head -5)
for id in $SAMPLE_IDS; do
    ALICE_TITLE=$(td_a show "$id" --json 2>/dev/null | jq -r '.title')
    BOB_TITLE=$(td_b show "$id" --json 2>/dev/null | jq -r '.title')
    assert_eq "issue $id title matches" "$BOB_TITLE" "$ALICE_TITLE"
done

# --- Status transition on alice's new issue ---
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
