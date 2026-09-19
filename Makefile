.PHONY: all build generate migrate up down dev stop test lint clean

all: generate build

generate:
	buf generate

build:
	go build -o bin/control-plane ./cmd/control-plane
	go build -o bin/node-agent ./cmd/node-agent
	go build -o bin/api-gateway ./cmd/api-gateway
	go build -o bin/scheduler ./cmd/scheduler
	go build -o bin/networking-engine ./cmd/networking-engine
	go build -o bin/storage-manager ./cmd/storage-manager
	@if [ -d ./cmd/auth-service ]; then go build -o bin/auth-service ./cmd/auth-service; fi

build-%:
	go build -o bin/$* ./cmd/$*

dev:
	./start.sh

stop:
	@if [ -f .dev-pids ]; then \
		while read -r pid; do kill "$$pid" 2>/dev/null || true; done < .dev-pids; \
		rm -f .dev-pids; \
	fi
	@docker compose -f deployments/docker/docker-compose.yml down 2>/dev/null || true
	@echo "Stopped."

up:
	docker compose -f deployments/docker/docker-compose.yml up -d postgres nats minio

down:
	docker compose -f deployments/docker/docker-compose.yml down

migrate:
	@echo "Running migrations..."

test:
	go test ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
	goimports -w .

tidy:
	go work sync
	@for dir in $$(find . -name "go.mod" -exec dirname {} \;); do \
		echo "Tidying $$dir"; \
		cd $$dir && go mod tidy && cd -; \
	done

clean:
	rm -rf bin/
	rm -f coverage.out

docker-build:
	docker compose -f deployments/docker/docker-compose.yml build

help:
	@echo "Available targets:"
	@echo "  dev          - Start everything locally (infra + all services)"
	@echo "  stop         - Stop all services and infra"
	@echo "  all          - Generate code and build all services"
	@echo "  generate     - Generate protobuf code"
	@echo "  build        - Build all services"
	@echo "  build-<svc>  - Build a specific service"
	@echo "  up           - Start infra containers only"
	@echo "  down         - Stop infra containers"
	@echo "  test         - Run all tests"
	@echo "  test-cover   - Run tests with coverage"
	@echo "  lint         - Run linter"
	@echo "  fmt          - Format code"
	@echo "  tidy         - Tidy all modules"
	@echo "  clean        - Clean build artifacts"
	@echo "  docker-build - Build Docker images"
	@echo "  help         - Show this help"
