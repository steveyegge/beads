---
id: aider
title: Aider
sidebar_position: 3
---

# Aider Integration

How to use beads with Aider.

## Setup

### Quick Setup

```bash
bd setup aider
```

This creates/updates `.aider.conf.yml` with beads context.

### Verify Setup

```bash
bd setup aider --check
```

## Configuration

The setup adds to `.aider.conf.yml`:

```yaml
# Beads integration
read:
  - .beads/issues.jsonl

# Optional: Auto-run bd prime
auto-commits: false
```

## Workflow

### Start Session

```bash
# Aider will have access to issues via .aider.conf.yml
aider

# Or manually inject context
bd prime | aider --message-file -
```

### During Work

Use bd commands alongside aider:

```bash
# In another terminal or after exiting aider
bd create "Found bug during work" --deps discovered-from:bd-42 --json
bd update bd-42 --status in_progress
bd ready
```

### End Session

```bash
# Dolt handles sync automatically - bd sync is deprecated
# Manual export if needed:
bd export
```

## Best Practices

1. **Keep issues visible** - Aider reads `.beads/issues.jsonl`
2. **Dolt syncs automatically** -- manual `bd sync` is deprecated; use `bd export` if you need a manual export
3. **Use discovered-from** - Track issues found during work
4. **Document context** - Include descriptions in issues

## Example Workflow

```bash
# 1. Check ready work
bd ready

# 2. Start aider with issue context
aider --message "Working on bd-42: Fix auth bug"

# 3. Work in aider...

# 4. Create discovered issues
bd create "Found related bug" --deps discovered-from:bd-42 --json

# 5. Complete work (Dolt syncs automatically)
bd close bd-42 --reason "Fixed"
bd export  # Optional manual export
```

## Troubleshooting

### Config not loading

```bash
# Check config exists
cat .aider.conf.yml

# Regenerate
bd setup aider
```

### Issues not visible

```bash
# Check JSONL exists
ls -la .beads/issues.jsonl

# Export if missing
bd export
```

## See Also

- [Claude Code](/integrations/claude-code)
- [IDE Setup](/getting-started/ide-setup)
