# Copyright 2026 Authors of spidernet-io
# SPDX-License-Identifier: Apache-2.0

# Project settings
PROJECT_NAME := vlan-cni
BINARY_NAME := vlan
CMD_PATH := ./cmd/vlan
BUILD_DIR := ./bin

# Go settings
GO := go
GOFLAGS := -v
LDFLAGS := -ldflags "-s -w"

# Test settings
GINKGO := ginkgo
TEST_TIMEOUT := 2m

.PHONY: all build unit-tests clean help

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Run unit tests
unit-tests:
	@echo "Running unit tests..."
	$(GO) run github.com/onsi/ginkgo/v2/ginkgo@v2.17.1 run -r --race --trace --timeout=$(TEST_TIMEOUT) --cover --coverprofile=coverage.out
	@echo "Unit tests complete"

# Run unit tests (fallback without ginkgo CLI)
test:
	@echo "Running tests with go test..."
	$(GO) test -v -race -timeout=$(TEST_TIMEOUT) ./pkg/...

# Generate test coverage report
coverage: unit-tests
	@echo "Generating coverage report..."
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	$(GO) clean
	@echo "Clean complete"

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# Lint code (requires golangci-lint)
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "Running golangci-lint..."; \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping..."; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Verify go.mod
verify:
	@echo "Verifying dependencies..."
	$(GO) mod verify

# CI target (build + test)
ci: deps build unit-tests

# Release build with version info
release:
	@echo "Building release binary..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Release build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Help
help:
	@echo "Available targets:"
	@echo "  make build       - Build the binary"
	@echo "  make unit-tests  - Run unit tests with Ginkgo"
	@echo "  make test        - Run tests with go test"
	@echo "  make coverage    - Generate test coverage report"
	@echo "  make clean       - Clean build artifacts"
	@echo "  make deps        - Download and tidy dependencies"
	@echo "  make lint        - Run linter"
	@echo "  make fmt         - Format code"
	@echo "  make verify      - Verify dependencies"
	@echo "  make ci          - Run CI pipeline (deps + build + test)"
	@echo "  make release     - Build release binary"
	@echo "  make help        - Show this help"
