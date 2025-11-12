# Go Agent Example

Example Go agent that uses bd with optional Agent Mail coordination for multi-agent workflows.

## Features

- Uses native Go Agent Mail client (`pkg/agentmail`)
- Graceful degradation when Agent Mail unavailable
- Handles reservation conflicts
- Discovers and links new work
- Environment-based configuration

## Usage

### Git-only mode (no Agent Mail)

```bash
cd examples/go-agent
go run main.go --agent-name agent-alpha --max-iterations 5
```

### With Agent Mail coordination

```bash
# Start Agent Mail server (in separate terminal)
cd integrations/agent-mail
python server.py

# Run agent
cd examples/go-agent
go run main.go \
  --agent-name agent-alpha \
  --project-id my-project \
  --agent-mail-url http://127.0.0.1:8765 \
  --max-iterations 10
```

### Environment Variables

```bash
export BEADS_AGENT_NAME=agent-alpha
export BEADS_PROJECT_ID=my-project
export BEADS_AGENT_MAIL_URL=http://127.0.0.1:8765

go run main.go
```

## Multi-Agent Demo

Run multiple agents concurrently with Agent Mail:

```bash
# Terminal 1: Start Agent Mail server
cd integrations/agent-mail
python server.py

# Terminal 2: Agent Alpha
cd examples/go-agent
go run main.go --agent-name agent-alpha --agent-mail-url http://127.0.0.1:8765

# Terminal 3: Agent Beta
go run main.go --agent-name agent-beta --agent-mail-url http://127.0.0.1:8765
```

## How It Works

1. **Initialization**: Creates Agent Mail client with health check
2. **Find work**: Queries `bd ready` for unblocked issues
3. **Claim issue**: Reserves via Agent Mail (if enabled) and updates status to `in_progress`
4. **Work simulation**: Processes the issue (sleeps 1s in this example)
5. **Discover work**: 33% chance to create linked issue via `discovered-from` dependency
6. **Complete**: Closes issue and releases Agent Mail reservation

## Collision Handling

When Agent Mail is enabled:
- Issues are reserved before claiming (prevents race conditions)
- Conflicts return immediately (<100ms latency)
- Agents gracefully skip reserved issues

Without Agent Mail:
- Relies on git-based eventual consistency
- Higher latency (2-5s for sync)
- Collision detection via git merge conflicts

## Comparison with Python Agent

The Go implementation mirrors the Python agent (`examples/python-agent/agent_with_mail.py`):
- ✅ Same API surface (ReserveIssue, ReleaseIssue, Notify, CheckInbox)
- ✅ Same graceful degradation behavior
- ✅ Same environment variable configuration
- ✅ Native Go types and idioms (no shell exec for Agent Mail)

Key differences:
- Go uses `pkg/agentmail.Client` instead of `lib/beads_mail_adapter.py`
- Go struct methods vs Python class methods
- Type safety with Go structs
