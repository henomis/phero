.PHONY: all test lint fmt vet clean coverage help license

# Variables
GO := go
GOFLAGS := -v
MODULE := $(shell $(GO) list -m)
PKGS := $(shell $(GO) list ./... | grep -v /examples/)
GOLANGCI_LINT := golangci-lint

# Default target
all: test lint

## help: Display this help message
help:
	@echo "Available targets:"
	@echo "  make test       - Run all tests"
	@echo "  make lint       - Run golangci-lint"
	@echo "  make fmt        - Format all Go files"
	@echo "  make vet        - Run go vet"
	@echo "  make coverage   - Generate test coverage report"
	@echo "  make clean      - Clean build artifacts and cache"
	@echo "  make license    - Add license headers to all Go files"
	@echo "  make help       - Display this help message"

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
	addlicense -c "Simone Vellei" -l apache ./**/*.go

## doc: Generate Go documentation in HTML format
doc:
	@echo "Generating Go documentation..."
	@echo "Open http://localhost:6060/pkg/github.com/henomis/phero/ in your browser. Press Ctrl+C to stop."
	godoc -http=:6060

## check: Run all checks (test + lint + vet)
check: test lint vet
	@echo "All checks passed!"
