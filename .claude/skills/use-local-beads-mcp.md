# Use Local Beads MCP Server

## Purpose
This skill documents how to configure Claude Code and Codex CLI to use the local development version of the beads-mcp server from this repository instead of the PyPI-installed package.

## PyPI vs Local Development Versions

### What is the PyPI Version?
- **PyPI** (Python Package Index) is the official Python package repository
- The **beads-mcp** package is published to PyPI at https://pypi.org/project/beads-mcp/
- Latest PyPI version: **0.23.1** (check with `pip index versions beads-mcp`)
- Installed via: `pip install beads-mcp` or `uv add beads-mcp`
- Command: `beads-mcp` (globally available after install)

### What is the Local Development Version?
- Source code located at: `/Users/jleechan/projects_other/beads/integrations/beads-mcp/`
- Current version in pyproject.toml: **0.23.1**
- Run via: `uv run python -m beads_mcp` (no installation needed)
- Changes take effect immediately (no rebuild/reinstall required)
- Used for development, testing, and debugging

### When to Use Each

**Use PyPI version when:**
- You want stable, released features
- Working on other projects (not developing beads itself)
- You need a simple, system-wide installation

**Use Local version when:**
- Developing or testing beads-mcp changes
- Testing unreleased features or bug fixes
- Debugging MCP server issues
- Contributing to beads development

## Quick Command Reference

### For Claude Code

#### Add to User Config (System-Wide)
```bash
# Remove existing beads MCP server from user config (if any)
claude mcp remove beads -s user

# Add local development version to user config (available in all projects)
claude mcp add -s user beads -e BEADS_USE_DAEMON=1 -e BEADS_PATH=/Users/jleechan/.local/bin/bd -- uv run --directory /Users/jleechan/projects_other/beads/integrations/beads-mcp python -m beads_mcp

# Verify configuration
claude mcp get beads
```

#### Add to Local Config (Project-Specific)
```bash
# Remove existing beads MCP server from local config (if any)
claude mcp remove beads -s local

# Add local development version to local config (this project only)
claude mcp add beads -e BEADS_USE_DAEMON=1 -e BEADS_PATH=/Users/jleechan/.local/bin/bd -- uv run --directory /Users/jleechan/projects_other/beads/integrations/beads-mcp python -m beads_mcp

# Verify configuration
claude mcp get beads
```

#### Add to BOTH Configs (Recommended for Development)
```bash
# Add to user config (for all projects)
claude mcp add -s user beads -e BEADS_USE_DAEMON=1 -e BEADS_PATH=/Users/jleechan/.local/bin/bd -- uv run --directory /Users/jleechan/projects_other/beads/integrations/beads-mcp python -m beads_mcp

# Add to local config (takes precedence in beads project)
claude mcp add beads -e BEADS_USE_DAEMON=1 -e BEADS_PATH=/Users/jleechan/.local/bin/bd -- uv run --directory /Users/jleechan/projects_other/beads/integrations/beads-mcp python -m beads_mcp

# List all configurations
claude mcp list
```

### For Codex CLI

```bash
# Create wrapper script (one-time setup)
cat > /Users/jleechan/projects_other/beads/integrations/beads-mcp/run-local-mcp.sh <<'EOF'
#!/bin/bash
# Wrapper script to run local beads-mcp for development
cd /Users/jleechan/projects_other/beads/integrations/beads-mcp
exec uv run python -m beads_mcp
EOF

chmod +x /Users/jleechan/projects_other/beads/integrations/beads-mcp/run-local-mcp.sh

# Remove existing beads MCP server (if any)
codex mcp remove beads

# Add local development version
# NOTE: Despite the help saying <COMMAND> <NAME>, the actual syntax is <NAME> <COMMAND>
codex mcp add --env BEADS_USE_DAEMON=1 --env BEADS_PATH=/Users/jleechan/.local/bin/bd beads /Users/jleechan/projects_other/beads/integrations/beads-mcp/run-local-mcp.sh

# Verify configuration
codex mcp get beads
codex mcp list | grep beads
```

## Important Notes

### Claude Code Syntax
- Name comes **first**: `claude mcp add <name> [options] -- <command>`
- Use `-e` for environment variables: `-e KEY=VALUE` (repeatable)
- Use `--` before the command to separate options from command arguments
- Scope options:
  - `-s user` or `--scope user`: Add to `~/.claude.json` (available in all projects)
  - `-s local` or `--scope local`: Add to `.claude.json` in project root (this project only)
  - No scope flag: Defaults to local config
- **Config Precedence**: Local config overrides user config when both exist

### Codex CLI Syntax
- **Contrary to help text**, name comes **first**: `codex mcp add [options] <name> <command>`
- Use `--env` for environment variables: `--env KEY=VALUE` (repeatable)
- Wrapper scripts are recommended for complex commands with multiple arguments
- Always added to global config (`~/.codex/config.toml`)
- No per-project config support (global only)

### Config Precedence Rules
When multiple configs define the same MCP server:

**Claude Code:**
1. `.claude.json` (local/project config) - **Highest priority**
2. `~/.claude.json` (user config)
3. Built-in defaults - **Lowest priority**

**Codex CLI:**
1. `~/.codex/config.toml` (global config) - **Only config**
2. Built-in defaults

**Recommendation for Development:**
- Add to **both** user and local configs in Claude Code
- This ensures local version is used in beads project and all other projects
- Add to **global** config in Codex CLI

### Testing the Configuration

```bash
# Test that MCP server is using local code
# In Claude Code, use the beads MCP tools:
# - set_context
# - where_am_i
# - list
# etc.

# Or check the MCP server logs for any errors
```

## Checking Versions

### Check PyPI Version
```bash
# Check latest available version on PyPI
pip index versions beads-mcp

# Check currently installed PyPI version
pip show beads-mcp | grep Version

# Check version via command
beads-mcp --version  # if installed globally
```

### Check Local Development Version
```bash
# Check version in pyproject.toml
grep '^version = ' /Users/jleechan/projects_other/beads/integrations/beads-mcp/pyproject.toml

# Or read the file
cat /Users/jleechan/projects_other/beads/integrations/beads-mcp/pyproject.toml | head -10
```

### Which Version is Claude Code Using?
```bash
# Check MCP server configuration
claude mcp get beads

# If command shows "beads-mcp" -> Using PyPI version
# If command shows "uv run" -> Using local development version
```

## Reverting to PyPI Version

### Claude Code - Remove Local, Use PyPI
```bash
# First, ensure beads-mcp is installed
pip install beads-mcp
# OR
uv pip install beads-mcp

# Remove local development configs
claude mcp remove beads -s local
claude mcp remove beads -s user

# Add PyPI version to user config
claude mcp add -s user beads -e BEADS_USE_DAEMON=1 -e BEADS_PATH=/Users/jleechan/.local/bin/bd -- beads-mcp

# Verify
claude mcp get beads  # Should show command: beads-mcp
```

### Codex CLI - Remove Local, Use PyPI
```bash
# Ensure beads-mcp is installed (same as above)

# Remove local development config
codex mcp remove beads

# Add PyPI version to global config
codex mcp add --env BEADS_USE_DAEMON=1 --env BEADS_PATH=/Users/jleechan/.local/bin/bd beads beads-mcp

# Verify
codex mcp get beads  # Should show command: beads-mcp
```

### Updating PyPI Version
```bash
# Update to latest PyPI version
pip install --upgrade beads-mcp
# OR
uv pip install --upgrade beads-mcp

# Restart Claude Code or Codex CLI session
# No config changes needed - it will use the new version automatically
```

## Troubleshooting

### MCP Server Not Connecting
1. Check that uv is installed and in PATH: `which uv`
2. Verify the wrapper script is executable: `ls -l run-local-mcp.sh`
3. Test the command manually:
   ```bash
   cd /Users/jleechan/projects_other/beads/integrations/beads-mcp
   uv run python -m beads_mcp
   ```
4. Check MCP server logs in Claude Code: use `--debug` flag

### Changes Not Reflected
- **Restart Claude Code or Codex CLI session** - MCP servers are started fresh for each session
- No need to rebuild/reinstall when using `uv run` - changes take effect on next session
- If still not working, check that you're editing the correct source directory
- Verify with: `which python` and `uv run which python` to ensure correct Python environment

### Wrong Version Being Used
```bash
# Check which version is configured
claude mcp get beads
codex mcp get beads

# If command shows "beads-mcp" but you want local:
#   Follow "Add to User/Local Config" section above

# If command shows "uv run" but you want PyPI:
#   Follow "Reverting to PyPI Version" section above

# Check config file locations to confirm
ls -la ~/.claude.json
ls -la .claude.json  # in beads project
ls -la ~/.codex/config.toml
```

### MCP Server Crashes or Errors
```bash
# Run MCP server manually to see error output
cd /Users/jleechan/projects_other/beads/integrations/beads-mcp
uv run python -m beads_mcp

# Check for Python/dependency issues
uv pip list | grep -E "(beads|fastmcp|pydantic)"

# Reinstall dependencies
cd /Users/jleechan/projects_other/beads/integrations/beads-mcp
uv sync

# Check bd daemon is running (if using BEADS_USE_DAEMON=1)
bd daemon status
bd daemon start  # if not running
```

### Environment Variables Not Working
```bash
# Verify environment variables are set in config
claude mcp get beads | grep Environment
codex mcp get beads | grep env

# Test with explicit env vars
BEADS_USE_DAEMON=1 BEADS_PATH=/Users/jleechan/.local/bin/bd uv run --directory /Users/jleechan/projects_other/beads/integrations/beads-mcp python -m beads_mcp
```

## File Locations

### Configuration Files
- Claude Code (user): `~/.claude.json`
- Claude Code (project): `.claude.json` in project root
- Codex CLI: `~/.codex/config.toml`

### Local Development Paths
- **MCP Server Source**: `/Users/jleechan/projects_other/beads/integrations/beads-mcp/`
- **Wrapper Script**: `/Users/jleechan/projects_other/beads/integrations/beads-mcp/run-local-mcp.sh`
- **BD Binary**: `/Users/jleechan/.local/bin/bd`
- **PyPI Package Location**: `~/.pyenv/versions/3.11.10/lib/python3.11/site-packages/beads_mcp/` (or similar)

### PyPI Package Information
- **Package Name**: beads-mcp
- **PyPI URL**: https://pypi.org/project/beads-mcp/
- **Latest Version**: 0.23.1 (as of last check)
- **GitHub**: https://github.com/steveyegge/beads
- **Installation**: `pip install beads-mcp` or `uv add beads-mcp`
- **Executable**: `beads-mcp` (when installed via pip/uv)

## Quick Reference Table

| Aspect | PyPI Version | Local Development |
|--------|--------------|-------------------|
| **Location** | `~/.pyenv/.../site-packages/` | `/Users/jleechan/projects_other/beads/integrations/beads-mcp/` |
| **Command** | `beads-mcp` | `uv run python -m beads_mcp` |
| **Installation** | `pip install beads-mcp` | Already available (source code) |
| **Updates** | `pip install --upgrade` | `git pull` in beads repo |
| **Changes** | Need reinstall | Immediate (next session) |
| **Version** | 0.23.1 (PyPI latest) | 0.23.1 (local source) |
| **Use Case** | Stable, production use | Development, testing |
| **Config (Claude)** | `-- beads-mcp` | `-- uv run --directory ... python -m beads_mcp` |
| **Config (Codex)** | `beads beads-mcp` | `beads /path/to/run-local-mcp.sh` |

## Common Workflows

### Workflow 1: Start Development Session
```bash
# 1. Pull latest changes
cd /Users/jleechan/projects_other/beads
git pull

# 2. Sync dependencies (if pyproject.toml changed)
cd integrations/beads-mcp
uv sync

# 3. Verify MCP config is using local version
claude mcp get beads | grep "uv run"  # Should see uv run in command

# 4. Start working - changes take effect on next Claude Code session
```

### Workflow 2: Test Local Changes
```bash
# 1. Make changes to beads-mcp source code
# (edit files in /Users/jleechan/projects_other/beads/integrations/beads-mcp/src/beads_mcp/)

# 2. Test manually first
cd /Users/jleechan/projects_other/beads/integrations/beads-mcp
uv run python -m beads_mcp  # Should start MCP server

# 3. Test in Claude Code - just start a new session
# No rebuild needed! Changes are live immediately

# 4. Check for errors in Claude Code with debug mode
claude --debug mcp
```

### Workflow 3: Switch Back to Stable PyPI Version
```bash
# Before important work, switch to stable PyPI version
claude mcp remove beads -s user
claude mcp add -s user beads -e BEADS_USE_DAEMON=1 -e BEADS_PATH=/Users/jleechan/.local/bin/bd -- beads-mcp

# Do the same for codex
codex mcp remove beads
codex mcp add --env BEADS_USE_DAEMON=1 --env BEADS_PATH=/Users/jleechan/.local/bin/bd beads beads-mcp

# After work, switch back to local development
# (use commands from "Add to User Config" section)
```
