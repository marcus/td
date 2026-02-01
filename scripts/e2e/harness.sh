#!/usr/bin/env bash
#
# Shared harness for e2e sync tests.
# Source this from test scripts — do NOT run directly.
#
# Provides:
#   setup [--auto-sync] [--debounce 2s] [--interval 10s]
#   td_a, td_b               run td as alice / bob
#   wait_for <cmd> [timeout]  poll until cmd succeeds (default 15s)
#   assert_eq, assert_ge, assert_contains, assert_json_field
#   teardown                  (also runs on EXIT trap)
#
# Env after setup:
#   WORKDIR, TD_BIN, SERVER_URL, PROJECT_ID, SERVER_PID,
#   CLIENT_A_DIR, CLIENT_B_DIR, HOME_A, HOME_B
#
set -euo pipefail

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

_step()  { echo -e "${CYAN}--- $1 ---${NC}"; }
_ok()    { echo -e "${GREEN}  OK:${NC} $1"; }
_fail()  { echo -e "${RED}  FAIL:${NC} $1" >&2; HARNESS_FAILURES=$((HARNESS_FAILURES + 1)); }
_fatal() { echo -e "${RED}  FATAL:${NC} $1" >&2; exit 1; }

HARNESS_FAILURES=0
HARNESS_ASSERTIONS=0
SERVER_PID=""
WORKDIR=""

# ---- Assertions ----

assert_eq() {
    local desc="$1" actual="$2" expected="$3"
    HARNESS_ASSERTIONS=$((HARNESS_ASSERTIONS + 1))
    if [ "$actual" = "$expected" ]; then
        _ok "$desc"
    else
        _fail "$desc: expected '$expected', got '$actual'"
    fi
}

assert_ge() {
    local desc="$1" actual="$2" min="$3"
    HARNESS_ASSERTIONS=$((HARNESS_ASSERTIONS + 1))
    if [ "$actual" -ge "$min" ] 2>/dev/null; then
        _ok "$desc"
    else
        _fail "$desc: expected >= $min, got '$actual'"
    fi
}

assert_contains() {
    local desc="$1" haystack="$2" needle="$3"
    HARNESS_ASSERTIONS=$((HARNESS_ASSERTIONS + 1))
    if echo "$haystack" | grep -q "$needle"; then
        _ok "$desc"
    else
        _fail "$desc: '$needle' not found in output"
    fi
}

assert_json_field() {
    local desc="$1" json="$2" jq_expr="$3" expected="$4"
    HARNESS_ASSERTIONS=$((HARNESS_ASSERTIONS + 1))
    local actual
    actual=$(echo "$json" | jq -r "$jq_expr" 2>/dev/null)
    if [ "$actual" = "$expected" ]; then
        _ok "$desc"
    else
        _fail "$desc: jq '$jq_expr' = '$actual', expected '$expected'"
    fi
}

# ---- wait_for: poll until a command succeeds ----

wait_for() {
    local cmd="$1"
    local timeout="${2:-15}"
    local interval="${3:-0.5}"
    local elapsed=0
    while ! eval "$cmd" >/dev/null 2>&1; do
        sleep "$interval"
        elapsed=$(echo "$elapsed + $interval" | bc)
        if (( $(echo "$elapsed >= $timeout" | bc -l) )); then
            return 1
        fi
    done
    return 0
}

# ---- Helpers: run td as each client ----

SESSION_ID_A="e2e-alice-$$"
SESSION_ID_B="e2e-bob-$$"

td_a() { (cd "$CLIENT_A_DIR" && HOME="$HOME_A" TD_SESSION_ID="$SESSION_ID_A" "$TD_BIN" "$@"); }
td_b() { (cd "$CLIENT_B_DIR" && HOME="$HOME_B" TD_SESSION_ID="$SESSION_ID_B" "$TD_BIN" "$@"); }

# ---- Teardown ----

teardown() {
    echo ""
    if [ -n "$SERVER_PID" ]; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    if [ -n "$WORKDIR" ] && [ -d "$WORKDIR" ]; then
        rm -rf "$WORKDIR"
    fi
}
trap teardown EXIT

# ---- Setup ----

setup() {
    local auto_sync=false
    local debounce="2s"
    local interval="10s"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --auto-sync) auto_sync=true; shift ;;
            --debounce)  debounce="$2"; shift 2 ;;
            --interval)  interval="$2"; shift 2 ;;
            *) _fatal "setup: unknown flag '$1'" ;;
        esac
    done

    # Locate repo root (harness.sh lives in scripts/e2e/)
    REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

    # Pick a random port to avoid conflicts between parallel tests
    PORT=$((10000 + RANDOM % 50000))
    SERVER_URL="http://localhost:$PORT"

    WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/td-e2e-XXXX")
    SERVER_DATA="$WORKDIR/server-data"
    CLIENT_A_DIR="$WORKDIR/client-a"
    CLIENT_B_DIR="$WORKDIR/client-b"
    HOME_A="$WORKDIR/home-a"
    HOME_B="$WORKDIR/home-b"
    TD_BIN="$WORKDIR/td"
    SYNC_BIN="$WORKDIR/td-sync"

    mkdir -p "$SERVER_DATA" "$CLIENT_A_DIR" "$CLIENT_B_DIR" \
             "$HOME_A/.config/td" "$HOME_B/.config/td"

    # Build
    _step "Building"
    (cd "$REPO_DIR" && go build -o "$TD_BIN" .)
    (cd "$REPO_DIR" && go build -o "$SYNC_BIN" ./cmd/td-sync)

    # Start server
    _step "Starting server (:$PORT)"
    SYNC_LISTEN_ADDR=":$PORT" \
    SYNC_SERVER_DB_PATH="$SERVER_DATA/server.db" \
    SYNC_PROJECT_DATA_DIR="$SERVER_DATA/projects" \
    SYNC_ALLOW_SIGNUP=true \
    SYNC_BASE_URL="$SERVER_URL" \
    SYNC_LOG_FORMAT=text \
    SYNC_LOG_LEVEL=info \
      "$SYNC_BIN" > "$WORKDIR/server.log" 2>&1 &
    SERVER_PID=$!

    for _ in $(seq 1 30); do
        curl -sf "$SERVER_URL/healthz" > /dev/null 2>&1 && break
        kill -0 "$SERVER_PID" 2>/dev/null || { cat "$WORKDIR/server.log"; _fatal "Server died"; }
        sleep 0.2
    done
    curl -sf "$SERVER_URL/healthz" > /dev/null || { cat "$WORKDIR/server.log"; _fatal "Server not healthy"; }

    # Auth helper
    _auth() {
        local email="$1" home_dir="$2"
        local resp dc uc ak uid did

        resp=$(curl -sf -X POST "$SERVER_URL/v1/auth/login/start" \
            -H "Content-Type: application/json" -d "{\"email\":\"$email\"}")
        dc=$(echo "$resp" | jq -r '.device_code')
        uc=$(echo "$resp" | jq -r '.user_code')
        curl -sf -X POST "$SERVER_URL/auth/verify" -d "user_code=$uc" > /dev/null
        resp=$(curl -sf -X POST "$SERVER_URL/v1/auth/login/poll" \
            -H "Content-Type: application/json" -d "{\"device_code\":\"$dc\"}")
        ak=$(echo "$resp" | jq -r '.api_key')
        uid=$(echo "$resp" | jq -r '.user_id')
        did=$(openssl rand -hex 16)

        cat > "$home_dir/.config/td/auth.json" <<EOF
{"api_key":"$ak","user_id":"$uid","email":"$email","server_url":"$SERVER_URL","device_id":"$did"}
EOF
        chmod 600 "$home_dir/.config/td/auth.json"
        cat > "$home_dir/.config/td/config.json" <<EOF
{"sync":{"url":"$SERVER_URL","enabled":true,"snapshot_threshold":0,"auto":{"enabled":$auto_sync,"on_start":false,"debounce":"$debounce","interval":"$interval","pull":true}}}
EOF
    }

    # Init + auth + link
    _step "Init + auth + link"
    echo "n" | td_a init >/dev/null 2>&1
    echo "n" | td_b init >/dev/null 2>&1
    _auth "alice@test.local" "$HOME_A"
    _auth "bob@test.local" "$HOME_B"

    local create_out
    create_out=$(td_a sync-project create "e2e-test" 2>&1)
    PROJECT_ID=$(echo "$create_out" | grep -oE 'p_[0-9a-f]+')
    [ -n "$PROJECT_ID" ] || _fatal "No project ID from: $create_out"

    td_a sync-project link "$PROJECT_ID" >/dev/null
    td_a sync >/dev/null 2>&1
    td_a sync-project invite "bob@test.local" writer >/dev/null
    td_b sync-project link "$PROJECT_ID" >/dev/null

    _ok "Ready (project $PROJECT_ID)"
}

# ---- Report ----

report() {
    echo ""
    if [ "$HARNESS_FAILURES" -eq 0 ]; then
        echo -e "${GREEN}${BOLD}PASS${NC} ($HARNESS_ASSERTIONS assertions)"
    else
        echo -e "${RED}${BOLD}FAIL${NC} ($HARNESS_FAILURES failures / $HARNESS_ASSERTIONS assertions)"
        echo ""
        echo "Server log: $WORKDIR/server.log"
        exit 1
    fi
}
