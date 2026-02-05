#!/usr/bin/env bash
#
# td-sync multi-environment deployment script
#
# Usage:
#   ./deploy.sh dev              # Run locally
#   ./deploy.sh staging          # Deploy to staging VPS
#   ./deploy.sh prod             # Deploy to production VPS
#   ./deploy.sh prod --build     # Force rebuild
#   ./deploy.sh prod --logs      # Deploy and tail logs
#   ./deploy.sh prod --dry-run   # Validate only, don't deploy
#   ./deploy.sh prod --status    # Check remote status
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

usage() {
    cat << EOF
Usage: ./deploy.sh <environment> [options]

Environments:
  dev       Run locally with docker compose
  staging   Deploy to staging VPS
  prod      Deploy to production VPS

Options:
  --build     Force rebuild of Docker image
  --logs      Tail logs after deployment
  --dry-run   Validate config only, don't deploy
  --status    Check deployment status
  --stop      Stop the deployment
  --help      Show this help

Setup:
  1. Copy deploy/envs/.env.<env>.example to deploy/envs/.env.<env>
  2. Fill in your values (DEPLOY_HOST, secrets, etc.)
  3. Run ./deploy.sh <env>

Examples:
  ./deploy.sh dev                # Start local dev server
  ./deploy.sh staging --dry-run  # Validate staging config
  ./deploy.sh prod --build       # Deploy prod with fresh build
  ./deploy.sh prod --status      # Check prod status
EOF
    exit 1
}

log() { echo -e "${GREEN}[deploy]${NC} $1"; }
warn() { echo -e "${YELLOW}[warn]${NC} $1"; }
error() { echo -e "${RED}[error]${NC} $1" >&2; exit 1; }

# Validate environment file exists and has required vars
validate_env() {
    local env=$1
    local env_file="$SCRIPT_DIR/envs/.env.$env"

    if [[ ! -f "$env_file" ]]; then
        error "Environment file not found: $env_file
Copy deploy/envs/.env.$env.example to deploy/envs/.env.$env and fill in values"
    fi

    # Source the env file
    set -a
    # shellcheck disable=SC1090
    source "$env_file"
    set +a

    # Required for all environments
    if [[ -z "${SYNC_BASE_URL:-}" ]]; then
        error "SYNC_BASE_URL is required in $env_file"
    fi

    # Remote environments need deploy config
    if [[ "$env" != "dev" ]]; then
        if [[ -z "${DEPLOY_HOST:-}" ]]; then
            error "DEPLOY_HOST is required for $env in $env_file"
        fi
        if [[ -z "${DEPLOY_USER:-}" ]]; then
            error "DEPLOY_USER is required for $env in $env_file"
        fi
        if [[ -z "${DEPLOY_PATH:-}" ]]; then
            error "DEPLOY_PATH is required for $env in $env_file"
        fi
    fi

    # Prod requires S3 backup
    if [[ "$env" == "prod" ]]; then
        if [[ -z "${LITESTREAM_S3_BUCKET:-}" ]]; then
            error "LITESTREAM_S3_BUCKET is required for prod in $env_file"
        fi
        if [[ -z "${AWS_ACCESS_KEY_ID:-}" ]]; then
            error "AWS_ACCESS_KEY_ID is required for prod in $env_file"
        fi
        if [[ -z "${AWS_SECRET_ACCESS_KEY:-}" ]]; then
            error "AWS_SECRET_ACCESS_KEY is required for prod in $env_file"
        fi
    fi

    log "Config validated: $env"
}

# Build compose command with correct files
compose_cmd() {
    local env=$1
    echo "docker compose -f docker-compose.yml -f compose/docker-compose.${env}.yml --env-file envs/.env.${env}"
}

# Deploy locally (dev)
deploy_local() {
    local build_flag=""
    [[ "${FORCE_BUILD:-}" == "1" ]] && build_flag="--build"

    log "Starting local dev environment..."
    cd "$SCRIPT_DIR"

    # shellcheck disable=SC2086
    $(compose_cmd dev) up -d $build_flag

    log "Dev server running at ${SYNC_BASE_URL}"
    log "Health check: curl ${SYNC_BASE_URL}/healthz"

    if [[ "${TAIL_LOGS:-}" == "1" ]]; then
        $(compose_cmd dev) logs -f td-sync
    fi
}

# Stop local deployment
stop_local() {
    log "Stopping local dev environment..."
    cd "$SCRIPT_DIR"
    $(compose_cmd dev) down
    log "Stopped"
}

# Deploy to remote VPS
deploy_remote() {
    local env=$1
    local build_flag=""
    [[ "${FORCE_BUILD:-}" == "1" ]] && build_flag="--build"

    log "Deploying to $env (${DEPLOY_USER}@${DEPLOY_HOST})..."

    # Sync source code (excluding sensitive/large files)
    log "Syncing source code..."
    rsync -avz --delete \
        --exclude '.git' \
        --exclude '.todos' \
        --exclude 'test/' \
        --exclude 'deploy/envs/.env.*' \
        --exclude 'td' \
        --exclude 'td-sync' \
        --exclude 'website/node_modules' \
        --exclude 'website/build' \
        --exclude '*.db' \
        --exclude '*.db-wal' \
        --exclude '*.db-shm' \
        "$REPO_ROOT/" \
        "${DEPLOY_USER}@${DEPLOY_HOST}:${DEPLOY_PATH}/"

    # Copy environment file to server
    log "Syncing environment config..."
    rsync -avz \
        "$SCRIPT_DIR/envs/.env.$env" \
        "${DEPLOY_USER}@${DEPLOY_HOST}:${DEPLOY_PATH}/deploy/envs/.env.$env"

    # Build and start on remote
    log "Building and starting on remote..."
    # shellcheck disable=SC2087
    ssh "${DEPLOY_USER}@${DEPLOY_HOST}" << EOF
cd ${DEPLOY_PATH}/deploy
docker compose -f docker-compose.yml -f compose/docker-compose.${env}.yml --env-file envs/.env.${env} up -d $build_flag
EOF

    # Health check
    log "Waiting for health check..."
    sleep 5
    local health_url="${SYNC_BASE_URL}/healthz"
    if curl -sf "$health_url" > /dev/null 2>&1; then
        log "Health check passed: $health_url"
    else
        warn "Health check pending - server may still be starting"
        warn "Check status: ./deploy.sh $env --status"
    fi

    log "Deployed to $env successfully"

    if [[ "${TAIL_LOGS:-}" == "1" ]]; then
        ssh "${DEPLOY_USER}@${DEPLOY_HOST}" \
            "cd ${DEPLOY_PATH}/deploy && docker compose -f docker-compose.yml -f compose/docker-compose.${env}.yml --env-file envs/.env.${env} logs -f td-sync"
    fi
}

# Stop remote deployment
stop_remote() {
    local env=$1
    log "Stopping $env deployment..."
    ssh "${DEPLOY_USER}@${DEPLOY_HOST}" \
        "cd ${DEPLOY_PATH}/deploy && docker compose -f docker-compose.yml -f compose/docker-compose.${env}.yml --env-file envs/.env.${env} down"
    log "Stopped"
}

# Check deployment status
check_status() {
    local env=$1

    if [[ "$env" == "dev" ]]; then
        cd "$SCRIPT_DIR"
        echo "=== Container Status ==="
        $(compose_cmd dev) ps
        echo ""
        echo "=== Recent Logs ==="
        $(compose_cmd dev) logs --tail=20 td-sync
        echo ""
        echo "=== Health Check ==="
        curl -s "${SYNC_BASE_URL}/healthz" && echo "" || echo "Health check failed"
    else
        # shellcheck disable=SC2087
        ssh "${DEPLOY_USER}@${DEPLOY_HOST}" << EOF
cd ${DEPLOY_PATH}/deploy
echo "=== Container Status ==="
docker compose -f docker-compose.yml -f compose/docker-compose.${env}.yml --env-file envs/.env.${env} ps
echo ""
echo "=== Recent Logs ==="
docker compose -f docker-compose.yml -f compose/docker-compose.${env}.yml --env-file envs/.env.${env} logs --tail=20 td-sync
echo ""
echo "=== Health Check ==="
curl -s http://localhost:8080/healthz && echo "" || echo "Health check failed"
EOF
    fi
}

# Main
main() {
    local env=""
    local dry_run=0
    local status_only=0
    local stop_only=0

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            dev|staging|prod)
                env=$1
                shift
                ;;
            --build)
                FORCE_BUILD=1
                shift
                ;;
            --logs)
                TAIL_LOGS=1
                shift
                ;;
            --dry-run)
                dry_run=1
                shift
                ;;
            --status)
                status_only=1
                shift
                ;;
            --stop)
                stop_only=1
                shift
                ;;
            --help|-h)
                usage
                ;;
            *)
                error "Unknown option: $1"
                ;;
        esac
    done

    [[ -z "$env" ]] && usage

    # Validate environment
    validate_env "$env"

    # Status check only
    if [[ $status_only -eq 1 ]]; then
        check_status "$env"
        exit 0
    fi

    # Stop only
    if [[ $stop_only -eq 1 ]]; then
        if [[ "$env" == "dev" ]]; then
            stop_local
        else
            stop_remote "$env"
        fi
        exit 0
    fi

    # Dry run - just validate
    if [[ $dry_run -eq 1 ]]; then
        log "Dry run completed - configuration is valid"
        exit 0
    fi

    # Deploy
    if [[ "$env" == "dev" ]]; then
        deploy_local
    else
        deploy_remote "$env"
    fi
}

main "$@"
