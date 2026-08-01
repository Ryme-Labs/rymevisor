.PHONY: all build generate migrate up down test lint clean

# Default target
all: generate build

# Generate protobuf code
generate:
	buf generate

# Build all services
build:
	go build -o bin/control-plane ./cmd/control-plane
	go build -o bin/node-agent ./cmd/node-agent
	go build -o bin/api-gateway ./cmd/api-gateway
	go build -o bin/auth-service ./cmd/auth-service
	go build -o bin/scheduler ./cmd/scheduler
	go build -o bin/networking-engine ./cmd/networking-engine
	go build -o bin/storage-manager ./cmd/storage-manager

# Build a specific service
build-%:
	go build -o bin/$* ./cmd/$*

# Start local dev environment
up:
	docker compose -f deployments/docker/docker-compose.yml up -d

# Stop local dev environment
down:
	docker compose -f deployments/docker/docker-compose.yml down

# Run database migrations
migrate:
	@echo "Running migrations..."

# Run all tests
test:
	go test ./...

# Run tests with coverage
test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

# Lint
lint:
	golangci-lint run ./...

# Format code
fmt:
	gofmt -s -w .
	goimports -w .

# Tidy modules
tidy:
	go work sync
	@for dir in $$(find . -name "go.mod" -exec dirname {} \;); do \
		echo "Tidying $$dir"; \
		cd $$dir && go mod tidy && cd -; \
	done

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out

# Docker build
docker-build:
	docker compose -f deployments/docker/docker-compose.yml build

# Show help
help:
	@echo "Available targets:"
	@echo "  all          - Generate code and build all services"
	@echo "  generate     - Generate protobuf code"
	@echo "  build        - Build all services"
	@echo "  build-<svc>  - Build a specific service"
	@echo "  up           - Start local dev environment"
	@echo "  down         - Stop local dev environment"
	@echo "  test         - Run all tests"
	@echo "  test-cover   - Run tests with coverage"
	@echo "  lint         - Run linter"
	@echo "  fmt          - Format code"
	@echo "  tidy         - Tidy all modules"
	@echo "  clean        - Clean build artifacts"
	@echo "  docker-build - Build Docker images"
	@echo "  help         - Show this help"
