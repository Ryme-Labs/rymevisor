# RymeVisor

> **⚠️ UNDER ACTIVE DEVELOPMENT — NOT READY FOR PRODUCTION USE**

Open-source Infrastructure-as-a-Service platform that transforms any Linux server into a private cloud.

## Status

This project is in early development. APIs, data models, and configuration are subject to breaking changes without notice.

## Architecture

- **Control Plane** — VM, node, image, backup management
- **API Gateway** — HTTP entry point with rate limiting and auth proxy
- **Auth Service** — Users, sessions, RBAC, API keys
- **Scheduler** — Intelligent workload placement
- **Networking Engine** — Private networks, firewalls, floating IPs
- **Storage Manager** — Pools, volumes, snapshots
- **Node Agent** — QEMU/KVM process management on compute nodes

## Tech Stack

- Go 1.23
- PostgreSQL 16
- Redis 7
- NATS JetStream
- QEMU/KVM
- Chi router
- ConnectRPC

## Quick Start

```bash
# Start local dev environment
make up

# Build all services
make build

# Run tests
make test
```

## License

MIT
