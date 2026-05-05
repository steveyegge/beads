#!/usr/bin/env bash
# benchstat-postgres.sh — print the benchstat delta between the two
# most recent bench/postgres-*.txt artifacts. Informational; never
# exits non-zero so a missing baseline (first run) does not break
# `make bench-postgres`.
#
# Install benchstat:
#   go install golang.org/x/perf/cmd/benchstat@latest
#
# Layout produced by `make bench-postgres`:
#   bench/postgres-<short-sha>.txt
# Sorted by mtime so manual edits or SHA collisions cannot reorder.

set -euo pipefail

shopt -s nullglob
artifacts=(bench/postgres-*.txt)
if [ "${#artifacts[@]}" -eq 0 ]; then
    echo "benchstat: no bench/postgres-*.txt artifacts yet"
    exit 0
fi

# Sort newest-first by mtime.
mapfile -t latest < <(ls -t bench/postgres-*.txt 2>/dev/null)

if [ "${#latest[@]}" -lt 2 ]; then
    echo "benchstat: only one artifact (${latest[0]}); no baseline to compare"
    exit 0
fi

curr="${latest[0]}"
prev="${latest[1]}"

# `make bench-postgres` runs with a minimal PATH that may not include the
# user's go bin. Probe the standard locations so a normal `go install
# benchstat` is enough to make the delta visible.
benchstat_bin=$(command -v benchstat 2>/dev/null || true)
if [ -z "$benchstat_bin" ]; then
    for cand in "${GOBIN:-}" "$(go env GOBIN 2>/dev/null)" "$(go env GOPATH 2>/dev/null)/bin" "$HOME/go/bin"; do
        if [ -n "$cand" ] && [ -x "$cand/benchstat" ]; then
            benchstat_bin="$cand/benchstat"
            break
        fi
    done
fi

if [ -z "$benchstat_bin" ]; then
    echo "benchstat not installed; skipping delta." >&2
    echo "Install with: go install golang.org/x/perf/cmd/benchstat@latest" >&2
    exit 0
fi

echo
echo "=== benchstat $prev -> $curr ==="
"$benchstat_bin" "$prev" "$curr"
