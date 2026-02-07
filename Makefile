# Makefile for beads project

.PHONY: all build test bench bench-quick bench-dolt bench-dolt-quick bench-compare clean install help \
       docker-build docker-push docker-test

# Default target
all: build

BINARY := bd
BUILD_DIR := .
INSTALL_DIR := $(HOME)/.local/bin

# On macOS, set CGO flags for Homebrew's keg-only icu4c
ifeq ($(shell uname),Darwin)
  ICU_PREFIX := $(shell brew --prefix icu4c 2>/dev/null)
  ifneq ($(ICU_PREFIX),)
    export CGO_CFLAGS   += -I$(ICU_PREFIX)/include
    export CGO_CXXFLAGS += -I$(ICU_PREFIX)/include
    export CGO_LDFLAGS  += -L$(ICU_PREFIX)/lib
  endif
endif

# Build the bd binary
build:
	@echo "Building bd..."
	go build -ldflags="-X main.Build=$$(git rev-parse --short HEAD)" -o $(BUILD_DIR)/$(BINARY) ./cmd/bd
ifeq ($(shell uname),Darwin)
	@codesign -s - -f $(BUILD_DIR)/$(BINARY) 2>/dev/null || true
	@echo "Signed $(BINARY) for macOS"
endif

# Run all tests (skips known broken tests listed in .test-skip)
test:
	@echo "Running tests..."
	@TEST_COVER=1 ./scripts/test.sh

# Run performance benchmarks (10K and 20K issue databases with automatic CPU profiling)
# Generates CPU profile: internal/storage/sqlite/bench-cpu-<timestamp>.prof
# View flamegraph: go tool pprof -http=:8080 <profile-file>
bench:
	@echo "Running performance benchmarks..."
	@echo "This will generate 10K and 20K issue databases and profile all operations."
	@echo "CPU profiles will be saved to internal/storage/sqlite/"
	@echo ""
	go test -bench=. -benchtime=1s -tags=bench -run=^$$ ./internal/storage/sqlite/ -timeout=30m
	@echo ""
	@echo "Benchmark complete. Profile files saved in internal/storage/sqlite/"
	@echo "View flamegraph: cd internal/storage/sqlite && go tool pprof -http=:8080 bench-cpu-*.prof"

# Run quick benchmarks (shorter benchtime for faster feedback)
bench-quick:
	@echo "Running quick performance benchmarks..."
	go test -bench=. -benchtime=100ms -tags=bench -run=^$$ ./internal/storage/sqlite/ -timeout=15m

# Run Dolt performance benchmarks
# Requires: Dolt installed (brew install dolt or from https://doltdb.com)
bench-dolt:
	@echo "Running Dolt performance benchmarks..."
	@echo "This measures bootstrap time, CRUD operations, and query performance."
	@echo ""
	@if ! command -v dolt >/dev/null 2>&1; then \
		echo "Error: Dolt not installed. Install with: brew install dolt"; \
		exit 1; \
	fi
	go test -bench=. -benchmem -benchtime=1s -run=^$$ ./internal/storage/dolt/ -timeout=30m
	@echo ""
	@echo "Dolt benchmark complete."

# Run quick Dolt benchmarks
bench-dolt-quick:
	@echo "Running quick Dolt benchmarks..."
	@if ! command -v dolt >/dev/null 2>&1; then \
		echo "Error: Dolt not installed. Install with: brew install dolt"; \
		exit 1; \
	fi
	go test -bench=. -benchmem -benchtime=100ms -run=^$$ ./internal/storage/dolt/ -timeout=15m

# Run comparison benchmarks: SQLite vs Dolt
# Outputs both side-by-side for easy comparison
bench-compare:
	@echo "=== SQLite vs Dolt Performance Comparison ==="
	@echo ""
	@echo "--- SQLite Benchmarks ---"
	@go test -bench='Benchmark(Create|Get|Search|Ready)' -benchmem -benchtime=500ms -run=^$$ ./internal/storage/sqlite/ 2>/dev/null || echo "SQLite benchmarks skipped"
	@echo ""
	@echo "--- Dolt Benchmarks ---"
	@if command -v dolt >/dev/null 2>&1; then \
		go test -bench='Benchmark(Create|Get|Search|Ready)' -benchmem -benchtime=500ms -run=^$$ ./internal/storage/dolt/ 2>/dev/null || echo "Dolt benchmarks failed"; \
	else \
		echo "Dolt not installed - skipping Dolt benchmarks"; \
	fi
	@echo ""
	@echo "Compare the ns/op values to see relative performance."

# Install bd to ~/.local/bin (builds, signs on macOS, and copies)
install: build
	@mkdir -p $(INSTALL_DIR)
	@rm -f $(INSTALL_DIR)/$(BINARY)
	@cp $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed $(BINARY) to $(INSTALL_DIR)/$(BINARY)"

# Clean build artifacts and benchmark profiles
clean:
	@echo "Cleaning..."
	rm -f bd
	rm -f internal/storage/sqlite/bench-cpu-*.prof
	rm -f beads-perf-*.prof

# Docker image settings
DOCKER_REGISTRY ?= ghcr.io
DOCKER_REPO     ?= groblegark/beads
DOCKER_TAG      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DOCKER_IMAGE    := $(DOCKER_REGISTRY)/$(DOCKER_REPO):$(DOCKER_TAG)
DOCKER_LATEST   := $(DOCKER_REGISTRY)/$(DOCKER_REPO):latest

# Build Docker image
docker-build:
	@echo "Building Docker image $(DOCKER_IMAGE)..."
	docker build \
		--build-arg VERSION=$(DOCKER_TAG) \
		--build-arg BUILD_COMMIT=$$(git rev-parse --short HEAD) \
		-t $(DOCKER_IMAGE) \
		-t $(DOCKER_LATEST) \
		.
	@echo "Built $(DOCKER_IMAGE)"

# Push Docker image to registry
docker-push: docker-build
	@echo "Pushing $(DOCKER_IMAGE)..."
	docker push $(DOCKER_IMAGE)
	docker push $(DOCKER_LATEST)
	@echo "Pushed $(DOCKER_IMAGE) and $(DOCKER_LATEST)"

# Test Docker image (build and run health check)
docker-test: docker-build
	@echo "Testing Docker image..."
	@CONTAINER=$$(docker run -d --name bd-test-$$$$ $(DOCKER_IMAGE)); \
	echo "Started container $$CONTAINER"; \
	sleep 5; \
	if docker exec $$CONTAINER nc -z localhost 9877; then \
		echo "Health check passed"; \
		docker stop $$CONTAINER >/dev/null; \
		docker rm $$CONTAINER >/dev/null; \
	else \
		echo "Health check failed"; \
		docker logs $$CONTAINER; \
		docker stop $$CONTAINER >/dev/null; \
		docker rm $$CONTAINER >/dev/null; \
		exit 1; \
	fi

# Show help
help:
	@echo "Beads Makefile targets:"
	@echo "  make build           - Build the bd binary"
	@echo "  make test            - Run all tests"
	@echo "  make bench           - Run SQLite performance benchmarks"
	@echo "  make bench-quick     - Run quick SQLite benchmarks"
	@echo "  make bench-dolt      - Run Dolt performance benchmarks"
	@echo "  make bench-dolt-quick - Run quick Dolt benchmarks"
	@echo "  make bench-compare   - Compare SQLite vs Dolt performance"
	@echo "  make install         - Install bd to ~/.local/bin (with codesign on macOS)"
	@echo "  make clean           - Remove build artifacts and profile files"
	@echo "  make docker-build    - Build Docker image"
	@echo "  make docker-push     - Build and push Docker image to registry"
	@echo "  make docker-test     - Build and test Docker image health check"
	@echo "  make help            - Show this help message"
	@echo ""
	@echo "Docker variables:"
	@echo "  DOCKER_REGISTRY=$(DOCKER_REGISTRY)"
	@echo "  DOCKER_REPO=$(DOCKER_REPO)"
	@echo "  DOCKER_TAG=$(DOCKER_TAG)"
