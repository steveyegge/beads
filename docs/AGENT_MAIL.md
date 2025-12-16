# Agent Mail Integration Guide

**Status:** Optional Enhancement  
**Minimum bd Version:** 0.21.0  
**Related ADR:** [002-agent-mail-integration.md](adr/002-agent-mail-integration.md)

## Overview

MCP Agent Mail provides real-time coordination for multi-agent beads workflows, reducing latency from 2-5 seconds (git sync) to <100ms (HTTP API) and preventing work collisions through file reservations.

**Key Benefits:**
- **20-50x latency reduction**: <100ms vs 2000-5000ms for status updates
- **Collision prevention**: Exclusive file reservations prevent duplicate work
- **Lightweight**: <50MB memory, simple HTTP API
- **100% optional**: Git-only mode works identically without it

## Quick Start

### Prerequisites
- Python 3.11+
- bd 0.21.0+
- Multi-agent workflow (2+ AI agents working on same repository)

### Installation

```bash
# Install Agent Mail server
git clone https://github.com/Dicklesworthstone/mcp_agent_mail.git
cd mcp_agent_mail
python3 -m venv .venv
source .venv/bin/activate  # Windows: .venv\Scripts\activate
pip install -e .

# Start server
python -m mcp_agent_mail.cli serve-http
# Server runs on http://127.0.0.1:8765
```

### Configuration

Enable Agent Mail by setting environment variables before running bd commands:

```bash
# Agent 1
export BEADS_AGENT_MAIL_URL=http://127.0.0.1:8765
export BEADS_AGENT_NAME=assistant-alpha
export BEADS_PROJECT_ID=my-project

# Agent 2
export BEADS_AGENT_MAIL_URL=http://127.0.0.1:8765
export BEADS_AGENT_NAME=assistant-beta
export BEADS_PROJECT_ID=my-project

# Now run bd commands normally
bd ready
bd update bd-42 --status in_progress
```

**All configuration is via environment variables** - no changes to `.beads/` or git required.

### Verification

```bash
# Check if Agent Mail is active
bd info --json | grep agent_mail

# View reservations in web UI
open http://127.0.0.1:8765/mail

# Test collision prevention
# Terminal 1 (Agent A):
bd update bd-123 --status in_progress

# Terminal 2 (Agent B):
bd update bd-123 --status in_progress
# Expected: Error - bd-123 reserved by assistant-alpha
```

## How It Works

### Architecture

```
┌─────────────────────────────────────────────┐
│ bd (Beads CLI)                              │
│                                             │
│  ┌─────────────┐      ┌─────────────────┐  │
│  │ Git Sync    │      │ Agent Mail      │  │
│  │ (required)  │      │ (optional)      │  │
│  │             │      │                 │  │
│  │ - Export    │      │ - Reservations  │  │
│  │ - Import    │      │ - Notifications │  │
│  │ - Commit    │      │ - Status updates│  │
│  │ - Push/Pull │      │                 │  │
│  └─────────────┘      └─────────────────┘  │
│         │                      │            │
└─────────┼──────────────────────┼────────────┘
          │                      │
          ▼                      ▼
  ┌──────────────┐      ┌──────────────┐
  │ .beads/      │      │ Agent Mail   │
  │ issues.jsonl │      │ Server       │
  │ (git)        │      │ (HTTP)       │
  └──────────────┘      └──────────────┘
```

**Git remains the source of truth.** Agent Mail provides ephemeral coordination state only.

### Coordination Flow

**Without Agent Mail (git-only):**
```
Agent A: bd update bd-123 --status in_progress
  ↓ (30s debounce)
  ↓ export to JSONL
  ↓ git commit + push (1-2s)
  ↓ 
Agent B: git pull (1-2s)
  ↓ import from JSONL
  ↓ sees bd-123 is taken (TOO LATE - work already started!)

Total latency: 2000-5000ms
Risk: Both agents work on same issue
```

**With Agent Mail:**
```
Agent A: bd update bd-123 --status in_progress
  ↓ 
  ├─ Agent Mail: POST /api/reservations (5ms)
  │  └─ Reserve bd-123 for Agent A
  ├─ Local: Update .beads/beads.db
  └─ Background: Export to JSONL (30s debounce)
  
Agent B: bd update bd-123 --status in_progress
  ↓
  └─ Agent Mail: POST /api/reservations (5ms)
     └─ HTTP 409 Conflict: "bd-123 reserved by Agent A"
     └─ bd exits with clear error

Total latency: <100ms
Risk: Zero - collision prevented at reservation layer
```

### Integration Points

Agent Mail integrates at 4 key points in the bd workflow:

1. **Issue Reservation** (`bd update --status in_progress`)
   - Check if issue already reserved
   - Create reservation if available
   - Error 409 if conflict

2. **Issue Release** (`bd close`)
   - Release reservation automatically
   - Notify other agents (optional)

3. **Ready Work Query** (`bd ready`)
   - Filter out reserved issues (future enhancement)
   - Show only truly available work

4. **Status Updates** (`bd update --priority`, etc.)
   - No reservation required for non-status changes
   - Graceful degradation if server unavailable

## Configuration Reference

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `BEADS_AGENT_MAIL_URL` | Yes | None | Agent Mail server URL (e.g., `http://127.0.0.1:8765`) |
| `BEADS_AGENT_NAME` | Yes | None | Unique agent identifier (e.g., `assistant-alpha`) |
| `BEADS_PROJECT_ID` | Yes | None | Project namespace (e.g., `my-project`) |
| `BEADS_AGENT_MAIL_TOKEN` | No | None | Bearer token for authentication (future) |

### Example Configurations

**Local Development (Single Machine):**
```bash
# ~/.bashrc or ~/.zshrc
export BEADS_AGENT_MAIL_URL=http://127.0.0.1:8765
export BEADS_AGENT_NAME=$(whoami)-$(hostname)
export BEADS_PROJECT_ID=$(basename $(pwd))
```

**Multi-Machine Setup:**
```bash
# Machine 1 (runs server)
export BEADS_AGENT_MAIL_URL=http://192.168.1.100:8765
export BEADS_AGENT_NAME=dev-machine-1
export BEADS_PROJECT_ID=beads

# Machine 2 (client only)
export BEADS_AGENT_MAIL_URL=http://192.168.1.100:8765
export BEADS_AGENT_NAME=dev-machine-2
export BEADS_PROJECT_ID=beads
```

**Docker Compose:**
```yaml
services:
  agent-mail:
    image: ghcr.io/dicklesworthstone/mcp_agent_mail:latest
    ports:
      - "8765:8765"
    
  agent-1:
    image: my-beads-agent
    environment:
      BEADS_AGENT_MAIL_URL: http://agent-mail:8765
      BEADS_AGENT_NAME: worker-1
      BEADS_PROJECT_ID: my-project
```

## When to Use Agent Mail

### ✅ Use Agent Mail When:

1. **Multiple AI agents** working on the same repository simultaneously
2. **Frequent status updates** (multiple agents claiming/releasing work)
3. **Collision-sensitive workflows** (duplicate work is expensive)
4. **Real-time coordination needed** (latency matters)
5. **CI/CD integration** (agents triggered by webhooks/events)

**Example Scenario:**
- Team has 3 AI agents (Claude, GPT-4, Gemini)
- Agents pull from work queue every 5 minutes
- Repository has 50+ open issues
- Duplicate work costs 30+ minutes to resolve

### ❌ Skip Agent Mail When:

1. **Single agent** workflows (no collision risk)
2. **Infrequent updates** (once per day/week)
3. **Git-only infrastructure** (no external services allowed)
4. **Offline workflows** (no network connectivity)
5. **Low issue volume** (<10 open issues)

**Example Scenario:**
- Solo developer using beads for personal task tracking
- Updates happen once per session
- No concurrent work
- Simplicity over latency

## Graceful Degradation

**Agent Mail failure NEVER breaks beads.** If the server is unavailable:

1. bd logs a warning: `Agent Mail unavailable, falling back to git-only mode`
2. All operations proceed normally
3. Git sync handles coordination (with higher latency)
4. No errors, no crashes

**Test graceful degradation:**
```bash
# Stop Agent Mail server
pkill -f "mcp_agent_mail.cli"

# bd commands work identically
bd ready  # Success
bd update bd-42 --status in_progress  # Success (git-only mode)
```

**Automatic fallback conditions:**
- Server not responding (connection refused)
- Server returns 500+ errors
- Network timeout (>5s)
- Invalid/missing environment variables

## Multi-Machine Deployment

### Centralized Server Pattern (Recommended)

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Agent A    │────▶│ Agent Mail  │◀────│  Agent B    │
│ (Machine 1) │     │   Server    │     │ (Machine 2) │
└─────────────┘     │ (Machine 3) │     └─────────────┘
                    └─────────────┘
                           │
                           ▼
                    ┌─────────────┐
                    │ PostgreSQL  │ (optional)
                    │  Storage    │
                    └─────────────┘
```

**Steps:**
1. Deploy Agent Mail on dedicated server/container
2. Configure firewall to allow port 8765 from agent machines
3. Set `BEADS_AGENT_MAIL_URL` to server IP on all agents
4. (Optional) Configure PostgreSQL backend for persistence

### Peer-to-Peer Pattern (Advanced)

```
┌─────────────┐     ┌─────────────┐
│  Agent A    │────▶│  Agent B    │
│ + Server    │     │ + Server    │
└─────────────┘     └─────────────┘
       │                   │
       └─────────┬─────────┘
                 ▼
          ┌─────────────┐
          │ Shared DB   │ (e.g., Redis)
          └─────────────┘
```

**Use case:** High-availability setups where server can't be single point of failure.

## Troubleshooting

### Issue: "Agent Mail unavailable" warnings

**Symptoms:**
```
WARN Agent Mail unavailable, falling back to git-only mode
```

**Solutions:**
1. Check server is running: `curl http://127.0.0.1:8765/health`
2. Verify `BEADS_AGENT_MAIL_URL` is set correctly
3. Check firewall allows connections to port 8765
4. Review server logs: `tail -f ~/.mcp_agent_mail/logs/server.log`

### Issue: "Reservation conflict" errors

**Symptoms:**
```
Error: bd-123 already reserved by assistant-alpha
```

**Solutions:**
1. **Expected behavior** - another agent claimed the issue first
2. Find different work: `bd ready`
3. If reservation is stale (agent crashed), release manually:
   ```bash
   curl -X DELETE http://127.0.0.1:8765/api/reservations/bd-123
   ```
4. Configure reservation TTL to auto-expire stale locks (future)

### Issue: Reservations persist after agent crashes

**Symptoms:**
- Agent crashes mid-work
- Reservation not released
- Other agents can't claim the issue

**Solutions:**
1. **Manual release** via web UI: http://127.0.0.1:8765/mail
2. **API release**: `curl -X DELETE http://127.0.0.1:8765/api/reservations/<issue-id>`
3. **Restart server** (clears all ephemeral state): `pkill -f mcp_agent_mail; python -m mcp_agent_mail.cli serve-http`
4. **Future:** Configure TTL to auto-expire (not yet implemented)

### Issue: Two agents have different project IDs

**Symptoms:**
- Agents don't see each other's reservations
- Collisions still happen

**Solutions:**
1. Ensure all agents use **same** `BEADS_PROJECT_ID`
2. Check environment: `echo $BEADS_PROJECT_ID`
3. Set globally in shell profile:
   ```bash
   # ~/.bashrc
   export BEADS_PROJECT_ID=my-project
   ```

### Issue: Server uses too much memory

**Symptoms:**
- Agent Mail process grows to 100+ MB
- Server becomes slow

**Solutions:**
1. **Normal for in-memory mode** (<50MB baseline + reservation data)
2. Use PostgreSQL backend for large-scale deployments
3. Configure reservation expiry to prevent unbounded growth
4. Restart server periodically (ephemeral state is OK to lose)

## Migration from Git-Only Mode

**Good news:** Zero migration required! Agent Mail is purely additive.

### Step 1: Test with Single Agent
```bash
# Start server
python -m mcp_agent_mail.cli serve-http

# Configure one agent
export BEADS_AGENT_MAIL_URL=http://127.0.0.1:8765
export BEADS_AGENT_NAME=test-agent
export BEADS_PROJECT_ID=test

# Run normal workflow
bd ready
bd update bd-42 --status in_progress
bd close bd-42 "Done"

# Verify in web UI
open http://127.0.0.1:8765/mail
```

### Step 2: Add Second Agent
```bash
# In separate terminal/machine
export BEADS_AGENT_MAIL_URL=http://127.0.0.1:8765
export BEADS_AGENT_NAME=test-agent-2
export BEADS_PROJECT_ID=test

# Try claiming same issue (should fail)
bd update bd-42 --status in_progress
# Expected: Error - reservation conflict
```

### Step 3: Roll Out to Production
1. Deploy Agent Mail server to production environment
2. Add environment variables to agent deployment configs
3. Monitor logs for "Agent Mail unavailable" warnings
4. Gradually enable for all agents

### Rollback Plan
Simply **unset environment variables** - agents immediately fall back to git-only mode:
```bash
unset BEADS_AGENT_MAIL_URL
unset BEADS_AGENT_NAME
unset BEADS_PROJECT_ID

# bd works identically without Agent Mail
bd ready
```

## FAQ

### Q: Do I need Agent Mail for single-agent workflows?
**A:** No. Agent Mail is only useful for multi-agent coordination. Single agents get no benefit from it.

### Q: Can I use Agent Mail with the MCP server (beads-mcp)?
**A:** Yes! Set the environment variables before starting beads-mcp, and it will use Agent Mail for all operations.

### Q: What happens if Agent Mail and git get out of sync?
**A:** Git is the source of truth. If Agent Mail has stale reservation data, worst case is a 409 error. Agent can manually release and retry.

### Q: Does Agent Mail require changes to .beads/ or git?
**A:** No. Agent Mail is 100% external. No changes to database schema, JSONL format, or git workflow.

### Q: Can I use Agent Mail for multiple projects on the same server?
**A:** Yes. Set different `BEADS_PROJECT_ID` for each project. Agent Mail provides namespace isolation.

### Q: Is Agent Mail required for beads 1.0?
**A:** No. Agent Mail is an optional enhancement. Git-only mode is fully supported indefinitely.

### Q: How does Agent Mail handle authentication?
**A:** Currently, no authentication (local network only). Bearer token support planned for future release.

### Q: Can I self-host Agent Mail on corporate infrastructure?
**A:** Yes. Agent Mail is open source (MIT license) and can be deployed anywhere Python runs.

### Q: What's the performance impact of Agent Mail?
**A:** ~5ms overhead per reservation API call. Negligible compared to 2000-5000ms git sync.

### Q: Does Agent Mail work with protected branches?
**A:** Yes. Agent Mail operates independently of git workflow. Use with `bd config set sync.branch beads-metadata` as normal.

## Advanced Topics

### Custom Reservation TTL
```bash
# Future feature (not yet implemented)
export BEADS_RESERVATION_TTL=3600  # 1 hour in seconds
```

### PostgreSQL Backend
```bash
# For production deployments with persistence
export AGENT_MAIL_DB_URL=postgresql://user:pass@localhost/agentmail
python -m mcp_agent_mail.cli serve-http
```

### Monitoring & Observability
```bash
# Server exposes Prometheus metrics (future)
curl http://127.0.0.1:8765/metrics

# Health check
curl http://127.0.0.1:8765/health
```

### Integration with CI/CD
```yaml
# GitHub Actions example
jobs:
  agent-workflow:
    runs-on: ubuntu-latest
    services:
      agent-mail:
        image: ghcr.io/dicklesworthstone/mcp_agent_mail:latest
        ports:
          - 8765:8765
    env:
      BEADS_AGENT_MAIL_URL: http://localhost:8765
      BEADS_AGENT_NAME: github-actions-${{ github.run_id }}
      BEADS_PROJECT_ID: ${{ github.repository }}
    steps:
      - run: bd ready | head -1 | xargs -I {} bd update {} --status in_progress
```

## Resources

- [ADR 002: Agent Mail Integration](adr/002-agent-mail-integration.md)
- [Agent Mail Repository](https://github.com/Dicklesworthstone/mcp_agent_mail)
- [Python Agent Example](../examples/python-agent/)

## Contributing

Found a bug or want to improve Agent Mail integration? See:
- [CONTRIBUTING.md](../CONTRIBUTING.md) for beads contribution guidelines
- [Agent Mail Issues](https://github.com/Dicklesworthstone/mcp_agent_mail/issues) for server-side issues

## License

Beads: Apache 2.0  
MCP Agent Mail: MIT
