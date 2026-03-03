#!/usr/bin/env bash
# migrate-jsonl-to-dolt.sh — Import bd export JSONL files into a Dolt database
#
# Usage:
#   ./scripts/migrate-jsonl-to-dolt.sh .beads/backup/
#   ./scripts/migrate-jsonl-to-dolt.sh --port 3307 --db beads .beads/backup/
#   ./scripts/migrate-jsonl-to-dolt.sh --dry-run .beads/backup/
#
# Reads JSONL files produced by `bd export` and loads them into Dolt via
# MySQL protocol. Requires: jq, mysql CLI (or dolt sql-client).
#
# Fixes GH#2276.

set -euo pipefail

# Defaults
HOST="127.0.0.1"
PORT=3307
DB="beads"
USER="root"
DRY_RUN=false
BACKUP_DIR=""

# Colors (if terminal supports them)
if [[ -t 1 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    BOLD='\033[1m'
    NC='\033[0m'
else
    RED='' GREEN='' YELLOW='' BLUE='' BOLD='' NC=''
fi

usage() {
    cat <<EOF
Usage: $0 [OPTIONS] BACKUP_DIR

Import bd export JSONL files into a Dolt database via MySQL protocol.

Arguments:
  BACKUP_DIR        Directory containing JSONL files from 'bd export'

Options:
  --host HOST       Dolt server host (default: 127.0.0.1)
  --port PORT       Dolt server port (default: 3307)
  --db DATABASE     Target database name (default: beads)
  --user USER       MySQL user (default: root)
  --dry-run         Show generated SQL without executing
  -h, --help        Show this help message

Example:
  bd export                                          # produces .beads/backup/*.jsonl
  $0 .beads/backup/                                  # import into Dolt
  $0 --port 3307 --db beads .beads/backup/           # explicit connection params
  $0 --dry-run .beads/backup/                        # preview SQL only
EOF
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --host) HOST="$2"; shift 2 ;;
        --port) PORT="$2"; shift 2 ;;
        --db)   DB="$2"; shift 2 ;;
        --user) USER="$2"; shift 2 ;;
        --dry-run) DRY_RUN=true; shift ;;
        -h|--help) usage ;;
        -*) echo "Unknown option: $1"; exit 1 ;;
        *)  BACKUP_DIR="$1"; shift ;;
    esac
done

if [[ -z "$BACKUP_DIR" ]]; then
    echo "Error: BACKUP_DIR is required"
    echo "Run '$0 --help' for usage"
    exit 1
fi

if [[ ! -d "$BACKUP_DIR" ]]; then
    echo "Error: $BACKUP_DIR is not a directory"
    exit 1
fi

# Check dependencies
if ! command -v jq &>/dev/null; then
    echo "Error: jq is required but not found (brew install jq)"
    exit 1
fi

# Find SQL client
SQL_CMD=""
if command -v mysql &>/dev/null; then
    SQL_CMD="mysql"
elif command -v dolt &>/dev/null; then
    SQL_CMD="dolt"
else
    echo "Error: mysql or dolt CLI is required but neither was found"
    exit 1
fi

# Execute SQL from stdin
pipe_sql() {
    if [[ "$SQL_CMD" == "mysql" ]]; then
        mysql -h "$HOST" -P "$PORT" -u "$USER" "$DB"
    else
        dolt sql-client -h "$HOST" -P "$PORT" -u "$USER" -d "$DB"
    fi
}

# Escape a value for SQL. Reads from stdin.
sql_escape() {
    sed -e "s/\\\\/\\\\\\\\/g" -e "s/'/\\\\'/g"
}

# Counters
issues_count=0
labels_count=0
deps_count=0
deps_orphan=0
events_count=0
comments_count=0
config_count=0

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║          JSONL → Dolt Migration                            ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""
echo "Backup dir:  $BACKUP_DIR"
echo "Dolt server: $HOST:$PORT"
echo "Database:    $DB"
echo "SQL client:  $SQL_CMD"
if $DRY_RUN; then
    echo -e "Mode:        ${YELLOW}DRY RUN${NC} (SQL output only)"
fi
echo ""

# ── 1. Issues ──────────────────────────────────────────────────────
import_issues() {
    local file="$BACKUP_DIR/issues.jsonl"
    if [[ ! -f "$file" ]]; then
        echo -e "${YELLOW}⚠${NC}  issues.jsonl not found, skipping"
        return
    fi

    echo -ne "${BLUE}→${NC}  Importing issues..."

    local sql="START TRANSACTION;\n"
    while IFS= read -r line; do
        # Extract columns and values from JSON
        local cols vals
        cols=$(echo "$line" | jq -r 'keys_unsorted | map(if . == "key" then "`key`" else . end) | join(", ")')
        vals=""
        local first=true
        for col in $(echo "$line" | jq -r 'keys_unsorted[]'); do
            local raw
            raw=$(echo "$line" | jq -r --arg c "$col" '.[$c] // empty')
            local typ
            typ=$(echo "$line" | jq -r --arg c "$col" '.[$c] | type')
            local sql_val
            if [[ "$typ" == "null" ]]; then
                sql_val="NULL"
            elif [[ "$typ" == "number" ]]; then
                sql_val="$raw"
            elif [[ "$typ" == "boolean" ]]; then
                if [[ "$raw" == "true" ]]; then sql_val="1"; else sql_val="0"; fi
            elif [[ "$typ" == "object" || "$typ" == "array" ]]; then
                local escaped
                escaped=$(echo "$line" | jq -c --arg c "$col" '.[$c]' | sql_escape)
                sql_val="'${escaped}'"
            else
                local escaped
                escaped=$(echo "$raw" | sql_escape)
                sql_val="'${escaped}'"
            fi
            if $first; then
                vals="$sql_val"
                first=false
            else
                vals="$vals, $sql_val"
            fi
        done
        sql+="REPLACE INTO issues ($cols) VALUES ($vals);\n"
        issues_count=$((issues_count + 1))
    done < "$file"
    sql+="COMMIT;\n"

    if $DRY_RUN; then
        echo ""
        echo -e "$sql"
    else
        echo -e "$sql" | pipe_sql
        echo -e " ${GREEN}✓${NC} $issues_count rows"
    fi
}

# ── 2. Labels ──────────────────────────────────────────────────────
import_labels() {
    local file="$BACKUP_DIR/labels.jsonl"
    if [[ ! -f "$file" ]]; then
        echo -e "${YELLOW}⚠${NC}  labels.jsonl not found, skipping"
        return
    fi

    echo -ne "${BLUE}→${NC}  Importing labels..."

    local sql="START TRANSACTION;\n"
    while IFS= read -r line; do
        local issue_id label
        issue_id=$(echo "$line" | jq -r '.issue_id' | sql_escape)
        label=$(echo "$line" | jq -r '.label' | sql_escape)
        sql+="REPLACE INTO labels (issue_id, label) VALUES ('${issue_id}', '${label}');\n"
        labels_count=$((labels_count + 1))
    done < "$file"
    sql+="COMMIT;\n"

    if $DRY_RUN; then
        echo ""
        echo -e "$sql"
    else
        echo -e "$sql" | pipe_sql
        echo -e " ${GREEN}✓${NC} $labels_count rows"
    fi
}

# ── 3. Dependencies (with orphan handling) ─────────────────────────
import_dependencies() {
    local file="$BACKUP_DIR/dependencies.jsonl"
    if [[ ! -f "$file" ]]; then
        echo -e "${YELLOW}⚠${NC}  dependencies.jsonl not found, skipping"
        return
    fi

    echo -ne "${BLUE}→${NC}  Importing dependencies..."

    # Collect known issue IDs for orphan checking (newline-separated list)
    local known_ids=""
    if [[ -f "$BACKUP_DIR/issues.jsonl" ]]; then
        known_ids=$(jq -r '.id' "$BACKUP_DIR/issues.jsonl")
    fi

    local sql="START TRANSACTION;\n"
    while IFS= read -r line; do
        local issue_id depends_on_id dep_type created_at created_by
        issue_id=$(echo "$line" | jq -r '.issue_id')
        depends_on_id=$(echo "$line" | jq -r '.depends_on_id')
        dep_type=$(echo "$line" | jq -r '.type' | sql_escape)
        created_at=$(echo "$line" | jq -r '.created_at' | sql_escape)
        created_by=$(echo "$line" | jq -r '.created_by' | sql_escape)

        # Skip orphan dependencies (issue_id must exist; depends_on_id may be external)
        if ! echo "$known_ids" | grep -qxF "$issue_id"; then
            echo -e "\n  ${YELLOW}⚠${NC}  skipping orphan dep: issue_id=$issue_id not in issues" >&2
            deps_orphan=$((deps_orphan + 1))
            continue
        fi

        local esc_iid esc_did
        esc_iid=$(echo "$issue_id" | sql_escape)
        esc_did=$(echo "$depends_on_id" | sql_escape)
        sql+="REPLACE INTO dependencies (issue_id, depends_on_id, type, created_at, created_by) VALUES ('${esc_iid}', '${esc_did}', '${dep_type}', '${created_at}', '${created_by}');\n"
        deps_count=$((deps_count + 1))
    done < "$file"
    sql+="COMMIT;\n"

    if $DRY_RUN; then
        echo ""
        echo -e "$sql"
    else
        echo -e "$sql" | pipe_sql
        echo -e " ${GREEN}✓${NC} $deps_count rows ($deps_orphan orphans skipped)"
    fi
}

# ── 4. Events ──────────────────────────────────────────────────────
import_events() {
    local file="$BACKUP_DIR/events.jsonl"
    if [[ ! -f "$file" ]]; then
        echo -e "${YELLOW}⚠${NC}  events.jsonl not found, skipping"
        return
    fi

    echo -ne "${BLUE}→${NC}  Importing events..."

    local sql="START TRANSACTION;\n"
    while IFS= read -r line; do
        local id issue_id event_type actor old_value new_value comment created_at
        id=$(echo "$line" | jq -r '.id')
        issue_id=$(echo "$line" | jq -r '.issue_id' | sql_escape)
        event_type=$(echo "$line" | jq -r '.event_type' | sql_escape)
        actor=$(echo "$line" | jq -r '.actor' | sql_escape)
        created_at=$(echo "$line" | jq -r '.created_at' | sql_escape)

        # Nullable fields
        local old_val_sql new_val_sql comment_sql
        local old_raw new_raw comment_raw
        old_raw=$(echo "$line" | jq -r '.old_value // empty')
        new_raw=$(echo "$line" | jq -r '.new_value // empty')
        comment_raw=$(echo "$line" | jq -r '.comment // empty')

        if [[ -z "$old_raw" ]]; then old_val_sql="NULL"; else old_val_sql="'$(echo "$old_raw" | sql_escape)'"; fi
        if [[ -z "$new_raw" ]]; then new_val_sql="NULL"; else new_val_sql="'$(echo "$new_raw" | sql_escape)'"; fi
        if [[ -z "$comment_raw" ]]; then comment_sql="NULL"; else comment_sql="'$(echo "$comment_raw" | sql_escape)'"; fi

        sql+="INSERT IGNORE INTO events (id, issue_id, event_type, actor, old_value, new_value, comment, created_at) VALUES ($id, '${issue_id}', '${event_type}', '${actor}', ${old_val_sql}, ${new_val_sql}, ${comment_sql}, '${created_at}');\n"
        events_count=$((events_count + 1))
    done < "$file"
    sql+="COMMIT;\n"

    if $DRY_RUN; then
        echo ""
        echo -e "$sql"
    else
        echo -e "$sql" | pipe_sql
        echo -e " ${GREEN}✓${NC} $events_count rows"
    fi
}

# ── 5. Comments ────────────────────────────────────────────────────
import_comments() {
    local file="$BACKUP_DIR/comments.jsonl"
    if [[ ! -f "$file" ]]; then
        echo -e "${YELLOW}⚠${NC}  comments.jsonl not found, skipping"
        return
    fi

    echo -ne "${BLUE}→${NC}  Importing comments..."

    local sql="START TRANSACTION;\n"
    while IFS= read -r line; do
        local id issue_id author text created_at
        id=$(echo "$line" | jq -r '.id')
        issue_id=$(echo "$line" | jq -r '.issue_id' | sql_escape)
        author=$(echo "$line" | jq -r '.author' | sql_escape)
        text=$(echo "$line" | jq -r '.text' | sql_escape)
        created_at=$(echo "$line" | jq -r '.created_at' | sql_escape)

        sql+="INSERT IGNORE INTO comments (id, issue_id, author, text, created_at) VALUES ($id, '${issue_id}', '${author}', '${text}', '${created_at}');\n"
        comments_count=$((comments_count + 1))
    done < "$file"
    sql+="COMMIT;\n"

    if $DRY_RUN; then
        echo ""
        echo -e "$sql"
    else
        echo -e "$sql" | pipe_sql
        echo -e " ${GREEN}✓${NC} $comments_count rows"
    fi
}

# ── 6. Config ──────────────────────────────────────────────────────
import_config() {
    local file="$BACKUP_DIR/config.jsonl"
    if [[ ! -f "$file" ]]; then
        echo -e "${YELLOW}⚠${NC}  config.jsonl not found, skipping"
        return
    fi

    echo -ne "${BLUE}→${NC}  Importing config..."

    local sql="START TRANSACTION;\n"
    while IFS= read -r line; do
        local key value
        key=$(echo "$line" | jq -r '.key' | sql_escape)
        value=$(echo "$line" | jq -r '.value' | sql_escape)
        sql+="REPLACE INTO config (\`key\`, value) VALUES ('${key}', '${value}');\n"
        config_count=$((config_count + 1))
    done < "$file"
    sql+="COMMIT;\n"

    if $DRY_RUN; then
        echo ""
        echo -e "$sql"
    else
        echo -e "$sql" | pipe_sql
        echo -e " ${GREEN}✓${NC} $config_count rows"
    fi
}

# ── Verify Dolt connection ─────────────────────────────────────────
if ! $DRY_RUN; then
    echo -ne "${BLUE}→${NC}  Testing Dolt connection..."
    if ! echo "SELECT 1;" | pipe_sql &>/dev/null; then
        echo -e " ${RED}✗${NC}"
        echo "Error: cannot connect to Dolt at $HOST:$PORT (database: $DB)"
        echo "Is the Dolt server running? Try: bd dolt start"
        exit 1
    fi
    echo -e " ${GREEN}✓${NC}"
fi

# ── Run imports in FK order ────────────────────────────────────────
import_issues
import_labels
import_dependencies
import_events
import_comments
import_config

# ── Summary ────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════════════════════"
echo -e "${BOLD}Summary:${NC}"
echo "  Issues:        $issues_count"
echo "  Labels:        $labels_count"
echo "  Dependencies:  $deps_count ($deps_orphan orphans skipped)"
echo "  Events:        $events_count"
echo "  Comments:      $comments_count"
echo "  Config:        $config_count"
total=$((issues_count + labels_count + deps_count + events_count + comments_count + config_count))
echo "  Total:         $total rows imported"
if [[ $deps_orphan -gt 0 ]]; then
    echo ""
    echo -e "  ${YELLOW}⚠${NC}  $deps_orphan orphan dependencies were skipped"
fi
echo ""

# ── Dolt commit ────────────────────────────────────────────────────
if ! $DRY_RUN && [[ $total -gt 0 ]]; then
    echo -ne "${BLUE}→${NC}  Committing to Dolt..."
    echo "CALL dolt_add('-A'); CALL dolt_commit('-m', 'Import $total rows from JSONL migration (GH#2276)');" | pipe_sql &>/dev/null
    echo -e " ${GREEN}✓${NC}"
fi
