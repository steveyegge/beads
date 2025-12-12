# Beads VS Code Integration

This package provides VS Code integration for Beads-First applications, including:

- **Claude Skills** for session rituals (bootup, landing, scope)
- **Event logging** infrastructure for complete observability
- **Git hooks** with logging
- **Templates** for CLAUDE.md, settings, and keybindings

## Quick Start

### Option A: Automated Installation (Recommended)

```bash
# From the beads repository root
./vscode/install.sh
```

This script will:
- Copy all hooks, scripts, skills, and templates
- Update your .gitignore
- Optionally initialize beads
- Set correct permissions

### Option B: Manual Installation

```bash
# From your project root
cp -r /path/to/beads/vscode/* ./

# Or on Windows
xcopy /E /I C:\path\to\beads\vscode .\vscode-beads
```

Then manually:
- Copy hooks to `.git/hooks/`
- Copy scripts to `scripts/`
- Copy skills to `.claude/skills/`
- Copy templates to project root
- Add session markers to `.gitignore`

### 3. Run InitApp Ceremony

Open VS Code chat and invoke the beads-init-app skill:

```
Load the beads-init-app skill and initialize this application
```

This creates the InitApp epic that blocks all other work until complete.

### 4. Work Through InitApp

```bash
bd ready --json  # Shows first unblocked task
# Complete each task
bd close <id> --reason "Done"
# Repeat until InitApp can close
```

### 5. Establish Epoch

```bash
bd close bd-0001 --reason "All initialization complete"
git tag -a epoch-v1 -m "Beads-first foundation"
```

## Directory Structure

After setup, your project should have:

```
your-project/
├── .beads/
│   ├── beads.jsonl      # Domain memory
│   └── events.log       # Event log
├── .claude/
│   └── skills/
│       ├── beads-bootup/
│       ├── beads-landing/
│       ├── beads-scope/
│       └── beads-init-app/
├── .git/
│   └── hooks/
│       ├── pre-commit
│       ├── post-commit
│       ├── pre-push
│       └── post-merge
├── scripts/
│   ├── beads-log-event.sh
│   └── beads-log-event.ps1
├── CLAUDE.md
└── .vscode/
    ├── settings.json
    └── keybindings.json
```

## Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+Shift+B` | Start beads session (bootup) |
| `Ctrl+Shift+L` | End beads session (landing) |
| `Ctrl+Shift+R` | Run `bd ready` in terminal |
| `Ctrl+Shift+S` | Sync beads and git status |
| `Ctrl+Shift+E` | View recent events |

## Implementation Status

| Component | Status | Functionality |
|-----------|--------|---------------|
| **Skills** | 🟡 GREEN FIELD | Log activation only (processing comes later) |
| **Git Hooks** | 🟢 ACTIVE | **Enforcing workflow** |
| **Event Logging** | 🟢 ACTIVE | Fully functional |
| **Templates** | 🟢 ACTIVE | Ready to use |

### Skills (Logging Only)

All skills currently:
- ✅ Create session marker files
- ✅ Log activation events
- ⏳ Don't perform ritual processing (future enhancement)

Skills serve as an **observability layer** - they track when rituals are triggered.

### Git Hooks (ENFORCING)

Hooks are **ACTIVE** and will block operations if rituals aren't followed:

- **pre-commit** 🔴 **BLOCKS** commits without active session
- **pre-push** 🔴 **BLOCKS** pushes without completed landing
- **post-commit** ✅ Auto-syncs beads state
- **post-merge** ✅ Auto-imports remote changes

## Session Marker Mechanism

The workflow enforcement uses marker files:

**`.beads/.session-active`**
- Created when beads-bootup skill loads
- Checked by pre-commit hook
- Contains timestamp

**`.beads/.landing-complete`**
- Created when beads-landing skill loads
- Checked by pre-push hook
- Deleted after successful push

These files are gitignored and serve as proof that rituals were followed.

## Event Logging

View events:
```bash
cat .beads/events.log
tail -f .beads/events.log  # Live monitoring
```

Log custom events:
```bash
./scripts/beads-log-event.sh "proj.custom.event" "bd-0001" "details here"
```

See `events/EVENT_TAXONOMY.md` for all event codes.

## Requirements

- **beads CLI** (`bd`) installed and in PATH
- **VS Code** 1.107+ (Insiders) for Claude Skills support
- **Git** for version control
- **Bash** or **PowerShell** for scripts

## Troubleshooting

### "No active beads session detected" on commit

**Cause**: You didn't load the beads-bootup skill before committing.

**Solution**:
```
1. Open VS Code chat (Ctrl+Shift+B)
2. Type: "Load beads-bootup skill"
3. Execute the bootup commands (creates .beads/.session-active)
4. Try commit again
```

### "Landing ritual not completed" on push

**Cause**: You didn't load the beads-landing skill before pushing.

**Solution**:
```
1. Open VS Code chat (Ctrl+Shift+L)
2. Type: "Load beads-landing skill"
3. Execute the landing commands (creates .beads/.landing-complete)
4. Try push again
```

### Events not logging
```bash
# Check script is executable
ls -la scripts/beads-log-event.sh

# Check .beads directory exists
ls -la .beads/

# Run manually to test
./scripts/beads-log-event.sh test.event none "test"
cat .beads/events.log
```

### Hooks not triggering
```bash
# Check hooks are in place
ls -la .git/hooks/

# Check hooks are executable
chmod +x .git/hooks/*

# Test manually
.git/hooks/pre-commit
```

### Beads doctor failures
```bash
# Run doctor to see issues
bd doctor

# Common fixes:
bd sync              # Sync beads state
bd import --force    # Reimport from JSONL
```

### Skills not loading
- Verify `.claude/skills/` structure
- Check VS Code version supports Claude Skills (1.107+)
- Restart VS Code after adding skills

## Documentation

- [BEADS_HARNESS_PATTERN.md](../docs/BEADS_HARNESS_PATTERN.md) - Full pattern documentation
- [EVENT_TAXONOMY.md](events/EVENT_TAXONOMY.md) - Event code reference
- [Beads CLI Reference](../docs/CLI_REFERENCE.md) - bd command documentation
