# beads-mcp

MCP server for [beads](https://github.com/steveyegge/beads) issue tracker and agentic memory system.
Enables AI agents to manage tasks using bd CLI through Model Context Protocol.

## Installing

Install from PyPI:

```bash
# Using uv (recommended)
uv tool install beads-mcp

# Or using pip
pip install beads-mcp
```

Add to your Claude Desktop config:

```json
{
  "mcpServers": {
    "beads": {
      "command": "beads-mcp"
    }
  }
}
```

### Development Installation

For development, clone the repository:

```bash
git clone https://github.com/steveyegge/beads
cd beads/integrations/beads-mcp
uv sync
```

Then use in Claude Desktop config:

```json
{
  "mcpServers": {
    "beads": {
      "command": "uv",
      "args": [
        "--directory",
        "/path/to/beads-mcp",
        "run",
        "beads-mcp"
      ]
    }
  }
}
```

**Environment Variables** (all optional):
- `BEADS_PATH` - Path to bd executable (default: `~/.local/bin/bd`)
- `BEADS_DB` - Path to beads database file (default: auto-discover from cwd)
- `BEADS_WORKING_DIR` - Working directory for bd commands (default: `$PWD` or current directory)
- `BEADS_ACTOR` - Actor name for audit trail (default: `$USER`)
- `BEADS_NO_AUTO_FLUSH` - Disable automatic JSONL sync (default: `false`)
- `BEADS_NO_AUTO_IMPORT` - Disable automatic JSONL import (default: `false`)

## Features

**Resource:**
- `beads://quickstart` - Quickstart guide for using beads

**Tools:**
- `init` - Initialize bd in current directory
- `create` - Create new issue (bug, feature, task, epic, chore)
- `list` - List issues with filters (status, priority, type, assignee)
- `ready` - Find tasks with no blockers ready to work on
- `show` - Show detailed issue info including dependencies
- `update` - Update issue (status, priority, design, notes, etc)
- `close` - Close completed issue
- `dep` - Add dependency (blocks, related, parent-child, discovered-from)
- `blocked` - Get blocked issues
- `stats` - Get project statistics
- `reopen` - Reopen a closed issue with optional reason


## Development

Run MCP inspector:
```bash
# inside beads-mcp dir
uv run fastmcp dev src/beads_mcp/server.py
```

Type checking:
```bash
uv run mypy src/beads_mcp
```

Linting and formatting:
```bash
uv run ruff check src/beads_mcp
uv run ruff format src/beads_mcp
```

## Testing

Run all tests:
```bash
uv run pytest
```

With coverage:
```bash
uv run pytest --cov=beads_mcp tests/
```

Test suite includes both mocked unit tests and integration tests with real `bd` CLI.
