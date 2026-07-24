# Xeet Makefile

# Build variables
BINARY_NAME := xeet
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

# Go variables
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod

# Keep in step with .github/workflows/ci.yml
STATICCHECK_VERSION := v0.7.0

# Directories
DIST_DIR := dist
BUILD_DIR := build

# Default target
.PHONY: all
all: clean build

# Build for current platform
.PHONY: build
build:
	@echo "Building $(BINARY_NAME) for current platform..."
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

# Build for all supported platforms
.PHONY: build-all
build-all: clean
	@echo "Building $(BINARY_NAME) for all platforms..."
	@mkdir -p $(DIST_DIR)

	# macOS AMD64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 .

	# macOS ARM64 (Apple Silicon)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 .

	# Linux AMD64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 .

	# Linux ARM64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 .

	@echo "Built binaries for all platforms in $(DIST_DIR)/"

# Install locally
.PHONY: install
install: build
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	@if [ -w /usr/local/bin ]; then \
		mv $(BINARY_NAME) /usr/local/bin/; \
	else \
		sudo mv $(BINARY_NAME) /usr/local/bin/; \
	fi
	@echo "$(BINARY_NAME) installed successfully!"

# Uninstall
.PHONY: uninstall
uninstall:
	@echo "Removing $(BINARY_NAME) from /usr/local/bin..."
	@if [ -w /usr/local/bin ]; then \
		rm -f /usr/local/bin/$(BINARY_NAME); \
	else \
		sudo rm -f /usr/local/bin/$(BINARY_NAME); \
	fi
	@echo "$(BINARY_NAME) uninstalled successfully!"

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

# Vet code
.PHONY: vet
vet:
	@echo "Vetting code..."
	$(GOCMD) vet ./...

# Lint code with the same staticcheck version CI runs
.PHONY: lint
lint:
	@echo "Linting code..."
	$(GOCMD) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

# Tidy dependencies
.PHONY: tidy
tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy

# Update dependencies
.PHONY: update
update:
	@echo "Updating dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	$(GOCLEAN)
	@rm -rf $(BINARY_NAME) $(DIST_DIR) $(BUILD_DIR) coverage.out coverage.html

# Development setup
.PHONY: dev-setup
dev-setup:
	@echo "Setting up development environment..."
	$(GOMOD) download
	$(GOMOD) tidy
	@echo "Development environment ready! (nix-shell also provides go, gopls, and staticcheck)"

# Create release (for GitHub Actions)
.PHONY: release
release: build-all
	@echo "Creating release assets..."
	@cd $(DIST_DIR) && \
	for file in *; do \
		if [ -f "$$file" ]; then \
			if command -v sha256sum >/dev/null 2>&1; then \
				sha256sum "$$file" > "$$file.sha256"; \
			else \
				shasum -a 256 "$$file" > "$$file.sha256"; \
			fi; \
		fi; \
	done
	@echo "Release assets created with checksums"

# Quick development build and test
.PHONY: dev
dev: clean fmt vet build test
	@echo "Development build completed successfully!"

# CI/CD pipeline
.PHONY: ci
ci: tidy fmt vet lint test build-all
	@echo "CI pipeline completed successfully!"

# Help
.PHONY: help
help:
	@echo "Available commands:"
	@echo "  build        Build for current platform"
	@echo "  build-all    Build for all platforms"
	@echo "  install      Install binary locally"
	@echo "  uninstall    Remove installed binary"
	@echo "  test         Run tests"
	@echo "  test-coverage Run tests with coverage"
	@echo "  fmt          Format code"
	@echo "  vet          Vet code"
	@echo "  lint         Lint code"
	@echo "  tidy         Tidy dependencies"
	@echo "  update       Update dependencies"
	@echo "  clean        Clean build artifacts"
	@echo "  dev-setup    Set up development environment"
	@echo "  release      Create release assets"
	@echo "  dev          Quick development build"
	@echo "  ci           Run CI/CD pipeline"
	@echo "  help         Show this help"