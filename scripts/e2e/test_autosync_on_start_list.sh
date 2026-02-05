#!/usr/bin/env bash
#
# Repro: on_start sync debounces away post-mutation push.
#
# Part 1: Alice creates issue. Bob gets it + sees the creation log.
# Part 2: Alice marks it reviewable. Bob sees in_review status (not reverted).
#         Both see the transition log.
#
# Uses on_start=true to match: bash scripts/e2e-sync-test.sh --manual --auto-sync
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"

setup --auto-sync --debounce "1s" --interval "3s"

# Patch on_start=true to match manual --auto-sync mode
for cfg in "$HOME_A/.config/td/config.json" "$HOME_B/.config/td/config.json"; do
    tmp=$(jq '.sync.auto.on_start = true' "$cfg")
    echo "$tmp" > "$cfg"
done
_ok "Patched on_start=true"

# ===================================================================
# Part 1: Alice creates issue, bob gets it + creation log
# ===================================================================
_step "Alice creates issue"
CREATE_OUT=$(td_a create "Sync test issue")
ISSUE_ID=$(echo "$CREATE_OUT" | grep -oE 'td-[0-9a-f]+')
[ -n "$ISSUE_ID" ] || _fatal "No issue ID from: $CREATE_OUT"
_ok "Created $ISSUE_ID"

# Poll until bob sees the issue
_step "Bob polling for issue"
TIMEOUT=15
elapsed=0
while [ "$elapsed" -lt "$TIMEOUT" ]; do
    td_b sync >/dev/null 2>&1
    BOB_COUNT=$(td_b list --json 2>/dev/null | jq 'length')
    if [ "$BOB_COUNT" -ge 1 ]; then
        _ok "Issue appeared after ~${elapsed}s"
        break
    fi
    sleep 2
    elapsed=$((elapsed + 2))
done

BOB_LIST=$(td_b list --json 2>/dev/null)
assert_eq "bob sees 1 issue" "$(echo "$BOB_LIST" | jq 'length')" "1"
assert_json_field "title" "$BOB_LIST" '.[0].title' "Sync test issue"
assert_json_field "status is open" "$BOB_LIST" '.[0].status' "open"

# Verify bob has issue with correct id
_step "Bob checks issue details"
BOB_SHOW=$(td_b show "$ISSUE_ID" --json 2>/dev/null)
assert_json_field "bob has correct id" "$BOB_SHOW" '.id' "$ISSUE_ID"

# ===================================================================
# Part 2: Alice submits for review, bob sees the change, not reverted
# ===================================================================
sleep 2  # clear debounce

_step "Alice submits for review"
td_a start "$ISSUE_ID" >/dev/null
sleep 2  # clear debounce
td_a review "$ISSUE_ID" >/dev/null
_ok "Submitted $ISSUE_ID for review"

# Poll until bob sees in_review
_step "Bob polling for in_review status"
elapsed=0
BOB_STATUS=""
while [ "$elapsed" -lt "$TIMEOUT" ]; do
    td_b sync >/dev/null 2>&1
    BOB_STATUS=$(td_b list --json --status all 2>/dev/null | jq -r --arg id "$ISSUE_ID" '.[] | select(.id == $id) | .status')
    if [ "$BOB_STATUS" = "in_review" ]; then
        _ok "in_review appeared after ~${elapsed}s"
        break
    fi
    sleep 2
    elapsed=$((elapsed + 2))
done

assert_eq "bob sees in_review" "$BOB_STATUS" "in_review"

# Verify not reverted: sync again and re-check
_step "Verify not reverted"
td_b sync >/dev/null 2>&1
sleep 2
td_b sync >/dev/null 2>&1
BOB_STATUS2=$(td_b list --json --status all 2>/dev/null | jq -r --arg id "$ISSUE_ID" '.[] | select(.id == $id) | .status')
assert_eq "still in_review after extra syncs" "$BOB_STATUS2" "in_review"

# Also confirm alice still sees in_review
td_a sync >/dev/null 2>&1
ALICE_STATUS=$(td_a list --json --status all 2>/dev/null | jq -r --arg id "$ISSUE_ID" '.[] | select(.id == $id) | .status')
assert_eq "alice still in_review" "$ALICE_STATUS" "in_review"

# Check both see the review transition log
_step "Both see transition logs"
ALICE_SHOW=$(td_a show "$ISSUE_ID" --json 2>/dev/null)
BOB_SHOW=$(td_b show "$ISSUE_ID" --json 2>/dev/null)

ALICE_LOGS=$(echo "$ALICE_SHOW" | jq '.logs // []')
BOB_LOGS=$(echo "$BOB_SHOW" | jq '.logs // []')

# Both should have at least 2 logs: start + review
ALICE_LOG_COUNT=$(echo "$ALICE_LOGS" | jq 'length')
BOB_LOG_COUNT=$(echo "$BOB_LOGS" | jq 'length')
assert_ge "alice has >=2 logs (start+review)" "$ALICE_LOG_COUNT" "2"
assert_ge "bob has >=2 logs (start+review)" "$BOB_LOG_COUNT" "2"

ALICE_REVIEW_LOG=$(echo "$ALICE_LOGS" | jq '[.[] | select(.message | test("[Rr]eview"))] | length')
BOB_REVIEW_LOG=$(echo "$BOB_LOGS" | jq '[.[] | select(.message | test("[Rr]eview"))] | length')

assert_ge "alice has review log" "$ALICE_REVIEW_LOG" "1"
assert_ge "bob has review log" "$BOB_REVIEW_LOG" "1"

# ===================================================================
# Part 3: Bob creates issue, alice gets it
# ===================================================================
sleep 2  # clear debounce

_step "Bob creates issue"
BOB_CREATE_OUT=$(td_b create "Issue created by bob for alice")
BOB_ISSUE_ID=$(echo "$BOB_CREATE_OUT" | grep -oE 'td-[0-9a-f]+')
[ -n "$BOB_ISSUE_ID" ] || _fatal "No issue ID from: $BOB_CREATE_OUT"
_ok "Created $BOB_ISSUE_ID"

# Poll until alice sees both issues
_step "Alice polling for bob's issue"
elapsed=0
while [ "$elapsed" -lt "$TIMEOUT" ]; do
    td_a sync >/dev/null 2>&1
    ALICE_COUNT=$(td_a list --json --status all 2>/dev/null | jq 'length')
    if [ "$ALICE_COUNT" -ge 2 ]; then
        _ok "Bob's issue appeared after ~${elapsed}s"
        break
    fi
    sleep 2
    elapsed=$((elapsed + 2))
done

ALICE_ALL=$(td_a list --json --status all 2>/dev/null)
assert_eq "alice sees 2 issues" "$(echo "$ALICE_ALL" | jq 'length')" "2"

ALICE_BOB_ISSUE=$(echo "$ALICE_ALL" | jq --arg id "$BOB_ISSUE_ID" '.[] | select(.id == $id)')
assert_json_field "alice has bob's issue title" "$ALICE_BOB_ISSUE" '.title' "Issue created by bob for alice"
assert_json_field "alice has bob's issue id" "$ALICE_BOB_ISSUE" '.id' "$BOB_ISSUE_ID"

# Confirm alice's original issue is still in_review
ALICE_ORIG=$(echo "$ALICE_ALL" | jq -r --arg id "$ISSUE_ID" '.[] | select(.id == $id) | .status')
assert_eq "alice's original still in_review" "$ALICE_ORIG" "in_review"

report
