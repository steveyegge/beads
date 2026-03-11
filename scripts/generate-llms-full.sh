#!/bin/bash
# Generate llms-full.txt from website documentation
# This concatenates all docs into a single file for LLM consumption

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
DOCS_DIR="$PROJECT_ROOT/website/docs"
OUTPUT_FILE="$PROJECT_ROOT/website/static/llms-full.txt"
BD="$PROJECT_ROOT/bd"

# Ensure bd binary exists
if [ ! -f "$BD" ]; then
    echo "Building bd..."
    cd "$PROJECT_ROOT"
    CGO_ENABLED=0 go build -o bd ./cmd/bd/
fi

# Header
cat > "$OUTPUT_FILE" << 'EOF'
# Beads Documentation (Complete)

> This file contains the complete beads documentation for LLM consumption.
> Generated automatically from the documentation source files.
> For the web version, visit: https://steveyegge.github.io/beads/

---

EOF

# Function to process a markdown file
process_file() {
    local file="$1"
    local relative_path="${file#$DOCS_DIR/}"

    echo "<document path=\"docs/$relative_path\">" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"

    # Remove frontmatter and add content
    sed '/^---$/,/^---$/d' "$file" >> "$OUTPUT_FILE"

    echo "" >> "$OUTPUT_FILE"
    echo "</document>" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"
}

# Process files in order (intro first, then by category)
if [ -f "$DOCS_DIR/intro.md" ]; then
    process_file "$DOCS_DIR/intro.md"
fi

# Process directories in logical order (conceptual docs only, skip cli-reference)
for dir in getting-started core-concepts architecture workflows multi-agent integrations recovery reference; do
    if [ -d "$DOCS_DIR/$dir" ]; then
        # Process index first if exists
        if [ -f "$DOCS_DIR/$dir/index.md" ]; then
            process_file "$DOCS_DIR/$dir/index.md"
        fi

        # Process other files
        for file in "$DOCS_DIR/$dir"/*.md; do
            if [ -f "$file" ] && [ "$(basename "$file")" != "index.md" ]; then
                process_file "$file"
            fi
        done
    fi
done

# Add live-generated CLI reference (replaces static cli-reference docs)
cat >> "$OUTPUT_FILE" << 'EOF'
---

# CLI Command Reference

> This section is auto-generated from the live bd command tree.
> Run `bd help --all` to regenerate this section.

EOF

# Add the generated CLI reference
$BD help --all >> "$OUTPUT_FILE"

# Add footer
cat >> "$OUTPUT_FILE" << 'EOF'
---

# End of Documentation

For updates and contributions, visit: https://github.com/steveyegge/beads
EOF

echo "Generated: $OUTPUT_FILE"
echo "Size: $(wc -c < "$OUTPUT_FILE" | tr -d ' ') bytes"
echo "Lines: $(wc -l < "$OUTPUT_FILE" | tr -d ' ')"
