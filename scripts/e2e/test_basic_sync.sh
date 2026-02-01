#!/usr/bin/env bash
#
# Test: basic bidirectional sync (manual td sync)
# Verifies: push from A, pull on B, push from B, pull on A — convergence.
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"

setup

_step "Alice creates 3 issues"
td_a create "Implement user authentication flow" >/dev/null
td_a create "Fix database connection pooling" >/dev/null
td_a create "Add integration test suite" >/dev/null

_step "Alice pushes"
td_a sync >/dev/null 2>&1

_step "Bob pulls"
td_b sync >/dev/null 2>&1

LIST_A=$(td_a list --json 2>/dev/null)
LIST_B=$(td_b list --json 2>/dev/null)
COUNT_A=$(echo "$LIST_A" | jq 'length')
COUNT_B=$(echo "$LIST_B" | jq 'length')

assert_eq "alice has 3 issues" "$COUNT_A" "3"
assert_eq "bob has 3 issues" "$COUNT_B" "3"

_step "Bob creates issue + syncs both ways"
td_b create "Client B created this issue" >/dev/null
td_b sync >/dev/null 2>&1
td_a sync >/dev/null 2>&1

LIST_A2=$(td_a list --json 2>/dev/null)
LIST_B2=$(td_b list --json 2>/dev/null)
COUNT_A2=$(echo "$LIST_A2" | jq 'length')
COUNT_B2=$(echo "$LIST_B2" | jq 'length')

assert_eq "bidirectional: alice has 4" "$COUNT_A2" "4"
assert_eq "bidirectional: bob has 4" "$COUNT_B2" "4"

report
