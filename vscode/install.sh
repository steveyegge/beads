#!/bin/bash
# Beads-First VS Code Integration Installer
# Run this from your project root to set up beads workflow

set -e

echo "═══════════════════════════════════════════════════════════════"
echo "Beads-First VS Code Integration Installer"
echo "═══════════════════════════════════════════════════════════════"
echo ""

# Detect if running from beads repo or target project
if [ -d "vscode/hooks" ]; then
    # Running from beads repo
    BEADS_VSCODE_DIR="./vscode"
    echo "✓ Running from beads repository"
else
    echo "ERROR: Run this script from the beads repository root"
    echo "  cd /path/to/beads"
    echo "  ./vscode/install.sh"
    exit 1
fi

echo ""
echo "Installation steps:"
echo "  1. Copy git hooks → .git/hooks/"
echo "  2. Copy scripts → scripts/"
echo "  3. Copy skills → .claude/skills/"
echo "  4. Copy templates → ./, .vscode/"
echo "  5. Update .gitignore"
echo "  6. Initialize beads (if needed)"
echo ""
read -p "Continue? (y/n) " -n 1 -r
echo ""

if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Installation cancelled"
    exit 0
fi

echo ""
echo "Installing..."

# 1. Copy hooks
echo "→ Installing git hooks..."
mkdir -p .git/hooks
cp "$BEADS_VSCODE_DIR/hooks"/* .git/hooks/
chmod +x .git/hooks/*
echo "  ✓ Hooks installed"

# 2. Copy scripts
echo "→ Installing scripts..."
mkdir -p scripts
cp "$BEADS_VSCODE_DIR/scripts"/*.sh scripts/ 2>/dev/null || true
cp "$BEADS_VSCODE_DIR/scripts"/*.ps1 scripts/ 2>/dev/null || true
chmod +x scripts/*.sh 2>/dev/null || true
echo "  ✓ Scripts installed"

# 3. Copy skills
echo "→ Installing Claude skills..."
mkdir -p .claude/skills
cp -r "$BEADS_VSCODE_DIR/skills"/* .claude/skills/
echo "  ✓ Skills installed"

# 4. Copy templates (don't overwrite existing)
echo "→ Installing templates..."
if [ ! -f "CLAUDE.md" ]; then
    cp "$BEADS_VSCODE_DIR/templates/CLAUDE.md" .
    echo "  ✓ CLAUDE.md created"
else
    echo "  ⊘ CLAUDE.md exists (not overwriting)"
fi

mkdir -p .vscode
if [ ! -f ".vscode/settings.json" ]; then
    cp "$BEADS_VSCODE_DIR/templates/settings.json" .vscode/
    echo "  ✓ settings.json created"
else
    echo "  ⊘ settings.json exists (not overwriting)"
fi

if [ ! -f ".vscode/keybindings.json" ]; then
    cp "$BEADS_VSCODE_DIR/templates/keybindings.json" .vscode/
    echo "  ✓ keybindings.json created"
else
    echo "  ⊘ keybindings.json exists (not overwriting)"
fi

# 5. Update .gitignore
echo "→ Updating .gitignore..."
if ! grep -q ".session-active" .gitignore 2>/dev/null; then
    echo "" >> .gitignore
    echo "# Beads Session Markers" >> .gitignore
    echo ".beads/.session-active" >> .gitignore
    echo ".beads/.landing-complete" >> .gitignore
    echo "  ✓ .gitignore updated"
else
    echo "  ⊘ .gitignore already has session markers"
fi

# 6. Initialize beads if needed
echo "→ Checking beads initialization..."
if [ ! -d ".beads" ]; then
    if command -v bd &> /dev/null; then
        read -p "Initialize beads? (y/n) " -n 1 -r
        echo ""
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            bd init
            echo "  ✓ Beads initialized"
        else
            echo "  ⊘ Beads initialization skipped"
        fi
    else
        echo "  ⚠ beads (bd) not found in PATH"
        echo "  Install beads first: https://github.com/steveyegge/beads"
    fi
else
    echo "  ✓ Beads already initialized"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "Installation Complete!"
echo "═══════════════════════════════════════════════════════════════"
echo ""
echo "Next steps:"
echo "  1. Restart VS Code to load new settings"
echo "  2. Open chat and load beads-bootup skill (Ctrl+Shift+B)"
echo "  3. Complete your work"
echo "  4. Load beads-landing skill before pushing (Ctrl+Shift+L)"
echo ""
echo "Git hooks are now active and will enforce the workflow:"
echo "  • pre-commit: Ensures bootup skill was loaded"
echo "  • pre-push: Ensures landing skill was loaded"
echo "  • post-commit: Auto-syncs beads state"
echo "  • post-merge: Auto-imports remote changes"
echo ""
echo "View events:"
echo "  ./scripts/view-events.sh --last 20"
echo "  ./scripts/view-events.sh --summary"
echo ""
