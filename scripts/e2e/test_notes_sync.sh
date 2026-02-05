#!/usr/bin/env bash
#
# Test: notes entity sync between clients
# Verifies: create note on A, push, B pulls, B updates, push, A pulls — convergence.
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"

setup

DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"

# Notes table schema (may not exist since sidecar creates it)
NOTES_SCHEMA='CREATE TABLE IF NOT EXISTS notes (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT "",
    content TEXT NOT NULL DEFAULT "",
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);'

_step "Create notes table in both clients"
sqlite3 "$DB_A" "$NOTES_SCHEMA"
sqlite3 "$DB_B" "$NOTES_SCHEMA"
_ok "Notes tables created"

# Generate a unique note ID
NOTE_ID="note-$(openssl rand -hex 4)"
NOTE_TITLE="Test Note from Alice"
NOTE_CONTENT="Initial content from client A"
ACTION_ID="al-$(openssl rand -hex 4)"
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

_step "Alice creates note directly in DB"
sqlite3 "$DB_A" <<SQL
INSERT INTO notes (id, title, content, created_at, updated_at)
VALUES ('$NOTE_ID', '$NOTE_TITLE', '$NOTE_CONTENT', '$TIMESTAMP', '$TIMESTAMP');
SQL
_ok "Note $NOTE_ID inserted"

_step "Alice creates action_log entry"
NEW_DATA=$(cat <<EOF
{"id":"$NOTE_ID","title":"$NOTE_TITLE","content":"$NOTE_CONTENT","created_at":"$TIMESTAMP","updated_at":"$TIMESTAMP"}
EOF
)
# Escape single quotes for SQL
NEW_DATA_ESCAPED=$(echo "$NEW_DATA" | sed "s/'/''/g")

sqlite3 "$DB_A" <<SQL
INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, previous_data, timestamp, undone)
VALUES ('$ACTION_ID', '$SESSION_ID_A', 'create', 'notes', '$NOTE_ID', '$NEW_DATA_ESCAPED', '', '$TIMESTAMP', 0);
SQL
_ok "action_log entry created"

_step "Alice syncs (push)"
td_a sync >/dev/null 2>&1

# Verify action was synced
SYNCED=$(sqlite3 "$DB_A" "SELECT COUNT(*) FROM action_log WHERE id = '$ACTION_ID' AND synced_at IS NOT NULL;")
assert_eq "alice's action synced" "$SYNCED" "1"

_step "Bob syncs (pull)"
td_b sync >/dev/null 2>&1

# Verify note exists in Bob's DB
BOB_NOTE=$(sqlite3 "$DB_B" "SELECT title FROM notes WHERE id = '$NOTE_ID';")
assert_eq "bob has note" "$BOB_NOTE" "$NOTE_TITLE"

BOB_CONTENT=$(sqlite3 "$DB_B" "SELECT content FROM notes WHERE id = '$NOTE_ID';")
assert_eq "bob has correct content" "$BOB_CONTENT" "$NOTE_CONTENT"
_ok "Note synced to Bob"

_step "Bob updates the note"
UPDATED_CONTENT="Updated content from client B"
UPDATED_TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
UPDATE_ACTION_ID="al-$(openssl rand -hex 4)"

sqlite3 "$DB_B" <<SQL
UPDATE notes SET content = '$UPDATED_CONTENT', updated_at = '$UPDATED_TIMESTAMP' WHERE id = '$NOTE_ID';
SQL
_ok "Note updated in Bob's DB"

_step "Bob creates action_log entry for update"
PREV_DATA=$(cat <<EOF
{"id":"$NOTE_ID","title":"$NOTE_TITLE","content":"$NOTE_CONTENT","created_at":"$TIMESTAMP","updated_at":"$TIMESTAMP"}
EOF
)
UPDATE_DATA=$(cat <<EOF
{"id":"$NOTE_ID","title":"$NOTE_TITLE","content":"$UPDATED_CONTENT","created_at":"$TIMESTAMP","updated_at":"$UPDATED_TIMESTAMP"}
EOF
)
PREV_DATA_ESCAPED=$(echo "$PREV_DATA" | sed "s/'/''/g")
UPDATE_DATA_ESCAPED=$(echo "$UPDATE_DATA" | sed "s/'/''/g")

sqlite3 "$DB_B" <<SQL
INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, previous_data, timestamp, undone)
VALUES ('$UPDATE_ACTION_ID', '$SESSION_ID_B', 'update', 'notes', '$NOTE_ID', '$UPDATE_DATA_ESCAPED', '$PREV_DATA_ESCAPED', '$UPDATED_TIMESTAMP', 0);
SQL
_ok "Update action_log entry created"

_step "Bob syncs (push)"
td_b sync >/dev/null 2>&1

BOB_UPDATE_SYNCED=$(sqlite3 "$DB_B" "SELECT COUNT(*) FROM action_log WHERE id = '$UPDATE_ACTION_ID' AND synced_at IS NOT NULL;")
assert_eq "bob's update synced" "$BOB_UPDATE_SYNCED" "1"

_step "Alice syncs (pull)"
td_a sync >/dev/null 2>&1

# Verify update in Alice's DB
ALICE_CONTENT=$(sqlite3 "$DB_A" "SELECT content FROM notes WHERE id = '$NOTE_ID';")
assert_eq "alice sees updated content" "$ALICE_CONTENT" "$UPDATED_CONTENT"
_ok "Update synced to Alice"

_step "Verify final convergence"
# Both should have identical note state
ALICE_NOTE_DATA=$(sqlite3 "$DB_A" "SELECT id || ':' || title || ':' || content FROM notes WHERE id = '$NOTE_ID';")
BOB_NOTE_DATA=$(sqlite3 "$DB_B" "SELECT id || ':' || title || ':' || content FROM notes WHERE id = '$NOTE_ID';")
assert_eq "notes match" "$ALICE_NOTE_DATA" "$BOB_NOTE_DATA"

# Verify synced event counts match
ALICE_SYNCED_EVENTS=$(sqlite3 "$DB_A" "SELECT COUNT(*) FROM action_log WHERE entity_id = '$NOTE_ID' AND server_seq IS NOT NULL;")
BOB_SYNCED_EVENTS=$(sqlite3 "$DB_B" "SELECT COUNT(*) FROM action_log WHERE entity_id = '$NOTE_ID' AND server_seq IS NOT NULL;")
assert_eq "synced event count matches" "$ALICE_SYNCED_EVENTS" "$BOB_SYNCED_EVENTS"

report
