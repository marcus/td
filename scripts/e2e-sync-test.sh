#!/usr/bin/env bash
#
# E2E sync test: builds td + td-sync, starts a local server, authenticates
# two clients, syncs issues between them, and verifies convergence.
# All artifacts live in a temp dir — zero impact on existing projects.
#
# Usage:
#   bash scripts/e2e-sync-test.sh              # automated test
#   bash scripts/e2e-sync-test.sh --manual     # interactive mode (auto-sync off)
#   bash scripts/e2e-sync-test.sh --manual --auto-sync  # interactive + auto-sync on
#
set -euo pipefail

# --- Parse flags ---
MODE="auto"
AUTO_SYNC=false
SEED_DB=""
for arg in "$@"; do
    case "$arg" in
        --manual)    MODE="manual" ;;
        --auto-sync) AUTO_SYNC=true ;;
        --seed)      SEED_DB="next" ;;
        --help|-h)
            echo "Usage: $0 [--manual] [--auto-sync] [--seed <path>]"
            echo ""
            echo "  (default)      Run automated convergence test"
            echo "  --manual       Set up server + 2 clients, print shell instructions, wait"
            echo "  --auto-sync    Enable auto-sync (default: off, manual td sync)"
            echo "  --seed <path>  Seed bob with an existing issues.db (syncs up to alice)"
            exit 0
            ;;
        *)
            if [ "$SEED_DB" = "next" ]; then
                SEED_DB="$arg"
            else
                echo "Unknown flag: $arg"; exit 1
            fi
            ;;
    esac
done
if [ "$SEED_DB" = "next" ]; then
    echo "Error: --seed requires a path argument"; exit 1
fi

# --- Config ---
PORT=9876
SERVER_URL="http://localhost:$PORT"
EMAIL_A="alice@test.local"
EMAIL_B="bob@test.local"
REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

step()  { echo -e "${CYAN}=== $1 ===${NC}"; }
ok()    { echo -e "${GREEN}OK:${NC} $1"; }
warn()  { echo -e "${YELLOW}WARN:${NC} $1"; }
fail()  { echo -e "${RED}FAIL:${NC} $1"; exit 1; }

# --- Setup temp dir ---
WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/td-e2e-XXXX")
SERVER_PID=""

cleanup() {
    echo ""
    step "Cleaning up"
    tmux kill-session -t "td-e2e" 2>/dev/null && ok "Killed tmux session" || true
    if [ -n "$SERVER_PID" ]; then
        kill "$SERVER_PID" 2>/dev/null && ok "Killed server (PID $SERVER_PID)" || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -rf "$WORKDIR"
    ok "Removed $WORKDIR"
}
trap cleanup EXIT

SERVER_DATA="$WORKDIR/server-data"
CLIENT_A_DIR="$WORKDIR/client-a"
CLIENT_B_DIR="$WORKDIR/client-b"
HOME_A="$WORKDIR/home-a"
HOME_B="$WORKDIR/home-b"
TD_BIN="$WORKDIR/td"
SYNC_BIN="$WORKDIR/td-sync"

mkdir -p "$SERVER_DATA" "$CLIENT_A_DIR" "$CLIENT_B_DIR" \
         "$HOME_A/.config/td" "$HOME_B/.config/td"

echo "Work dir: $WORKDIR"

# --- Build ---
step "Building td and td-sync"
(cd "$REPO_DIR" && go build -o "$TD_BIN" .)
(cd "$REPO_DIR" && go build -o "$SYNC_BIN" ./cmd/td-sync)
ok "Built binaries"

# --- Start server ---
step "Starting sync server on :$PORT"
SYNC_LISTEN_ADDR=":$PORT" \
SYNC_SERVER_DB_PATH="$SERVER_DATA/server.db" \
SYNC_PROJECT_DATA_DIR="$SERVER_DATA/projects" \
SYNC_ALLOW_SIGNUP=true \
SYNC_BASE_URL="$SERVER_URL" \
SYNC_LOG_FORMAT=text \
SYNC_LOG_LEVEL=info \
  "$SYNC_BIN" > "$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

for i in $(seq 1 30); do
    if curl -sf "$SERVER_URL/healthz" > /dev/null 2>&1; then break; fi
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        cat "$WORKDIR/server.log"; fail "Server failed to start"
    fi
    sleep 0.2
done
curl -sf "$SERVER_URL/healthz" > /dev/null || { cat "$WORKDIR/server.log"; fail "Server not healthy"; }
ok "Server running (PID $SERVER_PID)"

# --- Helper: authenticate a client ---
authenticate() {
    local email="$1"
    local home_dir="$2"
    local auto_sync_enabled="$3"

    local resp device_code user_code api_key user_id device_id

    resp=$(curl -sf -X POST "$SERVER_URL/v1/auth/login/start" \
        -H "Content-Type: application/json" \
        -d "{\"email\":\"$email\"}")
    device_code=$(echo "$resp" | jq -r '.device_code')
    user_code=$(echo "$resp" | jq -r '.user_code')

    curl -sf -X POST "$SERVER_URL/auth/verify" \
        -d "user_code=$user_code" > /dev/null

    resp=$(curl -sf -X POST "$SERVER_URL/v1/auth/login/poll" \
        -H "Content-Type: application/json" \
        -d "{\"device_code\":\"$device_code\"}")
    api_key=$(echo "$resp" | jq -r '.api_key')
    user_id=$(echo "$resp" | jq -r '.user_id')
    device_id=$(openssl rand -hex 16)

    cat > "$home_dir/.config/td/auth.json" <<EOF
{
  "api_key": "$api_key",
  "user_id": "$user_id",
  "email": "$email",
  "server_url": "$SERVER_URL",
  "device_id": "$device_id"
}
EOF
    chmod 600 "$home_dir/.config/td/auth.json"

    cat > "$home_dir/.config/td/config.json" <<EOF
{
  "sync": {
    "url": "$SERVER_URL",
    "enabled": true,
    "snapshot_threshold": 0,
    "auto": {
      "enabled": $auto_sync_enabled,
      "on_start": $auto_sync_enabled,
      "debounce": "2s",
      "interval": "10s",
      "pull": true
    }
  }
}
EOF

    ok "Authenticated $email (user $user_id)"
}

# --- Helpers: run td as each client ---
SESSION_ID_A="e2e-alice-$$"
SESSION_ID_B="e2e-bob-$$"
td_a() { (cd "$CLIENT_A_DIR" && HOME="$HOME_A" TD_SESSION_ID="$SESSION_ID_A" "$TD_BIN" "$@"); }
td_b() { (cd "$CLIENT_B_DIR" && HOME="$HOME_B" TD_SESSION_ID="$SESSION_ID_B" "$TD_BIN" "$@"); }

# --- Common setup (both modes) ---
step "Initializing client projects"
echo "n" | td_a init 2>/dev/null
echo "n" | td_b init 2>/dev/null
ok "Both projects initialized"

step "Authenticating clients"
authenticate "$EMAIL_A" "$HOME_A" "$AUTO_SYNC"
authenticate "$EMAIL_B" "$HOME_B" "$AUTO_SYNC"

step "Creating remote project"
CREATE_OUTPUT=$(td_a sync-project create "e2e-test" 2>&1)
echo "$CREATE_OUTPUT"
PROJECT_ID=$(echo "$CREATE_OUTPUT" | grep -oE 'p_[0-9a-f]+')
[ -n "$PROJECT_ID" ] || fail "Could not extract project ID from: $CREATE_OUTPUT"
ok "Project ID: $PROJECT_ID"

td_a sync-project link "$PROJECT_ID"
ok "Client A linked"

td_a sync
ok "Client A initial sync"

step "Inviting client B"
td_a sync-project invite "$EMAIL_B" writer
ok "Invited bob as writer"

td_b sync-project link "$PROJECT_ID"
ok "Client B linked"

# --- Seed DBs if --seed was provided ---
if [ -n "$SEED_DB" ]; then
    [ -f "$SEED_DB" ] || fail "Seed DB not found: $SEED_DB"
    SEED_COUNT=$(sqlite3 "$SEED_DB" 'SELECT COUNT(*) FROM issues' 2>/dev/null || echo "?")
    step "Seeding bob from $SEED_DB ($SEED_COUNT issues)"
    sqlite3 "$SEED_DB" 'PRAGMA wal_checkpoint(TRUNCATE);' 2>/dev/null || true
    cp "$SEED_DB" "$CLIENT_B_DIR/.todos/issues.db"
    sqlite3 "$CLIENT_B_DIR/.todos/issues.db" <<'SQL'
DELETE FROM sync_state;
DELETE FROM action_log WHERE id IS NULL OR entity_id IS NULL OR entity_id = '';
UPDATE action_log SET synced_at = NULL, server_seq = NULL;
SQL
    # Re-create session and re-link after DB replacement
    td_b status >/dev/null 2>&1 || true
    td_b sync-project link "$PROJECT_ID" >/dev/null
    ok "Seeded bob with $SEED_COUNT issues"
fi

# =====================================================================
# Manual mode: open tmux session (or print instructions as fallback)
# =====================================================================
if [ "$MODE" = "manual" ]; then
    SYNC_MODE_LABEL="manual (td sync)"
    if [ "$AUTO_SYNC" = "true" ]; then
        SYNC_MODE_LABEL="auto-sync (2s debounce, 10s interval)"
    fi

    # Write sourceable env files for each shell (handle bash vs zsh prompts)
    USER_SHELL="${SHELL:-/bin/bash}"
    if [[ "$USER_SHELL" == *zsh* ]]; then
        PROMPT_A='export PROMPT="%F{green}%Balice%b%f %F{242}td-sync%f %# "'
        PROMPT_B='export PROMPT="%F{cyan}%Bbob%b%f %F{242}td-sync%f %# "'
    else
        PROMPT_A='export PS1="\[\e[1;32m\]alice\[\e[0m\] \[\e[38;5;242m\]td-sync\[\e[0m\] \$ "'
        PROMPT_B='export PS1="\[\e[1;36m\]bob\[\e[0m\] \[\e[38;5;242m\]td-sync\[\e[0m\] \$ "'
    fi

    # Log files for client-side debug output (visible in the logs pane)
    LOG_A="$WORKDIR/alice.log"
    LOG_B="$WORKDIR/bob.log"
    touch "$LOG_A" "$LOG_B"

    cat > "$WORKDIR/shell-a.env" <<ENVEOF
export HOME="$HOME_A"
export PATH="$WORKDIR:\$PATH"
export TD_SESSION_ID="$SESSION_ID_A"
export TD_LOG_FILE="$LOG_A"
$PROMPT_A
cd "$CLIENT_A_DIR"
ENVEOF

    cat > "$WORKDIR/shell-b.env" <<ENVEOF
export HOME="$HOME_B"
export PATH="$WORKDIR:\$PATH"
export TD_SESSION_ID="$SESSION_ID_B"
export TD_LOG_FILE="$LOG_B"
$PROMPT_B
cd "$CLIENT_B_DIR"
ENVEOF

    # Build a hints message shown at the top of each pane
    HINTS="Sync: $SYNC_MODE_LABEL | Server: $SERVER_URL | Project: $PROJECT_ID"

    if command -v tmux &>/dev/null; then
        TMUX_SESSION="td-e2e"

        # Kill stale session if one exists
        tmux kill-session -t "$TMUX_SESSION" 2>/dev/null || true

        # Create session with first pane (alice)
        tmux new-session -d -s "$TMUX_SESSION" -x 200 -y 50 "$USER_SHELL"
        tmux send-keys -t "$TMUX_SESSION" "source $WORKDIR/shell-a.env" Enter
        tmux send-keys -t "$TMUX_SESSION" "echo ''; echo '$HINTS'; echo 'Ready as alice@test.local (client A)'; echo ''" Enter

        # Split and create second pane (bob)
        tmux split-window -h -t "$TMUX_SESSION" "$USER_SHELL"
        tmux send-keys -t "$TMUX_SESSION" "source $WORKDIR/shell-b.env" Enter
        tmux send-keys -t "$TMUX_SESSION" "echo ''; echo '$HINTS'; echo 'Ready as bob@test.local (client B)'; echo ''" Enter

        # Bottom pane for logs (server + client logs interleaved)
        tmux split-window -v -t "$TMUX_SESSION:.0" -l 12 "$USER_SHELL"
        tmux send-keys -t "$TMUX_SESSION" "echo 'Tailing server + client logs...'; tail -f $WORKDIR/server.log $LOG_A $LOG_B" Enter

        # Focus alice pane (top-left)
        tmux select-pane -t "$TMUX_SESSION:.0"

        echo ""
        echo -e "${GREEN}${BOLD}Launching tmux session: $TMUX_SESSION${NC}"
        echo -e "${DIM}Close all panes or detach (Ctrl-B d) to tear down.${NC}"
        echo -e "${DIM}Bottom pane: server + client logs${NC}"
        echo ""

        # Attach — blocks until user detaches or closes all panes
        tmux attach -t "$TMUX_SESSION" || true

        # After detach/exit, check if session still alive
        if tmux has-session -t "$TMUX_SESSION" 2>/dev/null; then
            echo ""
            echo -e "${YELLOW}Detached. Server still running (PID $SERVER_PID).${NC}"
            echo -e "  Reattach:  tmux attach -t $TMUX_SESSION"
            echo -e "  Tear down: tmux kill-session -t $TMUX_SESSION"
            echo ""
            echo -e "${YELLOW}Press Enter here to kill everything, or Ctrl-C to keep it running.${NC}"
            # Disable the auto-cleanup trap so Ctrl-C just exits the script
            trap - EXIT
            read -r
            # User pressed Enter — clean up manually
            tmux kill-session -t "$TMUX_SESSION" 2>/dev/null || true
            cleanup
        fi
        # Session gone — cleanup runs via trap
        exit 0
    fi

    # --- Fallback: no tmux ---
    echo ""
    echo -e "${GREEN}${BOLD}========================================${NC}"
    echo -e "${GREEN}${BOLD}  Setup complete! Ready for testing.${NC}"
    echo -e "${GREEN}${BOLD}========================================${NC}"
    echo ""
    echo -e "${BOLD}Sync mode:${NC} $SYNC_MODE_LABEL"
    echo -e "${BOLD}Server:${NC}    $SERVER_URL (PID $SERVER_PID)"
    echo -e "${BOLD}Project:${NC}   $PROJECT_ID"
    echo -e "${BOLD}Logs:${NC}      tail -f $WORKDIR/server.log"
    echo ""
    echo -e "${BOLD}Open two terminals and run:${NC}"
    echo ""
    echo -e "  ${CYAN}Shell A (alice):${NC}"
    echo -e "    source $WORKDIR/shell-a.env"
    echo ""
    echo -e "  ${CYAN}Shell B (bob):${NC}"
    echo -e "    source $WORKDIR/shell-b.env"
    echo ""
    echo -e "${BOLD}Then try:${NC}"
    echo -e "  ${DIM}# CLI testing${NC}"
    echo -e "  td create \"My first synced issue\""
    if [ "$AUTO_SYNC" = "false" ]; then
        echo -e "  td sync                  ${DIM}# push + pull${NC}"
    else
        echo -e "  ${DIM}# (auto-sync will push within 2s)${NC}"
    fi
    echo -e "  td list"
    echo ""
    echo -e "  ${DIM}# TUI testing${NC}"
    echo -e "  td monitor"
    echo ""
    if [ "$AUTO_SYNC" = "false" ]; then
        echo -e "  ${DIM}# Sync commands${NC}"
        echo -e "  td sync              ${DIM}# push + pull${NC}"
        echo -e "  td sync --push       ${DIM}# push only${NC}"
        echo -e "  td sync --pull       ${DIM}# pull only${NC}"
        echo -e "  td sync --status     ${DIM}# show state${NC}"
        echo ""
    fi
    echo -e "  ${DIM}# Server logs (in a third terminal)${NC}"
    echo -e "  tail -f $WORKDIR/server.log"
    echo ""
    echo -e "${YELLOW}Press Enter to tear down (kills server + removes temp dir)${NC}"
    read -r
    exit 0
fi

# =====================================================================
# Automated mode: create issues, sync, verify convergence
# =====================================================================
step "Creating issues on client A"
td_a create "Implement user authentication flow"
td_a create "Fix database connection pooling"
td_a create "Add integration test suite"
ok "3 issues created on client A"

step "Syncing client A (push)"
td_a sync
ok "Client A pushed"

step "Syncing client B (pull)"
td_b sync
ok "Client B pulled"

step "Verifying convergence"
LIST_A=$(td_a list --json 2>/dev/null)
LIST_B=$(td_b list --json 2>/dev/null)
COUNT_A=$(echo "$LIST_A" | jq 'length')
COUNT_B=$(echo "$LIST_B" | jq 'length')

echo "Client A: $COUNT_A issues"
echo "Client B: $COUNT_B issues"

if [ "$COUNT_A" != "$COUNT_B" ]; then
    echo "Client A issues:"; echo "$LIST_A" | jq '.[].title'
    echo "Client B issues:"; echo "$LIST_B" | jq '.[].title'
    fail "Issue count mismatch: A=$COUNT_A B=$COUNT_B"
fi
[ "$COUNT_A" -ge 3 ] || fail "Expected at least 3 issues, got $COUNT_A"

step "Testing bidirectional sync"
td_b create "Client B created this issue"
td_b sync
td_a sync

LIST_A2=$(td_a list --json 2>/dev/null)
LIST_B2=$(td_b list --json 2>/dev/null)
COUNT_A2=$(echo "$LIST_A2" | jq 'length')
COUNT_B2=$(echo "$LIST_B2" | jq 'length')

echo "After bidirectional: A=$COUNT_A2 B=$COUNT_B2"
[ "$COUNT_A2" = "$COUNT_B2" ] || fail "Bidirectional mismatch: A=$COUNT_A2 B=$COUNT_B2"
[ "$COUNT_A2" -ge 4 ] || fail "Expected at least 4 issues, got $COUNT_A2"

step "Sync status"
echo "--- Client A ---"
td_a sync --status || true
echo ""
echo "--- Client B ---"
td_b sync --status || true

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  E2E sync test passed!${NC}"
echo -e "${GREEN}  $COUNT_A2 issues synced between 2 clients${NC}"
echo -e "${GREEN}========================================${NC}"
