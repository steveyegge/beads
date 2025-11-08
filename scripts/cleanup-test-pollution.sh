#!/bin/bash
# Cleanup test pollution from bd database
# Removes issues created during development/testing

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BD="${SCRIPT_DIR}/../bd"

# Pattern to match test issues (case-insensitive)
# Matches: "Test issue", "Test Epic", "Test child", etc.
TEST_PATTERN="^Test (issue|Epic|child|parent|dependency|label|update|numeric|P1|FK|simple|lowercase)"

echo "=== BD Test Pollution Cleanup ==="
echo ""

# Find test issues
echo "Finding test pollution issues..."
TEST_ISSUES=$("$BD" list --json | jq -r --arg pattern "$TEST_PATTERN" \
  '.[] | select(.title | test($pattern; "i")) | .id')

if [ -z "$TEST_ISSUES" ]; then
  echo "✓ No test pollution found"
  exit 0
fi

COUNT=$(echo "$TEST_ISSUES" | wc -l | tr -d ' ')
echo "Found $COUNT test pollution issues:"
"$BD" list --json | jq -r --arg pattern "$TEST_PATTERN" \
  '.[] | select(.title | test($pattern; "i")) | "  - \(.id): \(.title)"'

echo ""
read -p "Delete these $COUNT issues? [y/N] " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "Aborted"
  exit 1
fi

# Delete via SQLite directly (bd doesn't have delete command yet)
DB="${SCRIPT_DIR}/../.beads/beads.db"
echo ""
echo "Deleting from database..."

echo "$TEST_ISSUES" | while read -r id; do
  echo "  Deleting $id..."
  sqlite3 "$DB" "DELETE FROM labels WHERE issue_id = '$id';"
  sqlite3 "$DB" "DELETE FROM dependencies WHERE issue_id = '$id' OR depends_on_id = '$id';"
  sqlite3 "$DB" "DELETE FROM comments WHERE issue_id = '$id';"
  sqlite3 "$DB" "DELETE FROM events WHERE issue_id = '$id';"
  sqlite3 "$DB" "DELETE FROM dirty_issues WHERE issue_id = '$id';"
  sqlite3 "$DB" "DELETE FROM export_hashes WHERE issue_id = '$id';"
  sqlite3 "$DB" "DELETE FROM issue_snapshots WHERE issue_id = '$id';"
  sqlite3 "$DB" "DELETE FROM compaction_snapshots WHERE issue_id = '$id';"
  sqlite3 "$DB" "DELETE FROM issues WHERE id = '$id';"
done

echo ""
echo "✓ Cleanup complete"
echo ""
echo "Run 'bd stats' to verify new counts"
echo "Run 'bd sync' to export cleaned database to JSONL"
