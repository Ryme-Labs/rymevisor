# RymeVisor

> **UNDER ACTIVE DEVELOPMENT — NOT READY FOR PRODUCTION USE**

An open-source server management and virtualization platform. Originally built as an internal tool for Ryme Labs to manage our own infrastructure, now open-sourced for the community.

## What is RymeVisor?

RymeVisor is a self-hosted platform that turns any Linux server into a manageable virtualization host. Think of it as a lightweight alternative to Proxmox or oVirt, but designed for modern workflows with a clean API-first architecture.

It was originally developed internally at Ryme Labs to manage our fleet of servers, and we decided to open-source it.

**Not an IaaS platform.** RymeVisor is a server management tool, not a cloud provider.

## Features

- **Virtual Machine Management** — Create, start, stop, snapshot, clone VMs via API or UI
- **Multi-Node** — Manage multiple compute nodes from a single control plane
- **Networking** — Private networks, firewalls, floating IPs
- **Storage** — Volume management with QCOW2, snapshots, clones
- **Scheduling** — Intelligent workload placement across nodes
- **Auth** — JWT, API keys, RBAC, multi-organization support
- **API-First** — Every feature available via REST API
- **Node Agent** — QEMU/KVM management with cloud-init support

## Quick Start

```bash
# Install on Ubuntu/Debian
curl -fsSL https://raw.githubusercontent.com/Ryme-Labs/rymevisor/main/install.sh | sudo bash

# Or clone and install
git clone https://github.com/Ryme-Labs/rymevisor.git
cd rymevalor
sudo bash install.sh
```

## Install Options

```bash
# Interactive install (asks for config)
sudo bash install.sh

# Install with defaults
sudo bash install.sh install

# Update to latest version
sudo bash install.sh update

# Check service status
sudo bash install.sh status

# Uninstall
sudo bash install.sh uninstall
```

## Architecture

```
API Gateway → Auth Service → Control Plane → Scheduler → Node Agents → QEMU/KVM
                            ↕ NATS JetStream ↕
                         PostgreSQL + Redis
```

- **API Gateway** — HTTP entry point, rate limiting, reverse proxy
- **Auth Service** — Users, sessions, JWT, API keys, RBAC
- **Control Plane** — VM, node, image, backup management
- **Scheduler** — Workload placement with scoring algorithm
- **Networking Engine** — Private networks, firewalls, floating IPs
- **Storage Manager** — Pools, volumes, snapshots
- **Node Agent** — QEMU/KVM process management on each compute node

## Tech Stack

- Go 1.23
- PostgreSQL 16
- Redis 7
- NATS JetStream
- QEMU/KVM
- Chi router

## Development

```bash
# Start local dev environment
docker compose -f deployments/docker/docker-compose.yml up -d

# Build all services
make build

# Run tests
make test
```

## License

MIT — Originally built by [Ryme Labs](https://github.com/Ryme-Labs) as an internal tool, now open-sourced.
