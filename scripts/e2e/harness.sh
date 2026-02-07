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
#   CLIENT_A_DIR, CLIENT_B_DIR, CLIENT_C_DIR (if HARNESS_ACTORS>=3),
#   HOME_A, HOME_B, HOME_C (if HARNESS_ACTORS>=3)
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
SESSION_ID_C="e2e-carol-$$"

td_a() { (cd "$CLIENT_A_DIR" && HOME="$HOME_A" TD_SESSION_ID="$SESSION_ID_A" TD_FEATURE_SYNC_CLI=1 TD_FEATURE_SYNC_AUTOSYNC=1 "$TD_BIN" "$@"); }
td_b() { (cd "$CLIENT_B_DIR" && HOME="$HOME_B" TD_SESSION_ID="$SESSION_ID_B" TD_FEATURE_SYNC_CLI=1 TD_FEATURE_SYNC_AUTOSYNC=1 "$TD_BIN" "$@"); }
td_c() { (cd "$CLIENT_C_DIR" && HOME="$HOME_C" TD_SESSION_ID="$SESSION_ID_C" TD_FEATURE_SYNC_CLI=1 TD_FEATURE_SYNC_AUTOSYNC=1 "$TD_BIN" "$@"); }

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
    CLIENT_C_DIR="$WORKDIR/client-c"
    HOME_A="$WORKDIR/home-a"
    HOME_B="$WORKDIR/home-b"
    HOME_C="$WORKDIR/home-c"
    TD_BIN="$WORKDIR/td"
    SYNC_BIN="$WORKDIR/td-sync"

    mkdir -p "$SERVER_DATA" "$CLIENT_A_DIR" "$CLIENT_B_DIR" \
             "$HOME_A/.config/td" "$HOME_B/.config/td"
    if [ "${HARNESS_ACTORS:-2}" -ge 3 ]; then
        mkdir -p "$CLIENT_C_DIR" "$HOME_C/.config/td"
    fi

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

    if [ "${HARNESS_ACTORS:-2}" -ge 3 ]; then
        echo "n" | td_c init >/dev/null 2>&1
        _auth "carol@test.local" "$HOME_C"
        td_a sync-project invite "carol@test.local" writer >/dev/null
        td_c sync-project link "$PROJECT_ID" >/dev/null
    fi

    _ok "Ready (project $PROJECT_ID, actors: ${HARNESS_ACTORS:-2})"
}

# ---- Late Joiner Setup ----
# Sets up a new client that joins an existing project after data has been created.
# Usage: setup_late_joiner <actor>
# Requires: PROJECT_ID to be set (from initial setup)

setup_late_joiner() {
    local actor="$1"
    local email home_dir client_dir session_var

    case "$actor" in
        c)
            email="carol@test.local"
            home_dir="$HOME_C"
            client_dir="$CLIENT_C_DIR"
            session_var="SESSION_ID_C"
            ;;
        *)
            _fatal "setup_late_joiner: unsupported actor '$actor' (only 'c' supported)"
            ;;
    esac

    _step "Late joiner: setting up actor $actor ($email)"

    # Create directories if they don't exist
    mkdir -p "$client_dir" "$home_dir/.config/td"

    # Init td in the client directory (must use td_c to set TD_SESSION_ID)
    echo "n" | td_c init >/dev/null 2>&1

    # Authenticate the user (inline version of _auth)
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
{"sync":{"url":"$SERVER_URL","enabled":true,"snapshot_threshold":0,"auto":{"enabled":false,"on_start":false,"debounce":"2s","interval":"10s","pull":true}}}
EOF

    # Invite the user to the project (from actor A)
    td_a sync-project invite "$email" writer >/dev/null

    # Link to the project
    case "$actor" in
        c) td_c sync-project link "$PROJECT_ID" >/dev/null ;;
    esac

    _ok "Late joiner $actor ($email) ready, linked to $PROJECT_ID"
}

# ---- Server Lifecycle ----
# These functions enable mid-test server restart scenarios.
# Server config is stored in env vars during setup for restart capability.

stop_server() {
    if [ -n "$SERVER_PID" ]; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
        SERVER_PID=""
        _ok "Server stopped"
    fi
}

start_server() {
    _step "Starting server (:$PORT)"
    SYNC_LISTEN_ADDR=":$PORT" \
    SYNC_SERVER_DB_PATH="$SERVER_DATA/server.db" \
    SYNC_PROJECT_DATA_DIR="$SERVER_DATA/projects" \
    SYNC_ALLOW_SIGNUP=true \
    SYNC_BASE_URL="$SERVER_URL" \
    SYNC_LOG_FORMAT=text \
    SYNC_LOG_LEVEL=info \
      "$SYNC_BIN" >> "$WORKDIR/server.log" 2>&1 &
    SERVER_PID=$!

    for _ in $(seq 1 30); do
        curl -sf "$SERVER_URL/healthz" > /dev/null 2>&1 && break
        kill -0 "$SERVER_PID" 2>/dev/null || { cat "$WORKDIR/server.log"; _fatal "Server died on restart"; }
        sleep 0.2
    done
    curl -sf "$SERVER_URL/healthz" > /dev/null || { cat "$WORKDIR/server.log"; _fatal "Server not healthy after restart"; }
    _ok "Server started (PID: $SERVER_PID)"
}

restart_server() {
    stop_server
    start_server
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

# ---- Soak Testing Support ----
# These functions collect metrics for endurance/soak testing

# Soak metrics output file (set by caller)
SOAK_METRICS_FILE=""

# Baseline values (captured at start)
_SOAK_BASELINE_ALLOC_MB=0
_SOAK_BASELINE_GOROUTINES=0
_SOAK_BASELINE_FD_COUNT=0
_SOAK_BASELINE_DIR_SIZE_KB=0

# Initialize soak metrics collection
# Usage: init_soak_metrics
init_soak_metrics() {
    SOAK_METRICS_FILE="${WORKDIR}/soak-metrics.jsonl"
    : > "$SOAK_METRICS_FILE"

    # Capture baseline (first collection)
    _capture_soak_baseline
}

# Capture baseline metrics for comparison
_capture_soak_baseline() {
    local stats
    stats=$(td_a debug-stats 2>/dev/null || echo '{}')

    _SOAK_BASELINE_ALLOC_MB=$(echo "$stats" | jq -r '.alloc_mb // 0')
    _SOAK_BASELINE_GOROUTINES=$(echo "$stats" | jq -r '.num_goroutine // 0')

    # File descriptors for td process (uses SERVER_PID as proxy for td activity)
    if [ -n "$SERVER_PID" ]; then
        if [[ "$OSTYPE" == darwin* ]]; then
            _SOAK_BASELINE_FD_COUNT=$(lsof -p "$SERVER_PID" 2>/dev/null | wc -l | tr -d ' ')
        else
            _SOAK_BASELINE_FD_COUNT=$(ls /proc/"$SERVER_PID"/fd 2>/dev/null | wc -l | tr -d ' ')
        fi
    fi
    _SOAK_BASELINE_FD_COUNT=${_SOAK_BASELINE_FD_COUNT:-0}

    # Directory size
    _SOAK_BASELINE_DIR_SIZE_KB=$(du -sk "$CLIENT_A_DIR/.todos" 2>/dev/null | cut -f1)
    _SOAK_BASELINE_DIR_SIZE_KB=${_SOAK_BASELINE_DIR_SIZE_KB:-0}
}

# Collect and append soak metrics to JSONL file
# Usage: collect_soak_metrics
# Outputs one JSON line per call with timestamp and all metrics
collect_soak_metrics() {
    [ -z "$SOAK_METRICS_FILE" ] && return

    local ts elapsed stats alloc_mb sys_mb num_gc num_goroutine heap_objects heap_inuse_mb
    local fd_count_a fd_count_server wal_size_a wal_size_b dir_size_a dir_size_b

    ts=$(date +%s)
    elapsed=$(( ts - CHAOS_TIME_START ))

    # Runtime stats from td debug-stats (actor A)
    stats=$(td_a debug-stats 2>/dev/null || echo '{}')
    alloc_mb=$(echo "$stats" | jq -r '.alloc_mb // 0')
    sys_mb=$(echo "$stats" | jq -r '.sys_mb // 0')
    num_gc=$(echo "$stats" | jq -r '.num_gc // 0')
    num_goroutine=$(echo "$stats" | jq -r '.num_goroutine // 0')
    heap_objects=$(echo "$stats" | jq -r '.heap_objects // 0')
    heap_inuse_mb=$(echo "$stats" | jq -r '.heap_inuse_mb // 0')

    # File descriptors
    fd_count_server=0
    if [ -n "$SERVER_PID" ]; then
        if [[ "$OSTYPE" == darwin* ]]; then
            fd_count_server=$(lsof -p "$SERVER_PID" 2>/dev/null | wc -l | tr -d ' ')
        else
            fd_count_server=$(ls /proc/"$SERVER_PID"/fd 2>/dev/null | wc -l | tr -d ' ')
        fi
    fi

    # WAL sizes (bytes)
    wal_size_a=0
    wal_size_b=0
    if [ -f "$CLIENT_A_DIR/.todos/issues.db-wal" ]; then
        if [[ "$OSTYPE" == darwin* ]]; then
            wal_size_a=$(stat -f %z "$CLIENT_A_DIR/.todos/issues.db-wal" 2>/dev/null || echo 0)
        else
            wal_size_a=$(stat -c %s "$CLIENT_A_DIR/.todos/issues.db-wal" 2>/dev/null || echo 0)
        fi
    fi
    if [ -f "$CLIENT_B_DIR/.todos/issues.db-wal" ]; then
        if [[ "$OSTYPE" == darwin* ]]; then
            wal_size_b=$(stat -f %z "$CLIENT_B_DIR/.todos/issues.db-wal" 2>/dev/null || echo 0)
        else
            wal_size_b=$(stat -c %s "$CLIENT_B_DIR/.todos/issues.db-wal" 2>/dev/null || echo 0)
        fi
    fi

    # Directory sizes (KB)
    dir_size_a=$(du -sk "$CLIENT_A_DIR/.todos" 2>/dev/null | cut -f1)
    dir_size_b=$(du -sk "$CLIENT_B_DIR/.todos" 2>/dev/null | cut -f1)
    dir_size_a=${dir_size_a:-0}
    dir_size_b=${dir_size_b:-0}

    # Append JSONL record
    cat >> "$SOAK_METRICS_FILE" <<EOF
{"ts":$ts,"elapsed_s":$elapsed,"alloc_mb":$alloc_mb,"sys_mb":$sys_mb,"num_gc":$num_gc,"num_goroutine":$num_goroutine,"heap_objects":$heap_objects,"heap_inuse_mb":$heap_inuse_mb,"fd_count_server":$fd_count_server,"wal_size_a":$wal_size_a,"wal_size_b":$wal_size_b,"dir_size_kb_a":$dir_size_a,"dir_size_kb_b":$dir_size_b}
EOF
}
