# Beads VS Code Integration

This package provides VS Code integration for Beads-First applications, including:

- **Claude Skills** for session rituals (bootup, landing, scope)
- **Event logging** infrastructure for complete observability
- **Git hooks** with logging
- **Templates** for CLAUDE.md, settings, and keybindings

## Quick Start

### 1. Copy to Your Project

```bash
# From your project root
cp -r /path/to/beads/vscode/* ./

# Or on Windows
xcopy /E /I C:\path\to\beads\vscode .\vscode-beads
```

### 2. Run Initialization

```bash
# Make scripts executable (Linux/Mac)
chmod +x scripts/*.sh hooks/*

# Initialize beads (if not already done)
bd init

# Install hooks
cp hooks/* .git/hooks/

# Copy skills to .claude directory
mkdir -p .claude/skills
cp -r skills/* .claude/skills/

# Copy CLAUDE.md template
cp templates/CLAUDE.md ./CLAUDE.md
```

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

## Green Field Status

All skills are currently in **GREEN FIELD** mode:
- They log their activation
- They don't perform processing yet
- This allows verification of event logging

Once you verify events are logging correctly (check `.beads/events.log`),
you can enable full processing in each skill.

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

### Skills not loading
- Verify `.claude/skills/` structure
- Check VS Code version supports Claude Skills
- Restart VS Code after adding skills

## Documentation

- [BEADS_HARNESS_PATTERN.md](../docs/BEADS_HARNESS_PATTERN.md) - Full pattern documentation
- [EVENT_TAXONOMY.md](events/EVENT_TAXONOMY.md) - Event code reference
- [Beads CLI Reference](../docs/CLI_REFERENCE.md) - bd command documentation
