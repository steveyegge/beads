#!/bin/bash
# docs-drift-runner.sh — Execute a doc recipe, generate evidence artifacts,
# and detect drift against checked-in docs.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

RECIPE_PATH="${1:-$PROJECT_ROOT/docs/recipes/cli-reference.recipe.yaml}"
BD_BIN="${2:-$PROJECT_ROOT/bd}"
ARTIFACT_DIR="${3:-$PROJECT_ROOT/.amp/in/artifacts/docs-drift}"

if [ ! -f "$RECIPE_PATH" ]; then
    echo "Error: recipe not found: $RECIPE_PATH"
    exit 1
fi

if [ ! -x "$BD_BIN" ]; then
    if [ "$BD_BIN" = "$PROJECT_ROOT/bd" ]; then
        echo "bd binary not found at $BD_BIN; building a local binary..."
        (cd "$PROJECT_ROOT" && CGO_ENABLED=0 go build -o bd ./cmd/bd/)
    else
        echo "Error: bd binary not executable: $BD_BIN"
        exit 1
    fi
fi

mkdir -p "$ARTIFACT_DIR"
TMPDIR_RUN="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_RUN"' EXIT

REPORT_MD="$ARTIFACT_DIR/report.md"
REPORT_JSON="$ARTIFACT_DIR/report.json"
COMBINED_HELP_OUT="$ARTIFACT_DIR/combined-help.md"
COMBINED_DIFF_OUT="$ARTIFACT_DIR/cli-reference.diff"
GENERATED_DOCS_DIR="$ARTIFACT_DIR/generated-cli-reference"
WEBSITE_DIFF_DIR="$ARTIFACT_DIR/website-diffs"

get_recipe_run_command() {
    local target="$1"
    awk -v target="$target" '
        match($0, /^[[:space:]]*-[[:space:]]*name:[[:space:]]*"?([^"[:space:]]+)"?[[:space:]]*$/, m) {
            current = m[1]
            next
        }
        current == target && match($0, /^[[:space:]]*run:[[:space:]]*"([^"]+)"[[:space:]]*$/, m) {
            print m[1]
            exit
        }
    ' "$RECIPE_PATH"
}

get_recipe_shell_checks() {
    awk '
        /^[[:space:]]*shell_checks:[[:space:]]*$/ {
            in_shell = 1
            next
        }
        in_shell && /^[[:space:]]*[a-z_]+:[[:space:]]*$/ {
            in_shell = 0
        }
        in_shell && match($0, /^[[:space:]]*-[[:space:]]*"?([^"]+)"?[[:space:]]*$/, m) {
            print m[1]
        }
    ' "$RECIPE_PATH"
}

get_recipe_watch_refs() {
    awk '
        /^[[:space:]]*watch:[[:space:]]*$/ {
            in_watch = 1
            next
        }
        in_watch && /^[[:space:]]*[a-z_]+:[[:space:]]*$/ {
            in_watch = 0
        }
        in_watch && match($0, /^[[:space:]]*-[[:space:]]*"?([^"]+)"?[[:space:]]*$/, m) {
            print m[1]
        }
    ' "$RECIPE_PATH"
}

run_recipe_command() {
    local template="$1"
    local command_arg="${2:-}"
    local expanded="$template"
    local bd_escaped

    if [ -n "$command_arg" ]; then
        expanded="${expanded//<command>/$command_arg}"
    fi

    bd_escaped="$(printf '%q' "$BD_BIN")"
    if [[ "$expanded" == bd\ * ]]; then
        expanded="$bd_escaped${expanded#bd}"
    fi

    # shellcheck disable=SC2086
    eval "$expanded"
}

extract_top_level_commands_from_help() {
    local help_file="$1"
    grep -oP '^### bd [a-z][-a-z]*$' "$help_file" | sed 's/^### bd //' | sort -u
}

extract_doc_commands_from_cli_reference() {
    local cli_ref_file="$1"
    grep -oP '\bbd [a-z][-a-z]*\b' "$cli_ref_file" | sed 's/^bd //' | sort -u
}

COMBINED_RUN="$(get_recipe_run_command "combined_help")"
LIST_RUN="$(get_recipe_run_command "command_list")"
DOC_RUN="$(get_recipe_run_command "single_command_doc")"

if [ -z "$COMBINED_RUN" ]; then
    echo "Error: recipe missing required combined_help command"
    exit 1
fi

echo "Running recipe: $RECIPE_PATH"
echo "Using bd binary: $BD_BIN"

HAS_COMMAND_LIST=false
HAS_SINGLE_DOC=false

if [ -n "$LIST_RUN" ] && run_recipe_command "$LIST_RUN" > "$TMPDIR_RUN/command-list.txt" 2>/dev/null; then
    HAS_COMMAND_LIST=true
fi

if [ "$HAS_COMMAND_LIST" = true ] && [ -n "$DOC_RUN" ]; then
    SAMPLE_COMMAND="$(head -n 1 "$TMPDIR_RUN/command-list.txt" || true)"
    if [ -n "$SAMPLE_COMMAND" ] && run_recipe_command "$DOC_RUN" "$SAMPLE_COMMAND" > "$TMPDIR_RUN/sample-doc.md" 2>/dev/null; then
        HAS_SINGLE_DOC=true
    fi
fi

echo "Collecting combined help output..."
run_recipe_command "$COMBINED_RUN" > "$COMBINED_HELP_OUT"

TOTAL_DRIFT=0
WEBSITE_DRIFT_COUNT=0
COMBINED_DRIFT=0

if [ -f "$PROJECT_ROOT/docs/CLI_REFERENCE.md" ]; then
    if ! diff -u "$PROJECT_ROOT/docs/CLI_REFERENCE.md" "$COMBINED_HELP_OUT" > "$COMBINED_DIFF_OUT"; then
        COMBINED_DRIFT=1
        TOTAL_DRIFT=$((TOTAL_DRIFT + 1))
    else
        rm -f "$COMBINED_DIFF_OUT"
    fi
fi

mkdir -p "$GENERATED_DOCS_DIR" "$WEBSITE_DIFF_DIR"

if [ "$HAS_COMMAND_LIST" = true ] && [ "$HAS_SINGLE_DOC" = true ]; then
    echo "Generating per-command docs from live help tree..."
    while IFS= read -r cmd; do
        [ -z "$cmd" ] && continue

        safe_name="${cmd// /-}"
        generated_file="$GENERATED_DOCS_DIR/$safe_name.md"
        target_file="$PROJECT_ROOT/website/docs/cli-reference/$safe_name.md"
        diff_file="$WEBSITE_DIFF_DIR/$safe_name.diff"

        run_recipe_command "$DOC_RUN" "$cmd" > "$generated_file"

        if [ -f "$target_file" ]; then
            if ! diff -u "$target_file" "$generated_file" > "$diff_file"; then
                WEBSITE_DRIFT_COUNT=$((WEBSITE_DRIFT_COUNT + 1))
                TOTAL_DRIFT=$((TOTAL_DRIFT + 1))
            else
                rm -f "$diff_file"
            fi
        else
            WEBSITE_DRIFT_COUNT=$((WEBSITE_DRIFT_COUNT + 1))
            TOTAL_DRIFT=$((TOTAL_DRIFT + 1))
            printf 'Missing checked-in file for command: %s\n' "$cmd" > "$diff_file"
        fi
    done < "$TMPDIR_RUN/command-list.txt"
fi

CHECKS_PASSED=0
CHECKS_FAILED=0
CHECK_LOG="$ARTIFACT_DIR/shell-checks.log"
: > "$CHECK_LOG"

while IFS= read -r check_cmd; do
    [ -z "$check_cmd" ] && continue
    echo "Running shell check: $check_cmd" | tee -a "$CHECK_LOG"
    if (cd "$PROJECT_ROOT" && eval "$check_cmd") >> "$CHECK_LOG" 2>&1; then
        CHECKS_PASSED=$((CHECKS_PASSED + 1))
    else
        CHECKS_FAILED=$((CHECKS_FAILED + 1))
    fi
done < <(get_recipe_shell_checks)

HELP_COMMAND_COUNT="$(extract_top_level_commands_from_help "$COMBINED_HELP_OUT" | wc -l | tr -d ' ')"
DOC_COMMAND_COUNT="0"
if [ -f "$PROJECT_ROOT/docs/CLI_REFERENCE.md" ]; then
    DOC_COMMAND_COUNT="$(extract_doc_commands_from_cli_reference "$PROJECT_ROOT/docs/CLI_REFERENCE.md" | wc -l | tr -d ' ')"
fi

mapfile -t WATCH_REFS < <(get_recipe_watch_refs)

{
    echo "# Docs Drift Report"
    echo
    echo "- Recipe: $RECIPE_PATH"
    echo "- Timestamp (UTC): $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
    echo "- bd binary: $BD_BIN"
    echo "- Capability: command_list = $HAS_COMMAND_LIST"
    echo "- Capability: single_command_doc = $HAS_SINGLE_DOC"
    echo
    echo "## Drift Summary"
    echo
    echo "- Combined CLI reference drift: $COMBINED_DRIFT"
    echo "- Website command doc drift count: $WEBSITE_DRIFT_COUNT"
    echo "- Total drift findings: $TOTAL_DRIFT"
    echo
    echo "## Coverage Snapshot"
    echo
    echo "- Top-level commands in live help: $HELP_COMMAND_COUNT"
    echo "- Distinct command tokens in docs/CLI_REFERENCE.md: $DOC_COMMAND_COUNT"
    echo
    echo "## Shell Check Results"
    echo
    echo "- Passed: $CHECKS_PASSED"
    echo "- Failed: $CHECKS_FAILED"
    echo "- Log: $CHECK_LOG"
    echo
    echo "## Watched Refs"
    echo
    if [ ${#WATCH_REFS[@]} -eq 0 ]; then
        echo "- (none)"
    else
        for ref in "${WATCH_REFS[@]}"; do
            echo "- $ref"
        done
    fi
} > "$REPORT_MD"

{
    printf '{\n'
    printf '  "recipe": "%s",\n' "$RECIPE_PATH"
    printf '  "timestamp_utc": "%s",\n' "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
    printf '  "bd_binary": "%s",\n' "$BD_BIN"
    printf '  "capabilities": {"command_list": %s, "single_command_doc": %s},\n' "$HAS_COMMAND_LIST" "$HAS_SINGLE_DOC"
    printf '  "drift": {"combined_cli_reference": %d, "website_count": %d, "total": %d},\n' "$COMBINED_DRIFT" "$WEBSITE_DRIFT_COUNT" "$TOTAL_DRIFT"
    printf '  "checks": {"passed": %d, "failed": %d},\n' "$CHECKS_PASSED" "$CHECKS_FAILED"
    printf '  "watch_refs": ['
    if [ ${#WATCH_REFS[@]} -gt 0 ]; then
        for i in "${!WATCH_REFS[@]}"; do
            if [ "$i" -gt 0 ]; then
                printf ', '
            fi
            printf '"%s"' "${WATCH_REFS[$i]}"
        done
    fi
    printf ']\n'
    printf '}\n'
} > "$REPORT_JSON"

echo "Drift report written to: $REPORT_MD"
echo "JSON summary written to: $REPORT_JSON"

if [ "$CHECKS_FAILED" -gt 0 ]; then
    echo "Result: FAILED (shell checks)"
    exit 1
fi

if [ "$TOTAL_DRIFT" -gt 0 ]; then
    echo "Result: DRIFT DETECTED"
    exit 2
fi

echo "Result: CLEAN"
exit 0
