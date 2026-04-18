.PHONY: all test lint fmt vet clean coverage help license tidy fix doc check e2e e2e-compile e2e-up e2e-down


# Variables
GO := go
GOFLAGS := -v
MODULE := $(shell $(GO) list -m)
PKGS := $(shell $(GO) list ./... | grep -v /examples/ | grep -v /tests/)
GOLANGCI_LINT := golangci-lint
DOCKER_COMPOSE := docker compose
E2E_OPENAI_BASE_URL ?= http://localhost:11434/v1
E2E_OPENAI_API_KEY ?= ollama
E2E_OPENAI_MODEL ?= minimax-m2.7:cloud
E2E_ANTHROPIC_BASE_URL ?= http://localhost:11434
E2E_ANTHROPIC_AUTH_TOKEN ?= ollama
E2E_ANTHROPIC_MODEL ?= minimax-m2.7:cloud
E2E_EMBEDDING_MODEL ?= nomic-embed-text

# Default target
all: test lint

## help: Display this help message
help:
	@echo "Available targets:"
	@echo "  make test        - Run all tests"
	@echo "  make lint        - Run golangci-lint"
	@echo "  make fmt         - Format all Go files"
	@echo "  make fix         - Run go fix on all packages"
	@echo "  make vet         - Run go vet"
	@echo "  make tidy        - Tidy and verify go modules"
	@echo "  make coverage    - Generate test coverage report"
	@echo "  make e2e-compile - Compile the e2e test suite"
	@echo "  make e2e-up      - Start Docker services needed by e2e tests"
	@echo "  make e2e-down    - Stop Docker services used by e2e tests"
	@echo "  make e2e         - Run the e2e test suite"
	@echo "  make clean       - Clean build artifacts and cache"
	@echo "  make license     - Add license headers to all Go files"
	@echo "  make help        - Display this help message"

## test: Run all tests (excluding examples)
test:
	@echo "Running tests..."
	$(GO) test $(GOFLAGS) -race -timeout 5m $(PKGS)

## coverage: Generate test coverage report
coverage:
	@echo "Generating coverage report..."
	$(GO) test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic $(PKGS)
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

## e2e-compile: Compile the e2e suite without running it
e2e-compile:
	@echo "Compiling e2e test suite..."
	$(GO) test -tags=e2e ./tests/e2e -run TestDoesNotExist

## e2e-up: Start Docker services required by e2e tests
e2e-up:
	@echo "Starting e2e services..."
	$(DOCKER_COMPOSE) -f tests/e2e/docker-compose.yml up -d

## e2e-down: Stop Docker services required by e2e tests
e2e-down:
	@echo "Stopping e2e services..."
	$(DOCKER_COMPOSE) -f tests/e2e/docker-compose.yml down -v

## e2e: Run the e2e test suite
e2e:
	@echo "Running e2e tests..."
	OPENAI_BASE_URL=$(E2E_OPENAI_BASE_URL) \
	OPENAI_API_KEY=$(E2E_OPENAI_API_KEY) \
	OPENAI_MODEL=$(E2E_OPENAI_MODEL) \
	ANTHROPIC_BASE_URL=$(E2E_ANTHROPIC_BASE_URL) \
	ANTHROPIC_AUTH_TOKEN=$(E2E_ANTHROPIC_AUTH_TOKEN) \
	ANTHROPIC_MODEL=$(E2E_ANTHROPIC_MODEL) \
	EMBEDDING_MODEL=$(E2E_EMBEDDING_MODEL) \
	$(GO) test $(GOFLAGS) -tags=e2e -timeout 20m ./tests/e2e

## lint: Run golangci-lint
lint:
	@echo "Running linters..."
	$(GOLANGCI_LINT) run ./...

## fmt: Format all Go files
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	gofumpt -l -w .
	gci write --skip-generated -s standard -s default -s "prefix($(MODULE))" .

## fix: Run go fix on all packages
fix:
	@echo "Running go fix..."
	$(GO) fix ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

## tidy: Tidy go modules
tidy:
	@echo "Tidying go modules..."
	$(GO) mod tidy
	$(GO) mod verify

## clean: Clean build artifacts and cache
clean:
	@echo "Cleaning..."
	rm -f coverage.out coverage.html

## license: Add Apache license headers to all Go files
license:
	@echo "Adding license headers..."
	find . -type f -name '*.go' -exec addlicense -c "Simone Vellei" -l apache {} +

## doc: Generate Go documentation in HTML format
doc:
	@echo "Generating Go documentation..."
	@echo "Open http://localhost:6060/pkg/github.com/henomis/phero/ in your browser. Press Ctrl+C to stop."
	godoc -http=:6060

## check: Run all checks (test + lint + vet)
check: test lint vet
	@echo "All checks passed!"
