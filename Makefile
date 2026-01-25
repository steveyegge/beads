# Makefile for beads project

.PHONY: all build test bench bench-quick bench-cli bench-concurrency clean install help

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

# Run CLI benchmark suite (tests actual CLI latency, throughput, percentiles)
# Requires synthetic database - run 'make bench-quick' first to generate it
bench-cli:
	@echo "Running CLI benchmark suite..."
	@if [ ! -f /tmp/beads-bench-cache/large.db ]; then \
		echo "Synthetic database not found. Generating..."; \
		go test -tags=bench -bench=BenchmarkGetReadyWork_Large -benchmem ./internal/storage/sqlite/ -timeout=10m; \
	fi
	@mkdir -p benchmarks
	./scripts/benchmark-suite.sh --synthetic --iterations 20 --output benchmarks/baseline-$$(date +%Y%m%d).json

# Run concurrency stress test
bench-concurrency:
	@echo "Running concurrency stress test..."
	./scripts/test-concurrency.sh --parallel 10 --iterations 10

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
	@echo "  make build            - Build the bd binary"
	@echo "  make test             - Run all tests"
	@echo "  make bench            - Run Go performance benchmarks (generates CPU profiles)"
	@echo "  make bench-quick      - Run quick Go benchmarks (shorter benchtime)"
	@echo "  make bench-cli        - Run CLI benchmark suite (latency, throughput, percentiles)"
	@echo "  make bench-concurrency - Run concurrency stress test"
	@echo "  make install          - Install bd to GOPATH/bin"
	@echo "  make clean            - Remove build artifacts and profile files"
	@echo "  make help             - Show this help message"
