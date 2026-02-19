# Beads Claude Code Plugin

AI-supervised issue tracker for coding workflows. Manage tasks, discover work, and maintain context with simple slash commands and MCP tools.

## What is Beads?

Beads (`bd`) is an issue tracker designed specifically for AI-supervised coding workflows. It helps AI agents and developers:
- Track work with a simple CLI
- Discover and link related tasks during development
- Maintain context across coding sessions
- Auto-sync issues via JSONL for git workflows

## Installation

### Prerequisites

1. Install beads CLI:
```bash
curl -sSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash
```

2. Install Python and uv (for MCP server):
```bash
# Install uv (if not already installed)
curl -LsSf https://astral.sh/uv/install.sh | sh
```

### Install Plugin

There are two ways to install the beads plugin:

#### Option 1: From GitHub (Recommended)

```bash
# In Claude Code
/plugin marketplace add steveyegge/beads
/plugin install beads
```

#### Option 2: Local Development

```bash
# Clone the repository (shell command)
git clone https://github.com/steveyegge/beads
cd beads
```

Then in Claude Code:

```
# Add local marketplace (Claude Code command)
/plugin marketplace add ./beads

# Install plugin
/plugin install beads
```

**Note:** If you want to install the plugin from a different repo, first `cd` to that repo's directory in your terminal, then use `./beads` (or the relative path to the beads directory) in Claude Code.

### Restart Claude Code

After installation, restart Claude Code to activate the MCP server.

## Quick Start

```bash
# Initialize beads in your project
/beads:init

# Create your first issue
/beads:create "Set up project structure" feature 1

# See what's ready to work on
/beads:ready

# Show full workflow guide
/beads:workflow
```

## Available Commands

### Version Management

- **`/beads:version`** - Check bd CLI, plugin, and MCP server versions

### Core Workflow Commands

- **`/beads:ready`** - Find tasks with no blockers, ready to work on
- **`/beads:create [title] [type] [priority]`** - Create a new issue interactively
- **`/beads:show [issue-id]`** - Show detailed information about an issue
- **`/beads:update [issue-id] [status]`** - Update issue status or other fields
- **`/beads:close [issue-id] [reason]`** - Close a completed issue

### Project Management

- **`/beads:init`** - Initialize beads in the current project
- **`/beads:workflow`** - Show the AI-supervised issue workflow guide
- **`/beads:stats`** - Show project statistics and progress

### Agents

- **`@beads-issue-manager-agent`** - Lifecycle/write authority via deterministic `flow` wrappers
- **`@beads-execution-coordinator-agent`** - Implements and verifies claimed tasks
- **`@beads-query-agent`** - Read-only queue intelligence and context summaries
- **`@beads-cleanup-agent`** - Landing/handoff cleanup with resumable state
- **`@task-agent`** - Compatibility orchestrator that routes to split agents

### CLI vs Split-Agent Boundary

Use this ownership split to avoid policy drift:

| Owner | Scope |
|---|---|
| `CLI-owned` | Deterministic enforcement: readiness truth, claim/write guards, intake audits, close safety, and deterministic result states. |
| `Split-agent-owned` | Judgment and strategy: intent translation, decomposition strategy, prioritization/preemption choices, and architectural tradeoffs. |
| `MCP-owned` | Adapter translation only: pass lifecycle operations to `bd flow ...` and surface CLI payloads without local policy forks. |

## MCP Tools Available

The plugin includes a full-featured MCP server with these tools:

- **`init`** - Initialize bd in current directory
- **`create`** - Create new issue (bug, feature, task, epic, chore)
- **`list`** - List issues with filters (status, priority, type, assignee)
- **`ready`** - Find tasks with no blockers ready to work on
- **`show`** - Show detailed issue info including dependencies
- **`flow`** - Deterministic lifecycle wrappers (`claim_next`, `create_discovered`, `block_with_context`, `close_safe`, `transition`)
- **`update`** - Update issue (status, priority, design, notes, etc)
- **`close`** - Close completed issue
- **`dep`** - Add dependency (blocks, related, parent-child, discovered-from)
- **`blocked`** - Get blocked issues
- **`stats`** - Get project statistics

For agent workflows, prefer `flow` for lifecycle writes and keep raw write tools for direct/manual usage.

### MCP Resources

- **`beads://quickstart`** - Interactive quickstart guide

## Workflow

The beads workflow is designed for AI agents but works great for humans too:

1. **Find ready work**: `/beads:ready`
2. **Agent-managed lifecycle (recommended)**: use split agents and `flow` wrappers for claim/discovery/block/close
3. **Work on it**: Implement, test, document
4. **Discover new work**: create linked follow-ups via `flow(action="create_discovered", ...)`
5. **Complete safely**: `flow(action="close_safe", issue_id=..., reason=..., verification=..., require_traceability=true, require_spec_drift_proof=true, require_parent_cascade=true, require_priority_poll=true, non_hermetic=true)`
6. **Repeat**: Check for newly unblocked tasks

Manual/direct lifecycle mode is still available with `/beads:update` and `/beads:close`, but agent automation should prefer `flow`.

## Issue Types

- **`bug`** - Something broken that needs fixing
- **`feature`** - New functionality
- **`task`** - Work item (tests, docs, refactoring)
- **`epic`** - Large feature composed of multiple issues
- **`chore`** - Maintenance work (dependencies, tooling)

## Priority Levels

- **`0`** - Critical (security, data loss, broken builds)
- **`1`** - High (major features, important bugs)
- **`2`** - Medium (nice-to-have features, minor bugs)
- **`3`** - Low (polish, optimization)
- **`4`** - Backlog (future ideas)

## Dependency Types

- **`blocks`** - Hard dependency (issue X blocks issue Y from starting)
- **`related`** - Soft relationship (issues are connected)
- **`parent-child`** - Epic/subtask relationship
- **`discovered-from`** - Track issues discovered during work

Ready-work blockers include `blocks`, `parent-child`, `conditional-blocks`, and `waits-for`.

## Configuration

### Auto-Approval Configuration

By default, Claude Code asks for confirmation every time the beads MCP server wants to run a command. This is a security feature, but it can disrupt workflow during active development.

**Available Options:**

#### 1. Auto-Approve All Beads Tools (Recommended for Trusted Projects)

Add to your Claude Code `settings.json`:

```json
{
  "enabledMcpjsonServers": ["beads"]
}
```

This auto-approves all beads commands without prompting.

#### 2. Auto-Approve Project MCP Servers

Add to your Claude Code `settings.json`:

```json
{
  "enableAllProjectMcpServers": true
}
```

This auto-approves all MCP servers defined in your project's `.mcp.json` file. Useful when working across multiple projects with different MCP requirements.

#### 3. Manual Approval (Default)

No configuration needed. Claude Code will prompt for approval on each MCP tool invocation.

**Security Trade-offs:**

- **Manual approval (default)**: Maximum safety, but interrupts workflow frequently
- **Server-level auto-approval**: Convenient for trusted projects, but allows any beads operation without confirmation
- **Project-level auto-approval**: Good balance for multi-project workflows with project-specific trust levels

**Limitation:** Claude Code doesn't currently support per-tool approval granularity. You cannot auto-approve only read operations (like `bd ready`, `bd show`) while requiring confirmation for mutations (like `bd create`, `bd update`). It's all-or-nothing at the server level.

**Recommended Configuration:**

For active development on trusted projects where you're frequently using beads:

```json
{
  "enabledMcpjsonServers": ["beads"]
}
```

For more information, see the [Claude Code settings documentation](https://docs.claude.com/en/docs/claude-code/settings).

### Environment Variables

The MCP server supports these environment variables:

- **`BEADS_PATH`** - Path to bd executable (default: `bd` in PATH)
- **`BEADS_DB`** - Path to beads database file (default: auto-discover from cwd)
- **`BEADS_ACTOR`** - Actor name for audit trail (default: `$USER`)
- **`BEADS_NO_AUTO_FLUSH`** - Disable automatic JSONL sync (default: `false`)
- **`BEADS_NO_AUTO_IMPORT`** - Disable automatic JSONL import (default: `false`)

To customize, edit your Claude Code MCP settings or the plugin configuration.

## Examples

### Basic Task Management (Manual Mode)

```bash
# Create a high-priority bug
/beads:create "Fix authentication" bug 1

# See ready work
/beads:ready

# Start working on bd-10
/beads:update bd-10 in_progress

# Complete the task
/beads:close bd-10 "Fixed auth token validation"
```

### Discovering Work During Development (Manual Mode)

```bash
# Working on bd-10, found a related bug
/beads:create "Add rate limiting to API" feature 2

# Link it to current work (via MCP tool)
# Use `dep` tool: issue="bd-11", depends_on="bd-10", type="discovered-from"

# Close original task
/beads:close bd-10 "Done, discovered bd-11 for rate limiting"
```

### Using Split Agents

```bash
# Read-only queue snapshot
@beads-query-agent

# Claim/manage lifecycle via deterministic wrappers
@beads-issue-manager-agent

# Implement and verify claimed work
@beads-execution-coordinator-agent

# Land session and produce handoff
@beads-cleanup-agent
```

### Flow Wrapper Examples (Agent Lifecycle)

```bash
# Claim next task with WIP gating
# MCP tool: flow(action="claim_next")

# Create discovered follow-up linked to current issue
# MCP tool: flow(action="create_discovered", title="...", discovered_from_id="bd-123")

# Block with context pack and optional blocker edge
# MCP tool: flow(action="block_with_context", issue_id="bd-123", context_pack="...", blocker_id="bd-456")

# Close safely with reason lint + verification evidence + strict close controls
# MCP tool: flow(action="close_safe", issue_id="bd-123", reason="Implemented ...", verification="pytest ...", require_traceability=true, require_spec_drift_proof=true, require_parent_cascade=true, require_priority_poll=true, non_hermetic=true)

# Execute lifecycle transition handlers (for example session abort)
# MCP tool: flow(action="transition", transition_type="session_abort", reason="...", context_pack="...")
```

If a dependency link fails after a successful create/update (rare race), `flow` returns a structured error payload with:
- `issue_id`
- `details.partial_state`
- `recovery_command`

Run the returned `recovery_command` to reconcile state (manual/advanced lifecycle mode).

Compatibility mode:

```bash
# Legacy single entrypoint (delegates to split-agent model)
@task-agent
```

## Auto-Sync with Git

Beads automatically syncs issues to `.beads/issues.jsonl`:
- **Export**: After any CRUD operation (5-second debounce)
- **Import**: When JSONL is newer than DB (e.g., after `git pull`)

This enables seamless collaboration:
```bash
# Make changes
bd create "Add feature" -p 1

# Changes auto-export after 5 seconds
# Commit when ready
git add .beads/issues.jsonl
git commit -m "Add feature tracking"

# After pull, JSONL auto-imports
git pull
bd ready  # Shows issues ready to work on (with fresh data from git)
```

## Updating

The beads plugin has three components that may need updating:

### 1. Plugin Updates

Check for plugin updates:
```bash
/plugin update beads
```

Claude Code will pull the latest version from GitHub. After updating, **restart Claude Code** to apply MCP server changes.

### 2. bd CLI Updates

The plugin requires the `bd` CLI to be installed. Update it separately:

```bash
# Quick update
curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash

# Or with go
go install github.com/steveyegge/beads/cmd/bd@latest
```

### 3. Version Compatibility

The MCP server **automatically checks** bd CLI version on startup and will fail with a clear error if your version is too old.

Check version compatibility manually:
```bash
/beads:version
```

This will show:
- bd CLI version
- Plugin version
- MCP server status
- Compatibility warnings if versions mismatch

**Recommended update workflow:**
1. Check versions: `/beads:version`
2. Update bd CLI if needed (see above)
3. Update plugin: `/plugin update beads`
4. Restart Claude Code
5. Verify: `/beads:version`

### Version Numbering

Beads follows semantic versioning. The plugin version tracks the bd CLI version:
- Plugin 0.9.2 requires bd CLI >= 0.9.0 (checked automatically at startup)
- Major version bumps may introduce breaking changes
- Check CHANGELOG.md for release notes

## Troubleshooting

### Plugin not appearing

1. Check installation: `/plugin list`
2. Restart Claude Code
3. Verify `bd` is in PATH: `which bd`
4. Check uv is installed: `which uv`

### MCP server not connecting

1. Check MCP server list: `/mcp`
2. Look for "beads" server with plugin indicator
3. Restart Claude Code to reload MCP servers
4. Check logs for errors

### Commands not working

1. Make sure you're in a project with beads initialized: `/beads:init`
2. Check if database exists: `ls -la .beads/`
3. Try direct MCP tool access instead of slash commands
4. Check the beads CLI works: `bd --help`

### MCP tool errors

1. Verify `bd` executable location: `BEADS_PATH` env var
2. Check `bd` works in terminal: `bd stats`
3. Review MCP server logs in Claude Code
4. Try reinitializing: `/beads:init`

## Learn More

- **GitHub**: https://github.com/steveyegge/beads
- **Documentation**: See README.md in the repository
- **Examples**: Check `examples/` directory for integration patterns
- **MCP Server**: See `integrations/beads-mcp/` for server details

## Contributing

Found a bug or have a feature idea? Create an issue in the beads repository!

## License

MIT License - see LICENSE file in the repository.
