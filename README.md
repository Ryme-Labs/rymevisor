# RymeVisor

> **UNDER ACTIVE DEVELOPMENT — NOT READY FOR PRODUCTION USE**

An open-source server management and virtualization platform. Originally built as an internal tool for Ryme Labs to manage our own infrastructure, now open-sourced for the community.

## What is RymeVisor?

RymeVisor is a self-hosted platform that turns any Linux server into a manageable virtualization host. Think of it as a lightweight alternative to Proxmox or oVirt, but designed for modern workflows with a clean API-first architecture.

It was originally developed internally at Ryme Labs to manage our fleet of servers, and we decided to open-source it.

**Not an IaaS platform.** RymeVisor is a server management tool, not a cloud provider.

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
make dev

# Build all services
make build

# Run tests
make test
```

## License

MIT — Originally built by [Ryme Labs](https://github.com/Ryme-Labs) as an internal tool, now open-sourced.
