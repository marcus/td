#!/usr/bin/env bash
#
# Test: monitor periodic auto-sync pushes edits made while monitor is running.
#
# Repro for the bug: Alice edits an issue while td monitor is open. Bob never
# receives the update until Alice exits the monitor. The periodic auto-sync
# (autoSyncOnce via TickMsg) should push the edit within the sync interval,
# but it either doesn't fire or fails silently.
#
# Strategy: instead of trying to drive the monitor UI with keystrokes,
# we run `td update --title ...` in a SEPARATE process while the monitor
# is running. This writes to the same DB via UpdateIssueLogged (identical
# to the monitor's submitForm path). The `td update` call has auto-sync
# DISABLED (TD_SYNC_AUTO=0) so it can't push — only the monitor's periodic
# sync can push the change.
#
# Uses `expect` for pseudo-TTY (BubbleTea requires a real TTY).
# Tests both directions: alice → bob and bob → alice.
#
# Requires: expect (pre-installed on macOS, `apt install expect` on Linux)
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"

command -v expect >/dev/null 2>&1 || _fatal "expect not found (needed for pseudo-TTY)"

# Kill any stale td monitor processes from previous test runs
pkill -f "td-e2e-.*/td monitor" 2>/dev/null || true

# auto-sync enabled, on_start=false (harness default), short interval for fast periodic sync
setup --auto-sync --debounce "1s" --interval "2s"

# Set up log files for debug inspection
LOG_A="$WORKDIR/alice.log"
LOG_B="$WORKDIR/bob.log"

# Helper: run td update with auto-sync DISABLED so only the monitor can sync
# This simulates editing inside the monitor (same DB path: UpdateIssueLogged)
td_a_nosync() { (cd "$CLIENT_A_DIR" && HOME="$HOME_A" TD_SESSION_ID="$SESSION_ID_A" TD_SYNC_AUTO=0 "$TD_BIN" "$@"); }
td_b_nosync() { (cd "$CLIENT_B_DIR" && HOME="$HOME_B" TD_SESSION_ID="$SESSION_ID_B" TD_SYNC_AUTO=0 "$TD_BIN" "$@"); }

# Helper: start monitor in background via expect (returns PID of expect process)
# The monitor runs with auto-sync enabled and a short interval.
start_monitor_bg() {
    local client_dir="$1" home="$2" session_id="$3" log="$4" pid_var="$5"
    local expect_log="$WORKDIR/expect_${pid_var}.log"
    expect -c "
        set timeout 120
        spawn bash -c {cd $client_dir && HOME=$home TD_SESSION_ID=$session_id TD_LOG_FILE=$log TERM=xterm $TD_BIN monitor --interval 1s}
        # Wait for quit signal file
        while {1} {
            sleep 1
            if {[file exists $WORKDIR/.quit_${pid_var}]} {
                send \"q\"
                expect eof
                break
            }
        }
    " > "$expect_log" 2>&1 &
    eval "$pid_var=$!"
}

# Helper: stop a background monitor
stop_monitor() {
    local pid_var="$1"
    local pid="${!pid_var}"
    # Signal the expect loop to send 'q' and exit
    touch "$WORKDIR/.quit_${pid_var}"
    # Wait for clean exit with timeout
    local i=0
    while kill -0 "$pid" 2>/dev/null && [ $i -lt 10 ]; do
        sleep 1
        i=$((i + 1))
    done
    if kill -0 "$pid" 2>/dev/null; then
        kill "$pid" 2>/dev/null || true
        sleep 1
        kill -9 "$pid" 2>/dev/null || true
    fi
    wait "$pid" 2>/dev/null || true
    rm -f "$WORKDIR/.quit_${pid_var}"
}

# Helper: run monitor briefly, quit after N seconds (for simple pull tests)
run_monitor_a() {
    local duration="${1:-6}"
    expect -c "
        set timeout [expr {$duration + 5}]
        spawn bash -c {cd $CLIENT_A_DIR && HOME=$HOME_A TD_SESSION_ID=$SESSION_ID_A TD_LOG_FILE=$LOG_A TERM=xterm $TD_BIN monitor --interval 1s}
        sleep $duration
        send \"q\"
        expect eof
    " >/dev/null 2>&1 || true
}

# =================================================================
# Part 1: Setup — create issues via CLI, sync to both sides
# =================================================================
_step "Setup: create issues and sync"

CREATE_OUT=$(td_a create "Original title for alice edit" 2>&1)
ISSUE_A1=$(echo "$CREATE_OUT" | grep -oE 'td-[0-9a-f]+')
[ -n "$ISSUE_A1" ] || _fatal "No issue ID from: $CREATE_OUT"
_ok "Created $ISSUE_A1"

sleep 2
CREATE_OUT=$(td_b create "Original title for bob edit" 2>&1)
ISSUE_B1=$(echo "$CREATE_OUT" | grep -oE 'td-[0-9a-f]+')
[ -n "$ISSUE_B1" ] || _fatal "No issue ID from: $CREATE_OUT"
_ok "Created $ISSUE_B1"

# Ensure both sides are fully synced
td_a sync >/dev/null 2>&1
td_b sync >/dev/null 2>&1
td_a sync >/dev/null 2>&1

ALICE_COUNT=$(td_a list --json --status all 2>/dev/null | jq 'length')
BOB_COUNT=$(td_b list --json --status all 2>/dev/null | jq 'length')
assert_eq "both start with 2 issues" "$ALICE_COUNT" "$BOB_COUNT"

# =================================================================
# Part 2: Alice edits while monitor runs — periodic sync should push
# =================================================================
_step "Part 2: Alice edits while monitor is running"

> "$LOG_A"

# Start alice's monitor in background
start_monitor_bg "$CLIENT_A_DIR" "$HOME_A" "$SESSION_ID_A" "$LOG_A" MON_A_PID
_ok "Alice's monitor started (pid $MON_A_PID)"

# Wait for monitor to initialize and verify it's logging
sleep 4
if [ ! -s "$LOG_A" ]; then
    echo "  WARN: Log file empty after 4s — monitor may not have started"
    echo "  Waiting 4s more..."
    sleep 4
fi
if [ -s "$LOG_A" ]; then
    _ok "Monitor log file has content ($(wc -l < "$LOG_A") lines)"
    echo "  First 5 lines:"
    head -5 "$LOG_A" | sed 's/^/    /'
else
    echo "  WARN: Log file still empty — monitor likely failed to start"
    echo "  Expect PID $MON_A_PID alive: $(kill -0 $MON_A_PID 2>&1 && echo yes || echo no)"
fi

# Edit the issue via CLI with auto-sync DISABLED
# This writes to the same DB path as the monitor's edit form (UpdateIssueLogged)
# but does NOT sync — only the monitor's periodic sync can push the change
td_a_nosync update "$ISSUE_A1" --title "EDITED WHILE MONITOR OPEN" >/dev/null 2>&1
_ok "Edited $ISSUE_A1 via td update (auto-sync disabled)"

# Verify the edit is in alice's local DB
ALICE_TITLE=$(td_a_nosync show "$ISSUE_A1" --json 2>/dev/null | jq -r '.title')
assert_eq "alice local DB has edit" "$ALICE_TITLE" "EDITED WHILE MONITOR OPEN"

# Verify the event is unsynced (since td update had auto-sync off)
UNSYNCED=$(sqlite3 "$CLIENT_A_DIR/.todos/issues.db" \
    "SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0" 2>/dev/null)
assert_ge "alice has unsynced events" "$UNSYNCED" "1"

# Wait for the monitor's periodic sync to fire and push
# With interval=2s and --interval 1s tick, it should fire within ~3s
_step "Waiting for monitor periodic sync to push edit"
sleep 8

# Check if events are now synced (without stopping the monitor)
STILL_UNSYNCED=$(sqlite3 "$CLIENT_A_DIR/.todos/issues.db" \
    "SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0" 2>/dev/null)
if [ "$STILL_UNSYNCED" = "0" ]; then
    _ok "Monitor periodic sync pushed the edit (0 unsynced events)"
else
    _fail "Monitor periodic sync did NOT push: $STILL_UNSYNCED unsynced events remain"
    echo "  --- td log file (TD_LOG_FILE) ---"
    cat "$LOG_A" 2>/dev/null || echo "  (log file empty or missing)"
    echo "  --- expect output ---"
    tail -20 "$WORKDIR/expect_MON_A_PID.log" 2>/dev/null || echo "  (no expect log)"
    echo "  --- Unsynced events ---"
    sqlite3 "$CLIENT_A_DIR/.todos/issues.db" \
        "SELECT id, action_type, entity_id FROM action_log WHERE synced_at IS NULL AND undone = 0" 2>/dev/null | head -5
    echo "  --- Monitor process check ---"
    ps aux | grep "td monitor" | grep -v grep || echo "  (no td monitor process found)"
fi

# Bob pulls and checks
_step "Bob checks for alice's edit"
td_b sync >/dev/null 2>&1
BOB_TITLE=$(td_b show "$ISSUE_A1" --json 2>/dev/null | jq -r '.title')
if [ "$BOB_TITLE" = "EDITED WHILE MONITOR OPEN" ]; then
    _ok "Bob received alice's edit via monitor periodic sync"
else
    _fail "Bob title: '$BOB_TITLE' (expected 'EDITED WHILE MONITOR OPEN')"
fi

# Stop alice's monitor
stop_monitor MON_A_PID
_ok "Alice's monitor stopped"

# =================================================================
# Part 3: If periodic sync failed, verify exit sync works (fallback)
# =================================================================
_step "Part 3: Verify exit sync pushed (fallback)"

# Alice's monitor just exited — PersistentPostRun should fire autoSyncAfterMutation
td_b sync >/dev/null 2>&1
BOB_TITLE_AFTER=$(td_b show "$ISSUE_A1" --json 2>/dev/null | jq -r '.title')
assert_eq "bob has edit after monitor exit" "$BOB_TITLE_AFTER" "EDITED WHILE MONITOR OPEN"

# =================================================================
# Part 4: Bob edits while monitor runs — alice checks (reverse)
# =================================================================
_step "Part 4: Bob edits while monitor running (reverse direction)"

> "$LOG_B"

start_monitor_bg "$CLIENT_B_DIR" "$HOME_B" "$SESSION_ID_B" "$LOG_B" MON_B_PID
_ok "Bob's monitor started (pid $MON_B_PID)"

sleep 4

td_b_nosync update "$ISSUE_B1" --title "EDITED BY BOB WHILE MONITOR OPEN" >/dev/null 2>&1
_ok "Edited $ISSUE_B1 via td update (auto-sync disabled)"

BOB_LOCAL=$(td_b_nosync show "$ISSUE_B1" --json 2>/dev/null | jq -r '.title')
assert_eq "bob local DB has edit" "$BOB_LOCAL" "EDITED BY BOB WHILE MONITOR OPEN"

# Wait for bob's monitor periodic sync
_step "Waiting for bob's monitor periodic sync"
sleep 8

# Alice syncs and checks
td_a sync >/dev/null 2>&1
ALICE_B1=$(td_a show "$ISSUE_B1" --json 2>/dev/null | jq -r '.title')
if [ "$ALICE_B1" = "EDITED BY BOB WHILE MONITOR OPEN" ]; then
    _ok "Alice received bob's edit via monitor periodic sync"
else
    _fail "Alice title: '$ALICE_B1' (expected 'EDITED BY BOB WHILE MONITOR OPEN')"
fi

stop_monitor MON_B_PID
_ok "Bob's monitor stopped"

# After exit, sync again to verify exit fallback
td_a sync >/dev/null 2>&1
ALICE_B1_AFTER=$(td_a show "$ISSUE_B1" --json 2>/dev/null | jq -r '.title')
assert_eq "alice has bob's edit after exit" "$ALICE_B1_AFTER" "EDITED BY BOB WHILE MONITOR OPEN"

# =================================================================
# Part 5: Both monitors running — bidirectional edit
# =================================================================
_step "Part 5: Both monitors running, bidirectional edits"

> "$LOG_A"
> "$LOG_B"

start_monitor_bg "$CLIENT_A_DIR" "$HOME_A" "$SESSION_ID_A" "$LOG_A" MON_A2_PID
start_monitor_bg "$CLIENT_B_DIR" "$HOME_B" "$SESSION_ID_B" "$LOG_B" MON_B2_PID
_ok "Both monitors started"

sleep 4

# Alice edits, bob edits (different issues to avoid conflict)
td_a_nosync update "$ISSUE_A1" --title "ALICE BIDIR EDIT" >/dev/null 2>&1
td_b_nosync update "$ISSUE_B1" --title "BOB BIDIR EDIT" >/dev/null 2>&1
_ok "Both edited their issues"

# Wait for periodic sync on both sides
sleep 12

# Check convergence while monitors are still running
ALICE_B1_TITLE=$(td_a_nosync show "$ISSUE_B1" --json 2>/dev/null | jq -r '.title')
BOB_A1_TITLE=$(td_b_nosync show "$ISSUE_A1" --json 2>/dev/null | jq -r '.title')

if [ "$ALICE_B1_TITLE" = "BOB BIDIR EDIT" ]; then
    _ok "Alice pulled bob's bidir edit via monitor"
else
    _fail "Alice has '$ALICE_B1_TITLE' for bob's issue (expected 'BOB BIDIR EDIT')"
fi

if [ "$BOB_A1_TITLE" = "ALICE BIDIR EDIT" ]; then
    _ok "Bob pulled alice's bidir edit via monitor"
else
    _fail "Bob has '$BOB_A1_TITLE' for alice's issue (expected 'ALICE BIDIR EDIT')"
fi

stop_monitor MON_A2_PID
stop_monitor MON_B2_PID
_ok "Both monitors stopped"

# =================================================================
# Final convergence
# =================================================================
_step "Final convergence"
td_a sync >/dev/null 2>&1
td_b sync >/dev/null 2>&1

ALICE_TOTAL=$(td_a list --json --status all 2>/dev/null | jq 'length')
BOB_TOTAL=$(td_b list --json --status all 2>/dev/null | jq 'length')
assert_eq "alice and bob have same count" "$ALICE_TOTAL" "$BOB_TOTAL"

FINAL_A1=$(td_a show "$ISSUE_A1" --json 2>/dev/null | jq -r '.title')
FINAL_B1=$(td_b show "$ISSUE_B1" --json 2>/dev/null | jq -r '.title')
assert_eq "final alice issue title matches" "$FINAL_A1" "$(td_b show "$ISSUE_A1" --json 2>/dev/null | jq -r '.title')"
assert_eq "final bob issue title matches" "$FINAL_B1" "$(td_a show "$ISSUE_B1" --json 2>/dev/null | jq -r '.title')"

report
