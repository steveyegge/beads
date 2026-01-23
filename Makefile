# Makefile for beads project

.PHONY: all build test bench bench-quick bench-dolt bench-dolt-quick bench-compare clean install help

# Default target
all: build

# Build the bd binary
build:
	@echo "Building bd..."
	go build -ldflags="-X main.Build=$$(git rev-parse --short HEAD)" -o bd ./cmd/bd

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

# Install bd to GOPATH/bin
install:
	@echo "Installing bd to $$(go env GOPATH)/bin..."
	@bash -c 'commit=$$(git rev-parse HEAD 2>/dev/null || echo ""); \
		branch=$$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo ""); \
		go install -ldflags="-X main.Commit=$$commit -X main.Branch=$$branch" ./cmd/bd'

# Clean build artifacts and benchmark profiles
clean:
	@echo "Cleaning..."
	rm -f bd
	rm -f internal/storage/sqlite/bench-cpu-*.prof
	rm -f beads-perf-*.prof

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
	@echo "  make install         - Install bd to GOPATH/bin"
	@echo "  make clean           - Remove build artifacts and profile files"
	@echo "  make help            - Show this help message"
