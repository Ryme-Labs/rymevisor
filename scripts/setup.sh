#!/bin/bash
set -e

echo "Setting up RymeVisor development environment..."

# Check prerequisites
echo "Checking prerequisites..."

if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed"
    exit 1
fi

if ! command -v docker &> /dev/null; then
    echo "Error: Docker is not installed"
    exit 1
fi

if ! command -v docker-compose &> /dev/null && ! command -v docker &> /dev/null; then
    echo "Error: Docker Compose is not installed"
    exit 1
fi

echo "All prerequisites found!"
echo ""

# Start infrastructure
echo "Starting infrastructure services..."
docker compose -f deployments/docker/docker-compose.yml up -d postgres redis nats minio

echo ""
echo "Waiting for services to be ready..."
sleep 5

# Verify services
echo "Verifying services..."
docker compose -f deployments/docker/docker-compose.yml ps

echo ""
echo "Development environment is ready!"
echo ""
echo "Services:"
echo "  PostgreSQL:  localhost:5432"
echo "  Redis:       localhost:6379"
echo "  NATS:        localhost:4222"
echo "  MinIO:       localhost:9000 (console: localhost:9001)"
echo ""
echo "Next steps:"
echo "  1. Run 'make generate' to generate protobuf code"
echo "  2. Run 'make build' to build all services"
echo "  3. Run 'make up' to start all services"
