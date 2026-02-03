#!/usr/bin/env bash
#
# Chaos sync e2e library.
# Source this from chaos test scripts — do NOT run directly.
# Requires harness.sh to be sourced first (provides td_a, td_b, _step, _ok, _fail, assert_eq, etc.)
#
# NOTE: This file is bash 3.2 compatible (macOS default). No associative arrays,
# no ${var,,} or ${var^^} syntax. Uses delimited-string key-value stores instead.
#
# This file is now a wrapper that sources the split modules in order:
#   chaos_lib_core.sh        - KV helpers, state tracking, random generators, action selection
#   chaos_lib_executors.sh   - All exec_* action executor functions
#   chaos_lib_conflicts.sh   - Field collision, delete-mutate, burst, safe exec, sync scheduling
#   chaos_lib_verification.sh - Convergence verification, idempotency, event ordering, reporting
#

CHAOS_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source the verification module, which chains: verification -> conflicts -> executors -> core
source "$CHAOS_LIB_DIR/chaos_lib_verification.sh"
