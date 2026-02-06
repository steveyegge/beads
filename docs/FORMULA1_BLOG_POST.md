# Race Control for Agent Teams

You've seen the demo. Five agents, one codebase, ten minutes, a feature ships. The pitch deck shows a DAG dissolving into green checkmarks. What it doesn't show is the three-car pileup at Turn 4 — two agents editing the same file, a third building on a spec that changed mid-flight, and your "autonomous engineering team" producing a diff that can't merge.

Multi-agent coding without coordination isn't engineering. It's a demolition derby.

## The Problem

When a single AI agent works on your codebase, the failure mode is manageable: it drifts from the spec, you correct it, life goes on. But when you launch three, five, eight agents in parallel — each with its own context window, its own interpretation of "the plan" — failure modes multiply combinatorially.

Agent A rewrites the auth module. Agent B adds a new endpoint that imports the old auth interface. Agent C updates the tests for code that no longer exists. Nobody told anyone. By the time you `git merge`, you're not debugging — you're doing archaeology.

This is the problem Shadowbook's `bd team` commands solve. And the cleanest way to explain how is to talk about Formula 1.

## The F1 Metaphor

In F1, twenty cars share the same track at 200 mph. They don't crash (usually) because of race control — the system that coordinates track state, monitors telemetry, enforces rules, and calls the red flag when conditions become unsafe.

Shadowbook is race control for agentic engineering. Here's the mapping:

**Specs are the track layout.** A spec defines the racing line — the intended path through the feature. It's the designed trajectory. Every agent reads the track before turning a wheel.

**Beads are the cars.** Each bead is a unit of work in motion. It has a status (queued, in-progress, done), an owner (which agent is driving), and a relationship to the spec it's executing against.

**Skills are the pit crew.** Skills are the tooling that keeps agents running correctly — prose instructions that tell Claude how to approach specific tasks. Without a good pit crew, even the fastest car DNFs.

**Wobble is tire degradation.** In F1, tires lose grip over a stint. In agentic engineering, skills degrade over context. Claude starts following instructions precisely, then gradually drifts — dropping a validation step here, improvising a shortcut there. This is wobble: skill drift measured over time.

**Volatility is track instability.** When a spec changes while agents are executing against it, the track itself is shifting. An agent following the original racing line is now heading for a wall that wasn't there two laps ago. Volatility quantifies how much a spec has changed since work began.

**Drift is running a different line.** The spec says "turn left at the auth module." The agent turns right. The code diverges from the spec's intent — not because the spec changed (that's volatility), but because the agent went its own way. Drift measures the gap between designed intent and actual output.

**Agent teams are the pit wall.** The pit wall in F1 coordinates strategy across all cars on the team. It decides who pits when, who attacks, who defends. `bd team` is the pit wall — it sees all agents, all beads, all specs, and coordinates the whole operation.

**File disjointness is the cardinal rule: no two cars on the same piece of track at the same time.** Shadowbook enforces that agents work on disjoint file sets. Two agents touching the same file is a collision waiting to happen.

## The `bd team` Commands

Six commands. Each maps to a phase of race operations.

```
bd team plan <epic>        # Race strategy briefing
```
Analyzes the epic's dependency graph and produces parallel waves — which beads can run simultaneously, which must wait. This is the strategist saying "Box car 1 on lap 15, car 2 stays out."

```
bd team watch              # Live telemetry dashboard
```
Real-time view of every agent's progress. Which bead is each agent working on? What's the completion percentage? Are any agents stuck? This is the telemetry feed the pit wall monitors during the race.

```
bd team gate <spec>        # Track inspection before green flag
```
Checks spec volatility before agents start work. If the spec has changed since the beads were created, gate blocks the start. You don't send cars onto a track that hasn't been inspected.

```
bd team score              # Championship points
```
Leaderboard showing agent throughput — beads completed, quality metrics, wobble scores. The constructor's championship, but for your agents.

```
bd team wobble             # Post-race debrief
```
Did the agents actually follow the strategy? Wobble analysis across the team — which agents drifted from their skills, where did instructions degrade? This is the engineers reviewing telemetry after the chequered flag.

```
bd team report             # Full race post-mortem
```
Complete metrics: drift per agent, volatility per spec, file collision near-misses, overall team efficiency. The Monday debrief where you figure out what to change for the next race.

## Why This Matters

The difference between a coordinated agent team and an uncoordinated one is the difference between a podium finish and a DNF you didn't see coming.

Without coordination, multi-agent coding is a bet: maybe the agents' work will compose cleanly, maybe you'll spend two hours untangling merge conflicts that erased the productivity gains. With coordination — file disjointness, spec gating, wobble monitoring, live telemetry — you get the actual promise of parallel autonomous work.

Full send, not full crash.

## Built on Beads

Shadowbook is a layer on top of [steveyegge/beads](https://github.com/steveyegge/beads), the open-source task-tracking format designed for agentic workflows. Beads are JSON. They work with any orchestrator — Claude Code, Codex, Aider, your custom harness. Shadowbook adds the coordination layer: specs, skills, drift, volatility, wobble, and now team operations.

The data is portable. The format is open. The race control is what Shadowbook brings.

---

*Shadowbook: because autonomous agents need a pit wall, not just a green light.*
