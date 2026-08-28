#!/usr/bin/env bash
#
# RymeVisor Installer
# Originally built as an internal tool for Ryme Labs, now open-sourced.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Ryme-Labs/rymevisor/main/install.sh | sudo bash
#
# Or run directly:
#   sudo bash install.sh [install|update|uninstall|status]
#

set -euo pipefail

# ============================================================
# Configuration
# ============================================================

RYMEVISOR_VERSION="${RYMEVISOR_VERSION:-latest}"
RYMEVISOR_HOME="/var/lib/rymevisor"
RYMEVISOR_CONFIG="/etc/rymevisor"
RYMEVISOR_BIN="/usr/local/bin"
RYMEVISOR_USER="rymevisor"
RYMEVISOR_GROUP="rymevisor"

# Default ports
PORT_API_GATEWAY=8080
PORT_CONTROL_PLANE=8081
PORT_SCHEDULER=8085
PORT_NETWORKING=8083
PORT_STORAGE=8084

# Database
DB_NAME="rymevisor"
DB_USER="rymevisor"
DB_PASS=""
DB_HOST="localhost"
DB_PORT=5432

# Redis
REDIS_HOST="localhost"
REDIS_PORT=6379
REDIS_PASS=""

# NATS
NATS_HOST="localhost"
NATS_PORT=4222

# Reverse proxy
DOMAIN=""
ENABLE_TLS=false
TLS_EMAIL=""
REVERSE_PROXY=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# ============================================================
# Utility Functions
# ============================================================

log_info()    { echo -e "${GREEN}[INFO]${NC} $*"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }
log_step()    { echo -e "${CYAN}[STEP]${NC} $*"; }
log_secret()  { echo -e "${YELLOW}[SECRET]${NC} $*"; }

confirm() {
  local prompt="$1"
  local default="${2:-n}"
  local yn

  if [ "$default" = "y" ]; then
    prompt="$prompt [Y/n]: "
  else
    prompt="$prompt [y/N]: "
  fi

  read -rp "$prompt" yn
  case "${yn:-$default}" in
    [yY][eE][sS]|[yY]) return 0 ;;
    *) return 1 ;;
  esac
}

ask() {
  local prompt="$1"
  local default="${2:-}"
  local result

  if [ -n "$default" ]; then
    read -rp "$prompt [$default]: " result
    echo "${result:-$default}"
  else
    read -rp "$prompt: " result
    echo "$result"
  fi
}

generate_password() {
  openssl rand -base64 32 | tr -d '/+=' | head -c 32
}

check_root() {
  if [ "$EUID" -ne 0 ]; then
    log_error "This script must be run as root (use sudo)"
    exit 1
  fi
}

detect_distro() {
  if [ -f /etc/os-release ]; then
    . /etc/os-release
    echo "$ID"
  else
    log_error "Cannot detect Linux distribution"
    exit 1
  fi
}

check_virtualization() {
  if ! grep -qE 'vmx|svm' /proc/cpuinfo 2>/dev/null; then
    log_warn "Hardware virtualization (VT-x/AMD-V) not detected"
    log_warn "VMs will run in software emulation mode (much slower)"
    if ! confirm "Continue anyway?"; then
      exit 1
    fi
  fi
}

wait_for_port() {
  local port=$1
  local timeout=${2:-30}
  local i=0
  while ! curl -sf "http://localhost:${port}/health" >/dev/null 2>&1; do
    sleep 1
    i=$((i + 1))
    if [ $i -ge $timeout ]; then
      return 1
    fi
  done
  return 0
}

# ============================================================
# Dependency Installation
# ============================================================

install_deps_ubuntu() {
  apt-get update -qq
  apt-get install -y -qq \
    curl wget git unzip jq \
    qemu-system-x86_64 qemu-utils \
    libvirt-daemon-system libvirt-clients \
    bridge-utils \
    genisoimage \
    nftables \
    docker.io docker-compose \
    gnupg2 lsb-release \
    software-properties-common \
    apt-transport-https ca-certificates

  systemctl enable --now docker
  log_info "System dependencies installed"
}

install_deps_debian() {
  apt-get update -qq
  apt-get install -y -qq \
    curl wget git unzip jq \
    qemu-system-x86_64 qemu-utils \
    libvirt-daemon-system libvirt-clients \
    bridge-utils \
    genisoimage \
    nftables \
    docker.io docker-compose \
    gnupg2 lsb-release

  systemctl enable --now docker
  log_info "System dependencies installed"
}

install_deps_fedora() {
  dnf install -y -q \
    curl wget git unzip jq \
    qemu-kvm qemu-img \
    libvirt libvirt-client \
    bridge-utils \
    genisoimage \
    nftables \
    docker docker-compose

  systemctl enable --now docker
  log_info "System dependencies installed"
}

install_deps_rocky() {
  dnf install -y -q \
    curl wget git unzip jq \
    qemu-kvm qemu-img \
    libvirt libvirt-client \
    bridge-utils \
    genisoimage \
    nftables \
    docker docker-compose

  systemctl enable --now docker
  log_info "System dependencies installed"
}

install_deps_arch() {
  pacman -Syu --noconfirm 2>&1 | tail -3 || true

  local PACKAGES=(
    curl wget git unzip jq
    qemu-system-x86
    libvirt
    nftables
    docker docker-compose
  )

  for pkg in "${PACKAGES[@]}"; do
    if ! pacman -Q "$pkg" &>/dev/null; then
      log_step "Installing $pkg..."
      yes "" | pacman -S --noconfirm "$pkg" 2>&1 | tail -2 || true
    fi
  done

  systemctl enable --now docker 2>/dev/null || true

  # Verify critical packages
  local missing=""
  for pkg in qemu-system-x86 docker; do
    if ! pacman -Q "$pkg" &>/dev/null; then
      missing="$missing $pkg"
    fi
  done

  if [ -n "$missing" ]; then
    log_error "Failed to install:$missing"
    return 1
  fi

  log_info "System dependencies installed"
}

install_nats() {
  if command -v nats-server &>/dev/null; then
    log_info "NATS already installed"
    return
  fi

  log_step "Installing NATS JetStream..."
  local ARCH
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
  esac

  local NATS_VERSION="2.10.24"
  curl -fsSL "https://github.com/nats-io/nats-server/releases/download/v${NATS_VERSION}/nats-server-v${NATS_VERSION}-linux-${ARCH}.tar.gz" \
    | tar xz -C /tmp
  mv /tmp/nats-server-v${NATS_VERSION}-linux-${ARCH}/nats-server /usr/local/bin/nats-server
  chmod +x /usr/local/bin/nats-server
  rm -rf /tmp/nats-server-*

  cat > /etc/systemd/system/nats.service << 'NATS_SERVICE'
[Unit]
Description=NATS JetStream Server
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/nats-server --jetstream --store_dir /var/lib/nats
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
NATS_SERVICE

  systemctl daemon-reload
  systemctl enable nats
  systemctl start nats
  log_info "NATS installed and started"
}

# ============================================================
# User & Directory Setup
# ============================================================

setup_user() {
  if ! id "$RYMEVISOR_USER" &>/dev/null; then
    useradd --system --shell /bin/false --home-dir "$RYMEVISOR_HOME" "$RYMEVISOR_USER" 2>/dev/null || true
    log_info "Created user: $RYMEVISOR_USER"
  fi
}

setup_directories() {
  mkdir -p "$RYMEVISOR_HOME"/{vms,images,backups,cloud-init,disks}
  mkdir -p "$RYMEVISOR_CONFIG"
  mkdir -p /var/log/rymevisor
  mkdir -p /var/lib/nats

  chown -R "$RYMEVISOR_USER:$RYMEVISOR_GROUP" "$RYMEVISOR_HOME"
  chown -R "$RYMEVISOR_USER:$RYMEVISOR_GROUP" /var/log/rymevisor
  chown -R "$RYMEVISOR_USER:$RYMEVISOR_GROUP" /var/lib/nats
}

# ============================================================
# Infrastructure (Docker PostgreSQL + Redis)
# ============================================================

start_infra() {
  log_step "Starting infrastructure containers..."

  # Check if containers already exist
  local PG_EXISTS=$(docker ps -a --format '{{.Names}}' 2>/dev/null | grep -c '^rymevalor-postgres$' || true)
  local RED_EXISTS=$(docker ps -a --format '{{.Names}}' 2>/dev/null | grep -c '^rymevalor-redis$' || true)

  # Find available ports
  local PG_PORT=$DB_PORT
  local RED_PORT=$REDIS_PORT

  if ss -tln | grep -q ":${PG_PORT} "; then
    log_warn "Port $PG_PORT in use, trying 5433..."
    PG_PORT=5433
  fi

  if ss -tln | grep -q ":${RED_PORT} "; then
    log_warn "Port $RED_PORT in use, trying 6380..."
    RED_PORT=6380
  fi

  if [ "$PG_EXISTS" -gt 0 ]; then
    log_info "PostgreSQL container exists, starting..."
    docker start rymevalor-postgres >/dev/null 2>&1 || true
    # Read existing password from container
    DB_PASS=$(docker exec rymevalor-postgres printenv POSTGRES_PASSWORD 2>/dev/null || echo "")
  else
    # Generate password if not set
    if [ -z "$DB_PASS" ]; then
      DB_PASS=$(generate_password)
    fi
    log_step "Pulling PostgreSQL image..."
    docker pull postgres:16-alpine 2>&1 | tail -1
    log_step "Starting PostgreSQL on port $PG_PORT..."
    docker run -d \
      --name rymevalor-postgres \
      --restart unless-stopped \
      -e POSTGRES_USER="$DB_USER" \
      -e POSTGRES_PASSWORD="$DB_PASS" \
      -e POSTGRES_DB="$DB_NAME" \
      -p "${PG_PORT}:5432" \
      -v rymevalor-pgdata:/var/lib/postgresql/data \
      postgres:16-alpine >/dev/null
  fi

  if [ "$RED_EXISTS" -gt 0 ]; then
    log_info "Redis container exists, starting..."
    docker start rymevalor-redis >/dev/null 2>&1 || true
  else
    log_step "Pulling Redis image..."
    docker pull redis:7-alpine 2>&1 | tail -1
    log_step "Starting Redis on port $RED_PORT..."
    local REDIS_ARGS=""
    if [ -n "$REDIS_PASS" ]; then
      REDIS_ARGS="--requirepass $REDIS_PASS"
    fi
    docker run -d \
      --name rymevalor-redis \
      --restart unless-stopped \
      -p "${RED_PORT}:6379" \
      -v rymevalor-redisdata:/data \
      redis:7-alpine redis-server $REDIS_ARGS >/dev/null
  fi

  # Update ports for config
  DB_PORT=$PG_PORT
  REDIS_PORT=$RED_PORT

  # Wait for PostgreSQL to be ready
  log_step "Waiting for PostgreSQL to be ready..."
  local retries=0
  while ! docker exec rymevalor-postgres pg_isready -U "$DB_USER" >/dev/null 2>&1; do
    sleep 1
    retries=$((retries + 1))
    if [ $retries -ge 30 ]; then
      log_error "PostgreSQL failed to start within 30 seconds"
      docker logs rymevalor-postgres --tail 20
      return 1
    fi
  done

  log_info "Infrastructure containers running (PostgreSQL + Redis)"
}

# ============================================================
# Database Migrations
# ============================================================

run_migrations() {
  log_step "Running database migrations..."

  local MIGRATION_DIR="/home/deyo/Rymelabs/rymevisor/migrations"
  if [ ! -d "$MIGRATION_DIR" ]; then
    MIGRATION_DIR="/opt/rymevisor-source/migrations"
    if [ ! -d "$MIGRATION_DIR" ]; then
      log_warn "Migration files not found, skipping migrations"
      return 0
    fi
  fi

  for migration_dir in "$MIGRATION_DIR"/*/; do
    for migration_file in "$migration_dir"*.up.sql; do
      if [ -f "$migration_file" ]; then
        log_step "Running $(basename $migration_file)..."
        docker exec -i rymevalor-postgres psql -U "$DB_USER" -d "$DB_NAME" < "$migration_file" 2>/dev/null || true
      fi
    done
  done

  log_info "Migrations complete"
}

# ============================================================
# Binary Installation
# ============================================================

install_binaries() {
  log_step "Installing RymeVisor binaries..."

  local ARCH
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
  esac

  local SERVICES=("control-plane" "api-gateway" "scheduler" "networking-engine" "storage-manager" "node-agent")
  local ANY_INSTALLED=false

  # 1. Try building from local source first
  local LOCAL_SRC=""
  if [ -f "./go.mod" ] && [ -d "./cmd" ]; then
    LOCAL_SRC="."
  elif [ -f "$(dirname "$0")/go.mod" ]; then
    LOCAL_SRC="$(dirname "$0")"
  fi

  if [ -n "$LOCAL_SRC" ] && command -v go &>/dev/null; then
    log_step "Building from local source..."
    for service in "${SERVICES[@]}"; do
      local dest="${RYMEVISOR_BIN}/rymevisor-${service}"
      if [ -f "${LOCAL_SRC}/cmd/${service}/main.go" ]; then
        (cd "$LOCAL_SRC" && CGO_ENABLED=0 go build -o "$dest" "./cmd/${service}" 2>/dev/null) && \
          log_info "Built: rymevalor-${service}" || \
          log_warn "Failed to build ${service}"
        ANY_INSTALLED=true
      fi
    done
  fi

  # 2. Try downloading from GitHub releases
  if [ "$ANY_INSTALLED" = false ]; then
    local VERSION="$RYMEVISOR_VERSION"
    if [ "$VERSION" = "latest" ]; then
      VERSION=$(curl -fsSL https://api.github.com/repos/Ryme-Labs/rymevisor/releases/latest | jq -r '.tag_name' 2>/dev/null || echo "")
    fi

    if [ -n "$VERSION" ]; then
      local BASE_URL="https://github.com/Ryme-Labs/rymevisor/releases/download/${VERSION}"
      for service in "${SERVICES[@]}"; do
        local url="${BASE_URL}/${service}-linux-${ARCH}"
        local dest="${RYMEVISOR_BIN}/rymevisor-${service}"
        if curl -fsSL "$url" -o "$dest" 2>/dev/null; then
          chmod +x "$dest"
          ANY_INSTALLED=true
          log_info "Downloaded: rymevalor-${service}"
        fi
      done
    fi
  fi

  # 3. Fall back to cloning and building
  if [ "$ANY_INSTALLED" = false ]; then
    log_warn "Building from GitHub source..."
    local SRC_DIR="/opt/rymevisor-source"

    if ! command -v go &>/dev/null; then
      log_step "Installing Go..."
      local GO_VERSION="1.23.6"
      curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" | tar xz -C /usr/local
      export PATH=$PATH:/usr/local/go/bin
    fi

    git clone --depth 1 https://github.com/Ryme-Labs/rymevisor.git "$SRC_DIR" 2>/dev/null || true

    if [ -f "${SRC_DIR}/go.mod" ]; then
      (cd "$SRC_DIR" && go mod tidy 2>/dev/null)
      for service in "${SERVICES[@]}"; do
        local dest="${RYMEVISOR_BIN}/rymevisor-${service}"
        if [ -f "${SRC_DIR}/cmd/${service}/main.go" ]; then
          (cd "$SRC_DIR" && CGO_ENABLED=0 go build -o "$dest" "./cmd/${service}" 2>/dev/null) && \
            log_info "Built: rymevalor-${service}" || \
            log_warn "Failed to build ${service}"
        fi
      done
    fi
  fi

  ln -sf "${RYMEVISOR_BIN}/rymevisor-api-gateway" "${RYMEVISOR_BIN}/rymevisor" 2>/dev/null || true

  if [ "$ANY_INSTALLED" = false ]; then
    log_error "No binaries installed"
    return 1
  fi
}

# ============================================================
# Configuration Generation
# ============================================================

generate_config() {
  log_step "Generating configuration..."

  # Ensure we have an API key
  if [ -z "${API_KEY:-}" ]; then
    API_KEY=$(generate_password)
    log_warn "No API key set, generated new one"
  fi

  cat > "${RYMEVISOR_CONFIG}/config.env" << EOF
# RymeVisor Configuration
# Generated by install.sh on $(date -Iseconds)

# Server
RYMEVISOR_SERVER_ADDR=:${PORT_API_GATEWAY}
RYMEVISOR_SERVER_READ_TIMEOUT=30s
RYMEVISOR_SERVER_WRITE_TIMEOUT=30s

# Database
RYMEVISOR_DATABASE_URL=postgres://${DB_USER}:${DB_PASS}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable

# Redis
RYMEVISOR_REDIS_ADDR=${REDIS_HOST}:${REDIS_PORT}
RYMEVISOR_REDIS_PASSWORD=${REDIS_PASS}

# NATS
RYMEVISOR_NATS_URL=nats://${NATS_HOST}:${NATS_PORT}

# API Key
RYMEVISOR_API_KEY=${API_KEY}

# Storage
RYMEVISOR_IMAGES_PATH=${RYMEVISOR_HOME}/images
RYMEVISOR_BACKUPS_PATH=${RYMEVISOR_HOME}/backups

# Logging
RYMEVISOR_LOG_LEVEL=info
RYMEVISOR_LOG_FORMAT=json

# Node
RYMEVISOR_NODE_ID=node-1
RYMEVISOR_NODE_HEARTBEAT=10s

# Monitoring
RYMEVISOR_PROMETHEUS_ENABLED=true
RYMEVISOR_METRICS_PATH=/metrics

# Downstream service URLs (for API gateway proxy)
RYMEVISOR_CONTROL_PLANE_URL=localhost:${PORT_CONTROL_PLANE}
RYMEVISOR_NETWORK_URL=localhost:${PORT_NETWORKING}
RYMEVISOR_STORAGE_URL=localhost:${PORT_STORAGE}
RYMEVISOR_SCHEDULER_URL=localhost:${PORT_SCHEDULER}
EOF

  chmod 600 "${RYMEVISOR_CONFIG}/config.env"
  chown root:${RYMEVISOR_GROUP} "${RYMEVISOR_CONFIG}/config.env"
  chmod 640 "${RYMEVISOR_CONFIG}/config.env"

  # Save version
  echo "$RYMEVISOR_VERSION" > "${RYMEVISOR_CONFIG}/VERSION"
  chmod 600 "${RYMEVISOR_CONFIG}/VERSION"

  log_info "Configuration written to ${RYMEVISOR_CONFIG}/config.env"
}

# ============================================================
# Systemd Services
# ============================================================

create_services() {
  log_step "Creating systemd services..."

  # Ensure user exists first
  if ! id "$RYMEVISOR_USER" &>/dev/null; then
    useradd --system --shell /bin/false --home-dir "$RYMEVISOR_HOME" "$RYMEVISOR_USER" 2>/dev/null || true
    log_info "Created user: $RYMEVISOR_USER"
  fi

  # Ensure directories exist with correct ownership
  mkdir -p "$RYMEVISOR_HOME"/{vms,images,backups,cloud-init,disks} /var/log/rymevisor /var/lib/nats
  chown -R "$RYMEVISOR_USER:$RYMEVISOR_GROUP" "$RYMEVISOR_HOME" /var/log/rymevisor /var/lib/nats 2>/dev/null || true

  local SERVICES=(
    "control-plane:${PORT_CONTROL_PLANE}"
    "scheduler:${PORT_SCHEDULER}"
    "networking-engine:${PORT_NETWORKING}"
    "storage-manager:${PORT_STORAGE}"
    "api-gateway:${PORT_API_GATEWAY}"
    "node-agent:0"
  )

  for entry in "${SERVICES[@]}"; do
    local name="${entry%%:*}"
    local port="${entry##*:}"
    local binary="${RYMEVISOR_BIN}/rymevisor-${name}"

    local extra_env=""
    if [ "$port" != "0" ]; then
      extra_env="Environment=RYMEVISOR_SERVER_ADDR=:${port}"
    fi

    cat > "/etc/systemd/system/rymevisor-${name}.service" << EOF
[Unit]
Description=RymeVisor ${name}
After=network.target docker.service nats.service
Wants=docker.service nats.service

[Service]
Type=simple
User=${RYMEVISOR_USER}
Group=${RYMEVISOR_GROUP}
EnvironmentFile=${RYMEVISOR_CONFIG}/config.env
${extra_env}
ExecStart=${binary}
Restart=always
RestartSec=5
LimitNOFILE=65535
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
EOF
  done

  # Force systemd to reload and recognize new units
  systemctl daemon-reexec 2>/dev/null || true
  sleep 1
  systemctl daemon-reload

  for entry in "${SERVICES[@]}"; do
    local name="${entry%%:*}"
    systemctl enable "rymevisor-${name}" 2>/dev/null || true
  done

  log_info "Systemd services created and enabled"
}

# ============================================================
# Reverse Proxy Setup
# ============================================================

setup_nginx() {
  log_step "Setting up Nginx reverse proxy..."

  if ! command -v nginx &>/dev/null; then
    apt-get install -y -qq nginx > /dev/null 2>&1 || dnf install -y -q nginx > /dev/null 2>&1 || \
    pacman -S --noconfirm nginx > /dev/null 2>&1 || true
  fi

  mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled 2>/dev/null || true

  if [ -n "$DOMAIN" ]; then
    cat > /etc/nginx/sites-available/rymevisor << NGINX_CONF
# RymeVisor - Auto-generated by install.sh
# Originally built as an internal tool for Ryme Labs

upstream rymevalor_backend {
    server 127.0.0.1:${PORT_API_GATEWAY};
}

server {
    listen 80;
    server_name ${DOMAIN};

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;

    location / {
        proxy_pass http://rymevisor_backend;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";

        proxy_read_timeout 86400;
        proxy_send_timeout 86400;
    }

    location /health {
        proxy_pass http://rymevisor_backend/health;
        access_log off;
    }
}
NGINX_CONF

    ln -sf /etc/nginx/sites-available/rymevisor /etc/nginx/sites-enabled/ 2>/dev/null || true

    if [ "$ENABLE_TLS" = true ] && [ -n "$TLS_EMAIL" ]; then
      apt-get install -y -qq certbot python3-certbot-nginx > /dev/null 2>&1 || true
      certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos -m "$TLS_EMAIL" || true
    fi
  else
    cat > /etc/nginx/sites-available/rymevisor << NGINX_CONF
server {
    listen 80;
    server_name _;

    location / {
        proxy_pass http://127.0.0.1:${PORT_API_GATEWAY};
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
        proxy_send_timeout 86400;
    }
}
NGINX_CONF

    ln -sf /etc/nginx/sites-available/rymevisor /etc/nginx/sites-enabled/ 2>/dev/null || true
  fi

  nginx -t 2>/dev/null && systemctl reload nginx 2>/dev/null || true
  log_info "Nginx configured"
}

setup_caddy() {
  log_step "Setting up Caddy reverse proxy..."

  if ! command -v caddy &>/dev/null; then
    curl -fsSL https://caddyserver.com/api/download?os=linux&arch=amd64 -o /usr/local/bin/caddy
    chmod +x /usr/local/bin/caddy
  fi

  local CADDY_CONFIG
  if [ -n "$DOMAIN" ]; then
    CADDY_CONFIG="${DOMAIN} {
    reverse_proxy localhost:${PORT_API_GATEWAY}
}"
  else
    CADDY_CONFIG=":80 {
    reverse_proxy localhost:${PORT_API_GATEWAY}
}"
  fi

  echo "$CADDY_CONFIG" > /etc/caddy/Caddyfile

  cat > /etc/systemd/system/caddy.service << 'CADDY_SERVICE'
[Unit]
Description=Caddy
After=network.target

[Service]
ExecStart=/usr/local/bin/caddy run --config /etc/caddy/Caddyfile
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
CADDY_SERVICE

  systemctl daemon-reload
  systemctl enable caddy
  systemctl start caddy
  log_info "Caddy configured"
}

# ============================================================
# Firewall Setup
# ============================================================

setup_firewall() {
  log_step "Configuring firewall..."

  if command -v nft &>/dev/null; then
    mkdir -p /etc/nftables.d
    cat > /etc/nftables.d/rymevisor.nft << EOF
# RymeVisor firewall rules
table inet rymevalor {
    chain input {
        type filter hook input priority 0; policy accept;

        # API Gateway
        tcp dport ${PORT_API_GATEWAY} accept
        # NATS
        tcp dport ${NATS_PORT} accept
        # SSH (keep existing)
        tcp dport 22 accept
    }
}
EOF
    nft -f /etc/nftables.d/rymevisor.nft 2>/dev/null || true
  fi

  log_info "Firewall configured"
}

# ============================================================
# Main Install Flow
# ============================================================

do_install() {
  echo ""
  log_info "RymeVisor Installer v${RYMEVISOR_VERSION}"
  echo ""

  check_root
  local DISTRO
  DISTRO=$(detect_distro)

  log_info "Detected distribution: $DISTRO"
  check_virtualization

  # Interactive configuration
  if [ -t 0 ]; then
    echo ""
    log_step "Interactive Configuration"
    echo "Press Enter to accept defaults shown in [brackets]"
    echo ""

    DOMAIN=$(ask "Domain name (leave empty for IP-only)" "")
    if [ -n "$DOMAIN" ]; then
      ENABLE_TLS=$(confirm "Enable TLS/SSL with Let's Encrypt?" "y")
      if [ "$ENABLE_TLS" = true ]; then
        TLS_EMAIL=$(ask "Email for Let's Encrypt" "admin@${DOMAIN}")
      fi
    fi

    DB_PASS=$(ask "Database password (leave empty to auto-generate)" "")
    REDIS_PASS=$(ask "Redis password (leave empty for no auth)" "")

    REVERSE_PROXY=$(ask "Reverse proxy [nginx/caddy/none]" "nginx")

    echo ""
    log_info "Installation summary:"
    echo "  Domain: ${DOMAIN:-none (IP only)}"
    echo "  TLS: $ENABLE_TLS"
    echo "  Reverse proxy: $REVERSE_PROXY"
    echo "  Database: PostgreSQL (Docker) @ $DB_HOST:$DB_PORT"
    echo "  Cache: Redis (Docker) @ $REDIS_HOST:$REDIS_PORT"
    echo "  Messaging: NATS @ $NATS_HOST:$NATS_PORT"
    echo ""

    if ! confirm "Proceed with installation?"; then
      log_info "Installation cancelled"
      exit 0
    fi
  fi

  # Install dependencies
  log_step "Installing system dependencies..."
  case "$DISTRO" in
    ubuntu)  install_deps_ubuntu ;;
    debian)  install_deps_debian ;;
    fedora)  install_deps_fedora ;;
    rocky|almalinux) install_deps_rocky ;;
    arch)    install_deps_arch ;;
    *)       log_error "Unsupported distribution: $DISTRO"; exit 1 ;;
  esac

  # Setup
  setup_user
  setup_directories
  install_nats
  start_infra
  install_binaries
  generate_config
  run_migrations
  create_services

  # Reverse proxy
  if [ -t 0 ] || [ -n "${REVERSE_PROXY:-}" ]; then
    case "${REVERSE_PROXY:-nginx}" in
      nginx)  setup_nginx ;;
      caddy)  setup_caddy ;;
      none)   log_info "Skipping reverse proxy setup" ;;
    esac
  fi

  setup_firewall

  # Start NATS
  log_step "Starting infrastructure..."
  systemctl start nats 2>/dev/null || true

  # Kill any stale processes from previous installs
  log_step "Cleaning up stale processes..."
  pkill -f "rymevisor-" 2>/dev/null || true
  sleep 1

  # Start RymeVisor services
  log_step "Starting RymeVisor services..."
  local SVCS=(
    "rymevisor-api-gateway"
    "rymevisor-control-plane"
    "rymevisor-scheduler"
    "rymevisor-networking-engine"
    "rymevisor-storage-manager"
    "rymevisor-node-agent"
  )

  for svc in "${SVCS[@]}"; do
    systemctl start "$svc" 2>/dev/null && log_info "Started: $svc" || log_warn "Failed to start: $svc"
  done

  # Wait for services to be ready
  log_step "Waiting for services to start..."
  sleep 3

  echo ""
  log_info "Installation Complete!"
  echo ""
  log_info "Dashboard: http://${DOMAIN:-localhost}"
  log_info "API:       http://${DOMAIN:-localhost}/api/v1"
  log_info "Health:    http://localhost:${PORT_API_GATEWAY}/health"
  echo ""
  local box_width=56
  local api_key_len=${#API_KEY}
  local key_line_pad=$(( box_width - 12 - api_key_len ))
  local example_prefix="  Example: curl -H 'X-API-Key: ${API_KEY:0:8}...' http://..."
  local example_len=${#example_prefix}
  local example_pad=$(( box_width - example_len ))
  echo -e "${GREEN}╔══════════════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║                  SAVE YOUR API KEY                      ║${NC}"
  echo -e "${GREEN}╠══════════════════════════════════════════════════════════╣${NC}"
  echo -e "${GREEN}║  API Key: ${API_KEY}${GREEN}$(printf '%*s' $key_line_pad '')║${NC}"
  echo -e "${GREEN}║                                                          ║${NC}"
  echo -e "${GREEN}║  Header:  X-API-Key: <your-key>                          ║${NC}"
  echo -e "${GREEN}║${GREEN}${example_prefix}$(printf '%*s' $example_pad '')║${NC}"
  echo -e "${GREEN}╚══════════════════════════════════════════════════════════╝${NC}"
  echo ""
  log_info "Config: ${RYMEVISOR_CONFIG}/config.env"
  log_info "Logs:   journalctl -u rymevalor-* -f"
  echo ""
}

# ============================================================
# Update Flow
# ============================================================

do_update() {
  echo ""
  log_step "Updating RymeVisor..."
  check_root

  local OLD_VERSION
  OLD_VERSION=$(cat "${RYMEVISOR_CONFIG}/VERSION" 2>/dev/null || echo "unknown")

  # Stop services
  log_step "Stopping services..."
  local SVCS=(
    "rymevisor-api-gateway"
    "rymevisor-control-plane"
    "rymevisor-scheduler"
    "rymevisor-networking-engine"
    "rymevisor-storage-manager"
    "rymevisor-node-agent"
  )

  for svc in "${SVCS[@]}"; do
    systemctl stop "$svc" 2>/dev/null || true
  done

  # Backup
  log_step "Creating backup..."
  local BACKUP_DIR="${RYMEVISOR_HOME}/backups/update-$(date +%Y%m%d-%H%M%S)"
  mkdir -p "$BACKUP_DIR"

  docker exec rymevalor-postgres pg_dump -U "$DB_USER" "$DB_NAME" > "${BACKUP_DIR}/database.sql" 2>/dev/null || true
  cp -r "${RYMEVISOR_CONFIG}" "${BACKUP_DIR}/config" 2>/dev/null || true
  log_info "Backup created: $BACKUP_DIR"

  # Install new version
  install_binaries
  run_migrations

  # Restart
  log_step "Starting services..."
  systemctl start nats 2>/dev/null || true

  for svc in "${SVCS[@]}"; do
    systemctl start "$svc" 2>/dev/null || true
  done

  echo ""
  log_info "Update complete: $OLD_VERSION -> $RYMEVISOR_VERSION"
  log_info "Backup saved at: $BACKUP_DIR"
}

# ============================================================
# Uninstall Flow
# ============================================================

do_uninstall() {
  echo ""
  log_step "Uninstalling RymeVisor..."
  check_root

  if [ -t 0 ]; then
    if ! confirm "This will remove RymeVisor and all data. Continue?"; then
      log_info "Uninstall cancelled"
      exit 0
    fi
    if ! confirm "Also remove database and all VMs?"; then
      REMOVE_DATA=false
    else
      REMOVE_DATA=true
    fi
  fi

  # Stop services
  local SERVICES=(
    "api-gateway" "control-plane"
    "scheduler" "networking-engine" "storage-manager" "node-agent"
  )
  for svc in "${SERVICES[@]}"; do
    systemctl stop "rymevisor-${svc}" 2>/dev/null || true
    systemctl disable "rymevisor-${svc}" 2>/dev/null || true
    rm -f "/etc/systemd/system/rymevisor-${svc}.service"
  done

  # Stop NATS if we installed it
  systemctl stop nats 2>/dev/null || true
  systemctl disable nats 2>/dev/null || true
  rm -f /etc/systemd/system/nats.service

  systemctl daemon-reload

  # Remove binaries
  rm -f "${RYMEVISOR_BIN}"/rymevisor-*
  rm -f "${RYMEVISOR_BIN}/rymevisor"

  # Remove config
  rm -rf "${RYMEVISOR_CONFIG}"

  # Stop and remove Docker containers
  docker stop rymevalor-postgres rymevalor-redis 2>/dev/null || true
  docker rm rymevalor-postgres rymevalor-redis 2>/dev/null || true

  # Remove data if requested
  if [ "${REMOVE_DATA:-false}" = true ]; then
    docker volume rm rymevalor-pgdata rymevalor-redisdata 2>/dev/null || true
    rm -rf "${RYMEVISOR_HOME}"
    rm -rf /var/lib/nats
  fi

  # Remove reverse proxy config
  rm -f /etc/nginx/sites-available/rymevisor
  rm -f /etc/nginx/sites-enabled/rymevisor
  rm -f /etc/nftables.d/rymevisor.nft

  # Remove caddy config
  rm -f /etc/caddy/Caddyfile
  systemctl stop caddy 2>/dev/null || true
  systemctl disable caddy 2>/dev/null || true
  rm -f /etc/systemd/system/caddy.service

  # Remove user
  userdel "$RYMEVISOR_USER" 2>/dev/null || true

  echo ""
  log_info "RymeVisor uninstalled"
  if [ "${REMOVE_DATA:-false}" != true ]; then
    log_warn "Docker volumes preserved. Remove with: docker volume rm rymevalor-pgdata rymevalor-redisdata"
  fi
}

# ============================================================
# Status
# ============================================================

do_status() {
  echo ""
  echo -e "${CYAN}RymeVisor Status${NC}"
  echo ""

  # Infrastructure
  echo "Infrastructure:"

  # Docker containers
  for container in rymevalor-postgres rymevalor-redis; do
    local state
    state=$(docker inspect -f '{{.State.Status}}' "$container" 2>/dev/null || echo "not found")
    if [ "$state" = "running" ]; then
      echo -e "  ${GREEN}●${NC} ${container}: ${GREEN}running${NC}"
    elif [ "$state" = "exited" ]; then
      echo -e "  ${YELLOW}●${NC} ${container}: ${YELLOW}stopped${NC}"
    else
      echo -e "  ${RED}●${NC} ${container}: ${RED}${state}${NC}"
    fi
  done

  # NATS
  local nats_status
  nats_status=$(systemctl is-active nats 2>/dev/null || echo "not found")
  if [ "$nats_status" = "active" ]; then
    echo -e "  ${GREEN}●${NC} nats: ${GREEN}running${NC}"
  elif [ "$nats_status" = "inactive" ]; then
    echo -e "  ${YELLOW}●${NC} nats: ${YELLOW}stopped${NC}"
  else
    echo -e "  ${RED}●${NC} nats: ${RED}${nats_status}${NC}"
  fi

  echo ""
  echo "RymeVisor Services:"
  local SERVICES=(
    "api-gateway" "control-plane"
    "scheduler" "networking-engine" "storage-manager" "node-agent"
  )

  for svc in "${SERVICES[@]}"; do
    local status
    status=$(systemctl is-active "rymevisor-${svc}" 2>/dev/null || echo "not installed")
    if [ "$status" = "active" ]; then
      echo -e "  ${GREEN}●${NC} rymevalor-${svc}: ${GREEN}running${NC}"
    elif [ "$status" = "inactive" ]; then
      echo -e "  ${YELLOW}●${NC} rymevalor-${svc}: ${YELLOW}stopped${NC}"
    else
      echo -e "  ${RED}●${NC} rymevalor-${svc}: ${RED}${status}${NC}"
    fi
  done

  echo ""
}

# ============================================================
# Version Check
# ============================================================

is_installed() {
  [ -f "${RYMEVISOR_CONFIG}/config.env" ] && [ -f "${RYMEVISOR_BIN}/rymevisor-api-gateway" ]
}

get_installed_version() {
  cat "${RYMEVISOR_CONFIG}/VERSION" 2>/dev/null || echo ""
}

get_latest_version() {
  local latest
  latest=$(curl -fsSL "https://api.github.com/repos/Ryme-Labs/rymevisor/releases/latest" 2>/dev/null | jq -r '.tag_name' 2>/dev/null)
  if [ -z "$latest" ] || [ "$latest" = "null" ]; then
    latest=$(curl -fsSL "https://api.github.com/repos/Ryme-Labs/rymevisor/tags" 2>/dev/null | jq -r '.[0].name' 2>/dev/null)
  fi
  echo "$latest"
}

version_gt() {
  [ "$1" != "$2" ] && [ "$(printf '%s\n' "$1" "$2" | sort -V | head -1)" = "$2" ]
}

# ============================================================
# Entry Point
# ============================================================

usage() {
  echo "Usage: $0 [command]"
  echo ""
  echo "Commands:"
  echo "  install    Install RymeVisor (default)"
  echo "  update     Update to latest version"
  echo  "  uninstall  Remove RymeVisor"
  echo "  status     Show service status"
  echo "  help       Show this help"
  echo ""
}

interactive_menu() {
  echo ""
  echo -e "${CYAN}╔══════════════════════════════════════════╗${NC}"
  echo -e "${CYAN}║         RymeVisor Installer              ║${NC}"
  echo -e "${CYAN}╚══════════════════════════════════════════╝${NC}"
  echo ""

  if is_installed; then
    local INSTALLED_VERSION
    INSTALLED_VERSION=$(get_installed_version)
    echo -e "  ${GREEN}● RymeVisor is installed${NC} (version: ${INSTALLED_VERSION:-unknown})"
    echo ""

    log_step "Checking for updates..."
    local LATEST_VERSION
    LATEST_VERSION=$(get_latest_version)

    if [ -n "$LATEST_VERSION" ] && [ "$LATEST_VERSION" != "null" ]; then
      if [ "$INSTALLED_VERSION" = "$LATEST_VERSION" ]; then
        echo -e "  ${GREEN}✓ You are on the latest version ($LATEST_VERSION)${NC}"
      elif version_gt "$LATEST_VERSION" "$INSTALLED_VERSION"; then
        echo -e "  ${YELLOW}↑ New version available: $LATEST_VERSION (current: $INSTALLED_VERSION})${NC}"
      else
        echo -e "  ${GREEN}✓ You are ahead of latest release ($LATEST_VERSION)${NC}"
      fi
    else
      echo -e "  ${YELLOW}⚠ Could not check for updates${NC}"
    fi

    echo ""
    echo "  What would you like to do?"
    echo ""
    echo "    1) Update to latest version"
    echo "    2) Show status"
    echo "    3) Uninstall"
    echo "    4) Exit"
    echo ""

    local choice
    read -rp "  Select [1-4]: " choice

    case "$choice" in
      1) do_update ;;
      2) do_status; interactive_menu ;;
      3) do_uninstall ;;
      4|*) log_info "Goodbye!"; exit 0 ;;
    esac
  else
    echo -e "  ${RED}● RymeVisor is not installed${NC}"
    echo ""

    log_step "Checking latest release..."
    local LATEST_VERSION
    LATEST_VERSION=$(get_latest_version)
    if [ -n "$LATEST_VERSION" ] && [ "$LATEST_VERSION" != "null" ]; then
      echo -e "  Latest version: ${GREEN}${LATEST_VERSION}${NC}"
    fi

    echo ""
    echo "  What would you like to do?"
    echo ""
    echo "    1) Install RymeVisor"
    echo "    2) Show help"
    echo "    3) Exit"
    echo ""

    local choice
    read -rp "  Select [1-3]: " choice

    case "$choice" in
      1) do_install ;;
      2) usage; interactive_menu ;;
      3|*) log_info "Goodbye!"; exit 0 ;;
    esac
  fi
}

case "${1:-}" in
  install)   do_install ;;
  update)    do_update ;;
  uninstall) do_uninstall ;;
  status)    do_status ;;
  help|-h|--help) usage ;;
  "")
    check_root
    if [ -t 0 ]; then
      interactive_menu
    else
      # Piped execution (curl | bash) - auto-install if not installed
      if is_installed; then
        log_info "RymeVisor is already installed (version: $(get_installed_version))"
        log_info "Run with a command: install, update, uninstall, status"
        log_info "Example: curl -fsSL ... | sudo bash -s -- update"
        exit 0
      else
        log_info "Installing RymeVisor..."
        do_install
      fi
    fi
    ;;
  *)
    log_error "Unknown command: $1"
    usage
    exit 1
    ;;
esac
