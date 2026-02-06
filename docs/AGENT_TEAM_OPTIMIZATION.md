# Agent Team Optimization Guide

Practical strategies for getting the most out of Claude Code agent teams.

## Task Granularity

Aim for **10-30 minutes per task**. Too small and coordination overhead dominates — team setup, context loading, and message passing eat the gains. Too large and agents block each other, killing parallelism.

Good tasks have three properties:
- **File-disjoint** — no two agents touch the same file
- **Clear acceptance criteria** — the agent knows when it's done
- **Self-contained context** — minimal cross-task dependencies

If a task takes under 5 minutes, batch it with related work. If it takes over an hour, split it.

## File Disjointness: The Core Constraint

Two agents editing the same file means merge conflicts and wasted tokens. This is the single most important constraint in team planning.

`bd team plan` validates disjointness upfront — run it before spawning agents. If it flags overlap, restructure tasks or serialize the conflicting ones.

Architecture that enables disjointness:
- **One file per concern** — a component, a route handler, a utility module
- **Modular boundaries** — clear interfaces between files so agents can work on either side independently
- **Shared types in separate files** — don't mix type definitions with implementation

## Wave Optimization

Tasks form a DAG. Analyze it to find the critical path, then optimize wave structure.

**Front-load independent work.** If 4 of 6 tasks have no dependencies, run them all in wave 1. Don't artificially serialize.

**Keep waves low.** 2-3 waves beats 5+ waves every time. Each wave boundary adds coordination overhead — agents finishing early sit idle while stragglers complete. Restructure tasks to flatten the DAG when possible.

`bd team plan` estimates wave count and highlights the critical path. Use it to iterate on task structure before execution.

## Agent Type Selection

Match the agent to the work:

| Agent Type | Use For | Cost |
|---|---|---|
| **Explore / Plan** | Research, codebase analysis, architecture review | Lower (read-only) |
| **general-purpose** | Implementation, file editing, full-stack tasks | Standard |
| **backend-architect** | API design, database queries, CI/CD pipelines | Standard (specialized) |

Never assign write tasks to read-only agents — they'll fail silently or work around the limitation poorly. Use Explore agents for upfront research, then hand findings to implementation agents via task descriptions.

**Cost-match agents to tasks.** Use haiku-model agents for straightforward tasks (renaming, simple refactors, boilerplate). Reserve opus for tasks requiring judgment and architectural decisions.

## Context Minimization

Each agent gets a full context window. Don't waste it.

- Use the `files:` field in task descriptions to scope exactly which files an agent needs
- Provide relevant type signatures and interfaces, not entire modules
- Summarize cross-cutting context in 2-3 sentences rather than dumping file contents

Lean prompts mean faster agent startup and fewer hallucinations about code that isn't relevant.

## The bd team Pipeline

Shadowbook's `bd team` commands encode these optimizations into a repeatable workflow:

- **`bd team gate`** — Prevents premature team spawn. Checks that the spec is stable (not volatile) before letting you assign agents. Avoids wasting tokens on work that will be invalidated.
- **`bd team plan`** — Validates file disjointness across tasks, estimates wave count, and flags dependency issues. Run this iteratively until the plan is clean.
- **`bd team watch`** — Live observability during execution. See which agents are active, idle, or blocked without polling.
- **`bd team wobble`** — Detects agents that have drifted off-script. Catches an agent implementing the wrong approach before it wastes significant tokens.
- **`bd team report`** — Post-execution metrics: tokens spent, time per wave, conflicts encountered. Feed this back into planning for continuous improvement.

## Anti-Patterns

**Broadcasting when one agent needs info.** SendMessage with `type: "message"` to a specific agent. Broadcasts wake every agent and cost N messages.

**Over-polling idle agents.** Agents auto-notify when they complete tasks or need help. Idle state is normal — don't react to it.

**Sequential work disguised as parallel.** If agent B always waits on agent A, that's not parallel — it's serial with extra overhead. Restructure so both can start independently.

**Too many agents.** 3-4 is the sweet spot. Beyond that, coordination overhead grows faster than throughput. Each additional agent adds message traffic and increases conflict probability.

**Opus for haiku work.** Simple file moves, boilerplate generation, and straightforward refactors don't need the most capable model. Match cost to complexity.
