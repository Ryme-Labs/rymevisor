#!/usr/bin/env bash
set -euo pipefail

# RymeVisor Local Dev Script
# Starts infra (skips if already running) + builds + runs all services

ROOT="$(cd "$(dirname "$0")" && pwd)"
COMPOSE_FILE="$ROOT/deployments/docker/docker-compose.yml"
BIN_DIR="$ROOT/bin"
LOG_DIR="$ROOT/.dev-logs"
PIDS_FILE="$ROOT/.dev-pids"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $*"; }
warn()  { echo -e "${YELLOW}[!]${NC} $*"; }
err()   { echo -e "${RED}[x]${NC} $*" >&2; }
step()  { echo -e "${CYAN}[>]${NC} $*"; }

cleanup() {
  log "Shutting down..."
  if [ -f "$PIDS_FILE" ]; then
    while read -r pid; do
      kill "$pid" 2>/dev/null || true
    done < "$PIDS_FILE"
    rm -f "$PIDS_FILE"
  fi
  log "Stopped."
}
trap cleanup EXIT INT TERM

# ── Load DB URL from /etc/rymevisor/config.env if it exists ──
DB_URL="postgres://rymevisor:rymevisor@localhost:5432/rymevisor?sslmode=disable"
NATS_URL="nats://localhost:4222"
REDIS_ADDR="localhost:6379"
JWT_SECRET="dev-secret-change-in-production"
API_KEY=""

if [ -f /etc/rymevisor/config.env ]; then
  if [ -r /etc/rymevisor/config.env ]; then
    set -a; source /etc/rymevisor/config.env 2>/dev/null || true; set +a
  elif command -v sudo &>/dev/null; then
    # Try via sudo (may prompt for password)
    TMP_ENV=$(sudo cat /etc/rymevisor/config.env 2>/dev/null || true)
    if [ -n "$TMP_ENV" ]; then
      set -a; eval "$TMP_ENV" 2>/dev/null || true; set +a
    fi
  fi
  DB_URL="${RYMEVISOR_DATABASE_URL:-$DB_URL}"
  NATS_URL="${RYMEVISOR_NATS_URL:-$NATS_URL}"
  REDIS_ADDR="${RYMEVISOR_REDIS_ADDR:-$REDIS_ADDR}"
  if [ -n "${RYMEVISOR_JWT_SECRET:-}" ]; then
    JWT_SECRET="$RYMEVISOR_JWT_SECRET"
  fi
  API_KEY="${RYMEVISOR_API_KEY:-$API_KEY}"
fi

# Parse DB_URL for psql (user, password, host, port, db)
parse_db_url() {
  # postgres://user:pass@host:port/db?params
  local url="$DB_URL"
  DB_USER=$(echo "$url" | sed -n 's|.*://\([^:]*\):.*|\1|p')
  DB_PASS=$(echo "$url" | sed -n 's|.*://[^:]*:\([^@]*\)@.*|\1|p')
  DB_HOST=$(echo "$url" | sed -n 's|.*@\([^:]*\):.*|\1|p')
  DB_PORT=$(echo "$url" | sed -n 's|.*:\([0-9]*\)/.*|\1|p')
  DB_NAME=$(echo "$url" | sed -n 's|.*/\([^?]*\).*|\1|p')
  DB_USER="${DB_USER:-rymevisor}"
  DB_PASS="${DB_PASS:-rymevisor}"
  DB_HOST="${DB_HOST:-localhost}"
  DB_PORT="${DB_PORT:-5432}"
  DB_NAME="${DB_NAME:-rymevisor}"
}

# ── Check if a port is in use ─────────────────────────────
port_in_use() {
  (echo >/dev/tcp/localhost/"$1") 2>/dev/null && return 0 || return 1
}

# ── Stop old systemd services that conflict on ports ──────
stop_old_services() {
  # Kill leftover dev services from previous run
  if [ -f "$PIDS_FILE" ]; then
    step "Stopping leftover dev services..."
    while read -r pid; do
      kill "$pid" 2>/dev/null || sudo kill "$pid" 2>/dev/null || true
    done < "$PIDS_FILE"
    rm -f "$PIDS_FILE"
    sleep 1
  fi

  if command -v systemctl &>/dev/null; then
    local svcs=(rymevisor-control-plane rymevisor-api-gateway rymevisor-scheduler rymevisor-networking-engine rymevisor-storage-manager rymevisor-node-agent)
    local running=()
    for svc in "${svcs[@]}"; do
      if systemctl is-active --quiet "$svc" 2>/dev/null; then
        running+=("$svc")
      fi
    done
    if [ ${#running[@]} -gt 0 ]; then
      step "Stopping old systemd services that would conflict on ports: ${running[*]}"
      if [ "$EUID" -eq 0 ]; then
        systemctl stop "${running[@]}" 2>/dev/null || true
      else
        sudo systemctl stop "${running[@]}" 2>/dev/null || true
      fi
      sleep 2
    fi
  fi
}

# ── Start infra (skip what's already running) ─────────────
start_infra() {
  step "Checking infrastructure..."

  local PG_PORT=5432 RED_PORT=6379 NATS_PORT=4222 MINIO_PORT=9000
  local NEED_DOCKER=false
  local DOCKER_SERVICES=()

  if port_in_use $PG_PORT; then
    log "PostgreSQL already running on :$PG_PORT"
  else
    NEED_DOCKER=true
    DOCKER_SERVICES+=(postgres)
  fi

  if port_in_use $RED_PORT; then
    log "Redis already running on :$RED_PORT"
  else
    NEED_DOCKER=true
    DOCKER_SERVICES+=(redis)
  fi

  if port_in_use $NATS_PORT; then
    log "NATS already running on :$NATS_PORT"
  else
    NEED_DOCKER=true
    DOCKER_SERVICES+=(nats)
  fi

  if port_in_use $MINIO_PORT; then
    log "MinIO already running on :$MINIO_PORT"
  else
    NEED_DOCKER=true
    DOCKER_SERVICES+=(minio)
  fi

  if [ "$NEED_DOCKER" = true ]; then
    step "Starting Docker containers: ${DOCKER_SERVICES[*]}..."
    docker compose -f "$COMPOSE_FILE" up -d "${DOCKER_SERVICES[@]}"
    sleep 2

    for svc in "${DOCKER_SERVICES[@]}"; do
      step "Waiting for $svc..."
      local i=0
      case "$svc" in
        postgres)
          until docker compose -f "$COMPOSE_FILE" exec -T postgres pg_isready -U "$DB_USER" >/dev/null 2>&1; do
            sleep 1; i=$((i + 1)); [ $i -ge 30 ] && { err "PostgreSQL not ready"; exit 1; }
          done
          log "  PostgreSQL ready"
          ;;
        redis)
          until docker compose -f "$COMPOSE_FILE" exec -T redis redis-cli ping >/dev/null 2>&1; do
            sleep 1; i=$((i + 1)); [ $i -ge 15 ] && { err "Redis not ready"; exit 1; }
          done
          log "  Redis ready"
          ;;
        nats)
          until docker compose -f "$COMPOSE_FILE" exec -T nats nats-server --signal ldm=/data >/dev/null 2>&1; do
            sleep 1; i=$((i + 1)); [ $i -ge 15 ] && { err "NATS not ready"; exit 1; }
          done
          log "  NATS ready"
          ;;
        minio)
          log "  MinIO starting (non-blocking)"
          ;;
      esac
    done
  fi
}

# ── Database migrations ───────────────────────────────────
run_migrations() {
  step "Running database migrations..."
  parse_db_url

  local USE_DOCKER=false
  if docker compose -f "$COMPOSE_FILE" ps postgres 2>/dev/null | grep -q "running"; then
    USE_DOCKER=true
  fi

  for dir in "$ROOT/migrations"/*/; do
    for f in "$dir"*.up.sql; do
      [ -f "$f" ] || continue
      log "  $(basename "$f")"
      if [ "$USE_DOCKER" = true ]; then
        docker compose -f "$COMPOSE_FILE" exec -T postgres psql -U "$DB_USER" -d "$DB_NAME" < "$f" >/dev/null 2>&1 || true
      else
        PGPASSWORD="$DB_PASS" psql -U "$DB_USER" -d "$DB_NAME" -h "$DB_HOST" -p "$DB_PORT" < "$f" >/dev/null 2>&1 || true
      fi
    done
  done
  log "Migrations complete"
}

# ── Build services ────────────────────────────────────────
build_services() {
  step "Building services..."
  mkdir -p "$BIN_DIR"

  local failed=0
  local services=(control-plane api-gateway scheduler networking-engine storage-manager node-agent)
  for svc in "${services[@]}"; do
    if [ -f "$ROOT/cmd/$svc/main.go" ]; then
      log "  Building $svc..."
      if ! CGO_ENABLED=0 go build -o "$BIN_DIR/$svc" "$ROOT/cmd/$svc" 2>&1; then
        warn "  Failed to build $svc"
        failed=1
      fi
    fi
  done

  if [ $failed -eq 1 ]; then
    err "Some services failed to build"
    exit 1
  fi
  log "Build complete"
}

# ── Start services ────────────────────────────────────────
start_services() {
  step "Starting services..."
  mkdir -p "$LOG_DIR"
  rm -f "$PIDS_FILE"
  touch "$PIDS_FILE"

  # control-plane :8080
  if [ -f "$BIN_DIR/control-plane" ]; then
    RYMEVISOR_DATABASE_URL="$DB_URL" RYMEVISOR_NATS_URL="$NATS_URL" RYMEVISOR_REDIS_ADDR="$REDIS_ADDR" RYMEVISOR_JWT_SECRET="$JWT_SECRET" RYMEVISOR_LOG_LEVEL=debug RYMEVISOR_LOG_FORMAT=console RYMEVISOR_SERVER_ADDR=":8080" "$BIN_DIR/control-plane" > "$LOG_DIR/control-plane.log" 2>&1 &
    echo $! >> "$PIDS_FILE"
    log "  control-plane  -> :8080"
  fi

  # scheduler :8083
  if [ -f "$BIN_DIR/scheduler" ]; then
    RYMEVISOR_DATABASE_URL="$DB_URL" RYMEVISOR_NATS_URL="$NATS_URL" RYMEVISOR_REDIS_ADDR="$REDIS_ADDR" RYMEVISOR_JWT_SECRET="$JWT_SECRET" RYMEVISOR_LOG_LEVEL=debug RYMEVISOR_LOG_FORMAT=console RYMEVISOR_SERVER_ADDR=":8083" "$BIN_DIR/scheduler" > "$LOG_DIR/scheduler.log" 2>&1 &
    echo $! >> "$PIDS_FILE"
    log "  scheduler      -> :8083"
  fi

  # networking-engine :8084
  if [ -f "$BIN_DIR/networking-engine" ]; then
    RYMEVISOR_DATABASE_URL="$DB_URL" RYMEVISOR_NATS_URL="$NATS_URL" RYMEVISOR_REDIS_ADDR="$REDIS_ADDR" RYMEVISOR_JWT_SECRET="$JWT_SECRET" RYMEVISOR_LOG_LEVEL=debug RYMEVISOR_LOG_FORMAT=console RYMEVISOR_SERVER_ADDR=":8084" "$BIN_DIR/networking-engine" > "$LOG_DIR/networking-engine.log" 2>&1 &
    echo $! >> "$PIDS_FILE"
    log "  networking     -> :8084"
  fi

  # storage-manager :8085
  if [ -f "$BIN_DIR/storage-manager" ]; then
    RYMEVISOR_DATABASE_URL="$DB_URL" RYMEVISOR_NATS_URL="$NATS_URL" RYMEVISOR_REDIS_ADDR="$REDIS_ADDR" RYMEVISOR_JWT_SECRET="$JWT_SECRET" RYMEVISOR_LOG_LEVEL=debug RYMEVISOR_LOG_FORMAT=console RYMEVISOR_SERVER_ADDR=":8085" "$BIN_DIR/storage-manager" > "$LOG_DIR/storage-manager.log" 2>&1 &
    echo $! >> "$PIDS_FILE"
    log "  storage        -> :8085"
  fi

  # api-gateway :8081 (start last, proxies to all above)
  if [ -f "$BIN_DIR/api-gateway" ]; then
    RYMEVISOR_DATABASE_URL="$DB_URL" RYMEVISOR_NATS_URL="$NATS_URL" RYMEVISOR_REDIS_ADDR="$REDIS_ADDR" RYMEVISOR_JWT_SECRET="$JWT_SECRET" RYMEVISOR_LOG_LEVEL=debug RYMEVISOR_LOG_FORMAT=console RYMEVISOR_SERVER_ADDR=":8081" RYMEVISOR_CONTROL_PLANE_URL="localhost:8080" RYMEVISOR_NETWORK_URL="localhost:8084" RYMEVISOR_STORAGE_URL="localhost:8085" RYMEVISOR_SCHEDULER_URL="localhost:8083" "$BIN_DIR/api-gateway" > "$LOG_DIR/api-gateway.log" 2>&1 &
    echo $! >> "$PIDS_FILE"
    log "  api-gateway    -> :8081"
  fi

  # node-agent (no HTTP, uses NATS)
  if [ -f "$BIN_DIR/node-agent" ]; then
    RYMEVISOR_DATABASE_URL="$DB_URL" RYMEVISOR_NATS_URL="$NATS_URL" RYMEVISOR_REDIS_ADDR="$REDIS_ADDR" RYMEVISOR_JWT_SECRET="$JWT_SECRET" RYMEVISOR_LOG_LEVEL=debug RYMEVISOR_LOG_FORMAT=console RYMEVISOR_NODE_ID="node-1" "$BIN_DIR/node-agent" > "$LOG_DIR/node-agent.log" 2>&1 &
    echo $! >> "$PIDS_FILE"
    log "  node-agent     -> (NATS only)"
  fi

  log "All services started"
}

# ── Status check ──────────────────────────────────────────
check_status() {
  echo ""
  step "Checking health (waiting 5s for services to boot)..."

  sleep 5

  # api-gateway uses /health, others use /health/live
  check_one() {
    local name=$1 port=$2 path=$3
    if curl -sf "http://localhost:$port$path" >/dev/null 2>&1; then
      log "  $name (:$port$path) - healthy"
    else
      warn "  $name (:$port$path) - not responding yet (check $LOG_DIR/$name.log)"
    fi
  }

  check_one "control-plane" "8080" "/health/live"
  check_one "scheduler" "8083" "/health/live"
  check_one "networking-engine" "8084" "/health/live"
  check_one "storage-manager" "8085" "/health/live"
  check_one "api-gateway" "8081" "/health"

  echo ""
  if [ -n "$API_KEY" ]; then
    log "API Key: $API_KEY"
    log "Usage: curl -H 'X-API-Key: $API_KEY' http://localhost:8081/api/v1/vms"
    echo ""
  fi
  log "Logs:  tail -f $LOG_DIR/<service>.log"
  log "Stop:  Ctrl+C or run: kill \$(cat $PIDS_FILE)"
  echo ""
}

# ── Main ──────────────────────────────────────────────────
main() {
  echo ""
  log "RymeVisor Local Dev"
  echo ""
  log "DB: $DB_URL" | sed 's/:[^:]*@/:****@/'

  if ! command -v go &>/dev/null; then
    err "Go not found. Install from https://go.dev/dl/"
    exit 1
  fi

  stop_old_services
  start_infra
  run_migrations
  build_services
  start_services
  check_status

  log "Running... Press Ctrl+C to stop."
  wait
}

main "$@"
