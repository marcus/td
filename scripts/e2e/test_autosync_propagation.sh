#!/usr/bin/env bash
#
# Test: auto-sync propagates issue creation + status update
#
# Alice has auto-sync on (1s debounce, 3s interval).
# She creates an issue, then starts it (status → in_progress).
# Bob polls until both the issue and the status change appear.
# Verifies: issue arrives with correct data, status update propagates.
#
# Uses `td start` to test status update propagation via auto-sync.
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"

setup --auto-sync --debounce "1s" --interval "3s"

# --- Alice creates an issue ---
# Auto-sync fires synchronously in PersistentPostRun, so the push
# completes before td exits.
_step "Alice creates issue"
CREATE_OUT=$(td_a create "Auto-synced issue from alice")
ISSUE_ID=$(echo "$CREATE_OUT" | grep -oE 'td-[0-9a-f]+')
[ -n "$ISSUE_ID" ] || _fatal "Could not extract issue ID from: $CREATE_OUT"
_ok "Created $ISSUE_ID"

# --- Wait for debounce to clear, then start the issue ---
sleep 2
_step "Alice starts issue (status → in_progress)"
td_a start "$ISSUE_ID" >/dev/null
_ok "Started $ISSUE_ID"

# --- Bob polls until issue appears with in_progress status ---
_step "Bob polling for issue + status update"

TIMEOUT=20
POLL_INTERVAL=2
elapsed=0
BOB_ISSUE_JSON=""

while [ "$elapsed" -lt "$TIMEOUT" ]; do
    td_b sync >/dev/null 2>&1

    BOB_LIST=$(td_b list --json --status all 2>/dev/null)
    BOB_ISSUE_JSON=$(echo "$BOB_LIST" | jq --arg t "Auto-synced issue from alice" \
        '.[] | select(.title == $t)' 2>/dev/null)

    if [ -n "$BOB_ISSUE_JSON" ]; then
        BOB_STATUS=$(echo "$BOB_ISSUE_JSON" | jq -r '.status')
        if [ "$BOB_STATUS" = "in_progress" ]; then
            _ok "Issue with in_progress status appeared after ~${elapsed}s"
            break
        fi
    fi

    sleep "$POLL_INTERVAL"
    elapsed=$((elapsed + POLL_INTERVAL))
done

# --- Assertions ---
_step "Verifying issue on bob"
if [ -z "$BOB_ISSUE_JSON" ]; then
    _fail "issue never appeared on bob's side (waited ${TIMEOUT}s)"
    report
fi

assert_json_field "title" "$BOB_ISSUE_JSON" '.title' "Auto-synced issue from alice"
assert_json_field "status is in_progress" "$BOB_ISSUE_JSON" '.status' "in_progress"

# --- Verify the full entity has expected fields ---
_step "Verifying issue completeness"
BOB_ISSUE_ID=$(echo "$BOB_ISSUE_JSON" | jq -r '.id')
assert_eq "issue ID matches" "$BOB_ISSUE_ID" "$ISSUE_ID"

BOB_CREATED=$(echo "$BOB_ISSUE_JSON" | jq -r '.created_at')
assert_contains "has created_at" "$BOB_CREATED" "20"  # starts with year

report
