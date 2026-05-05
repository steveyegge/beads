#!/usr/bin/env bash
# check-backend-flags.sh — Enforce the FR-7 no-new-flag invariant from
# ADR be-l7t.6 / bead be-6fk.6.
#
# bd's two storage backends (Dolt and Postgres) MUST present identical flag
# sets on every subcommand except `bd init` and `bd migrate`, which are
# intentionally divergent. This script snapshots the current cobra flag
# surface and diffs it against a committed fixture; any add/remove on a
# non-divergent subcommand fails CI loudly.
#
# Usage:
#   ./scripts/check-backend-flags.sh                  # check vs. fixture
#   ./scripts/check-backend-flags.sh --update         # rewrite fixture
#   ./scripts/check-backend-flags.sh /path/to/bd      # custom binary
#
# Exit codes:
#   0 - Flag tree matches fixture (or fixture rewritten with --update)
#   1 - Flag tree differs from fixture; review changes

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
FIXTURE="$PROJECT_ROOT/scripts/testdata/backend-flags.txt"
EXCLUDED_SUBCOMMANDS=("init" "migrate")

UPDATE=0
BD="bd"
for arg in "$@"; do
    case "$arg" in
        --update) UPDATE=1 ;;
        *)        BD="$arg" ;;
    esac
done

# Build a fresh bd binary if no path was supplied; using a stale binary
# would mask new flags added in the working tree.
if [[ "$BD" == "bd" ]]; then
    if [[ ! -x "$PROJECT_ROOT/bd" ]] || [[ "$PROJECT_ROOT/cmd/bd" -nt "$PROJECT_ROOT/bd" ]]; then
        (cd "$PROJECT_ROOT" && go build -tags gms_pure_go -o ./bd ./cmd/bd/) >/dev/null
    fi
    BD="$PROJECT_ROOT/bd"
fi

if [[ ! -x "$BD" ]]; then
    echo "Error: bd binary not found at '$BD'" >&2
    exit 2
fi

# is_excluded returns 0 if the subcommand is in the excluded list.
is_excluded() {
    local sub="$1"
    for ex in "${EXCLUDED_SUBCOMMANDS[@]}"; do
        if [[ "$sub" == "$ex" ]]; then
            return 0
        fi
    done
    return 1
}

# extract_subcommands lists the top-level subcommands from `bd help`.
# Output is sorted, one per line. bd's help groups commands by category
# (e.g. "Working With Issues:") rather than the cobra default
# "Available Commands:" header. Strategy:
#
#   - toggle "in_cmds" on whenever a section header (`^[A-Z][^:]*:$`) is
#     seen, with the explicit exception of "Usage:" and "Flags:" which
#     are not subcommand sections;
#   - print the first whitespace-delimited token of every indented line
#     while in_cmds is active.
#
# Filters out "bd" itself (appears in the "Usage: bd [command]" block)
# even if Usage somehow leaks through, since it is the binary name and
# `bd bd --help` is not a real subcommand.
extract_subcommands() {
    "$BD" help 2>/dev/null \
        | awk '
            /^Usage:/        { in_cmds=0; next }
            /^Flags:/        { in_cmds=0; next }
            /^Global Flags:/ { in_cmds=0; next }
            /^Use "bd /      { in_cmds=0; next }
            /^[A-Z][^:]*:$/  { in_cmds=1; next }
            in_cmds && /^  [a-z][a-zA-Z0-9_-]*[ \t]/ { print $1 }
        ' \
        | grep -v '^bd$' \
        | sort -u
}

# extract_flags prints the flags for a subcommand as a sorted set, one
# per line. Long flag form (--foo); we ignore short flags because cobra
# autogenerates them and they are not part of the stable contract.
extract_flags() {
    local sub="$1"
    "$BD" "$sub" --help 2>/dev/null \
        | awk '
            /^Flags:/             { in_flags=1; next }
            /^Global Flags:/      { in_flags=0; next }
            /^Use "bd /           { in_flags=0 }
            in_flags && /^      --/ { match($0, /--[a-zA-Z][-a-zA-Z0-9_]*/); print substr($0, RSTART, RLENGTH) }
            in_flags && /^  -[a-zA-Z],/ { match($0, /--[a-zA-Z][-a-zA-Z0-9_]*/); print substr($0, RSTART, RLENGTH) }
        ' \
        | sort -u
}

# generate_flag_tree writes the canonical fixture text to stdout.
generate_flag_tree() {
    echo "# bd backend flag-tree fixture (be-6fk.6 FR-7 enforcement)"
    echo "# Excluded: ${EXCLUDED_SUBCOMMANDS[*]}"
    echo "# Regenerate via: scripts/check-backend-flags.sh --update"
    echo
    while read -r sub; do
        if is_excluded "$sub"; then
            continue
        fi
        # extract_flags may return non-zero when the subcommand is an
        # alias or has been renamed since the fixture was last regenerated;
        # tolerate the failure rather than aborting the whole sweep.
        local flags=""
        flags="$(extract_flags "$sub" || true)"
        if [[ -z "$flags" ]]; then
            continue
        fi
        echo "## $sub"
        echo "$flags"
        echo
    done < <(extract_subcommands)
}

if [[ "$UPDATE" == "1" ]]; then
    mkdir -p "$(dirname "$FIXTURE")"
    generate_flag_tree > "$FIXTURE"
    echo "Updated fixture: $FIXTURE"
    exit 0
fi

if [[ ! -f "$FIXTURE" ]]; then
    echo "Error: fixture not found at $FIXTURE" >&2
    echo "Generate it once with: scripts/check-backend-flags.sh --update" >&2
    exit 2
fi

CURRENT="$(generate_flag_tree)"
EXPECTED="$(cat "$FIXTURE")"

if [[ "$CURRENT" == "$EXPECTED" ]]; then
    echo "OK: backend flag tree matches fixture"
    exit 0
fi

echo "FAIL: backend flag tree drifted from fixture"
echo
echo "  fixture: $FIXTURE"
echo "  excluded subcommands (intentionally divergent): ${EXCLUDED_SUBCOMMANDS[*]}"
echo
echo "Diff (current vs. fixture):"
diff -u <(echo "$EXPECTED") <(echo "$CURRENT") || true
echo
echo "If the diff is intentional (e.g., a new bd-init / bd-migrate flag), regenerate:"
echo "  scripts/check-backend-flags.sh --update"
echo "and commit the updated fixture."
exit 1
