# TypeScript Agent Example

Simple TypeScript agent workflow using bd (Beads issue tracker).

## Overview

This demonstrates how an agent can:

1. **Find ready work** - Query for unblocked issues
2. **Claim tasks** - Set status to `in_progress`
3. **Execute work** - Simulate doing the task
4. **Discover issues** - Create new issues found during work
5. **Link discoveries** - Add `discovered-from` dependencies
6. **Complete work** - Close issues with reason

## Prerequisites

- Node.js 18+
- bd (beads) installed and in PATH
- A beads-initialized project directory

## Installation

```bash
cd examples/ts-agent
npm install
```

## Usage

```bash
# Run with ts-node (development)
npm start

# Or with custom iteration count
npx ts-node agent.ts 5

# Build and run compiled JS
npm run build
node dist/agent.js
```

## Agent Workflow

```
┌─────────────────────────────────────────────────────────────┐
│                     Agent Loop                              │
│                                                             │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐              │
│  │  Find    │───▶│  Claim   │───▶│  Work    │              │
│  │  Ready   │    │  Task    │    │  (sim)   │              │
│  └──────────┘    └──────────┘    └────┬─────┘              │
│       ▲                               │                     │
│       │                               ▼                     │
│       │                        ┌──────────────┐             │
│       │                        │  Discovered  │             │
│       │                        │  new issue?  │             │
│       │                        └──────┬───────┘             │
│       │                               │                     │
│       │         ┌─────────────────────┼─────────────────┐   │
│       │         │ YES                 │ NO              │   │
│       │         ▼                     ▼                 │   │
│       │   ┌──────────┐          ┌──────────┐            │   │
│       │   │  Create  │          │ Complete │            │   │
│       │   │  Issue   │          │  Task    │────────────┘   │
│       │   └────┬─────┘          └──────────┘                │
│       │        │                                            │
│       │        ▼                                            │
│       │   ┌──────────┐                                      │
│       │   │   Link   │                                      │
│       │   │   Dep    │                                      │
│       │   └────┬─────┘                                      │
│       │        │                                            │
│       └────────┴────────────────────────────────────────────┘
└─────────────────────────────────────────────────────────────┘
```

## Code Structure

```typescript
class BeadsAgent {
  // Core bd interaction
  runBd(...args: string[]): unknown      // Run bd with --json
  runBdRaw(...args: string[]): void      // Run bd without JSON

  // Task lifecycle
  findReadyWork(): BeadsIssue | null     // bd ready --limit 1
  claimTask(id: string): void            // bd update --status in_progress
  completeTask(id: string, reason): void // bd close --reason

  // Discovery pattern
  createIssue(title, options): BeadsIssue // bd create
  linkDiscovery(new, parent): void        // bd dep add --type discovered-from

  // Simulation
  simulateWork(issue): boolean           // Returns true if discovered work

  // Main loop
  runOnce(): boolean                     // Single iteration
  run(maxIterations): void               // Full agent loop
}
```

## Example Output

```
🚀 Beads Agent starting...

═══════════════════════════════════════════════════
  Beads Statistics
═══════════════════════════════════════════════════
Total Issues:           15
Open:                   3
In Progress:            0
Closed:                 12

============================================================
Iteration 1/10
============================================================
📋 Claiming task: bd-abc1
✓ Task claimed

🤖 Working on: Implement user auth (bd-abc1)
   Priority: 1, Type: feature

💡 Discovered: Missing test coverage for this feature
✨ Creating issue: Add tests for Implement user auth
🔗 Linking bd-xyz2 ← discovered-from ← bd-abc1
✓ Dependency linked
✅ Completing task: bd-abc1 - Implemented successfully
✓ Task completed: bd-abc1

🔄 New work discovered and linked. Running another cycle...

============================================================
Iteration 2/10
============================================================
📭 No ready work found.

✨ Agent finished!
```

## Comparison with Other Agents

| Feature | bash-agent | python-agent | ts-agent |
|---------|------------|--------------|----------|
| Runtime | Bash + jq | Python 3 | Node.js + TypeScript |
| Type Safety | None | Type hints | Full TypeScript |
| Async Support | No | No | Ready (uses sync for simplicity) |
| Dependencies | jq | None | ts-node, typescript |
| Best For | CI/CD, scripts | Quick prototypes | Production apps |

## Extending

To use this agent with a real LLM:

```typescript
async simulateWork(issue: BeadsIssue): Promise<boolean> {
  // Replace simulation with actual LLM call
  const response = await llm.complete({
    prompt: `Complete this task: ${issue.title}\n${issue.description}`,
  });

  // Parse LLM response for discovered issues
  const discoveries = parseDiscoveries(response);

  for (const discovery of discoveries) {
    const newIssue = this.createIssue(discovery.title, {
      description: discovery.description,
    });
    this.linkDiscovery(newIssue.id, issue.id);
  }

  return discoveries.length > 0;
}
```

## Related

- [bash-agent](../bash-agent/) - Bash implementation
- [python-agent](../python-agent/) - Python implementation
- [library-usage](../library-usage/) - Using bd as a Go library
