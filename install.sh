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
PORT_AUTH_SERVICE=8082
PORT_SCHEDULER=8083
PORT_NETWORKING=8084
PORT_STORAGE=8085

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
REVERSE_PROXY=""  # nginx or caddy

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
    postgresql postgresql-contrib \
    redis-server \
    gnupg2 lsb-release \
    software-properties-common \
    apt-transport-https ca-certificates \
    > /dev/null 2>&1
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
    postgresql postgresql-contrib \
    redis-server \
    gnupg2 lsb-release \
    > /dev/null 2>&1
}

install_deps_fedora() {
  dnf install -y -q \
    curl wget git unzip jq \
    qemu-kvm qemu-img \
    libvirt libvirt-client \
    bridge-utils \
    genisoimage \
    nftables \
    postgresql-server postgresql \
    redis \
    > /dev/null 2>&1
}

install_deps_rocky() {
  dnf install -y -q \
    curl wget git unzip jq \
    qemu-kvm qemu-img \
    libvirt libvirt-client \
    bridge-utils \
    genisoimage \
    nftables \
    postgresql-server postgresql \
    redis \
    > /dev/null 2>&1
}

install_deps_arch() {
  pacman -Sy --noconfirm --needed \
    curl wget git unzip jq \
    qemu-full \
    libvirt \
    bridge-utils \
    cdrtools \
    nftables \
    postgresql \
    redis \
    > /dev/null 2>&1
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
    useradd --system --shell /bin/false --home-dir "$RYMEVISOR_HOME" "$RYMEVISOR_USER"
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
# Database Setup
# ============================================================

setup_database() {
  log_step "Setting up PostgreSQL..."

  local PG_SERVICE
  if systemctl is-active --quiet postgresql 2>/dev/null; then
    PG_SERVICE="postgresql"
  elif systemctl is-active --quiet postgresql16 2>/dev/null; then
    PG_SERVICE="postgresql16"
  elif systemctl is-active --quiet postgresql15 2>/dev/null; then
    PG_SERVICE="postgresql15"
  else
    systemctl enable --now postgresql 2>/dev/null || true
    PG_SERVICE="postgresql"
  fi

  # Generate password if not set
  if [ -z "$DB_PASS" ]; then
    DB_PASS=$(generate_password)
  fi

  sudo -u postgres psql -c "CREATE USER $DB_USER WITH PASSWORD '$DB_PASS';" 2>/dev/null || true
  sudo -u postgres psql -c "CREATE DATABASE $DB_NAME OWNER $DB_USER;" 2>/dev/null || true
  sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE $DB_NAME TO $DB_USER;" 2>/dev/null || true

  log_info "Database configured: $DB_NAME @ $DB_HOST:$DB_PORT"
}

run_migrations() {
  log_step "Running database migrations..."
  local DB_URL="postgres://$DB_USER:$DB_PASS@$DB_HOST:$DB_PORT/$DB_NAME?sslmode=disable"

  for migration_dir in migrations/*/; do
    local service_name
    service_name=$(basename "$migration_dir")
    for migration_file in "$migration_dir"*.up.sql; do
      if [ -f "$migration_file" ]; then
        sudo -u postgres psql -d "$DB_NAME" -f "$migration_file" 2>/dev/null || true
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

  local VERSION="$RYMEVISOR_VERSION"
  local ARCH
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
  esac

  if [ "$VERSION" = "latest" ]; then
    VERSION=$(curl -fsSL https://api.github.com/repos/Ryme-Labs/rymevisor/releases/latest | jq -r '.tag_name' 2>/dev/null || echo "v0.1.0")
  fi

  local BASE_URL="https://github.com/Ryme-Labs/rymevisor/releases/download/${VERSION}"

  local SERVICES=("control-plane" "api-gateway" "auth-service" "scheduler" "networking-engine" "storage-manager" "node-agent")

  for service in "${SERVICES[@]}"; do
    local binary_name="${service}"
    local url="${BASE_URL}/${binary_name}-linux-${ARCH}"
    local dest="${RYMEVISOR_BIN}/rymevisor-${binary_name}"

    if curl -fsSL "$url" -o "$dest" 2>/dev/null; then
      chmod +x "$dest"
      log_info "Installed: rymevisor-${binary_name}"
    else
      log_warn "Could not download ${binary_name} (may not be released yet)"
    fi
  done

  # Create symlinks for convenience
  ln -sf "${RYMEVISOR_BIN}/rymevisor-api-gateway" "${RYMEVISOR_BIN}/rymevisor" 2>/dev/null || true
}

# ============================================================
# Configuration Generation
# ============================================================

generate_config() {
  log_step "Generating configuration..."

  local JWT_SECRET
  JWT_SECRET=$(generate_password)

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

# Auth
RYMEVISOR_JWT_SECRET=${JWT_SECRET}
RYMEVISOR_JWT_EXPIRY=1h
RYMEVISOR_REFRESH_TOKEN_EXPIRY=168h

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
EOF

  chmod 600 "${RYMEVISOR_CONFIG}/config.env"
  log_info "Configuration written to ${RYMEVISOR_CONFIG}/config.env"
}

# ============================================================
# Systemd Services
# ============================================================

create_services() {
  log_step "Creating systemd services..."

  local SERVICES=(
    "control-plane:${PORT_CONTROL_PLANE}"
    "auth-service:${PORT_AUTH_SERVICE}"
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

    cat > "/etc/systemd/system/rymevisor-${name}.service" << EOF
[Unit]
Description=RymeVisor ${name}
After=network.target postgresql.service nats.service redis.service
Wants=postgresql.service nats.service redis.service

[Service]
Type=simple
User=${RYMEVISOR_USER}
Group=${RYMEVISOR_GROUP}
EnvironmentFile=${RYMEVISOR_CONFIG}/config.env
ExecStart=${binary}
Restart=always
RestartSec=5
LimitNOFILE=65535
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=${RYMEVISOR_HOME} /var/log/rymevisor /var/lib/nats
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
EOF
  done

  systemctl daemon-reload

  for entry in "${SERVICES[@]}"; do
    local name="${entry%%:*}"
    systemctl enable "rymevisor-${name}"
  done

  log_info "Systemd services created and enabled"
}

# ============================================================
# Reverse Proxy Setup
# ============================================================

setup_nginx() {
  log_step "Setting up Nginx reverse proxy..."

  if ! command -v nginx &>/dev/null; then
    apt-get install -y -qq nginx > /dev/null 2>&1 || dnf install -y -q nginx > /dev/null 2>&1
  fi

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

    ln -sf /etc/nginx/sites-available/rymevisor /etc/nginx/sites-enabled/
    rm -f /etc/nginx/sites-enabled/default

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

    ln -sf /etc/nginx/sites-available/rymevisor /etc/nginx/sites-enabled/
    rm -f /etc/nginx/sites-enabled/default
  fi

  nginx -t && systemctl reload nginx
  log_info "Nginx configured"
}

setup_caddy() {
  log_step "Setting up Caddy reverse proxy..."

  if ! command -v caddy &>/dev/null; then
    curl -fsSL https://caddyserver.com/api/download?os=linux&arch=amd64 -o /usr/local/bin/caddy
    chmod +x /usr/local/bin/caddy
  fi

  if [ -n "$DOMAIN" ]; then
    local CADDY_CONFIG="${DOMAIN} {
    reverse_proxy localhost:${PORT_API_GATEWAY}
}"
    if [ "$ENABLE_TLS" = true ]; then
      CADDY_CONFIG="${DOMAIN} {
    reverse_proxy localhost:${PORT_API_GATEWAY}
}"
    else
      CADDY_CONFIG="${DOMAIN} {
    tls internal
    reverse_proxy localhost:${PORT_API_GATEWAY}
}"
    fi
  else
    local CADDY_CONFIG=":80 {
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
  echo -e "${CYAN}╔══════════════════════════════════════╗${NC}"
  echo -e "${CYAN}║     RymeVisor Installer v${RYMEVISOR_VERSION}        ║${NC}"
  echo -e "${CYAN}║  Originally built for Ryme Labs      ║${NC}"
  echo -e "${CYAN}╚══════════════════════════════════════╝${NC}"
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
    echo "  Database: PostgreSQL @ $DB_HOST:$DB_PORT"
    echo "  Cache: Redis @ $REDIS_HOST:$REDIS_PORT"
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
  setup_database
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

  # Start services
  log_step "Starting services..."
  systemctl start rymevalor-auth-service
  systemctl start rymevalor-control-plane
  systemctl start rymevalor-scheduler
  systemctl start rymevalor-networking-engine
  systemctl start rymevalor-storage-manager
  systemctl start rymevalor-api-gateway
  systemctl start rymevalor-node-agent

  echo ""
  echo -e "${GREEN}╔══════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║      Installation Complete!          ║${NC}"
  echo -e "${GREEN}╚══════════════════════════════════════╝${NC}"
  echo ""
  log_info "Dashboard: http://${DOMAIN:-localhost}"
  log_info "API:       http://${DOMAIN:-localhost}/api/v1"
  log_info "Health:    http://localhost:${PORT_API_GATEWAY}/health"
  echo ""
  log_info "Services managed by systemd:"
  systemctl list-units --type=service --state=running | grep rymevalor || true
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
  systemctl stop rymevalor-api-gateway 2>/dev/null || true
  systemctl stop rymevalor-control-plane 2>/dev/null || true
  systemctl stop rymevalor-auth-service 2>/dev/null || true
  systemctl stop rymevalor-scheduler 2>/dev/null || true
  systemctl stop rymevalor-networking-engine 2>/dev/null || true
  systemctl stop rymevalor-storage-manager 2>/dev/null || true
  systemctl stop rymevalor-node-agent 2>/dev/null || true

  # Backup
  log_step "Creating backup..."
  local BACKUP_DIR="${RYMEVISOR_HOME}/backups/update-$(date +%Y%m%d-%H%M%S)"
  mkdir -p "$BACKUP_DIR"
  pg_dump -U "$DB_USER" "$DB_NAME" > "${BACKUP_DIR}/database.sql" 2>/dev/null || true
  cp -r "${RYMEVISOR_CONFIG}" "${BACKUP_DIR}/config" 2>/dev/null || true
  log_info "Backup created: $BACKUP_DIR"

  # Install new version
  install_binaries
  run_migrations

  # Restart
  log_step "Starting services..."
  systemctl start rymevalor-auth-service
  systemctl start rymevalor-control-plane
  systemctl start rymevalor-scheduler
  systemctl start rymevalor-networking-engine
  systemctl start rymevalor-storage-manager
  systemctl start rymevalor-api-gateway
  systemctl start rymevalor-node-agent

  echo ""
  log_info "Update complete: $OLD_VERSION -> $RYMEVISOR_VERSION"
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
    "api-gateway" "control-plane" "auth-service"
    "scheduler" "networking-engine" "storage-manager" "node-agent"
  )
  for svc in "${SERVICES[@]}"; do
    systemctl stop "rymevisor-${svc}" 2>/dev/null || true
    systemctl disable "rymevisor-${svc}" 2>/dev/null || true
    rm -f "/etc/systemd/system/rymevisor-${svc}.service"
  done

  systemctl daemon-reload

  # Remove binaries
  rm -f "${RYMEVISOR_BIN}"/rymevisor-*
  rm -f "${RYMEVISOR_BIN}/rymevisor"

  # Remove config
  rm -rf "${RYMEVISOR_CONFIG}"

  # Remove data if requested
  if [ "${REMOVE_DATA:-false}" = true ]; then
    sudo -u postgres dropdb "$DB_NAME" 2>/dev/null || true
    sudo -u postgres dropuser "$DB_USER" 2>/dev/null || true
    rm -rf "${RYMEVISOR_HOME}"
  fi

  # Remove reverse proxy config
  rm -f /etc/nginx/sites-available/rymevisor
  rm -f /etc/nginx/sites-enabled/rymevisor
  rm -f /etc/nftables.d/rymevisor.nft

  # Remove user
  userdel "$RYMEVISOR_USER" 2>/dev/null || true

  echo ""
  log_info "RymeVisor uninstalled"
  if [ "${REMOVE_DATA:-false}" != true ]; then
    log_warn "Data preserved at ${RYMEVISOR_HOME}"
  fi
}

# ============================================================
# Status
# ============================================================

do_status() {
  echo ""
  echo -e "${CYAN}RymeVisor Status${NC}"
  echo ""

  local SERVICES=(
    "api-gateway" "control-plane" "auth-service"
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
# Entry Point
# ============================================================

usage() {
  echo "Usage: $0 [command]"
  echo ""
  echo "Commands:"
  echo "  install    Install RymeVisor (default)"
  echo "  update     Update to latest version"
  echo "  uninstall  Remove RymeVisor"
  echo "  status     Show service status"
  echo "  help       Show this help"
  echo ""
}

case "${1:-install}" in
  install)   do_install ;;
  update)    do_update ;;
  uninstall) do_uninstall ;;
  status)    do_status ;;
  help|-h|--help) usage ;;
  *)
    log_error "Unknown command: $1"
    usage
    exit 1
    ;;
esac
